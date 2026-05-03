# Changelog

## [1.5.0] - 2026-05-03

### Summary

v1.5.0 是一次 minor 版本更新，聚焦于 **Slack 运维自服务、Agent 配置灵活性、反馈连续性诊断、Session 生命周期可靠性**。新增 `hotplex slack` CLI 子命令组（10 个命令），Agent 配置升级为 per-bot 3-level 目录 fallback（BREAKING：session ID 变化），hotplex-diagnostics skill 引入反馈链路时间线交叉验证检测静默中断。Gateway 修复了 resume 盲重试导致的 5 秒延迟（P0），飞书流式卡片解耦 Write/Flush 消除速率限制丢帧。

### Added

- **CLI**: `hotplex slack` 子命令组 — 10 个命令（send-message, update-message, schedule-message, upload-file, download-file, list-channels, search, canvas, bookmark, react），支持 env var 自动解析 channel/thread 上下文。Gateway 通过 bridge 注入 `HOTPLEX_SLACK_CHANNEL_ID`/`HOTPLEX_SLACK_THREAD_TS` 到 Worker 环境。(#137, #131)
- **Configuration**: Per-bot 3-level 目录 fallback — Agent 配置按文件独立解析 `global → platform/<platform>/ → <platform>/<botID>/`，替代旧的后缀追加机制（`SOUL.slack.md`）。BotID 贯穿 Slack/飞书全链路并纳入 session key 派生。新增 `agentConfigSuffixChecker` 检测废弃后缀文件。(#129)
- **Skills**: hotplex-diagnostics 运行时诊断 skill — 7 步方法论（进程 → Session → 反馈连续性 → 日志 → 适配器 → 源码 → Issue），FEEDBACK_STALL 分类（PIPELINE_STALL/BACKPRESSURE_DROP/ADAPTER_FAILURE/CLIENT_DISCONNECT），时间线交叉验证检测静默中断。
- **Gateway**: 精确 context 用量 — `get_context_usage` control channel 查询替代聚合 Done 事件统计，消除跨 turn 累积导致的 context fill 虚高。Turn summary 工具名显示限制为 top 5（"+N" 提示）。(#132)
- **CLI**: Gateway 子命令分解 — `runGateway()` 拆分为 initLogging/initOrphanCleanup/initStores/shutdownGateway，PID 文件升级为 JSON 格式存储 config path 和 dev mode。admin_adapters.go 从 routes.go 提取。
- **WebChat**: TurnSummaryCard 组件 — 前端渲染 per-turn session 数据（模型、context fill、时长、工具调用）。

### Changed

- **Gateway**: Worker 清理事件改为 Error 替代合成 Done — 语义正确，相同的 UI 清理（清除指示器、关闭流式卡片）但不触发 turn summary。提取 `sendError` 辅助函数替换 7 处内联 Error 信封创建。新增 `ErrCodeTurnTimeout` 常量。
- **Agent Config**: Context fill 计算修正 — Claude Code SDK `usage.input_tokens` 已含 cached tokens（billing breakdown 非叠加），移除重复计数消除 ContextFill 超过 ContextWindow 的不可能值。
- **Configuration**: `Bridge.adapter` 改用 `atomic.Value` 保证线程安全；`SetAdapter` 在 platform mismatch 时返回 error 而非仅日志 Warn。
- **CI**: GitHub Actions 升级至 Node.js 24 兼容版本 — actions/cache v5, upload/download-artifact v7。

### Fixed

- **Gateway**: Session 生命周期修复 — resume 盲重试消除（P0: 新增 `SessionFileChecker` 接口，bridge 恢复前检查 session 文件存在性，zombie GC 删除文件时自动降级为全新启动）；GC race 修复（handleGC 原子重读 session state 避免与 cleanupCrashedWorker 竞争）；空 session ID 验证。(#135, #133, #134)
- **Messaging/Feishu**: Write/Flush 解耦为后台 150ms timer loop — 防止 CardKit 100ms 速率限制静默丢帧和误报 integrity warning，修复 `bufRunes` 计数器 flush 后未重置。(#128)
- **WebChat**: 消息去重 — adapter 层在传入 assistant-ui ExternalStoreAdapter 前按 ID 去重，修复事件处理器竞争导致的 `MessageRepository same id already exists` 错误。

### Security

- **CLI/Slack**: `loadEnvFile` 防止覆盖已有环境变量；Worker 白名单使用精确条目替代宽泛前缀匹配；`download-file` 使用认证的 `client.GetFileContext` 替代裸 `http.Get`；rune 截断确保 CJK 字符正确处理。

## [1.4.0] - 2026-05-03

### Summary

v1.4.0 是一次 minor 版本更新，聚焦于 **运维自服务能力、Gateway 稳定性与并发安全、SQLite OOM 韧性、Messaging 层架构治理**。新增 CLI 自更新命令（GitHub Releases API + SHA256 校验 + 原子替换）、Turn Summary 紧凑单行摘要（per-turn stats + context fill 百分比）、智能目录切换安全策略、config-driven Worker 环境变量注入、SQLite CGo 双驱动自动降级。Gateway 全面加固并发安全（pcEntry race condition、Session panic recovery、WriteBufferFull 信号化），上下文占用率改用当前 turn token 计算以对齐 Claude Code 行为。Messaging 层提取 BaseAdapter 消除 connPool 重复、统一 `$context` 命令输出。CI 流水线并行化和缓存优化显著缩短构建时间。

### Added

- **CLI**: `hotplex update` 自更新命令 — GitHub Releases API 集成、SHA256 校验验证、原子二进制替换、服务自动重启支持（`--check`、`-y`、`--restart` 标志）。(#115)
- **Messaging**: Turn Summary per-turn stats — Done 事件后生成紧凑单行摘要（`Model · N% · ⏱ Xs · 🔧 N`），从 session accumulator 提取模型、上下文占用率、时长、工具调用数。(#122, #118)
- **Security**: 智能目录切换安全策略 — 支持用户主目录和 `/usr/local` 约定优于配置的智能判断，跨平台路径验证。(#110 相关)
- **Configuration**: Worker 环境变量注入 — `worker.environment` 配置驱动，支持 `${VAR}` 展开和 `env_whitelist` 过滤，默认注入 `BUN_RUNTIME_NV=disable_avx512`。
- **Configuration**: `db.events_path` 可配置 — events.db 路径支持自定义，脱离默认数据目录。
- **SQLite**: CGo 双驱动自动降级 — CGo 构建使用 mattn/go-sqlite3（性能优先），纯 Go 构建使用 modernc.org/sqlite（OOM 韧性），build tag 自动选择。
- **Messaging**: `$context` 命令输出美化 — 共享格式化层（severity 级别、进度条、友好 token 计数、操作建议），Slack/飞书/WebChat 统一输出。WebChat 新增 ContextUsageCard 玻璃态组件。(#110)
- **Messaging**: events.Message 处理 — 飞书和 Slack 适配器新增 handler/bridge 发起的独立消息处理（cd 确认、命令反馈）。
- **Messaging**: 控制命令详细错误消息 — 用户友好的错误提示替代静默失败。

### Changed

- **Messaging**: 提取 BaseAdapter[C] 泛型结构体 — 消除 Slack/飞书适配器约 30 行 connPool 重复代码，提供 `InitConnPool`/`GetOrCreateConn`/`DrainConns`/`DeleteConn` 统一生命周期管理。(#114)
- **Gateway**: 模块拆分 — 从 hub.go 提取 SeqGen 和 pcEntry 到独立文件，提取 `createAndLaunchWorker`、`requireActiveOwner` 辅助函数，Handler 改用 SessionManager 接口替代具体类型。
- **Gateway**: 上下文占用率改用 `ContextFill`（当前 turn input tokens）替代累计 TotalInput 计算 — 对齐 Claude Code per-turn 上下文跟踪语义，新增 `context_fill` 字段到 session stats snapshot。
- **Gateway**: 移除 TurnIdleTimeout idle detection — 合成 Done 机制不可靠，execution_timeout 30m 仍是僵尸会话安全网。
- **Gateway**: 消除 `toInt64`/`toFloat64`/`formatTokenCount`/`extractSessionStats` 重复实现 — 替换为 events 包 `ToInt64`/`ToFloat64` 等价函数，移除死代码。
- **Session**: 提取 `audit_store.go`（审计追踪方法）、`stores.go`（store 工厂）、`updateSession` 辅助函数，store.go 减少 182 行。
- **SQLite**: 提取 `sqlutil` 包统一 DB 初始化和 PRAGMA 调优，消除冗余零值回退。
- **Security**: 提取 `resolveSigningKey` 辅助函数，移除无用的 `DevAllowedTools`。
- **Admin**: 提取 CORS 处理到中间件。
- **Metrics**: 移除 `session_id` label 防止 delta 指标基数无限增长。
- **CI**: 流水线优化 — 移除 Gate 阶段、步骤并行化、Go/WebChat 缓存。
- **Messaging**: 关闭 turn_timeout 默认值（原 15m 过于激进，execution_timeout 30m 已足够捕获僵尸会话）。
- **Service**: systemd WorkingDirectory 对齐 worker.default_work_dir 而非硬编码 `$HOME`。(#109)

### Fixed

- **Service**: ExecStart 路径解析 — `ResolveBinaryPath()` 优先使用 PATH 查找（`exec.LookPath`），修复 build 目录路径写入 systemd unit 的问题。(#113)
- **Configuration**: `${VAR}` 环境变量展开 — ExpandEnv 已定义但未在加载路径中调用，worker.environment 值注入为字面量而非展开值。(#111)
- **Configuration**: Workdir fallback 链统一 — messaging 适配器路径正确展开 `~` 为用户主目录。
- **Gateway**: pcEntry WriteCtx/Close race condition — 并发写入和关闭导致 panic，新增错误分类辅助函数。关闭 data channel 解除 writeLoop 阻塞。
- **Session**: 并发安全加固 — UpdateWorkDir/ClearContext 添加 RLock 保护早期返回字段读取；回调函数添加 panic recovery；级联删除包裹在事务中；WriteBuffer 满时返回 `ErrWriteBufferFull` 替代静默丢弃。
- **Worker**: RLIMIT_AS 自限 bug 修复 — 网关进程被自身内存限制崩溃；禁用 RLIMIT_AS 修复 Bun 运行时崩溃。(#112 相关)
- **Worker**: OCS SSE 超时和启动问题修复；releaseOnce 行为测试覆盖。(#112 相关)
- **Messaging**: watchTimeout panic recovery 和 idleMonitor context race 修复。
- **Messaging/Feishu**: ChatQueue `wg.Add` 移入 mutex lock 内防止 race；非用户流式卡片清理路径用 Close() 替代 Abort()；控制命令错误静默失败修复。
- **Messaging/Feishu**: `sendTurnSummary` 改用 fresh context 并在活跃流式卡片中追加摘要，避免重复消息和 stale context 问题。
- **Agent Config**: `readFile` 区分 IsNotExist 和其他错误类型。
- **CLI**: wizard stepAgentConfig 写入和关闭错误处理。

### Security

- **Configuration**: GH_TOKEN/GITHUB_TOKEN 重命名为 HOTPLEX_WORKER_GH_TOKEN/HOTPLEX_WORKER_GITHUB_TOKEN 前缀，防止 shell 环境污染影响 `gh` CLI keyring 认证。(#111)

## [1.3.0] - 2026-04-30

### Summary

v1.3.0 是一次 minor 版本更新，聚焦于 **跨平台 Windows 支持、Gateway 安全默认值与生命周期管理、WebChat 嵌入式 SPA + AI Native UX、Messaging 适配器 DRY/SOLID 重构、Session History REST API、DI 构造函数注入**。新增 Windows amd64/arm64 一等公民支持（Job Object 进程树管理、纯 Go SQLite、跨平台路径安全验证）。Gateway 全面加固安全基线（localhost-only 默认绑定）、新增系统服务管理（systemd/launchd/SCM）和守护进程模式。WebChat 完成嵌入式 SPA 部署（`go:embed`）和 AI Native UX 升级（ghost assistant、AgentTool/TodoTool、glassmorphism 设计）。Messaging 层完成 issue #65 七阶段重构，提取共享 Pipeline/ConnPool/Streaming/Dedup/Backoff/Gate 到基础包。DI 层从 setter 链迁移到构造函数注入。

### Added

- **Platform**: Windows amd64/arm64 一等公民支持 — Job Object 进程树清理、纯 Go SQLite (modernc.org)、跨平台 TTY 检测、路径安全验证、pipe 错误检测。(#64, 794a8ff, de8b097, 1e75218, 6229be9)
- **Gateway**: 安全默认值加固 — localhost-only 默认绑定、daemon 模式 (`-d`)、统一进程生命周期管理 (stop/restart/status/service)。(#62, 3204cfc)
- **Gateway**: 系统服务管理 — install/uninstall/start/stop/restart 支持 systemd/launchd/Windows SCM，user/system 级别。(#62, 3204cfc)
- **Gateway**: `GET /api/sessions/{id}/history` — 会话历史 REST 端点，支持 cursor 分页查询 ConversationStore。(#60, d17b90d, 440525e)
- **Gateway**: `GetBySessionBefore` — ConversationStore 新增基于序列号的分页查询，用于历史回放。(#60, 440525e)
- **WebChat**: 嵌入式 SPA — Next.js 静态导出通过 `go:embed` 嵌入 gateway 二进制，SPA fallback 路由，零外部依赖部署。(#62, 5d0a414)
- **WebChat**: Ghost assistant — 后端静默期间显示 skeleton 占位符，消除"黑洞效应"。(#62, 0ce448c, 171963f)
- **WebChat**: AI Native UX — thinking indicator 重设计、glassmorphism copy button、inline message actions、AgentTool 和 TodoTool 结构化任务渲染。(#62, 08fa8ed, cf6a2b6, 3003c0b, 2f49dfa, aa727b6)
- **WebChat**: History runtime adapter — L1 (React state) + L2 (LocalStorage) 双层缓存，cursor 分页历史加载。(#60, a5bbb72, 686e1fa, ed7aecb)
- **Skills**: Skills 发现整合到独立 `skills` 包 — 可配置 TTL 缓存 + CWD 目录扫描。(#67, f1538fe)
- **CI**: OpenCode + OMO 离线 bundle — GitHub Release 自动打包和安装脚本，支持离线环境部署。(#59, ffaf168, 3412b22)
- **Infrastructure**: `make hooks` 目标自动安装 git hooks，`make quickstart` 适配非开发者用户。(#62, c61b03e, ca5b698)

### Changed

- **Messaging**: 七阶段 DRY/SOLID 重构 (issue #65) — 提取共享 Pipeline、ConnPool、StreamingCard 抽象、Dedup/Backoff/Gate 到 `internal/messaging/` 基础包；Feishu/Slack 适配器代码量大幅缩减。(#67, 02f239b, 1be2d0f, c0892b4, 37751dc, e8f0ae3, 24cf4eb)
- **DI**: 构造函数注入替代 setter 链 — Handler/Bridge/Adapter 改用 `Deps` struct 注入，编译期保证完整性。(#61, f906d30)
- **Logging**: 结构化日志优化 — gateway 栈统一 slog key-value 风格 (snake_case)，注入 channel 标识到所有组件 logger，14 个可恢复错误从 Error 降级为 Warn。(#67, baf4546)
- **Security**: TOCTOU 修复 + SSRF DNS mock — ExpandAndAbs 解析符号链接防止 workdir 竞态，6 个 Manager 方法 copy-then-write 模式消除 lock-DB-I/O。(#67, a290bb3)
- **Security**: 跨平台路径验证重构 — `ValidateWorkDir` 拆分为 `path_unix.go`/`path_windows.go`，Windows 大小写不敏感。(#64, 6229be9, d702753)
- **Session**: 移除 EventStore + PostgresStore，ConversationStore 增加统计查询 — 简化持久化架构。(#67, a7b6eda)
- **Gateway**: PID 文件容错 — restart 遇到 stale PID 时 warning 并继续启动而非失败。(#62, 0165d17)
- **Config**: `~` 展开修复 — 所有 YAML 路径字段正确展开波浪号为 home 目录，防止创建字面 `~` 目录。(#62, dad790c)
- **Hooks**: pre-push gate 增强 — 新增 fmt + lint + go vet + go mod verify + race test，本地拦截 CI 失败。(#62, 05961c1, c61b03e, 6e08233)

### Fixed

- **Messaging**: 飞书消息无响应三类根因 — ResumeSession 输入丢失、Error 事件静默丢弃、长时间无输出无提示。(#68, e96640d)
- **Gateway**: `handler.sm` nil guard — conn.go idle 转换检查添加空指针防护。(#67, 9203060)
- **Slack**: `TestShortenPaths` data race — mutex-guarded `SetWorkDir` 消除竞态条件。(#68, 0e30a30)
- **WebChat**: 多项 UX 修复 — 额外换行、tool 折叠/展开、terminal 状态生命周期、reasoning markdown 渲染、scroll button 定位。(#62, various)
- **WebChat**: 依赖更新和类型兼容性修复 — pin ai SDK 版本，修复 ConversationRecord success 类型。(#68, fab6be6, 632f180)
- **Windows**: 进程管理加固 — 修复跨平台 STT Job Object 清理、PID 文件路径、signal 处理、pipe 错误检测。(#64, 031eab2, f602b6a, de8b097)
- **CI**: large file guard 扫描 HEAD tree only 避免误报；OMO 插件通过 OpenCode auto-discovery 注册。(#59, d4b6818, 1883866)

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
