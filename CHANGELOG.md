# Changelog

## [1.7.0] - 2026-05-07

### Summary

v1.7.0 是一次 minor 版本更新，聚焦于 **LLM 编排能力** 和 **语音交互管道**。新增 Brain 模块作为 Gateway 内置的 LLM 编排层（支持 OpenAI/Anthropic 双协议，含重试/缓存/限流/熔断装饰器链），实现飞书和 Slack 双平台的 voice-in → voice-out 全链路管道（STT → Brain 摘要 → Edge TTS → Opus 编码 → 音频消息回传）。Agent Config 获得 Windows 命令注入防护和 XML 标签清洗，Brain 模块经过严格清理删除 ~2700 行死代码并修复 4 个并发安全缺陷。

### Added

- **Brain**: LLM orchestration module — lightweight Chat()/ChatStream() interface with OpenAI and Anthropic protocol support, decorator chain (retry → cache → rate limit → circuit breaker → metrics), model routing, and cost estimation. Fail-open design: missing API key = graceful degradation. (#273)
- **Messaging/Feishu**: TTS voice reply pipeline — voice input detected → Brain text summary → Edge TTS synthesis → FFmpeg MP3→Opus conversion → Feishu audio message upload. Configurable voice, max chars, and concurrency limiter. (#273)
- **Messaging/Slack**: TTS voice reply pipeline mirroring Feishu behavior — identical voice-triggered flow with Slack file upload integration. (#273)
- **Configuration**: Onboard wizard enhanced with environment-backed worker commands and contextual tips in generated config and .env files.

### Changed

- **Agent Config**: Windows command injection prevention — switch from inline `--append-system-prompt` to temporary file injection (`--append-system-prompt-file`) to avoid cmd.exe argument mangling.
- **Agent Config**: XML tag sanitization in system prompt builder — unreserved XML tags are HTML-escaped to prevent prompt injection through config files.
- **Agent Config**: META-COGNITION.md enhanced with P0–P4 decision tree, config modification SOP, and self-correction protocol. Moved to B-channel (directives) for higher authority.
- **Brain**: Simplified API key resolution to 3 clear priority levels — dedicated `HOTPLEX_BRAIN_*` env vars → worker config file extraction (`~/.claude/settings.json`) → system env vars (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`). Removed `PROVIDER_TYPE` concept.
- **Brain**: Removed ~2700 lines of dead code — 7 unused components (failover, priority, budget, health, observable, builder, presets), 3 dead interfaces, duplicate LLM client wrappers. Extracted shared helpers, promoted atomic counters, consolidated token estimation.

### Fixed

- **Brain**: 4 critical concurrency issues — ChatStream context cancel causing goroutine leaks, globalBrain unprotected concurrent access, Anthropic streaming goroutine leak on ctx cancellation, CachedClient silently swallowing json.Marshal errors. (#273)
- **Brain**: API key not wired to LLM client constructors — hardcoded empty string bypassed config resolution entirely.
- **Brain**: IntentRouter data race (int64 counters → atomic.Int64) and MetricsCollector nil panic on OTel instrument creation failure.
- **Messaging/TTS**: `voiceTriggered` data race (bool → atomic.Bool), TTS goroutine using platform writer's 30s context instead of independent 60s context, text truncation off-by-3.
- **Messaging/Feishu**: Content() extraction moved before Close() to prevent empty text on TTS pipeline, brain input truncated to 5× maxChars before LLM summary.
- **CLI**: Onboard wizard missing worker commands in generated config.yaml and .env files.

### Security

- **Agent Config**: Windows cmd.exe argument injection mitigated via temporary file-based prompt injection. Unreserved XML tags in agent configs are sanitized at prompt assembly time.

## [1.6.1] - 2026-05-06

### Summary

v1.6.1 是一次 patch 版本更新，聚焦于 **Worker 环境变量安全隔离重构**。将脆弱的 allow-by-default 白名单替换为 deny-by-default 黑名单，修复 Windows 平台因系统变量被过滤导致的 Worker 启动失败。新增 `HOTPLEX_WORKER_` 前缀剥离机制，让用户在 .env 中配置的密钥（如 `HOTPLEX_WORKER_GITHUB_TOKEN`）安全注入 Worker 子进程。清理了 735 行死代码，修复了 `allowed_envs` 配置项的语义反转 bug，并全面同步了文档和规格。

### Added

- **Worker**: `HOTPLEX_WORKER_` prefix stripping — secrets in .env prefixed with `HOTPLEX_WORKER_` are automatically stripped and injected into worker subprocess env (e.g., `HOTPLEX_WORKER_GITHUB_TOKEN` → `GITHUB_TOKEN`). Gateway-internal `HOTPLEX_*` vars are never exposed.
- **Messaging**: `turn_summary_enabled` config toggle to control whether turn summary messages are sent after each completed turn (default: true). (#223)

### Changed

- **Worker**: Environment variable filtering inverted from whitelist to blocklist — all `os.Environ()` vars pass through by default; only explicitly blocked entries are filtered. This fixes Windows compatibility where system vars (SystemRoot, ProgramFiles, etc.) were previously stripped. (307ae85)
- **Worker**: Blocklist entries ending with `_` are treated as prefix matches (e.g., `HOTPLEX_` blocks all `HOTPLEX_*` vars).
- **Worker**: `BuildEnv` optimized — single `os.Environ()` snapshot instead of 3 calls; Phase 7 reuses `setOrAppend` helper.
- **Messaging**: TurnSummary rendering deduplicated — extracted `Fields()` method consolidating field selection across Slack/Feishu paths. (#223)
- **Security**: Removed dead code — `SafeEnvBuilder`, `BuildWorkerEnv`, `IsSensitive`, `env_builder.go` (~735 lines). `base.BuildEnv` is now the sole env construction path.
- **Configuration**: Removed broken `allowed_envs` field (its entries were incorrectly merged into blocklist, blocking vars instead of allowing them). Use `env_blocklist` or `HOTPLEX_WORKER_` prefix instead.
- **Docs**: All documentation synchronized from whitelist to blocklist terminology — Env-Whitelist-Strategy rewritten as Blocklist Strategy, spec docs (SEC-030~036), config reference, and README updated.

## [1.6.0] - 2026-05-06

### Summary

v1.6.0 是一次 minor 版本更新，聚焦于 **Onboard 体验重构** 和 **Gateway 可靠性加固**。Onboard Wizard 经历全面改造：新增步骤注册表支持增量重跑、凭据校验、binary PATH 安装，并修复了安全默认值（localhost 绑定）和测试二进制覆盖 PATH 的严重 bug。Gateway Core 获得多层 panic recovery 和 FD leak 修复。Slack 适配器新增流式轮转消除 Stream expired 降级。Worker 新增 permission_prompt 可配置和 interaction chain 三个 P0 修复。

### Added

- **CLI**: Onboard wizard step registry with three run modes — full config, keep existing, and incremental step selection. (#203)
- **CLI**: Binary install step — auto-installs hotplex to PATH after onboard (SHA256 verification, cross-platform rc file config).
- **CLI**: Credential validation for Slack (xoxb-/xapp-) and Feishu (cli_) tokens during onboard input.
- **CLI**: Agent config template deployment during onboard with 3-level fallback documentation.
- **Worker**: `permission_prompt` config option under `worker.claude_code` to control `--permission-prompt-tool stdio`. (#201)
- **Worker**: `permission_auto_approve` list for silent tool approval (default: ExitPlanMode). (#201)
- **Messaging**: Slack stream rotation — proactive 6min wallclock rotation aligned with Feishu pattern, eliminating Stream expired fallback. (#211)

### Changed

- **Messaging**: Interaction chain hardening — close active stream writer before sending interaction events (Slack/Feishu), strip nested backticks from args preview, fallback candidate matching for lost request IDs. (#201)
- **Messaging**: Feishu permission text matching expanded vocabulary (added "同意", "ok", "好的", "确认", "取消" etc.).
- **Messaging**: Turn summary refactored — extracted clampContextPct/tokenPair helpers, renamed Timer field.
- **Gateway Core**: WritePump, Hub.Run, and bridge w.Wait goroutine all now have panic recovery with graceful cleanup. (#221)
- **Gateway Core**: handleInput returns error on worker failure instead of silent nil. (#221)
- **Worker**: Pi-mono adapter removed — was a stub with no real implementation, cleaned from 27 files.

### Fixed

- **CLI**: Onboard generated `gateway.addr: ":8888"` binding to 0.0.0.0 — now correctly uses `localhost:8888`. (#203)
- **CLI**: Onboard generated `db.busy_timeout: 500ms` — now matches code default of `5s`.
- **CLI**: Onboard `stepBinaryInstall` copied test binary to PATH during `go test` — added `.test` suffix guard.
- **CLI**: Onboard hallucination audit — corrected Feishu Open ID lookup instructions, hot reload claims, and STT URL.
- **Session**: Data race in `ListActive` — missing RLock on SessionInfo field reads. (#220)
- **Session**: Silent error swallow in `TransitionWithInput` max-turns cleanup and `scanSession` unmarshal. (#220)
- **Worker**: Panic in `readOutput` goroutine without recovery (claudecode adapter). (#220)
- **Worker**: FD leak — stdin/stdout/stderr file descriptors not closed after process exit. (#220)
- **Event Store**: Nil map panic in `Collector.Capture` after `Close()`. (#220)

## [1.5.4] - 2026-05-06

### Summary

v1.5.4 是一次 patch 版本更新，聚焦于 **交互系统安全加固与数据完整性**。修复交互管道两级数据丢失 bug（Question/Elicitation 字段丢失 + OwnerID 未持久化）形成的连锁失败链，飞书/Slack 双平台增加 OwnerID 验证防止群聊未授权操作。Worker stdin 写入统一为单 mutex 序列化防止 NDJSON 交错。同时修复 OCS 响应安全限制、turn summary 显示问题、config audit 内存增长等多项 bug。

### Added

- **Messaging/Slack**: Text-based interaction fallback — 当 Block Kit 按钮不可用时，用户可通过文本（"allow/deny/accept/decline \<requestID\>"）响应交互。(#197)
- **Messaging/Feishu**: Card 发送失败自动回退纯文本 — 与 Slack invalid_blocks fallback 对齐。(#197)

### Changed

- **Config**: STT 模式自动推断 — 移除 `stt_local_mode` 配置项，由 `stt_local_cmd` 中是否包含 `{file}` 占位符自动判断 ephemeral/persistent 模式。(#186)
- **Messaging**: Turn Summary 共享 helper 提取 — `FormatDurationParts`/`FormatTokenUsage` 统一至 turn_summary.go，消除飞书/Slack 重复格式化代码。(#194)
- **Messaging/Feishu**: Turn Summary 改用专用卡片格式，cooldown 检查使用 atomic CAS 消除 TOCTOU 竞态。(#194)
- **Client/SDK**: `decodeAs` 迁移至 `events.DecodeAs[T]` — 支持泛型类型透传，删除冗余测试。(#178)

### Fixed

- **Messaging**: 交互管道数据丢失 — `ExtractQuestionData`/`ExtractElicitationData` 在 `map[string]any` 分支丢失 `questions` 字段，改用 `events.DecodeAs[T]` 修复。(#195)
- **Session**: OwnerID 未持久化 — `upsert_session` SQL ON CONFLICT 不更新 `owner_id`，导致 session resume 后交互响应静默失败。(#196)
- **Gateway Core**: OwnerID 未注入 Worker 信封 — worker 发出的 permission/question/elicitation envelope 缺少 OwnerID，交互响应无法路由。(#191)
- **Gateway Core**: `handleCD` 路径提取错误 — `map[string]any` 分支读取 `d["path"]` 而非 `d["details"]["path"]`。(#197)
- **Gateway Core**: TruncatePath 显示误导 — 含隐藏目录的深路径截断显示 `/.hotplex/workspace` 而非 `~/.hotplex/workspace`，增加 home 目录替换。(#185)
- **Gateway Core**: Detached HEAD 显示空分支 — 改为显示短 commit hash。(#194)
- **Worker/OCS**: 4 处未限制的错误响应读取 — 添加 `io.LimitReader(resp.Body, 4096)` 防止 OOM，URL path segment 使用 `url.PathEscape` 防止注入。(#145)
- **Worker/ClaudeCode**: Stdin 写入交错 — `Conn.Send`/`SendUserMessage` 改为持锁写入，`ControlHandler` 共享同一 mutex 统一序列化。(#197)
- **Messaging**: CancelAll 后 timeout goroutine 复活 — 新增 `cancelCh` 机制，`CancelAll` 关闭 channel 停止 `watchTimeout`。(#197)
- **CLI**: `loadEnvFile` 重复写入 + 受保护变量过滤 — `.env` 重复 key 消除，跳过 HOME/PATH 等受保护变量。(#150)
- **Config**: Audit log 无限增长 — 添加 `maxAuditLen(256)` 上限，static callbacks 合并为单 goroutine 触发。(#146)

### Security

- **Messaging**: 飞书/Slack 交互响应 OwnerID 验证 — 飞书 `checkPendingInteraction` 增加 userID 参数，Slack `handleInteractionEvent` 改为 Get→验证→Complete 防止 TOCTOU 竞态。(#197)
- **Session**: Admin 所有权绕过增加审计日志 — 记录 admin_user_id、session_id、session_owner。(#143)

## [1.5.3] - 2026-05-05

### Summary

v1.5.3 是一次 patch 版本更新，聚焦于 **消息通道稳定性** 和 **EventStore 架构简化**。修复 Slack 流式输出死锁、消息静默丢弃、DM 线程丢失三个高频问题。EventStore 合并至共享数据库消除双文件运维负担，Turn Summary 统一为 Slack/飞书共享的多行卡片布局。同时修复安全路径穿越、Worker 控制请求映射、per-turn token 统计等多项 bug。

### Added

- **Gateway Core**: `GatewayEventsNoSubscribersDropped` Prometheus counter — 追踪因无订阅连接而静默丢弃的事件，提升消息丢失可观测性。(#182)
- **STT**: Auto-patch ONNX models — 首次加载时自动修补模型文件，添加 `.patched` marker 避免重复修补。(#182)
- **STT**: Audio STT failure fallback — STT 失败时下载音频到磁盘并提示 Worker 使用 `stt_once.py` 转录，防止音频内容静默丢失。(#182)

### Changed

- **EventStore**: Events 表合并至 session 共享数据库 — 新增 goose migration 002/003，`source` 列追踪事件来源（normal/crash/timeout/fresh_start），`v_turns` SQL VIEW 替代 ConversationStore。(#171)
- **Messaging**: Turn Summary 统一为 Slack/飞书共享的多行卡片布局 — 导出 `FormatDuration`/`FormatToolNames` 共享 helper，消除 47 行 Slack 重复代码。(#181)
- **Gateway Core**: Bridge 拆分 `captureEvent`/`CaptureInbound` 使用 `json.RawMessage` 减少重复序列化，`events.DecodeAs[T]` 泛型 helper 替代 6 处 map→struct JSON round-trip。(#176)
- **Slack**: Streaming start 使用首个 Write payload 作为初始内容，移除 "Thinking..." 占位符。(#182)

### Fixed

- **Slack**: `closeStreamWriter` 不可重入死锁 — `sync.Mutex` 持锁调用 `Close()` 触发 `onComplete` 回调再次获锁，导致所有后续 turn 永久阻塞。(#182)
- **Slack**: `flushBuffer` 在 `w.closed=true` 后跳过最终 flush — 移除 closed 检查，确保退出时缓冲内容完整发送。(#182)
- **Slack**: `appendWithRetry` 无限阻塞 — 添加 per-call 10s 超时防止 Slack API 无响应时 Close() 永久挂起。(#182)
- **Slack**: DM 流式输出失败 — `StartStreamContext` 缺少 `thread_ts` 和 `team_id` 导致 DM 消息 API 报错。(#174)
- **Slack**: Fallback PostMessage 缺少 `thread_ts` — 非流式回退消息发到 DM 顶层而非线程内。(#182)
- **Gateway Core**: `JoinPlatformSession` 死连接未检测 — `pcEntry` writeLoop 退出后事件仍被静默丢弃，新增 `select on done` 检测并替换。(#182)
- **Gateway Core**: Worker cold-start 计入 Turn 1 时长 — `turnStartTime` 改为 firstEvent 时重置，排除 worker 启动开销。(#176)
- **Gateway Core**: Per-turn token delta 与累计总量相同 — `resetPerTurn()` 被标记为死代码未调用，Done 后补充调用修复。(#176)
- **Gateway Core**: `pendingError` 双重 JSON 编码 — captureEvent 内部已 Marshal，调用方重复 Marshal 导致数据损坏。(#176)
- **Worker/ClaudeCode**: `HasSessionFiles` 空目录误判 — 空 `session-env/<id>` 目录触发无效 resume 浪费 ~14s。(#173)
- **Worker**: `control_request` 字段映射错误 — SDK 使用 "request" 非 "response"，导致 `can_use_tool` 阻塞无限等待。(#170)
- **Security**: Slack/飞书媒体处理路径穿越 — `filepath.Clean` 前缀检查和 `filepath.Base` 文件名清洗。(#167)
- **WebChat**: `message.parts` 为 null 时 filter/find 崩溃 — 新增 null guard 防止反序列化异常。(#177)

## [1.5.2] - 2026-05-04

### Summary

v1.5.2 是一次 patch 版本更新，聚焦于 **安全加固** 和 **交互授权链修复**。修复 REST API 所有权绕过、JWT 弱密钥接受、Admin IP 欺骗和时序侧信道攻击四个安全漏洞。交互授权链全路径 5 个断裂点全部修复，Slack/飞书/WebChat 用户现在可正常响应 Permission/Question/Elicitation 请求。同时新增 Slack delete-file CLI 命令和 Block Kit 表格渲染，WebChat 和 EventStore 获得性能优化。

### Added

- **Messaging/Slack**: `hotplex slack delete-file --file-id <F_FILE_ID>` 命令 — 调用 Slack files.delete API，支持 AI agent 清理过时上传文件。(#164)
- **Messaging/Slack**: Markdown 表格渲染为 Block Kit TableBlock — 流式输出结束后自动升级为表格+Markdown 混合布局，header-only/列不匹配等边界安全降级为代码块。(#148)

### Changed

- **Gateway Core**: bridge.go 分解为 4 个职责文件（bridge/bridge_worker/bridge_forward/bridge_retry），SessionManager 拆分为 5 个子接口（ISP），提升可读性和可测试性。(#153)
- **WebChat**: delta rAF 批处理、React.memo 非流式消息跳过重渲染、highlight.js 懒加载（~200KB → 按需 8 语言）。(#141)
- **EventStore**: delta 优化 — 捕获 seq 后的快速路径、三触发器 flush（大小/计时器/事件结束）、LLM 重试时丢弃过期 accumulator。(#153)

### Fixed

- **Gateway**: 交互授权链全路径修复 — 添加 `--permission-prompt-tool stdio` 到 Claude Code Worker 参数、修复 OwnerID 空值、替换静默错误丢弃为结构化日志、handleInput metadata 路由短路、WebChat 新增 Question/Elicitation 事件类型。(#160)
- **Gateway**: SIGTERM（exit code 143）误报为用户可见错误 — 正常退出/bridge 发起的终止改为 debug 日志。(#160)
- **Messaging/Slack**: Markdown 表格在 header-only 或列不匹配时触发 OOB panic。(#148)
- **Messaging/Slack**: joinSegments 末尾多余空白 — 未修剪换行符导致消息间距异常。

### Security

- **API**: GetSession/DeleteSession 端点缺失所有权检查 — 任何已认证用户可访问/删除他人 session。ListSessions limit 上限 500 防止资源滥用。(#155)
- **Security**: JWT secret 弱密钥检查 — 拒绝非 32 字节密钥，config audit 日志脱敏 api_keys/jwt_secret 字段。(#156)
- **Admin**: X-Forwarded-For 无条件信任导致 IP 白名单绕过 — admin token 验证改用常量时间比较消除时序侧信道。(#149)

## [1.5.1] - 2026-05-04

### Summary

v1.5.1 是一次 patch 版本更新，修复 v1.5.0 引入的 **agent 配置注入丢失** 和 **.env 路径展开失败** 两个关键问题。Bridge 闭包捕获导致 agent config SystemPrompt 在 worker Start/Resume 时被静默丢弃；Viper env override 绕过路径规范化导致 `~` 前缀路径无法解析。同时修复了 dev.sh 与 gateway PID 文件 JSON 格式不兼容导致的 `make dev` 启动失败。

### Fixed

- **Gateway**: Agent config SystemPrompt 在 worker Start/Resume 时被静默丢弃 — `workerStartFunc` 闭包捕获了 injection 前的 `workerInfo` 副本，注入的 SystemPrompt 未传递到 worker 进程。(#139)
- **Configuration**: `.env` 中 `~` 前缀路径（如 `~/.hotplex/...`）未展开 — Viper env override 在路径规范化之后写入原始值，导致 agent config、STT 等目录解析失败。(#138)
- **Configuration**: Worker 和 STT 环境变量通过 `.env` 设置时被静默忽略 — 缺少 `BindEnv` 调用导致 `claude_code.command`、`opencode_server.*` 和 `messaging.*.stt_*` 字段无法从环境变量读取。(#138)
- **Agent Config**: Agent config 启用但无对应文件时无告警 — 新增 `HasGlobalFiles()` 检测和 `agentConfigGlobalFilesChecker` 诊断检查。(#138)
- **Dev Tooling**: `make dev` 启动失败 — gateway PID 文件升级为 JSON 格式后 dev.sh `cat` 读取失败，误判进程已死。新增 `read_pid()` 兼容两种格式。

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
