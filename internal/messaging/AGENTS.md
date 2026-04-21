# Messaging Package (Slack/Feishu Bidirectional)

## OVERVIEW
Bidirectional messaging bridge connecting Slack/Feishu platforms to the gateway session lifecycle. Self-registering adapter pattern with platform-agnostic Bridge layer. Interaction management with timeout + auto-deny.

## STRUCTURE
```
messaging/
  platform_conn.go       # PlatformConn interface: WriteCtx + Close
  platform_adapter.go    # PlatformAdapter base + registry (Register/New/RegisteredTypes)
  bridge.go              # Bridge: 3-step join (StartSession → Join → Handle)
  interaction.go         # InteractionManager: user permission/Q&A/elicitation with timeout + auto-deny
  control_command.go     # Slash commands (/gc, /reset, /park, /restart, /new) + $prefix natural language
  sanitize.go            # Text sanitization: control chars, null bytes, BOM, surrogates
  integration_test.go     # Cross-adapter integration tests
  slack/                 # Slack Socket Mode adapter (18 files)
  feishu/                # Feishu ws.Client adapter (15 files + STT)
  mock/                  # Mock adapter for testing
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new platform adapter | `internal/messaging/<name>/` | Embed `PlatformAdapter`, implement `PlatformAdapterInterface`: `Platform()`/`Start()`/`HandleTextMessage()`/`Close()` |
| Wire adapter in main | `cmd/worker/main.go` | `startMessagingAdapters()`: config → New → Configure → SetHub/SetSM/SetHandler/SetBridge → Start |
| Bridge lifecycle | `bridge.go` | 3-step: `StartPlatformSession` → `JoinPlatformSession` → `Handle` |
| PlatformConn interface | `platform_conn.go:11` | `WriteCtx(ctx, env)` + `Close()` — the contract gateway uses to send to platforms |
| Adapter registration | `platform_adapter.go:47` | `Register(PlatformType, Builder)` — called in each adapter's `init()` |
| Interaction management | `interaction.go` | InteractionManager: timeout (5min) + auto-deny for permission/Q&A/elicitation |
| Control commands | `control_command.go` | Slash + $prefix parsing, natural language triggers |
| Text sanitization | `sanitize.go` | `SanitizeText()` — all user-facing output must pass through |

## KEY PATTERNS

**Adapter self-registration (init + blank import)**
```go
// internal/messaging/slack/adapter.go
func init() { messaging.Register(messaging.PlatformSlack, func() (PlatformAdapterInterface, error) { return New(), nil }) }

// cmd/worker/main.go
_ "hotplex-worker/internal/messaging/slack"
_ "hotplex-worker/internal/messaging/feishu"
```

**3-step Bridge lifecycle**
1. `Bridge.Handle(platform, msg)` → `starter.StartPlatformSession(...)` → creates worker session
2. `Bridge.JoinSession(sessionID, platformconn)` → `hub.JoinPlatformSession(...)` → registers conn in hub
3. Platform conn receives events via `WriteCtx` → forwards to platform API

**PlatformConn implementations**
- `SlackConn`: channelID + threadTS, uses Slack chat.postMessage API
- `FeishuConn`: chatID + chatType, uses Feishu reply message API

**Streaming writer pattern**
- Both adapters provide `NewStreamingWriter()` returning `io.WriteCloser`
- Slack: `SlackStreamingWriter` — 150ms flush interval, 20-rune threshold, 3 append retries, 10min TTL
- Feishu: chunked streaming — intervals between message updates

**Interaction flow**
- Worker sends permission/question/elicitation request → InteractionManager registers with timeout
- Adapter renders platform-specific card/message → user responds
- Response delivered back to worker via gateway → auto-deny after 5min if no response

## ANTI-PATTERNS
- ❌ Skip `SetHub`/`SetSM`/`SetHandler`/`SetBridge` — all 4 must be called before `Start()`
- ❌ Create platform connections without dedup — always use `GetOrCreateConn`
- ❌ Send messages directly — use `Bridge.Handle()` to ensure session lifecycle
- ❌ Skip `SanitizeText()` on user-facing output