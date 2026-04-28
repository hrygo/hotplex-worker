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
  <img src="https://img.shields.io/badge/Version-v1.1.2-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>


## ✨ Highlights

- 🌐 **Unified WebSocket interface** — 23+ AEP v1 event types for streaming, permissions, and MCP Elicitation
- 🔌 **Multi-channel bridge** — bidirectional support for Slack (Socket Mode) and Feishu (WebSocket)
- 🤖 **Agent config injection** — personality, rules, and memory auto-injected via B/C dual-channel XML system
- 🧠 **Built-in Meta-Cognition** — 5-state machine, intelligent LLM retry, and 3-layer self-healing for environment awareness
- 🛡️ **Production-hardened** — JWT ES256 auth, SSRF protection, orphan process cleanup, and physical isolation
- 📊 **Full observability** — Prometheus metrics, OpenTelemetry tracing, structured JSON logging
- 🛠️ **Self-contained CLI** — `onboard`, `doctor`, `security`, `status` in a single binary
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
make dev
```

| Service             | Address                  |
| :------------------ | :----------------------- |
| Gateway (WebSocket) | `ws://localhost:8888/ws` |
| Admin API           | `http://localhost:9999`  |
| Web Chat UI         | `http://localhost:3000`  |

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

## 🏗️ Architecture

HotPlex sits between frontend clients and backend AI coding agents, featuring a built-in meta-cognition core that abstracts protocol differences into a unified AEP v1 WebSocket layer.

```
┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│   Web UI    │   │    Slack    │   │   Feishu    │
└──────┬──────┘   └──────┬──────┘   └──────┬──────┘
       │                 │                 │
       └─────────────────┼─────────────────┘
                         │
                WebSocket / AEP v1
                         │
               ┌─────────┴─────────┐
               │      HotPlex      │  Session · Auth · Retry · Config
               │      Gateway      │  Metrics · Tracing · Admin API
               └─────────┬─────────┘
                         │
       ┌─────────────────┼─────────────────┐
       │                 │                 │
┌──────┴──────┐   ┌──────┴──────┐   ┌──────┴──────┐
│ Claude Code │   │   OpenCode  │   │   Pi-mono   │
└─────────────┘   └─────────────┘   └─────────────┘
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
| `agent_config.config_dir` | `~/.hotplex/agent-configs/`  | Config files directory (SOUL.md, AGENTS.md, etc.) |
| `gateway.addr`            | `:8888`                      | WebSocket gateway address                         |
| `admin.addr`              | `:9999`                      | Admin API address                                 |
| `db.path`                 | `~/.hotplex/data/hotplex.db` | SQLite database path                              |
| `log.level`               | `info`                       | Log level: debug, info, warn, error               |

> [!TIP]
> See [Config Reference](docs/management/Config-Reference.md) for the full list of environment variables and YAML settings.

## 📖 Documentation

| Area                | Guide                                                                                                                                                                                                                                                           |
| :------------------ | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Getting Started** | [Quick Start](docs/User-Manual.md) · [Slack Setup](docs/channels/slack/slack-integration-guide.md) · [Feishu Setup](docs/channels/feishu/feishu-integration-guide.md) · [Reference Manual](docs/Reference-Manual.md) · [Whitepaper](docs/Product-Whitepaper.md) |
| **Protocol**        | [AEP v1 Specification](docs/architecture/AEP-v1-Protocol.md)                                                                                                                                                                                                    |
| **Architecture**    | [Gateway Design](docs/architecture/Worker-Gateway-Design.md) · [Agent Config Design](docs/architecture/Agent-Config-Design.md) · [Meta-Cognition Design](internal/agentconfig/META-COGNITION.md)                                                                |
| **Security**        | [Authentication](docs/security/Security-Authentication.md) · [SSRF Protection](docs/security/SSRF-Protection.md)                                                                                                                                                |
| **Operations**      | [Admin API](docs/management/Admin-API-Design.md) · [Observability](docs/management/Observability-Design.md) · [Testing](docs/testing/Testing-Strategy.md)                                                                                                       |

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
