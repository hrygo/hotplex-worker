# Multi-Bot Support Design

**Date**: 2026-05-13
**Status**: Draft
**Author**: Claude + 黄飞虹

---

## Problem

HotPlex currently supports only one bot per messaging platform (Slack/Feishu). Both platforms natively support multiple bots in a single workspace, enabling:

1. **Multi-persona** — Different bots with distinct roles (tech support, HR, customer service)
2. **Load distribution** — Multiple bot instances sharing message volume
3. **Multi-tenant isolation** — Different teams/customers using different bots with isolated configs

The lower layers (session isolation, agent config, JWT security) already support multi-bot via BotID. The upper layers (config, adapter initialization, bridge creation) are limited to a single bot per platform.

## Approach: Adapter-per-Bot

Each bot gets an independent Adapter + Bridge + ConnPool instance with its own WebSocket connection to the platform.

**Why this approach:**
- Slack and Feishu both require independent credentials per bot, making connection-sharing infeasible
- Minimal changes to existing architecture — no Adapter interface modifications needed
- Natural fault isolation between bots
- Straightforward lifecycle management

**Limitation:** Slack Socket Mode allows max 10 WebSocket connections per app. Feishu has similar enterprise-dependent limits. This design targets ≤10 bots per platform.

## Configuration

### New `BotConfig` struct

Each platform has its own bot config type with platform-specific credential fields:

```go
// SlackBotConfig — credentials are bot_token + app_token
type SlackBotConfig struct {
    Name       string                `mapstructure:"name"`
    BotToken   string                `mapstructure:"bot_token"`
    AppToken   string                `mapstructure:"app_token"`
    Soul       string                `mapstructure:"soul,omitempty"`
    WorkerType string                `mapstructure:"worker_type,omitempty"`
    STT        *STTConfig            `mapstructure:"stt,omitempty"`
    TTS        *TTSConfig            `mapstructure:"tts,omitempty"`
}

// FeishuBotConfig — credentials are app_id + app_secret
type FeishuBotConfig struct {
    Name       string                `mapstructure:"name"`
    AppID      string                `mapstructure:"app_id"`
    AppSecret  string                `mapstructure:"app_secret"`
    Soul       string                `mapstructure:"soul,omitempty"`
    WorkerType string                `mapstructure:"worker_type,omitempty"`
    STT        *STTConfig            `mapstructure:"stt,omitempty"`
    TTS        *TTSConfig            `mapstructure:"tts,omitempty"`
}
```

### Config YAML structure

```yaml
messaging:
  slack:
    enabled: true
    # Legacy single-bot format (backward compatible)
    bot_token: xoxb-legacy
    app_token: xapp-legacy

    # New multi-bot format
    bots:
      - name: tech-support
        bot_token: xoxb-aaa
        app_token: xapp-aaa
        soul: tech-support
        worker_type: claude_code
        stt:
          enabled: false
        tts:
          enabled: true

      - name: hr-bot
        bot_token: xoxb-bbb
        app_token: xapp-bbb
        soul: hr-assistant
```

Same pattern applies to `feishu:` with `app_id`/`app_secret` credential fields:

```yaml
messaging:
  feishu:
    enabled: true
    bots:
      - name: feishu-tech
        app_id: cli_xxx
        app_secret: xxx
        soul: tech-support
      - name: feishu-hr
        app_id: cli_yyy
        app_secret: yyy
        soul: hr-assistant
```

### Backward compatibility

`normalizeBots()` logic in config parsing:
- If `bots[]` is non-empty → ignore top-level single-bot credentials
- If `bots[]` is empty but top-level credentials exist → auto-wrap as `bots: [{name: "default", bot_token: ..., app_token: ...}]`
- If both are empty → no bots created for this platform

Environment variable support: `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` (singular) maps to the default bot in backward-compat mode. Multi-bot requires config file.

## Initialization

### Changes to `messaging_init.go`

Current flow:
```
for platform in RegisteredTypes():
    adapter = builder()
    bridge = new Bridge(adapter)
    adapter.Start()
```

New flow:
```
for platform in RegisteredTypes():
    bots = resolveBots(platform)  // parse config, handle backward compat
    for bot in bots:
        adapter = builder()
        adapter.ConfigureWith(botConfig)
        bridge = new Bridge(adapter)
        adapter.Start()
        botRegistry.Register(bot.Name, platform, adapter, bridge)
```

### AdapterConfig extension

```go
type AdapterConfig struct {
    // existing fields...
    BotName    string
    Soul       string
    WorkerType string  // per-bot override
    STTConfig  *STTConfig
    TTSConfig  *TTSConfig
}
```

### Bridge

Each bot gets its own `messaging.Bridge` instance. All bridges share the same `SessionStarter` (gateway bridge). No Bridge interface changes required — `SetAdapter()` already binds a single adapter.

## Message Routing

**No additional routing logic needed.** Platform APIs handle @mention routing automatically:

- **Slack**: Each Socket Mode connection (per bot_token) only receives events for that bot (mentions and DMs). The platform filters at the API level.
- **Feishu**: Each bot's WebSocket connection only receives events addressed to that bot.

Direct messages to a specific bot go to that bot's adapter exclusively.

## Per-Bot Agent Config

The existing 3-level fallback already supports per-bot configuration:

```
~/.hotplex/agent-configs/
  SOUL.md              # global
  slack/               # platform-level
    SOUL.md
  slack/U12345/        # bot-level (highest priority)
    SOUL.md
    AGENTS.md
```

`BotConfig.Soul` field serves as:
- Bot display name in logs and status API
- Future soul template selector
- Current version: personas managed via filesystem directory structure (no changes needed)

## Bot Status Query API

### Endpoints

```
GET /admin/bots          → list all active bots
GET /admin/bots/{name}   → single bot details
```

### Response schema

```json
{
  "bots": [
    {
      "name": "tech-support",
      "platform": "slack",
      "bot_id": "U12345",
      "status": "running",
      "connected_at": "2026-05-13T20:00:00Z",
      "sessions": 3,
      "soul": "tech-support",
      "worker_type": "claude_code",
      "stt_enabled": false,
      "tts_enabled": true
    }
  ]
}
```

### Implementation

New `internal/messaging/bot_registry.go`:
- Thread-safe registry: `map[string]*BotEntry` (keyed by `platform/botName`)
- `BotEntry` holds: adapter reference, bridge reference, status, connected timestamp
- Registered during `startMessagingAdapters()`, updated on adapter lifecycle events
- Queried by admin API handlers

## Validation and Error Handling

### Startup validation

- Duplicate bot `name` within same platform → startup error
- Missing credentials (empty bot_token/app_token) → skip bot with warning log
- All bots fail to start → gateway exits with error
- Bot count exceeds platform limit (default 10) → startup warning, excess bots ignored

### Runtime behavior

- Individual bot connection failure does not crash other bots
- Bot status tracks: `starting` → `running` → `stopped` / `error`
- Admin API reflects real-time status for each bot

### Graceful shutdown

Iterate all registered bots in reverse creation order:
1. adapter.Stop() — close platform connections
2. bridge.Shutdown() — drain sessions
3. botRegistry.Unregister() — clean up

Existing shutdown ordering (signal → cancel ctx → hub → bridge → sessionMgr → HTTP) is preserved.

## Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `SlackBotConfig`/`FeishuBotConfig` structs, `normalizeBots()` to `SlackConfig`/`FeishuConfig` |
| `cmd/hotplex/messaging_init.go` | Nested loop: platforms × bots, register to bot registry |
| `internal/messaging/platform_adapter.go` | Extend `AdapterConfig` with bot-specific fields |
| `internal/messaging/bot_registry.go` | **New** — thread-safe bot registry |
| `cmd/hotplex/routes.go` | Add `/admin/bots` and `/admin/bots/{name}` endpoints |
| `configs/config.yaml` | Add example `bots[]` section (commented out) |

No changes needed in:
- `internal/session/` — BotID isolation already works
- `internal/agentconfig/` — 3-level fallback already works
- `internal/security/` — JWT bot_id claim already works
- `internal/gateway/bridge.go` — `StartPlatformSession(botID)` already parameterized
- Adapter internals (`slack/`, `feishu/`) — no interface changes

## Scope Exclusion

- Runtime bot add/remove (future: hot-reload or admin API)
- Cross-bot message forwarding or handoff
- Bot load balancing (multiple adapters sharing one bot identity)
- Per-bot rate limiting or quota management
