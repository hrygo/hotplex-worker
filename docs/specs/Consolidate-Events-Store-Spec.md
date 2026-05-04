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

> 版本: v2.1
> 日期: 2026-05-04
> 状态: Proposed (audited)
> 架构决策: 接受删库重来，整洁 schema

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

经逐字段对比和实际数据验证，**conversation 表是 events 表的严格子集**，可以安全合并。

需两个前提：（1）events 表增加 `source` 列；（2）events 表引入 goose 迁移框架。

---

## 2. 字段覆盖分析

### 2.1 conversation 字段 → events 等效映射

| # | conversation 字段 | events 等效来源 | 覆盖 | 备注 |
|---|------------------|----------------|------|------|
| 1 | `role` = `user` | `type = 'input'` | ✅ | |
| 2 | `role` = `assistant` | `type = 'done'` | ✅ | |
| 3 | `content` (用户) | `input` 的 `data.content` | ✅ | |
| 4 | `content` (AI) | 相邻 `message` 的 `data.content`（id 区间关联） | ✅ | VIEW JOIN |
| 5 | `platform` | **sessions 表** `platform` 字段 | ⚠️ | input metadata 仅 messaging 路径写入，需 JOIN sessions |
| 6 | `user_id` | **sessions 表** `owner_id` 字段 | ⚠️ | 无代码路径写入 input.metadata.user_id，需 JOIN sessions |
| 7 | `model` | `done` 的 `data.stats._session.model_name` | ✅ | |
| 8 | `success` | `done` 的 `data.success` | ✅ | |
| 9 | `tokens_in` | `done` 的 `data.stats._session.turn_input_tok` | ✅ | |
| 10 | `tokens_out` | `done` 的 `data.stats._session.turn_output_tok` | ✅ | |
| 11 | `duration_ms` | `done` 的 `data.stats._session.turn_duration_ms` | ✅ | |
| 12 | `cost_usd` | `done` 的 `data.stats._session.turn_cost_usd` | ✅ | |
| 13 | `tool_call_count` | `done` 的 `data.stats._session.tool_call_count` | ✅ | |
| 14 | `tools_json` | `done` 的 `data.stats._session.tool_names` | ✅ | |
| 15 | `metadata_json` | 从未被写入（始终为空） | N/A | |
| 16 | `source` | `events.source`（待增加） | ✅ | Phase 1 |

### 2.2 数据验证

对 session `474115bb` 的 5 条 assistant 记录做 1:1 数值对比，全部一致。

---

## 3. 修改方案

### 3.1 Phase 1: 引入 goose 迁移 + source 列

> 现状：eventstore 使用 `//go:embed sql/schema.sql`，无迁移机制。
> 决策：接受删库重来，与 session store 一致引入 goose。

**新增**: `internal/eventstore/migrate.go`

参照 `internal/session/migrate.go`，使用 `goose.NewProvider(goose.DialectSQLite3, ...)`。

**新增**: `internal/eventstore/sql/migrations/001_base_schema.sql`

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    data TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'outbound',
    source TEXT NOT NULL DEFAULT 'normal'
      CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start')),
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);

-- +goose Down
DROP TABLE IF EXISTS events;
```

**删除**: `internal/eventstore/sql/schema.sql`

**修改**:
- `store.go`: embed 指令移除 `schema.sql`，`NewSQLiteStore` 改用 `runMigrations()`；`StoredEvent` 增加 `Source` 字段；`scanEvents` 扫描 7 列；`Append` 传 7 参数
- `collector.go`: `Capture()` 增加 `source string` 参数
- `bridge_forward.go`: `captureEvent`/`CaptureInbound` 传递 `"normal"`
- `events.insert.sql`: INSERT 7 列（含 source）
- `events.query_*.sql`: SELECT 7 列（含 source）

### 3.2 Phase 2: 创建 turns VIEW

**新增**: `internal/eventstore/sql/migrations/002_create_turns_view.sql`

> **关键修正**: `platform` 和 `user_id` 从 sessions 表 JOIN 获取，不从 input.metadata 取（WebSocket 客户端不传）。

```sql
-- +goose Up

-- 用户输入视图
CREATE VIEW v_turns_user AS
SELECT
  e.session_id,
  e.seq,
  'user' AS role,
  json_extract(e.data, '$.content') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
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
LEFT JOIN sessions s ON s.id = e.session_id
WHERE e.type = 'input' AND e.direction = 'inbound';

-- AI 回复视图
CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id,
  d.seq,
  'assistant' AS role,
  COALESCE(m.content, '') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
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
LEFT JOIN sessions s ON s.id = d.session_id
LEFT JOIN (
  SELECT MAX(m.id) AS id, m.session_id,
    group_concat(json_extract(m.data, '$.content'), char(10)) AS content
  FROM events m
  WHERE m.type = 'message'
  GROUP BY m.session_id, (SELECT MAX(d2.id) FROM events d2 WHERE d2.type='done' AND d2.id <= m.id AND d2.session_id = m.session_id)
) m ON m.session_id = d.session_id AND m.id < d.id
WHERE d.type = 'done' AND d.direction = 'outbound';

-- 合并视图
CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at;

-- +goose Down
DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_assistant;
DROP VIEW IF EXISTS v_turns_user;
```

### 3.3 Phase 3: 迁移 API 消费者

**`GET /api/sessions/{id}/history`** — `internal/gateway/api.go:289-355`

当前：`g.convStore.GetBySession()` → conversation 表
改为：`g.eventStore.GetTurns()` → `v_turns` 视图

**`GET /admin/sessions/{id}/stats`** — `internal/admin/sessions.go:136-159`

当前：`a.convStore.SessionStats()` → conversation 聚合
改为：`a.eventStore.SessionTurnStats()` → `v_turns_assistant` 聚合

新增 `TurnRecord` 结构和三个接口方法：

```go
type TurnRecord struct {
    Seq        int64   `json:"seq"`
    Role       string  `json:"role"`
    Content    string  `json:"content"`
    Platform   string  `json:"platform"`
    UserID     string  `json:"user_id"`
    Model      string  `json:"model"`
    Success    *bool   `json:"success"`
    Source     string  `json:"source"`
    ToolCount  int     `json:"tool_call_count"`
    TokensIn   int     `json:"tokens_in"`
    TokensOut  int     `json:"tokens_out"`
    DurationMs int64   `json:"duration_ms"`
    CostUSD    float64 `json:"cost_usd"`
    CreatedAt  int64   `json:"created_at"`
}
```

### 3.4 Phase 4: 废弃 ConversationStore

| 移除目标 | 文件 |
|---------|------|
| ConversationStore 接口和实现 | `internal/session/conversation_store.go` |
| SQL queries | `internal/session/sql/queries/conversation.*` |
| migration DDL | `internal/session/sql/migrations/001_init.sql` conversation 部分 |
| handler.go Append | `internal/gateway/handler.go:207-217` |
| bridge_forward.go Append | `internal/gateway/bridge_forward.go:231-253`, `:386-406` |
| bridge_worker.go Append | `internal/gateway/bridge_worker.go:204-214` |
| DI 注入 | `cmd/hotplex/gateway_run.go` ConvStore |
| admin 适配器 | `cmd/hotplex/admin_adapters.go:83-89` convStoreAdapter |
| 路由引用 | `cmd/hotplex/routes.go:32,75,81` |

### 3.5 Phase 5: 补充 events 写入点

| 场景 | 覆盖 | 需补充 |
|------|------|--------|
| 正常 input | ✅ `CaptureInbound()` | — |
| 正常 done | ✅ `forwardEvents()` → `captureEvent()` | — |
| Worker 崩溃 | ❌ | `handleWorkerExit()` 写入 source=`crash` |
| Turn 超时 | ❌ | turnTimer handler 写入 source=`timeout` |
| Fresh-start 重投递 | ❌ | `attemptResumeFallback()` 写入 source=`fresh_start` |

---

## 4. 修改文件清单

| # | 文件 | Phase | 类型 |
|---|------|-------|------|
| 1 | `internal/eventstore/migrate.go` | 1 | 新增 |
| 2 | `internal/eventstore/sql/migrations/001_base_schema.sql` | 1 | 新增 |
| 3 | `internal/eventstore/sql/schema.sql` | 1 | 删除 |
| 4 | `internal/eventstore/store.go` | 1 | 修改 |
| 5 | `internal/eventstore/collector.go` | 1 | 修改 |
| 6 | `internal/gateway/bridge_forward.go` | 1 | 修改 |
| 7 | `internal/eventstore/sql/queries/events.*` | 1 | 修改 |
| 8 | `internal/eventstore/sql/migrations/002_create_turns_view.sql` | 2 | 新增 |
| 9 | `internal/eventstore/store.go` | 3 | 修改 (GetTurns/GetTurnsBefore/SessionTurnStats) |
| 10 | `internal/gateway/api.go` | 3 | 修改 |
| 11 | `internal/admin/sessions.go` | 3 | 修改 |
| 12 | `internal/admin/admin.go` | 3 | 修改 |
| 13 | `cmd/hotplex/admin_adapters.go` | 4 | 修改 |
| 14 | `cmd/hotplex/routes.go` | 4 | 修改 |
| 15 | `cmd/hotplex/gateway_run.go` | 4 | 修改 |
| 16 | `internal/gateway/handler.go` | 4 | 修改 |
| 17 | `internal/gateway/bridge_forward.go` | 5 | 修改 (crash/timeout) |
| 18 | `internal/gateway/bridge_worker.go` | 5 | 修改 (fresh_start) |
| 19 | `internal/session/conversation_store.go` | 4 | 删除 |
| 20 | `internal/session/sql/queries/conversation.*` | 4 | 删除 |
| 21 | `internal/gateway/api_test.go` | 4 | 修改 (mock) |

---

## 5. 收益与风险

### 5.1 收益

| 维度 | 改善 |
|------|------|
| **数据一致性** | 消除双源写入不一致风险，单点真实 |
| **代码量** | 净减 ~300 行 |
| **写入开销** | 每次 Turn 从 2 次 SQLite 写降为 1 次 |
| **可维护性** | 单一持久化路径 + goose 迁移框架 |

### 5.2 风险

| 风险 | 缓解 |
|------|------|
| VIEW 查询性能（跨库 JOIN） | events.db 和 sessions 表在不同数据库，VIEW 不能跨库 JOIN。需在 events.db 中用 Go 代码查询 sessions 表，或在应用层拼接 |
| API 响应格式变化 | `TurnRecord` 字段与 `ConversationRecord` 保持 JSON 兼容 |

> **重要**: VIEW 中 JOIN `sessions` 表需要跨数据库操作。events.db 与 hotplex.db 是不同 SQLite 文件，VIEW 无法直接 JOIN。Phase 2 需改用以下方案之一：
> - (A) Go 代码层 JOIN：先查 v_turns 获取 session_id，再查 sessions 表补充 platform/user_id
> - (B) ATTACH DATABASE：在 events.db 连接上 ATTACH hotplex.db，VIEW 可跨库 JOIN
> - (C) 冗余存储：在 input 事件写入时强制注入 platform/user_id 到 metadata（修改 CaptureInbound 调用点）

---

## 6. 测试要求

| 测试 | 覆盖点 |
|------|--------|
| `TestMigration_SourceColumn` | goose 迁移正确创建 source 列及 CHECK 约束 |
| `TestTurnsView_UserInput` | v_turns_user 正确提取 content，JOIN sessions 取 platform/user_id |
| `TestTurnsView_AssistantResponse` | v_turns_assistant 正确关联 message + done + sessions |
| `TestTurnsView_StatsAccuracy` | tokens/cost/duration 数值一致 |
| `TestTurnsView_CrashSource` | source=crash 事件正确写入和查询 |
| `TestTurnsView_TimeoutSource` | source=timeout 事件正确写入和查询 |
| `TestTurnsView_FreshStartSource` | source=fresh_start 事件正确写入和查询 |
| `TestGetTurns_Pagination` | limit/offset 分页正确 |
| `TestGetTurnsBefore_Cursor` | before_seq 游标分页正确 |
| `TestSessionTurnStats_Aggregation` | 聚合统计与 conversation SessionStats 一致 |
| `TestHistoryAPI_BackwardCompatible` | API 响应 JSON 格式与旧接口兼容 |
