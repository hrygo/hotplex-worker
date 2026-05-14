---
title: 开发环境搭建
weight: 32
description: 从零开始搭建 HotPlex Gateway 开发环境，10 分钟完成
---

# 开发环境搭建

> 阅读本文后，你将拥有一套完整的 HotPlex 开发环境，能够构建、测试和运行网关。

## 概述

HotPlex Gateway 基于 Go 1.26 构建，使用 Make 作为构建入口，依赖 golangci-lint 做代码质量检查。开发环境搭建分为三个阶段：工具链安装、项目初始化、开发服务启动。

## 前提条件

### 必需工具

| 工具 | 最低版本 | 安装方式 |
|------|---------|---------|
| Go | 1.26+ | [go.dev/dl](https://go.dev/dl/) 或 `brew install go` |
| golangci-lint | v1.64+ | `brew install golangci-lint` 或 `go install github.com/golangci-lint/golangci-lint/cmd/golangci-lint@latest` |
| goimports | latest | `go install golang.org/x/tools/cmd/goimports@latest` |
| make | any | macOS 自带 / Linux `apt install build-essential` |
| git | 2.30+ | 系统包管理器 |

### 可选工具

| 工具 | 用途 | 安装方式 |
|------|------|---------|
| Node.js / pnpm | WebChat 前端开发 | `npm install -g pnpm` |
| gh | PR 创建、Issue 管理 | `brew install gh` |
| SQLite3 | 数据库调试 | `brew install sqlite` |
| delve | Go 调试器 | `go install github.com/go-delve/delve/cmd/dlv@latest` |

### 验证工具链

```bash
make check-tools
```

输出应显示：

```
  ✓ Go
  ✓ golangci-lint
  ✓ goimports
```

## 步骤

### 1. 克隆仓库

**Admin（有仓库权限）**：

```bash
git clone git@github.com:hrygo/hotplex.git
cd hotplex
```

**外部贡献者**：

```bash
# 先在 GitHub 上 fork 仓库
git clone https://github.com/<your-username>/hotplex.git
cd hotplex
git remote add upstream https://github.com/hrygo/hotplex.git
```

### 2. 安装 Git Hooks

项目提供 pre-push hook，推送前自动运行格式化、lint、构建和测试：

```bash
make hooks
```

hook 流程：`fmt → lint → vet → mod verify → build → test`。

### 3. 配置环境变量

```bash
cp configs/env.example .env
```

**必填项**（开发阶段至少需要）：

```bash
# JWT 签名密钥（ES256）
HOTPLEX_JWT_SECRET=$(openssl rand -base64 32 | tr -d '\n')

# Admin API Token
HOTPLEX_ADMIN_TOKEN_1=$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)
```

**消息平台（按需配置）**：

- Slack：`HOTPLEX_MESSAGING_SLACK_BOT_TOKEN`、`HOTPLEX_MESSAGING_SLACK_APP_TOKEN`
- 飞书：`HOTPLEX_MESSAGING_FEISHU_APP_ID`、`HOTPLEX_MESSAGING_FEISHU_APP_SECRET`

不配置消息平台也能启动网关，WebSocket 和 Web Chat 功能独立可用。

### 4. 一键初始化

```bash
make quickstart
```

此命令依次执行 `hooks → check-tools → build → test-short`。成功后输出：

```
  ✓ Developer setup complete

    make dev      Start dev environment
    make run      Run gateway
    make help     Show all commands
```

### 5. 启动开发环境

```bash
make dev
```

启动两个服务：

| 服务 | 地址 | 说明 |
|------|------|------|
| Gateway | localhost:8888 | WebSocket 网关（使用 `configs/config-dev.yaml`） |
| Webchat | localhost:3000 | Web Chat UI（需要 pnpm） |

> Webchat 首次启动会自动构建 Next.js 并嵌入 Go 二进制（`make webchat-embed`）。如果跳过前端开发，只启动网关即可：`make gateway-start`。

## 验证

### 确认服务运行

```bash
make dev-status
```

### 运行完整 CI 流程

```bash
make check
```

如果输出 `✓ CI check passed`，开发环境搭建完成。

### 查看日志

```bash
make dev-logs       # 所有服务日志
make gateway-logs   # 仅网关日志
```

日志文件位于 `./logs/hotplex.log`，使用 JSON 格式（`log/slog`）。

## 开发工作流

### 日常循环

```bash
# 1. 编辑代码
# 2. 快速测试
make test-short

# 3. 提交前完整检查（等同于 CI）
make check
```

`make check` 执行流水线：`fmt → lint → test → build`。

### 命令速查

```bash
# 构建
make build          # 构建网关二进制（输出到 bin/）
make run            # 构建并前台运行
make clean          # 清理构建产物

# 测试
make test           # 完整测试（含 -race，15 分钟超时）
make test-short     # 快速测试（-short，5 分钟超时）
make coverage       # 生成覆盖率报告

# 质量
make fmt            # go fmt + goimports
make lint           # golangci-lint 检查
make quality        # fmt + lint + test
make check          # quality + build（CI 等价）

# 开发环境
make dev-start      # 启动所有服务（推荐使用 make dev）
make dev-stop       # 停止所有服务
make dev-reset      # 停止并重启
make dev-status     # 查看运行状态
```

### 运行特定测试

```bash
# 单个包
go test -race -run TestSessionTransition ./internal/session/...

# 带 verbose 输出
go test -race -v -run TestDeriveSessionKey ./internal/session/...
```

### 查看实时日志

```bash
tail -f logs/hotplex.log | jq .

# 过滤特定 session
cat logs/hotplex.log | jq 'select(.session_id == "xxx")'
```

## IDE 配置

### VS Code

安装扩展：

- **Go**（golang.go）— 代码补全、跳转、调试

推荐 `settings.json`：

```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.testFlags": ["-race"],
  "go.testTimeout": "120s",
  "[go]": {
    "editor.formatOnSave": true,
    "editor.defaultFormatter": "golang.go"
  },
  "go.formatTool": "goimports",
  "go.goimportsLocalPrefix": "github.com/hrygo/hotplex"
}
```

### GoLand

- 启用 Go module 支持（Settings → Go → Go Modules）
- 配置 File Watcher 运行 `goimports` on save
- 测试配置中勾选 **Run with race detector**

### 调试 Gateway

```bash
# 方式 1：使用 delve
dlv debug ./cmd/hotplex -- gateway start -c configs/config-dev.yaml

# 方式 2：前台运行
make run

# 方式 3：attach 到运行中的进程
dlv attach $(pgrep -f "hotplex.*gateway")
```

## 故障排查

| 问题 | 解决方案 |
|------|---------|
| `go.mod requires go >= 1.26` | 升级 Go 到 1.26+ |
| `⚠ golangci-lint (missing)` | `brew install golangci-lint` |
| `webchat-embed needs pnpm` | `npm install -g pnpm && cd webchat && pnpm install` |
| `bind: address already in use` | `lsof -i :8888` 检查并停止占用进程 |
| `⚠ goimports (missing)` | `go install golang.org/x/tools/cmd/goimports@latest` |

## 下一步

- [架构概览](architecture.md) — 了解 HotPlex 核心模块和数据流
- [测试指南](testing-guide.md) — 学习项目的测试约定和模式
- [PR 工作流](pr-workflow.md) — 了解如何提交贡献
- [扩展指南](extending.md) — 学习如何添加新组件
