# HotPlex 架构详解

本文档提供 HotPlex 项目架构的详细说明，帮助理解多 channel、多 worker、跨平台兼容等特性。

## 多 Channel 支持

HotPlex 支持多种消息 channel，每个 channel 有特定的适配器和特性。

### Slack Socket Mode

**目录**：`internal/messaging/slack/`

**特性**：
- 流式消息（SlackStreamingWriter）
- 打字提示（`typing...` indicator）
- 交互按钮（Button、Select、Static Select）
- Socket Mode 长连接（WebSocket）
- 消息分片（chunker）和去重（dedup）
- 速率限制（rate limiter）

**关键文件**：
- `adapter.go` - Slack 适配器主逻辑
- `streaming_writer.go` - 流式消息写入器
- `chunker.go` - 长消息分片
- `dedup.go` - 消息去重
- `interaction.go` - 交互按钮处理

**在 PR 中说明影响的 channel**：
```markdown
### Changes
- 修复 Slack 流式消息丢包问题
```

### 飞书 WebSocket (P2)

**目录**：`internal/messaging/feishu/`

**特性**：
- 流式卡片（Card Kit）
- STT 语音转文字（飞书云端 STT 或本地 SenseVoice）
- 交互卡片（Button、Select、Input）
- WebSocket 长连接（ws.Client）
- 打字提示（`i_am_typing` event）

**关键文件**：
- `adapter.go` - 飞书适配器主逻辑
- `streaming.go` - 流式卡片渲染
- `stt.go` - STT 语音转文字
- `interaction.go` - 交互卡片处理

**在 PR 中说明影响的 channel**：
```markdown
### Changes
- 优化飞书流式卡片渲染
```

### WebChat

**目录**：`webchat/` + `internal/gateway/`

**特性**：
- HTTP/SSE（Server-Sent Events）
- Session 粘性（localStorage 持久化）
- Next.js 前端（React）
- AI SDK transport 适配

**关键文件**：
- `webchat/app/page.tsx` - WebChat 主页面
- `internal/gateway/api.go` - Gateway HTTP API
- `internal/session/key.go` - Session key 派生（UUIDv5）

**在 PR 中说明影响的 channel**：
```markdown
### Changes
- 修复 WebChat session 粘性问题
```

## 多 Worker 支持

HotPlex 支持多种 AI worker 运行时，每个 worker 有特定的适配器和特性。

### Claude Code (CC)

**目录**：`internal/worker/claudecode/`

**特性**：
- Claude Code CLI 适配器
- `--append-system-prompt` Agent 配置注入
- stdin/stdout NDJSON 通信
- Session 池化

**关键文件**：
- `worker.go` - CC worker 适配器
- `base/worker.go` - BaseWorker 共享逻辑
- `base/conn.go` - NDJSON over stdio

**在 PR 中说明影响的 worker**：
```markdown
### Changes
- 优化 CC Agent 配置注入
```

### OpenCode Server (OCS)

**目录**：`internal/worker/opencodeserver/`

**特性**：
- 单例进程管理器（SingletonProcessManager）
- SSE 长连接（无 Timeout 的 sseClient）
- Session 池化（30m 空闲回收）
- Context cancellation（优雅关闭）

**关键文件**：
- `singleton.go` - 单例进程管理器
- `worker.go` - OCS worker 适配器
- `worker_test.go` - 单元测试

**在 PR 中说明影响的 worker**：
```markdown
### Changes
- 修复 OCS SSE timeout 问题
```

### Pi-mono

**目录**：`internal/worker/pi/`

**特性**：
- Pi-mono CLI 适配器
- stdin/stdout 通信

**关键文件**：
- `worker.go` - Pi worker 适配器

**在 PR 中说明影响的 worker**：
```markdown
### Changes
- 修复 Pi worker 启动问题
```

## 跨平台兼容

HotPlex 支持三大平台，CI 必须在所有平台通过。

### 平台列表

| 平台 | CI | 系统服务 | 特殊注意 |
|------|-----|----------|----------|
| **Linux** | GitHub Actions | systemd | 主要开发平台 |
| **macOS** | GitHub Actions | launchd | SIP 保护 `/System` |
| **Windows** | GitHub Actions | SCM | 无 POSIX 信号 |

### 跨平台注意事项

#### 路径分隔符

```go
// ✅ 正确（跨平台）
path := filepath.Join("dir", "file")
path := filepath.Dir("/path/to/file")
path := filepath.Base("/path/to/file")

// ❌ 错误（硬编码分隔符）
path := "dir/file"      // 仅 Unix
path := "dir\\file"     // 仅 Windows
```

#### 文件权限

```go
// ✅ 正确（跨平台）
err := os.MkdirAll(dir, 0755)
err := os.WriteFile(file, data, 0644)

// ❌ 错误（平台特定权限）
err := os.MkdirAll(dir, 0700)  // macOS SIP 保护问题
```

#### 进程管理

```go
// ✅ 正确（跨平台）
err := process.Kill(p)
err := p.Terminate()

// ❌ 错误（仅 POSIX）
err := syscall.Kill(p.Pid, syscall.SIGTERM)
syscall.Kill(p.Pid, syscall.SIGKILL)
```

#### 平台特定代码

使用 build tags 分离平台特定代码：

```go
//go:build darwin || linux
// +build darwin linux

package foo

import "syscall"

func setProcessGroup() (*syscall.Process, error) {
    return syscall.StartProcess(...)
}
```

```go
//go:build windows
// +build windows

package foo

import "syscall"

func setProcessGroup() (*syscall.Process, error) {
    return syscall.CreateProcess(...)
}
```

#### 系统服务

```go
// ✅ 正确（跨平台）
import "github.com/hrygo/hotplex/internal/service"

// service.Install() 自动选择平台特定实现
// - Linux: systemd
// - macOS: launchd
// - Windows: SCM
```

### 跨平台测试

```bash
# CI 会自动在 Linux/macOS/Windows 三平台运行
# 本地测试可以使用 build tags

# 仅测试 Unix 平台
go test -tags=unix ./...

# 仅测试 Windows 平台
go test -tags=windows ./...

# 测试特定平台
go test -tags=linux ./...
go test -tags=darwin ./...
```

### 在 PR 中说明平台影响

```markdown
### Changes

**架构影响**：
- 平台: Linux / macOS / Windows / 跨平台

## Test Plan

- [x] make test - Linux/macOS/Windows CI 通过
- [ ] 平台特定测试: 描述测试步骤
```

## 架构组件关系

```
HotPlex Gateway
├── Gateway Core (internal/gateway/)
│   ├── Hub (WebSocket 广播)
│   ├── Session Manager (5 状态机)
│   └── Bridge (session ↔ worker 编排)
│
├── Channel Adapters (internal/messaging/)
│   ├── Slack Socket Mode
│   ├── 飞书 WebSocket (P2)
│   └── WebChat (HTTP/SSE)
│
├── Worker Adapters (internal/worker/)
│   ├── Claude Code (CC)
│   ├── OpenCode Server (OCS)
│   └── Pi-mono
│
└── Cross-Platform Support
    ├── Linux (systemd)
    ├── macOS (launchd)
    └── Windows (SCM)
```

## Commit Scope 推断规则

根据修改的文件路径自动推断 commit scope：

| 修改路径 | Scope | 说明 |
|---------|-------|------|
| `internal/gateway/*` | `gateway` | Gateway 核心 |
| `internal/session/*` | `session` | Session 管理 |
| `internal/messaging/slack/*` | `messaging/slack` | Slack 适配器 |
| `internal/messaging/feishu/*` | `messaging/feishu` | 飞书适配器 |
| `webchat/*` | `webchat` | WebChat 前端 |
| `internal/worker/claudecode/*` | `worker/cc` | CC worker |
| `internal/worker/opencodeserver/*` | `worker/ocs` | OCS worker |
| `internal/worker/pi/*` | `worker/pi` | Pi worker |
| `internal/service/*` | `service` | 系统服务（跨平台） |
| `internal/cli/*` | `cli` | CLI 命令 |
| `internal/security/*` | `security` | 安全 |
| `cmd/hotplex/*` | `cli` | CLI 命令 |
| `Makefile`、`build*` | `build` | 构建系统 |

## 常见架构相关 PR 示例

### 示例 1：Channel 相关

```
fix(messaging/slack): resolve streaming message loss

修复 Slack 流式消息在长消息时丢失部分内容的问题。

使用 chunker 分片长消息，确保每个分片都正确发送。

Fixes #123

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 示例 2：Worker 相关

```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 示例 3：跨平台相关

```
fix(service): Windows service installation fails

修复 Windows 系统服务安装时的权限问题。

使用 `openFile` 替代 `os.MkdirAll` 以正确处理 Windows ACL。

Fixes #456

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 示例 4：多组件影响

```
fix(gateway): resolve session timeout across all channels

修复 session timeout 问题，影响所有 channel 适配器。

**架构影响**：
- Channel: All (Slack / 飞书 / WebChat)
- Worker: N/A
- 平台: 跨平台

Fixes #789

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```
