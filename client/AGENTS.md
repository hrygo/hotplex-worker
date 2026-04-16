# Go Client SDK

## OVERVIEW
Standalone Go module (github.com/hotplex/client) for connecting to HotPlex Worker Gateway via WebSocket + AEP v1 protocol. Separate go.mod from main gateway.

## STRUCTURE
```
client.go    # Client struct: Connect, Resume, SendInput, SendPermissionResponse, SendControl, Close, Events
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

**Error handling**
- ErrNotConnected sentinel for operations before Connect()
- isClosedWS checks for websocket.CloseError

## COMMANDS
```bash
go test ./...       # Run client tests
go mod tidy         # Clean deps
```
