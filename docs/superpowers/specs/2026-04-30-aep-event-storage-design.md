# AEP Event Storage Design

**Date:** 2026-04-30
**Branch:** feat/62-webchat-persistence-enhancement
**Status:** Draft

## Goal

Store all user-replayable AEP events in an independent SQLite database, enabling WebChat to fully reconstruct conversation history after page refresh or reconnection.

## Current State

The system stores only aggregated conversation turns (via `ConversationStore` → `conversation` table). Individual AEP events like `message.delta`, `tool_call`, `reasoning`, `step`, and `permission_request` are forwarded in real-time but never persisted. Users lose all streaming context on page refresh.

## Architecture

### Storage: Independent SQLite File

Location: `~/.hotplex/data/events.db` (configurable via `event_store.path`)

New package: `internal/eventstore/` — fully independent, no dependency on gateway or session packages.

### Schema

```sql
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    data TEXT NOT NULL,                          -- JSON payload (smart-merged for deltas)
    direction TEXT NOT NULL DEFAULT 'outbound',  -- inbound | outbound
    created_at INTEGER NOT NULL                  -- Unix milliseconds
);

CREATE INDEX idx_events_session_seq ON events(session_id, seq);
CREATE INDEX idx_events_created ON events(created_at);
```

### Event Type Filter

**Stored (user-replayable):** `init`, `error`, `state`, `input`, `done`, `message`, `message.start`, `message.end`, `tool_call`, `tool_result`, `reasoning`, `step`, `permission_request`, `permission_response`, `question_request`, `question_response`, `elicitation_request`, `elicitation_response`, `context_usage`, `control`

**Excluded:** `ping`, `pong`, `raw` — no replay value, excessive volume.

### Smart Delta Merging

`message.delta` events are merged in-memory into a single `message` row:

```
message.start(seq=10)  → stored as independent row
message.delta(seq=11, "Hello")   ┐
message.delta(seq=12, " world")  ├→ merged: type="message", data={"content":"Hello world!", "merged_count":3, "seq_range":[11,13]}
message.delta(seq=13, "!")       ┘
message.end(seq=14)     → stored as independent row
```

Merge triggers: `message.end` or next non-delta event.

### EventStore Interface

```go
// CursorDirection controls pagination direction relative to a cursor seq value.
type CursorDirection int

const (
    CursorLatest CursorDirection = iota // no cursor → fetch latest N
    CursorAfter                         // seq > cursor → newer events (incremental catch-up)
    CursorBefore                        // seq < cursor → older events (load history)
)

type EventStore interface {
    Append(ctx context.Context, event *StoredEvent) error

    // QueryBySession fetches events with cursor-based bidirectional pagination.
    //   dir=CursorLatest, cursor=0  → latest N events (initial load)
    //   dir=CursorAfter,  cursor=X  → events with seq > X (catch-up)
    //   dir=CursorBefore, cursor=X  → events with seq < X (load older history)
    // Returns events always in seq ASC order. Sets OldestSeq/NewestSeq/HasOlder/TotalCount in result.
    QueryBySession(ctx context.Context, sessionID string, cursor int64, dir CursorDirection, limit int) (*EventPage, error)

    DeleteBySession(ctx context.Context, sessionID string) error
    DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error)
    Close() error
}

type StoredEvent struct {
    SessionID string
    Seq       int64
    Type      string
    Data      json.RawMessage
    Direction string // "inbound" | "outbound"
    CreatedAt int64  // Unix ms
}

// EventPage is a page of events with pagination metadata.
type EventPage struct {
    Events    []*StoredEvent
    OldestSeq int64 // smallest seq in this page
    NewestSeq int64 // largest seq in this page
    HasOlder  bool  // true if older events exist beyond OldestSeq
    TotalCount int64 // total events for this session
}
```

## Collection Pipeline

### Collection Point: Bridge.forwardEvents()

```
Worker → Bridge.forwardEvents()
              ├─ Hub.SendToSession()     (existing forwarding)
              ├─ ConvStore.Append()      (existing aggregation on done)
              └─ EventCollector.Capture() (new: all replayable events)
```

### Collector Component

```go
type Collector struct {
    store    EventStore
    captureC chan *captureRequest  // capacity 2048
    log      *slog.Logger

    // delta merge state (per-session)
    accumMu  sync.Mutex
    accum    map[string]*deltaAccumulator
}
```

### Write Pipeline

```
captureC → batch writer goroutine:
    accumulate up to 100 items or 100ms
    → BEGIN TRANSACTION
    → INSERT INTO events ... (batch)
    → COMMIT
```

- Non-blocking channel send (drops on full with warn log)
- Graceful shutdown: drain channel + flush remaining batch
- Pattern consistent with existing ConversationStore

### Lifecycle

- **Created** during `GatewayDeps` initialization, same level as ConvStore
- **Injected** into Bridge via constructor parameter
- **Shutdown order**: Hub → Collector → ConvStore
- **Session cleanup**: `DeleteBySession` called when session is physically deleted

### Relationship with ConversationStore

Completely parallel, neither replaces the other:
- ConvStore: turn-level aggregation for admin UI / statistics
- EventStore: event-level storage for WebChat replay

## Replay API

### Endpoint

`GET /api/sessions/{id}/events`

**Query parameters:**

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `cursor` | int64 | (omit) | Seq value used as pagination anchor |
| `direction` | string | `"latest"` | `"latest"` / `"after"` / `"before"` |
| `limit` | int | 200 | Max events to return (max 1000) |

**Three pagination modes:**

| Mode | `cursor` | `direction` | SQL | Use case |
|------|----------|-------------|-----|----------|
| Initial load | omit | `"latest"` | `ORDER BY seq DESC LIMIT ?` then reverse | Page refresh, first visit |
| Load older | oldest displayed seq | `"before"` | `WHERE seq < ? ORDER BY seq DESC LIMIT ?` then reverse | Scroll to top, "load more" |
| Catch-up | newest displayed seq | `"after"` | `WHERE seq > ? ORDER BY seq ASC LIMIT ?` | Reconnect, incremental |

All modes return events in **seq ASC order**. The `cursor` value itself is excluded from results.

**Response:**

```json
{
  "session_id": "abc-123",
  "events": [
    {
      "seq": 1,
      "type": "init",
      "data": { "capabilities": {} },
      "direction": "outbound",
      "created_at": 1746000000000
    },
    {
      "seq": 10,
      "type": "message",
      "data": {
        "content": "Hello world!",
        "merged_count": 3,
        "seq_range": [11, 13]
      },
      "direction": "outbound",
      "created_at": 1746000001000
    }
  ],
  "oldest_seq": 1,
  "newest_seq": 10,
  "has_older": true,
  "total_count": 456
}
```

### Auth

Reuses existing Gateway API JWT middleware. Verifies requester is session owner.

## WebChat Integration

### Three Scenarios

| Scenario | API call | Effect |
|----------|----------|--------|
| Page initial load | `direction=latest`, `limit=200` | Get latest 200 events |
| Scroll to top, "load more" | `cursor={oldestSeq}`, `direction=before`, `limit=200` | Get 200 older events before current oldest |
| Reconnect catch-up | `cursor={newestSeq}`, `direction=after`, `limit=200` | Get events missed during disconnect |

```
Page load:
  1. Read sessionID from localStorage
  2. GET /api/sessions/{id}/events?direction=latest&limit=200
  3. Render events to UI in seq order
  4. Track oldestSeq and newestSeq from response
  5. Establish WebSocket for live events

Scroll to top ("load more"):
  1. GET /api/sessions/{id}/events?cursor={oldestSeq}&direction=before&limit=200
  2. Prepend returned events to top of UI
  3. Update oldestSeq from response

Reconnect:
  1. GET /api/sessions/{id}/events?cursor={newestSeq}&direction=after&limit=200
  2. Append returned events to bottom of UI
  3. Update newestSeq from response
```

### Merged Event Handling

Client recognizes merged `message` events by `merged_count > 0`:
- Render complete text immediately (no streaming animation)
- Use `seq_range` for ordering relative to adjacent events

### WebChat Changes

1. New API client method: `fetchSessionEvents(sessionID, { cursor?, direction, limit })`
2. Modified session init: fetch history before connecting WS
3. Track `oldestSeq` / `newestSeq` from response for bidirectional pagination
4. Merged event rendering logic

## File Layout

```
internal/eventstore/
    store.go           # EventStore interface + SQLiteStore implementation
    collector.go       # Collector + delta accumulator + channel writer
    storeable.go       # storeable event types set + filter logic
    sql/
        schema.sql     # events table DDL
        queries.sql    # insert, select by session, delete

cmd/hotplex/routes.go  # add GET /api/sessions/{id}/events route
internal/admin/
    events.go          # new handler for events API endpoint

webchat/               # frontend changes for history replay
```

## Migration

1. New SQLite file created automatically on first gateway start
2. No migration of existing conversation data — old history remains in ConvStore
3. Event collection starts immediately for new sessions after deployment
