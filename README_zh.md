<h1 align="center">Hotplex</h1>

<p align="center">
<strong>AI Coding Agent 统一接入网关</strong>
</p>

<p align="center">
<strong>简体中文</strong> | <a href="README.md">English</a>
</p>

<p align="center">
<img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go" alt="Go">
<img src="https://img.shields.io/badge/Protocol-AEP%20v1-7C3AED?style=for-the-badge" alt="AEP v1">
<img src="https://img.shields.io/badge/License-Apache%202.0-3B82F6?style=for-the-badge" alt="License">
<img src="https://img.shields.io/badge/Platform-Slack%20%7C%20飞书-E11D48?style=for-the-badge" alt="Platforms">
</p>

---

Hotplex 是一个单进程 Go 网关，通过 **一个 WebSocket 接口** 接入任意 AI Coding Agent。它会话管理、协议适配、多渠道接入——一个二进制文件搞定。

**一个网关。任意 Agent。全渠道接入。**

```
   Web Chat ──┐
              │     ┌──────────────────────┐     ┌──────────────┐
  Slack Bot ──┼────▶│    Hotplex Gateway   │────▶│ Claude Code  │
              │     │    (AEP v1 / WS)     │     │ OpenCode Srv │
  飞书 Bot ───┘     └──────────────────────┘     └──────────────┘
                          Go 1.26+ · SQLite        NDJSON/stdio
```

## 特性

- **AEP v1 协议** — 23+ 种事件类型的 WebSocket 信令：流式输出、权限交互、问答交互、MCP Elicitation、用户交互
- **会话状态机** — 5 状态生命周期（Created → Running → Idle → Terminated → Deleted），支持崩溃恢复和断线恢复
- **Worker 适配器** — 插件式：Claude Code、OpenCode Server、ACPX、Pi-mono。BaseWorker embedding 模式，轻松扩展
- **多渠道接入** — Slack（Socket Mode）和飞书（WebSocket Events）双向消息桥接，支持用户交互
- **Web Chat UI** — Next.js 15 + React 19 + Vercel AI SDK，开箱即用
- **多语言 SDK** — Go、TypeScript、Python、Java
- **企业级特性** — JWT ES256 认证、Admin API、Prometheus 指标、OpenTelemetry 链路追踪、配置热重载、语音转文字

## 快速开始

### 安装

```bash
git clone https://github.com/hotplex/hotplex-worker.git
cd hotplex-worker
cp configs/env.example .env
make quickstart
```

### 启动

```bash
make dev
```

启动后网关在 `http://localhost:8888`，Web Chat 在 `http://localhost:3000`。

### 生成 Token

```bash
go run client/scripts/gen-token/main.go -secret "$(grep HOTPLEX_JWT_SECRET .env | cut -d= -f2)"
```

### Go SDK 连接

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

    // 发送消息
    c.SendInput(context.Background(), "解释一下 AEP v1 协议")

    // 处理流式响应
    go func() {
        for env := range c.Events() {
            switch env.Event.Type {
            case "message.delta":
                delta := env.Event.Data.(map[string]any)
                fmt.Print(delta["content"].(string))
            case "done":
                fmt.Println("\n--- 完成 ---")
            }
        }
    }()

    select {} // 阻塞
}
```

## 使用

### TypeScript 连接

```typescript
import { HotplexClient } from "@hotplex/client";

const client = new HotplexClient("ws://localhost:8888/ws", {
  token: "<your-jwt-token>",
});

client.on("message.delta", (data) => process.stdout.write(data.content));
client.on("done", () => console.log("\n--- 完成 ---"));

await client.connect();
await client.sendInput("重构认证模块");
```

### Python 连接

```python
import asyncio
from hotplex import Client

async def main():
    client = Client("ws://localhost:8888/ws", token="<your-jwt-token>")
    await client.connect()
    await client.send_input("为 session manager 编写单元测试")

    async for event in client.events():
        if event.type == "message.delta":
            print(event.data["content"], end="")
        elif event.type == "done":
            print("\n--- 完成 ---")
            break

asyncio.run(main())
```

### Admin API

```bash
# 健康检查
curl -H "Authorization: Bearer <admin-token>" http://localhost:9999/api/v1/health

# 会话列表
curl -H "Authorization: Bearer <admin-token>" http://localhost:9999/api/v1/sessions
```

## 架构

```
┌─────────────┐  ┌──────────────┐  ┌──────────────────┐
│  Web Chat   │  │  Slack Bot   │  │  飞书 Bot        │
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
│  │              Worker 适配器                        ││
│  │  ┌──────────────┐  ┌─────────────────────────┐   ││
│  │  │ Claude Code  │  │ OpenCode Server         │   ││
│  │  │ (NDJSON/stdio)│  │ (NDJSON/stdio)         │   ││
│  │  ├──────────────┤  ├─────────────────────────┤   ││
│  │  │ ACPX         │  │ Pi-mono                 │   ││
│  │  │ (ACP/stdio)  │  │ (raw stdout)            │   ││
│  │  └──────────────┘  └─────────────────────────┘   ││
│  └──────────────────────────────────────────────────┘│
│  ┌──────────┐ ┌──────────┐ ┌──────┐ ┌─────────────┐ │
│  │ Admin API│ │ SQLite   │ │ 认证 │ │ 可观测性    │ │
│  │ (:9999)  │ │ (WAL)    │ │ JWT  │ │ OTel/Prom   │ │
│  └──────────┘ └──────────┘ └──────┘ └─────────────┘ │
└──────────────────────────────────────────────────────┘
```

## 扩展

### 新增 Worker 适配器

```go
// internal/worker/<name>/worker.go
type workerAdapter struct {
    *base.BaseWorker  // 共享生命周期：Terminate, Kill, Wait, Health, LastIO
}

func (w *workerAdapter) Start(ctx context.Context, env []string) error {
    // 1. exec.Command + PGID 进程隔离
    // 2. 建立 NDJSON stdio 通道
    // 3. 启动读/写 goroutine
    return nil
}

func (w *workerAdapter) Input(ctx context.Context, content string) error {
    return w.Conn.Send(events.Envelope{...})
}

func init() { worker.Register(worker.TypeMyWorker, New) }
```

### 新增消息平台适配器

```go
// internal/messaging/<name>/adapter.go
type Adapter struct {
    *platformadapter.PlatformAdapter  // SetHub, SetSM, SetHandler, SetBridge
}

func (a *Adapter) Start(ctx context.Context) error { ... }
func (a *Adapter) HandleTextMessage(ctx context.Context, msg *Message) error { ... }
```

## 配置

| 配置路径 | 默认值 | 说明 |
|----------|--------|------|
| `gateway.addr` | `:8888` | WebSocket 网关地址 |
| `admin.addr` | `:9999` | Admin API 地址 |
| `session.retention_period` | `168h` | 会话保留时间（7 天） |
| `session.max_concurrent` | `1000` | 最大并发会话数 |
| `pool.max_size` | `100` | 会话池大小 |
| `worker.idle_timeout` | `60m` | Worker 空闲超时 |
| `worker.execution_timeout` | `10m` | 单次执行超时 |
| `worker.max_lifetime` | `24h` | Worker 进程最大存活时间 |
| `db.path` | `data/hotplex-worker.db` | SQLite 数据库路径 |
| `log.level` | `info` | 日志级别（debug/info/warn/error） |
| `log.format` | `json` | 日志格式（dev 用 text，prod 用 json） |

完整参考：[docs/management/Config-Reference.md](docs/management/Config-Reference.md)

### 环境变量

```bash
# 必需
HOTPLEX_JWT_SECRET=<ES256 公钥或 HS256 密钥>
HOTPLEX_ADMIN_TOKEN_1=<Admin API 访问令牌>

# Slack 集成
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-...

# 飞书集成
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_...
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=...
```

## 部署

### Docker

```bash
docker build -t hotplex-worker .

# 开发环境
docker compose up -d

# 生产环境（Traefik + TLS + Let's Encrypt）
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

### Systemd

```bash
sudo bash scripts/install.sh
```

自动配置 systemd 服务、TLS、密钥轮换。

## 客户端 SDK

| 语言 | 位置 | 特性 |
|------|------|------|
| **Go** | `client/` | AEP v1 全协议、自动重连、事件驱动 |
| **TypeScript** | `examples/typescript-client/` | 流式输出、多轮对话、权限处理 |
| **Python** | `examples/python-client/` | asyncio、流式、会话恢复 |
| **Java** | `examples/java-client/` | 完整 AEP v1 实现 |

Go SDK 示例（`client/examples/`）：[快速开始](client/examples/01_quickstart) · [流式输出](client/examples/02_streaming_output) · [多轮对话](client/examples/03_multi_turn_chat) · [会话恢复](client/examples/04_session_resume) · [权限处理](client/examples/05_permission_handling) · [生产用法](client/examples/09_production)

## 开发

```bash
make build           # 构建（输出 bin/hotplex-worker-<os>-<arch>）
make test            # 测试（含 -race，超时 15m）
make test-short      # 快速测试
make lint            # golangci-lint 检查
make fmt             # gofmt + goimports
make quality         # fmt + vet + lint + test
make check           # 完整 CI 流水线
make coverage        # 覆盖率报告
```

## 文档

| 文档 | 说明 |
|------|------|
| [产品白皮书](docs/Product-Whitepaper.md) | 产品概述、核心价值、完整架构 |
| [用户手册](docs/User-Manual.md) | 安装配置、CLI、协议详解 |
| [架构设计](docs/architecture/Worker-Gateway-Design.md) | Gateway 内部设计 |
| [AEP v1 协议](docs/architecture/AEP-v1-Protocol.md) | 协议规范 |
| [消息平台架构](docs/architecture/Platform-Messaging-Architecture-Diagrams.md) | Slack/飞书集成 |
| [Admin API](docs/management/Admin-API-Design.md) | 管理 API 参考 |
| [安全设计](docs/security/Security-Authentication.md) | 认证、输入校验、SSRF 防护 |
| [测试策略](docs/testing/Testing-Strategy.md) | 测试规范与指南 |

## 项目结构

```
cmd/worker/main.go         # 入口：DI 组装、信号处理
internal/
  admin/                    # Admin API：CRUD、指标、健康检查
  aep/                      # AEP v1 编解码
  config/                   # Viper + 文件监听 + 热重载
  gateway/                  # WebSocket：Hub、Conn、Handler、Bridge
  messaging/
    slack/                  # Slack Socket Mode 适配器
    feishu/                 # 飞书 WebSocket 适配器（含 STT）
  metrics/                  # Prometheus 指标
  security/                 # JWT、SSRF 防护、命令白名单、环境隔离
  session/                  # 状态机 + SQLite 持久化
  tracing/                  # OpenTelemetry 链路追踪
  worker/
    base/                   # BaseWorker 共享生命周期
    claudecode/             # Claude Code 适配器
    opencodeserver/         # OpenCode Server 适配器
pkg/
  events/                   # AEP 事件类型和数据结构
  aep/                      # AEP v1 编解码工具
client/                     # Go 客户端 SDK（独立模块）
webchat/                    # Web Chat UI（Next.js 15 + React 19）
examples/                   # TypeScript、Python、Java SDK 示例
scripts/                    # 构建、部署、验证脚本
configs/                    # config.yaml、config-dev.yaml、env.example
docs/                       # 架构、管理、安全、测试文档
```

## 贡献

欢迎贡献！提交前请阅读[贡献指南](CONTRIBUTING.md)。

1. Fork 本仓库
2. 创建功能分支（`git checkout -b feat/my-feature`）
3. 提交变更（`git commit -m 'feat: add my feature'`）
4. 推送到分支（`git push origin feat/my-feature`）
5. 创建 Pull Request

提交前请确保 `make check` 通过。

## 安全

如发现安全漏洞，**请不要公开提 Issue**。请私下联系维护者进行负责任的披露。

## 许可证

[Apache License 2.0](LICENSE)
