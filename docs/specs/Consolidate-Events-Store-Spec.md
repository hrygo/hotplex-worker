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
estimated_hours: 8
---

# 合并 conversation 表到 events 表规格书

> 版本: v3.0
> 日期: 2026-05-04
> 状态: Proposed (audited)
> 架构决策: 单数据库（合并 events.db → hotplex.db），接受删库重来

---

## 1. 概述

### 1.1 问题

HotPlex 维护两个独立 SQLite 数据库、三张持久化表：

| 数据库 | 表 | 粒度 | 写入时机 |
|--------|-----|------|---------|
| `hotplex.db` | `sessions` | Session 级 | 状态变更时 |
| `hotplex.db` | `conversation` | Turn 级 | `convStore.Append()` |
| `events.db` | `events` | 事件级 | `collector.Capture()` |

`conversation` 和 `events` 观察同一条 Worker 事件流但写入独立，数据大量重叠，路径不同步。

### 1.2 结论

经逐字段对比和实际数据验证，**conversation 表是 events 表的严格子集**，可以安全合并。

### 1.3 架构决策：合并为单数据库

**现状**: `events.db` 与 `hotplex.db` 分离，无文档化理由（开发便利性决策）。

**决策**: 将 events 表迁移到 `hotplex.db`，消除 `events.db`。

**评估依据**:

| 维度 | 分离 | 合并 |
|------|------|------|
| VIEW 跨库 JOIN | ❌ 不可能，需 ATTACH 或应用层拼接 | ✅ 直接 JOIN sessions |
| 配置复杂度 | 两个路径 (`DB.Path` + `DB.EventsPath`) | 单一 `DB.Path` |
| 连接数 | 3 个 `*sql.DB` (session + conversation + events) | 2 个 (session + events) |
| 备份/恢复 | 两个文件需协调 | 单文件 |
| GC 协调 | session 删除不级联 events（已知缺陷） | 同库便于 GC 流程调用 `DeleteBySession` |
| 写入争用 | 隔离（但 events.db 写入量本身不高） | WAL 模式 + busy_timeout=5s 充分缓解 |
| goose 迁移 | 两套独立管理 | 统一管理 |

**写入争用分析**: events 批次间隔 100ms，session upsert 秒级间隔，WAL 模式允许并发读 + busy_timeout=5s。实测无瓶颈。

---

## 2. 字段覆盖分析

### 2.1 conversation 字段 → events 等效映射

| # | conversation 字段 | events 等效来源 | 覆盖 | 备注 |
|---|------------------|----------------|------|------|
| 1 | `role` = `user` | `type = 'input'` | ✅ | |
| 2 | `role` = `assistant` | `type = 'done'` | ✅ | |
| 3 | `content` (用户) | `input` 的 `data.content` | ✅ | |
| 4 | `content` (AI) | 相邻 `message` 的 `data.content`（id 区间关联） | ✅ | VIEW JOIN |
| 5 | `platform` | `sessions.platform`（同库 JOIN） | ✅ | 合并后可直连 |
| 6 | `user_id` | `sessions.owner_id`（同库 JOIN） | ✅ | 合并后可直连 |
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

---

## 3. 修改方案

### 3.1 Phase 0: 合并数据库

> 将 events 表迁移到 hotplex.db，消除 events.db。

**修改**: `internal/session/sql/migrations/002_events_table.sql` — 在 session store 的 goose 迁移中新增 events 表

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

**修改**: `internal/eventstore/store.go`

- `NewSQLiteStore` 改为接收 `*sql.DB`（由 session store 共享），不再自行开库
- 移除 `sqlutil.OpenDB` 调用和 `dbPath` 参数
- 移除 `sql/schema.sql` 嵌入（schema 由 session store goose 管理）
- `StoredEvent` 增加 `Source string` 字段
- `scanEvents` 扫描 7 列
- `Append` 传 7 参数（含 source）

**修改**: `internal/eventstore/collector.go`

- `Capture()` 增加 `source string` 参数

**修改**: `internal/gateway/bridge_forward.go`

- `captureEvent`/`CaptureInbound` 传递 `"normal"`

**修改**: `cmd/hotplex/gateway_run.go`

- EventStore 使用 session store 的 `*sql.DB` 连接
- 移除 `cfg.DB.EventsPath` 引用
- EventStore 不再独立 Close（由 session store 统一管理生命周期）

**修改**: `internal/eventstore/sql/queries/events.*`

- `events.insert.sql`: INSERT 7 列（含 source）
- `events.query_*.sql`: SELECT 7 列（含 source）

**修改**: `internal/config/config.go`

- 标记 `EventsPath` 为 deprecated（保留字段但不再使用，兼容旧配置文件）

**删除**: `internal/eventstore/sql/schema.sql`

**删除**: `~/.hotplex/data/events.db`（接受删库重来）

### 3.2 Phase 1: 创建 turns VIEW

**新增**: `internal/session/sql/migrations/003_create_turns_view.sql`

> 合并后 events 与 sessions 同库，VIEW 可直接 JOIN sessions 表获取 platform/owner_id。

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

### 3.3 Phase 2: 迁移 API 消费者

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

### 3.4 Phase 3: 废弃 ConversationStore

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

### 3.5 Phase 4: 补充 events 写入点

| 场景 | 覆盖 | 需补充 |
|------|------|--------|
| 正常 input | ✅ `CaptureInbound()` | — |
| 正常 done | ✅ `forwardEvents()` → `captureEvent()` | — |
| Worker 崩溃 | ❌ | `handleWorkerExit()` 写入 source=`crash` |
| Turn 超时 | ❌ | turnTimer handler 写入 source=`timeout` |
| Fresh-start 重投递 | ❌ | `attemptResumeFallback()` 写入 source=`fresh_start` |

### 3.6 清理

| 目标 | 说明 |
|------|------|
| `internal/eventstore/migrate.go` | 不再需要独立迁移（由 session store goose 统一管理） |
| `config.DB.EventsPath` | 标记 deprecated，后续版本移除 |
| `events.db` 文件 | 删除（接受删库重来） |

---

## 4. 修改文件清单

| # | 文件 | Phase | 类型 |
|---|------|-------|------|
| 1 | `internal/session/sql/migrations/002_events_table.sql` | 0 | 新增 |
| 2 | `internal/eventstore/store.go` | 0 | 修改（共享 DB 连接，Source 字段，7 列） |
| 3 | `internal/eventstore/collector.go` | 0 | 修改（Capture source 参数） |
| 4 | `internal/gateway/bridge_forward.go` | 0 | 修改（传递 source） |
| 5 | `internal/eventstore/sql/queries/events.*` | 0 | 修改（7 列） |
| 6 | `internal/eventstore/sql/schema.sql` | 0 | 删除 |
| 7 | `cmd/hotplex/gateway_run.go` | 0 | 修改（共享 DB，移除 EventsPath） |
| 8 | `internal/config/config.go` | 0 | 修改（EventsPath deprecated） |
| 9 | `internal/session/sql/migrations/003_create_turns_view.sql` | 1 | 新增 |
| 10 | `internal/eventstore/store.go` | 2 | 修改（GetTurns/GetTurnsBefore/SessionTurnStats） |
| 11 | `internal/gateway/api.go` | 2 | 修改 |
| 12 | `internal/admin/sessions.go` | 2 | 修改 |
| 13 | `internal/admin/admin.go` | 2 | 修改 |
| 14 | `cmd/hotplex/admin_adapters.go` | 3 | 修改 |
| 15 | `cmd/hotplex/routes.go` | 3 | 修改 |
| 16 | `cmd/hotplex/gateway_run.go` | 3 | 修改 |
| 17 | `internal/gateway/handler.go` | 3 | 修改 |
| 18 | `internal/gateway/bridge_forward.go` | 4 | 修改（crash/timeout） |
| 19 | `internal/gateway/bridge_worker.go` | 4 | 修改（fresh_start） |
| 20 | `internal/session/conversation_store.go` | 3 | 删除 |
| 21 | `internal/session/sql/queries/conversation.*` | 3 | 删除 |
| 22 | `internal/gateway/api_test.go` | 3 | 修改（mock） |

---

## 5. 收益与风险

### 5.1 收益

| 维度 | 改善 |
|------|------|
| **数据一致性** | 消除双源写入，单点真实 |
| **VIEW 直连** | events JOIN sessions 同库直连，无需 ATTACH 或应用层拼接 |
| **GC 协调** | 同库便于 Session GC 流程中调用 `DeleteBySession` 清理 events |
| **代码量** | 净减 ~400 行（conversation_store + 独立 eventstore 迁移 + EventsPath 配置） |
| **连接数** | 从 3 个 `*sql.DB` 降至 2 个 |
| **配置** | 单一 `DB.Path`，移除 `EventsPath` |
| **迁移** | 统一 goose 管理，不再两套独立 schema 机制 |

### 5.2 风险

| 风险 | 缓解 |
|------|------|
| 写入争用（event 批次 vs session upsert） | WAL 模式允许并发读 + busy_timeout=5s；event 写入已批量间隔 100ms；session upsert 秒级 |
| VACUUM 影响范围扩大 | session store Compact 已有 VACUUM，events 表用 DELETE 清理，碎片可控 |
| API 响应格式变化 | `TurnRecord` 字段与 `ConversationRecord` 保持 JSON 兼容 |

---

## 6. 测试要求

| 测试 | 覆盖点 |
|------|--------|
| `TestEventsTable_DeleteBySession` | Session GC 调用 DeleteBySession 清理 events |
| `TestEventsTable_SourceCheck` | source CHECK 约束生效 |
| `TestTurnsView_UserInput` | v_turns_user JOIN sessions 取 platform/owner_id |
| `TestTurnsView_AssistantResponse` | v_turns_assistant 关联 message + done + sessions |
| `TestTurnsView_StatsAccuracy` | tokens/cost/duration 数值一致 |
| `TestTurnsView_CrashSource` | source=crash 写入和查询 |
| `TestTurnsView_TimeoutSource` | source=timeout 写入和查询 |
| `TestTurnsView_FreshStartSource` | source=fresh_start 写入和查询 |
| `TestGetTurns_Pagination` | limit/offset 分页 |
| `TestGetTurnsBefore_Cursor` | before_seq 游标分页 |
| `TestSessionTurnStats_Aggregation` | 聚合统计一致 |
| `TestHistoryAPI_BackwardCompatible` | API JSON 兼容 |
