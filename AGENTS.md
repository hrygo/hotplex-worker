# PROJECT KNOWLEDGE BASE

**Last updated:** 2026-04-25 · **Commit:** fcedf5cd · **Branch:** feat/28-premium-ux-sdk-integration

## OVERVIEW

HotPlex Worker Gateway — Go 1.26 unified access layer for AI Coding Agent sessions.
WebSocket gateway (AEP v1) abstracting Claude Code, OpenCode Server, Pi-mono protocol differences.
Multi-language client SDKs (TS, Python, Java, Go) + AI SDK transport adapter + web chat UI + bidirectional messaging (Slack/Feishu).

## ENVIRONMENT

**Setup** (first time):
```bash
cp configs/env.example .env
# edit .env with your API keys
```

**Dev** (`make dev`):
- Gateway → http://localhost:8888
- Webchat → http://localhost:3000
- Admin API → http://localhost:9999

**Logs**: `./logs/` · **PIDs**: `~/.hotplex/.pids/`

## STRUCTURE

### Entry
```
cmd/hotplex/main.go          (~54 lines)  cobra CLI root: gateway, doctor, security, onboard, version
cmd/hotplex/gateway_run.go   (~393 lines) gateway run: DI, signal handler, hub/session/bridge setup, HTTP routes
cmd/hotplex/serve.go         (~110 lines) serve subcommand: flags, config loading, deps orchestration
cmd/hotplex/routes.go        (~197 lines) HTTP route registration: gateway WS, admin API, health, metrics
cmd/hotplex/messaging_init.go (~233 lines) messaging adapter lifecycle: init Slack/Feishu, STT setup
cmd/hotplex/doctor.go        (~150 lines) doctor subcommand: diagnostic checks via CLI checker registry
cmd/hotplex/security.go      (~182 lines) security subcommand: path/env validation
cmd/hotplex/onboard.go       (~105 lines) onboard subcommand: interactive setup wizard
cmd/hotplex/config_cmd.go    (~61 lines)  config subcommand: validate/dump/show
cmd/hotplex/status.go        (~95 lines)  status subcommand: gateway process status
cmd/hotplex/banner.go        (~167 lines) startup banner rendering (ASCII art + config summary)
cmd/hotplex/dev.go           (~29 lines)  dev subcommand: start gateway + webchat
cmd/hotplex/pid.go           (~50 lines)  PID file helpers for gateway process management
cmd/hotplex/version.go       (~46 lines)  version subcommand
```

### internal/

**Core**
- `admin/`      Admin API: handlers, middleware, rate-limit, log buffer
- `aep/`        AEP v1 codec: JSON envelope encode/decode/validate
- `config/`     Viper config + file watcher + hot-reload
- `agentconfig/` Agent personality/context loader: B-channel (system prompt) + C-channel (rules injection)

**Gateway** (WebSocket)
- `gateway/hub.go`     WS broadcast hub: conn registry, session routing, seq gen
- `gateway/conn.go`    Single WS connection: read/write pumps, heartbeat
- `gateway/handler.go`  AEP event dispatch (input, ping, control)
- `gateway/bridge.go`  Session ↔ worker lifecycle orchestration + LLM retry + agent config injection
- `gateway/llm_retry.go`  LLMRetryController: exponential backoff on retryable errors
- `gateway/api.go`     GatewayAPI: HTTP session endpoints (list/get/terminate/create with idempotency)
- `gateway/init.go`    Init handshake: InitData, InitAckData, caps, 30s timeout
- `gateway/heartbeat.go` Missed ping counter with stop channel
- `gateway/session_stats.go` Session statistics tracking

**Session**
- `session/manager.go`   5-state machine, state transitions, GC, physical delete
- `session/store.go`     SQLite persistence (Upsert, Get, List, expired, DeletePhysical)
- `session/message_store.go`  Event log, single-writer goroutine
- `session/key.go`       DeriveSessionKey (UUIDv5) + PlatformContext for deterministic session IDs
- `session/pool.go`      PoolManager: global + per-user quota + per-user memory tracking
- `session/pgstore.go`   Postgres stub (ErrNotImplemented)
- `session/sql/`         Externalized .sql files (schema, migrations, queries)
- `session/queries.go`  embed.FS loader + stripComments
- `session/stores.go`   Multi-store registry (SQLite/Postgres)

**Messaging** (Slack/Feishu bidirectional)
- `messaging/bridge.go`   SessionStarter + ConnFactory + joined dedup (3-step: StartSession → Join → Handle)
- `messaging/platform_conn.go`  PlatformConn interface: WriteCtx + Close
- `messaging/platform_adapter.go`  Base adapter + self-registration (Register/New/RegisteredTypes)
- `messaging/interaction.go`  InteractionManager: user permission/Q&A/elicitation with timeout + auto-deny
- `messaging/control_command.go`  Slash commands + natural language control triggers ($prefix)
- `messaging/sanitize.go`  Text sanitization: control chars, null bytes, BOM, surrogates
- `messaging/slack/`      Socket Mode: streaming writer, chunker, dedup, validator, interaction, backoff, slash commands, image blocks, status
- `messaging/feishu/`     ws.Client: P2 events, converter, streaming, typing, interaction cards, stt (speech-to-text)
- `scripts/stt_server.py`  Persistent STT subprocess (SenseVoice-Small ONNX)
- `scripts/fix_onnx_model.py`  ONNX model Less node type mismatch auto-patch
- `messaging/mock/`       Mock adapter for testing

**Worker** (3 runtime adapters + 1 noop)
- `worker/claudecode/`    Claude Code adapter
- `worker/opencodeserver/`  OpenCode Server adapter (singleton process via `SingletonProcessManager`)
- `worker/pi/`            Pi-mono adapter
- `worker/noop/`          No-op adapter (testing)
- `worker/acpx/`          ACPX type constant only (no implementation)
- `worker/base/`          Shared BaseWorker + Conn + BuildEnv
- `worker/proc/`          Process lifecycle: PGID isolation, layered SIGTERM→SIGKILL, PID file orphan cleanup

**CLI** (Self-service commands — see `internal/cli/AGENTS.md`)
- `cli/checker.go`       Checker interface + CheckerRegistry for diagnostic checks
- `cli/checkers/`        7 checkers: config, dependencies, environment, messaging, runtime, security, stt
- `cli/onboard/`         Interactive wizard + YAML templates for Slack/Feishu setup
- `cli/output/`          Terminal output: printer (color/format), report (structured diagnostic results)

**Support**
- `security/`   JWT (ES256), SSRF, command whitelist, env isolation, path safety
- `metrics/`    Prometheus counters/gauges/histograms
- `tracing/`    OpenTelemetry setup (idempotent)

### pkg/
- `events/`   Envelope, Event, SessionState, all data structs
- `events/helpers.go`  Shared mapper helpers for event data extraction
- `aep/`      AEP v1 codec

### Top-level
```
client/    Go client SDK (standalone module, typed events)
webchat/  Next.js web chat UI + AI SDK transport
examples/  TS / Python / Java client SDKs
docs/     Architecture, specs, security, testing, management
scripts/  Build/validation scripts
configs/  config.yaml, config-dev.yaml, env.example
```

## WHERE TO LOOK

**Add new components**
- New AEP event type → `pkg/events/events.go` — add Kind const + Data struct + Validate
- New Worker adapter → `internal/worker/<name>/` — embed `base.BaseWorker`, implement `Start`/`Input`/`Resume`, register in `init()`
- New messaging adapter → `internal/messaging/<name>/` — embed `PlatformAdapter`, implement `Start`/`HandleTextMessage`/`Close`
- Add diagnostic check → `internal/cli/checkers/` — implement `Checker` interface, register in `DefaultRegistry`
- New cobra subcommand → `cmd/hotplex/<name>.go` — register in `main.go` root cmd
- New admin endpoint → `internal/admin/handlers.go` — follow `Handle*` pattern, check scopes

**CLI self-service** (see `internal/cli/AGENTS.md` and `cmd/hotplex/AGENTS.md`)
- Modify onboard wizard → `internal/cli/onboard/wizard.go` — interactive prompts and templates
- CLI output formatting → `internal/cli/output/` — printer (color/status) and report (structured output)
- Gateway startup/DI → `cmd/hotplex/gateway_run.go` — DI container, signal handler, hub/session/bridge setup, OCS singleton init
- Messaging adapter wiring → `cmd/hotplex/messaging_init.go` — init Slack/Feishu, STT setup
- Route registration → `cmd/hotplex/routes.go` — HTTP routes for gateway WS, admin API, health, metrics

**Modify existing**
- Agent config files → `internal/agentconfig/loader.go` — file loading, size limits, frontmatter stripping; `prompt.go` for unified system prompt assembly (nested XML: `<directives>` + `<context>` groups with per-section behavioral directives)
- Agent config directory → `~/.hotplex/agent-configs/` — place SOUL.md, AGENTS.md, SKILLS.md (B-channel) + USER.md, MEMORY.md (C-channel); platform variants like SOUL.slack.md
- Session lifecycle → `internal/session/manager.go` — state machine + `TransitionWithInput` atomicity + `DeletePhysical` for forced removal
- Session key derivation → `internal/session/key.go` — UUIDv5 deterministic session IDs + platform context
- WebSocket protocol → `internal/gateway/conn.go` — ReadPump/WritePump + Handler dispatch
- LLM auto-retry → `internal/gateway/llm_retry.go` — retryable error detection + exponential backoff
- Gateway HTTP API → `internal/gateway/api.go` — session list/get/terminate/create over HTTP (CreateSession has idempotency: reuses active sessions, physically deletes deleted ones)
- Config structure → `internal/config/config.go` — structs + Default() + Validate() (includes `AgentConfig` for agent personality/context loading)
- Agent config injection → `internal/gateway/bridge.go` — `injectAgentConfig()` loads configs and applies B/C channels per worker type at Start/Resume/Fresh-start
- STT config → `internal/config/config.go` — FeishuConfig.STTProvider/STTLocalCmd/STTLocalMode/STTLocalIdleTTL + SlackConfig.STTProvider/STTLocalCmd/STTLocalMode/STTLocalIdleTTL
- Wire messaging adapter → `cmd/hotplex/serve.go` — `startMessagingAdapters()`: config → New → Configure → SetConnFactory → Start

**Security**
- Add validation → `internal/security/` — one file per concern (jwt, ssrf, path, env, tool, command)

**Monitoring & API**
- Prometheus metric → `internal/metrics/` — follow `hotplex_<group>_<metric>_<unit>` naming
- Admin endpoint → `internal/admin/` — handlers.go (stats/health/config), sessions.go (CRUD)

## CODE MAP

**Entry**
- `main` → `cmd/hotplex/main.go` — cobra CLI root (gateway, doctor, security, onboard, version)
- `GatewayDeps` → `cmd/hotplex/serve.go` — gateway DI container, signal handler, messaging init, LLM retry init

**Gateway** (`internal/gateway/`)
- `Hub` → `hub.go:68` — WS broadcast hub, conn registry, session routing, seq gen
- `Conn` → `conn.go:35` — single WS connection, read/write pumps, heartbeat
- `Handler` → `handler.go` — AEP event dispatch (input, ping, control, /cd workdir switch) + panic recovery
- `Bridge` → `bridge.go` — session ↔ worker lifecycle, StartPlatformSession, fresh start fallback, InputRecoverer, LLM retry integration, agent config injection, SwitchWorkDir
- `LLMRetryController` → `llm_retry.go` — retryable error pattern detection, per-session attempt tracking, exponential backoff
- `GatewayAPI` → `api.go` — HTTP session endpoints: ListSessions, GetSession, TerminateSession, CreateSession (idempotent with DeletePhysical fallback)
- `pcEntry` → `hub.go` — wraps PlatformConn for sessions map

**Session** (`internal/session/`)
- `Manager` → `manager.go:34` — 5-state machine, transitions, GC, worker attach/detach, `DeletePhysical` for forced removal bypassing state machine
- `managedSession` → `manager.go:54` — per-session state + mutex + worker ref
- `DeriveSessionKey` → `key.go` — UUIDv5 deterministic session ID from (ownerID, workerType, clientSessionID, workDir)
- `PlatformContext` → `key.go` — platform-specific fields for DerivePlatformSessionKey (Slack channel/thread, Feishu chat)
- `PoolManager` → `pool.go` — global + per-user quota, per-user memory tracking (512MB per worker estimate)
- `Store` (interface) → `store.go:22` — SQLite: Upsert, Get, List, expired queries, DeletePhysical
- `MessageStore` (interface) → `message_store.go` — event log, single-writer goroutine

**Worker** (`internal/worker/`)
- `Worker` (interface) → `worker.go:84` — Start/Input/Resume/Terminate/Kill/Wait/Conn/Health
- `SessionConn` (interface) → `worker.go:19` — bidirectional channel: Send/Recv/Close
- `Capabilities` (interface) → `worker.go:40` — feature query: resume, streaming, tools, env
- `InputRecoverer` (interface) → `worker.go:141` — LastInput() for crash recovery input re-delivery
- `base.BaseWorker` → `base/worker.go` — shared lifecycle: Terminate/Kill/Wait/Health/LastIO
- `base.Conn` → `base/conn.go` — stdin SessionConn: NDJSON over stdio, exported `WriteAll`, implements `InputRecoverer`
- `base.BuildEnv` → `base/env.go` — env construction: whitelist + session vars
- `proc.Manager` → `proc/manager.go:26` — PGID isolation, layered SIGTERM→SIGKILL
- `proc.Tracker` → `proc/pidfile.go` — PID file orphan cleanup: Write/Remove/RemoveAll/CleanupOrphans, globalTracker, PID recycling defense

**OpenCode Server** (`internal/worker/opencodeserver/`)
- `SingletonProcessManager` → `singleton.go` — lazy-started shared `opencode serve` process with ref counting, 30m idle drain, crash detection
- `Worker` → `worker.go` — thin session adapter; Start/Resume acquire singleton ref, Terminate/Kill only release ref + close SSE (not process)
- `InitSingleton` / `ShutdownSingleton` → `singleton.go` — gateway lifecycle hooks for the global singleton

**Messaging** (`internal/messaging/`)
- `Bridge` → `bridge.go` — 3-step: StartSession → Join → handler.Handle
- `PlatformConn` (interface) → `platform_conn.go` — WriteCtx + Close
- `PlatformAdapter` → `platform_adapter.go` — base: SetHub/SetSM/SetHandler/SetBridge
- `InteractionManager` → `interaction.go` — PendingInteraction registry with timeout + auto-deny (5min default)
- `ParseControlCommand` → `control_command.go` — slash commands (/gc, /reset, /park, /new, /cd) + $prefix natural language
- `SanitizeText` → `sanitize.go` — removes control chars, null bytes, BOM, surrogates
- `FeishuSTT` → `feishu/stt.go` — cloud transcription via Feishu speech_to_text API
- `LocalSTT` → `stt/stt.go` — ephemeral per-request external command transcription
- `PersistentSTT` → `stt/stt.go` — long-lived subprocess, JSON-over-stdio, PGID isolation
- `FallbackSTT` → `stt/stt.go` — primary + secondary fallback chain
- `Transcriber` (interface) → `stt/stt.go` — Transcribe(ctx, audioData) → (text, error), shared by Feishu and Slack
- `PlatformAdapterInterface` → `platform_adapter.go:21` — Platform/Start/HandleTextMessage/Close
- Adapter registration → `platform_adapter.go:47` — `Register(t PlatformType, b Builder)`, blank import in main.go

**Agent Config** (`internal/agentconfig/`)
- `AgentConfigs` → `loader.go` — holds loaded content: Soul/Agents/Skills (B-channel) + User/Memory (C-channel)
- `Load` → `loader.go` — reads config dir, appends platform variants (e.g. SOUL.slack.md), strips YAML frontmatter, enforces size limits (12K/file, 60K total)
- `BuildSystemPrompt` → `prompt.go` — assembles unified B+C system prompt with nested XML tags (`<directives>/<context>`) for both CC and OCS

**Core**
- `Envelope` → `pkg/events/events.go:73` — AEP v1 envelope (id, version, seq, session_id, event)
- `SessionState` → `pkg/events/events.go:240` — Created/Running/Idle/Terminated/Deleted
- `Config` → `config/config.go:118` — all config structs
- `JWTValidator` → `security/jwt.go:27` — ES256 + JTI blacklist
- `client.Client` → `client/client.go:33` — Go SDK: Connect/Resume/SendInput/SendPermissionResponse/SendControl/Close
- `client.Event` → `client/events.go` — typed event constants + data helpers (AsDoneData, AsErrorData, AsToolCallData)
- `client.Option` → `client/options.go` — functional options (AutoReconnect, ClientSessionID, Metadata, Logger)
- `admin.AdminAPI` → `admin/admin.go` — stats, health, config, session CRUD

## CONVENTIONS

- **Mutex**: Explicit `mu` field, zero-value, no embedding, no pointer passing
- **Errors**: `Err` prefix for sentinel vars, `Error` suffix for custom types, `fmt.Errorf("%w", ...)` for wrapping
- **Logging**: `log/slog` JSON handler, key-value pairs, `service.name=hotplex-gateway`
- **Testing**: `testify/require` (not `t.Fatal`), table-driven, `t.Parallel()`, `t.Cleanup()`
- **Config**: Viper YAML + env expansion, `SecretsProvider` interface for secrets
- **Worker registration**: `init()` + `worker.Register(WorkerType, Builder)` pattern via blank imports
- **STT engine**: SenseVoice-Small via `funasr-onnx` (ONNX FP32, non-quantized), auto-patches ONNX model on first load, persistent subprocess for zero cold-start
- **DI**: Manual constructor injection (no wire/dig), `GatewayDeps` struct in serve.go
- **Shutdown order**: signal → cancel ctx → tracing → hub → configWatcher → sessionMgr → HTTP server
- **Panic recovery**: Gateway handler + bridge forwardEvents must recover panics, log error, return `handler panic` / `bridge panic` to caller
- **Control commands**: Natural language triggers require `$` prefix (e.g. `$gc`, `$休眠`) to prevent accidental matches; slash commands (`/gc`, `/reset`, `/park`, `/new`, `/cd <path>`) have no prefix
- **Text sanitization**: All user-facing text output passes through `SanitizeText()` before delivery to messaging platforms
- **Interaction timeout**: Permission/Q&A/elicitation requests auto-deny after 5 minutes to prevent indefinite blocking
- **Session key derivation**: UUIDv5 deterministic mapping from (ownerID, workerType, clientSessionID, workDir) for cross-environment consistency
- **LLM auto-retry**: Configurable retryable error patterns (429, 5xx, network errors) with exponential backoff; per-session attempt tracking
- **Agent config injection**: `agentconfig` package loads personality/context from `~/.hotplex/agent-configs/`; B-channel (SOUL.md, AGENTS.md, SKILLS.md) in `<directives>` XML group; C-channel (USER.md, MEMORY.md) in `<context>` XML group; platform variants (e.g. SOUL.slack.md) appended automatically; size limits: 8K/file, 40K total
- **Session physical delete**: `DeletePhysical` bypasses state machine for forced removal — used by GatewayAPI for idempotent session creation when previous session is in `deleted` state
- **Documentation**: 增量文档中文优先，重要文档中英双语。技术术语保留英文原文。增量文档（Issue/PR 模板、配置说明、changelog）用中文；重要文档（根 README、架构设计、协议规范）拆分为独立的中英文文件（如 `README.md` + `README_zh.md`），文件头部互相链接跳转
- **File safety (multi-agent)**: 当前环境存在多 Agent 协同工作，对文件执行还原（`git restore`）、恢复、撤销（`git checkout`）、暂存（`git stash`）等操作前，**必须先在 `/tmp` 下创建备份**（`cp <file> /tmp/<file>.bak.$(date +%s)`），防止其他 Agent 的未提交改动被意外覆盖或丢失

## ANTI-PATTERNS (THIS PROJECT)

- ❌ `sync.Mutex` embedding or pointer passing — always explicit `mu` field
- ❌ Multi-statement SQL in single `db.Exec()` — SQLite driver silently ignores all but the first; split into individual `db.Exec()` calls
- ❌ `math/rand` for crypto (JTI, tokens) — use `crypto/rand`
- ❌ Shell execution — only `claude` binary, no shell interpreters
- ❌ Non-ES256 JWT algorithms
- ❌ Missing goroutine shutdown path — every goroutine needs ctx cancel / channel close / WaitGroup
- ❌ `t.Fatal` in tests — use `testify/require`
- ❌ Skipping WAL mode for SQLite
- ❌ Cross-Bot session access
- ❌ Processing `done` + `input` without mutex — must be atomic in `TransitionWithInput`
- ❌ ACPX worker type registered without implementation — directory is empty, only type constant exists

## UNIQUE STYLES

- **Lock ordering**: `m.mu` (Manager) → `ms.mu` (per-session) — always in this order to prevent deadlock
- **Backpressure**: `message.delta` and `raw` events silently dropped when broadcast channel full; `state`/`done`/`error` never dropped
- **Seq allocation**: Per-session atomic monotonic counter; dropped deltas don't consume seq
- **Process termination**: 3-layer: SIGTERM → wait 5s → SIGKILL, PGID isolation for child cleanup
- **Worker types as constants**: `TypeClaudeCode`, `TypeOpenCodeSrv`, `TypeACPX`, `TypePimon`
- **BaseWorker embedding**: Adapters embed `*base.BaseWorker` for shared lifecycle; each adapter implements only `Start`, `Input`, `Resume` + unique I/O parsing
- **Admin API extracted to package**: `internal/admin/` with interfaces for SessionManager/Hub/Bridge to avoid circular imports; adapters in main.go bridge concrete types
- **Gateway split**: conn.go (WebSocket lifecycle), handler.go (AEP dispatch), bridge.go (session orchestration), llm_retry.go (auto-retry), api.go (HTTP endpoints) — same package, separate concerns
- **Config hot-reload**: File watcher with rollback capability, updates live config reference
- **Single-writer SQLite**: Channel-based write serialization with batch flush (50 items / 100ms)
- **InputRecoverer**: Workers implement `LastInput() string` via base.Conn; bridge extracts last input from dead worker for crash recovery re-delivery
- **Fresh start fallback**: When resume fails after retry, bridge creates a fresh worker and re-delivers the last input — conversation history is lost but user gets a response
- **Feishu streaming card 4-layer defense**: TTL guard → integrity check → retry with backoff → IM Patch fallback for degraded CardKit
- **Slack message pipeline**: chunker (split long messages) → dedup (TTL-based duplicate filter) → format (markdown conversion) → rate limiter → send
- **Slack streaming**: SlackStreamingWriter with 150ms flush interval, 20-rune threshold, max 3 append retries, 10min TTL
- **LLM auto-retry**: LLMRetryController detects retryable errors via regex patterns (429/5xx/network), exponential backoff (initial 2s, max 60s), per-session attempt counter
- **Deterministic session IDs**: DeriveSessionKey uses UUIDv5 (SHA-1 namespace+name) for cross-environment consistency; PlatformContext for platform-specific key derivation
- **Per-user memory tracking**: PoolManager tracks estimated memory per user (512MB/worker) alongside session count quotas
- **Agent config unified prompt**: B+C channels merged into single `BuildSystemPrompt` with nested XML tags (`<agent-configuration>` → `<directives>` + `<context>`); each section has a 1-line behavioral directive; both CC (`--append-system-prompt`) and OCS (`system` field) use identical structure injected via `bridge.injectAgentConfig`; no file-based injection, no hedging
- **Webchat session stickiness**: Deterministic "main" session ID via DeriveSessionKey + localStorage persistence for active session across page reloads; auto-creates first session when none exist
- **OCS singleton process**: All OpenCode Server sessions share one lazily-started `opencode serve` process managed by `SingletonProcessManager`; ref-counted with 30m idle drain; Workers are thin adapters that acquire/release refs; Terminate/Kill only close SSE connections, never the shared process; crash detected via `monitorProcess` goroutine, new `crashCh` created per lifecycle
- **Switch-workdir**: `/cd <path>` (WebSocket control) or `POST /api/sessions/{id}/cd` (REST) terminates old worker, derives new session ID via PlatformContext with new workDir, starts fresh session on the same singleton process; security validated via `config.ExpandAndAbs` + `security.ValidateWorkDir`

## COMMANDS

All build/test/lint operations MUST use `make` targets. Do NOT use raw `go build` / `go test` / `golangci-lint` directly.

```bash
make build                    # Build gateway binary (output: bin/hotplex-<os>-<arch>)
make test                     # Run tests with -race (timeout 15m)
make test-short               # Quick test pass (-short)
make lint                     # golangci-lint
make coverage                 # Coverage report
make check                    # Full CI workflow: quality + build
make quality                  # fmt + lint + test (no build)
make fmt                      # Format code (gofmt + goimports)
make clean                    # Clean build artifacts

# Development
make quickstart               # First-time setup (check-tools + build + test-short)
make run                      # Build and run gateway
make dev                      # Start dev environment (gateway + webchat)
make dev-stop                 # Stop all dev services
make dev-status               # Check running services
make dev-logs                 # Tail gateway logs
make dev-reset                # Stop and restart all services

# Gateway management
make gateway-start            # Build and start gateway
make gateway-stop             # Stop gateway
make gateway-status           # Check gateway status
make gateway-logs             # Tail gateway logs

# Webchat
make webchat-dev              # Start webchat dev server
make webchat-stop             # Stop webchat dev server
```

## NOTES

- `configs/` config-dev.yaml / config-prod.yaml / config.yaml / env.example / grafana / monitoring
- `CLAUDE.md` is symlinked to `AGENTS.md` — edit AGENTS.md only, CLAUDE.md auto-follows
- `.claude` is symlinked to `.agent` — both directories exist
- No `api/` directory — project uses JSON over WebSocket, not protobuf
- Project targets POSIX only (PGID isolation requires `syscall.SysProcAttr{Setpgid: true}`)
- Largest files: `feishu/adapter.go` (1228), `slack/adapter.go` (1208), `opencodeserver/worker.go` (1011), `bridge.go` (860), `hub.go` (816), `manager.go` (825), `config.go` (783)
- STT scripts (`scripts/stt_server.py`, `scripts/fix_onnx_model.py`) are also deployed to `~/.agents/skills/audio-transcribe/scripts/` for Claude Code skill use
- STT model: `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall` (~900MB), ONNX FP32 non-quantized
- Zombie IO timeout default: 30 minutes (configurable via `worker.execution_timeout`); worker idle timeout default: 60 minutes (configurable via `worker.idle_timeout`)
- OpenCode CLI adapter removed — replaced by OpenCode Server adapter (singleton process model)
- OCS singleton config defaults: `idle_drain_period=30m`, `ready_timeout=10s`, `ready_poll_interval=200ms`, `http_timeout=30s` — configurable via `worker.opencode_server` in config.yaml
- Onboard wizard auto-generates OCS singleton config when `opencode_server` worker type is selected
- ACPX adapter has type constant (`TypeACPX`) but no implementation — `internal/worker/acpx/` is empty
- Postgres store is stub only (`ErrNotImplemented`) — only SQLite is production-ready
- `internal/gateway/api.go` provides REST session management alongside the WebSocket gateway
- Agent config files live in `~/.hotplex/agent-configs/` (configurable via `agent_config.config_dir`): SOUL.md, AGENTS.md, SKILLS.md (B-channel), USER.md, MEMORY.md (C-channel); platform variants like SOUL.slack.md auto-appended
- `DeletePhysical` in session.Manager bypasses state machine for forced removal — used when recreating sessions that were soft-deleted
