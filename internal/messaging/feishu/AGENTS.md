# Feishu Adapter

## OVERVIEW
Feishu (Lark) platform adapter using ws.Client for P2 event-driven messaging. Handles message conversion, streaming output, typing indicators, interaction cards (permission/Q&A/elicitation), rate limiting, STT, and media download.

## STRUCTURE
```
feishu/
  adapter.go          # Adapter struct, lifecycle, session mgmt (971 lines)
  converter.go        # Feishu msg → AEP event extraction (239 lines)
  streaming.go        # Chunked streaming output with intervals (756 lines)
  interaction.go      # Permission/Q&A/elicitation card handling (321 lines)
  markdown.go         # AEP markdown → Feishu rich text conversion (487 lines)
  typing.go           # Typing indicator reaction cycling (93 lines)
  events.go           # P2 event type constants (62 lines)
  chat_queue.go       # Serial message send queue per chat (105 lines)
  gate.go             # Gate: concurrent session limiter (98 lines)
  dedup.go            # Message deduplication (91 lines)
  rate_limiter.go     # Per-user rate limiter (79 lines)
  mention.go          # @mention extraction (35 lines)
  abort.go            # Session abort handling (28 lines)
  stt.go              # STT: FeishuSTT, LocalSTT, PersistentSTT, FallbackSTT, Transcriber interface
  sdk_logger.go       # Lark SDK → slog adapter (25 lines)
  *_test.go           # Adapter, converter, markdown, chat_queue, streaming, interaction, gate_dedup tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Message lifecycle | `adapter.go:154` | `handleMessage()` → parse P2 event → dispatch |
| Text message handling | `adapter.go:273` | `handleTextMessage()` → Bridge.Handle → session start |
| Connection management | `adapter.go:329` | `GetOrCreateConn()` — dedup + activeConns map |
| Streaming output | `streaming.go` | `FeishuConn.EnableStreaming()` → chunked updates with intervals |
| Permission/Q&A cards | `interaction.go` | `sendPermissionRequest()`, `sendQuestionRequest()`, `sendElicitationRequest()` |
| Typing indicator | `typing.go` | Reaction cycle: ⏳ → 🔄 → ✅ based on stream progress |
| Markdown conversion | `markdown.go` | AEP markdown → Feishu rich text `post` format |
| Message conversion | `converter.go` | Extract text/events from Feishu P2 message events |
| Rate limiting | `rate_limiter.go` | Per-user token bucket |
| Chat send queue | `chat_queue.go` | Serial message send per chatID (prevent reorder) |
| Media download | `adapter.go` | `downloadMedia()` — file/image download with MIME detection |
| Session gate | `gate.go` | Concurrent session limiter per user |
| Message dedup | `dedup.go` | Event ID dedup with TTL cleanup |
| Speech-to-text | `stt.go` | 4 implementations: FeishuSTT (cloud), LocalSTT, PersistentSTT, FallbackSTT |

## KEY PATTERNS

**P2 event flow**
```
Feishu WebSocket → ws.Client.Events → handleMessage() → handleTextMessage()
  → Bridge.Handle() → StartPlatformSession → Join → forwardEvents
  → FeishuConn.WriteCtx() → replyMessage()/sendTextMessage()
```

**Streaming with reactions**
1. `EnableStreaming()` → start stream, set ⏳ reaction
2. Each chunk → update message content + cycle reaction (🔄)
3. Stream complete → final update + ✅ reaction

**Interaction cards (`interaction.go`)**
- Permission request: display-only card (WS client doesn't forward card.action.trigger)
- Users respond by typing "允许/allow" or "拒绝/deny"
- Q&A and elicitation: structured card with instructions
- 5min auto-deny timeout via InteractionManager

**Chat send queue (`chat_queue.go`)**
- Per `chatID` serial queue prevents message reordering
- `writeC` channel (cap 64) + single goroutine per chat
- Non-blocking send with drop on full channel

**Markdown → Rich text (`markdown.go`)**
- AEP markdown parsed → Feishu `post` content blocks
- Supports: bold, italic, code, code_block, link, list, heading

**Reaction-based typing**
- ⏳ (thinking) → 🔄 (streaming) → ✅ (done) / ❌ (error)
- `cycleReaction()` manages reaction add/remove lifecycle

**Streaming card 4-layer defense**
- TTL guard (10min) → integrity check → retry with backoff → IM Patch fallback
- Handles degraded CardKit gracefully

## ANTI-PATTERNS
- ❌ Send messages without `chatQueue` — causes reorder in group chats
- ❌ Skip reaction cleanup on stream abort — always remove ⏳/🔄 on error
- ❌ Use `math/rand` for dedup TTL — use `crypto/rand` via UUID
- ❌ Assume card.action.trigger works — WS client doesn't forward them