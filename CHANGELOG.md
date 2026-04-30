# Changelog

## [1.3.0] - 2026-04-30

### Summary

v1.3.0 是一次 minor 版本更新，聚焦于 **跨平台 Windows 支持、Messaging 适配器 DRY/SOLID 重构、Session History REST API、DI 构造函数注入和离线 Bundle 打包**。新增 Windows amd64/arm64 一等公民支持（Job Object 进程树管理、跨平台路径安全验证、TTY 检测）。Messaging 层完成 issue #65 七阶段重构，提取共享 Pipeline、ConnPool、Streaming、Dedup/Backoff/Gate 到基础包，Feishu/Slack 适配器代码量大幅缩减。Gateway 新增 `GET /api/sessions/{id}/history` 端点和 ConversationStore 分页查询。DI 层从 setter 链式调用迁移到构造函数注入。CI 新增 OpenCode + OMO 离线 bundle 自动打包发布流程。

### Added

- **Platform**: Windows amd64/arm64 一等公民支持 — Job Object 进程树清理、跨平台 TTY 检测、路径安全验证、pipe 错误检测。(#64, 794a8ff, de8b097, 1e75218, 6229be9)
- **Platform**: 跨平台构建目标 — Makefile 新增 `GOOS=windows` 构建，release workflow 支持 Windows 二进制发布。(#64, 794a8ff)
- **Gateway**: `GET /api/sessions/{id}/history` — 会话历史 REST 端点，支持分页查询 ConversationStore。(#60, d17b90d, 440525e)
- **Gateway**: `GetBySessionBefore` — ConversationStore 新增基于序列号的分页查询，用于历史回放。(#60, 440525e)
- **WebChat**: History runtime adapter — 将 session history API 集成到运行时适配器，支持客户端历史加载。(#60, a5bbb72, 686e1fa)
- **CI**: OpenCode + OMO 离线 bundle — GitHub Release 自动打包和安装脚本，支持离线环境部署。(#59, ffaf168, 3412b22)

### Changed

- **Messaging**: 七阶段 DRY/SOLID 重构 (issue #65) — 提取共享 Pipeline（消息处理管道）、ConnPool（连接池）、StreamingCard 抽象、Dedup/Backoff/Gate 到 `internal/messaging/` 基础包；Feishu/Slack 适配器代码量大幅缩减，消除重复逻辑。(#67, 02f239b, 1be2d0f, c0892b4, 37751dc, e8f0ae3, 24cf4eb)
- **Messaging**: ConnPool 热路径优化 — 消除 stringly-typed log channels，提升连接查找性能。(#67, e8f0ae3)
- **DI**: 构造函数注入替代 setter 链 — `Bridge`、`Hub`、`SessionManager` 等组件改用构造函数注入，消除运行时 nil 风险。(#63, f906d30)
- **Logging**: 结构化日志优化 — gateway 栈统一 slog key-value 风格，消除冗余日志属性。(#67, baf4546)
- **Security**: SSRF DNS mock + TOCTOU 修复 — 消除 lock-DB-I/O 反模式，修复配置验证时序窗口，平台 key 去重。(#67, a290bb3)
- **Security**: 跨平台路径验证重构 — `ValidateWorkDir` 拆分为 `path_unix.go`/`path_windows.go`，Windows 路径比较大小写不敏感。(#64, 6229be9, d702753)
- **Session**: 移除 EventStore，ConversationStore 增加统计查询 — 简化持久化架构为双层（MessageStore + ConversationStore）。(#67, a7b6eda)
- **Config**: codecov 统一 1% threshold，patch target 50% + 10% threshold。(#67, ab5b465, b048eb9)
- **Hooks**: pre-push gate 增加 fmt + lint 检查，在本地拦截 CI 失败。(#63, 05961c1)

### Fixed

- **Gateway**: `handler.sm` nil guard — conn.go idle 转换检查添加空指针防护。(#67, 9203060)
- **Slack**: `TestShortenPaths` data race — 使用 mutex-guarded `SetWorkDir` 消除竞态条件。(#68, 0e30a30)
- **WebChat**: 依赖更新和类型兼容性修复。(#68, fab6be6)
- **Windows**: 进程管理加固 — 修复跨平台 STT Job Object 清理、PID 文件路径、signal 处理。(#64, 031eab2, f602b6a, de8b097)
- **CI**: large file guard 扫描 HEAD tree only，避免误报。(#59, d4b6818)
- **CI**: OMO 插件通过 OpenCode auto-discovery 注册。(#59, 1883866)

## [1.2.0] - 2026-04-29

### Summary

v1.2.0 是一次 minor 版本更新，聚焦于 **会话历史持久化、Skills 体验升级和仓库安全加固**。新增 MessageStore（事件级持久化）和 ConversationStore（轮次级持久化）双层架构，为会话回放和审计提供数据基础。Gateway 新增 Skills 发现与列表展示能力（缓存 + DRY 共享模块）。WebChat 完成 UX v5 重构（智能折叠、高级动画、GenUI 工具组件）。安全层面完成 5 层大文件防御体系（gitignore → gitattributes → pre-commit → pre-push → CI guard），并从历史中彻底清除 46MB 误提交二进制。测试覆盖率持续提升至中高风险路径全覆盖。

### Added

- **Session**: MessageStore — event-level persistence with async batch writer, SQLite backend, Postgres stub; `EventStoreEnabled` config flag (default: true). (4f20233)
- **Session**: ConversationStore — turn-level persistence (user input + assistant response with tools, tokens, cost, duration); cascade delete on session removal. (4f20233)
- **Session**: Admin stats endpoint — `GET /admin/sessions/{id}/stats` for aggregated session statistics. (4f20233)
- **Gateway Core**: SkillsLocator — project/user/plugin directory scanning with configurable TTL cache. (c7446b3, ef58fe7)
- **Gateway Core**: Skills listing event dispatch — `/skills` command in WebSocket and messaging platforms with paginated display. (36699d5)
- **Messaging**: Shared skills helpers — DRY consolidation of Feishu/Slack skills list formatting into `messaging/skills_helpers.go`. (2bf2bbd)
- **WebChat**: Hybrid architecture v5 — smart collapsing, advanced animations, and layout optimization. (4fea33b)
- **WebChat**: GenUI tool components — ListTool and TodoTool for enhanced tool rendering. (5164387)
- **WebChat**: Message cache and turn replay utilities for client-side session history. (4f20233)
- **Security**: 5-layer large file defense — `.gitignore` + `.gitattributes` + pre-commit hook + pre-push scan + CI large-file-guard job; blocks all files >1MB from entering the repository. (b410de5, 0b3e866)
- **Security**: `RegisterCommand` validation — path separator and dangerous character checks for worker command whitelist. (e9f6415)
- **Gateway Core**: Configurable Claude Code startup command via `worker.claude_code.command`. (e9f6415)
- **Agent Config**: Built-in metacognition via `go:embed` — agent self-knowledge (architecture, mechanisms) injected as C-channel. (879298d)
- **Docs**: Feishu and Slack integration guides (Chinese + English bilingual). (4f20233)

### Changed

- **Gateway Core**: Skills parsing deduplication — eliminated duplication between skills_locator and gateway handler. (624ca02)
- **Gateway Core**: Control request timeout isolated — prevent global timeout from affecting individual control commands. (d940e91)
- **Messaging**: Feishu streaming card error handling — error events now close streaming card to prevent stale cards after TURN_TIMEOUT. (879298d)
- **Messaging**: Silent message drop prevention — check session state before assuming worker is alive on resume. (879298d)
- **Session**: Reset ExpiresAt on session resume — prevent GC max_lifetime from killing reactivated sessions. (879298d)
- **Infrastructure**: Removed 46MB `hotplex` binary from git history; `.git` size reduced from 52MB to 8MB. (0b3e866)
- **Testing**: Coverage expansion — medium-ROI packages (cli/checkers, skills, worker/claudecode, feishu) now at 67–89%. (e9f6415, 879298d)

### Fixed

- **Gateway Core**: Frontmatter parsing fixed — skills display format unified across platforms. (db98ffa)
- **Gateway Core**: Claude Code skills properly discovered from all skill directories (user, project, plugin). (63c7b64)
- **Gateway Core**: LLM retry false positives — `ShouldRetry` now matches only ErrorData, not turn text containing error-like strings. (879298d)
- **Gateway Core**: Proc double-log on exit — guarded by `exited` flag to prevent duplicate log lines. (879298d)
- **Worker**: OCS compact auto-resolves model from message history; rewind auto-resolves last assistant messageID. (b7569f1)
- **Messaging**: Slack `sendSkillsList` method added for SkillsList event handling. (a6a0b2c)
- **WebChat**: Bot avatar alignment, rendering fixes, and useAuiState migration. (34d6f7c)
- **WebChat**: Default workDir passed when creating sessions. (c7146a4)
- **Security**: 22 non-functional issues resolved from comprehensive audit (errcheck, gocritic, gofmt). (175671e)

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
