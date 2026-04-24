---
paths:
  - "**/*.go"
---

# Go 代码规范

> 适用于 hotplex 项目范围的 Go 编码规范
> 通用标准 → 见 `linting.md` | 测试规范 → 见 `testing.md`
> AEP 协议 → 见 `aep.md` | Session 管理 → 见 `session.md`
> 安全 → 见 `security.md` | 进程管理 → 见 `worker-proc.md`

---

## DI 注入模式

**禁止 wire/dig**，全部手动构造函数注入。

```go
// ✅ GatewayDeps 结构承载所有依赖
type GatewayDeps struct {
    Hub          *gateway.Hub
    SM           *session.Manager
    JWTValidator *security.JWTValidator
    Bridge       *gateway.Bridge
}

// ❌ 禁止全局变量或包级别状态共享
var globalState *Something
```

---

## Worker 适配器模式

新增 Worker 类型时，遵循 **BaseWorker embedding** 模式：

```go
type workerAdapter struct {
    *base.BaseWorker  // 共享生命周期：Terminate/Kill/Wait/Health
}

func (w *workerAdapter) Start(ctx context.Context, env []string) error { ... }
func (w *workerAdapter) Input(ctx context.Context, content string) error { ... }

// init() 中注册
func init() { worker.Register(worker.TypeOpenCodeSrv, New) }
```

**BaseWorker 提供**：Terminate / Kill / Wait / Health / LastIO / ResetContext

---

## Messaging 适配器模式

```go
type Adapter struct {
    *platformadapter.PlatformAdapter  // SetHub/SetSM/SetHandler/SetBridge
}

// PlatformConn 接口（适配器必须实现）：
type PlatformConn interface {
    WriteCtx(ctx context.Context, env *Envelope) error
    Close()
}
```

**4 步初始化**（缺一不可）：SetHub → SetSM → SetHandler → SetBridge → Start

---

## Admin API 包隔离模式

避免循环依赖：Admin API 使用 Provider 接口而非直接引用具体类型。

```go
// internal/admin/admin.go — 定义 Provider 接口
type SessionManagerProvider interface { ... }

// cmd/hotplex/main.go — 适配器桥接具体类型
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

## 文本清洗模式

所有用户面向的文本输出通过 `SanitizeText()` 清洗：

```go
// sanitize.go — 移除 control chars (保留 \t\n), null bytes, BOM, surrogates
func SanitizeText(s string) string { ... }
```

---

## SDK Logger 重定向模式

将第三方 SDK（如 Lark）的日志输出重定向到项目统一的 slog：

```go
type sdkLogger struct{ slog *slog.Logger }
func (l sdkLogger) Debug(msg string)  { l.slog.Debug(msg) }
func (l sdkLogger) Info(msg string)   { l.slog.Info(msg) }
func (l sdkLogger) Warn(msg string)   { l.slog.Warn(msg) }
func (l sdkLogger) Error(msg string)  { l.slog.Error(msg) }
```

---

## Context 传播规范

- **函数参数**：`ctx context.Context` 必须作为第一个参数
- **禁止**：请求处理路径中使用 `context.Background()`
- **禁止**：存储 ctx 在 struct 字段中 — 沿调用链传递
- **超时**：外部请求 30s，Worker 生命周期绑定请求 ctx

---

## Lock 顺序（防止死锁）

固定顺序：`Manager.mu` → `managedSession.mu` — 始终按此顺序加锁

```go
func (sm *Manager) GetSession(id string) (*ManagedSession, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    ms, ok := sm.sessions[id]
    // 调用方负责锁 ms.mu，不在这里嵌套
    return ms, nil
}
```

---

## 配置热重载模式

```go
watcher, err := config.Watch(cfgPath, func(newCfg *config.Config) {
    applyMessagingEnv(newCfg)
    gateway.UpdateConfig(newCfg)
})
defer func() { _ = watcher.Close() }()
// 错误日志 + 继续运行（旧配置仍然有效）
```
