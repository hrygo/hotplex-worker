---
type: spec
tags:
  - project/HotPlex
  - session/store
  - eventstore
  - refactor
date: 2026-05-04
status: proposed
priority: P2
estimated_hours: 6
---

# 合并 conversation 表到 events 表规格书

> 版本: v1.0
> 日期: 2026-05-04
> 状态: Proposed
> 前置: events 表已上线并稳定运行

---

## 1. 概述

### 1.1 问题

HotPlex 维护两张独立的持久化表，观察同一条 Worker 事件流但写入独立：

| 表 | 数据库 | 粒度 | 写入时机 | 消费者 |
|----|--------|------|---------|--------|
| `conversation` | `hotplex.db` | Turn 级（用户输入 + AI 回复 + 统计） | `convStore.Append()` | `GET /api/sessions/{id}/history`, `GET /admin/sessions/{id}/stats` |
| `events` | `events.db` | 事件级（AEP 协议：input/message/done/tool_call/...） | `collector.Capture()` | 未来审计/调试/回放 |

两张表的数据存在大量重叠（用户输入内容、AI 回复文本、token/cost 统计），且写入路径不同步，存在数据一致性风险。

### 1.2 结论

经逐字段对比和实际数据验证（12/13 字段完全覆盖），**conversation 表是 events 表的严格子集**，可以安全合并。

唯一缺口 `source` 字段（`crash`/`timeout`/`fresh_start`）可通过在 events 表增加字段解决。

---

## 2. 字段覆盖分析

### 2.1 conversation 字段 → events 等效映射

| # | conversation 字段 | events 等效来源 | 类型 | 覆盖 |
|---|------------------|----------------|------|------|
| 1 | `role` = `user` | `type = 'input'` | 事件类型 | ✅ |
| 2 | `role` = `assistant` | `type = 'done'` | 事件类型 | ✅ |
| 3 | `content` (用户) | `input` 的 `data.content` | JSON 字段 | ✅ |
| 4 | `content` (AI) | 相邻 `message` 的 `data.content`（id 区间关联） | JSON 字段 | ✅ |
| 5 | `platform` | `input` 的 `data.metadata.platform` | JSON 字段 | ✅ |
| 6 | `user_id` | `input` 的 `data.metadata.user_id` | JSON 字段 | ✅ |
| 7 | `model` | `done` 的 `data.stats._session.model_name` | JSON 字段 | ✅ |
| 8 | `success` | `done` 的 `data.success` | JSON 字段 | ✅ |
| 9 | `tokens_in` | `done` 的 `data.stats._session.turn_input_tok` | JSON 字段 | ✅ |
| 10 | `tokens_out` | `done` 的 `data.stats._session.turn_output_tok` | JSON 字段 | ✅ |
| 11 | `duration_ms` | `done` 的 `data.stats._session.turn_duration_ms` | JSON 字段 | ✅ |
| 12 | `cost_usd` | `done` 的 `data.stats._session.turn_cost_usd` | JSON 字段 | ✅ |
| 13 | `tool_call_count` | `done` 的 `data.stats._session.tool_call_count` | JSON 字段 | ✅ |
| 14 | `tools_json` | `done` 的 `data.stats._session.tool_names`（结构更丰富） | JSON 字段 | ✅ |
| 15 | `metadata_json` | 从未被写入（始终为空） | — | N/A |
| 16 | `source` | **events 无对应** | — | ❌ 缺失 |

### 2.2 数据验证结果

对 session `474115bb` 的 5 条 assistant 记录做 1:1 数值对比：

```
seq=11  conv: tok_in=115217 tok_out=6455  cost=0.539  tools=3
seq=11  evnt: tok_in=115217 tok_out=6455  cost=0.539  tools=3  ✅

seq=18  conv: tok_in=134878 tok_out=8760  cost=0.688  tools=102
seq=18  evnt: tok_in=134878 tok_out=8760  cost=0.688  tools=102  ✅

seq=91  conv: tok_in=145228 tok_out=6577  cost=1.965  tools=32
seq=91  evnt: tok_in=145228 tok_out=6577  cost=1.965  tools=32  ✅
```

全部一致。

### 2.3 events 表独有数据（conversation 不具备）

| 数据 | 来源 | 价值 |
|------|------|------|
| `reasoning` 事件 | 思考过程 | 调试/审计 |
| `tool_call` / `tool_result` | 工具调用详情 | 调试/审计 |
| `message.delta` | 流式输出片段 | 回放/调试 |
| `done.stats.model_usage` | 每模型详细用量 | 成本分析 |
| `done.stats._session.context_fill` | 上下文窗口占用 | 容量监控 |
| `input.metadata` | 平台/渠道/用户元数据 | 多租户分析 |

---

## 3. 修改方案

### 3.1 Phase 1: events 表增加 `source` 字段

**文件**: `internal/eventstore/sql/migrations/` — 新增迁移文件

```sql
-- 002_add_source.sql
ALTER TABLE events ADD COLUMN source TEXT NOT NULL DEFAULT 'normal'
  CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start'));
```

**写入点调整**: `collector.Capture()` 增加 `source` 参数，默认 `'normal'`。

### 3.2 Phase 2: 创建 `turns` VIEW 替代 conversation 查询

**文件**: `internal/eventstore/sql/migrations/` — 迁移中创建视图

```sql
-- 003_create_turns_view.sql

-- 用户输入视图（替代 conversation WHERE role='user'）
CREATE VIEW v_turns_user AS
SELECT
  e.session_id,
  e.seq,
  'user' AS role,
  json_extract(e.data, '$.content') AS content,
  COALESCE(json_extract(e.data, '$.metadata.platform'), '') AS platform,
  COALESCE(json_extract(e.data, '$.metadata.user_id'), '') AS user_id,
  '' AS model,
  NULL AS success,
  e.source,
  NULL AS tools_json,
  0 AS tool_call_count,
  0 AS tokens_in,
  0 AS tokens_out,
  0 AS duration_ms,
  0.0 AS cost_usd,
  e.created_at
FROM events e
WHERE e.type = 'input' AND e.direction = 'inbound';

-- AI 回复视图（替代 conversation WHERE role='assistant'）
CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id,
  d.seq,
  'assistant' AS role,
  COALESCE(m.content, '') AS content,
  COALESCE(i.platform, '') AS platform,
  COALESCE(i.user_id, '') AS user_id,
  COALESCE(json_extract(d.data, '$.stats._session.model_name'), '') AS model,
  json_extract(d.data, '$.success') AS success,
  d.source,
  json_extract(d.data, '$.stats._session.tool_names') AS tools_json,
  COALESCE(json_extract(d.data, '$.stats._session.tool_call_count'), 0) AS tool_call_count,
  COALESCE(json_extract(d.data, '$.stats._session.turn_input_tok'), 0) AS tokens_in,
  COALESCE(json_extract(d.data, '$.stats._session.turn_output_tok'), 0) AS tokens_out,
  COALESCE(json_extract(d.data, '$.stats._session.turn_duration_ms'), 0) AS duration_ms,
  COALESCE(json_extract(d.data, '$.stats._session.turn_cost_usd'), 0.0) AS cost_usd,
  d.created_at
FROM events d
-- 关联最近一条 input 获取 platform/user_id
LEFT JOIN (
  SELECT session_id,
    json_extract(data, '$.metadata.platform') AS platform,
    json_extract(data, '$.metadata.user_id') AS user_id,
    ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY id DESC) AS rn
  FROM events WHERE type = 'input'
) i ON i.session_id = d.session_id AND i.rn = 1
-- 关联前序 message 获取 AI 回复文本
LEFT JOIN (
  SELECT m.id, m.session_id,
    group_concat(json_extract(m.data, '$.content'), char(10)) AS content
  FROM events m
  WHERE m.type = 'message'
  GROUP BY m.session_id, (SELECT MAX(d2.id) FROM events d2 WHERE d2.type='done' AND d2.id <= m.id AND d2.session_id = m.session_id)
) m ON m.session_id = d.session_id AND m.id < d.id
WHERE d.type = 'done' AND d.direction = 'outbound';

-- 合并视图（完全替代 conversation 表）
CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at;
```

### 3.3 Phase 3: 迁移 API 消费者到 events 视图

**`GET /api/sessions/{id}/history`** — `internal/gateway/api.go:289-355`

当前：`g.convStore.GetBySession()` → conversation 表查询
改为：`g.eventStore.QueryTurns()` → `v_turns` 视图查询

**`GET /admin/sessions/{id}/stats`** — `internal/admin/sessions.go:136-159`

当前：`a.convStore.SessionStats()` → conversation 表聚合
改为：`a.eventStore.SessionStats()` → `v_turns_assistant` 视图聚合

在 `EventStore` 接口上新增方法：

```go
type TurnRecord struct {
    Seq          int64   `json:"seq"`
    Role         string  `json:"role"`
    Content      string  `json:"content"`
    Platform     string  `json:"platform"`
    UserID       string  `json:"user_id"`
    Model        string  `json:"model"`
    Success      *bool   `json:"success"`
    Source       string  `json:"source"`
    ToolCount    int     `json:"tool_call_count"`
    TokensIn     int     `json:"tokens_in"`
    TokensOut    int     `json:"tokens_out"`
    DurationMs   int64   `json:"duration_ms"`
    CostUSD      float64 `json:"cost_usd"`
    CreatedAt    int64   `json:"created_at"`
}

type EventStore interface {
    // ... existing methods ...
    GetTurns(ctx context.Context, sessionID string, limit, offset int) ([]*TurnRecord, error)
    GetTurnsBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*TurnRecord, error)
    SessionTurnStats(ctx context.Context, sessionID string) (*TurnSessionStats, error)
}
```

### 3.4 Phase 4: 废弃 ConversationStore

移除文件和调用：

| 移除目标 | 文件 |
|---------|------|
| `ConversationStore` 接口和实现 | `internal/session/conversation_store.go` |
| SQL queries | `internal/session/sql/queries/conversation.*` |
| migration DDL | `internal/session/sql/migrations/001_init.sql` 中 conversation 部分 |
| handler.go Append 调用 | `internal/gateway/handler.go:189-198` |
| bridge_forward.go Append 调用 | `internal/gateway/bridge_forward.go:231-252`, `:386-406` |
| bridge_worker.go Append 调用 | `internal/gateway/bridge_worker.go:204-214` |
| DI 注入 | `cmd/hotplex/gateway_run.go` ConvStore 字段和注入 |

### 3.5 Phase 5: 补充 events 写入点

当前 events 通过 `collector.Capture()` 写入，需补充 crash/timeout/fresh_start 场景：

| 场景 | 当前 events 覆盖 | 需补充 |
|------|-----------------|--------|
| 正常 input | ✅ `CaptureInbound()` | — |
| 正常 done | ✅ `forwardEvents()` 转发 | — |
| Worker 崩溃 | ❌ | 在 `handleWorkerExit()` 中写入 source=`crash` 事件 |
| Turn 超时 | ❌ | 在 turnTimer handler 中写入 source=`timeout` 事件 |
| Fresh-start 重投递 | ❌ | 在 `attemptResumeFallback()` 中写入 source=`fresh_start` 事件 |

### 3.6 清理：dead code

| 目标 | 说明 |
|------|------|
| `conversation.max_seq_by_session.sql` | 从未被引用的查询 |
| `DeleteExpired` 方法 | 从未被调用 |
| `DeleteBySession` 方法 | 仅测试使用（CASCADE 替代） |
| `idx_conv_user` / `idx_conv_platform` 索引 | 随表一起删除 |

---

## 4. 修改文件清单

| # | 文件 | 修改类型 | 说明 |
|---|------|---------|------|
| 1 | `internal/eventstore/sql/migrations/002_add_source.sql` | 新增 | events 表增加 source 列 |
| 2 | `internal/eventstore/sql/migrations/003_create_turns_view.sql` | 新增 | 创建 v_turns/v_turns_user/v_turns_assistant 视图 |
| 3 | `internal/eventstore/store.go` | 修改 | 新增 GetTurns/GetTurnsBefore/SessionTurnStats 方法 |
| 4 | `internal/eventstore/collector.go` | 修改 | Capture 增加 source 参数 |
| 5 | `internal/gateway/api.go` | 修改 | history 端点改用 eventStore |
| 6 | `internal/admin/sessions.go` | 修改 | stats 端点改用 eventStore |
| 7 | `internal/admin/admin.go` | 修改 | ConvStoreProvider → EventStoreProvider |
| 8 | `internal/gateway/bridge_forward.go` | 修改 | 移除 convStore.Append，补充 crash/timeout 事件写入 |
| 9 | `internal/gateway/bridge_worker.go` | 修改 | 移除 convStore.Append，补充 fresh_start 事件写入 |
| 10 | `internal/gateway/handler.go` | 修改 | 移除 convStore.Append |
| 11 | `cmd/hotplex/gateway_run.go` | 修改 | 移除 ConvStore DI |
| 12 | `internal/session/conversation_store.go` | 删除 | 整个文件 |
| 13 | `internal/session/sql/queries/conversation.*` | 删除 | 所有 conversation 查询文件 |

---

## 5. 收益与风险

### 5.1 收益

| 维度 | 改善 |
|------|------|
| **数据一致性** | 消除双源写入不一致风险，单点真实 |
| **代码量** | 净减 ~300 行（conversation_store.go + SQL + 调用点） |
| **写入开销** | 每次 Turn 从 2 次 SQLite 写（conversation + events）降为 1 次 |
| **存储** | 消除重复数据，conversation 119 条记录全部可从 events 重建 |
| **可维护性** | 单一持久化路径，减少认知负担 |

### 5.2 风险

| 风险 | 缓解 |
|------|------|
| VIEW 查询性能（JOIN + 聚合） | `v_turns` 按主键/索引关联，且 history 端点已分页；可用物化视图替代 |
| migration 兼容性 | ALTER TABLE ADD COLUMN 是 SQLite 兼容操作；VIEW 可随时 DROP/CREATE |
| API 响应格式变化 | `TurnRecord` 字段与 `ConversationRecord` 保持 JSON 兼容 |

---

## 6. 测试要求

### 6.1 单元测试

| 测试 | 覆盖点 |
|------|--------|
| `TestTurnsView_UserInput` | v_turns_user 正确提取 content/platform/user_id |
| `TestTurnsView_AssistantResponse` | v_turns_assistant 正确关联 message + done + input |
| `TestTurnsView_StatsAccuracy` | tokens/cost/duration 与 conversation 历史数据一致 |
| `TestTurnsView_CrashSource` | source=crash 事件正确写入和查询 |
| `TestTurnsView_TimeoutSource` | source=timeout 事件正确写入和查询 |
| `TestTurnsView_FreshStartSource` | source=fresh_start 事件正确写入和查询 |
| `TestGetTurns_Pagination` | limit/offset 分页正确 |
| `TestGetTurnsBefore_Cursor` | before_seq 游标分页正确 |
| `TestSessionTurnStats_Aggregation` | 聚合统计与 conversation SessionStats 一致 |
| `TestHistoryAPI_BackwardCompatible` | API 响应 JSON 格式与旧接口兼容 |

### 6.2 数据验证

| 验证项 | 方法 |
|--------|------|
| 历史数据完整性 | 对比 conversation 119 条记录与 events 重建结果，逐条校验 |
| 实时写入一致性 | 并发请求后检查 events 表的 done + message 关联完整性 |
| 空内容处理 | crash/timeout 场景下 message 可能为空，验证 VIEW 返回空字符串 |

---

## 7. 附录：数据验证 SQL

重建 conversation 等效记录的 SQL（已验证）：

```sql
-- AI 回复记录（含文本关联）
SELECT d.seq,
  group_concat(json_extract(m.data, '$.content')) AS content,
  json_extract(d.data, '$.stats._session.model_name') AS model,
  json_extract(d.data, '$.success') AS success,
  json_extract(d.data, '$.stats._session.turn_input_tok') AS tokens_in,
  json_extract(d.data, '$.stats._session.turn_output_tok') AS tokens_out,
  json_extract(d.data, '$.stats._session.turn_duration_ms') AS duration_ms,
  json_extract(d.data, '$.stats._session.turn_cost_usd') AS cost_usd,
  json_extract(d.data, '$.stats._session.tool_call_count') AS tool_call_count
FROM events d
LEFT JOIN events m ON m.session_id = d.session_id
  AND m.type = 'message'
  AND m.id > COALESCE(
    (SELECT MAX(d2.id) FROM events d2 WHERE d2.type='done'
     AND d2.id < d.id AND d2.session_id = d.session_id), 0)
  AND m.id < d.id
WHERE d.type='done' AND d.session_id = ?
GROUP BY d.id ORDER BY d.seq;
```
