<h1 align="center">HotPlex 网关</h1>

<p align="center">
  <strong>AI Coding Agent 统一接入桥梁</strong>
</p>

<p align="center">
  高性能 Go 网关，提供统一的 WebSocket 接口，<br>
  一键接入任意 AI Coding Agent，覆盖 Web、Slack 和飞书全渠道。
</p>

<p align="center">
  <strong>简体中文</strong> | <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/hrygo/hotplex/actions/workflows/ci.yml"><img src="https://github.com/hrygo/hotplex/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/Version-v1.1.2-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---



## ✨ 核心亮点

- 🌐 **统一 WebSocket 接口** — 23+ AEP v1 事件类型，支持流式输出、权限交互和 MCP Elicitation
- 🔌 **多渠道桥接** — 双向支持 Slack (Socket Mode) 和飞书 (WebSocket)
- 🤖 **Agent 配置注入** — 人格、规则、记忆通过 B/C 双通道 XML 系统自动注入
- 🧠 **内置元认知系统** — 5 状态机、LLM 智能重试与 3 层故障自愈，赋予 Agent 环境感知与自我修复能力
- 🛡️ **生产级可靠性** — JWT ES256 认证、SSRF 防护、孤儿进程清理与物理隔离安全
- 📊 **全链路可观测** — Prometheus 指标、OpenTelemetry 链路追踪、结构化 JSON 日志
- 🛠️ **一体化 CLI** — `onboard`、`doctor`、`security`、`status` 集成在单个二进制中
- 🌍 **多语言 SDK** — Go、TypeScript、Python、Java 客户端开箱即用

## ⚡ 快速开始

### 从源码安装

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # 检查工具链 + 构建 + 测试
```

### 或使用 Docker

```bash
cp configs/env.example .env  # 填入你的 API 密钥
docker compose up -d
```

### 配置

```bash
# 交互式配置向导（自动检测已有配置，支持保留或重新配置）
hotplex onboard

# 或快速自动生成全部配置：
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

### 启动

```bash
make dev
```

| 服务             | 地址                     |
| :--------------- | :----------------------- |
| 网关 (WebSocket) | `ws://localhost:8888/ws` |
| Admin API        | `http://localhost:9999`  |
| Web Chat UI      | `http://localhost:3000`  |

### 使用 Go SDK 连接

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

    c.SendInput(context.Background(), "解释一下 HotPlex 架构")

    for env := range c.Events() {
        if data, ok := env.AsMessageDeltaData(); ok {
            fmt.Print(data.Content)
        }
    }
}
```

## 🧱 系统架构

HotPlex 位于前端客户端和后端 AI Coding Agent 之间，内置元认知控制内核，将协议差异抽象为统一的 AEP v1 WebSocket 层。

```
┌──────────┐   ┌──────────┐   ┌──────────┐
│  Web UI  │   │  Slack   │   │  飞书    │
└────┬─────┘   └────┬─────┘   └────┬─────┘
     │              │              │
     └──────────────┼──────────────┘
                    │  WebSocket / AEP v1
              ┌─────┴─────┐
              │  HotPlex  │  会话 · 认证 · 重试 · 配置注入
              │  Gateway  │  指标 · 链路追踪 · Admin API
              └─────┬─────┘
     ┌──────────────┼──────────────┐
     │              │              │
┌────┴─────┐  ┌────┴──────┐  ┌───┴───────┐
│  Claude  │  │  OpenCode │  │  Pi-mono  │
│  Code    │  │  Server   │  │           │
└──────────┘  └───────────┘  └───────────┘
```

## 🔗 SDK 与客户端库

|      语言      | 路径                                                         | 特性                             |
| :------------: | :----------------------------------------------------------- | :------------------------------- |
|     **Go**     | [`client/`](client/)                                         | 全功能支持，事件驱动，生产级可用 |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | 流式输出、多轮对话、React 兼容   |
|   **Python**   | [`examples/python-client/`](examples/python-client/)         | Asyncio 支持、会话恢复、CLI 友好 |
|    **Java**    | [`examples/java-client/`](examples/java-client/)             | 企业级 AEP v1 协议实现           |

## 🛠️ 配置说明

| 配置项                    | 默认值                       | 说明                                |
| :------------------------ | :--------------------------- | :---------------------------------- |
| `agent_config.enabled`    | `true`                       | 启用 Agent 人格/上下文注入          |
| `agent_config.config_dir` | `~/.hotplex/agent-configs/`  | Agent 配置文件目录 (SOUL.md 等)     |
| `gateway.addr`            | `:8888`                      | WebSocket 网关地址                  |
| `admin.addr`              | `:9999`                      | Admin API 地址                      |
| `db.path`                 | `~/.hotplex/data/hotplex.db` | SQLite 数据库路径                   |
| `log.level`               | `info`                       | 日志级别 (debug, info, warn, error) |

> [!TIP]
> 完整的环境变量和 YAML 设置请参考 [配置指南](docs/management/Config-Reference.md)。

## 📖 文档中心

| 领域         | 指南                                                                                                                                                       |
| :----------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **入门指南** | [快速上手](docs/User-Manual.md) · [技术参考手册](docs/Reference-Manual.md) · [产品白皮书](docs/Product-Whitepaper.md)                                      |
| **协议规范** | [AEP v1 协议详解](docs/architecture/AEP-v1-Protocol.md)                                                                                                    |
| **架构设计** | [网关架构](docs/architecture/Worker-Gateway-Design.md) · [Agent 配置设计](docs/architecture/Agent-Config-Design.md) · [元认知内核设计](internal/agentconfig/META-COGNITION.md) |
| **安全**     | [认证机制](docs/security/Security-Authentication.md) · [SSRF 防护](docs/security/SSRF-Protection.md)                                                       |
| **运维**     | [Admin API 手册](docs/management/Admin-API-Design.md) · [可观测性](docs/management/Observability-Design.md) · [测试策略](docs/testing/Testing-Strategy.md) |

## 👥 参与贡献

我们欢迎任何形式的贡献！请阅读 [贡献指南](CONTRIBUTING.md) 了解更多。

1. Fork 本项目
2. 创建特性分支 (`git checkout -b feat/AmazingFeature`)
3. 使用规范提交格式 (`git commit -m 'feat: add AmazingFeature'`)
4. 推送到分支 (`git push origin feat/AmazingFeature`)
5. 开启 Pull Request

> [!NOTE]
> 所有构建/测试/lint 操作必须使用 `make` 目标。完整列表请运行 `make help`。

## 🛡️ 安全

如果您发现安全漏洞，请**不要**公开开启 Issue。请通过 [安全政策](SECURITY.md) 报告漏洞，或直接联系维护者。

## 📜 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。
