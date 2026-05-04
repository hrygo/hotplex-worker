# Event Store Package

## OVERVIEW
Session event persistence with SQLite backend, delta accumulation/coalescing, batched writes, and cursor-based pagination. Collector pattern with channel-based async writer.

## STRUCTURE
```
eventstore/
  store.go            # EventStore interface, SQLiteStore, StoredEvent, EventPage, cursor pagination
  collector.go        # Collector: delta accumulator, batched async writer, timed flush
  sql/                # Embedded SQL queries (go:embed)
    queries/          # Per-query .sql files
  store_test.go       # Store tests
  collector_test.go   # Collector tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Event storage interface | `store.go` EventStore | Append, Query, DeleteBySession, BeginTx |
| SQLite implementation | `store.go:123` SQLiteStore | WAL mode, busy_timeout |
| Stored event schema | `store.go:74` StoredEvent | session_id, seq, type, data, direction, created_at |
| Pagination | `store.go` EventPage | cursor-based (oldest/newest seq), HasOlder flag |
| Delta coalescing | `collector.go:62` Collector | per-session deltaAccumulator, flush on MessageEnd or timeout |
| Batch writer | `collector.go:169` runWriter | batch up to collectorBatchMax, flush on interval or close |
| Event filtering | `collector.go:90` IsStorable | Only storable event types persisted |

## KEY PATTERNS

**Delta accumulation**: message.delta events are accumulated per-session in memory. Flushed to single StoredEvent when:
- MessageEnd received for session
- Content exceeds deltaFlushSize threshold
- Timeout (deltaFlushInterval) exceeded

**Batch writer goroutine**: Single writer reads from captureC channel (cap 1024). Batches up to collectorBatchMax items. Flushes on ticker interval or channel close. Uses transaction for batch atomicity.

**Cursor pagination**: Query returns EventPage with oldest/newest seq bounds. HasOlder flag for backward navigation. No offset-based pagination.

**Drop on full**: captureC channel non-blocking send — events silently dropped if channel full (logged as warning).

## ANTI-PATTERNS
- ❌ Store every message.delta individually — use Collector coalescing
- ❌ Query with OFFSET — use cursor-based pagination
- ❌ Skip flushTimedOutAccumulators — orphaned deltas would leak memory
- ❌ Access SQLite directly — use EventStore interface for testability
