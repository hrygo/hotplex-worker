---
paths:
  - "**/*.go"
---

# Go 代码规范

> 适用于 hotplex-worker 项目范围的 Go 编码规范
> 通用标准 → 见 `linting.md` | 测试规范 → 见 `testing.md`
> 本文件聚焦其他规则未覆盖的**项目架构模式**

---

## DI 注入模式

**禁止使用 wire/dig 等 DI 框架**，全部手动构造函数注入。

```go
// ✅ 正确：GatewayDeps 结构承载所有依赖
type GatewayDeps struct {
    Hub          *gateway.Hub
    SM           *session.Manager
    JWTValidator *security.JWTValidator
    Bridge       *gateway.Bridge
    // ...
}

// ❌ 禁止：不要通过全局变量或包级别变量共享状态
var globalState *Something  // 禁止
```

---

## Worker 适配器模式

新增 Worker 类型时，遵循 **BaseWorker embedding** 模式：

```go
// internal/worker/<name>/worker.go
func New(cfg *config.Config) worker.Builder {
    return &workerAdapter{}
}

type workerAdapter struct {
    *base.BaseWorker  // 共享生命周期：Terminate/Kill/Wait/Health
    // 唯一字段：cmd、conn、env 等
}

func (w *workerAdapter) Start(ctx context.Context, env []string) error {
    // 1. exec.Command with SysProcAttr{Setpgid: true}
    // 2. 建立 stdio NDJSON 通道
    // 3. 启动读/写 pump goroutine
    return nil
}

func (w *workerAdapter) Input(ctx context.Context, content string) error {
    return w.Conn.Send(events.Envelope{...})
}

// init() 中注册
func init() {
    worker.Register(worker.TypeOpenCodeSrv, New)
}
```

**BaseWorker 提供的能力**（无需重复实现）：
- `Terminate(ctx)` — 优雅终止
- `Kill()` — 强制终止
- `Wait()` — 等待进程退出
- `Health()` — 健康检查
- `LastIO()` — 最后一次 I/O 时间

---

## Messaging 适配器模式

新增平台消息适配器时，遵循 **PlatformAdapter embedding** 模式：

```go
// internal/messaging/<name>/adapter.go
func New(cfg *config.FeishuConfig) *Adapter {
    a := &Adapter{}
    platformadapter.Register(a)  // 注入 PlatformAdapter 基类
    return a
}

type Adapter struct {
    *platformadapter.PlatformAdapter  // SetHub/SetSM/SetHandler/SetBridge
    // 唯一字段：wsClient、messageConverter 等
}
```

**PlatformAdapter 提供的能力**：
- `SetHub(*Hub)` — 注册 WS hub
- `SetSM(*SessionManager)` — 注册 session manager
- `SetHandler(*Handler)` — 注册 AEP handler
- `SetBridge(*Bridge)` — 注册 lifecycle bridge

**PlatformConn 接口**（适配器必须实现）：
```go
type PlatformConn interface {
    WriteCtx(ctx context.Context, env *Envelope) error
    Close()
}
```

---

## Admin API 包隔离模式

避免循环依赖：Admin API 使用接口而非直接引用具体类型。

```go
// internal/admin/admin.go — 定义接口
type SessionManager interface {
    Get(id string) (*managedSession, error)
    List(filter sessionFilter) ([]*SessionInfo, error)
    Delete(id string) error
}
```

```go
// cmd/worker/main.go — 适配器桥接具体类型
type adminDeps struct {
    sm        *session.Manager        // 具体类型，非接口
    hub       *gateway.Hub
    validator *security.JWTValidator
}
```

---

## 广播通道与 Backpressure

```go
// 每个 session 一个带缓冲 channel
ch := make(chan *Envelope, cfg.BroadcastQueueSize)  // 默认 256

// Backpressure 丢弃规则
if env.Event.Type == "message.delta" || env.Event.Type == "raw" {
    select {
    case ch <- env:
    default:
        // 静默丢弃，不返回错误
        sessionDropped[sessionID] = true
        return nil
    }
}
// 关键事件不可丢弃
ch <- env
```

---

## 单写者 SQLite 模式

所有写操作通过单写 goroutine 串行化：

```go
type SQLiteStore struct {
    db    *sql.DB
    write chan writeRequest  // buffered channel
}

func (s *SQLiteStore) writer() {
    batch := make([]writeRequest, 0, 50)
    flush := time.NewTicker(100 * time.Millisecond)
    for {
        select {
        case req := <-s.write:
            batch = append(batch, req)
            if len(batch) >= 50 {
                s.flush(batch)
                batch = batch[:0]
            }
        case <-flush.C:
            if len(batch) > 0 {
                s.flush(batch)
                batch = batch[:0]
            }
        }
    }
}
```

---

## 配置热重载模式

```go
// 1. 创建 watcher
watcher, err := config.Watch(cfgPath, func(newCfg *config.Config) {
    // 应用新配置
    applyMessagingEnv(newCfg)
    gateway.UpdateConfig(newCfg)
})

// 2. defer 关闭
defer func() { _ = watcher.Close() }()

// 3. 错误日志 + 继续运行（旧配置仍然有效）
```

---

## SDK Logger 重定向模式

将第三方 SDK（如 Lark）的日志输出重定向到项目统一的 slog：

```go
import "github.com/larksuite/oapi-sdk-go/v3"

func init() {
    oapi.SetLogger(sdkLogger{slog: slog.Default()})
}

type sdkLogger struct{ slog *slog.Logger }

func (l sdkLogger) Debug(msg string)  { l.slog.Debug(msg) }
func (l sdkLogger) Info(msg string)   { l.slog.Info(msg) }
func (l sdkLogger) Warn(msg string)   { l.slog.Warn(msg) }
func (l sdkLogger) Error(msg string)  { l.slog.Error(msg) }
```

---

## 错误处理层级

| 场景 | 处理方式 |
|------|---------|
| 外部输入验证失败 | 返回 `&AppError{Code: "...", ...}` |
| 内部逻辑错误 | `return fmt.Errorf("...: %w", err)` |
| 第三方调用失败 | `return fmt.Errorf("invoke ...: %w", err)` |
| 关键事件发送失败 | `log.Error(...)` + `return err` |
| 非关键操作失败 | `_ = op()` 或 `log.Warn(...)` |
| Handler/Bridge panic | `recover()` + `log.Error("panic", ...)` + `return fmt.Errorf("handler/bridge panic: %v", r)` |

---

## Panic Recovery 模式

Gateway handler 和 bridge forwardEvents 必须包含 panic recovery：

```go
func (h *Handler) Handle(ctx context.Context, env *Envelope) (err error) {
    defer func() {
        if r := recover(); r != nil {
            h.log.Error("gateway: panic in handler", "error", r, "session_id", env.SessionID)
            err = fmt.Errorf("handler panic: %v", r)
        }
    }()
    // ... normal handling
}
```

---

## 控制命令模式

Messaging 通道的控制命令通过两层 map 解析：

```go
// control_command.go
var slashCommandMap = map[string]events.ControlAction{
    "/gc":      events.ControlActionGC,
    "/reset":   events.ControlActionReset,
    "/park":    events.ControlActionPark,
    "/restart": events.ControlActionRestart,
    "/new":     events.ControlActionNew,
}

// 自然语言触发必须带 $ 前缀
var naturalLanguageMap = map[string]events.ControlAction{
    "$gc":    events.ControlActionGC,
    "$休眠":  events.ControlActionPark,
    "$挂起":  events.ControlActionPark,
    "$重置":  events.ControlActionReset,
}
```

**设计决策**：自然语言 map 使用 `$` 前缀防止用户日常对话意外触发控制命令。

---

## 文本清洗模式

所有用户面向的文本输出通过 `SanitizeText()` 清洗：

```go
// sanitize.go
func SanitizeText(s string) string {
    // 移除: control chars (保留 \t\n), null bytes, BOM, surrogates
    // 保留: tabs, newlines, normal Unicode
}
```

---

## Context 传播规范

- **函数参数**：必须作为第一个参数传递 `ctx context.Context`
- **禁止**：`context.Background()` 在请求处理路径中
- **超时**：外部请求 30s，Worker 生命周期绑定请求 ctx
- **禁止**存储在 struct 字段中：ctx 应沿调用链传递

---

## Lock 顺序（防止死锁）

```go
// 固定顺序：Manager lock → per-session lock
func (sm *Manager) GetSession(id string) (*ManagedSession, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    ms, ok := sm.sessions[id]
    if !ok {
        return nil, ErrSessionNotFound
    }

    // 不在这里锁 ms.mu，避免与外层锁形成循环依赖
    // 调用方负责锁 ms.mu
    return ms, nil
}
```
