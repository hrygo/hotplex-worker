# PROJECT KNOWLEDGE BASE

**Last updated:** 2026-04-22 · **Commit:** 1b69bfd2 · **Branch:** feat/21-worker-stdio-session-control

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
cmd/worker/main.go    (~656 lines) flags, DI, signal, messaging init, LLM retry init
```

### internal/

**Core**
- `admin/`      Admin API: handlers, middleware, rate-limit, log buffer
- `aep/`        AEP v1 codec: JSON envelope encode/decode/validate
- `config/`     Viper config + file watcher + hot-reload

**Gateway** (WebSocket)
- `gateway/hub.go`     WS broadcast hub: conn registry, session routing, seq gen
- `gateway/conn.go`    Single WS connection: read/write pumps, heartbeat
- `gateway/handler.go`  AEP event dispatch (input, ping, control)
- `gateway/bridge.go`  Session ↔ worker lifecycle orchestration + LLM retry integration
- `gateway/llm_retry.go`  LLMRetryController: exponential backoff on retryable errors
- `gateway/api.go`     GatewayAPI: HTTP session endpoints (list/get/terminate)
- `gateway/init.go`    Init handshake: InitData, InitAckData, caps, 30s timeout
- `gateway/heartbeat.go` Missed ping counter with stop channel

**Session**
- `session/manager.go`   5-state machine, state transitions, GC
- `session/store.go`     SQLite persistence (Upsert, Get, List, expired)
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
- `worker/opencodeserver/`  OpenCode Server adapter
- `worker/pi/`            Pi-mono adapter
- `worker/noop/`          No-op adapter (testing)
- `worker/acpx/`          ACPX type constant only (no implementation)
- `worker/base/`          Shared BaseWorker + Conn + BuildEnv
- `worker/proc/`          Process lifecycle: PGID isolation, layered SIGTERM→SIGKILL, PID file orphan cleanup

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

**Modify existing**
- Session lifecycle → `internal/session/manager.go` — state machine + `TransitionWithInput` atomicity
- Session key derivation → `internal/session/key.go` — UUIDv5 deterministic session IDs + platform context
- WebSocket protocol → `internal/gateway/conn.go` — ReadPump/WritePump + Handler dispatch
- LLM auto-retry → `internal/gateway/llm_retry.go` — retryable error detection + exponential backoff
- Gateway HTTP API → `internal/gateway/api.go` — session list/get/terminate over HTTP
- Config structure → `internal/config/config.go` — structs + Default() + Validate()
- STT config → `internal/config/config.go` — FeishuConfig.STTProvider/STTLocalCmd/STTLocalMode/STTLocalIdleTTL
- Wire messaging adapter → `cmd/worker/main.go` — `startMessagingAdapters()`: config → New → Configure → SetConnFactory → Start

**Security**
- Add validation → `internal/security/` — one file per concern (jwt, ssrf, path, env, tool, command)

**Monitoring & API**
- Prometheus metric → `internal/metrics/` — follow `hotplex_<group>_<metric>_<unit>` naming
- Admin endpoint → `internal/admin/` — handlers.go (stats/health/config), sessions.go (CRUD)

## CODE MAP

**Entry**
- `main` / `GatewayDeps` → `cmd/worker/main.go` — entry point, DI container, LLM retry init

**Gateway** (`internal/gateway/`)
- `Hub` → `hub.go:57` — WS broadcast hub, conn registry, session routing, seq gen
- `Conn` → `conn.go:27` — single WS connection, read/write pumps, heartbeat
- `Handler` → `handler.go` — AEP event dispatch (input, ping, control) + panic recovery
- `Bridge` → `bridge.go` — session ↔ worker lifecycle, StartPlatformSession, fresh start fallback, InputRecoverer, LLM retry integration
- `LLMRetryController` → `llm_retry.go` — retryable error pattern detection, per-session attempt tracking, exponential backoff
- `GatewayAPI` → `api.go` — HTTP session endpoints: ListSessions, GetSession, TerminateSession
- `pcEntry` → `hub.go` — wraps PlatformConn for sessions map

**Session** (`internal/session/`)
- `Manager` → `manager.go:34` — 5-state machine, transitions, GC, worker attach/detach
- `managedSession` → `manager.go:52` — per-session state + mutex + worker ref
- `DeriveSessionKey` → `key.go` — UUIDv5 deterministic session ID from (ownerID, workerType, clientSessionID, workDir)
- `PlatformContext` → `key.go` — platform-specific fields for DerivePlatformSessionKey (Slack channel/thread, Feishu chat)
- `PoolManager` → `pool.go` — global + per-user quota, per-user memory tracking (512MB per worker estimate)
- `Store` (interface) → `store.go:22` — SQLite: Upsert, Get, List, expired queries
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

**Messaging** (`internal/messaging/`)
- `Bridge` → `bridge.go` — 3-step: StartSession → Join → handler.Handle
- `PlatformConn` (interface) → `platform_conn.go` — WriteCtx + Close
- `PlatformAdapter` → `platform_adapter.go` — base: SetHub/SetSM/SetHandler/SetBridge
- `InteractionManager` → `interaction.go` — PendingInteraction registry with timeout + auto-deny (5min default)
- `ParseControlCommand` → `control_command.go` — slash commands (/gc, /reset, /park, /restart, /new) + $prefix natural language
- `SanitizeText` → `sanitize.go` — removes control chars, null bytes, BOM, surrogates
- `FeishuSTT` → `feishu/stt.go:41` — cloud transcription via Feishu speech_to_text API
- `LocalSTT` → `feishu/stt.go:98` — ephemeral per-request external command transcription
- `PersistentSTT` → `feishu/stt.go:185` — long-lived subprocess, JSON-over-stdio, PGID isolation
- `FallbackSTT` → `feishu/stt.go:143` — primary + secondary fallback chain
- `Transcriber` (interface) → `feishu/stt.go:27` — Transcribe(ctx, audioData) → (text, error)
- `PlatformAdapterInterface` → `platform_adapter.go:21` — Platform/Start/HandleTextMessage/Close
- Adapter registration → `platform_adapter.go:47` — `Register(t PlatformType, b Builder)`, blank import in main.go

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
- **DI**: Manual constructor injection (no wire/dig), `GatewayDeps` struct in main.go
- **Shutdown order**: signal → cancel ctx → tracing → hub → configWatcher → sessionMgr → HTTP server
- **Panic recovery**: Gateway handler + bridge forwardEvents must recover panics, log error, return `handler panic` / `bridge panic` to caller
- **Control commands**: Natural language triggers require `$` prefix (e.g. `$gc`, `$休眠`) to prevent accidental matches; slash commands (`/gc`, `/reset`, `/park`, `/restart`, `/new`) have no prefix
- **Text sanitization**: All user-facing text output passes through `SanitizeText()` before delivery to messaging platforms
- **Interaction timeout**: Permission/Q&A/elicitation requests auto-deny after 5 minutes to prevent indefinite blocking
- **Session key derivation**: UUIDv5 deterministic mapping from (ownerID, workerType, clientSessionID, workDir) for cross-environment consistency
- **LLM auto-retry**: Configurable retryable error patterns (429, 5xx, network errors) with exponential backoff; per-session attempt tracking
- **Documentation**: 增量文档中文优先，重要文档中英双语。技术术语保留英文原文。增量文档（Issue/PR 模板、配置说明、changelog）用中文；重要文档（根 README、架构设计、协议规范）拆分为独立的中英文文件（如 `README.md` + `README_zh.md`），文件头部互相链接跳转
- **File safety (multi-agent)**: 当前环境存在多 Agent 协同工作，对文件执行还原（`git restore`）、恢复、撤销（`git checkout`）、暂存（`git stash`）等操作前，**必须先在 `/tmp` 下创建备份**（`cp <file> /tmp/<file>.bak.$(date +%s)`），防止其他 Agent 的未提交改动被意外覆盖或丢失

## ANTI-PATTERNS (THIS PROJECT)

- ❌ `sync.Mutex` embedding or pointer passing — always explicit `mu` field
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

## COMMANDS

All build/test/lint operations MUST use `make` targets. Do NOT use raw `go build` / `go test` / `golangci-lint` directly.

```bash
make build                    # Build gateway binary (optimized, output: bin/hotplex-worker-<os>-<arch>)
make test                     # Run tests with -race (timeout 15m)
make test-short               # Quick test pass (-short)
make lint                     # golangci-lint
make coverage                 # Coverage report
make check                    # Full CI workflow: fmt + vet + lint + test + build
make quality                  # fmt + vet + lint + test (no build)
make fmt                      # Format code (gofmt + goimports)
make tidy                     # go mod tidy
make build-pgo                # PGO-optimized build
make clean                    # Clean build artifacts
```

## NOTES

- `configs/` config-dev.yaml / config-prod.yaml / config.yaml / env.example / grafana / monitoring
- `CLAUDE.md` is symlinked to `AGENTS.md` — edit AGENTS.md only, CLAUDE.md auto-follows
- `.claude` is symlinked to `.agent` — both directories exist
- No `api/` directory — project uses JSON over WebSocket, not protobuf
- Project targets POSIX only (PGID isolation requires `syscall.SysProcAttr{Setpgid: true}`)
- Largest files: `feishu/adapter.go` (1065), `opencodeserver/worker.go` (1001), `slack/adapter.go` (915), `hub.go` (798), `manager.go` (777), `bridge.go` (766), `config.go` (728), `main.go` (656)
- STT scripts (`scripts/stt_server.py`, `scripts/fix_onnx_model.py`) are also deployed to `~/.agents/skills/audio-transcribe/scripts/` for Claude Code skill use
- STT model: `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall` (~900MB), ONNX FP32 non-quantized
- Zombie IO timeout default: 30 minutes (configurable via `worker.execution_timeout`)
- OpenCode CLI adapter removed — replaced by OpenCode Server adapter
- ACPX adapter has type constant (`TypeACPX`) but no implementation — `internal/worker/acpx/` is empty
- Postgres store is stub only (`ErrNotImplemented`) — only SQLite is production-ready
- `internal/gateway/api.go` provides REST session management alongside the WebSocket gateway
