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
  <img src="https://img.shields.io/badge/Version-v1.2.0-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---


## ✨ Core Capabilities

- 🌐 **Universal Agent Gateway** — Abstract any AI Coding Agent protocol into a unified AEP v1 (Agent Exchange Protocol) WebSocket interface for consistent streaming and interaction.
- 📱 **Cross-Platform Delivery** — **"Write Once, Deploy Anywhere"**. Bridge agents to Web, Slack (Socket Mode), and Feishu (WebSocket) with zero code changes.
- 🛠️ **Multi-Modal Interaction** — Native Speech-to-Text (SenseVoice-Small) support for a seamless voice-to-code development workflow.
- 🤖 **Deep Personality Injection** — Dynamic **B/C dual-channel** injection: **B-channel** (SOUL/AGENTS/SKILLS) for directives and **C-channel** (USER/MEMORY) for context.
- 🧠 **Autonomous Meta-Cognition** — Built-in 5-state machine with **META-COGNITION** core, intelligent LLM retry, and 3-layer self-healing for unmatched session stability.
- 🌐 **Embedded Web Chat** — A single binary serves both the API/WebSocket gateway and a premium Next.js-based web chat interface out of the box.
- 🛡️ **Enterprise-Grade Security** — JWT ES256 authentication, SSRF protection, and PGID-isolated process management with orphan cleanup.
- 📊 **End-to-End Observability** — Native Prometheus metrics, OpenTelemetry tracing, and structured JSON logging for full auditability.
- 🛠️ **Self-contained CLI** — `gateway`, `service`, `dev`, `onboard`, `doctor`, `security`, `status` in a single binary
- 🌍 **Multi-language SDKs** — Go, TypeScript, Python, Java clients ready to use

## ⚡ Quick Start

### Install from Source

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # check tools + build + test
```

### Or with Docker

```bash
cp configs/env.example .env  # edit with your API keys
docker compose up -d
```

### Configure

```bash
# Interactive setup wizard
hotplex onboard

# Or auto-generate all configs:
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

### Run

```bash
# Development mode (foreground)
make dev

# Production mode (background daemon)
hotplex gateway start -d

# Stop / restart
hotplex gateway stop
hotplex gateway restart -d
```

### Install as System Service

```bash
# Install as user-level service (no root required)
hotplex service install

# Manage the service
hotplex service start      # Start service
hotplex service stop       # Stop service
hotplex service restart    # Restart service
hotplex service status     # Check status
hotplex service logs -f    # Follow logs

# System-wide installation (requires sudo)
sudo hotplex service install --level system

# Uninstall
hotplex service uninstall
```

Supports **systemd** (Linux), **launchd** (macOS), and **Windows SCM**.

| Service             | Address                  | Note                                     |
| :------------------ | :----------------------- | :--------------------------------------- |
| Gateway (WebSocket) | `ws://localhost:8888/ws` | Main protocol endpoint                   |
| Admin API           | `http://localhost:9999`  | Management & Statistics                  |
| Web Chat UI         | `http://localhost:8888`  | **Embedded SPA** (served from Gateway)   |
| Dev Web Chat        | `http://localhost:3000`  | Next.js Dev Server (when running `make dev`) |

### Connect with Go SDK

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

HotPlex sits between frontend clients and backend AI coding agents, featuring a built-in **Meta-Cognition Core** that abstracts protocol differences into a unified **AEP v1** (Agent Exchange Protocol) WebSocket layer.

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

## 🔗 SDKs & Libraries

|    Language    | Path                                                         | Features                                              |
| :------------: | :----------------------------------------------------------- | :---------------------------------------------------- |
|     **Go**     | [`client/`](client/)                                         | Full-featured, channel-based events, production-grade |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | Streaming, multi-turn chat, React compatible          |
|   **Python**   | [`examples/python-client/`](examples/python-client/)         | Asyncio, session resume, CLI ready                    |
|    **Java**    | [`examples/java-client/`](examples/java-client/)             | Enterprise AEP v1 implementation                      |

## 🛠️ Configuration

| Key                       | Default                      | Description                                       |
| :------------------------ | :--------------------------- | :------------------------------------------------ |
| `agent_config.enabled`    | `true`                       | Enable agent personality/context injection        |
| `webchat.enabled`         | `true`                       | Serve embedded webchat SPA from gateway           |
| `worker.auto_retry.enabled`| `true`                      | Intelligent LLM retry with exponential backoff    |
| `gateway.addr`            | `localhost:8888`             | WebSocket gateway address                         |
| `admin.addr`              | `localhost:9999`             | Admin API address                                 |
| `db.path`                 | `~/.hotplex/data/hotplex.db` | SQLite database path                              |
| `log.level`               | `info`                       | Log level: debug, info, warn, error               |

> [!TIP]
> See [Config Reference](docs/management/Config-Reference.md) for the full list of environment variables and YAML settings.

## 📖 Documentation

| Area                | Guide                                                                                                                                                     |
| :------------------ | :-------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Getting Started** | [Quick Start](docs/User-Manual.md) · [Reference Manual](docs/Reference-Manual.md) · [Whitepaper](docs/Product-Whitepaper.md)                              |
| **Protocol**        | [AEP v1 Specification](docs/architecture/AEP-v1-Protocol.md)                                                                                              |
| **Architecture**    | [Gateway Design](docs/architecture/Worker-Gateway-Design.md) · [Agent Config Design](docs/architecture/Agent-Config-Design.md) · [Meta-Cognition Design](internal/agentconfig/META-COGNITION.md) |
| **Security**        | [Authentication](docs/security/Security-Authentication.md) · [SSRF Protection](docs/security/SSRF-Protection.md)                                          |
| **Operations**      | [Admin API](docs/management/Admin-API-Design.md) · [Observability](docs/management/Observability-Design.md) · [Testing](docs/testing/Testing-Strategy.md) |

## 👥 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create your feature branch (`git checkout -b feat/AmazingFeature`)
3. Commit with conventional messages (`git commit -m 'feat: add AmazingFeature'`)
4. Push to the branch (`git push origin feat/AmazingFeature`)
5. Open a Pull Request

> [!NOTE]
> All build/test/lint operations must use `make` targets. See `make help` for the full list.

## 🛡️ Security

If you discover a security vulnerability, please do NOT open a public issue. Report it via [SECURITY.md](SECURITY.md) or contact maintainers directly.

## 📜 License

Distributed under the [Apache License 2.0](LICENSE).
