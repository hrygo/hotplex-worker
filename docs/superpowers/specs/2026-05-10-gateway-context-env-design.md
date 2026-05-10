# Gateway Context Environment Variables Design

**Date**: 2026-05-10
**Status**: Approved
**Branch**: feat/cron-scheduler

## Problem

Worker processes cannot perceive gateway runtime context. When a worker needs to interact with the gateway environment (calling platform APIs, constructing skill/tool paths, or understanding its session context), it must resort to complex exploration like reading logs and analyzing data. Platform context such as bot_id, user_id, channel_id, and thread_id exists in the gateway but is not consistently injected into worker processes.

## Solution

Inject a unified set of `GATEWAY_*` environment variables into all worker processes at startup, normalizing platform-specific fields (Slack `channel_id` / Feishu `chat_id`) into consistent names.

## Environment Variables

| Variable | Source (Slack) | Source (Feishu) | Example |
|----------|---------------|-----------------|---------|
| `GATEWAY_PLATFORM` | `"slack"` | `"feishu"` | `slack` |
| `GATEWAY_BOT_ID` | `botID` | `botOpenID` | `B12345` |
| `GATEWAY_USER_ID` | `userID` | `userID` | `U12345` |
| `GATEWAY_CHANNEL_ID` | `channel_id` | `chat_id` | `C12345` |
| `GATEWAY_THREAD_ID` | `thread_ts` | `message_id` | `1234.56` |
| `GATEWAY_TEAM_ID` | `teamID` | `""` (omitted) | `T12345` |
| `GATEWAY_SESSION_ID` | session ID | session ID | `uuid-v5` |
| `GATEWAY_WORK_DIR` | `workDir` | `workDir` | `/tmp/xxx` |

**Naming conventions**:
- `GATEWAY_*` prefix: not affected by `HOTPLEX_*` blocklist in `base.BuildEnv()`
- Empty values are omitted (not set to empty string)
- Platform-specific fields (e.g., `GATEWAY_TEAM_ID` for Slack only) are set only when non-empty

## Architecture

### New function: `injectGatewayContext`

Location: `internal/gateway/bridge.go`

```go
func injectGatewayContext(env map[string]string, platform, botID, userID string, platformKey map[string]string, sessionID, workDir string) {
    if env == nil {
        env = make(map[string]string)
    }
    env["GATEWAY_PLATFORM"] = platform
    env["GATEWAY_BOT_ID"] = botID
    env["GATEWAY_USER_ID"] = userID
    env["GATEWAY_SESSION_ID"] = sessionID
    if workDir != "" {
        env["GATEWAY_WORK_DIR"] = workDir
    }
    if chID := firstNonEmpty(platformKey["channel_id"], platformKey["chat_id"]); chID != "" {
        env["GATEWAY_CHANNEL_ID"] = chID
    }
    if threadID := firstNonEmpty(platformKey["thread_ts"], platformKey["message_id"]); threadID != "" {
        env["GATEWAY_THREAD_ID"] = threadID
    }
    if teamID := platformKey["team_id"]; teamID != "" {
        env["GATEWAY_TEAM_ID"] = teamID
    }
}
```

### Injection points (3 locations)

Called after the existing Slack-specific env injection in each path:

1. `bridge.go:StartSession` (~L130) — `platformKey` from argument
2. `bridge.go:ResumeSession` (~L215) — `platformKey` from `si.PlatformKey`
3. `bridge_worker.go:attemptResumeFallback` (~L162) — `platformKey` from `si.PlatformKey`

### Backward compatibility

Existing `HOTPLEX_SLACK_CHANNEL_ID` and `HOTPLEX_SLACK_THREAD_TS` are **preserved**. The only consumer is `internal/cli/slack/client.go` (`ResolveChannel` / `ResolveThreadTS`), which remains unchanged. `GATEWAY_*` variables are additive — no existing behavior is modified.

## Files Changed

| File | Change |
|------|--------|
| `internal/gateway/bridge.go` | Add `injectGatewayContext()`, call at 2 injection points |
| `internal/gateway/bridge_worker.go` | Call `injectGatewayContext()` at 1 injection point |
| `internal/gateway/bridge_test.go` | Add `TestInjectGatewayContext` (table-driven) |

**No changes to**: `cli/slack/client.go`, `worker/base/env.go`, blocklists, OCS worker, consumer tests.

## Testing

- **Unit test**: `TestInjectGatewayContext` — table-driven covering Slack full fields, Feishu full fields, empty field omission, nil env initialization
- **Existing tests**: `cli/slack/client_test.go` unchanged, all must pass
- **CI**: `make test` (with -race) + `make lint` must pass
- **Manual verification**: Start gateway, send Slack message triggering worker, inspect worker process env for `GATEWAY_*` variables
