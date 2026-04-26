# Switch Working Directory During Session

**Date:** 2026-04-25
**Status:** Draft
**Branch:** feat/28-premium-ux-sdk-integration

## Summary

Support switching the working directory during an active session. The feature creates a new session in the target directory and idles the old session, providing a seamless directory switch experience across all platforms (WebChat, Slack, Feishu, SDK).

## Requirements

- Switching workDir = create new session with new workDir
- All platforms supported: control command `/cd` + HTTP API
- Old session auto-idles (releases worker process, session record preserved)
- Agent config (SOUL/AGENTS/SKILLS) naturally inherited via global config directory
- No conversation context inheritance

## Design

### Control Command `/cd`

**Format:** `/cd <path>`

**Natural language triggers:** `$cd <path>`, `$切换目录 <path>`

**Registered in:** `internal/messaging/control_command.go`, new `cd` action (slash command + `$` prefix triggers)

**Behavior:**

1. Parse path argument, expand `~` → `$HOME`
2. Validate path (security checks via `security/path.go` + `security/ssrf.go`)
3. Idle current session
4. Create new session with new workDir
5. Return new session ID to caller

**Path validation rules:**

- Must be absolute path (or `~` prefixed)
- Must pass SSRF check (block `/proc`, `/sys`, `/dev`, `/etc`)
- Must normalize and reject path traversal (`..` escape)
- Must exist, be a directory, and be readable
- Reject if target == current session's workDir

**Edge cases:**

| Scenario | Behavior |
|----------|----------|
| `/cd` (no arg) | Return current workDir |
| `/cd ~/projects/foo` | Expand `~`, proceed |
| `/cd ../relative` | Reject, require absolute path |
| Same directory as current | Return `400` "already in directory" |

**Response messages:**

- Success: `已切换到 /new/path（新会话 #session-id）`
- Failure: `切换失败：路径不存在 / 权限不足 / ...`

### HTTP API

**Endpoint:** `POST /gateway/sessions/{id}/cd`

**Request body:**

```json
{
  "work_dir": "/path/to/new/dir"
}
```

**Success response:**

```json
{
  "old_session_id": "uuid-xxx",
  "new_session_id": "uuid-yyy",
  "work_dir": "/path/to/new/dir"
}
```

**Error responses:**

| Code | Condition |
|------|-----------|
| 400 | Path invalid / not found / not a directory |
| 404 | Session not found |
| 409 | Session in `terminated`/`deleted` state |
| 500 | Internal error (worker start failure) |

**Auth:** Reuse existing admin JWT middleware

**Registered in:** `internal/gateway/api.go`, new `SwitchWorkDir` handler

### WebChat

WebChat does NOT provide a dedicated directory switching UI. Users can simply create a new session with a different workDir via the existing NewSessionModal.

### Gateway Processing Flow

**Core function:** `bridge.SwitchWorkDir(ctx, sessionID, newWorkDir)`

```
SwitchWorkDir(ctx, sessionID, newWorkDir)
  ├── 1. sm.Get(sessionID) → get current session
  ├── 2. Validate session state ∈ {running, idle}
  ├── 3. Validate newWorkDir path security
  ├── 4. Extract current session metadata
  │     ├── userID, botID, workerType
  │     └── clientSessionID (from session store or reverse from session key)
  ├── 5. sm.Transition(oldSessionID → idle)
  ├── 6. DeriveSessionKey(userID, workerType, clientSessionID, newWorkDir)
  │     → new session ID (automatically different)
  ├── 7. bridge.StartSession(ctx, newID, userID, botID, workerType, nil, newWorkDir, platform, platformKey)
  │     → create new session + start worker
  └── 8. Return {oldSessionID, newSessionID, newWorkDir}
```

**Two call-in points, one implementation:**

- **Control command:** `handler.handleControl` detects `cd` action → calls `bridge.SwitchWorkDir` → returns text result via platform conn
- **HTTP API:** `api.SwitchWorkDir` handler → calls `bridge.SwitchWorkDir` → returns JSON

## Security

- Path validation reuses existing `security/path.go` and `security/ssrf.go`
- SSRF protection: block system paths (`/proc`, `/sys`, `/dev`, `/etc`)
- Path traversal: normalize then verify no `..` escape
- Directory must exist with read permission
- Concurrent `/cd` on same session: second request gets `409` (session already idled)

## Files to Modify

| File | Change |
|------|--------|
| `internal/gateway/bridge.go` | Add `SwitchWorkDir` method |
| `internal/gateway/api.go` | Add `SwitchWorkDir` HTTP handler |
| `internal/gateway/handler.go` | Route `cd` control action to bridge |
| `internal/messaging/control_command.go` | Register `cd` action |
| `cmd/hotplex/routes.go` | Register new API route |
| `pkg/events/events.go` | Add `CdData` struct for event payload |
