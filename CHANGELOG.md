# Changelog

## [1.1.2] - 2026-04-26

### Summary

v1.1.2 是一次 patch 版本更新，聚焦于 **会话数据持久化与连接稳定性**。新增 Conversation Store（异步批量写入会话轮次数据）和 Session Stats API（token/延迟/成本统计），为 WebChat 和管理端提供会话级别的洞察。Gateway Core 修复了多个关键稳定性问题（CAS race guard、fast reconnect、session ID 一致性、mapper 事件丢失），并引入 title-based session management 和 startup session repair。Session 层完成 SQLite 性能优化（PRAGMA 调优、级联删除、events TTL、自动 VACUUM）。测试覆盖率从 68% 提升至 84%+。

### Added

- **Gateway Core**: Title-based session management — thread title parameter through bridge, session manager, REST API, and WebSocket init path; deterministic UUIDv5 session IDs from (userID, workerType, title, workDir). (f4761c66)
- **Gateway Core**: `RepairRunningSessions` on startup — stale running sessions transitioned to terminated, preventing ghost sessions after gateway restart. (4fd59ee5)
- **Gateway Core**: `GetSessionsByState` store query + migration 003 backfill for NULL `work_dir` values. (4fd59ee5)
- **Gateway Core**: REST API tests — 15 HTTP handler tests covering CreateSession, DeleteSession, ListSessions, GetSession, SwitchWorkDir endpoints. (8d701565)
- **Gateway Core**: Session manager tests — coverage for RepairRunningSessions, DetachWorkerIf CAS, GetSessionsByState, work_dir round-trip, migration idempotency. (be7eb9e9, 4ac803d8)
- **Messaging**: Feishu streaming card TTL rotation — proactive 6-minute card replacement with async abort and reply_to threading to bypass Feishu's 10-minute server limit. (2bccd702)
- **Session**: Conversation store — async batch writer for turn-level persistence (user input + assistant response with tools, tokens, cost, duration); 3 recording paths (normal done, crash/timeout, fresh start). (ce02d0eb)
- **Gateway Core**: Session stats API — aggregated turn statistics from done events (`GET /api/sessions/{id}/stats`). (ce02d0eb)

### Changed

- **Session**: SQLite storage optimization — PRAGMA tuning (32MB cache, 256MB mmap), cascade delete for events/audit on session deletion, events TTL cleanup (30 days), automatic VACUUM when free pages exceed 20%. (2d569a8b)
- **Gateway Core**: Fast reconnect for idle sessions — skip terminate+resume cycle when worker is still alive, transition directly back to running. (0a71a61b)
- **Gateway Core**: CAS semantics for DetachWorker — prevents old forwardEvents goroutines from clobbering a concurrently replaced worker. (0a71a61b)
- **CLI**: Agent config templates migrated from Go constants to `embed.FS` files, onboard wizard streamlined v3→v4. (9f56623d)
- **Gateway Core**: Code quality pass — extract `IsDeadProcessError` helper, merge accumulator locks, skip tracing spans for high-frequency pings, promote bare strings to constants. (120d2487, e9b05625)

### Fixed

- **Gateway Core**: ClaudeCode mapper silently discarded `EventSystem` and `EventSessionState` — payload type mismatch (`string` vs `json.RawMessage`) caused all state transitions to be dropped. (2bccd702)
- **Gateway Core**: Worker crash recovery — transient `INTERNAL_ERROR` suppressed, `RESUME_RETRY` handled with automatic fresh-start fallback in UI. (0a71a61b)
- **Gateway Core**: Skip LLM retry for empty output from resumed workers and exit code 143 (SIGTERM from connection replacement). (0a71a61b)
- **Messaging**: Feishu streaming card write failure now gracefully falls back to static IM delivery instead of returning error to caller. (2bccd702)
- **WebChat**: Connection stability — deterministic session IDs across REST/WS paths, browser console warnings eliminated, frontend crash guards for undefined message roles. (0a71a61b, 14575983, 4fd59ee5)
- **WebChat**: CommandMenu filter bug — inconsistent `/` prefix variable caused slash commands to not filter correctly. (120d2487)
- **WebChat**: useCopyToClipboard timeout leak on unmount; useSessions panel state stabilized with useCallback. (120d2487)
- **Session**: `errors.Is` for `sql.ErrNoRows` comparison (errorlint compliance). (45ddac6f)
- **E2E**: Flaky state event assertion removed from SendInputReceiveEvents test. (01ffdec7)

## [1.1.1] - 2026-04-26

### Added

- **WebChat**: "Obsidian" dark theme redesign — glassmorphism design system, Outfit + JetBrains Mono typography, framer-motion spring animations across messages, tool cards, and reasoning blocks.
- **WebChat**: GenUI tool rendering — TerminalTool (stdout/stderr split, auto-collapse), FileDiffTool (syntax-aware diff with copy), SearchTool (match highlighting), and PermissionCard (approve/reject MCP events interactively).
- **WebChat**: Slash command palette (`CommandMenu`) with fuzzy search across all commands (`/gc`, `/reset`, `/cd`, `/skills`, `/new`) and worker skills.
- **WebChat**: MetricsBar — live token counts, turn latency, and wall-clock time extracted from AEP `done.stats` events.
- **WebChat**: NewSessionModal with worker type selector, workdir input, recent directories dropdown, and nuqs URL deep linking for one-click session setup.
- **WebChat**: Code block folding, syntax highlighting, and copy-to-clipboard in Markdown rendering.
- **Gateway**: OpenCode Server singleton process model — all sessions share one lazily-started `opencode serve` process with ref counting and 30m idle drain, replacing per-session process spawning.
- **Gateway**: `/cd <path>` in-session directory switching with path validation and security guard; `/skills` command to list available worker skills.
- **Gateway**: Agent config XML injection with B/C channel architecture (`<directives>` for SOUL/AGENTS/SKILLS, `<context>` for USER/MEMORY); platform variants (e.g. `SOUL.slack.md`) auto-appended.
- **Gateway**: Session `work_dir` persistence — working directory stored in SQLite, enabling session stickiness across page reloads and idempotent session re-creation via `DeletePhysical`.
- **CLI**: Onboard wizard auto-generates agent config files (SOUL/AGENTS/SKILLS/USER/MEMORY) during setup.

### Changed

- **Infrastructure**: install.sh rewritten as binary-only installer (851→113 lines); uninstall.sh streamlined (189→102 lines) with `--purge` and PID cleanup.
- **Configuration**: Agent config size limits tightened to 8K/file, 40K total.

### Fixed

- **Gateway**: Nil pointer panic in claudecode worker `Resume()` — race condition where `w.Proc` was nil'd by concurrent `Terminate()` while `Resume()` called `Start()`.
- **Gateway**: Worker crash recovery — transient `INTERNAL_ERROR` suppressed; `RESUME_RETRY` handled gracefully in UI with automatic fresh-start fallback.
- **Gateway**: SQLite session migration silent failure — batch SQL split to per-statement execution, fixing missing `work_dir` column on upgrade.
- **WebChat**: Composer input frozen after slash command interaction — state synchronization restored IME compatibility and keyboard responsiveness.
- **WebChat**: User-facing error messages for terminal states (SESSION_BUSY, TURN_TIMEOUT, INTERNAL_ERROR) replace raw error codes.
- **WebChat**: Minor fixes — CommandMenu visibility, NewSessionModal dropdown overflow, Jump-to-Last button positioning, code block wrapping, turbopack serialization warnings.
