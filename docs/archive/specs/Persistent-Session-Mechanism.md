# HotPlex 持久会话机制规格书

---
type: spec
tags:
  - project/HotPlex
  - session/management
  - session/persistence
date: 2026-04-06
status: implemented
progress: 100
priority: high
estimated_hours: 16
completion_date: 2026-04-07
---

> 版本: v1.4
> 日期: 2026-04-18
> 状态: ✅ 已实现
> 交叉复核: 已对齐 `pkg/events/events.go`、`internal/gateway/handler.go`、`internal/session/manager.go` 源码
> 实现验证: 2026-04-07 通过代码审查，所有 Phase 1 文件已实现并有单元测试覆盖
> v1.4 更新: 统一所有渠道 session 映射为 UUIDv5；修复 orphan 处理从 Delete+StartSession 改为 ResumeSession

---

## 1. 概述

### 1.1 目标

HotPlex Worker Gateway 持久会话机制，支持：

1. **客户端管理的 Session ID** — 客户端通过 `init.session_id` 上送 `client_session_id`，服务端用 UUIDv5 做一致性映射，确保相同 `(owner_id, worker_type, client_session_id)` 永远映射为同一服务端 session
2. **会话重置 (reset)** — 清空 `Session.Context`，终止并重建 Worker（相同 sessionID），Worker 内部删除旧会话文件，状态切至 `RUNNING`
3. **会话归档 (gc)** — 终止 Worker（Worker 内部自行保存状态），状态切至 `TERMINATED`，后续可 resume

### 1.2 设计原则

- **确定性映射**：UUIDv5 算法，同一组输入永远生成相同输出
- **分层透明**：Worker 会话持久化由 Worker 自行实现，上层 Gateway 只发指令
- **reset 实现自由**：Gateway 负责清空 `Session.Context`；Worker 负责清空运行时上下文（in-place 指令 or terminate+start，由 Worker 自行决定）

### 1.3 分层职责

```
┌────────────────────────────────────────────────────────────────┐
│                     HotPlex Gateway (上层)                      │
│                                                                │
│  Session 状态机 + 消息路由 + Worker 生命周期管理               │
│  不感知会话持久化细节（由 Worker 自行实现）                      │
└────────────────────────────────────────────────────────────────┘

  reset  →  sm.ClearContext  →  w.ResetContext  →  Transition(RUNNING)
  gc     →  w.Terminate     →  DetachWorker →  Transition(TERMINATED)
  resume →  Bridge.ResumeSession → w.Resume  →  Transition(RUNNING)

  reset 说明:
    Gateway: sm.ClearContext() → 清空 SessionInfo.Context
    Worker:  w.ResetContext()   → 清空运行时上下文（in-place 或 terminate+start，由 Worker 决定）

┌────────────────────────────────────────────────────────────────┐
│                    Worker Adapter (下层)                         │
│                                                                │
│  各 Worker 自行实现会话持久化，上层透明：                         │
│  ClaudeCode:  claude --resume <session_id>                      │
│  OpenCodeServer: opencode serve + HTTP API                │
│  OpenCodeSrv:  HTTP POST /session/<id>/resume                  │
│                                                                │
│  reset 实现（各 Worker 自行决定）：                              │
│  ClaudeCode:  terminate + start（claude 删除旧会话文件）         │
│  OpenCodeServer: terminate + start                                │
│  OpenCodeSrv:  发送 HTTP POST /session/<id>/reset             │
└────────────────────────────────────────────────────────────────┘
```

---

## 2. Session ID 映射机制

### 2.1 统一 UUIDv5 映射

所有渠道（Web + 平台）统一使用 UUIDv5 确定性映射。相同输入永远映射为相同的 session ID。

**两种派生函数**：

```go
// internal/session/key.go

// DeriveSessionKey — Web 客户端使用（包含 workDir + clientSessionID）
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID string, workDir string) string {
    name := ownerID + "|" + string(wt) + "|" + clientSessionID + "|" + workDir
    id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
    return id.String()
}

// PlatformContext holds platform-specific fields for session derivation.
type PlatformContext struct {
    Platform  string  // "slack" | "feishu"
    TeamID    string  // Slack: workspace ID
    ChannelID string  // Slack: channel ID
    ThreadTS  string  // 跨平台通用: thread/conversation key
    ChatID    string  // Feishu: chat ID
    UserID    string  // 跨平台通用: user identity
}

// DerivePlatformSessionKey — 平台消息使用（Feishu/Slack）
// 相同 (ownerID, workerType, platformContext) 永远映射为同一 session。
// 不同平台（Feishu vs Slack）即使原始 ID 相同也绝不冲突。
func DerivePlatformSessionKey(ownerID string, wt worker.WorkerType, ctx PlatformContext) string {
    // 内部拼接: ownerID + "|" + wt + "|" + platform + [platform-specific fields] + "|" + userID
    // 其中空字段不参与哈希
    ...
}
```

### 2.2 各渠道 Session ID 派生

| 渠道 | 派生函数 | 输入字段 | UUIDv5 哈希输入 |
|------|---------|---------|----------------|
| Web | `DeriveSessionKey` | ownerID, workerType, clientSessionID, workDir | `ownerID\|wt\|clientSessionID\|workDir` |
| Feishu | `DerivePlatformSessionKey` | userID, workerType, platform="feishu", chatID, threadTS, userID | `ownerID\|wt\|feishu\|chatID\|threadTS\|userID` |
| Slack channel | `DerivePlatformSessionKey` | userID, workerType, platform="slack", teamID, channelID="C...", threadTS, userID | `ownerID\|wt\|slack\|[teamID\|]C...\|threadTS\|userID` |
| Slack DM | `DerivePlatformSessionKey` | userID, workerType, platform="slack", teamID, channelID="D...", threadTS, userID | `ownerID\|wt\|slack\|[teamID\|]D...\|threadTS\|userID`（threadTS 为空时不参与哈希）|

**字段稳定性**：
- Feishu: `chat_id` 稳定（同一人与 bot 的 DM 固定），`threadTS` 区分群话题线程
- Slack channel: `channel_id` 稳定（`C...` 前缀），`threadTS` 区分 thread
- Slack DM: `channel_id` 以 `D...` 前缀标识 bot 与该 user 的私密对话（workspace 内唯一），支持 thread（`threadTS` 有值时参与哈希）
- 空字段不参与哈希，确保"无值"和"缺失字段"产生相同 session

### 2.3 Slack DM 语义

Slack Direct Message（DM）通过 channel ID 的 `D` 前缀与普通 channel 区分：

```
普通 channel:  channel_id = "C0123456789"  → threadTS 可能存在
DM channel:   channel_id = "D9876543210"  → threadTS 可能为空，也可能开启 thread
```

**Session 映射特性**：

| 特性 | 说明 |
|------|------|
| Workspace 隔离 | `teamID` 参与哈希，同一 user 在不同 workspace 的 DM 是不同 session |
| DM 标识 | `D` 前缀是 Slack 内部保证的 DM channel ID，同 bot + 同 user → 同一 `D...` ID |
| 支持 Thread | DM 中可开启 thread，`threadTS` 有值时参与哈希 → DM 主会话与 thread 子会话映射为不同 session |
| 同 user 重开 DM | Slack 会重用同一 `D...` ID → 映射到同一 session，对话历史保留 |
| DM 中开启 thread | Slack 允许 DM 内开 thread，此时 `threadTS` 有值 → 映射为子 session，与主 DM 分离 |
| Bot reinstall | Slack 可能重建 DM channel → `D...` ID 变化 → 映射为新 session（Slack 侧行为，无法控制）|

**Gateway 重启后 DM session 恢复**：与普通 channel 流程一致，orphan → `ResumeSession` → 同一个 UUIDv5 → Worker 恢复内部 session。

### 2.4 Session ID 与 Worker Session ID 的区分

| 概念 | 定义 | 持久化位置 |
|------|------|-----------|
| **Gateway Session ID** | UUIDv5 派生，服务端 DB 主键 | `sessions.id` |
| **Worker Session ID** | Worker runtime 内部 ID（如 Claude Code 的 session token） | `sessions.worker_session_id`（在首个 worker 事件后写入）|

Worker Session ID 的持久化流程：
1. Worker 启动时，Gateway 传递 `WorkerSessionID`（首次为空）
2. Worker 发出的第一个事件携带其内部 session ID
3. `Bridge.forwardEvents` 通过 `WorkerSessionIDHandler` 接口捕获，写入 DB
4. 下次 `ResumeSession` 时，`WorkerSessionID` 从 DB 恢复，传递给 worker

### 2.5 Gateway 重启后 Orphan 处理

Gateway 重启后，已建立的 platform session 变为 orphan（DB 有记录但 worker 进程已消失）。

**v1.4 修复前**：orphan → `Delete + StartSession` → 丢弃 WorkerSessionID，worker 进程重建，Claude Code 内部 session 丢失。

**v1.4 修复后**：orphan → `ResumeSession` → worker 恢复内部 session 状态，对话历史保留。

```
platform message arrives → sm.Get(sessionID) = exists
    ├─ sm.GetWorker(sessionID) = nil (orphan)
    │   └─ ResumeSession(ctx, sessionID, workDir)
    │       ├─ sm.AttachWorker → w.Resume
    │       ├─ WorkerSessionID 从 DB 恢复
    │       └─ Worker 内部 session 恢复（如 Claude Code --resume）
    └─ sm.GetWorker(sessionID) ≠ nil (live)
        └─ no-op
```

### 2.6 init 流程（Web）

```
Client init{session_id: "my-chat-001", worker_type: "claude_code", workDir: "/tmp/proj"}
  → DeriveSessionKey(ownerID="user_001", wt="claude_code", clientSessionID="my-chat-001", workDir="/tmp/proj")
  → UUIDv5: "550e8400-e29b-41d4-a716-446655440000"
  → sm.Get("550e8400-...") → found? → ResumeSession
  → sm.Get("550e8400-...") → not found? → StartSession
```

### 2.7 行为矩阵

| 场景 | 行为 |
|------|------|
| `client_session_id` 存在（Web） | UUIDv5 映射，确定性查找/创建 |
| 相同 tuple 重连 | 映射为同一 sessionID → resume |
| 不同 tuple | 映射为不同 sessionID |
| Platform orphan | `ResumeSession` → worker 内部 session 恢复 |
| 不同平台相同原始 ID | UUIDv5 隔离（platform 字段不同） |
| Slack DM 同 user 重开 | Slack 重用同一 `D...` ID → 映射为同一 session |
| Slack DM 中开 thread | `threadTS` 有值 → 映射为独立子 session |

---

## 3. Session 状态机

### 3.1 现有状态（源码 `pkg/events/events.go`）

```go
const (
    StateCreated    SessionState = "created"
    StateRunning    SessionState = "running"
    StateIdle       SessionState = "idle"
    StateTerminated SessionState = "terminated"
    StateDeleted    SessionState = "deleted"
)

var ValidTransitions = map[SessionState]map[SessionState]bool{
    StateCreated:    {StateRunning: true, StateTerminated: true},
    StateRunning:    {StateIdle: true, StateTerminated: true, StateDeleted: true},
    StateIdle:       {StateRunning: true, StateTerminated: true, StateDeleted: true},
    StateTerminated: {StateRunning: true, StateDeleted: true},  // resume
    StateDeleted:    {},  // 终态
}
```

### 3.2 状态流转图

```
                          input/resume       reset                  gc/idle_timeout
    ┌────────┐       ┌────────────────┐       ┌────────────┐
    │CREATED │──────►│    RUNNING     │──────►│    IDLE   │
    └────────┘ start  └────────────────┘  done  └─────┬──────┘
                                                       │
                                                       │  gc / idle_timeout
                                                       ▼
                                                  ┌────────────┐
                                                  │ TERMINATED │
                                                  └──────┬─────┘
                                                         │
                                                 retention_period
                                                         │
                                                         ▼
                                                    ┌─────────┐
                                                    │ DELETED │
                                                    └─────────┘
```

| 触发 | 转换 | Worker | 说明 |
|------|------|--------|------|
| `control.reset` | `*` → `RUNNING` | Worker 决定 | Gateway 清 Context；Worker 清自身上下文（in-place 或 terminate+start） |
| `control.gc` | `*` → `TERMINATED` | 终止 | Worker 内部保存状态 |
| WS 断开 | `*` → `IDLE` | 暂停 | Worker 暂停，不终止 |
| resume | `IDLE/TERMINATED` → `RUNNING` | 重建 | 发送 resume 指令 |

---

## 4. Control 事件

### 4.1 新增常量

```go
// pkg/events/events.go

const (
    // ... 现有常量 ...
    ControlActionTerminate  ControlAction = "terminate"
    ControlActionDelete     ControlAction = "delete"

    // 新增
    ControlActionReset ControlAction = "reset"  // 清空上下文，Worker 自行决定 in-place 或 terminate+start
    ControlActionGC    ControlAction = "gc"     // 归档会话，Worker 终止，保留历史
)
```

### 4.2 reset 操作

```
┌────────────────────────────────────────────────────────────────┐
│                        control.reset                            │
├────────────────────────────────────────────────────────────────┤
│  目标: Session.Context 必须清空                                  │
│                                                                │
│  触发: client → gateway (event.type="control")                 │
│  payload: {"action": "reset", "reason": "user_requested"}   │
│                                                                │
│  前置条件:                                                     │
│  - Session.State ∈ {CREATED, RUNNING, IDLE}                  │
│  - Worker 已 attached                                          │
│                                                                │
│  行为:                                                        │
│  1. Gateway: sm.ClearContext(sessionID)                      │
│     → SessionInfo.Context = {}                                │
│  2. Gateway: w.ResetContext(ctx)                             │
│     → Worker 内部清空运行时上下文                               │
│        ├─ Worker 支持 in-place 指令 → 发送 reset 信号，Worker 保持 │
│        └─ Worker 不支持 → terminate + start（物理删除会话文件）  │
│  3. sm.TransitionWithReason(sessionID, StateRunning, "reset")│
│                                                                │
│  响应: gateway → client                                       │
│  → event.type="state", data={"state": "running", "message": "context_reset"}
│                                                                │
│  后置:                                                        │
│  - Session.Context = {}                                        │
│  - Worker 运行时上下文已清空                                     │
│  - 下一条 input 开始全新对话                                    │
└────────────────────────────────────────────────────────────────┘
```

### 4.3 gc 操作

```
┌────────────────────────────────────────────────────────────────┐
│                         control.gc                              │
├────────────────────────────────────────────────────────────────┤
│  触发: client → gateway (event.type="control")                 │
│  payload: {"action": "gc", "reason": "user_idle"}             │
│                                                                │
│  前置条件:                                                      │
│  - Session.State ∈ {CREATED, RUNNING, IDLE}                  │
│  - Worker 已 attached                                           │
│                                                                │
│  行为:                                                          │
│  1. worker.Terminate(ctx)                                      │
│     → Worker 内部自行保存会话状态                                 │
│  2. sm.DetachWorker(sessionID)                                 │
│  3. sm.TransitionWithReason(sessionID, StateTerminated, "gc")│
│                                                                │
│  响应: gateway → client                                        │
│  → event.type="state", data={"state": "terminated", "message": "session_archived"}
│                                                                │
│  后置:                                                          │
│  - Worker 已终止/断开                                            │
│  - 会话历史由 Worker 内部保留                                     │
│  - 可通过 resume 恢复                                            │
└────────────────────────────────────────────────────────────────┘
```

---

## 5. 实现变更清单

### 5.1 文件变更总览

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `pkg/events/events.go` | 修改 | +2 常量: `ControlActionReset`, `ControlActionGC` |
| `internal/session/key.go` | 修改 | `DeriveSessionKey()`（UUIDv5，v1.4 新增 workDir 参数）；新增 `DerivePlatformSessionKey()` + `PlatformContext` 结构体 |
| `internal/session/manager.go` | 修改 | +1 方法: `ClearContext()` |
| `internal/gateway/conn.go` | 修改 | `performInit` 调用 `DeriveSessionKey`；`SessionStarter` 接口 `ResumeSession` 增加 `workDir` 参数 |
| `internal/gateway/bridge.go` | 修改 | `StartPlatformSession` orphan 路径改为 `ResumeSession`；`ResumeSession` 传递 `ProjectDir` |
| `internal/gateway/handler.go` | 修改 | `handleControl` +2 case: `handleReset`, `handleGC` |
| `internal/worker/worker.go` | 修改 | +1 方法: `Worker.ResetContext()` — Worker 自行决定清空方式 |
| `internal/messaging/bridge.go` | 修改 | 删除字符串格式常量；`MakeFeishuEnvelope`/`MakeSlackEnvelope` 改用 `DerivePlatformSessionKey` |
| `internal/messaging/platform_adapter.go` | 修改 | `HandleTextMessage` 接口增加 `teamID, threadTS` 参数 |
| `internal/messaging/feishu/adapter.go` | 修改 | 适配新接口签名 |
| `internal/messaging/slack/adapter.go` | 修改 | 从 `EventsAPIEvent.TeamID` 提取 teamID；`HandleTextMessage` 传递完整参数 |

### 5.7 internal/worker/worker.go

```go
// Worker 新增方法
type Worker interface {
    // ... 现有方法 ...

    // ResetContext 清空 Worker 运行时上下文。
    // Worker 自行决定实现方式：
    // - 支持 in-place 清空的 Worker → 发送内部 reset 信号
    // - 不支持的 Worker → terminate + start（物理删除会话文件）
    // 注意：Gateway 层已通过 sm.ClearContext() 清空 SessionInfo.Context。
    ResetContext(ctx context.Context) error
}
```

**实现示例**：

```go
// ClaudeCodeWorker: 不支持 in-place 清空 → terminate + start
func (w *Worker) ResetContext(ctx context.Context) error {
    // 1. 终止旧进程（claude 会删除自身会话文件）
    if err := w.Terminate(ctx); err != nil {
        return fmt.Errorf("terminate: %w", err)
    }
    // 2. 重建进程（使用相同 sessionID，claude 会创建全新会话）
    return w.Start(ctx, w.currentSession)
}

// OpenCodeSrvWorker: 支持 in-place 清空 → 发送 reset 请求
func (w *Worker) ResetContext(ctx context.Context) error {
    return w.client.Post("/session/" + w.sessionID + "/reset")
}
```

### 5.2 pkg/events/events.go

```go
// ControlAction 新增常量
const (
    // ... 现有 ...
    ControlActionTerminate  ControlAction = "terminate"
    ControlActionDelete     ControlAction = "delete"

    // 新增
    ControlActionReset ControlAction = "reset"  // 清空上下文，Worker 自行决定 in-place 或 terminate+start
    ControlActionGC    ControlAction = "gc"     // 归档会话，Worker 终止，保留历史
)
```

### 5.3 internal/session/key.go（修改）

```go
package session

import (
    "crypto/sha1"
    "strings"

    "github.com/google/uuid"
    "github.com/hrygo/hotplex/internal/worker"
)

var hotplexNamespace = uuid.MustParse("urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// DeriveSessionKey — Web 客户端使用（包含 workDir）
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID string, workDir string) string {
    name := ownerID + "|" + string(wt) + "|" + clientSessionID + "|" + workDir
    id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
    return id.String()
}

// PlatformContext — 平台消息 session 派生字段
type PlatformContext struct {
    Platform  string  // "slack" | "feishu"
    TeamID    string  // Slack: workspace ID
    ChannelID string  // Slack: channel ID
    ThreadTS  string  // 跨平台 thread/conversation key
    ChatID    string  // Feishu: chat ID
    UserID    string  // 跨平台 user identity
}

// DerivePlatformSessionKey — 平台消息使用（Feishu/Slack）
func DerivePlatformSessionKey(ownerID string, wt worker.WorkerType, ctx PlatformContext) string {
    var parts []string
    parts = append(parts, ctx.Platform)
    if ctx.Platform == "slack" {
        if ctx.TeamID != "" { parts = append(parts, ctx.TeamID) }
        if ctx.ChannelID != "" { parts = append(parts, ctx.ChannelID) }
        if ctx.ThreadTS != "" { parts = append(parts, ctx.ThreadTS) }
    } else if ctx.Platform == "feishu" {
        if ctx.ChatID != "" { parts = append(parts, ctx.ChatID) }
        if ctx.ThreadTS != "" { parts = append(parts, ctx.ThreadTS) }
    }
    if ctx.UserID != "" { parts = append(parts, ctx.UserID) }
    name := ownerID + "|" + string(wt) + "|" + strings.Join(parts, "|")
    id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
    return id.String()
}
```

**依赖**: `github.com/google/uuid`（检查是否已引入）

### 5.4 internal/session/manager.go

```go
// ClearContext 清空会话上下文。
// 用于 control.reset 操作：Gateway 层清空 SessionInfo.Context。
// Worker 自身运行时的上下文清空由 Worker.ResetContext() 负责（in-place 或 terminate+start）。
func (m *Manager) ClearContext(ctx context.Context, sessionID string) error {
    if m == nil {
        return ErrSessionNotFound
    }
    ms := m.getManagedSession(sessionID)
    if ms == nil {
        return ErrSessionNotFound
    }

    ms.mu.Lock()
    defer ms.mu.Unlock()

    ms.info.Context = map[string]any{}
    ms.info.UpdatedAt = time.Now()

    return m.store.Upsert(ctx, &ms.info)
}
```

### 5.5 internal/gateway/conn.go:performInit

```go
// DeriveSessionKey 现在接受 4 参数（含 workDir）
sessionID := session.DeriveSessionKey(c.userID, initData.WorkerType, initData.SessionID, workDir)

// ResumeSession 现在接受 workDir 参数
if err := c.starter.ResumeSession(context.Background(), sessionID, workDir); err != nil {
    ...
}
```

### 5.6 internal/gateway/SessionStarter 接口

```go
type SessionStarter interface {
    StartSession(ctx context.Context, id, userID, botID string,
        wt worker.WorkerType, allowedTools []string, workDir string) error
    ResumeSession(ctx context.Context, id string, workDir string) error
}
```

### 5.7 internal/gateway/bridge.go:StartPlatformSession（v1.4 修复）

```go
func (b *Bridge) StartPlatformSession(ctx, sessionID, ownerID, workerType, workDir) error {
    _, err := b.sm.Get(sessionID)
    if err == nil {
        if w := b.sm.GetWorker(sessionID); w != nil {
            return nil
        }
        // v1.4 修复: orphan → ResumeSession（而非 Delete + StartSession）
        return b.ResumeSession(ctx, sessionID, workDir)
    }
    return b.StartSession(ctx, sessionID, ownerID, "", wt, nil, workDir)
}
```

### 5.8 internal/messaging/bridge.go（v1.4 重构）

```go
// 删除旧的字符串格式常量
// const SessionIDFormatSlack  = "slack:%s:%s:%s:%s"
// const SessionIDFormatFeishu = "feishu:%s:%s:%s"

// 使用 DerivePlatformSessionKey
func (b *Bridge) MakeFeishuEnvelope(chatID, threadTS, userID, text string) *Envelope {
    sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
        Platform: "feishu", ChatID: chatID, ThreadTS: threadTS, UserID: userID,
    })
    return b.makeEnvelope(sessionID, userID, text, map[string]any{"platform": "feishu", "chat_id": chatID})
}

func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text string) *Envelope {
    sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
        Platform: "slack", TeamID: teamID, ChannelID: channelID, ThreadTS: threadTS, UserID: userID,
    })
    return b.makeEnvelope(sessionID, userID, text, map[string]any{"platform": "slack", "team_id": teamID, "channel_id": channelID})
}
```

---

## 6. AEP 协议消息格式

### 6.1 control.reset

**请求**:
```json
{
  "id": "msg-010",
  "version": "aep/v1",
  "seq": 5,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "control",
    "data": {
      "action": "reset",
      "reason": "user_requested"
    }
  }
}
```

**服务端响应**:
```json
{
  "id": "msg-011",
  "version": "aep/v1",
  "seq": 6,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "state",
    "data": {
      "state": "running",
      "message": "context_reset"
    }
  }
}
```

### 6.2 control.gc

**请求**:
```json
{
  "id": "msg-020",
  "version": "aep/v1",
  "seq": 10,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "control",
    "data": {
      "action": "gc",
      "reason": "user_idle"
    }
  }
}
```

**服务端响应**:
```json
{
  "id": "msg-021",
  "version": "aep/v1",
  "seq": 11,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "state",
    "data": {
      "state": "terminated",
      "message": "session_archived"
    }
  }
}
```

---

## 7. 错误处理

### 7.1 现有错误码（已满足，无需新增）

| 错误码 | 适用场景 |
|--------|---------|
| `SESSION_NOT_FOUND` | session 不存在 |
| `SESSION_BUSY` | 向 RUNNING 状态发送消息 |
| `UNAUTHORIZED` | 所有权校验失败 |
| `INTERNAL_ERROR` | 内部错误 |
| `PROTOCOL_VIOLATION` | 未知 control action |

---

## 8. 验收标准（AC）

### AC-1：Session ID 确定性映射

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-1.1 | `DeriveSessionKey("u1", "claude_code", "s1")` 连续调用 N 次（≥1000），返回完全相同的 UUIDv5 字符串 | 单元测试 loop |
| AC-1.2 | `DeriveSessionKey("u1", "claude_code", "s1")` ≠ `DeriveSessionKey("u2", "claude_code", "s1")` | 单元测试 |
| AC-1.3 | `DeriveSessionKey("u1", "claude_code", "s1")` ≠ `DeriveSessionKey("u1", "s1")` | 单元测试 |
| AC-1.4 | `DeriveSessionKey("u1", "claude_code", "s1")` ≠ `DeriveSessionKey("u1", "claude_code", "s2")` | 单元测试 |
| AC-1.5 | 输出格式匹配正则 `/[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/` | 单元测试 |
| AC-1.6 | 不同机器（不同主机名/IP）上相同三元组生成相同 UUID | 多机验证脚本 |

**通过标准**：AC-1.1–AC-1.5 全通过；AC-1.6 为可选（RFC 4122 UUIDv5 算法保证）

### AC-2：init 流程使用 DeriveSessionKey

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-2.1 | `init{client_session_id: "my-chat-001", worker_type: "claude_code"}` → `DeriveSessionKey` 计算 sessionID | 集成测试：mock/拦截 `DeriveSessionKey` 调用，验证传入参数 |
| AC-2.2 | 相同 `(owner_id, worker_type, client_session_id)` 两次 init → 同一服务端 session（幂等） | 集成测试：创建后立即再次 init，验证 session 数量不增加 |
| AC-2.3 | 相同 `client_session_id` 不同 `worker_type` → 不同 session | 集成测试 |
| AC-2.4 | `client_session_id` 为空字符串 → `DeriveSessionKey` 仍返回合法 UUID（空串参与哈希） | 单元测试 |
| AC-2.5 | 新建 session 的初始状态为 `created` | 集成测试 |

**通过标准**：AC-2.1–AC-2.5 全通过

### AC-3：ClearContext

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-3.1 | 有 Context 的 session 调用 `ClearContext` → `Context = {}`（空 map） | 单元测试 |
| AC-3.2 | `ClearContext` 后 `UpdatedAt` 更新为当前时间 | 单元测试 |
| AC-3.3 | `ClearContext` 持久化到 Store（重启后仍为空） | 集成测试：ClearContext → 重启 → Get → Context 为空 |
| AC-3.4 | 对不存在的 sessionID 调用 `ClearContext` → 返回 `ErrSessionNotFound` | 单元测试 |
| AC-3.5 | Context 中原有 key 全部消失（`len(ctx) == 0`） | 单元测试 |
| AC-3.6 | `ClearContext` 不会改变 Session 的其他字段（State, OwnerID 等） | 单元测试 |

**通过标准**：AC-3.1–AC-3.6 全通过

### AC-4：control.reset

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-4.1 | 向 `RUNNING` 状态 session 发送 `control.reset` → 状态变为 `running` | 集成测试 |
| AC-4.2 | `control.reset` 后 `Session.Context = {}` | 集成测试 |
| AC-4.3 | `control.reset` 调用 `Worker.ResetContext()` | mock 测试：验证该方法被调用 |
| AC-4.4 | `control.reset` 后发送 `input` → 收到 `done`，Worker 输出无历史对话内容 | 集成测试 |
| AC-4.5 | 非 owner 发送 `control.reset` → 返回 `UNAUTHORIZED`，状态不变 | 集成测试 |
| AC-4.6 | 向 `TERMINATED` 状态发送 `control.reset` → 返回错误（前置条件不满足） | 集成测试 |
| AC-4.7 | 响应消息为 `event.type="state"`，`data.state="running"`，`data.message="context_reset"` | 集成测试：检查 WS 响应 |
| AC-4.8 | `control.reset` 期间 Worker.ResetContext 失败 → 状态不变，返回 `INTERNAL_ERROR` | mock 测试 |
| AC-4.9 | 无 attached Worker 时 `control.reset` 仍成功（只清 Context） | 集成测试 |

**通过标准**：AC-4.1–AC-4.9 全通过

### AC-5：control.gc

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-5.1 | 向 `RUNNING` 状态 session 发送 `control.gc` → 状态变为 `terminated` | 集成测试 |
| AC-5.2 | `control.gc` 调用 `Worker.Terminate()` | mock 测试：验证该方法被调用 |
| AC-5.3 | `control.gc` 后 `sm.GetWorker(sessionID)` 返回 `nil`（Worker 已 detach） | 集成测试 |
| AC-5.4 | `control.gc` 后 Worker 进程已退出 | 集成测试：检查进程表 |
| AC-5.5 | `control.gc` 后同一 `(owner_id, worker_type, client_session_id)` 再次 init → session 可恢复（TERMINATED → RUNNING） | 集成测试 |
| AC-5.6 | 非 owner 发送 `control.gc` → 返回 `UNAUTHORIZED`，状态不变 | 集成测试 |
| AC-5.7 | 响应消息为 `event.type="state"`，`data.state="terminated"`，`data.message="session_archived"` | 集成测试：检查 WS 响应 |
| AC-5.8 | 向 `TERMINATED` 状态再次发送 `control.gc` → 幂等：仍返回成功（idempotent） | 集成测试 |
| AC-5.9 | 向 `DELETED` 状态发送 `control.gc` → 返回 `SESSION_NOT_FOUND` | 集成测试 |

**通过标准**：AC-5.1–AC-5.9 全通过

### AC-6：状态流转

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-6.1 | `reset`: `RUNNING` → `RUNNING`（Context 清空，Worker 重建） | 集成测试 |
| AC-6.2 | `gc`: `RUNNING` → `TERMINATED` | 集成测试 |
| AC-6.3 | `gc`: `IDLE` → `TERMINATED` | 集成测试 |
| AC-6.4 | `resume`: `TERMINATED` → `RUNNING`（init 时自动触发） | 集成测试 |
| AC-6.5 | 所有非法状态转换被拒绝（ValidTransitions 表之外的转换） | 单元测试：遍历 ValidTransitions |

**通过标准**：AC-6.1–AC-6.5 全通过

### AC-7：Worker.ResetContext 接口

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-7.1 | `Worker` interface 新增 `ResetContext(ctx context.Context) error` 方法 | 编译检查 |
| AC-7.2 | ClaudeCodeWorker: `ResetContext` 执行 `Terminate()` + `Start()` | mock 测试 |
| AC-7.3 | OpenCodeServerWorker: `ResetContext` 执行 `Terminate()` + `Start()` | mock 测试 |
| AC-7.4 | OpenCodeSrvWorker: `ResetContext` 发送 HTTP POST `/session/<id>/reset` | mock 测试 |
| AC-7.5 | 所有 Worker adapter 实现 `var _ Worker = (*Worker)(nil)` 编译验证 | 编译检查 |

**通过标准**：AC-7.1–AC-7.5 全通过

### AC-8：新增常量

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-8.1 | `pkg/events/events.go` 包含 `ControlActionReset = "reset"` | 编译检查 |
| AC-8.2 | `pkg/events/events.go` 包含 `ControlActionGC = "gc"` | 编译检查 |
| AC-8.3 | `handleControl` 对未知 action 返回 `PROTOCOL_VIOLATION` | 单元测试 |

**通过标准**：AC-8.1–AC-8.3 全通过

### AC-9：全流程端到端

| ID | 描述 | 验证方法 |
|----|------|---------|
| AC-9.1 | reset 后 input：init → input("Q1") → done("A1") → reset → input("Q2") → done("A2") → "Q2"回答中无"A1"内容 | E2E 测试 |
| AC-9.2 | gc 后 resume：init → input → done → gc → init(resume) → 历史消息全部恢复 | E2E 测试 |
| AC-9.3 | 并发 reset 请求（同 session）：第二次 reset 在第一次完成后执行，状态最终一致 | 并发测试 |
| AC-9.4 | reset/gc 后 WS 连接保持，客户端收到确认 state 事件 | E2E 测试 |

**通过标准**：AC-9.1–AC-9.4 全通过

---

## 9. 测试用例

### 8.1 单元测试

| 测试 | 输入 | 预期 |
|------|------|------|
| `DeriveSessionKey` 确定性 | `("u1", "claude_code", "s1")` × N | 每次相同 UUID |
| `DeriveSessionKey` 差异 | 不同三元组 | 不同 UUID |
| `DeriveSessionKey` UUIDv5 格式 | 任意输入 | `/[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/` |
| `ClearContext` | 有 Context 的 session | Context = `{}` |
| `handleReset` | RUNNING session | State → RUNNING, Context = `{}`, Worker.ResetContext() 被调用 |
| `handleGC` | RUNNING session | State → TERMINATED, Worker.Terminate() 被调用 |
| Worker.ResetContext | CLI Worker | Terminate() + Start() |
| Worker.ResetContext | Server Worker | 发送 in-place reset 请求 |

### 8.2 集成测试

| 测试 | 场景 |
|------|------|
| `control.reset` 全流程 | init → input → control.reset → State=RUNNING → input（新 Worker）|
| `control.gc` 全流程 | init → input → control.gc → State=TERMINATED → init(resume) |
| reset 后 input | reset → input → 全新对话，无历史 |
| gc 后 resume | gc → init(resume) → 历史恢复 |
| 非法 reset | 向 TERMINATED 发 reset → `PROTOCOL_VIOLATION` |
| 所有权校验 | 非 owner 发 reset/gc → `UNAUTHORIZED` |

---

## 10. 实现计划

> **验收依据**：第 8 节 AC（Acceptance Criteria），实现完成须通过全部 AC-1 至 AC-9

### 阶段一：核心变更 ✅ 已完成
- [x] `pkg/events/events.go` — 新增 `ControlActionReset` / `ControlActionGC`（events.go:227-228）
- [x] `internal/session/key.go` — 新建 `DeriveSessionKey()`（UUIDv5）+ 测试
- [x] `internal/session/manager.go` — 新增 `ClearContext()`（manager.go:464）+ 6个单元测试
- [x] `internal/gateway/conn.go` — `performInit` 调用 `DeriveSessionKey`（conn.go:237）
- [x] `internal/gateway/handler.go` — `handleReset`（handler.go:273）+ `handleGC`（handler.go:312）+ 33+测试
- [x] `internal/worker/worker.go` — `Worker.ResetContext()` 接口定义（worker.go:119,124）
- [x] 所有 Worker adapters 实现 `ResetContext`: ClaudeCode, OpenCodeSrv, NoOp
- [x] 单元测试覆盖: manager_test.go (ClearContext 6测试), handler_test.go (handleReset/GC 33测试), key_test.go

### 阶段二：文档 ✅ 已完成
- [x] `AEP-v1-Protocol.md` 已更新:
  - Control action 表格新增 `reset`（3.4 表格 line 171）和 `gc`（line 172）
  - 新增 3.4.4 `control.reset` 章节（line 286-319）：JSON格式、服务端行为、详细流程
  - 新增 3.4.5 `control.gc` 章节（line 321-354）：JSON格式、服务端行为、详细流程
  - Minimal Compliance 章节（line 837）更新 `control` 列表包含 `reset`/`gc`
- [x] `WebSocket-Full-Duplex-Flow.md` 已更新:
  - Client → Server Events 表格（line 348）新增 `reset`/`gc`
  - Server → Client Events 表格（line 355,361）更新 state 和 control payload 描述

---

## 11. Changelog

| 日期 | 版本 | 变更 |
|------|------|------|
| 2026-04-18 | 1.4 | **统一 session 映射**: 新增 `DerivePlatformSessionKey()` 供 Feishu/Slack 使用，与 Web 的 `DeriveSessionKey` 共存；所有平台 session ID 统一为 UUIDv5 格式；新增 `PlatformContext` 结构体隔离各平台字段；Slack adapter 修复 teamID/threadTS 提取；**修复 orphan 处理**: `StartPlatformSession` orphan 路径从 `Delete + StartSession` 改为 `ResumeSession`，使 gateway 重启后 worker session 内部状态可恢复；`ResumeSession` 增加 `workDir` 参数 |
| 2026-04-07 | 1.3 | 新增第 8 节 AC（Acceptance Criteria），共 9 组 44 条验收标准；章节重新编号 |
| 2026-04-06 | 1.2 | 移除向后兼容逻辑；改用 UUIDv5 算法替代 SHA-256 hex |
| 2026-04-06 | 1.1 | 交叉复核源码，精确到文件/行号；明确 minimal change set |
| 2026-04-06 | 1.0 | 初始版本 |

---

## 12. 相关文档

- [[architecture/AEP-v1-Protocol]] — AEP v1 协议规范
- [[architecture/WebSocket-Full-Duplex-Flow]] — WebSocket 全双工通信流程
- [[specs/Worker-ClaudeCode-Spec]] — Claude Code Worker 实现
- [[management/Admin-API-Design]] — Admin API 设计
