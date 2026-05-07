---
type: spec
tags:
  - project/HotPlex
  - gateway/handler
  - gateway/bridge
  - gateway/conn
  - messaging/bridge
  - messaging/platform-adapter
  - eventstore
  - bug/high
date: 2026-05-07
status: proposed
priority: high
estimated_hours: 4
---

# Inbound 事件未存储修复规格书

> 版本: v1.0
> 日期: 2026-05-07
> 状态: Proposed
> 影响: Slack / 飞书平台的用户输入事件从未被持久化，导致 v_turns 视图缺少 user turns、会话重放不完整、审计追踪断裂

---

## 1. 概述

### 1.1 问题

HotPlex Gateway 的 events 表中 **0 条 inbound 事件**（全部 3,969 条均为 outbound）。根因是 Messaging 层（Slack/飞书）构造的 AEP envelope 未分配 `Seq`，导致用户输入事件以 `seq=0` 的状态被处理。

同时 `handler.go` 的 interaction response 分支直接 return 而不调用 `CaptureInbound`，导致 permission/question/elicitation 响应也不被存储。

### 1.2 影响范围

| 影响 | 说明 |
|------|------|
| `v_turns` 视图 | user turns 为空，无法看到完整对话（当前仅有 64 条 assistant turns） |
| 会话重放 | 缺少用户输入，重放不完整 |
| 审计追踪 | 无法追溯用户做了什么操作 |
| 统计分析 | 无法计算 per-turn 的用户消息和交互 |

### 1.3 数据流对比

**WebSocket 路径（正常）**：

```
conn.go:178    env.Seq = c.hub.NextSeq(sessionID)     // Seq 正确递增
    ↓
handler.go:224 CaptureInbound(sessionID, env.Seq, ...) // Seq > 0, 存储成功
```

**Messaging 路径（Bug）**：

```
bridge.go:106  makeEnvelope()                           // Seq 未设置，默认 0
    ↓
bridge.go:97   handler.Handle(ctx, env)                 // Seq=0 传入
    ↓
handler.go:224 CaptureInbound(sessionID, env.Seq, ...) // Seq=0
```

---

## 2. 根因分析

### 2.1 主因：Messaging Bridge Envelope 缺少 Seq 分配

**位置**: `internal/messaging/bridge.go:106-119` (`makeEnvelope`)

```go
func (b *Bridge) makeEnvelope(sessionID, ownerID, text string, metadata map[string]any) *events.Envelope {
    return &events.Envelope{
        Version:   events.Version,
        ID:        aep.NewID(),
        SessionID: sessionID,
        // Seq 字段未设置! 默认为 0
        Event: events.Event{
            Type: events.Input,
            Data: map[string]any{"content": ..., "metadata": ...},
        },
        OwnerID: ownerID,
    }
}
```

`HubInterface` 接口（`platform_adapter.go:145-147`）仅暴露 `JoinPlatformSession`，缺少 `NextSeq` 方法，messaging 层无法为 envelope 分配 seq。

### 2.2 次因：Interaction Response 分支跳过 CaptureInbound

**位置**: `internal/gateway/handler.go:98-128`

interaction response（permission_response / question_response / elicitation_response）分支在投递给 worker 后直接 `return nil`，不经过第 222-224 行的 `CaptureInbound` 调用。

### 2.3 约束：captureSyntheticEvent 已正确使用 NextSeq

`bridge_forward.go:422` 的 `captureSyntheticEvent` 正确调用 `b.hub.NextSeq(sessionID)`，说明 Bridge 已经持有 hub 引用且知道如何分配 seq。此模式可复用。

---

## 3. 修复方案

### 3.1 Fix 1: HubInterface 添加 NextSeq（主修复）

**修改文件**:

#### 3.1.1 `internal/messaging/platform_adapter.go`

扩展 `HubInterface` 接口：

```go
type HubInterface interface {
    JoinPlatformSession(sessionID string, pc PlatformConn)
    NextSeq(sessionID string) int64
}
```

#### 3.1.2 `internal/messaging/bridge.go`

在 `Handle()` 方法中，调用 `handler.Handle()` 前分配 Seq：

```go
func (b *Bridge) Handle(ctx context.Context, env *events.Envelope, pc PlatformConn) error {
    // ... existing code ...

    // Assign sequence number (messaging path doesn't go through conn.ReadPump).
    if b.hub != nil {
        env.Seq = b.hub.NextSeq(env.SessionID)
    }

    return b.handler.Handle(ctx, env)
}
```

或在 `makeEnvelope` 中分配（需 bridge 持有 hub 引用）。

#### 3.1.3 影响分析

- `gateway.Hub` 已实现 `NextSeq` 方法，无需修改
- `HubInterface` 消费方只有 `PlatformAdapter`，接口扩展兼容
- 需验证 Slack/Feishu adapter 的 mock 测试是否需要更新

### 3.2 Fix 2: Interaction Response 存储（次修复）

**修改文件**: `internal/gateway/handler.go`

在 interaction response 分支的 worker 投递后添加 CaptureInbound：

```go
if md["permission_response"] != nil ||
    md["question_response"] != nil ||
    md["elicitation_response"] != nil {
    // ... existing worker delivery code ...

    // Capture interaction response for audit/replay.
    if h.bridge != nil {
        h.bridge.CaptureInbound(env.SessionID, env.Seq, events.Input, env.Event.Data)
    }
    return nil
}
```

### 3.3 不修改的部分

- **Help/Control/WorkerCommand** — 这些在 handler.go 中被转换为其他事件类型，不需要存储为 input。它们已有各自的 seq 分配和路由。
- **eventstore.Collector / Capture** — 存储层完全支持 inbound，无需修改。
- **DDL / 视图** — 表结构和索引已支持 inbound，无需迁移。

---

## 4. 验证方案

### 4.1 单元测试

- [ ] `messaging/bridge_test.go` — 验证 `Handle()` 后 envelope.Seq > 0
- [ ] `gateway/handler_test.go` — 验证 interaction response 分支调用 CaptureInbound
- [ ] Mock `HubInterface` 更新 — 添加 `NextSeq` mock 实现

### 4.2 集成验证

- [ ] 通过 Slack 发送消息，查询 `SELECT * FROM events WHERE direction='inbound'` 确认存储
- [ ] 通过飞书发送消息，确认 inbound 事件存储
- [ ] `v_turns` 视图同时包含 user 和 assistant turns
- [ ] `make test` 通过
- [ ] `make lint` 无新警告

### 4.3 数据验证 SQL

```sql
-- 修复后验证
SELECT direction, COUNT(*) FROM events GROUP BY direction;
-- 预期: inbound > 0, outbound > 0

SELECT role, COUNT(*) FROM v_turns GROUP BY role;
-- 预期: user > 0, assistant > 0

-- 确认无 seq=0 的 inbound 事件
SELECT COUNT(*) FROM events WHERE direction='inbound' AND seq=0;
-- 预期: 0
```

---

## 5. 风险评估

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| HubInterface 扩展破坏 mock | 低 | 低 | 更新 mock 实现 |
| Seq 分配与 WS 路径冲突 | 无 | — | Hub.NextSeq 是 per-session 原子操作，两条路径共享同一计数器，天然兼容 |
| 历史 seq=0 数据修复 | 低 | 低 | 无需回填，旧数据 v_turns 仍然正确（user 表无数据不会报错） |

---

## 6. 实现优先级

| Fix | Priority | Effort | Files | Risk |
|-----|----------|--------|-------|------|
| Fix 1: HubInterface + Seq 分配 | **P0** | Small | 2-3 files | Low |
| Fix 2: Interaction Response 存储 | **P1** | Small | 1 file | Low |

**推荐**: 一次 PR 完成两个 fix。
