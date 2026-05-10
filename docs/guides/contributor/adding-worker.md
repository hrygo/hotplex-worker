---
title: 添加 Worker 适配器
description: 如何为 HotPlex Gateway 开发新的 AI Coding Agent Worker 适配器
persona: contributor
difficulty: advanced
---

# 添加 Worker 适配器

本文介绍如何为 HotPlex Gateway 添加一个新的 Worker 适配器。Worker 适配器负责与具体的 AI Agent CLI（如 Claude Code、OpenCode Server）通信，是 Gateway 与 Agent 运行时之间的桥梁。

## 架构概览

```
Gateway (WebSocket Hub)
    │
    ▼
worker.Worker interface       ← 你需要实现这个
    │
    ├── base.BaseWorker       ← 嵌入获取共享生命周期方法
    │       └── proc.Manager  ← 跨平台进程管理 (PGID/Job Object)
    │
    └── base.Conn             ← stdin/stdout 双向通信 (NDJSON)
            ├── Send()        → stdin 写入
            ├── Recv()        ← goroutine 填充的 channel
            └── TrySend()     ← 非阻塞写入 recvCh
```

## 接口清单

### Worker 接口（必须实现）

定义在 `internal/worker/worker.go`。它内嵌了 `Capabilities` 接口，共分两部分。

**Capabilities** -- 声明 Worker 的能力，供 Gateway 路由和调度参考：

| 方法 | 返回值 | 说明 |
|------|--------|------|
| `Type()` | `WorkerType` | 类型标识符，如 `"my_agent"` |
| `SupportsResume()` | `bool` | 是否支持恢复已有会话 |
| `SupportsStreaming()` | `bool` | 是否输出流式 delta 事件 |
| `SupportsTools()` | `bool` | 是否暴露工具调用能力 |
| `EnvBlocklist()` | `[]string` | 不传递给子进程的环境变量名前缀 |
| `SessionStoreDir()` | `string` | 会话状态持久化目录，空串表示不持久化 |
| `MaxTurns()` | `int` | 每会话最大轮次，0 表示无限 |
| `Modalities()` | `[]string` | 支持的内容模态（如 `"text"`, `"code"`, `"image"`） |

**生命周期方法** -- Gateway 通过这些方法控制 Worker 进程：

| 方法 | 签名 | 说明 |
|------|------|------|
| `Start` | `(ctx, SessionInfo) error` | 启动运行时进程，阻塞直到就绪 |
| `Input` | `(ctx, content string, metadata map[string]any) error` | 向运行时发送用户输入 |
| `Resume` | `(ctx, SessionInfo) error` | 恢复已有会话 |
| `Terminate` | `(ctx) error` | 优雅终止（SIGTERM → 5s → SIGKILL） |
| `Kill` | `() error` | 强制终止 SIGKILL |
| `Wait` | `() (int, error)` | 阻塞等待退出，返回 exit code |
| `Conn` | `() SessionConn` | 返回数据平面连接 |
| `Health` | `() WorkerHealth` | 返回健康快照 |
| `LastIO` | `() time.Time` | 最后 I/O 时间（GC 僵尸检测） |
| `ResetContext` | `(ctx) error` | 清除运行时上下文 |

### SessionConn 接口（数据平面）

定义在 `internal/worker/worker.go`。用于 Gateway 与 Worker 运行时之间的双向消息传递：

```go
type SessionConn interface {
    Send(ctx context.Context, msg *events.Envelope) error
    Recv() <-chan *events.Envelope
    Close() error
    UserID() string
    SessionID() string
}
```

如果使用 stdin/stdout 通信，`base.Conn` 已经完整实现了此接口。

### 可选接口

根据 Agent 能力选择性实现，Bridge 通过 type assertion 检测：

| 接口 | 方法 | 用途 |
|------|------|------|
| `SessionFileChecker` | `HasSessionFiles(sessionID) bool` | resume 前检查磁盘会话文件是否存在 |
| `InputRecoverer` | `LastInput() string` | 缓存最后输入用于崩溃恢复 |
| `InPlaceReseter` | `InPlaceReset() bool` | 标记 ResetContext 是否原地重置 |
| `WorkerSessionIDHandler` | `SetWorkerSessionID` / `GetWorkerSessionID` | 管理 Worker 内部会话 ID |

## 分步实现

### 1. 创建包目录

```bash
mkdir -p internal/worker/myagent/
```

典型文件布局：

```
internal/worker/myagent/
├── worker.go        # 核心适配器 + init() 注册
├── parser.go        # Agent 输出解析（可选）
├── mapper.go        # 解析结果 → AEP 事件映射（可选）
└── worker_test.go   # 测试
```

### 2. 添加 WorkerType 常量

在 `internal/worker/worker.go` 中添加：

```go
const (
    // ... 已有常量
    TypeMyAgent WorkerType = "my_agent"
)
```

### 3. 定义 Worker 结构体

嵌入 `base.BaseWorker` 获取 Terminate/Kill/Wait/Health/LastIO/Conn：

```go
package myagent

import (
    "context"
    "fmt"
    "io"
    "log/slog"
    "time"

    "github.com/hrygo/hotplex/internal/worker"
    "github.com/hrygo/hotplex/internal/worker/base"
    "github.com/hrygo/hotplex/internal/worker/proc"
    "github.com/hrygo/hotplex/pkg/events"
)

// Compile-time interface compliance check.
var _ worker.Worker = (*Worker)(nil)

type Worker struct {
    *base.BaseWorker

    sessionID string
    cancel    context.CancelFunc
    // ... agent-specific fields
}

func New() *Worker {
    return &Worker{
        BaseWorker: base.NewBaseWorker(slog.Default(), nil),
    }
}
```

### 4. 实现 Capabilities

```go
func (w *Worker) Type() worker.WorkerType            { return worker.TypeMyAgent }
func (w *Worker) SupportsResume() bool                { return false }
func (w *Worker) SupportsStreaming() bool              { return true }
func (w *Worker) SupportsTools() bool                  { return true }
func (w *Worker) EnvBlocklist() []string               { return nil }
func (w *Worker) SessionStoreDir() string              { return "" }
func (w *Worker) MaxTurns() int                        { return 0 }
func (w *Worker) Modalities() []string                 { return []string{"text"} }
```

### 5. 实现 Start / Resume

```go
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    w.Mu.Lock()
    defer w.Mu.Unlock()

    if w.Proc != nil {
        return fmt.Errorf("myagent: already started")
    }

    // 1. 构建进程管理器
    w.Proc = proc.New(proc.Opts{Logger: w.Log})

    // 2. 构建环境变量（base.BuildEnv 合并 blocklist + ConfigEnv + ConfigBlocklist）
    env := base.BuildEnv(session, nil, "my-agent")

    // 3. 启动子进程（注意：使用 context.Background()，不绑定请求 ctx）
    stdin, _, _, err := w.Proc.Start(
        context.Background(), "my-agent-binary",
        []string{"--json"}, env, session.ProjectDir,
    )
    if err != nil {
        w.Proc = nil
        return fmt.Errorf("myagent: start: %w", err)
    }

    // 4. 创建 SessionConn（base.Conn 实现了 worker.SessionConn）
    bc := base.NewConn(w.Log, stdin, session.UserID, session.SessionID)
    w.SetConnLocked(bc)

    w.sessionID = session.SessionID
    w.BaseWorker.StartTime = time.Now()
    w.BaseWorker.SetLastIO(w.BaseWorker.StartTime)

    // 5. 启动输出读取 goroutine
    childCtx, cancel := context.WithCancel(context.Background())
    w.cancel = cancel
    go w.readOutput(childCtx)

    return nil
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    return fmt.Errorf("myagent: resume not supported")
}
```

### 6. 实现 Input

```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    conn := w.Conn()
    if conn == nil {
        return fmt.Errorf("myagent: not started")
    }

    // metadata 分发（权限响应、问答响应等），需要 Worker 实现对应 Handler
    handled, err := base.DispatchMetadata(ctx, metadata, w)
    if err != nil {
        return err
    }
    if handled {
        w.SetLastIO(time.Now())
        return nil
    }

    // 通过 base.Conn.SendUserMessage 发送用户消息
    if baseConn, ok := conn.(*base.Conn); ok {
        if err := baseConn.SendUserMessage(ctx, content); err != nil {
            return fmt.Errorf("myagent: input: %w", err)
        }
    }
    w.SetLastIO(time.Now())
    return nil
}
```

### 7. 实现 Terminate / Health / LastIO / ResetContext

```go
func (w *Worker) Terminate(ctx context.Context) error {
    if w.cancel != nil {
        w.cancel()
    }
    return w.BaseWorker.Terminate(ctx)
}

func (w *Worker) Health() worker.WorkerHealth {
    return w.BaseWorker.Health(worker.TypeMyAgent)
}

func (w *Worker) LastIO() time.Time {
    return w.BaseWorker.LastIO()
}

func (w *Worker) ResetContext(ctx context.Context) error {
    if err := w.Terminate(ctx); err != nil {
        return fmt.Errorf("myagent: reset terminate: %w", err)
    }
    // 重新 Start 即可（也可删除 session 文件后 Start）
    return nil
}
```

### 8. 实现输出读取循环

```go
func (w *Worker) readOutput(ctx context.Context) {
    // 捕获 entryConn 防止 reset 后关闭新 Conn
    entryConn := w.Conn()
    defer func() {
        if r := recover(); r != nil {
            w.Log.Error("myagent: readOutput panic", "panic", r)
        }
        if entryConn != nil {
            _ = entryConn.Close()
        }
    }()

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        line, err := w.Proc.ReadLine()
        if err != nil {
            if err == io.EOF {
                return
            }
            w.Log.Error("myagent: read error", "err", err)
            return
        }
        if line == "" {
            continue
        }

        // 解析 agent 输出 → 映射为 AEP 事件 → 非阻塞发送
        envs := w.parseAndMap(line)
        for _, env := range envs {
            w.trySend(env)
        }
    }
}

func (w *Worker) trySend(env *events.Envelope) {
    conn := w.Conn()
    if conn == nil {
        return
    }
    if ts, ok := conn.(interface{ TrySend(*events.Envelope) bool }); ok {
        if !ts.TrySend(env) {
            w.Log.Warn("myagent: recv channel full, dropping event",
                "session_id", w.sessionID, "event_type", env.Event.Type)
        }
    }
}
```

### 9. 注册 Worker

在包内 `init()` 中注册，重复注册会 panic：

```go
// internal/worker/myagent/worker.go 末尾
func init() {
    worker.Register(worker.TypeMyAgent, func() (worker.Worker, error) {
        return &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
    })
}
```

注册机制（`internal/worker/registry.go`）使用 `map[WorkerType]Builder` + `sync.RWMutex` 保护。`Builder` 签名为 `func() (Worker, error)`。

### 10. 添加 blank import

在 `cmd/hotplex/main.go` 中确保包被导入：

```go
import (
    _ "github.com/hrygo/hotplex/internal/worker/myagent"
)
```

### 11. 添加配置

在 `internal/config/` 中添加配置结构体，并在 `configs/config.yaml` 增加对应配置段。

## 关键约束

| 约束 | 说明 |
|------|------|
| Mutex | 显式 `mu` 字段，不嵌入 `sync.Mutex`，不传指针 |
| 进程终止 | 必须遵循 SIGTERM → 5s grace → SIGKILL 三层终止，`proc.Manager` 已封装 |
| 环境隔离 | 使用 `base.BuildEnv()` 合并 blocklist，防止 `CLAUDECODE`/`HOTPLEX_*` 泄漏 |
| Goroutine | 每个 goroutine 必须有明确退出路径（ctx cancel / channel close） |
| 跨平台 | 使用 `filepath.Join()`，不硬编码路径分隔符 |
| Panic recovery | `readOutput` 等 goroutine 必须有 `defer recover()` |
| 日志 | 使用 `log/slog` JSON handler |

## 测试要求

| 测试类型 | 要求 |
|---------|------|
| 编译时检查 | `var _ worker.Worker = (*Worker)(nil)` 放在文件顶部 |
| 单元测试 | 注入 `readLineFn` mock 替代真实进程（参考 claudecode/worker_test.go） |
| Table-driven | 事件映射、输入解析使用 table-driven + `t.Parallel()` |
| 错误路径 | 覆盖：未启动时 Input、进程崩溃恢复、stdin 关闭 |
| 竞态检测 | `go test -race ./internal/worker/myagent/...` 通过 |

运行测试：

```bash
go test -race ./internal/worker/myagent/...
```

## 参考实现

| 模式 | 位置 | 说明 |
|------|------|------|
| stdin/stdout | `internal/worker/claudecode/` | Claude Code 适配器，NDJSON 协议，最完整的参考 |
| HTTP/SSE | `internal/worker/opencodeserver/` | 单例进程 + HTTP 长连接 |
