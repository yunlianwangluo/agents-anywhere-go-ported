#!/usr/bin/env bash
#
# 一键构建交付包：aa-server（云端）与 connector（Mac）。
# 产物输出到 <dsh-go-ported>/build 下，按模块分目录：
#
#   build/aa-server/  aa-server(linux) + config.yaml
#   build/connector/  dsh-connector(darwin) + config.yaml + start-mac.command
#
# 用法：
#   ./build.sh                 构建全部（aa-server=linux/amd64，connector=本机）
#   ./build.sh mac             构建本机可执行的 aa-server + connector，连同本地运行配置
#   ./build.sh aa-server       只构建 aa-server
#   ./build.sh connector       只构建 connector
#
# 已存在的 config.yaml / start-mac.command 不会被覆盖（避免冲掉手填的
# server_url、client_key、API Key）；想重新生成模板就先删掉这几个文件。
#
# 可用环境变量覆盖目标平台：
#   AA_GOOS/AA_GOARCH               默认 linux/amd64（mac 模式强制为本机平台）
#   CONN_GOOS/CONN_GOARCH           默认 darwin/本机架构
#   WORKSPACE_DIR                   默认 dsh-go-ported 的上级目录（仓库根）
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BUILD="$ROOT/build"
AA_SRC="$ROOT/aa-server"
CONN_SRC="$ROOT/dsh-connector"
WORKSPACE_DIR="${WORKSPACE_DIR:-$(cd "$ROOT/.." && pwd)}"

command -v go >/dev/null 2>&1 || {
  printf '\033[1;31m错误:\033[0m %s\n' "未找到 go，请先安装 Go 工具链" >&2
  exit 1
}
HOST_GOOS="$(go env GOHOSTOS)"
HOST_GOARCH="$(go env GOHOSTARCH)"

AA_GOOS="${AA_GOOS:-linux}"
AA_GOARCH="${AA_GOARCH:-amd64}"

case "$(uname -m)" in
  arm64 | aarch64) DEFAULT_ARCH=arm64 ;;
  *) DEFAULT_ARCH=amd64 ;;
esac
CONN_GOOS="${CONN_GOOS:-darwin}"
CONN_GOARCH="${CONN_GOARCH:-$DEFAULT_ARCH}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() {
  printf '\033[1;31m错误:\033[0m %s\n' "$*" >&2
  exit 1
}

# ---------------------------------------------------------------- aa-server
build_aa_server() {
  local out="$BUILD/aa-server"
  log "构建 aa-server ($AA_GOOS/$AA_GOARCH) -> build/aa-server/"
  rm -rf "$out"
  mkdir -p "$out"

  (
    cd "$AA_SRC"
    CGO_ENABLED=0 GOOS="$AA_GOOS" GOARCH="$AA_GOARCH" \
      go build -trimpath -ldflags "-s -w" -o "$out/aa-server" ./cmd/aa-server
  )

  cat >"$out/config.yaml" <<'YAML'
# aa-server 部署配置（云端）。改完后启动：./aa-server -config config.yaml
host: 0.0.0.0
port: 8080

# 会话/项目/附件等数据的本地目录，需可写
storage_root: /var/lib/aa-server/data

# 手机 App 与 connector 共用的密钥，必须与 connector 的 client_key 相同
client_key: CHANGE_ME

# 手机 App 里填写的服务器地址（用于 OAuth 回跳校验）
advertise_url: http://CHANGE_ME:8080

# 可选：文件浏览器在 root 为空时的默认根目录
# workspace_roots:
#   - /srv/workspace
YAML
}

# 本机直跑用的 aa-server 配置：数据放在 build 之外的 var/ 下，监听 127.0.0.1。
# 放在 build 内会被每次构建的 rm -rf build/aa-server 一起删掉。
write_local_aa_config() {
  cat >"$BUILD/aa-server/config.yaml" <<YAML
# aa-server 本地运行配置。启动：./aa-server -config config.yaml
host: 127.0.0.1
port: 8080

# 会话/项目/附件等数据的本地目录（刻意放在 build 之外，重新构建不会丢）
storage_root: $ROOT/var/aa-server-data

# 手机 App 与 connector 共用的密钥，必须与 connector 的 client_key 相同
client_key: CHANGE_ME

# 手机 App 里填写的服务器地址
advertise_url: http://127.0.0.1:8080
YAML
}

# ----------------------------------------------------------------- connector
build_connector() {
  local out="$BUILD/connector"
  # 手改过的部署文件先取出来，构建完再放回去：只有二进制是每次重出的产物。
  local keep_config="" keep_launcher=""
  if [ -f "$out/config.yaml" ]; then
    keep_config="$(mktemp)"
    cp "$out/config.yaml" "$keep_config"
  fi
  if [ -f "$out/start-mac.command" ]; then
    keep_launcher="$(mktemp)"
    cp "$out/start-mac.command" "$keep_launcher"
  fi

  log "构建 dsh-connector ($CONN_GOOS/$CONN_GOARCH) -> build/connector/"
  rm -rf "$out"
  mkdir -p "$out"

  (
    cd "$CONN_SRC"
    CGO_ENABLED=0 GOOS="$CONN_GOOS" GOARCH="$CONN_GOARCH" \
      go build -trimpath -ldflags "-s -w" -o "$out/dsh-connector" ./cmd/dsh-connector
  )

  if [ -n "$keep_config" ]; then
    cp "$keep_config" "$out/config.yaml"
    rm -f "$keep_config"
    log "保留已有 config.yaml（未覆盖手填的 server_url / client_key）"
  else
    cat >"$out/config.yaml" <<YAML
# connector 配置（这台 Mac）。
# server_url / client_key 必须与云端 aa-server 保持一致。
server_url: http://CHANGE_ME:8080
connector_id: workstation-001
client_key: CHANGE_ME

# DSH bridge endpoint，由 dsh 启动后生成，一般无需修改
bridge_endpoint: $WORKSPACE_DIR/.runtime/dsh-home/agents-anywhere/bridge/endpoint.json

# 允许手机端浏览/操作的目录
workspace_roots:
  - $WORKSPACE_DIR
YAML
  fi

  if [ -n "$keep_launcher" ]; then
    cp "$keep_launcher" "$out/start-mac.command"
    rm -f "$keep_launcher"
    log "保留已有 start-mac.command（未覆盖手填的 API Key）"
  else
    write_mac_launcher "$out"
  fi

  chmod +x "$out/dsh-connector" "$out/start-mac.command"
}

# 生成可双击启动的 .command（占位符替换后再落盘）
write_mac_launcher() {
  local out="$1"
  local launcher="$out/start-mac.command"
  local tmp="$launcher.tmp"

  cat >"$tmp" <<'LAUNCHER'
#!/bin/bash
# 双击本文件即可在这台 Mac 上启动 DSH 与 connector。
# 首次使用：先编辑同目录下的 config.yaml（server_url / client_key 要与云端 aa-server 一致）。

set -u

# ===== 需要修改的地方 =====
WORKSPACE_DIR="__WORKSPACE_DIR__"   # 仓库根目录（内含 .runtime）
DEEPSEEK_API_KEY="sk-apkey"         # 改成你自己的 DeepSeek API Key
# =========================

DIR="$(cd "$(dirname "$0")" && pwd)"

[ -f "$DIR/config.yaml" ] || { echo "缺少配置文件：$DIR/config.yaml"; exit 1; }
[ -x "$DIR/dsh-connector" ] || { echo "缺少可执行文件：$DIR/dsh-connector"; exit 1; }
[ -d "$WORKSPACE_DIR" ] || { echo "找不到仓库目录：$WORKSPACE_DIR"; exit 1; }

echo "启动 DSH（Web UI http://127.0.0.1:3080）…"
if lsof -nP -iTCP:3080 -sTCP:LISTEN >/dev/null 2>&1; then
  # 端口已被占用时不重复启动，否则 dsh 会以 EADDRINUSE 直接退出。
  echo "检测到 127.0.0.1:3080 已有 DSH 在运行，直接复用（不会结束它）。"
  DSH_PID=""
else
  cd "$WORKSPACE_DIR" && PATH="$PWD/.runtime/node24-x64/bin:$PWD/.runtime/dsh/node_modules/.bin:$PATH" DSH_HOME="$PWD/.runtime/dsh-home" DEEPSEEK_API_KEY="$DEEPSEEK_API_KEY" dsh --profile web --host 127.0.0.1 --port 3080 --no-open &
  DSH_PID=$!
  trap 'kill "$DSH_PID" 2>/dev/null' EXIT INT TERM
fi

# 等 bridge endpoint 生成，connector 首次连接更顺
ENDPOINT="$WORKSPACE_DIR/.runtime/dsh-home/agents-anywhere/bridge/endpoint.json"
for _ in $(seq 1 60); do
  [ -f "$ENDPOINT" ] && break
  sleep 1
done

echo "启动 connector（关闭本窗口即停止）…"
"$DIR/dsh-connector" -config "$DIR/config.yaml"
LAUNCHER

  sed "s|__WORKSPACE_DIR__|$WORKSPACE_DIR|g" "$tmp" >"$launcher"
  rm -f "$tmp"
}

# ---------------------------------------------------------------------- main
TARGET="${1:-all}"
case "$TARGET" in
  all)
    build_aa_server
    build_connector
    ;;
  mac)
    # 本机直跑：按当前 Mac 的 OS/架构构建，否则二进制会因平台不符而报
    # "exec format error"。已有 config.yaml / start-mac.command 会被保留。
    AA_GOOS="$HOST_GOOS"
    AA_GOARCH="$HOST_GOARCH"
    CONN_GOOS="$HOST_GOOS"
    CONN_GOARCH="$HOST_GOARCH"
    build_aa_server
    write_local_aa_config
    build_connector
    ;;
  aa-server) build_aa_server ;;
  connector) build_connector ;;
  *) die "未知模块：${TARGET}（可选：all / mac / aa-server / connector）" ;;
esac

log "完成，产物如下："
find "$BUILD" -maxdepth 2 -mindepth 1 | sort | sed "s|$ROOT/||"
echo
if [ "$TARGET" = "mac" ]; then
  echo "接下来（本机运行，${HOST_GOOS}/${HOST_GOARCH}）："
  echo "  1) cd build/aa-server && ./aa-server -config config.yaml"
  echo "     （空数据启动，监听 127.0.0.1:8080，数据在 var/aa-server-data）"
  echo "  2) 复用现有本地数据：./aa-server -config $ROOT/../.runtime/aa-server.yaml"
  echo "  3) cd build/connector && ./dsh-connector -config config.yaml（client_key 需与 aa-server 一致）"
else
  echo "接下来："
  echo "  1) 修改 build/aa-server/config.yaml（client_key / storage_root / advertise_url）后部署到云端"
  echo "  2) 修改 build/connector/config.yaml（server_url / client_key 与云端一致）"
  echo "  3) 修改 build/connector/start-mac.command 里的 DEEPSEEK_API_KEY，然后双击它启动 DSH + connector"
fi
