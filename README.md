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
  <img src="https://img.shields.io/badge/Version-v1.11.2-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---

## ✨ Core Capabilities

### 🏗️ Core Architecture & Agent Orchestration
- 🌐 **Universal Agent Gateway** — Abstract any AI Coding Agent protocol into a unified AEP v1 (Agent Exchange Protocol) WebSocket interface for consistent streaming and interaction.
- 🧠 **Brain Orchestration Core** — New `internal/brain` layer for LLM-based summarization, intent routing, and safety guarding, decoupling complex AI logic.
- ⏰ **AI-Native Cron Scheduler** — Agents autonomously create scheduled tasks from natural language (e.g., "remind me in 30 minutes"), with lifecycle management (`max_runs`, `expires_at`), automatic result delivery, and embedded skill manual.

### 🤖 AI Intelligence & Interaction
- 🤖 **Deep Personality Injection** — Dynamic **B/C dual-channel** injection: **B-channel** (SOUL/AGENTS/SKILLS) for directives and **C-channel** (USER/MEMORY) for context.
- 🎙️ **Multi-Modal Interaction** — Native Speech-to-Text (SenseVoice) and **Edge-TTS Voice Reply** support for a complete voice-in, voice-out development workflow.

### 🛡️ Security & Reliability
- 🛡️ **Meta-Cognition Hardening** — Constitutional **META-COGNITION** promoted to the top of B-channel with built-in **XML Sanitizer** to block prompt injection.
- 🔒 **Enterprise-Grade Security** — JWT ES256 authentication, SSRF protection, and **Windows File-Based Injection** to bypass cmd.exe escaping traps.

### 📱 Multi-Platform Delivery & Integration
- 📱 **Cross-Platform Delivery** — **"Write Once, Deploy Anywhere"**. Bridge agents to Web, Slack (Socket Mode), and Feishu (WebSocket) with zero code changes.
- 💬 **Embedded Web Chat** — A single binary serves both the API/WebSocket gateway and a premium Next.js-based web chat interface out of the box.
- 🌍 **Multi-language SDKs** — Go, TypeScript, Python, Java clients ready to use

### ⚙️ Developer Experience & Operations
- 🛠️ **Self-contained CLI** — `gateway`, `service`, `slack`, `cron`, `update`, `config`, `dev`, `onboard`, `doctor`, `security`, `status` in a single binary
- 📊 **End-to-End Observability** — Native Prometheus metrics, OpenTelemetry tracing, and structured JSON logging for full auditability.

## ⚡ Quick Start

> **AI Agents:** Read https://github.com/hrygo/hotplex/blob/main/INSTALL.md and complete the HotPlex installation and configuration

### Install

**macOS / Linux (one-click binary):**

```bash
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest
```

**Windows (PowerShell 5.1+):**

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

**From source (requires Go 1.26+, pnpm, Node.js 22+):**

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart
```

> **Tip (Claude Code):** After cloning, use `/hotplex-setup` for interactive environment setup and `.env` configuration.

**Docker (Experimental):**

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
hotplex service install              # user-level (no root)
sudo hotplex service install --level system  # system-wide
hotplex service start
hotplex service status
hotplex service logs -f

# Uninstall
hotplex service uninstall
```

Supports **systemd** (Linux), **launchd** (macOS), and **Windows SCM**.

### Services

| Service             | Address                  | Note                                   |
| :------------------ | :----------------------- | :------------------------------------- |
| Gateway (WebSocket) | `ws://localhost:8888/ws` | Main protocol endpoint                 |
| Admin API           | `http://localhost:9999`  | Management & Statistics                |
| Web Chat UI         | `http://localhost:8888`  | **Embedded SPA** (served from Gateway) |
| Dev Web Chat        | `http://localhost:3000`  | Next.js Dev Server (`make dev`)        |

## 🏗️ Architecture

HotPlex sits between frontend clients and backend AI coding agents, featuring a built-in **Meta-Cognition Core** that abstracts protocol differences into a unified **AEP v1** WebSocket layer.

```
┌──────────┐   ┌──────────┐   ┌──────────┐
│  Web UI  │   │  Slack   │   │  Feishu  │
└────┬─────┘   └────┬─────┘   └────┬─────┘
     │              │              │
     └──────────────┼──────────────┘
                    │
              ┌─────┴─────┐
              │  HotPlex  │
              │  Gateway  │
              └─────┬─────┘
                    │
         ┌──────────┴──────────┐
         │                     │
   ┌─────┴─────┐          ┌────┴──────┐
   │  Claude   │          │  OpenCode │
   │  Code     │          │  Server   │
   └───────────┘          └───────────┘
```

## 🔗 SDKs & Libraries

|    Language    | Path                                                         | Features                                              |
| :------------: | :----------------------------------------------------------- | :---------------------------------------------------- |
|     **Go**     | [`client/`](client/)                                         | Full-featured, channel-based events, production-grade |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | Streaming, multi-turn chat, React compatible          |
|   **Python**   | [`examples/python-client/`](examples/python-client/)         | Asyncio, session resume, CLI ready                    |
|    **Java**    | [`examples/java-client/`](examples/java-client/)             | Enterprise AEP v1 implementation                      |

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

## 🛠️ Configuration

| Key                         | Default                      | Description                                    |
| :-------------------------- | :--------------------------- | :--------------------------------------------- |
| `agent_config.enabled`      | `true`                       | Enable agent personality/context injection     |
| `tts.enabled`               | `true`                       | Enable Edge-TTS voice reply (voice-in → voice-out) |
| `brain.enabled`             | `false`                      | Enable Brain LLM orchestration (requires key)  |
| `webchat.enabled`           | `true`                       | Serve embedded webchat SPA from gateway        |
| `worker.auto_retry.enabled` | `true`                       | Intelligent LLM retry with exponential backoff |
| `gateway.addr`              | `localhost:8888`             | WebSocket gateway address                      |
| `admin.addr`                | `localhost:9999`             | Admin API address                              |
| `db.path`                   | `~/.hotplex/data/hotplex.db` | SQLite database path                           |
| `log.level`                 | `info`                       | Log level: debug, info, warn, error            |

> [!TIP]
> See [Config Reference](docs/management/Config-Reference.md) for the full list of environment variables and YAML settings.

## 📖 Documentation

HotPlex ships with **self-hosted documentation** — a Chinese-first docs portal built from Markdown sources, compiled into static HTML, and embedded directly into the gateway binary. Access it at `http://localhost:8888/docs` after starting the gateway.

| Area                | Guide                                                                                                              |
| :------------------ | :----------------------------------------------------------------------------------------------------------------- |
| **Getting Started** | [5-Minute Quick Start](docs/getting-started.md) · [Docs Portal](docs/index.md)                                    |
| **Tutorials**       | [Slack Integration](docs/tutorials/slack-integration.md) · [Feishu Integration](docs/tutorials/feishu-integration.md) · [AI Personality](docs/tutorials/agent-personality.md) · [Cron Tasks](docs/tutorials/cron-scheduled-tasks.md) |
| **Guides**          | [Remote Coding Agent](docs/guides/developer/remote-coding-agent.md) · [Enterprise Deployment](docs/guides/enterprise/deployment.md) · [Contributing](docs/guides/contributor/development-setup.md) |
| **Reference**       | [CLI Reference](docs/reference/cli.md) · [Configuration](docs/reference/configuration.md) · [Admin API](docs/reference/admin-api.md) · [AEP v1 Protocol](docs/reference/aep-protocol.md) |
| **Architecture**    | [Gateway Design](docs/architecture/Worker-Gateway-Design.md) · [Agent Config Design](docs/architecture/Agent-Config-Design.md) · [Meta-Cognition](internal/agentconfig/META-COGNITION.md) |
| **Security**        | [Security Policies](docs/reference/security-policies.md) · [Authentication](docs/security/Security-Authentication.md) · [SSRF Protection](docs/security/SSRF-Protection.md) |

> [!TIP]
> Build docs locally: `make docs-build`. Source files live in `docs/`, output goes to `internal/docs/out/` (embedded via `go:embed`).

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
