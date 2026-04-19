# PROJECT KNOWLEDGE BASE

## OVERVIEW

HotPlex Worker Gateway — Go 1.26 unified access layer for AI Coding Agent sessions.
WebSocket gateway (AEP v1) abstracting Claude Code, OpenCode CLI/Server, Pi-mono protocol differences.
Multi-language client SDKs (TS, Python, Java, Go) + AI SDK transport adapter + web chat UI.

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
cmd/worker/main.go    (~539 lines) flags, DI, signal, messaging init
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
- `gateway/bridge.go`  Session ↔ worker lifecycle orchestration

**Session**
- `session/manager.go`   5-state machine, state transitions, GC
- `session/store.go`     SQLite persistence (Upsert, Get, List, expired)
- `session/message_store.go`  Event log, single-writer goroutine
- `session/sql/`         Externalized .sql files (schema, migrations, queries)
- `session/queries.go`  embed.FS loader + stripComments
- `session/stores.go`   Multi-store registry (SQLite/Postgres)

**Messaging** (Slack/Feishu bidirectional)
- `messaging/bridge.go`   SessionStarter + ConnFactory + joined dedup
- `messaging/platform_conn.go`  PlatformConn: WriteCtx + Close
- `messaging/platform_adapter.go`  Base adapter + self-registration
- `messaging/slack/`      Socket Mode: NativeStreamingWriter, rate limiter
- `messaging/feishu/`     ws.Client: P2 events, converter, streaming, typing, stt.go (speech-to-text)
- `scripts/stt_server.py`  Persistent STT subprocess (SenseVoice-Small ONNX)
- `scripts/fix_onnx_model.py`  ONNX model Less node type mismatch auto-patch
- `messaging/mock/`       Mock adapter for testing

**Worker** (6 adapters)
- `worker/claudecode/`    Claude Code adapter
- `worker/opencodecli/`    OpenCode CLI adapter
- `worker/opencodeserver/`  OpenCode Server adapter
- `worker/acpx/`          ACPX: ACP bridge, stdio I/O
- `worker/pi/`            Pi-mono adapter
- `worker/noop/`          No-op adapter (testing)
- `worker/base/`          Shared BaseWorker + Conn + BuildEnv
- `worker/proc/`          Process lifecycle: PGID isolation, layered SIGTERM→SIGKILL

**Support**
- `security/`   JWT (ES256), SSRF, command whitelist, env isolation, path safety
- `metrics/`    Prometheus counters/gauges/histograms
- `tracing/`    OpenTelemetry setup (idempotent)

### pkg/
- `events/`   Envelope, Event, SessionState, all data structs
- `aep/`      AEP v1 codec

### Top-level
```
client/    Go client SDK (standalone module)
webchat/  Next.js web chat UI
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
- WebSocket protocol → `internal/gateway/conn.go` — ReadPump/WritePump + Handler dispatch
- Config structure → `internal/config/config.go` — structs + Default() + Validate()
- STT config → `internal/config/config.go:161-173` — FeishuConfig.STTProvider/STTLocalCmd/STTLocalMode/STTLocalIdleTTL
- Wire messaging adapter → `cmd/worker/main.go` — `startMessagingAdapters()`: config → New → Configure → SetConnFactory → Start

**Security**
- Add validation → `internal/security/` — one file per concern (jwt, ssrf, path, env, tool, command)

**Monitoring & API**
- Prometheus metric → `internal/metrics/` — follow `hotplex_<group>_<metric>_<unit>` naming
- Admin endpoint → `internal/admin/` — handlers.go (stats/health/config), sessions.go (CRUD)

## CODE MAP

**Entry**
- `main` / `GatewayDeps` → `cmd/worker/main.go:45/255` — entry point, DI container

**Gateway** (`internal/gateway/`)
- `Hub` → `hub.go:57` — WS broadcast hub, conn registry, session routing, seq gen
- `Conn` → `conn.go:27` — single WS connection, read/write pumps, heartbeat
- `Handler` → `handler.go` — AEP event dispatch (input, ping, control)
- `Bridge` → `bridge.go` — session ↔ worker lifecycle, StartPlatformSession
- `pcEntry` → `hub.go:551` — wraps PlatformConn for sessions map

**Session** (`internal/session/`)
- `Manager` → `manager.go:34` — 5-state machine, transitions, GC, worker attach/detach
- `managedSession` → `manager.go:52` — per-session state + mutex + worker ref
- `Store` (interface) → `store.go:22` — SQLite: Upsert, Get, List, expired queries
- `MessageStore` (interface) → `message_store.go` — event log, single-writer goroutine

**Worker** (`internal/worker/`)
- `Worker` (interface) → `worker.go:84` — Start/Input/Resume/Terminate/Kill/Wait/Conn/Health
- `SessionConn` (interface) → `worker.go:19` — bidirectional channel: Send/Recv/Close
- `Capabilities` (interface) → `worker.go:40` — feature query: resume, streaming, tools, env
- `base.BaseWorker` → `base/worker.go` — shared lifecycle: Terminate/Kill/Wait/Health/LastIO
- `base.Conn` → `base/conn.go` — stdin SessionConn: NDJSON over stdio, exported `WriteAll`
- `base.BuildEnv` → `base/env.go` — env construction: whitelist + session vars
- `proc.Manager` → `proc/manager.go:26` — PGID isolation, layered SIGTERM→SIGKILL

**Messaging** (`internal/messaging/`)
- `Bridge` → `bridge.go` — 3-step: StartSession → Join → handler.Handle
- `PlatformConn` (interface) → `platform_conn.go` — WriteCtx + Close
- `PlatformAdapter` → `platform_adapter.go` — base: SetHub/SetSM/SetHandler/SetBridge
- `FeishuSTT` → `feishu/stt.go:41` — cloud transcription via Feishu speech_to_text API
- `LocalSTT` → `feishu/stt.go:98` — ephemeral per-request external command transcription
- `PersistentSTT` → `feishu/stt.go:185` — long-lived subprocess, JSON-over-stdio, PGID isolation
- `FallbackSTT` → `feishu/stt.go:143` — primary + secondary fallback chain
- `Transcriber` (interface) → `feishu/stt.go:27` — Transcribe(ctx, audioData) → (text, error)

**Core**
- `Envelope` → `pkg/events/events.go:73` — AEP v1 envelope (id, version, seq, session_id, event)
- `SessionState` → `pkg/events/events.go:240` — Created/Running/Idle/Terminated/Deleted
- `Config` → `config/config.go:118` — all config structs
- `JWTValidator` → `security/jwt.go:27` — ES256 + JTI blacklist
- `client.Client` → `client/client.go:33` — Go SDK: Connect/Resume/SendInput/Close
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

## ANTI-PATTERNS (THIS PROJECT)

- ❌ `sync.Mutex` embedding or pointer passing — always explicit `mu` field
- ❌ `math/rand` for crypto (JTI, tokens) — use `crypto/rand`
- ❌ Shell execution — only `claude`/`opencode` binaries, no shell interpreters
- ❌ Non-ES256 JWT algorithms
- ❌ Missing goroutine shutdown path — every goroutine needs ctx cancel / channel close / WaitGroup
- ❌ `t.Fatal` in tests — use `testify/require`
- ❌ Skipping WAL mode for SQLite
- ❌ Cross-Bot session access
- ❌ Processing `done` + `input` without mutex — must be atomic in `TransitionWithInput`

## UNIQUE STYLES

- **Lock ordering**: `m.mu` (Manager) → `ms.mu` (per-session) — always in this order to prevent deadlock
- **Backpressure**: `message.delta` and `raw` events silently dropped when broadcast channel full; `state`/`done`/`error` never dropped
- **Seq allocation**: Per-session atomic monotonic counter; dropped deltas don't consume seq
- **Process termination**: 3-layer: SIGTERM → wait 5s → SIGKILL, PGID isolation for child cleanup
- **Worker types as constants**: `TypeClaudeCode`, `TypeOpenCodeCLI`, `TypeOpenCodeSrv`, `TypeACPX`, `TypePimon`
- **BaseWorker embedding**: Adapters embed `*base.BaseWorker` for shared lifecycle; each adapter implements only `Start`, `Input`, `Resume` + unique I/O parsing
- **Admin API extracted to package**: `internal/admin/` with interfaces for SessionManager/Hub/Bridge to avoid circular imports; adapters in main.go bridge concrete types
- **Gateway split**: conn.go (WebSocket lifecycle), handler.go (AEP dispatch), bridge.go (session orchestration) — same package, separate concerns
- **Config hot-reload**: File watcher with rollback capability, updates live config reference
- **Single-writer SQLite**: Channel-based write serialization with batch flush (50 items / 100ms)

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
- `.claude` is symlinked to `.agent` — both directories exist
- No `api/` directory — project uses JSON over WebSocket, not protobuf
- Project targets POSIX only (PGID isolation requires `syscall.SysProcAttr{Setpgid: true}`)
- Largest files: `opencodeserver/worker.go` (802), `manager.go` (765), `hub.go` (575), `config.go` (593), `opencodecli/worker.go` (528)
- STT scripts (`scripts/stt_server.py`, `scripts/fix_onnx_model.py`) are also deployed to `~/.agents/skills/audio-transcribe/scripts/` for Claude Code skill use
- STT model: `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall` (~900MB), ONNX FP32 non-quantized
