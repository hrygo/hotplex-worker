---
title: 扩展 HotPlex
description: 如何为 HotPlex 添加新组件：Worker 适配器、消息平台、AEP 事件类型和 CLI 子命令
persona: contributor
difficulty: advanced
---

# 扩展 HotPlex

> 阅读本文后，你将掌握为 HotPlex 添加新组件的完整流程：Worker 适配器、消息平台、AEP 事件类型和 CLI 子命令。

## 概述

HotPlex 采用插件式架构，通过 interface + registry 模式实现组件解耦。新增组件只需实现对应接口、完成注册，无需修改核心逻辑。本文覆盖四种最常见的扩展场景。

## 前提条件

- 已完成[开发环境搭建](development-setup.md)
- 已阅读[架构概览](architecture.md)
- 理解 Go interface 和 embedding 模式

## 场景一：添加新 Worker 适配器

Worker 适配器负责与 AI Agent 运行时通信。所有 Worker 实现 `worker.Worker` 接口并嵌入 `base.BaseWorker`。

### 文件结构

```
internal/worker/<name>/
├── worker.go          # 主适配器实现
├── worker_test.go     # 单元测试
└── ...
```

### 实现步骤

#### 1. 创建适配器结构体

```go
package myworker

import (
    "context"
    "sync"

    "github.com/hrygo/hotplex/internal/worker"
    "github.com/hrygo/hotplex/internal/worker/base"
)

// 编译时接口检查
var _ worker.Worker = (*Worker)(nil)

// Worker 实现 myworker 适配器
type Worker struct {
    *base.BaseWorker  // 共享生命周期：Terminate/Kill/Wait/Health/LastIO/Conn
    mu sync.Mutex
}
```

#### 2. 实现 Worker 接口

```go
// Type 返回 Worker 类型标识
func (w *Worker) Type() worker.WorkerType {
    return worker.WorkerType("myworker")
}

// SupportsResume 标识是否支持恢复会话
func (w *Worker) SupportsResume() bool { return true }

// SupportsStreaming 标识是否支持流式输出
func (w *Worker) SupportsStreaming() bool { return true }

// SupportsTools 标识是否支持工具调用
func (w *Worker) SupportsTools() bool { return true }

// Start 启动 Worker 运行时
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // 1. 构建启动命令
    // 2. 启动子进程（使用 proc 包确保跨平台隔离）
    // 3. 建立 SessionConn
    return nil
}

// Input 投递用户消息
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // 通过 SessionConn 发送消息
    return nil
}

// Resume 恢复已有会话
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // 重连到已有会话
    return nil
}
```

#### 3. 注册 Worker

使用 `init()` + `worker.Register()` 模式：

```go
func New(log *slog.Logger, cfg *config.Config) *Worker {
    return &Worker{BaseWorker: base.NewBaseWorker(log, cfg)}
}

func init() {
    worker.Register(worker.WorkerType("myworker"), func() (worker.Worker, error) {
        return New(slog.Default(), nil), nil
    })
}
```

#### 4. 确保 blank import

在 `cmd/hotplex/main.go` 中添加 blank import：

```go
import (
    _ "github.com/hrygo/hotplex/internal/worker/myworker"
)
```

#### 5. 编写测试

```go
// worker_test.go
package myworker

import (
    "testing"
    "github.com/stretchr/testify/require"
)

func TestWorker_Type(t *testing.T) {
    t.Parallel()
    w := New(slog.Default(), nil)
    require.Equal(t, worker.WorkerType("myworker"), w.Type())
}
```

> **完整的分步实现指南**（包含 Start/Input/Resume 的详细实现、输出读取循环、进程隔离等）请参考 [添加 Worker 适配器](adding-worker.md)。

### 关键注意事项

- **进程隔离**：使用 `internal/worker/proc` 包启动子进程，确保 PGID (POSIX) 或 Job Object (Windows) 隔离
- **分层终止**：遵循 SIGTERM → 等待 5s → SIGKILL 的终止顺序
- **跨平台**：使用 `filepath.Join()`、`os.TempDir()`，不硬编码路径分隔符
- **Mutex**：显式 `mu` 字段，不嵌入 `sync.Mutex`

## 场景二：添加新消息平台适配器

消息平台适配器负责与 Slack、飞书等平台通信。所有适配器实现 `PlatformAdapterInterface` 接口并嵌入 `PlatformAdapter`。

### 文件结构

```
internal/messaging/<name>/
├── adapter.go         # 主适配器实现
├── adapter_test.go    # 单元测试
└── ...
```

### 实现步骤

#### 1. 创建适配器结构体

```go
package myplatform

import (
    "context"
    "github.com/hrygo/hotplex/internal/messaging"
)

type Adapter struct {
    messaging.BaseAdapter[*MyPlatformConn]  // 嵌入获取连接池
    // 平台特有字段
}
```

#### 2. 实现 PlatformAdapterInterface

实现 `PlatformAdapterInterface` 的 6 个方法：`Platform()`、`Start()`、`HandleTextMessage()`、`Close()`、`ConfigureWith(AdapterConfig)`、`GetBotID()`。

#### 3. 2 步初始化

在 `cmd/hotplex/messaging_init.go` 中注册初始化流程：

```go
// 1. 统一注入依赖
adapter.ConfigureWith(messaging.AdapterConfig{
    Hub: hub, SM: sm, Handler: handler, Bridge: bridge, Gate: gate,
    Extras: map[string]any{"token": token},
})
// 2. 启动连接
adapter.Start(ctx)
```

#### 4. 结果投递

在 `internal/cron/delivery.go` 中添加新平台的投递逻辑，将 cron 任务执行结果回传到该平台。

> **完整的分步实现指南**（包含 PlatformConn 实现、消息去重、SDK 集成、测试等）请参考 [添加消息平台适配器](adding-messaging-adapter.md)。

## 场景三：添加新 AEP 事件类型

AEP v1 是 HotPlex 的统一通信协议。添加新事件类型需要修改 `pkg/events/events.go`。

### 实现步骤

#### 1. 添加 Kind 常量

```go
// pkg/events/events.go

const (
    // ... 已有事件类型
    MyNewEvent Kind = "my_new_event"  // 添加新类型
)
```

#### 2. 添加 Event Data 结构体（如需要）

```go
// 如果新事件有自定义数据结构
type MyNewEventData struct {
    Field1 string `json:"field1"`
    Field2 int    `json:"field2"`
}
```

#### 3. 更新编解码验证

在 `pkg/aep/` 中更新编解码逻辑（如需对新类型做特殊验证）。

#### 4. 更新 Handler

在 `internal/gateway/handler.go` 中添加对新事件类型的处理逻辑：

```go
func (h *Handler) handle(ctx context.Context, env *events.Envelope) error {
    switch env.Event.Type {
    // ... 已有 case
    case events.MyNewEvent:
        return h.handleMyNewEvent(ctx, env)
    }
}
```

#### 5. 更新背压策略

在 `internal/gateway/hub.go` 的 `isDroppable` 函数中决定新事件类型是否可丢弃：

```go
func isDroppable(kind events.Kind) bool {
    switch kind {
    case events.MessageDelta, events.Raw:
        return true
    // MyNewEvent 默认不可丢弃（关键事件）
    default:
        return false
    }
}
```

#### 6. 编写测试

```go
func TestMyNewEvent_Kind(t *testing.T) {
    t.Parallel()
    require.Equal(t, events.Kind("my_new_event"), events.MyNewEvent)
}
```

### 关键注意事项

- Kind 命名使用 `snake_case`（如 `permission_request`）
- C→S（Client → Server）事件需在 Handler 中处理
- S→C（Server → Client）事件需在 Bridge/Hub 中生成
- 所有事件必须有文档化的语义和时间约束

## 场景四：添加新 CLI 子命令

HotPlex 使用 Cobra CLI 框架，子命令定义在 `cmd/hotplex/` 目录。

### 文件结构

```
cmd/hotplex/
├── main.go            # 根命令
├── <name>.go          # 新子命令
└── <name>_test.go     # 测试
```

### 实现步骤

#### 1. 创建命令文件

```go
// cmd/hotplex/mycmd.go
package main

import (
    "github.com/spf13/cobra"
)

func newMyCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "mycmd",
        Short: "Short description",
        Long:  "Long description",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 命令逻辑
            return nil
        },
    }

    // 添加 flag
    cmd.Flags().String("name", "", "description")

    // 添加子命令
    cmd.AddCommand(newMySubCmd())

    return cmd
}
```

#### 2. 注册到根命令

在 `cmd/hotplex/main.go` 的 `init()` 函数中添加注册：

```go
func init() {
    rootCmd.AddCommand(newMyCmd())
}
```

#### 3. 编写测试

```go
// cmd/hotplex/mycmd_test.go
package main

import (
    "testing"
    "github.com/stretchr/testify/require"
)

func TestMyCmd_Execute(t *testing.T) {
    t.Parallel()
    cmd := newMyCmd()
    cmd.SetArgs([]string{"--name", "test"})
    require.NoError(t, cmd.Execute())
}
```

## 通用注意事项

### 跨平台兼容

所有新组件必须兼容 Linux、macOS、Windows：

- 路径：使用 `filepath.Join()`、`filepath.Dir()`、`filepath.Base()`
- 临时目录：使用 `os.TempDir()`
- 用户主目录：使用 `os.UserHomeDir()`
- 进程终止：使用 `process.Kill()`（通过 proc 包）
- 平台特有逻辑：使用 `*_unix.go` / `*_windows.go` build tags

### 代码规范

- **Mutex**：显式 `mu` 字段，不嵌入，不传指针
- **错误**：`Err` 前缀（哨兵）、`Error` 后缀（自定义）、`fmt.Errorf("%w")` 包装
- **日志**：使用 `log/slog` JSON handler
- **测试**：`testify/require`、表驱动、`t.Parallel()`

### DI 注入

项目禁止 wire/dig，全部手动构造函数注入。通过 `GatewayDeps` 结构传递依赖。

## 验证

添加新组件后，运行完整检查：

```bash
# 1. 确保编译通过
make build

# 2. 运行完整测试
make test

# 3. 运行 lint
make lint

# 4. 完整 CI 等价检查
make check
```

验证清单：

- [ ] 新组件有对应测试
- [ ] 通过 race 检测
- [ ] 跨平台兼容（至少确保无硬编码路径分隔符）
- [ ] 注册到 registry（Worker 适配器）
- [ ] blank import 已添加（Worker 适配器）
- [ ] 遵循项目代码规范

## 下一步

- [架构概览](architecture.md) — 理解新组件在系统中的位置
- [测试指南](testing-guide.md) — 为新组件编写符合规范的测试
- [PR 工作流](pr-workflow.md) — 提交新组件的 PR
