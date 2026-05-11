# Cron Fast Path — In-Session Callback Specification

**Version**: 2.1
**Status**: Ready for Implementation
**Supersedes**: v1.1 Draft, v2.0 Draft
**Related**: [AI-Native-Cronjob-Spec.md](AI-Native-Cronjob-Spec.md)

---

## 1. Problem Statement

Current Cron (`agent_turn`) always creates an **isolated, stateless session** — ideal for standalone tasks but fundamentally unable to:

- Continue an ongoing conversation with full context
- Follow up on an in-progress task (e.g., "check the build result in 5 minutes")
- Inject a reminder into an active agent session

This spec adds **`session_callback`**: a new payload kind that injects a scheduled input into an **existing session**, preserving all conversation context.

---

## 2. Design Decisions

### D1: Additive Constant — No Rename

**Decision**: Add `PayloadSessionCallback` as a new constant. Do NOT rename `PayloadAgentTurn` or `PayloadSystemEvent`.

**Rationale**: Existing SQLite records use `"agent_turn"`. Renaming requires data migration and risks breaking running jobs. Additive is zero-risk.

### D2: New `SessionCallbackHandler` — Not Executor Extension

**Decision**: Create a dedicated `SessionCallbackHandler` struct in `internal/cron/callback.go`. The existing `Executor` handles only `agent_turn`.

**Rationale**: The two paths have fundamentally different lifecycles:

| Aspect | `agent_turn` (Executor) | `session_callback` (CallbackHandler) |
|--------|------------------------|--------------------------------------|
| Session | Create new | Find existing |
| Worker | Start fresh | Resume or reuse |
| Completion | Wait → terminate → deliver | Fire-and-forget (result stays in session) |
| Timeout | Job-level (`TimeoutSec`) | Session-level (inherited from original session) |
| Delivery | Extract response → platform route | None (agent responds in-session) |
| Concurrency | Own slot via `tryAcquireSlot` | Own slot via `tryAcquireSlot` (same cap) |

### D3: Fire-and-Forget Delivery Model

**Decision**: `session_callback` does NOT use the Delivery pipeline. The callback result becomes the agent's next turn in the existing session — visible to the user through normal platform channels.

**Consequence**: The delivery path (`delivery.go`) is never called for `session_callback`. The `Silent` field is irrelevant for this payload kind.

### D4: Session Reference via `TargetSessionID`

**Decision**: Add `TargetSessionID string` to `CronPayload`. Populated from `$GATEWAY_SESSION_ID` when the agent creates a callback job.

**Constraint**: Only `at:` and `every:` schedules are valid for callbacks (see [§6.2](#62-schedule-restrictions)).

### D5: Reuse Existing `OnTerminate` Hook for Cascade Cleanup

**Decision**: Wire cascade cleanup through `session.Manager.OnTerminate` callback. Do NOT create a new interface in the `cron` package.

**Rationale**: The session manager already exposes `OnTerminate func(sessionID string)` which fires for both `TERMINATED` and `DELETED` transitions. Defining `SessionDeletionListener` in the `cron` package would create a circular dependency (`session` → `cron.SessionDeletionListener` vs `cron` → `session.SessionStateChecker`). Instead, the wiring happens in `cmd/hotplex/` at the application layer:

```go
// cmd/hotplex/gateway_run.go (during startup)
sessionMgr.OnTerminate = func(sessionID string) {
    if cronScheduler != nil {
        cronScheduler.CleanupForSession(sessionID)
    }
}
```

---

## 3. Data Model Changes

### 3.1 `internal/cron/types.go`

```go
const (
	PayloadAgentTurn       PayloadKind = "agent_turn"          // existing, unchanged
	PayloadSystemEvent     PayloadKind = "system_event"        // existing, reserved
	PayloadSessionCallback PayloadKind = "session_callback"    // NEW
)

type CronPayload struct {
	Kind            PayloadKind `json:"kind"`
	Message         string      `json:"message"`
	TargetSessionID string      `json:"target_session_id,omitempty"` // NEW: only for session_callback
	AllowedTools    []string    `json:"allowed_tools,omitempty"`
	WorkerType      string      `json:"worker_type,omitempty"`
}
```

No change to `Clone()` needed — `TargetSessionID` is a `string` (value type, not reference).

### 3.2 SQLite Migration Required

The `payload_kind` column has a CHECK constraint that must be updated:

```sql
-- Current (005_cron_jobs_table.sql line 10):
payload_kind TEXT NOT NULL DEFAULT 'agent_turn' CHECK(payload_kind IN ('agent_turn', 'system_event')),

-- New migration: 008_cron_session_callback.sql
-- +goose Up
ALTER TABLE cron_jobs DROP CONSTRAINT payload_kind;  -- SQLite doesn't support DROP CONSTRAINT
-- → Use replacement table pattern for SQLite:

-- Step 1: Create new table with expanded CHECK
CREATE TABLE cron_jobs_new (
    -- ... identical columns ...
    payload_kind TEXT NOT NULL DEFAULT 'agent_turn'
        CHECK(payload_kind IN ('agent_turn', 'system_event', 'session_callback')),
    -- ... rest identical ...
);

-- Step 2: Copy data
INSERT INTO cron_jobs_new SELECT * FROM cron_jobs;

-- Step 3: Swap
DROP TABLE cron_jobs;
ALTER TABLE cron_jobs_new RENAME TO cron_jobs;

-- Step 4: Recreate indexes
CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));

-- +goose Down
-- (reverse: rebuild without 'session_callback' in CHECK)
```

> **Note**: `payload_data` is a JSON text column. The new `TargetSessionID` field is automatically serialized/deserialized by `json.Marshal`/`json.Unmarshal` in `store.go` — no column change needed for this.

### 3.3 `internal/cron/normalize.go` — Validation Extensions

Add to `ValidateJob` before the recurring job lifecycle check:

```go
// Session callback validation.
if job.Payload.Kind == PayloadSessionCallback {
    if job.Payload.TargetSessionID == "" {
        return errors.New("cron: target_session_id is required for session_callback")
    }
    if job.Schedule.Kind == ScheduleCron {
        return errors.New("cron: session_callback does not support cron expression schedules")
    }
}
```

### 3.4 Store Compatibility

`store.go` uses `json.Unmarshal` for `payload_data` → `CronPayload`. Adding `TargetSessionID` is backward-compatible:
- Old records without `target_session_id` → field remains `""` (zero value)
- New records with `target_session_id` → field populated normally

---

## 4. Execution Flow

### 4.1 New Interface: `SessionCallbackRouter`

Defined in `internal/cron/callback.go`. Uses types from packages `cron` already depends on (`session`, `events`).

```go
// SessionCallbackRouter is the narrow interface for session callback execution.
// Implemented by an adapter in cmd/hotplex/ that bridges Bridge + SessionManager.
type SessionCallbackRouter interface {
	// GetSessionInfo returns session metadata for callback dispatch.
	GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error)

	// ResumeAndInput resumes a dormant session and injects the callback prompt.
	ResumeAndInput(ctx context.Context, sessionID string, workDir string, prompt string, metadata map[string]any) error

	// InjectInput sends a prompt to an already-running session's worker.
	InjectInput(ctx context.Context, sessionID string, prompt string, metadata map[string]any) error
}
```

> Uses `*session.SessionInfo` directly (not a custom DTO) since `cron` already imports `session` via `SessionStateChecker`. This avoids type duplication.

### 4.2 `SessionCallbackHandler`

```go
// internal/cron/callback.go

type SessionCallbackHandler struct {
	log    *slog.Logger
	router SessionCallbackRouter
}

func NewSessionCallbackHandler(log *slog.Logger, router SessionCallbackRouter) *SessionCallbackHandler {
	return &SessionCallbackHandler{
		log:    log.With("component", "cron_callback"),
		router: router,
	}
}

// Execute dispatches a callback into the target session.
// Returns nil on successful injection (fire-and-forget), or an error if dispatch fails.
func (h *SessionCallbackHandler) Execute(ctx context.Context, job *CronJob) error {
	sid := job.Payload.TargetSessionID

	// Step 1: Look up session
	info, err := h.router.GetSessionInfo(ctx, sid)
	if err != nil {
		return fmt.Errorf("callback: session %s not found: %w", sid, err)
	}

	// Step 2: Build prompt with metadata
	prompt := fmt.Sprintf("[cron:%s %s] %s\n%s",
		job.ID, job.Name, job.Payload.Message, time.Now().Format(time.RFC3339))
	metadata := map[string]any{
		"source":   "cron_callback",
		"cron_job": job.ID,
	}

	// Step 3: Dispatch based on session state
	switch info.State {
	case events.StateRunning:
		// Worker is live — inject directly
		if err := h.router.InjectInput(ctx, sid, prompt, metadata); err != nil {
			return fmt.Errorf("callback: inject into running session: %w", err)
		}
		h.log.Info("callback: injected into running session",
			"session_id", sid, "job_id", job.ID)

	case events.StateIdle, events.StateTerminated:
		// Worker dormant — resume + inject
		if err := h.router.ResumeAndInput(ctx, sid, info.WorkDir, prompt, metadata); err != nil {
			return fmt.Errorf("callback: resume session %s: %w", sid, err)
		}
		h.log.Info("callback: resumed and injected",
			"session_id", sid, "job_id", job.ID, "from_state", info.State)

	case events.StateDeleted:
		return fmt.Errorf("callback: session %s is deleted, aborting", sid)

	case events.StateCreated:
		return fmt.Errorf("callback: session %s is in CREATED state (never started), aborting", sid)

	default:
		return fmt.Errorf("callback: session %s in unexpected state %s", sid, info.State)
	}

	return nil
}
```

### 4.3 Bridge Adapter Implementation

In `cmd/hotplex/`, implement `SessionCallbackRouter` using existing `Bridge` + `SessionManager`:

```go
// cmd/hotplex/cron_admin_adapter.go (or a new file)

type cronCallbackRouter struct {
	bridge *gateway.Bridge
	sm     *session.Manager
}

func (r *cronCallbackRouter) GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error) {
	return r.sm.Get(ctx, id)
}

func (r *cronCallbackRouter) InjectInput(ctx context.Context, sessionID string, prompt string, metadata map[string]any) error {
	w := r.sm.GetWorker(sessionID)
	if w == nil {
		return fmt.Errorf("no worker for session %s", sessionID)
	}
	return w.Input(ctx, prompt, metadata)
}

func (r *cronCallbackRouter) ResumeAndInput(ctx context.Context, sessionID string, workDir string, prompt string, metadata map[string]any) error {
	if err := r.bridge.ResumeSession(ctx, sessionID, workDir); err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	w := r.sm.GetWorker(sessionID)
	if w == nil {
		return fmt.Errorf("no worker after resume for session %s", sessionID)
	}
	return w.Input(ctx, prompt, metadata)
}
```

### 4.4 Integration Point: `timer.go` → `executeJob`

In `Scheduler.executeJob`, add dispatch before the existing executor call:

```go
func (s *Scheduler) executeJob(job *CronJob) {
	// Resolve workdir with system fallback.
	if job.WorkDir == "" && s.resolveWorkDir != nil {
		if resolved := s.resolveWorkDir(job); resolved != "" {
			job.WorkDir = resolved
		}
	}

	// *** NEW: Dispatch session_callback to dedicated handler ***
	if job.Payload.Kind == PayloadSessionCallback {
		s.executeCallback(job)
		return
	}

	// Existing agent_turn execution path (unchanged)
	// ...
}
```

Full `executeCallback` method:

```go
func (s *Scheduler) executeCallback(job *CronJob) {
	now := time.Now().UnixMilli()
	job.State.RunningAtMs = now
	s.persistState(job.ID, job.State)
	s.mergeJobState(job.ID, job.State, false)

	err := s.callbackHandler.Execute(s.ctx, job)

	job.State.RunningAtMs = 0
	job.State.LastRunAtMs = now

	if err != nil {
		s.log.Error("cron: callback failed",
			"job_id", job.ID, "name", job.Name, "err", err)
		job.State.LastStatus = StatusFailed
		job.State.ConsecutiveErrs++
		metrics.CronErrorsTotal.WithLabelValues(job.Name, "callback").Inc()

		// One-shot retry logic (reuse existing at-retry path).
		if job.Schedule.Kind == ScheduleAt && isTemporaryError(err) && job.State.RetryCount < maxRetries(job) {
			s.scheduleRetry(s.ctx, job)
			return
		}
	} else {
		job.State.LastStatus = StatusSuccess
		job.State.ConsecutiveErrs = 0
		job.State.RunCount++
		resetRetry(job)
		metrics.CronFiresTotal.WithLabelValues(job.Name).Inc()
	}

	// One-shot lifecycle: delete or disable after execution.
	if job.Schedule.Kind == ScheduleAt {
		if job.DeleteAfterRun {
			if err := s.store.Delete(s.persistCtx(), job.ID); err != nil {
				s.log.Error("cron: delete one-shot callback", "job_id", job.ID, "err", err)
			}
			s.mu.Lock()
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return
		}
		// Disable one-shot after first run.
		shouldDisable := true
		s.persistState(job.ID, job.State)
		if shouldDisable {
			if err := s.store.SetEnabled(s.persistCtx(), job.ID, false); err != nil {
				s.log.Error("cron: persist disable callback", "job_id", job.ID, "err", err)
			}
		}
		s.mergeJobState(job.ID, job.State, shouldDisable)
		return
	}

	// Recurring (every:) lifecycle: max_runs / expires_at.
	s.persistState(job.ID, job.State)
	shouldDisable := false
	if job.MaxRuns > 0 && job.State.RunCount >= job.MaxRuns {
		shouldDisable = true
	} else if job.ExpiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, job.ExpiresAt); perr == nil && time.Now().After(t) {
			shouldDisable = true
		}
	}
	if shouldDisable {
		if err := s.store.SetEnabled(s.persistCtx(), job.ID, false); err != nil {
			s.log.Error("cron: persist disable callback", "job_id", job.ID, "err", err)
		}
	}
	s.mergeJobState(job.ID, job.State, shouldDisable)
}
```

### 4.5 `Scheduler` Struct Changes

```go
type Scheduler struct {
	// ... existing fields ...
	callbackHandler *SessionCallbackHandler  // NEW: nil when SessionCallbackRouter is nil
}

type Deps struct {
	// ... existing fields ...
	CallbackRouter SessionCallbackRouter  // NEW: nil → callback jobs fail at execution
}
```

In `New(deps)`:

```go
if deps.CallbackRouter != nil {
	s.callbackHandler = NewSessionCallbackHandler(deps.Log, deps.CallbackRouter)
}
```

When `callbackHandler` is nil (CLI-only mode without gateway), `session_callback` jobs are created successfully but fail at execution with a logged warning.

---

## 5. CLI Changes

### 5.1 `--callback` Flag

Add to `cmd/hotplex/cron_create.go`:

```
--callback    Create a session_callback job (requires $GATEWAY_SESSION_ID)
```

When `--callback` is set:

1. `Payload.Kind` = `"session_callback"`
2. `Payload.TargetSessionID` = `$GATEWAY_SESSION_ID` (fail if unset)
3. `BotID` and `OwnerID` are auto-filled from `$GATEWAY_BOT_ID` / `$GATEWAY_USER_ID` — these remain structurally required by `ValidateJob` but the CLI fills them automatically
4. `Platform` and `PlatformKey` are set to `"callback"` (no delivery routing needed)
5. `DeleteAfterRun` defaults to `true` for `at:` schedule (single follow-up, auto-cleanup)
6. Schedule defaults to `at:+10m` if `--schedule` is omitted

### 5.2 Relative Time Offset: `at:+N`

Extend `ParseSchedule` in `internal/cli/cron/client.go` to support relative offsets.

**Syntax**: `at:+5m` | `at:+2h` | `at:+30s` | `at:+1h30m`

**Parsing rules**:
- `+` prefix triggers relative mode
- Duration format: Go-style (`5m`, `2h30m`, `90s`), parsed with `time.ParseDuration`
- Resolve to absolute RFC3339 at parse time: `time.Now().Add(d).Format(time.RFC3339)`
- Validation: resolved time must be in the future; maximum offset 72 hours; minimum offset 1 minute

```go
// In ParseSchedule (internal/cli/cron/client.go)
case cron.ScheduleAt:
    if strings.HasPrefix(value, "+") {
        d, err := time.ParseDuration(value[1:])
        if err != nil {
            return cron.CronSchedule{}, fmt.Errorf("invalid relative duration in at schedule: %w", err)
        }
        if d < time.Minute {
            return cron.CronSchedule{}, fmt.Errorf("relative duration must be at least 1 minute")
        }
        if d > 72*time.Hour {
            return cron.CronSchedule{}, fmt.Errorf("relative duration must not exceed 72 hours")
        }
        abs := time.Now().Add(d).Format(time.RFC3339)
        return cron.CronSchedule{Kind: kind, At: abs}, nil
    }
    // existing absolute parsing...
```

This works for ALL `at:` jobs, not just `--callback` — any one-shot job benefits from relative time.

### 5.3 `--callback` Shorthand for `--schedule`

When `--callback` is set and `--schedule` is omitted:

```
--callback              →  at:+10m (default 10-minute follow-up)
--callback -m "..."     →  at:+10m
--callback --schedule "at:+1h" -m "..."  →  explicit override
--callback --schedule "every:5m" -m "..."  →  periodic check-in
```

---

## 6. Lifecycle & Edge Cases

### 6.1 Session Deletion Cascade

When a session transitions to `TERMINATED` or `DELETED`, associated callback jobs must be cleaned.

**Mechanism**: Reuse existing `session.Manager.OnTerminate` callback. Wire in `cmd/hotplex/gateway_run.go`:

```go
// During startup, after session manager and cron scheduler are created:
prevOnTerminate := sessionMgr.OnTerminate
sessionMgr.OnTerminate = func(sessionID string) {
    if prevOnTerminate != nil {
        prevOnTerminate(sessionID)
    }
    if cronScheduler != nil {
        cronScheduler.CleanupForSession(sessionID)
    }
}
```

`CleanupForSession` implementation:

```go
func (s *Scheduler) CleanupForSession(sessionID string) {
	s.mu.Lock()
	var toDelete []string
	for id, job := range s.jobs {
		if job.Payload.Kind == PayloadSessionCallback &&
			job.Payload.TargetSessionID == sessionID {
			toDelete = append(toDelete, id)
		}
	}
	s.mu.Unlock()

	for _, id := range toDelete {
		if err := s.DeleteJob(context.Background(), id); err != nil {
			s.log.Warn("callback cascade: failed to delete job",
				"job_id", id, "session_id", sessionID, "err", err)
		}
	}
	if len(toDelete) > 0 {
		s.log.Info("callback cascade: cleaned up jobs for session",
			"session_id", sessionID, "jobs_removed", len(toDelete))
	}
}
```

> **Note**: GC transitions sessions to `TERMINATED` (not `DELETED`). This means `OnTerminate` fires on GC-evicted sessions, which is the desired behavior — callback jobs targeting GC'd sessions should be cleaned up since those sessions can no longer be meaningfully resumed.

### 6.2 Schedule Restrictions

`session_callback` jobs support:

| Schedule Kind | Allowed | Rationale |
|--------------|---------|-----------|
| `at` (one-shot) | Yes | Primary use case — "follow up in 5 minutes" |
| `every` (interval) | Yes | Periodic check-ins within a long-running session |
| `cron` (expression) | No | Sessions are transient; cron expressions imply long-lived targets that may outlive the session |

Validation enforces this in `ValidateJob` (see [§3.3](#33-internalcronnormalizego--validation-extensions)).

### 6.3 Gateway Restart

On restart, `loadFromDB` loads `session_callback` jobs normally. When they fire:

- If target session is `RUNNING` or `IDLE` → normal execution (inject or resume)
- If target session is `TERMINATED` → attempt resume (valid transition: `TERMINATED → RUNNING`)
- If target session was physically deleted → job fails, marked as failed
- Failed one-shot (`at:`) callbacks with remaining retries are retried with exponential backoff (reuses existing `scheduleRetry` logic)

### 6.4 Concurrency

- Callback execution occupies a concurrency slot (same `tryAcquireSlot` / `releaseSlot` as `agent_turn`)
- Callback injection is serialized per-session by the session manager's per-session mutex (same as user input via `TransitionWithInput`)
- If the worker is currently streaming a response, `Input` is buffered by the worker adapter (existing behavior for Claude Code `--print` mode stdin)
- No additional queuing is needed — the existing backpressure mechanism is sufficient

### 6.5 State After Callback

After a successful callback injection:

| Field | Update |
|-------|--------|
| `State.RunningAtMs` | `0` (callback is fire-and-forget) |
| `State.LastRunAtMs` | Execution timestamp |
| `State.LastStatus` | `"success"` |
| `State.RunCount` | Incremented |
| `State.ConsecutiveErrs` | Reset to `0` |
| `State.RetryCount` | Reset to `0` |

For `at:` callbacks: `DeleteAfterRun` defaults to `true` (single follow-up, auto-cleanup).
For `every:` callbacks: Normal `max_runs` / `expires_at` lifecycle applies.

### 6.6 CLI-Only Mode

When `SessionCallbackRouter` is nil (running `hotplex cron` commands outside gateway):

- `session_callback` job **creation** succeeds normally (no gateway needed for CRUD)
- `session_callback` job **execution** logs a warning and marks the job as failed:

```go
if s.callbackHandler == nil {
    s.log.Warn("cron: session_callback execution skipped, no callback router",
        "job_id", job.ID, "name", job.Name)
    job.State.LastStatus = StatusFailed
    // ... persist state, no retry ...
    return
}
```

---

## 7. Skill Manual Updates

Rewrite `internal/cron/cron-skill-manual.md` to integrate callback mode into the execution flow. The changes follow the agent's decision chain: **角色定义 → 意图识别 → 策略选择 → 策略分叉**.

### 7.1 `<role>` — Expand Role Definition

**Before**:
```markdown
你当前需要为用户创建定时任务。从用户的自然语言中识别调度意图，组装自包含的 Prompt，调用 `hotplex cron` CLI 完成创建。后续调度与执行由系统自动完成，无需你参与。
```

**After**:
```markdown
你当前需要为用户创建定时任务。从用户的自然语言中识别调度意图，选择执行策略（独立任务 or 会话回调），组装对应格式的 Prompt，调用 `hotplex cron` CLI 完成创建。后续调度与执行由系统自动完成，无需你参与。
```

### 7.2 `<critical_rules>` — Fork Rule 1 by Strategy

**Before** (Rule 1):
```markdown
1. **Prompt 自包含**：`-m` 参数将由未来的全新 Worker 实例执行，无当前对话上下文。提供绝对路径、确切 URL、具体操作指令和输出格式。
```

**After** (split into two sub-rules):
```markdown
1. **Prompt 按策略组装**：
   - **标准模式**（无 `--callback`）：`-m` 由全新 Worker 执行，无当前对话上下文。提供绝对路径、确切 URL、具体操作指令和输出格式。
   - **回调模式**（`--callback`）：`-m` 注入当前会话，有完整对话上下文。Prompt 可以简短（如"继续上一步""检查结果"）。
```

### 7.3 `<intent_recognition>` — Add Callback Intent Row

**Before**:
```markdown
| X 分钟后/过一会儿/稍后  | at            | `at:ISO timestamp` |
```

**After** (split into two rows):
```markdown
| X 分钟后/过一会儿/稍后 + 上下文相关 | at + callback | `--callback` (默认 at:+10m)  |
| X 分钟后/过一会儿/稍后 + 独立任务    | at            | `at:ISO timestamp`           |
```

### 7.4 `<strategy_selection>` — NEW Section

Insert **after** `</intent_recognition>` and **before** `<prompt_assembly>`:

```markdown
<strategy_selection>
识别到调度意图后，根据以下决策树选择执行策略：

```
用户意图含定时/调度
    │
    ├─ 任务需要当前对话上下文？
    │   │
    │   ├─ 是：用户说"跟进/继续/刚才那个/检查结果" → ──→ callback（会话回调）
    │   │      特征：依赖当前文件、之前的操作结果、对话中的上下文
    │   │      CLI: 加 `--callback`
    │   │      Prompt: 可以简短，有上下文
    │   │
    │   └─ 否：独立任务，无上下文依赖 → ──→ standard（标准模式）
    │          特征：自包含操作、巡检、提醒、定时报告
    │          CLI: 不加 `--callback`
    │          Prompt: 必须自包含
    │
    └─ 不确定 → 默认 standard（更安全，不依赖会话存活）
```

**callback 适用场景**：
- "5分钟后检查刚才的构建结果"
- "过一会儿提醒我继续这个任务"
- "10分钟后跟进那个 PR 的 review 状态"

**standard 适用场景**：
- "每天9点做健康巡检"
- "30分钟后提醒我开会"（纯提醒，无上下文）
- "每小时检查一次 API 可用性"

**callback 前提**：`$GATEWAY_SESSION_ID` 环境变量存在（仅在会话内可用）。
</strategy_selection>
```

### 7.5 `<prompt_assembly>` — Add Fork Guidance

Replace the entire section:

```markdown
<prompt_assembly>
根据 `<strategy_selection>` 选定的策略组装 `-m` 参数：

**标准模式**（无 `--callback`）：
- 由全新 Worker 实例执行，**无当前对话上下文**
- 自包含校验：
  - 用绝对路径、确切 URL、具体文件名替代代词
  - 明确操作步骤、输出格式，必要时指定工具
  - 换位思考：把这段话单独发给一个刚唤醒的 AI，它能正确完成吗？
- 对比示例：
  - 不充分：`"检查一下刚才那个文件是否更新了"`
  - 充分：`"检查 /Users/xxx/project/main.go 文件，对比最新 commit 的修改内容，生成 markdown 报告"`

**回调模式**（`--callback`）：
- 注入当前会话，**有完整对话上下文**
- 可以使用代词和简短指令（"继续""检查结果""那个文件"等）
- 无需重复路径、URL 或背景信息
- 示例：
  - `"刚才那个测试跑完了吗？结果如何？"`
  - `"继续检查部署状态"`
  - `"那个文件的修改提交了吗？"`
</prompt_assembly>
```

### 7.6 `<cli_quick_ref>` — Add Callback Variant

After the existing "创建" block, add a second variant:

```markdown
创建（回调模式 — 在当前会话中跟进）：
```bash
hotplex cron create --callback \
  --name <名称> \
  [-m <Prompt>] \
  [--schedule <调度>]       # 省略则默认 at:+10m
  [--max-runs <次数>]       # every: 时需要
```

省略 `--schedule` 时默认 `at:+10m`（10分钟后回调）。`--bot-id` / `--owner-id` 自动填充。
```

Also add `at:+N` to the Schedule 格式 table:

```markdown
| `at:+N`       | 相对偏移 | 最低1分钟，最高72小时 | `at:+5m`、`at:+2h`、`at:+30m` |
```

### 7.7 `<examples>` — Add Callback Examples

Append after existing examples:

```markdown
# ===== 回调模式（--callback）=====

# 快速跟进（默认10分钟后，自动清理）
hotplex cron create --callback \
  --name "follow-up-test" \
  -m "之前的测试跑完了吗？结果如何？"

# 指定时间的回调
hotplex cron create --callback \
  --name "deploy-check" \
  --schedule "at:+30m" \
  -m "检查刚才部署的服务状态"

# 周期性会话跟进
hotplex cron create --callback \
  --name "build-monitor" \
  --schedule "every:5m" \
  -m "继续检查构建进度" \
  --max-runs 10
```

### 7.8 `<field_reference>` — Add `--callback` Row

```markdown
| `--callback`          | 否       | 会话回调模式，需要 `$GATEWAY_SESSION_ID`。省略 `--schedule` 时默认 `at:+10m` |
```

### 7.9 Summary of All Changes

| Section | Action | What Changes |
|---------|--------|-------------|
| `<role>` | **Edit** | 加入"选择执行策略" |
| `<critical_rules>` | **Edit** | Rule 1 分叉为 standard/callback 两条 |
| `<intent_recognition>` | **Edit** | 拆分"X分钟后"行为 standard/callback 两行 |
| `<strategy_selection>` | **NEW** | 决策树 + 适用场景 + 前提条件 |
| `<prompt_assembly>` | **Edit** | 按 standard/callback 分叉说明 |
| `<cli_quick_ref>` | **Edit** | 新增 callback 创建模板 + `at:+N` 格式 |
| `<examples>` | **Edit** | 追加 3 个 callback 示例 |
| `<field_reference>` | **Edit** | 新增 `--callback` 行 |

---

## 8. Metrics

Extend the existing `CronFiresTotal` and `CronErrorsTotal` counters with a `payload_kind` label. This is consistent with the existing metric naming and avoids creating a separate metric family.

**Add label to existing counters** (breaking change: label dimension addition):

```go
// Before:
CronFiresTotal.WithLabelValues(job.Name).Inc()

// After:
CronFiresTotal.WithLabelValues(job.Name, string(job.Payload.Kind)).Inc()
```

If changing existing label dimensions is undesirable (breaks existing dashboards), add a dedicated counter instead:

```go
var CronCallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "hotplex_cron_callback_total",
    Help: "Total session callback attempts by result",
}, []string{"result"}) // result: success, session_not_found, resume_failed, inject_failed, no_router
```

Record in `executeCallback`:

```go
metrics.CronCallbackTotal.WithLabelValues("success").Inc()
metrics.CronCallbackTotal.WithLabelValues("session_not_found").Inc()
// etc.
```

---

## 9. Testing Strategy

### Unit Tests

| Test | File | Description |
|------|------|-------------|
| `TestValidateJob_Callback` | `normalize_test.go` | target_session_id required; `cron:` schedule rejected; valid callback passes |
| `TestValidateJob_CallbackAt` | `normalize_test.go` | `at:` schedule is valid for callback |
| `TestValidateJob_CallbackEvery` | `normalize_test.go` | `every:` schedule is valid for callback |
| `TestCallbackHandler_Running` | `callback_test.go` | Mock router, verify `InjectInput` called, no `ResumeAndInput` |
| `TestCallbackHandler_Idle` | `callback_test.go` | Mock router, verify `ResumeAndInput` called with correct workDir |
| `TestCallbackHandler_Terminated` | `callback_test.go` | Mock router, verify `ResumeAndInput` called (TERMINATED → RUNNING valid) |
| `TestCallbackHandler_Deleted` | `callback_test.go` | Verify specific error returned for deleted session |
| `TestCallbackHandler_Created` | `callback_test.go` | Verify error for session in CREATED state |
| `TestCallbackHandler_NotFound` | `callback_test.go` | Verify error when session doesn't exist |
| `TestCleanupForSession` | `callback_test.go` | Create N callback + M agent_turn jobs, delete session, verify only callbacks removed |
| `TestCleanupForSession_NoCallbackJobs` | `callback_test.go` | Delete session with no callbacks → no-op, no errors |
| `TestParseSchedule_Relative` | `client_test.go` | `at:+5m`, `at:+2h30m`, `at:+90s` resolve correctly |
| `TestParseSchedule_RelativeInvalid` | `client_test.go` | `at:+30s` (too short), `at:+100h` (too long), `at:+abc` (invalid) |
| `TestCallback_NoRouter` | `cron_test.go` | Verify graceful failure when `callbackHandler` is nil |

### Integration Tests

| Test | Description |
|------|-------------|
| `TestCallback_RoundTrip` | Create callback job with `at:+1s`, wait for fire, verify input appears in target session |
| `TestCallback_CascadeOnDelete` | Create callback targeting session, delete session, verify job auto-removed |

---

## 10. Implementation Checklist

Ordered by dependency chain:

- [ ] **P1: SQLite migration** — `008_cron_session_callback.sql` (expand CHECK constraint)
- [ ] **P1: Data model** — Add `PayloadSessionCallback` constant, `TargetSessionID` field to `CronPayload`
- [ ] **P1: Validation** — Add callback-specific rules to `ValidateJob`
- [ ] **P2: `SessionCallbackRouter` interface** — Define in `internal/cron/callback.go`
- [ ] **P2: `SessionCallbackHandler`** — Implement `Execute` with state-based dispatch
- [ ] **P2: `CleanupForSession`** — Implement cascade deletion in `Scheduler`
- [ ] **P3: Scheduler integration** — Wire `callbackHandler` into `Scheduler`, add `executeCallback`
- [ ] **P3: Bridge adapter** — Implement `cronCallbackRouter` in `cmd/hotplex/`
- [ ] **P3: Session hook** — Wire `OnTerminate` callback in `gateway_run.go` startup
- [ ] **P4: CLI `--callback` flag** — Add flag to `cron_create.go`, auto-fill `TargetSessionID`, default schedule `at:+10m`
- [ ] **P4: Relative time parsing** — Extend `ParseSchedule` for `at:+N` syntax
- [ ] **P5: Skill manual** — Add `<callback_mode>` section, update intent recognition table
- [ ] **P5: Metrics** — Add `hotplex_cron_callback_total` counter (or extend existing labels)
- [ ] **P6: Unit tests** — All tests from §9
- [ ] **P6: Integration tests** — `TestCallback_RoundTrip`, `TestCallback_CascadeOnDelete`

---

## Appendix A: Updates to AI-Native-Cronjob-Spec.md

The following sections of [AI-Native-Cronjob-Spec.md](AI-Native-Cronjob-Spec.md) require updates:

### A.1 §1 Overview — Non-Goals

**Remove** `system_event payload（注入已有 session）` from the Non-Goals list. This is now implemented as `session_callback`.

### A.2 §2.2 CronPayload — Add `session_callback`

```go
const (
    PayloadAgentTurn       Payload = "agent_turn"        // isolated session
    PayloadSystemEvent     Payload = "system_event"      // reserved
    PayloadSessionCallback Payload = "session_callback"  // inject existing session — NEW
)

type CronPayload struct {
    Kind            Payload  `json:"kind"`
    Message         string   `json:"message"`
    TargetSessionID string   `json:"target_session_id,omitempty"` // NEW: session_callback only
    AllowedTools    []string `json:"allowed_tools,omitempty"`
}
```

### A.3 §3 SQLite Schema — Expand CHECK Constraint

```sql
-- Change line 10 from:
payload_kind TEXT NOT NULL DEFAULT 'agent_turn' CHECK(payload_kind IN ('agent_turn', 'system_event')),
-- To:
payload_kind TEXT NOT NULL DEFAULT 'agent_turn' CHECK(payload_kind IN ('agent_turn', 'system_event', 'session_callback')),
```

### A.4 §4.1 Directory Layout — Add `callback.go`

```
internal/cron/
├── callback.go    # NEW: SessionCallbackRouter + SessionCallbackHandler
├── cron.go
├── store.go
├── timer.go
├── executor.go
├── schedule.go
├── loader.go
├── delivery.go
├── skill.go
├── cron-skill-manual.md
├── types.go
├── normalize.go
└── cron_test.go
```

### A.5 §4.2 Scheduler — Add `callbackHandler`

```go
type Scheduler struct {
    // ... existing fields ...
    callbackHandler *SessionCallbackHandler  // NEW
}
```

### A.6 §5 Error Handling — Add Callback-Specific Behavior

Add after §5.3:

```
### 5.4 Session Callback Errors

| Error | Behavior |
|-------|----------|
| Target session not found | Mark failed, no retry (permanent) |
| Target session deleted | Mark failed, no retry (permanent) |
| Target session CREATED | Mark failed, no retry (session never started) |
| Resume failed | Mark failed, retry if one-shot with remaining retries |
| Inject failed (worker gone) | Retry: resume session then inject |
```

### A.7 §11 Implementation Phases — Add Phase 4

```
### Phase 4 — Session Callback (Fast Path)

- SQLite migration: expand `payload_kind` CHECK constraint
- `internal/cron/callback.go`: `SessionCallbackRouter` interface + `SessionCallbackHandler`
- `internal/cron/types.go`: `PayloadSessionCallback` + `TargetSessionID`
- `internal/cron/normalize.go`: callback validation rules
- `internal/cron/timer.go`: `executeCallback` dispatch in `executeJob`
- Bridge adapter in `cmd/hotplex/`: `cronCallbackRouter`
- Session GC hook: `OnTerminate` → `CleanupForSession`
- CLI: `--callback` flag + `at:+N` relative time
- Skill manual: `<callback_mode>` section
- Metrics: `hotplex_cron_callback_total`
- Tests: unit + integration
```

---

## Appendix B: Updates to docs/specs/README.md

Add both cron spec documents to the index under a new category:

```markdown
### 定时任务

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [AI-Native-Cronjob-Spec.md](./AI-Native-Cronjob-Spec.md) | AI 原生定时任务 — Worker 自管理调度系统 | implemented | 2026-05-09 | 100% |
| [Cron-Fast-Path-Spec.md](./Cron-Fast-Path-Spec.md) | Cron Fast Path — 会话内回调机制 | draft | 2026-05-11 | 0% |
```

Also update the "按领域分类" section — add:

```markdown
- **定时任务**: 2 个
```
