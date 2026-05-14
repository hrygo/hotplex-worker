---

---

# Message Persistence Design

> HotPlex v1.0 消息持久化设计，基于行业最佳实践。

---

## 0. 实施状态：v1.0 不实现

> ⚠️ **v1.0 决定：不实现 EventStore / MessageStore / AuditLog。**
>
> 理由：Worker 自身已具备持久化能力（Claude Code 的 `~/.claude/projects/`，OpenCode Server 的服务端状态），Gateway 的职责是**控制面路由**，不做数据面 replay。
>
> 后续如有合规/审计需求，可按本文档设计在 v1.1 引入。

---

## 1. 核心定位（重要）

> ⚠️ **本设计是可选存储插件，不参与 SessionManager 状态流转。**
>
> - Worker-Gateway-Design §6.2 明确：**Gateway 仅持久化 session 元数据（控制面），不做 event log，不负责 replay**
> - EventStore replay **仅用于审计和外部回放**，不参与 SessionManager 的正常状态管理
> - Session state machine 保持无状态（由 AEP 事件驱动）

---

## 2. 设计原则

### 1.1 当前状态评估

> ⚠️ **HotPlex 当前实现是 CRUD + 软删除，而非真正的 Event Sourcing**。

| 维度 | 当前实现 | 最佳实践 | 差距 |
|------|----------|----------|------|
| 事件追加 | `INSERT OR REPLACE` | Append-only | ❌ 可覆盖 |
| 事件类型 | 无 `event_type`/`version` | Schema 版本化 | ❌ 缺失 |
| 不可变性 | 无保证 | Append-only 触发器 | ❌ 无 |
| 快照机制 | 无 | Periodic Snapshot | ❌ 缺失 |

### 1.2 分阶段演进

| 阶段 | 方案 | 目标 |
|------|------|------|
| **v1.0** | SQLite WAL + 改进 Schema | 事件可追溯 |
| **v1.0** | Temporal Query + 快照 | 性能优化 |
| **v1.0** | PostgreSQL JSONB + pgaudit | 生产级合规 |

---

## 2. Event Sourcing 模式

### 2.1 核心概念（Martin Fowler）

> Event Sourcing 的核心是将应用状态的所有变更存储为事件序列，而非仅存储当前状态。

**Event Store vs CRUD**：

| 维度 | CRUD | Event Sourcing |
|------|------|----------------|
| 状态表示 | 当前快照 | 历史事件序列 |
| 变更记录 | 无 | 完整审计追踪 |
| 时间旅行 | ❌ | ✅ |
| 事件重放 | ❌ | ✅ |

### 2.2 事件 Schema 设计

```sql
CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id       TEXT NOT NULL UNIQUE,          -- UUID v4，全局唯一
    event_type     TEXT NOT NULL,                  -- 事件类型：session.created
    event_version   TEXT NOT NULL DEFAULT '1.0',    -- Schema 版本

    -- 身份
    session_id     TEXT NOT NULL,
    user_id        TEXT NOT NULL,

    -- 时序
    seq            INTEGER NOT NULL,                -- 事件序列号
    timestamp_ms   INTEGER NOT NULL,                -- Unix ms

    -- 内容（不可变 JSON）
    payload        JSONB NOT NULL,

    -- 元数据
    metadata       JSONB,                          -- trace_id, span_id 等
    created_at     INTEGER DEFAULT (strftime('%s', 'now'))
);

-- 索引
CREATE INDEX idx_events_session_seq ON events(session_id, seq);
CREATE INDEX idx_events_type ON events(event_type);
CREATE INDEX idx_events_timestamp ON events(timestamp_ms);
```

### 2.3 事件类型定义

```go
// internal/persistence/event_types.go

var EventTypes = map[string]string{
    // Session 生命周期
    "session.created":     "1.0",
    "session.resumed":     "1.0",
    "session.input":       "1.0",
    "session.output":      "1.0",
    "session.terminated":  "1.0",
    "session.deleted":     "1.0",

    // AEP 事件
    "aep.message_delta":   "1.0",
    "aep.tool_call":       "1.0",
    "aep.tool_result":     "1.0",
    "aep.state":           "1.0",
    "aep.error":           "1.0",
    "aep.done":            "1.0",

    // 安全事件
    "security.auth_success":  "1.0",
    "security.auth_failure":  "1.0",
    "security.owner_mismatch": "1.0",
}

// ValidateEventType 验证事件类型和版本
func ValidateEventType(eventType, version string) error {
    expectedVersion, ok := EventTypes[eventType]
    if !ok {
        return fmt.Errorf("unknown event type: %s", eventType)
    }

    if !isVersionCompatible(expectedVersion, version) {
        return fmt.Errorf("event type %s version %s not compatible with %s",
            eventType, version, expectedVersion)
    }

    return nil
}
```

---

### 3.2 插件架构（核心设计）

> ⚠️ **EventStore 是独立插件，不依赖 SessionManager，避免循环依赖。**

```go
// plugins/storage/message_store.go

// MessageStore 是可选插件接口
// 不参与 SessionManager 状态流转，仅用于审计和外部回放
type MessageStore interface {
    // Append 追加事件（仅写入，不更新任何状态）
    Append(ctx context.Context, event *Event) error

    // Query 查询事件（用于审计和回放）
    Query(ctx context.Context, sessionID string, fromSeq int64) ([]*Event, error)

    // GetOwner 获取 session owner（直接从 DB 查询，避免依赖 SessionManager）
    GetOwner(ctx context.Context, sessionID string) (string, error)

    Close() error
}

// 实现验证
var _ MessageStore = (*SQLiteMessageStore)(nil)
var _ MessageStore = (*PostgresMessageStore)(nil)
```

### 3.3 Ownership 验证（无循环依赖）

> ⚠️ **EventStore 直接查询 session 表验证 ownership，不调用 SessionManager。**

```go
func (s *SQLiteMessageStore) GetOwner(ctx context.Context, sessionID string) (string, error) {
    var ownerID string
    err := s.db.QueryRowContext(ctx,
        "SELECT owner_id FROM sessions WHERE id = ?", sessionID).
        Scan(&ownerID)
    if err == sql.ErrNoRows {
        return "", ErrSessionNotFound
    }
    return ownerID, err
}

func (s *SQLiteMessageStore) Query(ctx context.Context, sessionID string, fromSeq int64) ([]*Event, error) {
    // 1. 直接查 session 表验证 ownership
    ownerID, err := s.GetOwner(ctx, sessionID)
    if err != nil {
        return nil, err
    }

    // 2. 查询事件（仅读权限）
    rows, err := s.db.QueryContext(ctx, `
        SELECT event_id, event_type, seq, payload, timestamp_ms
        FROM events
        WHERE session_id = ? AND seq > ?
        ORDER BY seq ASC
    `, sessionID, fromSeq)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var events []*Event
    for rows.Next() {
        var e Event
        var payloadJSON []byte
        if err := rows.Scan(&e.ID, &e.Type, &e.Seq, &payloadJSON, &e.TimestampMs); err != nil {
            return nil, err
        }
        json.Unmarshal(payloadJSON, &e.Payload)
        events = append(events, &e)
    }

    return events, rows.Err()
}
```

### 3.4 SessionManager 集成（无侵入）

```go
// internal/engine/pool.go（SessionManager 保持无状态）

type SessionManager struct {
    db     *sql.DB
    pool   map[string]*Session

    // EventStore 是可选依赖，通过接口注入
    // ⚠️ SessionManager 状态流转不调用 EventStore
    eventStore MessageStore  // 可为 nil（禁用消息持久化时）

    // 同步写入事件（异步批量，由 EventStore 处理）
    onEvent func(event *Event)
}

func (sm *SessionManager) notifyEvent(event *Event) {
    if sm.eventStore != nil {
        // 异步追加到 EventStore（不阻塞主流程）
        go func() {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            sm.eventStore.Append(ctx, event)
        }()
    }
}
```

### 3.5 移除 ComputeSessionState

> ⚠️ **ComputeSessionState 不适用于热路径。仅用于离线审计回放。**

```go
// ⚠️ 以下函数已废弃，不在热路径中使用
// ComputeSessionState 仅用于：审计报告、合规查询、故障排查
// 不参与 SessionManager 的正常状态管理


func ComputeSessionState(events []*Event) *SessionState { ... }
```

### 3.6 插件架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                    HotPlex Gateway                                │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────┐   │
│  │SessionManager│───►│  AEP Events │───►│ EventStore (Plugin) │
│  │  (无状态)    │    │   Channel   │    │  (异步追加)      │   │
│  └─────────────┘    └─────────────┘    └────────┬────────┘   │
│                                                  │              │
│  ┌─────────────┐    ┌─────────────┐    ┌────────▼────────┐   │
│  │ Admin API   │◄───│  /metrics   │◄───│  SQLite WAL     │   │
│  │  (只读)     │    │             │    │  Append-only     │   │
│  └─────────────┘    └─────────────┘    └─────────────────┘   │
│                                                                 │
│  SessionManager ──→ 不调用 EventStore ──→ 状态流转无侵入       │
│  EventStore ──→ 仅追加事件 ──→ 用于审计和外部回放               │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. Append-Only 触发器

```sql
-- 防止 UPDATE 和 DELETE（Append-only）
CREATE TRIGGER IF NOT EXISTS prevent_event_update
BEFORE UPDATE ON events
BEGIN
    SELECT RAISE(ABORT, 'Events are immutable');
END;

CREATE TRIGGER IF NOT EXISTS prevent_event_delete
BEFORE DELETE ON events
BEGIN
    SELECT RAISE(ABORT, 'Events are immutable');
END;

-- 自动填充 event_type（如果缺失）
CREATE TRIGGER IF NOT EXISTS set_event_type
BEFORE INSERT ON events
WHEN NEW.event_type IS NULL
BEGIN
    SELECT RAISE(ABORT, 'event_type is required');
END;
```

---

## 5. 异步批量写入

```go
// internal/persistence/event_writer.go

type EventWriter struct {
    db            *sql.DB
    batchSize     int
    flushInterval time.Duration
    ch            chan *Event
    wg            sync.WaitGroup
}

func NewEventWriter(db *sql.DB, batchSize int, flushInterval time.Duration) *EventWriter {
    w := &EventWriter{
        db:            db,
        batchSize:     batchSize,
        flushInterval: flushInterval,
        ch:            make(chan *Event, batchSize*2),
    }

    w.wg.Add(1)
    go w.processLoop()

    return w
}

func (w *EventWriter) processLoop() {
    defer w.wg.Done()

    ticker := time.NewTicker(w.flushInterval)
    batch := make([]*Event, 0, w.batchSize)

    for {
        select {
        case event := <-w.ch:
            batch = append(batch, event)
            if len(batch) >= w.batchSize {
                w.flush(batch)
                batch = batch[:0]
            }

        case <-ticker.C:
            if len(batch) > 0 {
                w.flush(batch)
                batch = batch[:0]
            }
        }
    }
}

func (w *EventWriter) flush(batch []*Event) error {
    tx, err := w.db.Begin()
    if err != nil {
        return err
    }

    stmt, _ := tx.Prepare(`
        INSERT INTO events (event_id, event_type, event_version, session_id, user_id, seq, timestamp_ms, payload, metadata)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `)
    defer stmt.Close()

    for _, e := range batch {
        payload, _ := json.Marshal(e.Payload)
        metadata, _ := json.Marshal(e.Metadata)

        _, err := stmt.Exec(e.ID, e.Type, e.Version, e.SessionID, e.UserID, e.Seq, e.TimestampMs, payload, metadata)
        if err != nil {
            tx.Rollback()
            return err
        }
    }

    return tx.Commit()
}
```

---

## 4. 会话查询与重放

> ⚠️ **ReplaySession 和 ComputeSessionState 仅用于离线审计，不在热路径中使用。**

### 4.1 查询接口

查询功能由 `MessageStore.Query()` 提供（见 §3.3），直接返回事件序列，供外部使用。

### 4.2 ComputeSessionState（离线审计用）

```go
// 仅用于：审计报告、合规查询、故障排查
// 不参与 SessionManager 的正常状态管理
func ComputeSessionState(events []*Event) *SessionState {
    state := &SessionState{}
    for _, e := range events {
        switch e.Type {
        case "session.created":
            state.Status = "created"
        case "session.input":
            state.InputCount++
        case "session.output":
            state.OutputCount++
        case "session.terminated":
            state.Status = "terminated"
        }
    }
    return state
}
```

### 4.3 快照策略（可选优化）

```go
const (
    SnapshotIntervalEvents = 1000  // 每 1000 个事件快照
    SnapshotIntervalTime   = 1 * time.Hour
)

func (s *EventStore) ShouldSnapshot(session *Session) bool {
    if session.EventCount%s.SnapshotIntervalEvents == 0 {
        return true
    }
    if time.Since(session.LastSnapshotTime) > s.SnapshotIntervalTime {
        return true
    }

    return false
}
```

---

## 5. 审计日志

### 5.1 审计事件定义

```sql
CREATE TABLE audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id     TEXT NOT NULL UNIQUE,
    timestamp_ms  INTEGER NOT NULL,

    -- 操作者
    user_id       TEXT NOT NULL,
    ip_address    TEXT,
    user_agent    TEXT,

    -- 操作
    action        TEXT NOT NULL,    -- create, read, update, delete, admin_*
    resource_type TEXT NOT NULL,    -- session, config, admin
    resource_id   TEXT NOT NULL,

    -- 上下文
    details       JSONB,
    result        TEXT,             -- success, failure
    error_message TEXT
);

CREATE INDEX idx_audit_timestamp ON audit_log(timestamp_ms);
CREATE INDEX idx_audit_user ON audit_log(user_id);
CREATE INDEX idx_audit_resource ON audit_log(resource_type, resource_id);
```

### 5.2 不可篡改性

**哈希链**（可选，用于高合规场景）：

```go
type AuditEvent struct {
    EventID      string `json:"event_id"`
    PreviousHash string `json:"previous_hash"`  // 前一个事件的哈希
    Hash         string `json:"hash"`            // 当前事件的哈希
    // ... 其他字段
}

func (e *AuditEvent) ComputeHash() string {
    data := fmt.Sprintf("%s:%s:%v:%d",
        e.EventID, e.PreviousHash, e.Details, e.TimestampMs)
    return sha256.Sum256([]byte(data))
}

func (s *AuditStore) Append(event *AuditEvent) error {
    // 验证前一个哈希
    prev, _ := s.GetLast()
    if prev != nil && event.PreviousHash != prev.Hash {
        return errors.New("hash chain broken")
    }

    event.Hash = event.ComputeHash()
    return s.db.Insert(event)
}
```

---

## 6. PostgreSQL 生产方案（v1.0）

### 6.1 Schema 设计

```sql
-- 事件分区表（按月分区）
CREATE TABLE events (
    id              BIGSERIAL,
    event_id       UUID NOT NULL,
    event_type     TEXT NOT NULL,
    event_version   TEXT NOT NULL DEFAULT '1.0',

    session_id     TEXT NOT NULL,
    user_id        TEXT NOT NULL,

    seq            BIGINT NOT NULL,
    timestamp_ms   BIGINT NOT NULL,

    payload        JSONB NOT NULL,
    metadata       JSONB,

    PRIMARY KEY (id, timestamp_ms)
) PARTITION BY RANGE (timestamp_ms);

-- 创建分区
CREATE TABLE events_2026_01 PARTITION OF events
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');

-- 索引
CREATE INDEX idx_events_session_seq ON events(session_id, seq);
CREATE INDEX idx_events_payload ON events USING GIN (payload);

-- pgaudit 配置
ALTER SYSTEM SET pgaudit.log = 'READ, WRITE';
```

### 6.2 合规特性

| 合规要求 | PostgreSQL 实现 |
|----------|----------------|
| SOC2 | pgaudit 审计日志 |
| GDPR | Row-level Security (RLS) |
| 不可篡改 | WAL + Trigger |
| 数据保留 | Partition DROP |

---

## 7. 配置

```yaml
# configs/persistence.yaml
message_store:
  enabled: true

  # MVP: SQLite
  sqlite:
    path: "${HOTPLEX_MESSAGE_STORE_PATH}"
    wal_mode: true          # Write-Ahead Logging
    batch_size: 100
    flush_interval_ms: 1000

  # 生产: PostgreSQL
  postgresql:
    host: "${HOTPLEX_PG_HOST}"
    port: 5432
    database: hotplex
    pool_max_conns: 25

  # 保留策略
  retention:
    events_days: 30
    audit_days: 90
    snapshot_interval_events: 1000
```

---

## 8. 安全考虑

### 8.1 访问控制

```go
func (s *EventStore) GetEvents(sessionID, userID string, fromSeq int64) ([]*Event, error) {
    // 1. 验证 session ownership
    session, err := s.sessionMgr.GetSession(sessionID)
    if err != nil {
        return nil, err
    }

    if session.OwnerID != userID {
        // 记录审计日志
        s.audit.Log(&AuditEvent{
            UserID:   userID,
            Action:   "read",
            Result:   "failure",
            Details:  map[string]string{"reason": "ownership_mismatch"},
        })
        return nil, ErrUnauthorized
    }

    return s.replaySession(sessionID, fromSeq)
}
```

---

## 9. 实施路线图

| 版本 | 任务 | 产出 | 状态 |
|------|------|------|------|
| **v1.0** | 改进 Schema + Append-only 触发器 | `event_type`/`event_version` 字段 | ❌ 不实现 |
| **v1.0** | 快照策略 + Temporal Query | 性能优化 | ❌ 不实现 |
| **v1.0** | PostgreSQL 迁移 + pgaudit | 生产级合规 | ❌ 不实现 |

> v1.0 由 Worker 自身负责数据持久化，Gateway 只管控制面。

---

## 10. 参考资料

- [Martin Fowler: Event Sourcing](https://martinfowler.com/articles/eventSourcing.html)
- [Greg Young: Versioning in an Event Sourced System](https://www.oreilly.com/library/view/versioning-in-an/9781492034642/)
- [Event Store: Event Sourcing](https://eventstore.com/event-sourcing/)
- [PostgreSQL JSONB Best Practices](https://www.postgresql.org/docs/current/functions-json.html)
- [pgaudit Extension](https://www.pgaudit.org/)