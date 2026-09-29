# dsh-go-ported 需求分析与技术方案

> 状态：需求分析稿
> 目标：在不修改现有移动端 App、不修改第三方官方 DSH 的前提下，以 Go 重写一套可自部署的 DSH 远程控制服务端和本地工作机 Connector。

## 1. 目标与不可变约束

### 1.1 目标

- 云端部署 Go Server，提供移动端/Web 所需的会话、Timeline、消息、交互、附件和终端能力。
- 本地工作机部署 Go Connector，主动连接 Go Server，执行本地文件、Shell、终端以及 DSH runtime 操作。
- 复用现有 `dsh-bridge-next` 的 DSH 业务实现和 Connector-DSH Bridge 线协议。
- 移动端继续使用当前已经实现的协议和数据格式，移动端代码不修改。
- 服务端尽量保持薄逻辑，不依赖 MySQL、PostgreSQL、Redis；优先使用本地文件，必要时使用 SQLite。
- 开发和测试环境全部部署在当前 Mac 工作机上；手机通过与 Mac 相同的局域网访问 Go Server，完成真实移动端链路验证。
- REST API 使用 Beego；WebSocket/SSE 只保留移动端现有协议实际需要的部分。

### 1.2 不可变约束

- 不修改官方 DSH 源码、官方 CLI 行为或官方存储格式。
- 不使用当前仓库的非官方 DSH Desktop 作为运行载体。
- 官方 DSH 只采用 CLI/profile/headless/sdk 等官方入口；`dsh-bridge` 通过官方 CLI 提供的插件和 Host 能力接入。
- 不把 DSH 原生端口暴露到公网。
- Go Server 不直接调用 DSH；所有 DSH 操作必须通过本地 Go Connector 和 `dsh-bridge` 完成。
- 不能因为服务端简化而改变移动端可见的 JSON 字段、事件顺序、错误语义和协议路径。

## 2. 总体架构

```text
移动端 / Web（同一局域网手机通过 Mac 局域网 IP 访问）
    │ HTTP/HTTPS + WebSocket/SSE
    ▼
Go Server（当前 Mac，监听 0.0.0.0 或 Mac 局域网 IP）
    ├── 静态文件/HTTP API
    ├── 客户端固定密钥认证
    ├── 内存在线连接表
    ├── 文件会话仓库
    ├── 文件附件仓库
    └── Connector WebSocket RPC Hub
            │ 本机回环或本机局域网连接
            ▼
Go Connector（当前 Mac 本地常驻进程）
    ├── Server WebSocket 客户端
    ├── RPC 请求分发
    ├── 本地文件/Shell/PTY
    └── DSH Bridge endpoint client
            │ 本机 stdio/localhost JSON-RPC
            ▼
官方 DSH CLI + dsh-bridge
    ├── DSH 会话发现和历史读取
    ├── Timeline 投影
    ├── 消息/附件/配置/中断
    └── ask_user_question / 实时事件
```

### 2.1 组件职责

#### Go Server

只拥有远程控制层状态：

- 固定密钥认证。
- 一个或多个本地 Connector 的连接注册和在线状态。
- 移动端/Web API 兼容层。
- Server 到 Connector 的 RPC 请求路由。
- Connector 上报的会话元数据、Timeline、通知持久化。
- 附件接收、下载、向 Connector 转发。
- WebSocket/SSE 事件广播。
- 终端 relay。

不拥有：

- DSH 原生会话业务语义。
- DSH 模型调用。
- DSH 原始日志解析。
- 用户、OAuth、复杂租户和设备授权体系。

#### Go Connector

- 使用配置文件中的 Server 地址和密钥主动回连。
- 维持单条或有限数量的 WebSocket 长连接。
- 接收 Server RPC，映射为本地操作。
- 启动和监控官方 DSH CLI 及 `dsh-bridge`。
- 连接 DSH Bridge endpoint，转发 `contracts/dsh-bridge/1.0` JSON-RPC。
- 将 DSH Bridge 的 response/notification 转换成移动端兼容的 Server ingest 事件。
- 处理本地文件、Shell、PTY 和附件暂存。

#### dsh-bridge

继续承担全部 DSH 特有业务：

- 会话发现、历史读取和 Timeline 投影。
- 实时事件订阅和重连补偿。
- 新建会话、续聊、消息发送、中断。
- 图片、普通文件、模型、effort、权限和 Agent preset。
- 审批/用户问答响应。
- DSH 原生 endpoint 鉴权和 DSH 版本适配。

Go 层不能重新实现这些语义，否则会形成第二套 DSH 业务实现并放大版本兼容风险。

## 3. 移动端兼容协议范围

现有移动端兼容边界以 `/api/v2` 为基线。Go Server 应保留以下逻辑路径，具体字段以当前客户端实际请求和仓库协议 schema 为准：

### 3.1 必须实现

```text
GET  /api/v2/health

GET/POST /api/v2/connectors/*
GET/POST /api/v2/connector/*

GET/POST /api/v2/sessions/*
GET/POST /api/v2/sessions/{id}/events
GET/POST /api/v2/sessions/{id}/messages
POST /api/v2/sessions/{id}/interrupt
POST /api/v2/sessions/{id}/interaction/*

POST /api/v2/sessions/{id}/attachments
GET  /api/v2/sessions/{id}/attachments/{fileId}
GET  /api/v2/sessions/{id}/attachments/{fileId}/open

WS   /api/v2/connector/ws
WS   /api/v2/connector/terminals/{terminalId}/relay
SSE/WS 会话事件流，按当前移动端实际使用的路径保留
```

### 3.2 可以删除或替换

以下功能不属于首期目标，可直接返回明确的 unsupported：

- 用户注册、登录、OAuth、刷新 token。
- 多租户和复杂账号权限。
- 管理员用户体系。
- 多 Server 实例路由。
- Redis lease、Pub/Sub 和分布式锁。
- 复杂 pairing/onboarding。
- 其他 Codex/Claude runtime 的完整适配，如果首期只服务 DSH。

但是不能删除移动端启动时实际调用的接口。应先抓取当前移动端协议流量，形成兼容测试集。

### 3.3 认证模型

移动端和 Connector 均使用固定密钥：

```text
X-DSH-Key: <configured-key>
Authorization: Bearer <configured-key>
?token=<configured-key> 仅在现有 WebSocket 协议要求时使用
```

实现建议：

- 服务端配置 `client_key`。
- 客户端请求统一使用 HTTPS。
- Connector 使用独立配置项但首期可以与客户端共用密钥。
- 所有 WebSocket 握手也必须认证。
- 不把密钥写入 URL、日志或事件正文；如果现有协议只能通过 query token，则限制其日志输出。
- 后续如需区分手机和 Connector，可增加 `connector_key`，不改变移动端协议。

## 4. 文件持久化设计

### 4.1 根目录

```text
/data/dsh-go-ported/
├── config.yaml
├── server.lock
├── sessions/
│   └── {sessionId}/
│       ├── meta.json
│       ├── timeline.jsonl
│       ├── state.json
│       └── events/
│           └── {sequence}.json
├── attachments/
│   └── {fileId}/
│       ├── meta.json
│       └── content
├── connectors/
│   └── {connectorId}.json
├── runtime/
│   └── {connectorId}.json
└── logs/
    └── server.jsonl
```

### 4.2 会话文件

用户指定的基本约定是：会话用文件夹保存，文件名或目录标识使用 session ID，内容保存当时历史记录。推荐采用目录而不是单个超大 JSON 文件：

- `meta.json`：session ID、connector ID、runtime、cwd、标题、创建时间、更新时间、能力。
- `timeline.jsonl`：每行一个标准 Timeline item 或事件，追加写入。
- `state.json`：当前状态、阻塞状态、归档状态、待处理 interaction。
- `events/{sequence}.json`：可选；用于 SSE/断线恢复和排查。

写入规则：

- 临时文件写入后 `fsync`，再原子 rename。
- Timeline 事件按 session 内 sequence 单调递增。
- 每个 session 使用独立 mutex。
- 读历史时允许按游标分页。
- 服务重启后通过扫描目录恢复 session index；index 可作为内存缓存，不是唯一事实来源。
- 并发写入不能依赖 JSON 整体覆盖，实时路径使用 JSONL append。

### 4.3 附件

附件必须先落盘，再把元数据转发给 Connector：

```text
移动端上传
  → Server attachments/{fileId}/content
  → Server 生成 uploadId/fileId 元数据
  → Connector 拉取或接收文件
  → Connector 写入 DSH Bridge staging
  → dsh-bridge 调用 DSH 官方 file/image upload API
```

附件元数据至少包括：

- fileId
- uploadId
- name
- mediaType
- size
- sha256
- sessionId
- createdAt
- status

文件名必须做路径隔离，不能直接拼接用户输入。

## 5. Go Server 模块设计

建议目录：

```text
dsh-go-ported/server/
├── cmd/dsh-server/main.go
├── internal/config/
├── internal/auth/
├── internal/httpapi/
├── internal/ws/
├── internal/rpc/
├── internal/session/
├── internal/attachment/
├── internal/connector/
├── internal/terminal/
├── internal/event/
├── internal/storage/
│   ├── filesystem/
│   └── sqlite/              # 可选，不作为默认实现
└── contracts/               # 从现有协议生成/复制的 Go 类型
```

### 5.1 Beego 层

- Router 仅负责路径、认证 middleware 和请求/响应编解码。
- 业务 service 不依赖 Beego context。
- WebSocket 使用稳定库实现，挂在 Beego route 下。
- SSE 仅实现移动端确实使用的事件流；如果移动端已有 WebSocket 替代能力，不额外引入 SSE。

### 5.2 RPC Hub

内存结构：

```text
map[connectorID]*ConnectorConn
map[requestID]PendingRequest
map[sessionID][]ClientSubscriber
```

RPC 方向：

```text
Mobile/Web → Go Server → Go Connector → dsh-bridge
Go Connector → Go Server → Mobile/Web
```

需要处理：

- request ID 唯一性。
- response 与 notification 分离。
- 单请求超时和取消。
- Connector 断线时 pending request 返回明确错误。
- 写请求不因断线自动重试，避免重复执行。
- notification 按 session 顺序写入并广播。
- Connector 重连后主动发送初始化和已有 session 基线同步。

### 5.3 Connector 在线状态

首期不做复杂 lease：

- WebSocket 鉴权成功即 online。
- 收到 ping/pong 或固定 heartbeat 即刷新 `lastSeen`。
- 连接关闭或超时即 offline。
- 服务重启后内存连接表清空，Connector 自动重连。
- `connectors/{id}.json` 只保存静态元数据和最后一次状态，不作为实时状态唯一来源。

## 6. Go Connector 设计

建议目录：

```text
dsh-go-ported/connector/
├── cmd/dsh-connector/main.go
├── internal/config/
├── internal/serverconn/
├── internal/rpc/
├── internal/localfs/
├── internal/shell/
├── internal/terminal/
├── internal/attachments/
├── internal/dshbridge/
└── internal/process/
```

配置示例：

```yaml
server_url: https://dsh.example.com
connector_id: workstation-001
client_key: replace-me
workspace_roots:
  - /Users/me/workspace
 dsh:
  home: /Users/me/.dsh
  profile: headless
  bridge_endpoint: /Users/me/.dsh/agents-anywhere/bridge/endpoint.json
```

Connector 启动流程：

1. 加载配置并检查数据目录权限。
2. 启动或连接官方 DSH CLI/profile。
3. 启动/发现 dsh-bridge endpoint。
4. 读取 endpoint 地址和一次性鉴权信息。
5. 连接 Go Server WebSocket。
6. 发送 connector initialize/capabilities。
7. 发送 DSH runtime discovery 和初始 session inventory。
8. 持续接收 Server RPC。
9. 将 DSH notification 转换为 Server ingest。
10. 断线后指数退避重连，并重新发送基线同步。

## 7. 官方 DSH CLI 兼容性判断

### 7.1 当前公开事实

截至本分析，官方 GitHub 仓库公开的 `package.json` 版本为 `0.1.0-rc.5`，仓库明确标注为 developer preview，并警告存在兼容性破坏。官方 CLI 提供：

- `dsh --profile <name>`
- `dsh --profile headless "job"`
- `dsh web`
- `dsh plugin --profile <name> ...`
- `web`、`headless`、`sdk`、`sdk-minimal`、`acp` profile

官方架构以 Cordis plugin/profile 为核心，session log、agent loop、模型、工具和 UI 都是插件能力。

### 7.2 对本方案的支持程度

官方 DSH 的架构方向支持本方案：

- CLI 可以独立运行，不依赖非官方 Desktop。
- profile 可以组合官方 bundle 和第三方插件。
- headless/sdk 适合被本地 Connector 托管。
- session log 是持久事件流，适合由 `dsh-bridge` 读取和投影。
- 官方插件机制允许继续使用现有 DSH Bridge Host 逻辑。

但目前不能直接断言完全兼容，原因是当前仓库 `dsh-bridge-next` 使用了 Host/Client、SessionController、sessionQuery、fileUploads 和 DSH 原生服务的具体 API；官方仓库仍在 developer preview，版本变化可能破坏这些接口。

### 7.3 必须先做的 CLI PoC

在正式开发 Go Server 前，固定一个官方 DSH 版本并验证：

1. `dsh --profile headless` 能否加载现有 dsh-bridge Host。
2. `dsh --profile sdk` 是否提供可稳定连接的 JSON-RPC/remote gateway。
3. 是否能获取 session list、session snapshot、session state。
4. 是否能订阅实时 session/agent 事件。
5. 是否能创建 session、发送 turn、interrupt。
6. 是否能收到 ask_user_question 并提交 response。
7. 是否能上传图片和普通文件。
8. 是否能在 CLI profile 下暴露当前 bridge endpoint。
9. DSH 重启后 session log、session ID 和事件序号是否稳定。
10. 官方 CLI 更新到下一 RC 后，dsh-bridge 是否只需更新适配层。

如果 PoC 不能满足以上条件，不能通过修改 DSH 解决；应只在 `dsh-bridge` 中增加版本适配，或固定官方 DSH 版本。

## 8. 现有 dsh-bridge 的复用策略

### 8.1 保留

保留现有：

- `contracts/dsh-bridge/1.0` request/response/notification schema。
- `dsh-bridge-next/src/host/dsh-runtime` 的 DSH 业务实现。
- DSH 原生历史、Timeline、attachments、configuration、interaction 处理。
- Host 与 DSH endpoint 的鉴权和实例互斥。

### 8.2 替换

替换以下部分：

- Python Connector 的 Server transport。
- AA Server 的 OAuth、用户、设备注册和复杂 connector 管理。
- PostgreSQL/Redis repository。
- 当前插件内部启动 Python Connector 的管理路径。

### 8.3 不建议

不建议让 Go Server 直接解析 DSH session log；这会复制 `dsh-runtime` 的业务投影并破坏“DSH 版本变化只影响 bridge”的边界。

## 9. 事件和实时传输建议

首期优先级：

1. HTTP REST：查询、发送控制命令、附件上传下载。
2. Connector WebSocket：Server 与工作机之间的长连接和 RPC。
3. 客户端 WebSocket：移动端实时事件和终端 relay。
4. SSE：只有确认当前移动端必须使用时才实现。

事件持久化：

- 每个 session 维护单调 sequence。
- `timeline.sync` 用于完整替换/重连校准。
- `timeline.item.upsert` 用于增量。
- `session.meta.upsert`、`session.state.update`、`notice.upsert`、能力更新按现有 notification 名称保持。
- Server 先持久化，再广播；广播失败不丢历史。
- 客户端断线后通过 cursor 请求恢复，无法恢复时返回完整 snapshot。

## 10. 终端 relay

必须覆盖：

- 创建终端。
- attach 既有终端。
- PTY input。
- resize。
- output sequence。
- snapshot/replay。
- close。
- Connector 断线重连时不自动销毁本地 PTY。

推荐路径保持现有形式：

```text
WS /api/v2/connector/terminals/{terminalId}/relay
```

Server 只持有终端 relay broker；PTY 始终由本地 Go Connector 创建和拥有。终端写操作不自动重试。

## 11. 安全要求

即使使用固定密钥，也必须做最小安全边界：

- 只允许 HTTPS/WSS。
- 密钥从配置文件或环境变量加载，文件权限 0600。
- 日志禁止打印密钥、附件正文、原始 DSH 事件和完整用户请求。
- 所有文件路径必须基于 workspace root 解析，拒绝 `..` 穿越。
- 附件下载使用 fileId，不接受任意路径。
- 限制上传大小、单 session 附件数量和终端消息大小。
- WebSocket 设置读写 deadline 和最大 frame size。
- 终端和 Shell 能力必须通过 Connector 配置的 workspace allowlist 限制。
- 固定密钥泄露即意味着所有客户端和工作机失守；后续应增加 key rotation，但不影响第一版移动端协议。

## 12. 分阶段实施

### 12.0 本机局域网测试拓扑

开发阶段不部署公网云端环境，所有组件统一运行在当前 Mac 工作机：

```text
同一局域网手机
    │ 访问 Mac 的局域网 IP，例如 http://192.168.1.20:8080
    ▼
Mac 上的 Go Server（Beego，监听 0.0.0.0:8080）
    ▲
    │ WebSocket 回连
Mac 上的 Go Connector
    │ localhost / stdio
    ▼
Mac 上的官方 DSH CLI + dsh-bridge
```

测试环境要求：

- Go Server 必须监听 `0.0.0.0` 或 Mac 的局域网网卡地址，不能只监听 `127.0.0.1`。
- 手机和 Mac 必须连接同一个局域网，并确认 Mac 防火墙允许测试端口入站。
- 手机使用 Mac 局域网 IP 访问 Server；不能使用手机上的 `localhost`，因为那指向手机自身。
- WebSocket 地址必须使用与 HTTP 相同的局域网 host；如果使用 HTTPS，则测试环境需要局域网可信任证书，否则优先使用受控局域网 HTTP 验证协议功能。
- Connector 和 DSH CLI 可以只绑定本机回环地址，不需要向局域网开放。
- 测试数据、会话文件、附件和日志全部落在 Mac 本地测试目录，测试结束可以整体清理。
- 验收必须包含手机真实访问，不以 Mac 浏览器访问 `localhost` 代替移动端验证。

建议配置：

```yaml
listen:
  host: 0.0.0.0
  port: 8080
advertise_url: http://192.168.1.20:8080
storage_root: ./var/test-data
client_key: local-test-key
```

`advertise_url` 使用 Mac 当前局域网 IP；若 DHCP 导致地址变化，应在每次测试前更新，或给 Mac 配置局域网固定租约。

### Phase 0：协议抓取与 DSH CLI PoC

产出：

- 固定官方 DSH 版本。
- 现有移动端 HTTP/WS 请求样本。
- 当前 Server API 和 DSH Bridge schema 的 Go 类型。
- CLI 下 dsh-bridge 能力矩阵。

验收：不写业务代码即可证明移动端协议和官方 DSH runtime 两端可连接。

### Phase 1：Go Server 最小闭环

实现：

- Beego。
- Go Server 最小闭环。
- 在 Mac 上以 `0.0.0.0` 启动 Go Server。
- 手机通过 Mac 局域网 IP 完成健康检查、会话读取和一条文本消息发送。
- Mac 浏览器本地访问和手机局域网访问都必须通过，后者是必选验收项。

验收：手机能通过局域网地址看到工作机的 DSH 历史并发送一条文本消息。

### Phase 2：Go Connector

实现：

- Server 长连接和重连。
- RPC dispatcher。
- dsh-bridge endpoint client。
- session inventory/snapshot。
- 实时 notification。
- 附件拉取和本地 staging。
- 基础文件和 Shell。

验收：云端 Server 重启、本地 Connector 重启、DSH runtime 重启后均能恢复。

### Phase 3：交互和附件

实现：

- ask_user_question。
- approval/interaction response。
- 图片和普通文件。
- 模型、effort、权限、preset。
- message id 幂等。

验收：手机发送附件到 DSH，DSH 产生问答请求，手机可以响应并继续执行。

### Phase 4：终端 relay

实现：

- PTY 生命周期。
- output sequence。
- replay/snapshot。
- resize/input/close。
- relay 断线恢复。

验收：移动端可打开终端、输入命令、断线重连且不丢失未过期输出。

## 13. 主要风险和决策

| 风险 | 影响 | 对策 |
|---|---|---|
| 官方 DSH developer preview 频繁破坏 API | dsh-bridge 失效 | 固定版本，维护 CLI 兼容矩阵 |
| 移动端实际协议比公开 schema 更复杂 | Go Server 无法兼容 | 先抓包/录制请求，建立 golden tests |
| 文件存储并发覆盖 | 历史丢失或 Timeline 损坏 | JSONL append + session mutex + 原子 meta 写入 |
| 单固定密钥泄露 | 全部权限泄露 | HTTPS、密钥文件权限、后续轮换 |
| Connector 断线时重复写请求 | 重复发消息/重复执行 | 写请求不自动重试，依赖 stable clientMessageId |
| SSE/WS 路径或字段差异 | 移动端实时状态异常 | 保持 `/api/v2` 路径和现有事件 envelope |
| 直接解析 DSH 日志 | 业务重复、升级困难 | 只通过 dsh-bridge 获取标准化数据 |

## 14. 当前结论

该架构可行，但不是简单把 Python Server 翻译成 Go。真正的兼容核心有三层：

1. 移动端到 Go Server 的现有 `/api/v2` 协议和实时事件格式。
2. Go Server 到 Go Connector 的 RPC/notification 语义。
3. Go Connector 到官方 DSH CLI 中 `dsh-bridge` 的 JSON-RPC 协议。

服务端可以去掉用户、OAuth、PostgreSQL、Redis 和复杂设备体系；不能去掉会话事件顺序、Timeline 游标、附件元数据、交互请求、终端 relay 和 Connector 长连接。

最先执行的工作不是开发 Beego handler，而是 Phase 0：固定官方 DSH CLI 版本、验证现有 dsh-bridge 在官方 CLI/profile/headless/sdk 下的可用性，并录制当前移动端协议样本。只有这两项通过，Go Server/Connector 的接口设计才不会建立在错误假设上。

## 15. 参考资料

- 当前仓库 Server API namespace：`docs/api/namespace.md`
- 当前仓库 DSH Bridge schema：`contracts/dsh-bridge/1.0/`
- 当前仓库 DSH Bridge 分工：`dsh-bridge-next/DEVELOPMENT_PLAN.md`
- 官方 DSH GitHub：https://github.com/deepseek-ai/deepseek-harness
- 官方 DSH CLI 文档：https://github.com/deepseek-ai/deepseek-harness/tree/master/apps/cli
- 官方 DSH 架构文档：https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md
- 官方 DSH 快速开始：https://deepseek-harness.github.io/deepseek-harness/en/guide/quickstart
