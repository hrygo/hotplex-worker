# Go Client SDK

## OVERVIEW
Standalone Go module (github.com/hotplex/hotplex-go-client) for connecting to HotPlex Worker Gateway via WebSocket + AEP v1 protocol. Separate go.mod from main gateway. Typed event constants + data helpers.

## STRUCTURE
```
client.go    # Client struct: Connect, Resume, SendInput, SendPermissionResponse, SendQuestionResponse, SendElicitationResponse, SendControl, SendReset, SendGC, Close, Events
events.go    # Typed event constants (EventMessageDelta, EventDone, etc.) + data helpers (AsDoneData, AsErrorData, AsToolCallData)
options.go   # Functional options (AutoReconnect, ClientSessionID, Metadata, Logger, PingInterval)
token.go     # TokenGenerator for JWT creation
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Client struct | `client.go:33` | url, workerType, authToken, apiKey, state machine |
| Connect flow | `client.go:93` | WebSocket dial → send init → recv init_ack → start pumps |
| Resume session | `client.go:99` | Reconnect with existing session_id |
| Event stream | `client.go:193` | Events() returns read-only channel of Event structs |
| Send input | `client.go:212` | SendInput: enqueue AEP envelope via sendCh |
| Connection pumps | `client.go:281` | recvPump (read WS → parse → deliver), sendPump (sendCh → WS) |
| Heartbeat | `client.go:353` | pingPump: periodic ping at DefaultPingInterval |
| Event constants | `events.go` | 18+ typed constants matching pkg/events Kind values |
| Event data helpers | `events.go` | AsDoneData(), AsErrorData(), AsToolCallData(), etc. |
| Functional options | `options.go` | URL(), WorkerType(), AuthToken(), AutoReconnect(), ClientSessionID(), Metadata() |

## KEY PATTERNS

**Three goroutines per connection**
- recvPump: reads WS frames, parses NDJSON envelopes, delivers to eventsCh
- sendPump: reads from sendCh (cap SendChannelCap=256), writes to WS
- pingPump: ticker-based ping with context cancellation

**State machine**
- Client tracks connection state internally
- Connect/Resume use shared doConnect() with different init data
- Close: cancel context → wait for goroutine group (wg)

**Event delivery**
- Events() returns `<-chan Event` (read-only)
- Event struct: Type (string), Seq (int64), Session (string), Data (any)
- Data helpers: `evt.AsDoneData()`, `evt.AsErrorData()`, `evt.AsToolCallData()` for type-safe access

**Functional options pattern**
- `client.New(ctx, URL(...), WorkerType(...), AuthToken(...), AutoReconnect(true))`
- `ClientSessionID("my-session-001")` for UUIDv5 deterministic mapping
- `Metadata(map[string]any{"key": "val"})` for init handshake metadata

**Error handling**
- ErrNotConnected sentinel for operations before Connect()
- isClosedWS checks for websocket.CloseError

## COMMANDS
```bash
go test ./...       # Run client tests
go mod tidy         # Clean deps
```
