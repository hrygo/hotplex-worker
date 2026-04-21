# Slack Adapter

## OVERVIEW
Slack platform adapter using Socket Mode for real-time messaging. Handles streaming output, message chunking, dedup, slash commands, interaction blocks, rate limiting, and image blocks.

## STRUCTURE
```
slack/
  adapter.go          # Adapter struct, lifecycle, session mgmt (756 lines)
  stream.go           # SlackStreamingWriter: 150ms flush, 20-rune threshold, 3 retries (362 lines)
  interaction.go      # Permission/Q&A/elicitation block handling (445 lines)
  slash_command.go    # Slash command handlers (/gc, /reset, /park, /restart, /new) (182 lines)
  status.go           # Session status indicators (222 lines)
  chunker.go          # Long message splitting (Slack ~4000 char limit)
  dedup.go            # TTL-based message deduplication
  validator.go        # Input validation + sanitization
  converter.go        # Slack msg → AEP event extraction
  format.go           # Markdown → Slack mrkdwn conversion
  backoff.go          # Exponential backoff for Slack API retries
  gate.go             # Gate: concurrent session limiter per user (118 lines)
  rate_limiter.go     # Per-user rate limiter
  image_blocks.go     # Image block construction for file sharing (111 lines)
  mention.go          # @mention extraction
  typing.go           # Typing indicator
  abort.go            # Session abort handling
  events.go           # Socket Mode event type constants
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Message lifecycle | `adapter.go` | `handleMessage()` → parse event → dispatch |
| Text message handling | `adapter.go` | `handleTextMessage()` → Bridge.Handle → session start |
| Connection management | `adapter.go` | `GetOrCreateConn()` — dedup + activeConns map |
| Streaming output | `stream.go` | `SlackStreamingWriter`: 150ms flush, 20-rune threshold, 10min TTL |
| Slash commands | `slash_command.go` | /gc, /reset, /park, /restart, /new handlers |
| Interaction blocks | `interaction.go` | Permission/Q&A/elicitation via Slack Block Kit |
| Session status | `status.go` | Visual status indicators in Slack |
| Image/file blocks | `image_blocks.go` | Construct Slack image blocks for file sharing |
| Message chunking | `chunker.go` | Split long messages under Slack ~4000 char limit |
| Dedup | `dedup.go` | TTL-based duplicate message filter |
| Rate limiting | `rate_limiter.go` | Per-user token bucket |
| Session gate | `gate.go` | Concurrent session limiter per user |
| Backoff | `backoff.go` | Exponential backoff for API rate limit responses |

## KEY PATTERNS

**Socket Mode event flow**
```
Slack Socket Mode → socketmode.Client → handleMessage() → handleTextMessage()
  → Bridge.Handle() → StartPlatformSession → Join → forwardEvents
  → SlackConn.WriteCtx() → chat.postMessage/update
```

**SlackStreamingWriter (`stream.go`)**
- 150ms flush interval for responsive streaming
- 20-rune threshold for immediate flush
- Max 3 append retries with 50ms backoff
- 10min TTL guard against stale streams
- maxAppendSize 3000 (Slack ~4000 limit with safety margin)

**Message pipeline**
```
worker output → chunker (split long messages) → dedup (TTL duplicate filter) → format (mrkdwn) → rate limiter → send
```

**Interaction blocks (`interaction.go`)**
- Permission request: Block Kit with approve/deny buttons
- Q&A: structured blocks with answer prompts
- Elicitation: MCP elicitation via Block Kit
- 5min auto-deny timeout via InteractionManager

**Slash commands (`slash_command.go`)**
- `/gc` — archive session, terminate worker
- `/reset` — clear context, restart worker
- `/park` — idle session, keep for resume
- `/restart` — restart current session
- `/new` — create fresh session

## ANTI-PATTERNS
- ❌ Send messages exceeding ~4000 chars — use chunker
- ❌ Skip dedup on message sends — causes duplicate output
- ❌ Ignore Slack rate limits — use backoff + rate limiter
- ❌ Block on streaming write — use non-blocking with retry
- ❌ Skip status indicator update on state change
