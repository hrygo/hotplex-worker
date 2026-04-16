## OVERVIEW
WebSocket broadcast hub with AEP v1 protocol dispatch, per-connection read/write pumps, and session-worker lifecycle orchestration.

## STRUCTURE
```
hub.go          # Hub struct: broadcast loop, conn/session registry, backpressure, seq gen
conn.go         # Conn struct: read/write pumps, Handler struct for event dispatch
bridge.go       # Bridge struct: session ↔ worker lifecycle, event forwarding
init.go         # Init handshake: InitData, InitAckData, caps, ValidateInit, BuildInitAck
heartbeat.go    # Missed ping counter with stop channel
*_test.go       # 5 test files (conn_test, hub_test, ctrl_test, bridge_test, init_test)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Broadcast hub | `hub.go:57` | Hub struct, Run() goroutine, seq gen |
| Connection pumps | `conn.go:27` | Conn struct, ReadPump/WritePump goroutines |
| Event dispatch | `handler.go` | Handler: handleInput, handlePing, handleControl |
| Session lifecycle | `bridge.go` | Bridge: StartSession, ResumeSession, forwardEvents |
| Init handshake | `init.go` | 30s timeout, first frame must be "init" |
| Heartbeat | `heartbeat.go:12` | Missed ping tracking |

## KEY PATTERNS

**Hub goroutine (hub.go)**
- Run() loop: select on broadcast channel + ctx.Done()
- broadcast chan *EnvelopeWithConn (buffered, size from config)
- seqGen *SeqGen: per-session monotonic seq allocation
- sessionDropped map: tracks delta drops per session

**Conn pumps (conn.go)**
- ReadPump: reads WS frames, dispatches via Handler, defers cleanup
- WritePump: exits on done/heartbeat stopped
- mu sync.Mutex protects closed flag

**Bridge lifecycle (bridge.go)**
- StartSession: create session → start worker → attach → forward events
- ResumeSession: resume existing → re-attach worker
- forwardEvents: goroutine reads worker.Recv() and broadcasts via hub

**Backpressure**
- message.delta/raw: non-blocking select, drop if broadcast full
- state/done/error: blocking send, never dropped

**Init handshake (init.go)**
- 30s timeout from first connection
- First frame must be type="init"
- InitError returned on validation failure

## ANTI-PATTERNS
- ❌ Skip heartbeat stop on connection close
- ❌ Send on closed broadcast channel
- ❌ Handle input after session terminated without mutex
- ❌ Allow init after 30s timeout
