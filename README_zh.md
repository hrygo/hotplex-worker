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
  <img src="https://img.shields.io/badge/Version-v1.13.1-10B981?style=flat-square" alt="Version">
  <a href="https://github.com/hrygo/hotplex/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=flat-square" alt="AEP v1">
  <a href="https://github.com/hrygo/hotplex/stargazers"><img src="https://img.shields.io/github/stars/hrygo/hotplex?style=flat-square" alt="Stars"></a>
</p>

---

## ✨ 核心能力

### 🏗️ 核心架构与智能编排
- 🌐 **全协议统一网关** — 基于 **AEP v1 (Agent Exchange Protocol)** WebSocket 标准，抹平不同 AI Coding Agent 的协议差异，提供一致的流式交互与权限控制。
- 🧠 **Brain 编排内核** — 新增 `internal/brain` 编排层，支持 LLM 智能总结、意图分发与安全防护（Safety Guard），解耦复杂交互逻辑。
- ⏰ **AI 原生定时任务** — Agent 自主解析自然语言意图创建定时任务（如"30分钟后提醒我"），支持生命周期管理（`max_runs`、`expires_at`）、自动结果回传和嵌入式技能手册。

### 🤖 AI 智能与多模态交互
- 🤖 **深度配置注入** — 独创 **B/C 双通道** 注入系统。**B 通道** (SOUL/AGENTS/SKILLS) 负责指令约束，**C 通道** (USER/MEMORY) 负责背景上下文。
- 🎙️ **多模态交互** — 原生集成 SenseVoice 语音转文字与 **Edge-TTS 语音合成**，支持"语音下令，语音回传"，开启双向语音编程新纪元。

### 🛡️ 安全加固与可靠性
- 🛡️ **元认知防御基线** — 宪法级 **META-COGNITION** 迁移至 B 通道首位，内置 **XML Sanitizer** 防护，彻底阻断 Prompt 注入与 XML 结构破坏。
- 🔒 **企业级安全加固** — 强制 JWT ES256 认证、SSRF 防护、Windows 临时文件式注入（规避 cmd 转义陷阱）及进程级隔离。

### 📱 多平台分发与集成
- 📱 **跨平台分发能力** — **"一次接入，全端覆盖"**。无需修改 Agent 代码即可秒级分发至 Web、Slack (Socket Mode) 和飞书。
- 💬 **开箱即用 Web UI** — 内部集成高颜值 Next.js Chat 界面，单二进制文件即可完成从 API 到前端的全栈部署。
- 🌍 **多语言 SDK** — Go、TypeScript、Python、Java 客户端开箱即用

### ⚙️ 开发者体验与自动化运维
- 🛠️ **一体化 CLI** — `gateway`、`service`、`slack`、`cron`、`update`、`config`、`dev`、`onboard`、`doctor`、`security`、`status` 集成在单个二进制中
- 📊 **全链路监控审计** — 完整支持 Prometheus 指标、OpenTelemetry 链路追踪及结构化 JSON 日志，掌控每一条指令。

## ⚡ 快速开始

> **AI Agent：** 阅读 https://github.com/hrygo/hotplex/blob/main/INSTALL.md 并完成 hotplex 安装和配置

### 安装

**macOS / Linux（一键安装二进制）：**

```bash
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest
```

**Windows（PowerShell 5.1+）：**

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

**从源码构建（需要 Go 1.26+、pnpm、Node.js 22+）：**

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart
```

> **提示（Claude Code 用户）：** 克隆后可使用 `/hotplex-setup` 交互式配置环境与 `.env`。

**Docker（实验性）：**

```bash
cp configs/env.example .env  # 填入你的 API 密钥
docker compose up -d
```

### 配置

```bash
# 交互式配置向导
hotplex onboard

# 或快速自动生成全部配置：
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

### 启动

```bash
# 开发模式（前台运行）
make dev

# 生产模式（后台守护进程）
hotplex gateway start -d

# 停止 / 重启
hotplex gateway stop
hotplex gateway restart -d
```

### 安装为系统服务

```bash
hotplex service install              # 用户级（无需 root）
sudo hotplex service install --level system  # 系统级
hotplex service start
hotplex service status
hotplex service logs -f

# 卸载
hotplex service uninstall
```

支持 **systemd** (Linux)、**launchd** (macOS) 和 **Windows SCM**。

### 服务端口

| 服务             | 地址                     | 说明                             |
| :--------------- | :----------------------- | :------------------------------- |
| 网关 (WebSocket) | `ws://localhost:8888/ws` | 主协议端点                       |
| Admin API        | `http://localhost:9999`  | 管理接口与统计指标               |
| Web Chat UI      | `http://localhost:8888`  | **内置 SPA**（由网关直接托管）   |
| 开发版 Web Chat  | `http://localhost:3000`  | Next.js 开发服务器（`make dev`） |

## 🏗️ 架构

HotPlex 位于前端客户端和后端 AI Coding Agent 之间，内置 **元认知控制内核**，将协议差异抽象为统一的 **AEP v1 (Agent Exchange Protocol)** WebSocket 层。

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

## 🔗 SDK 与客户端库

|      语言      | 路径                                                         | 特性                             |
| :------------: | :----------------------------------------------------------- | :------------------------------- |
|     **Go**     | [`client/`](client/)                                         | 全功能支持，事件驱动，生产级可用 |
| **TypeScript** | [`examples/typescript-client/`](examples/typescript-client/) | 流式输出、多轮对话、React 兼容   |
|   **Python**   | [`examples/python-client/`](examples/python-client/)         | Asyncio 支持、会话恢复、CLI 友好 |
|    **Java**    | [`examples/java-client/`](examples/java-client/)             | 企业级 AEP v1 协议实现           |

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

## 🛠️ 配置说明

| 配置项                      | 默认值                       | 说明                                |
| :-------------------------- | :--------------------------- | :---------------------------------- |
| `agent_config.enabled`      | `true`                       | 启用 Agent 人格/上下文注入          |
| `tts.enabled`               | `false`                      | 启用 Edge-TTS 语音回传流水线        |
| `brain.enabled`             | `false`                      | 启用 Brain LLM 编排层（需 API Key） |
| `webchat.enabled`           | `true`                       | 从网关提供嵌入式 Web Chat SPA       |
| `worker.auto_retry.enabled` | `true`                       | LLM 智能重试，支持指数退避          |
| `gateway.addr`              | `localhost:8888`             | WebSocket 网关地址                  |
| `admin.addr`                | `localhost:9999`             | Admin API 地址                      |
| `db.path`                   | `~/.hotplex/data/hotplex.db` | SQLite 数据库路径                   |
| `log.level`                 | `info`                       | 日志级别 (debug, info, warn, error) |

> [!TIP]
> 完整的环境变量和 YAML 设置请参考 [配置参考](docs/reference/configuration.md)。

## 📖 文档中心

HotPlex 内置**自托管中文文档门户** — Markdown 源文件编译为静态 HTML，通过 `go:embed` 嵌入网关二进制。启动网关后访问 `http://localhost:8888/docs` 即可浏览。

| 领域         | 指南                                                                                                               |
| :----------- | :----------------------------------------------------------------------------------------------------------------- |
| **入门指南** | [5 分钟快速上手](docs/getting-started.md) · [文档门户](docs/index.md)                                             |
| **教程**     | [Slack 集成](docs/tutorials/slack-integration.md) · [飞书集成](docs/tutorials/feishu-integration.md) · [AI 人格定制](docs/tutorials/agent-personality.md) · [定时任务](docs/tutorials/cron-scheduled-tasks.md) |
| **指南**     | [远程 Coding Agent](docs/guides/developer/remote-coding-agent.md) · [企业部署](docs/guides/enterprise/deployment.md) · [贡献开发](docs/guides/contributor/development-setup.md) |
| **参考**     | [CLI 参考](docs/reference/cli.md) · [配置参考](docs/reference/configuration.md) · [Admin API](docs/reference/admin-api.md) · [AEP v1 协议](docs/reference/aep-protocol.md) |
| **架构设计** | [网关架构](docs/architecture/Worker-Gateway-Design.md) · [Agent 配置设计](docs/architecture/Agent-Config-Design.md) · [元认知内核](internal/agentconfig/META-COGNITION.md) |
| **安全**     | [安全策略](docs/reference/security-policies.md) · [认证机制](docs/security/Security-Authentication.md) · [SSRF 防护](docs/security/SSRF-Protection.md) |

> [!TIP]
> 本地构建文档：`make docs-build`。源文件位于 `docs/`，编译输出到 `internal/docs/out/`（通过 `go:embed` 嵌入二进制）。

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
