# DI Setter 链 → 构造函数注入

**Date:** 2026-04-29
**Status:** Approved
**Scope:** gateway + messaging layers

## Problem

当前 Handler、Bridge、Slack/Feishu Adapter 通过 15+ 个 `Set*` 方法分步注入依赖。问题：

1. **遗漏无编译时检查** — 忘调某个 Set 方法只会在运行时 nil panic
2. **类型断言脆弱** — `adapter.(interface{ SetHub(...) })` 遗漏不会报错
3. **依赖关系不透明** — 新人无法从构造函数签名看出完整依赖图

## Design

### 1. Gateway 层 — HandlerDeps + BridgeDeps

新增 `internal/gateway/deps.go`：

```go
type HandlerDeps struct {
    Bridge        *Bridge
    ConvStore     session.ConversationStore  // nil = disabled
    SkillsLocator SkillsLocator              // nil = skills commands return empty
}

type BridgeDeps struct {
    ConvStore      session.ConversationStore  // nil = disabled
    RetryCtrl      *LLMRetryController        // nil = no retry
    AgentConfigDir string                     // "" = disabled
    TurnTimeout    time.Duration              // 0 = no timeout
}
```

构造函数签名变更：

```go
func NewHandler(log *slog.Logger, hub *Hub, sm *session.Manager, jwt *security.JWTValidator, deps HandlerDeps) *Handler
func NewBridge(log *slog.Logger, hub *Hub, sm SessionManager, deps BridgeDeps) *Bridge
```

**构造顺序**：先 Bridge（不依赖 Handler），再 Handler（依赖 Bridge），打破原循环。

**删除的 setter**：`Handler.SetBridge`、`Handler.SetConvStore`、`Handler.SetSkillsLocator`、`Bridge.SetConvStore`、`Bridge.SetRetryController`、`Bridge.SetAgentConfigDir`、`Bridge.SetTurnTimeout`

**保留的 setter**：`Bridge.SetWorkerFactory`（仅测试用）

### 2. Messaging 层 — AdapterConfig

新增 `internal/messaging/config.go`：

```go
type AdapterConfig struct {
    Hub      HubInterface
    SM       SessionManager
    Handler  HandlerInterface
    Bridge   *Bridge
    Gate     *Gate
    STT      stt.Transcriber

    Platform PlatformType
    Extras   map[string]any
}
```

`PlatformAdapterInterface` 新增方法：

```go
type PlatformAdapterInterface interface {
    Platform() PlatformType
    ConfigureWith(config AdapterConfig)
    Start(ctx context.Context) error
    Close(ctx context.Context) error
}
```

各 adapter 实现 `ConfigureWith`，从 `AdapterConfig` 一次性接收所有依赖。平台特有参数通过 `Extras` map 传递。

**Slack Extras**：`assistant_enabled (*bool)`、`reconnect_base_delay (time.Duration)`、`reconnect_max_delay (time.Duration)`、`bot_token (string)`、`app_token (string)`

**Feishu Extras**：`reconnect_base_delay (time.Duration)`、`reconnect_max_delay (time.Duration)`、`app_id (string)`、`app_secret (string)`

`PlatformAdapter` 基类提供默认 `ConfigureWith` 实现，子类可 override。

**删除的 setter**：`PlatformAdapter.SetHub`、`PlatformAdapter.SetSessionManager`、`PlatformAdapter.SetHandler`、`PlatformAdapter.SetBridge`、Slack 的 `SetGate`/`SetAssistantEnabled`/`SetReconnectDelays`/`SetTranscriber`、Feishu 的 `SetGate`/`SetTranscriber`/`SetReconnectDelays`

**保留**：`Configure` 方法内部逻辑合并到 `ConfigureWith`

### 3. 调用方变更

**gateway_run.go**：

```go
bridge := gateway.NewBridge(log, hub, sm, gateway.BridgeDeps{
    ConvStore:      convStore,
    RetryCtrl:      retryCtrl,
    AgentConfigDir: agentConfigDir,
    TurnTimeout:    turnTimeout,
})
handler := gateway.NewHandler(log, hub, sm, jwtValidator, gateway.HandlerDeps{
    Bridge:        bridge,
    ConvStore:     convStore,
    SkillsLocator: skillsCache,
})
```

**messaging_init.go**：

```go
adapter.ConfigureWith(messaging.AdapterConfig{
    Hub:      hub,
    SM:       sm,
    Handler:  handler,
    Bridge:   msgBridge,
    Gate:     gate,
    STT:      transcriber,
    Platform: pt,
    Extras:   platformExtras(pt, cfg),
})
```

### 4. 测试适配

- Gateway 测试：改用 deps struct 构造，删除 `TestHandler_SetBridge` 等 setter 测试
- Messaging 测试：改用 `ConfigureWith` 替代分步 Set
- `SetWorkerFactory` 保留为测试专用 setter

## Files Changed

| File | Change |
|------|--------|
| `internal/gateway/deps.go` | New — HandlerDeps + BridgeDeps structs |
| `internal/gateway/handler.go` | Constructor signature + delete 3 setters |
| `internal/gateway/bridge.go` | Constructor signature + delete 4 setters |
| `internal/messaging/config.go` | New — AdapterConfig struct |
| `internal/messaging/platform_adapter.go` | Add ConfigureWith to interface + default impl |
| `internal/messaging/slack/adapter.go` | Implement ConfigureWith, delete 6 setters |
| `internal/messaging/feishu/adapter.go` | Implement ConfigureWith, delete 4 setters |
| `cmd/hotplex/gateway_run.go` | Adapt call sites |
| `cmd/hotplex/messaging_init.go` | Adapt call sites |
| `internal/gateway/handler_test.go` | Adapt ~6 test funcs |
| `internal/gateway/bridge_test.go` | Adapt ~5 test funcs |
| `internal/gateway/conn_test.go` | Adapt handler/bridge construction in helpers |
| `internal/messaging/integration_test.go` | Adapt ConfigureWith |
| `internal/messaging/slack/adapter_test.go` | Adapt ~5 test funcs |
| `internal/messaging/feishu/adapter_helper_test.go` | Adapt construction |

## Migration Strategy

两批提交，每批独立可验证：

1. **Batch 1 — Gateway**：deps.go + handler/bridge 构造函数变更 + gateway_run.go + gateway tests
2. **Batch 2 — Messaging**：config.go + adapter ConfigureWith + messaging_init.go + messaging tests

每批提交后跑 `make check` 确保编译+测试+lint 全过。

## Risk

- **编译器兜底**：删除 setter 后所有未迁移调用点编译失败，零遗漏
- **接口稳定**：Worker/SessionConn 等核心接口不变
- **向后兼容**：`init()` 注册机制不变，adapter 自注册流程不变
