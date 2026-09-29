# Agents Anywhere — Go backend for self-hosted agent management

This repository is a Go implementation of the **server side** of Agents Anywhere.
It lets you manage coding agents that run on **your own machine** from your phone,
without depending on a hosted service.

The mobile app is the **official Agents Anywhere client, used as-is**. Nothing in
this repository requires a forked or customised app, so there is no separate
mobile client to build, sign or maintain. Point the official app at your own
server address and it works.

## Architecture

```
Mobile App (official Agents Anywhere client, unmodified)
        │  HTTPS + WebSocket
        ▼
aa-server            Go service, runs on a cloud host
        │  WebSocket JSON-RPC (connector initiates the connection)
        ▼
dsh-connector        Go agent, runs on your workstation
        │  localhost JSON-RPC
        ▼
DSH bridge  →  DSH Host (DeepSeek Harness)
```

The connector dials **out** to aa-server, so the workstation never needs an
inbound port. Only aa-server has to be reachable from the phone.

## Components

| Path | Role |
| --- | --- |
| `aa-server/` | Cloud gateway. Serves the mobile client API (OAuth-compatible login, connectors, projects, sessions, timeline, runtime controls, terminal relay, attachments) and routes JSON-RPC requests to a connected connector. |
| `dsh-connector/` | Workstation agent. Keeps the connection to aa-server, connects to the local DSH bridge, forwards runtime calls and streams conversation events back. |
| `build.sh` | Builds deployable artifacts for both modules into `build/` (cloud package for aa-server, macOS package for the connector). |
| `REQUIREMENTS_ANALYSIS.md` | Design notes, protocol constraints and the file-persistence specification this port follows. |

## Build

```bash
./build.sh              # both modules
./build.sh aa-server    # cloud package only
./build.sh connector    # macOS package only
```

Artifacts land in `build/` and are intentionally **not** committed:

```
build/aa-server/    aa-server (linux binary) + config.yaml
build/connector/    dsh-connector (darwin binary) + config.yaml + start-mac.command
```

Target platforms can be overridden with `AA_GOOS`/`AA_GOARCH` (default
`linux/amd64`) and `CONN_GOOS`/`CONN_GOARCH` (default `darwin` on the build host).

## Deploy

### 1. aa-server (cloud host)

```yaml
host: 0.0.0.0
port: 8080
storage_root: /var/lib/aa-server/data   # must be writable
client_key: <random secret>             # shared by the app and the connector
advertise_url: https://your-domain
```

Put it behind TLS. If you use nginx, the proxy **must** forward WebSocket
upgrades, otherwise connectors and the app's realtime channel fail to connect:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;   # map $http_upgrade, 'close'
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
}
```

### 2. dsh-connector (your workstation)

```yaml
server_url: https://your-domain
connector_id: workstation-001
client_key: <same value as aa-server>
bridge_endpoint: <DSH_HOME>/agents-anywhere/bridge/endpoint.json
workspace_roots:
  - /path/to/your/workspace
```

On macOS, the packaged `start-mac.command` starts DSH and the connector together;
set your DeepSeek API key in that file, then double-click it.

### 3. Mobile app

Install the official Agents Anywhere client, enter your server address
(`https://your-domain`), and sign in with the `client_key` when the login page
asks for it.

## Authentication

`client_key` is the single shared credential:

- The app signs in through an OAuth-compatible flow: the web session opened by
  the app shows a prompt for the access key; a correct key is exchanged for a
  bearer token. The phone therefore needs the URL **and** the key.
- The connector authenticates its WebSocket handshake with the same key
  (`X-DSH-Key`).
- Rotating `client_key` in the server config immediately locks out anyone who
  does not know the new value; already signed-in devices must sign in again.

Because the key is issued through the login page rather than stored in the URL,
keep `client_key` long and random, and expose aa-server only over TLS.

## Persistence

Conversation content is owned by DSH: the DSH session log is the source of
truth, and the bridge can re-project it at any time. aa-server keeps a local
mirror so the server keeps working when it already knows about a session:

```
<storage_root>/sessions/<sessionId>/meta.json     session metadata
<storage_root>/sessions/<sessionId>/timeline.jsonl append-only event/notification records
<storage_root>/projects/<projectId>.json          user-created projects
<storage_root>/attachments/<fileId>               uploaded files
```

The session index is rebuilt by scanning these directories on startup.

## Status and known gaps

The following are known limitations of the current implementation:

- Session listing, session snapshots and timeline reads call the connector
  first. When the connector is offline or DSH no longer sees a session, the
  locally mirrored history is not yet served as a fallback.
- `GET /sessions/{id}/timeline` returns stored connector records rather than
  standard timeline items, and ignores the client's paging parameters
  (`mode`, `afterSeq`, `beforeOrderSeq`, `limit`). Snapshots report
  `hasMore: false`, so only the most recent window is reachable from the app.
- The storage layout does not yet implement every item of
  `REQUIREMENTS_ANALYSIS.md` §4.2 (`state.json`, per-session locks, `fsync`
  before rename, attachment metadata, `logs/`, `server.lock`).
