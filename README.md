<h1 align="center">Hotplex Gateway</h1>

<p align="center">
  <strong>The Unified Bridge for AI Coding Agents</strong>
</p>

<p align="center">
  <a href="README_zh.md">简体中文</a> | <strong>English</strong>
</p>

<p align="center">
  <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://github.com/hrygo/hotplex/actions/workflows/ci.yml/badge.svg" alt="CI Status"></a>
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <img src="https://img.shields.io/badge/Platforms-Slack%20%7C%20Feishu-E11D48?style=flat-square" alt="Platforms">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---

Hotplex is a high-performance Go gateway that provides a **single WebSocket interface** to access any AI Coding Agent. It abstracts protocol differences, manages complex session lifecycles, and connects users across Web, Slack, and Feishu — all through one optimized binary.

**One Gateway. Any Agent. Every Channel.**

## 🧭 Table of Contents
- [Core Features](#-core-features)
- [Quick Start](#-quick-start)
- [Architecture](#-architecture)
- [SDKs & Libraries](#-sdks--libraries)
- [Configuration](#-configuration)
- [Documentation](#-documentation)
- [Contributing](#-contributing)
- [License](#-license)

## 🚀 Core Features

### 🔌 Connectivity
- 🔹 **Unified AEP v1 Protocol**: 23+ event types over WebSocket (streaming, permissions, MCP Elicitation).
- 🔹 **Multi-Channel Bridge**: Bidirectional support for **Slack** (Socket Mode) and **Feishu** (WebSocket).
- 🔹 **Worker Adapters**: Out-of-the-box support for Claude Code, OpenCode Server, and Pi-mono.

### 🤖 Agent Intelligence
- 🔹 **Agent Config System**: Define agent personality (SOUL.md), workspace rules (AGENTS.md), tool guides (SKILLS.md), user profile (USER.md), and persistent memory (MEMORY.md) — automatically injected into every worker session via nested XML tags (`<directives>` + `<context>`) with per-section behavioral directives.
- 🔹 **Platform Variants**: Per-platform config overrides (e.g. `SOUL.slack.md`, `SOUL.feishu.md`) appended automatically for channel-specific behavior.

### 🛡️ Reliability & Security
- 🔹 **Robust Session Management**: 5-state lifecycle machine with crash recovery and auto-reconnection.
- 🔹 **Security First**: JWT ES256 authentication, SSRF protection, and command whitelisting.
- 🔹 **Observability**: Prometheus metrics, OpenTelemetry tracing, and structured JSON logging.
- 🔹 **Admin API**: Dedicated management interface for session control and health monitoring.

### 💎 Developer Experience
- 🔹 **CLI Self-Service**: Interactive `onboard` wizard, `doctor` diagnostics, `security` audit, `status` check, and `config validate` — all built into a single binary.
- 🔹 **Ready-to-use Web UI**: Next.js 16 + Vercel AI SDK integration with Premium UX, Generative UI (GenUI), and Glassmorphism design system.
- 🔹 **Hot-Reload Config**: Update gateway settings without downtime.
- 🔹 **Multi-Language SDKs**: Native support for Go, TypeScript, Python, and Java.

## ⚡ Quick Start

### 1. Installation
```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart
```

### 2. Configure

```bash
# Interactive setup wizard (detects existing config, supports keep-or-reconfigure)
hotplex onboard

# Or quick auto-generate all configs:
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

### 3. Run Development Servers
```bash
make dev
```
- **Gateway WebSocket**: `ws://localhost:8888/ws`
- **Admin API**: `http://localhost:9999`
- **Web Chat**: `http://localhost:3000`

### 4. Connect via Go SDK
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

    c.SendInput(context.Background(), "Explain Hotplex architecture")

    for env := range c.Events() {
        if data, ok := env.AsMessageDeltaData(); ok {
            fmt.Print(data.Content)
        }
    }
}
```

## 🧱 Architecture

Hotplex acts as an orchestration layer between frontend clients and backend coding agents.

![Architecture](assets/architecture.svg)

## 📦 SDKs & Libraries

| Language | Path | Features |
|:---:|:---|:---|
| **Go** | [`client/`](client/) | **Full feature**, channel-based events, production-grade |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | Streaming, multi-turn chat, React compatible |
| **Python** | [`examples/python-client/`](examples/python-client/) | Asyncio, session resume, CLI ready |
| **Java** | [`examples/java-client/`](examples/java-client/) | Enterprise AEP v1 implementation |

## 🛠️ Configuration

Hotplex uses Viper for configuration with support for environment variable overrides.

| Key | Default | Description |
|:---|:---|:---|
| `agent_config.enabled` | `true` | Enable agent personality/context injection |
| `agent_config.config_dir` | `~/.hotplex/agent-configs/` | Directory for SOUL.md, AGENTS.md, etc. |
| `gateway.addr` | `:8888` | WebSocket gateway endpoint |
| `admin.addr` | `:9999` | Admin API endpoint |
| `db.path` | `~/.hotplex/data/hotplex.db` | SQLite database location |
| `log.level` | `info` | debug, info, warn, error |

> [!TIP]
> See [Config Reference](docs/management/Config-Reference.md) for the full list of environment variables and YAML settings.

## 📖 Documentation

| Area | Guide |
|:---|:---|
| **Getting Started** | [Quick Start](docs/User-Manual.md) · [Reference Manual](docs/Reference-Manual.md) · [Whitepaper](docs/Product-Whitepaper.md) |
| **Protocol** | [AEP v1 Specification](docs/architecture/AEP-v1-Protocol.md) |
| **Internals** | [Gateway Design](docs/architecture/Worker-Gateway-Design.md) · [Agent Config Design](docs/architecture/Agent-Config-Design.md) · [Security](docs/security/Security-Authentication.md) |
| **Management** | [Admin API](docs/management/Admin-API-Design.md) · [Testing](docs/testing/Testing-Strategy.md) |

## 👥 Contributing

We welcome contributions of all kinds! Please see our [Contributing Guide](CONTRIBUTING.md) for more details.

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feat/AmazingFeature`)
3. Commit your Changes (`git commit -m 'feat: Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feat/AmazingFeature`)
5. Open a Pull Request

## 🛡️ Security

If you find a security vulnerability, please do NOT open a public issue. Report it via the instructions in our [Security Policy](SECURITY.md) (or contact maintainers directly).

## 📜 License

Distributed under the Apache License 2.0. See [`LICENSE`](LICENSE) for more information.

---

<p align="center">
  Built with ❤️ by the Hotplex Team
</p>
