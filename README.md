<h1 align="center">HotPlex Gateway</h1>

<p align="center">
  <strong>The Unified Bridge for AI Coding Agents</strong>
</p>

<p align="center">
  A high-performance Go gateway providing a single WebSocket interface<br>
  to access any AI Coding Agent across Web, Slack, and Feishu.
</p>

<p align="center">
  <a href="README_zh.md">简体中文</a> | <strong>English</strong>
</p>

<p align="center">
  <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://github.com/hrygo/hotplex/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/Version-v1.3.0-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---

```
┌──────────┐   ┌──────────┐   ┌──────────┐
│  Web UI  │   │  Slack   │   │  Feishu  │
└────┬─────┘   └────┬─────┘   └────┬─────┘
     │              │              │
     └──────────────┼──────────────┘
                    │  WebSocket / AEP v1
              ┌─────┴─────┐
              │  HotPlex  │  Session · Auth · Retry · B/C Config
              │  Gateway  │  Metrics · Tracing · Admin · Meta-Core
              └─────┬─────┘
     ┌──────────────┼──────────────┐
     │              │              │
┌────┴─────┐  ┌────┴──────┐  ┌───┴───────┐
│  Claude  │  │  OpenCode │  │  Pi-mono  │
│  Code    │  │  Server   │  │           │
└──────────┘  └───────────┘  └───────────┘
```

HotPlex sits between frontend clients and backend AI coding agents, abstracting protocol differences into a unified **AEP v1** WebSocket layer — with a built-in Meta-Cognition Core for session stability.

## Core Capabilities

- **Universal Agent Gateway** — Unified AEP v1 WebSocket interface for Claude Code, OpenCode Server, and Pi-mono
- **Cross-Platform Delivery** — Bridge agents to Web, Slack (Socket Mode), and Feishu (WebSocket) with zero code changes
- **Embedded Web Chat** — Single binary serves both the gateway and a Next.js-based web chat UI
- **Deep Personality Injection** — B/C dual-channel agent config: directives (SOUL/AGENTS/SKILLS) + context (USER/MEMORY)
- **Enterprise Security** — JWT ES256, SSRF protection, PGID-isolated process management, path traversal prevention
- **Full Observability** — Prometheus metrics, OpenTelemetry tracing, structured JSON logging

## Quick Start

> **AI Agents:** See [INSTALL.md](INSTALL.md) for machine-readable installation instructions.

### 1. Install

**Binary (macOS / Linux):**

```bash
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest
```

**Binary (Windows):**

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

**From Source:**

```bash
git clone https://github.com/hrygo/hotplex.git && cd hotplex
make quickstart    # check tools + build + test
```

**Docker (Experimental):**

```bash
cp configs/env.example .env  # edit with your API keys
docker compose up -d
```

### 2. Configure

```bash
# Interactive setup wizard
hotplex onboard

# Or auto-generate all configs:
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

> **Tip (Claude Code users):** Use the `/setup-env` skill for interactive `.env` configuration — Slack/Feishu tokens, access policies, STT, worker settings, and more.

### 3. Run

```bash
make dev                   # Development (foreground, gateway + webchat)
hotplex gateway start -d   # Production (background daemon)
```

| Service             | Address                  | Note                                   |
| :------------------ | :----------------------- | :------------------------------------- |
| Gateway (WebSocket) | `ws://localhost:8888/ws` | Main protocol endpoint                 |
| Admin API           | `http://localhost:9999`  | Management & Statistics                |
| Web Chat UI         | `http://localhost:8888`  | Embedded SPA (served from Gateway)     |
| Dev Web Chat        | `http://localhost:3000`  | Next.js Dev Server (`make dev` only)   |

## System Service

Install as a native system service — no root required for user-level setup.

```bash
hotplex service install              # User-level (no root)
sudo hotplex service install --level system  # System-wide
```

```bash
hotplex service start       # Start
hotplex service stop        # Stop
hotplex service restart     # Restart
hotplex service status      # Status
hotplex service logs -f     # Follow logs
hotplex service uninstall   # Uninstall
```

Supports **systemd** (Linux), **launchd** (macOS), and **Windows SCM**.

## SDKs

| Language       | Path                                                         | Features                          |
| :------------- | :----------------------------------------------------------- | :-------------------------------- |
| **Go**         | [`client/`](client/)                                         | Channel-based events, production  |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | Streaming, multi-turn, React      |
| **Python**     | [`examples/python-client/`](examples/python-client/)         | Asyncio, session resume           |
| **Java**       | [`examples/java-client/`](examples/java-client/)             | Enterprise AEP v1                 |

Quick example with Go SDK:

```go
package main

import (
    "context"
    "fmt"
    client "github.com/hrygo/hotplex/client"
)

func main() {
    c, err := client.New(context.Background(),
        client.URL("ws://localhost:8888/ws"),
        client.WorkerType("claude_code"),
        client.APIKey("<your-api-key>"),
    )
    if err != nil {
        panic(err)
    }
    defer c.Close()

    c.SendInput(context.Background(), "Explain HotPlex architecture")

    for env := range c.Events() {
        if data, ok := env.AsMessageDeltaData(); ok {
            fmt.Print(data.Content)
        }
    }
}
```

## Configuration

| Key                        | Default                      | Description                                    |
| :------------------------- | :--------------------------- | :--------------------------------------------- |
| `gateway.addr`             | `localhost:8888`             | WebSocket gateway address                      |
| `admin.addr`               | `localhost:9999`             | Admin API address                              |
| `agent_config.enabled`     | `true`                       | Agent personality/context injection            |
| `worker.auto_retry.enabled`| `true`                       | LLM retry with exponential backoff             |
| `webchat.enabled`          | `true`                       | Embedded webchat SPA                           |
| `db.path`                  | `~/.hotplex/data/hotplex.db` | SQLite database path                           |
| `log.level`                | `info`                       | Log level: debug, info, warn, error            |

> [!TIP]
> See [Config Reference](docs/management/Config-Reference.md) for the full list of environment variables and YAML settings.

## Documentation

| Area                | Guide                                                                                                                        |
| :------------------ | :--------------------------------------------------------------------------------------------------------------------------- |
| **Getting Started** | [Quick Start](docs/User-Manual.md) · [Reference Manual](docs/Reference-Manual.md) · [Whitepaper](docs/Product-Whitepaper.md) |
| **Protocol**        | [AEP v1 Specification](docs/architecture/AEP-v1-Protocol.md)                                                                 |
| **Architecture**    | [Gateway Design](docs/architecture/Worker-Gateway-Design.md) · [Agent Config](docs/architecture/Agent-Config-Design.md) · [Meta-Cognition](internal/agentconfig/META-COGNITION.md) |
| **Security**        | [Authentication](docs/security/Security-Authentication.md) · [SSRF Protection](docs/security/SSRF-Protection.md)             |
| **Operations**      | [Admin API](docs/management/Admin-API-Design.md) · [Observability](docs/management/Observability-Design.md) · [Testing](docs/testing/Testing-Strategy.md) |

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create your feature branch (`git checkout -b feat/AmazingFeature`)
3. Commit with conventional messages (`git commit -m 'feat: add AmazingFeature'`)
4. Push to the branch (`git push origin feat/AmazingFeature`)
5. Open a Pull Request

> [!NOTE]
> All build/test/lint operations must use `make` targets. See `make help` for the full list.

## Security

If you discover a security vulnerability, please do NOT open a public issue. Report it via [SECURITY.md](SECURITY.md) or contact maintainers directly.

## License

Distributed under the [Apache License 2.0](LICENSE).
