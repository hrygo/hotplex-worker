<h1 align="center">Hotplex</h1>

<p align="center">
<strong>Unified Access Gateway for AI Coding Agents</strong>
</p>

<p align="center">
<a href="README_zh.md">简体中文</a> | <strong>English</strong>
</p>

<p align="center">
<img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go" alt="Go">
<img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=for-the-badge" alt="AEP v1">
<img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=for-the-badge" alt="License">
<img src="https://img.shields.io/badge/Platform-Slack%20%7C%20Feishu-E11D48?style=for-the-badge" alt="Platforms">
</p>

---

Hotplex is a single-process Go gateway that gives you **one WebSocket interface** to any AI Coding Agent. It manages sessions, adapts protocols, and connects your users through web, Slack, or Feishu — all from one binary.

**One gateway. Any agent. Every channel.**

```
   Web Chat ──┐
              │     ┌──────────────────────┐     ┌──────────────┐
  Slack Bot ──┼────▶│    Hotplex Gateway   │────▶│ Claude Code  │
              │     │    (AEP v1 / WS)     │     │ OpenCode Srv │
 Feishu Bot ──┘     └──────────────────────┘     └──────────────┘
                          Go 1.26+ · SQLite        NDJSON/stdio
```

## Features

- **AEP v1 Protocol** — 23+ event types over WebSocket: streaming, permissions, Q&A, MCP Elicitation, user interactions
- **Session State Machine** — 5-state lifecycle (Created → Running → Idle → Terminated → Deleted) with crash recovery and automatic reconnection
- **Worker Adapters** — Plugin-based: Claude Code, OpenCode Server, ACPX, Pi-mono. Add your own with the BaseWorker embedding pattern
- **Multi-Channel** — Bidirectional bridge to Slack (Socket Mode) and Feishu (WebSocket Events) with user interaction support
- **Web Chat UI** — Next.js 15 + React 19 + Vercel AI SDK, ready out of the box
- **Multi-Language SDKs** — Go, TypeScript, Python, Java
- **Enterprise Ready** — JWT ES256 auth, Admin API, Prometheus metrics, OpenTelemetry tracing, hot-reload config, speech-to-text

## Quick Start

### Install

```bash
git clone https://github.com/hotplex/hotplex-worker.git
cd hotplex-worker
cp configs/env.example .env
make quickstart
```

### Run

```bash
make dev
```

This starts the gateway at `http://localhost:8888` and web chat at `http://localhost:3000`.

### Generate a Token

```bash
go run client/scripts/gen-token/main.go -secret "$(grep HOTPLEX_JWT_SECRET .env | cut -d= -f2)"
```

### Connect with Go SDK

```go
package main

import (
    "context"
    "fmt"
    "log"

    client "github.com/hotplex/hotplex-go-client"
)

func main() {
    c, err := client.Connect(context.Background(), "ws://localhost:8888/ws",
        client.WithToken("<your-jwt-token>"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    // Send a message
    c.SendInput(context.Background(), "Explain the AEP v1 protocol")

    // Handle streaming responses
    go func() {
        for env := range c.Events() {
            switch env.Event.Type {
            case "message.delta":
                delta := env.Event.Data.(map[string]any)
                fmt.Print(delta["content"].(string))
            case "done":
                fmt.Println("\n--- done ---")
            }
        }
    }()

    select {} // block
}
```

## Usage

### Connect from TypeScript

```typescript
import { HotplexClient } from "@hotplex/client";

const client = new HotplexClient("ws://localhost:8888/ws", {
  token: "<your-jwt-token>",
});

client.on("message.delta", (data) => process.stdout.write(data.content));
client.on("done", () => console.log("\n--- done ---"));

await client.connect();
await client.sendInput("Refactor the auth module");
```

### Connect from Python

```python
import asyncio
from hotplex import Client

async def main():
    client = Client("ws://localhost:8888/ws", token="<your-jwt-token>")
    await client.connect()
    await client.send_input("Write unit tests for the session manager")

    async for event in client.events():
        if event.type == "message.delta":
            print(event.data["content"], end="")
        elif event.type == "done":
            print("\n--- done ---")
            break

asyncio.run(main())
```

### Admin API

```bash
# Health check
curl -H "Authorization: Bearer <admin-token>" http://localhost:9999/api/v1/health

# List sessions
curl -H "Authorization: Bearer <admin-token>" http://localhost:9999/api/v1/sessions
```

## Architecture

```
┌─────────────┐  ┌──────────────┐  ┌──────────────────┐
│  Web Chat   │  │  Slack Bot   │  │  Feishu Bot      │
│  (Next.js)  │  │  (SocketMode)│  │  (WebSocket)     │
└──────┬──────┘  └──────┬───────┘  └────────┬─────────┘
       │                │                    │
       ▼                ▼                    ▼
┌──────────────────────────────────────────────────────┐
│                  Hotplex Gateway                      │
│  ┌──────────┐ ┌──────────┐ ┌───────────────────────┐│
│  │ WS Hub   │ │ Session  │ │ Messaging Bridge      ││
│  │ (AEP v1) │ │ Manager  │ │ (3-step flow)         ││
│  └────┬─────┘ └────┬─────┘ └───────────┬───────────┘│
│       │             │                   │            │
│  ┌────▼─────────────▼───────────────────▼───────────┐│
│  │              Worker Adapters                      ││
│  │  ┌──────────────┐  ┌─────────────────────────┐   ││
│  │  │ Claude Code  │  │ OpenCode Server         │   ││
│  │  │ (NDJSON/stdio)│  │ (NDJSON/stdio)         │   ││
│  │  ├──────────────┤  ├─────────────────────────┤   ││
│  │  │ ACPX         │  │ Pi-mono                 │   ││
│  │  │ (ACP/stdio)  │  │ (raw stdout)            │   ││
│  │  └──────────────┘  └─────────────────────────┘   ││
│  └──────────────────────────────────────────────────┘│
│  ┌──────────┐ ┌──────────┐ ┌──────┐ ┌─────────────┐ │
│  │ Admin API│ │ SQLite   │ │ Auth │ │ Observability│ │
│  │ (:9999)  │ │ (WAL)    │ │ JWT  │ │ OTel/Prom   │ │
│  └──────────┘ └──────────┘ └──────┘ └─────────────┘ │
└──────────────────────────────────────────────────────┘
```

## Extend

### Add a Worker Adapter

```go
// internal/worker/<name>/worker.go
type workerAdapter struct {
    *base.BaseWorker  // shared lifecycle: Terminate, Kill, Wait, Health, LastIO
}

func (w *workerAdapter) Start(ctx context.Context, env []string) error {
    // 1. exec.Command with PGID isolation
    // 2. Setup NDJSON stdio channel
    // 3. Start read/write pumps
    return nil
}

func (w *workerAdapter) Input(ctx context.Context, content string) error {
    return w.Conn.Send(events.Envelope{...})
}

func init() { worker.Register(worker.TypeMyWorker, New) }
```

### Add a Messaging Platform Adapter

```go
// internal/messaging/<name>/adapter.go
type Adapter struct {
    *platformadapter.PlatformAdapter  // SetHub, SetSM, SetHandler, SetBridge
}

func (a *Adapter) Start(ctx context.Context) error { ... }
func (a *Adapter) HandleTextMessage(ctx context.Context, msg *Message) error { ... }
```

## Configuration

| Config Path | Default | Description |
|-------------|---------|-------------|
| `gateway.addr` | `:8888` | WebSocket gateway address |
| `admin.addr` | `:9999` | Admin API address |
| `session.retention_period` | `168h` | Session retention (7 days) |
| `session.max_concurrent` | `1000` | Max concurrent sessions |
| `pool.max_size` | `100` | Session pool size |
| `worker.idle_timeout` | `60m` | Worker idle timeout |
| `worker.execution_timeout` | `10m` | Single execution timeout |
| `worker.max_lifetime` | `24h` | Max worker process lifetime |
| `db.path` | `data/hotplex-worker.db` | SQLite database path |
| `log.level` | `info` | Log level (debug/info/warn/error) |
| `log.format` | `json` | Log format (text for dev, json for prod) |
| `messaging.*.require_mention` | `true` | In channels/groups, bot must be @mentioned |

Full reference: [docs/management/Config-Reference.md](docs/management/Config-Reference.md)

### Environment Variables

```bash
# Required
HOTPLEX_JWT_SECRET=<ES256 public key or HS256 secret>
HOTPLEX_ADMIN_TOKEN_1=<admin API access token>

# Slack integration
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-...
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true  # Default: true
HOTPLEX_MESSAGING_SLACK_DM_POLICY=allowlist   # open | allowlist | disabled (default: allowlist)
HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=U12345
HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=U67890
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=U_ADMIN

# Feishu integration
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_...
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=...
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true # Default: true
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=allowlist  # open | allowlist | disabled (default: allowlist)
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=allowlist # open | allowlist | disabled (default: allowlist)
HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM=ou_dm_xxx
HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM=ou_group_yyy
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=ou_admin_zzz
```

## Deploy

### Docker

```bash
docker build -t hotplex-worker .

# Development
docker compose up -d

# Production (Traefik + TLS + Let's Encrypt)
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

### Systemd

```bash
sudo bash scripts/install.sh
```

Configures systemd service, TLS, and key rotation automatically.

## Client SDKs

| Language | Location | Features |
|----------|----------|----------|
| **Go** | `client/` | Full AEP v1, auto-reconnect, event-driven |
| **TypeScript** | `examples/typescript-client/` | Streaming, multi-turn, permission handling |
| **Python** | `examples/python-client/` | asyncio, streaming, session resume |
| **Java** | `examples/java-client/` | Full AEP v1 implementation |

Go SDK examples (`client/examples/`): [Quickstart](client/examples/01_quickstart) · [Streaming](client/examples/02_streaming_output) · [Multi-turn](client/examples/03_multi_turn_chat) · [Resume](client/examples/04_session_resume) · [Permissions](client/examples/05_permission_handling) · [Production](client/examples/09_production)

## Development

```bash
make build           # Build binary (bin/hotplex-worker-<os>-<arch>)
make test            # Tests with -race (timeout 15m)
make test-short      # Quick test pass
make lint            # golangci-lint
make fmt             # gofmt + goimports
make quality         # fmt + vet + lint + test
make check           # Full CI pipeline
make coverage        # Coverage report
```

## Documentation

| Document | Description |
|----------|-------------|
| [Product Whitepaper](docs/Product-Whitepaper.md) | Product overview, core values, architecture |
| [User Manual](docs/User-Manual.md) | Installation, config, CLI, protocol |
| [Architecture](docs/architecture/Worker-Gateway-Design.md) | Gateway internal design |
| [AEP v1 Protocol](docs/architecture/AEP-v1-Protocol.md) | Protocol specification |
| [Platform Messaging](docs/architecture/Platform-Messaging-Architecture-Diagrams.md) | Slack/Feishu integration |
| [Admin API](docs/management/Admin-API-Design.md) | Management API reference |
| [Security](docs/security/Security-Authentication.md) | Auth, input validation, SSRF protection |
| [Testing](docs/testing/Testing-Strategy.md) | Testing strategy and guidelines |

## Project Structure

```
cmd/worker/main.go         # Entry point: DI, signal handling
internal/
  admin/                    # Admin API: CRUD, metrics, health
  aep/                      # AEP v1 codec
  config/                   # Viper + file watcher + hot-reload
  gateway/                  # WebSocket: Hub, Conn, Handler, Bridge
  messaging/
    slack/                  # Slack Socket Mode adapter
    feishu/                 # Feishu WebSocket adapter (with STT)
  metrics/                  # Prometheus counters/gauges/histograms
  security/                 # JWT, SSRF, command whitelist, env isolation
  session/                  # State machine + SQLite persistence
  tracing/                  # OpenTelemetry setup
  worker/
    base/                   # BaseWorker shared lifecycle
    claudecode/             # Claude Code adapter
    opencodeserver/         # OpenCode Server adapter
pkg/
  events/                   # AEP event types and data structures
  aep/                      # AEP v1 codec utilities
client/                     # Go client SDK (standalone module)
webchat/                    # Web Chat UI (Next.js 15 + React 19)
examples/                   # TypeScript, Python, Java SDK examples
scripts/                    # Build, deploy, validation scripts
configs/                    # config.yaml, config-dev.yaml, env.example
docs/                       # Architecture, management, security, testing
```

## Contributing

Contributions are welcome! Please read the [contributing guidelines](CONTRIBUTING.md) before submitting.

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit your changes (`git commit -m 'feat: add my feature'`)
4. Push to the branch (`git push origin feat/my-feature`)
5. Open a Pull Request

Make sure `make check` passes before submitting.

## Security

If you discover a security vulnerability, **please do not open a public issue**. Report it privately to the maintainers so it can be addressed responsibly.

## License

[Apache License 2.0](LICENSE)
