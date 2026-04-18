# Feishu Adapter

## OVERVIEW
Feishu (Lark) platform adapter using ws.Client for P2 event-driven messaging. Handles message conversion, streaming output, typing indicators, rate limiting, and media download.

## STRUCTURE
```
feishu/
  adapter.go          # Adapter struct, lifecycle, session mgmt (783 lines)
  converter.go        # Feishu msg → AEP event extraction (239 lines)
  streaming.go        # Chunked streaming output with intervals (544 lines)
  markdown.go         # AEP markdown → Feishu rich text conversion (487 lines)
  typing.go           # Typing indicator reaction cycling (93 lines)
  events.go           # P2 event type constants (62 lines)
  chat_queue.go       # Serial message send queue per chat (105 lines)
  gate.go             # Gate: concurrent session limiter (98 lines)
  gate_dedup_test.go  # Gate dedup test (255 lines)
  dedup.go            # Message deduplication (91 lines)
  rate_limiter.go     # Per-user rate limiter (79 lines)
  mention.go          # @mention extraction (35 lines)
  abort.go            # Session abort handling (28 lines)
  sdk_logger.go       # Lark SDK → slog adapter (25 lines)
  *_test.go           # Adapter, converter, markdown, chat_queue tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Message lifecycle | `adapter.go:154` | `handleMessage()` → parse P2 event → dispatch |
| Text message handling | `adapter.go:273` | `handleTextMessage()` → Bridge.Handle → session start |
| Connection management | `adapter.go:329` | `GetOrCreateConn()` — dedup + activeConns map |
| Streaming output | `streaming.go` | `FeishuConn.EnableStreaming()` → chunked updates with intervals |
| Typing indicator | `typing.go` | Reaction cycle: ⏳ → 🔄 → ✅ based on stream progress |
| Markdown conversion | `markdown.go` | AEP markdown → Feishu rich text `post` format |
| Message conversion | `converter.go` | Extract text/events from Feishu P2 message events |
| Rate limiting | `rate_limiter.go` | Per-user token bucket |
| Chat send queue | `chat_queue.go` | Serial message send per chatID (prevent reorder) |
| Media download | `adapter.go:646` | `downloadMedia()` — file/image download with MIME detection |
| Session gate | `gate.go` | Concurrent session limiter per user |
| Message dedup | `dedup.go` | Event ID dedup with TTL cleanup |

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

## ANTI-PATTERNS
- ❌ Send messages without `chatQueue` — causes reorder in group chats
- ❌ Skip reaction cleanup on stream abort — always remove ⏳/🔄 on error
- ❌ Use `math/rand` for dedup TTL — use `crypto/rand` via UUID