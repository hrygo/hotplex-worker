---
title: 测试指南
weight: 36
description: HotPlex 项目的测试策略、约定和最佳实践
persona: contributor
difficulty: beginner
---

# 测试指南

> 阅读本文后，你将掌握 HotPlex 的测试约定、断言模式、表驱动写法，能够为贡献代码编写符合规范的测试。

## 概述

HotPlex 是高并发 Go 系统，测试覆盖从纯逻辑单元测试到平台集成测试。项目使用 `testify/require` 做断言，表驱动测试覆盖多场景，`t.Parallel()` 加速执行，`-race` 标志检测竞态条件。

## 前提条件

- 已完成[开发环境搭建](development-setup.md)
- 了解 Go 测试基础（`testing.T`、`go test`）
- 了解 [testify](https://github.com/stretchr/testify) 断言库

## 测试约定

### 断言库：testify/require

项目统一使用 `testify/require` 做断言。`require` 在断言失败时立即终止测试（`t.FailNow`），适合前置条件检查。对于需要收集多个错误的场景，使用 `testify/assert`。

```go
import (
    "testing"
    "github.com/stretchr/testify/require"
    "github.com/stretchr/testify/assert"
)

func TestDeriveSessionKey(t *testing.T) {
    key := DeriveSessionKey("U123", worker.TypeClaudeCode, "C456:ts123", "/tmp/workdir")
    require.NotEmpty(t, key, "session key should not be empty")

    // 相同输入应产生相同 key
    key2 := DeriveSessionKey("U123", worker.TypeClaudeCode, "C456:ts123", "/tmp/workdir")
    assert.Equal(t, key, key2, "same input should produce same key")
}
```

### 表驱动测试

所有多场景测试必须使用表驱动模式：

```go
func TestIsDroppable(t *testing.T) {
    t.Parallel()

    tests := map[string]struct {
        kind     events.Kind
        expected bool
    }{
        "delta is droppable":     {kind: events.MessageDelta, expected: true},
        "raw is droppable":       {kind: events.Raw, expected: true},
        "state is not droppable": {kind: events.State, expected: false},
        "done is not droppable":  {kind: events.Done, expected: false},
        "error is not droppable": {kind: events.Error, expected: false},
    }

    for name, tc := range tests {
        t.Run(name, func(t *testing.T) {
            t.Parallel()
            got := isDroppable(tc.kind)
            require.Equal(t, tc.expected, got)
        })
    }
}
```

关键要点：
- 使用 `map[string]struct{}` 定义测试用例，名称即描述
- 顶层 `t.Parallel()` 允许包级并行
- 每个子测试内 `t.Parallel()` 允许用例级并行

### 并行测试

所有独立测试必须调用 `t.Parallel()`：

```go
func TestValidateWorkDir(t *testing.T) {
    t.Parallel()  // 包级并行

    t.Run("valid path", func(t *testing.T) {
        t.Parallel()  // 用例级并行
        // ...
    })
}
```

### 测试文件命名

- 单元测试：`<source>_test.go`（与源文件同目录）
- E2E 测试：`e2e_test.go`（使用 build tags 隔离：`//go:build slack_e2e`）
- 集成测试：`integration_test.go`

### 错误哨兵测试

错误哨兵用 `errors.Is` 比较，不用 `==`：

```go
func TestStore_NotFound(t *testing.T) {
    t.Parallel()

    _, err := store.Get(ctx, "nonexistent")
    require.Error(t, err)
    assert.True(t, errors.Is(err, session.ErrSessionNotFound))
}
```

## 步骤

### 运行测试

```bash
# 完整测试（含 -race，15 分钟超时）
make test

# 快速测试（-short 标志，5 分钟超时）
make test-short

# 运行特定包
go test -race ./internal/session/...

# 运行特定测试函数
go test -race -run TestHub_SendToSession ./internal/gateway/...

# 带 verbose 输出
go test -race -v -run TestManager_Transition ./internal/session/...

# 生成覆盖率报告
make coverage

# 生成 HTML 覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 编写新测试

1. 在源文件同目录创建 `<source>_test.go`
2. 使用表驱动模式覆盖多个场景
3. 调用 `t.Parallel()`
4. 使用 `testify/require` 做断言
5. 确保通过 race 检测

示例：

```go
package session

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestValidTransitions(t *testing.T) {
    t.Parallel()

    tests := map[string]struct {
        from    events.SessionState
        to      events.SessionState
        valid   bool
    }{
        "created to running":    {from: events.StateCreated, to: events.StateRunning, valid: true},
        "running to idle":       {from: events.StateRunning, to: events.StateIdle, valid: true},
        "running to running":    {from: events.StateRunning, to: events.StateRunning, valid: false},
        "deleted to running":    {from: events.StateDeleted, to: events.StateRunning, valid: false},
    }

    for name, tc := range tests {
        t.Run(name, func(t *testing.T) {
            t.Parallel()
            got := IsValidTransition(tc.from, tc.to)
            require.Equal(t, tc.valid, got)
        })
    }
}
```

## Mock 模式

### 不 Mock 核心安全逻辑

以下功能**禁止 Mock**，必须真实测试：

| 核心功能 | 原因 | 测试方式 |
|----------|------|----------|
| `os/exec` + PGID | 进程隔离是核心价值 | 真实子进程集成测试 |
| Session Pool 并发 | 竞态条件检测 | 真实并发测试 + `-race` |
| 安全正则检测器 | WAF 规则正确性 | 真实输入验证 |

### 接口 Mock

对依赖的外部接口使用接口 Mock：

```go
// 定义接口
type Store interface {
    Get(ctx context.Context, id string) (*SessionInfo, error)
    Save(ctx context.Context, info *SessionInfo) error
}

// Mock 实现
type mockStore struct {
    getFunc func(ctx context.Context, id string) (*SessionInfo, error)
    saveFunc func(ctx context.Context, info *SessionInfo) error
}

func (m *mockStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
    return m.getFunc(ctx, id)
}

func (m *mockStore) Save(ctx context.Context, info *SessionInfo) error {
    return m.saveFunc(ctx, info)
}
```

项目测试中普遍使用上述函数字段 mock 模式（如 `getFunc`/`saveFunc`），而非独立的 mock 包。

### 内嵌 httptest

WebSocket 测试使用 `net/http/httptest` 创建 mock server：

```go
func TestConn_ReadPump(t *testing.T) {
    t.Parallel()

    // 创建 mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // WebSocket upgrade logic
    }))
    defer server.Close()

    // 连接并测试
    ws := connectWS(t, server)
    defer ws.Close()
    // ...
}
```

## 覆盖率目标

各模块按可测试性设置分级覆盖率阈值：

| 模块 | 目标 | 理由 |
|------|------|------|
| `internal/security/` | 85%+ | 安全核心，边界条件必须覆盖 |
| `pkg/events/`, `pkg/aep/` | 85%+ | 协议编解码，纯逻辑 |
| `internal/gateway/` | 75%+ | WebSocket 核心引擎 |
| `internal/session/` | 70%+ | 状态机 + SQLite 持久化 |
| `internal/config/` | 70%+ | Viper 配置解析与热重载 |
| `internal/worker/` (registry) | 70%+ | Worker 注册与调度 |
| `internal/worker/claudecode/` | 60%+ | 外部进程适配器 |
| `internal/messaging/` | 50%+ | 平台适配层，SDK 依赖重 |
| `internal/worker/proc/` | 排除 | 通过集成测试覆盖 |
| `cmd/hotplex/` | 排除 | DI 组装，非单元测试目标 |

## Race 检测

HotPlex 是高并发系统，**所有测试必须通过 race 检测**。

```bash
# 完整测试（含 race 检测）
make test

# 快速测试（含 race 检测）
make test-short

# 手动运行
GORACE="history_size=5" go test -race -timeout 15m ./...
```

`GORACE="history_size=5"` 增加 race detector 历史缓冲区大小，提高检测灵敏度。

## 验证

提交 PR 前对照以下检查清单：

- [ ] 新功能有对应的单元测试
- [ ] 使用表驱动模式覆盖多个场景
- [ ] 调用了 `t.Parallel()`
- [ ] 使用 `testify/require` 做断言
- [ ] 没有对核心安全逻辑使用 Mock
- [ ] 测试通过 race 检测（`make test`）
- [ ] 覆盖率满足模块级阈值
- [ ] 测试名称清晰描述测试场景

运行完整验证：

```bash
make check  # fmt + lint + test + build
```

## 下一步

- [PR 工作流](pr-workflow.md) — 了解如何提交 PR
- [架构概览](architecture.md) — 加深对测试对象的理解
- [扩展指南](extending.md) — 为新组件编写测试
