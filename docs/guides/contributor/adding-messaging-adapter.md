---
title: 添加消息平台适配器
description: 如何为 HotPlex Gateway 开发新的消息平台适配器
persona: contributor
difficulty: advanced
---

# 添加消息平台适配器

本文介绍如何为 HotPlex Gateway 添加一个新的消息平台适配器。消息适配器负责桥接外部聊天平台（Slack、飞书等）与 Gateway 的 AEP 协议层，使 Agent 可以通过新平台与用户交互。

## 架构概览

```
Platform SDK (WebSocket/HTTP)
    │
    ▼
PlatformAdapterInterface       ← 你需要实现这个
    │  ├── PlatformAdapter     ← 嵌入获取共享状态管理
    │  └── BaseAdapter[C]      ← 嵌入获取连接池
    │
    ├── PlatformConn           ← 你需要实现这个（Hub 广播回平台）
    │       ├── WriteCtx()     → 将 Worker 输出写回平台
    │       └── Close()        → 关闭平台连接
    │
    ▼
Bridge.Handle()
    │  1. starter.StartPlatformSession()  → 创建 Gateway Session
    │  2. hub.JoinPlatformSession(conn)    → 注册 PlatformConn 到 Hub
    │  3. hub.NextSeq()                    → 分配序列号
    │  4. handler.Handle(envelope)         → 分发 AEP 事件到 Worker
    ▼
Worker (AI Agent)
```

## 接口清单

### PlatformAdapterInterface（必须实现）

定义在 `internal/messaging/platform_adapter.go`：

```go
type PlatformAdapterInterface interface {
    Platform() PlatformType
    Start(ctx context.Context) error          // 非阻塞
    HandleTextMessage(ctx, platformMsgID, channelID, teamID, threadTS, userID, text string) error
    Close(ctx context.Context) error
    ConfigureWith(config AdapterConfig) error
    GetBotID() string
}
```

### PlatformConn（必须实现）

定义在 `internal/messaging/platform_conn.go`，是 Hub 向平台回写事件的接口：

```go
type PlatformConn interface {
    WriteCtx(ctx context.Context, env *events.Envelope) error
    Close() error
}
```

`Hub.JoinPlatformSession(sessionID, pc)` 将 `PlatformConn` 注册到 Hub 广播列表。Worker 的每个输出事件都会触发 `pc.WriteCtx()`。

### AdapterConfig（依赖注入）

定义在 `internal/messaging/config.go`，通过 `ConfigureWith` 传入：

```go
type AdapterConfig struct {
    Hub     HubInterface       // WebSocket Hub（广播路由）
    SM      SessionManager     // Session 状态机
    Handler HandlerInterface   // AEP Handler（事件分发）
    Bridge  *Bridge            // MessagingBridge（Session 生命周期）
    Gate    *Gate              // 访问控制
    Extras  map[string]any     // 平台特定配置（凭据、开关等）
}
```

## 分步实现

### 1. 创建包目录

```bash
mkdir -p internal/messaging/mychat/
```

典型文件布局：

```
internal/messaging/mychat/
├── adapter.go      # 核心适配器 + init() 注册
├── conn.go         # PlatformConn 实现
├── handler.go      # 平台消息处理（可选）
└── adapter_test.go # 测试
```

### 2. 定义 PlatformType 常量

在 `internal/messaging/platform_adapter.go` 中添加：

```go
const (
    // ... 已有常量
    PlatformMyChat PlatformType = "mychat"
)
```

### 3. 定义 Adapter 结构体

嵌入 `BaseAdapter[C]`（内含 `PlatformAdapter`）获取共享状态管理和连接池：

```go
package mychat

import (
    "context"
    "fmt"
    "log/slog"
    "sync"

    "github.com/hrygo/hotplex/internal/messaging"
    "github.com/hrygo/hotplex/pkg/events"
)

// Compile-time interface compliance check.
var _ messaging.PlatformAdapterInterface = (*Adapter)(nil)

type Adapter struct {
    messaging.BaseAdapter[*MyChatConn]

    mu sync.RWMutex

    botID  string
    token  string
    client *MyChatSDK
}
```

**PlatformAdapter 提供**：
- `ConfigureWith()` -- 注入 Hub/SM/Handler/Bridge
- `StartGuard()` -- 防止重复启动
- `MarkClosed()` / `IsClosed()` -- 关闭状态管理
- `InitSharedState()` -- 创建 Dedup + InteractionManager
- `CloseSharedState()` -- 清理共享状态
- `ConfigureShared()` -- 提取 Gate + backoff 配置

**BaseAdapter[C] 提供**：
- `InitConnPool(factory)` -- 创建连接池
- `GetOrCreateConn(id, thread)` -- 获取或创建平台连接
- `DrainConns()` -- 排空并关闭所有连接
- `DeleteConn(id, thread)` -- 删除指定连接

### 4. 实现 Platform()

```go
func (a *Adapter) Platform() messaging.PlatformType {
    return messaging.PlatformMyChat
}
```

### 5. 实现 ConfigureWith()

```go
func (a *Adapter) ConfigureWith(config messaging.AdapterConfig) error {
    // 1. 调用基类注入 Hub/SM/Handler/Bridge
    _ = a.PlatformAdapter.ConfigureWith(config)

    // 2. 提取平台凭据
    a.token = config.ExtrasString("token")

    // 3. 提取共享配置（Gate、backoff delays）
    a.ConfigureShared(config)

    // 4. 提取平台特定配置
    // config.ExtrasBool("feature_flag")
    // config.ExtrasDuration("timeout")

    return nil
}
```

### 6. 实现 Start()

`Start` 必须是**非阻塞的** -- 长时间运行的初始化放在后台 goroutine 中：

```go
func (a *Adapter) Start(ctx context.Context) error {
    // 1. 防重复启动
    if !a.StartGuard() {
        a.Log.Warn("mychat: adapter already started, skipping")
        return nil
    }

    // 2. 验证凭据
    if a.token == "" {
        return fmt.Errorf("mychat: token required")
    }

    // 3. 初始化共享状态（Dedup + InteractionManager）
    a.InitSharedState()

    // 4. 初始化连接池
    a.InitConnPool(func(key string) *MyChatConn {
        // key 格式为 "channelID#threadTS"
        // 解析并创建 PlatformConn
        return NewMyChatConn(a, channelID, threadTS, a.Bridge().WorkDir())
    })

    // 5. 初始化平台 SDK（示例）
    client, err := mychatsdk.New(a.token)
    if err != nil {
        return fmt.Errorf("mychat: init sdk: %w", err)
    }
    a.client = client

    // 6. 注册消息回调
    a.client.OnMessage(a.onMessage)

    // 7. 后台连接（非阻塞）
    go func() {
        if err := a.client.Connect(ctx); err != nil {
            a.Log.Error("mychat: connection failed", "err", err)
        }
    }()

    // 8. 获取 bot identity（阻塞但快速）
    a.botID = a.client.BotID()

    a.Log.Info("mychat: adapter started", "bot_id", a.botID)
    return nil
}
```

### 7. 实现 HandleTextMessage()

这是消息处理的核心 -- 将平台消息映射为 AEP 信封并交给 Bridge：

```go
func (a *Adapter) HandleTextMessage(
    ctx context.Context,
    platformMsgID, channelID, teamID, threadTS, userID, text string,
) error {
    // 1. 消息去重
    if a.Dedup.Seen(platformMsgID) {
        return nil
    }

    // 2. 获取或创建 PlatformConn
    conn := a.GetOrCreateConn(channelID, threadTS)

    // 3. 构造 AEP 信封
    envelope := a.Bridge().MakeEnvelope(userID, text, session.PlatformContext{
        Platform:  string(messaging.PlatformMyChat),
        BotID:     a.botID,
        ChannelID: channelID,
        ThreadTS:  threadTS,
        UserID:    userID,
        WorkDir:   a.Bridge().WorkDir(),
    })

    // 4. 通过 Bridge 处理（自动完成 Session 创建 + Hub 注册 + 事件分发）
    return a.Bridge().Handle(ctx, envelope, conn)
}
```

### 8. 实现 GetBotID()

```go
func (a *Adapter) GetBotID() string { return a.botID }
```

### 9. 实现 Close()

```go
func (a *Adapter) Close(ctx context.Context) error {
    if a.IsClosed() {
        return nil
    }
    a.MarkClosed()

    // 1. 关闭共享状态
    a.CloseSharedState()

    // 2. 关闭所有平台连接
    for _, conn := range a.DrainConns() {
        _ = conn.Close()
    }

    // 3. 关闭平台 SDK
    if a.client != nil {
        return a.client.Close()
    }
    return nil
}
```

### 10. 实现 PlatformConn

`PlatformConn` 负责将 Worker 输出事件写回平台：

```go
type MyChatConn struct {
    adapter   *Adapter
    channelID string
    threadTS  string
    workDir   string
}

func NewMyChatConn(adapter *Adapter, channelID, threadTS, workDir string) *MyChatConn {
    return &MyChatConn{
        adapter:   adapter,
        channelID: channelID,
        threadTS:  threadTS,
        workDir:   workDir,
    }
}

// WriteCtx 将 AEP 事件写回平台 -- 由 Hub 广播触发。
func (c *MyChatConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
    if env == nil {
        return fmt.Errorf("mychat: nil envelope")
    }

    switch env.Event.Type {
    case events.Done:
        return c.handleDone(ctx, env)
    case events.Error:
        return c.handleError(ctx, env)
    case events.MessageDelta:
        return c.handleDelta(ctx, env)
    case events.PermissionRequest:
        return c.handlePermissionRequest(ctx, env)
    default:
        return nil // 忽略不关心的事件类型
    }
}

// Close 关闭连接 -- 从连接池移除并清理资源。
func (c *MyChatConn) Close() error {
    c.adapter.DeleteConn(c.channelID, c.threadTS)
    return nil
}
```

### 11. 注册适配器

使用 `messaging.Register()` 在 `init()` 中注册，重复注册会 panic：

```go
// internal/messaging/mychat/adapter.go 末尾
func init() {
    messaging.Register(messaging.PlatformMyChat, func(log *slog.Logger) messaging.PlatformAdapterInterface {
        return &Adapter{
            BaseAdapter: messaging.BaseAdapter[*MyChatConn]{
                PlatformAdapter: messaging.PlatformAdapter{Log: log.With("channel", string(messaging.PlatformMyChat))},
            },
        }
    })
}
```

### 12. 添加 blank import

在 `cmd/hotplex/messaging_init.go` 中添加：

```go
import (
    _ "github.com/hrygo/hotplex/internal/messaging/mychat"
)
```

### 13. 在 messaging_init.go 中添加配置段

在 `startMessagingAdapters` 的 switch 中添加平台分支：

```go
case messaging.PlatformMyChat:
    if !appCfg.Messaging.MyChat.Enabled {
        statuses = append(statuses, AdapterStatus{Name: "mychat", Started: false})
        continue
    }
    workerType = appCfg.Messaging.MyChat.WorkerType
    workDir = appCfg.Messaging.MyChat.WorkDir
```

并在配置构建段添加凭据注入：

```go
case messaging.PlatformMyChat:
    gateway := messaging.NewGate(/* ... */)
    acfg.Gate = gateway
    acfg.Extras["token"] = appCfg.Messaging.MyChat.Token
```

## Bridge 3 步流程

当 `HandleTextMessage` 调用 `Bridge.Handle(ctx, envelope, conn)` 时，Bridge 自动执行：

1. **StartPlatformSession** -- 创建 Gateway Session（含 worker 启动、session 状态持久化）
2. **JoinPlatformSession** -- 将 PlatformConn 注册到 Hub，后续 Worker 输出自动路由到平台
3. **Handler.Handle** -- 将 AEP Input 事件分发给 Worker

这个流程是自动的，Adapter 只需构造正确的 envelope 和 conn。

## 关键约束

| 约束 | 说明 |
|------|------|
| 非阻塞 Start | `Start()` 不能阻塞，长时间初始化放 goroutine |
| 消息去重 | 必须使用 `Dedup.Seen()` 防止重复处理 |
| 文本清洗 | 发送到平台的文本使用 `messaging.SanitizeText()` |
| Mutex 规范 | 显式 `mu` 字段，不嵌入 `sync.Mutex`，不传指针 |
| Panic recovery | 回调 handler 中必须有 `defer recover()` |
| SDK 日志重定向 | 第三方 SDK 日志重定向到 `slog` |

## 测试要求

| 测试类型 | 要求 |
|---------|------|
| 编译时检查 | `var _ messaging.PlatformAdapterInterface = (*Adapter)(nil)` |
| Mock Hub | 实现 `HubInterface`（`JoinPlatformSession` + `NextSeq`） |
| Mock Handler | 实现 `HandlerInterface`（`Handle`） |
| Mock PlatformConn | 实现 `WriteCtx` / `Close` |
| Table-driven | 事件处理使用 table-driven + `t.Parallel()` |
| 竞态检测 | `go test -race ./internal/messaging/mychat/...` 通过 |

Mock PlatformConn 示例：

```go
type mockConn struct {
    messages []*events.Envelope
    mu       sync.Mutex
}

func (m *mockConn) WriteCtx(_ context.Context, env *events.Envelope) error {
    m.mu.Lock()
    m.messages = append(m.messages, env)
    m.mu.Unlock()
    return nil
}

func (m *mockConn) Close() error { return nil }
```

运行测试：

```bash
go test -race ./internal/messaging/mychat/...
```

## 参考实现

| 实现 | 位置 | 说明 |
|------|------|------|
| Slack Socket Mode | `internal/messaging/slack/` | 最完整的参考，含 STT/TTS/流式输出 |
| 飞书 WebSocket | `internal/messaging/feishu/` | 飞书适配器，含语音转文字、流式卡片 |
