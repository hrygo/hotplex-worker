# PROJECT KNOWLEDGE BASE

## OVERVIEW

HotPlex Worker Gateway — Go 1.26 unified access layer for AI Coding Agent sessions.
WebSocket gateway (AEP v1) abstracting Claude Code, OpenCode CLI/Server, Pi-mono protocol differences.
Multi-language client SDKs (TS, Python, Java, Go) + AI SDK transport adapter + web chat UI.

## STRUCTURE

```
cmd/worker/          # main.go (~530 lines): flags, DI wire, signal handling, messaging init
internal/
  admin/             # Admin API: handlers, middleware, rate-limit, log buffer
  aep/               # AEP v1 codec (JSON envelope encode/decode/validate)
  config/            # Viper config loading + file watcher + hot-reload + applyMessagingEnv
  gateway/           # WS gateway: Hub (broadcast), Conn (read/write pump), Handler, Bridge
  session/           # Session Manager (5-state machine), Pool manager, GC
    sql/             # Externalized SQL: schema, migrations, queries (.sql files)
    queries.go       # embed.FS SQL loader with comment stripping
    stores.go        # Multi-store registry (SQLite/Postgres) with builder pattern
  messaging/         # Platform messaging adapters (Slack/Feishu bidirectional)
    bridge.go        # Platform Bridge: SessionStarter + ConnFactory + joined dedup
    platform_conn.go # PlatformConn interface (WriteCtx + Close)
    platform_adapter.go # Base adapter + self-registration registry
    slack/           # Slack Socket Mode: NativeStreamingWriter, rate limiter, thread ownership
    feishu/          # Feishu ws.Client: P2 events, converter, streaming, typing, chat queue
    mock/            # Mock adapter for testing
  worker/            # Worker interface + registry + base + 6 adapters (claudecode, opencodecli, opencodeserver, acpx, pi, noop)
    acpx/             # ACPX adapter: ACP bridge, session ID passthrough, stdio I/O
    base/             # Shared BaseWorker + Conn + BuildEnv for CLI-based adapters
    proc/            # Process lifecycle (PGID isolation, layered termination)
  security/          # JWT (ES256), SSRF, command whitelist, env isolation, path safety, tool policy
  metrics/           # Prometheus counters/gauges/histograms
  tracing/           # OTel setup (idempotent)
pkg/
  events/            # Shared types: Envelope, Event, SessionState, all data structs
  aep/               # AEP v1 codec
client/              # Go client SDK (standalone module)
webchat/            # Next.js web chat UI
examples/            # Multi-language client SDKs (typescript-client, python-client, java-client)
docs/                # Architecture, specs, security, testing, management docs
scripts/             # Build/validation scripts
```

## WHERE TO LOOK

| Task                      | Location                      | Notes                                                                                          |
| ------------------------- | ----------------------------- | ---------------------------------------------------------------------------------------------- |
| Add new AEP event type    | `pkg/events/events.go`        | Add Kind constant + Data struct + update Validate                                              |
| Add new Worker adapter    | `internal/worker/<name>/`     | Embed `base.BaseWorker`, implement `Start`/`Input`/`Resume` + unique I/O, register in `init()` |
| Add platform messaging adapter | `internal/messaging/<name>/` | Embed `PlatformAdapter`, implement `Start`/`HandleTextMessage`/`Close` + PlatformConn, register in `init()` |
| Change session lifecycle  | `internal/session/manager.go` | State machine + `TransitionWithInput` atomicity                                                |
| Modify WebSocket protocol | `internal/gateway/conn.go`    | ReadPump/WritePump + Handler dispatch                                                          |
| Add security validation   | `internal/security/`          | Separate file per concern (jwt, ssrf, path, env, tool, command)                                |
| Change config structure   | `internal/config/config.go`   | Struct definitions + Default() + Validate()                                                    |
| Add messaging config      | `internal/config/config.go`   | Add fields to SlackConfig/FeishuConfig + `applyMessagingEnv`                                   |
| Wire platform adapter     | `cmd/worker/main.go`          | `startMessagingAdapters()`: resolve config → New adapter → Configure → SetConnFactory → Start |
| Add Prometheus metric     | `internal/metrics/`           | Follow `hotplex_<group>_<metric>_<unit>` naming                                                |
| Admin API endpoint        | `internal/admin/`             | handlers.go for stats/health/config, sessions.go for session CRUD                              |

## CODE MAP

| Symbol            | Type      | Location                             | Role                                                                              |
| ----------------- | --------- | ------------------------------------ | --------------------------------------------------------------------------------- |
| `main`            | func      | `cmd/worker/main.go:45`              | Entry: flags → config → wire → serve → shutdown                                   |
| `GatewayDeps`     | struct    | `cmd/worker/main.go:255`             | DI container for all gateway dependencies                                         |
| `admin.AdminAPI`  | struct    | `internal/admin/admin.go`            | Admin endpoints (stats, health, session CRUD, config)                             |
| `Hub`             | struct    | `internal/gateway/hub.go:57`         | WS broadcast hub: conn registry, session routing, seq gen, JoinPlatformSession    |
| `Conn`            | struct    | `internal/gateway/conn.go:27`        | Single WS connection: read/write pumps, init, heartbeat                           |
| `Handler`         | struct    | `internal/gateway/handler.go`        | AEP event dispatch (input, ping, control)                                         |
| `Bridge`          | struct    | `internal/gateway/bridge.go`         | Orchestrates session ↔ worker lifecycle + StartPlatformSession                    |
| `Manager`         | struct    | `internal/session/manager.go:34`     | Session CRUD, state transitions, GC, worker attach/detach                         |
| `managedSession`  | struct    | `internal/session/manager.go:52`     | Per-session state + mutex + worker ref                                            |
| `Store`           | interface | `internal/session/store.go:22`       | SQLite persistence (Upsert, Get, List, expired queries)                           |
| `MessageStore`    | interface | `internal/session/message_store.go`  | Event log (Append, GetBySession) — single-writer goroutine                        |
| `Worker`          | interface | `internal/worker/worker.go:84`       | Core adapter: Start/Input/Resume/Terminate/Kill/Wait/Conn/Health                  |
| `base.BaseWorker` | struct    | `internal/worker/base/worker.go`     | Shared lifecycle: Terminate/Kill/Wait/Health/LastIO (embed in adapters)           |
| `base.Conn`       | struct    | `internal/worker/base/conn.go`       | Stdin-based SessionConn: Send/Recv/Close (NDJSON over stdio), exported `WriteAll` |
| `base.BuildEnv`   | func      | `internal/worker/base/env.go`        | Shared env construction: whitelist + session vars + StripNestedAgent              |
| `base.WriteAll`   | func      | `internal/worker/base/conn.go`       | Raw fd write with EAGAIN retry + `runtime.Gosched()`                              |
| `SessionConn`     | interface | `internal/worker/worker.go:19`       | Data plane: Send/Recv/Close bidirectional channel                                 |
| `Capabilities`    | interface | `internal/worker/worker.go:40`       | Worker feature query (resume, streaming, tools, env)                              |
| `proc.Manager`    | struct    | `internal/worker/proc/manager.go:26` | Process lifecycle: PGID isolation, layered SIGTERM→SIGKILL                        |
| `JWTValidator`    | struct    | `internal/security/jwt.go:27`        | ES256 JWT validation + JTI blacklist + token generation                           |
| `Envelope`        | struct    | `pkg/events/events.go:73`            | AEP v1 message envelope (id, version, seq, session_id, event)                     |
| `SessionState`    | type      | `pkg/events/events.go:240`           | 5 states: Created/Running/Idle/Terminated/Deleted                                 |
| `Config`          | struct    | `internal/config/config.go:118`      | All config: Gateway, DB, Worker, Security, Session, Pool, Admin, Messaging        |
| `client.Client`   | struct    | `client/client.go:33`                | Go client SDK: Connect/Resume/SendInput/Close                                     |
| `messaging.Bridge`| struct    | `internal/messaging/bridge.go`       | Platform bridge: Handle (3-step: StartSession → Join → handler.Handle)            |
| `PlatformConn`    | interface | `internal/messaging/platform_conn.go`| WriteCtx(ctx, env) + Close() — hub-compatible write interface                     |
| `PlatformAdapter` | struct    | `internal/messaging/platform_adapter.go`| Base type: SetHub/SetSM/SetHandler/SetBridge, embed in adapters               |
| `pcEntry`         | struct    | `internal/gateway/hub.go:551`        | Wraps PlatformConn for sessions map, delegates WriteCtx/Close                     |

## CONVENTIONS

- **Mutex**: Explicit `mu` field, zero-value, no embedding, no pointer passing
- **Errors**: `Err` prefix for sentinel vars, `Error` suffix for custom types, `fmt.Errorf("%w", ...)` for wrapping
- **Logging**: `log/slog` JSON handler, key-value pairs, `service.name=hotplex-gateway`
- **Testing**: `testify/require` (not `t.Fatal`), table-driven, `t.Parallel()`, `t.Cleanup()`
- **Config**: Viper YAML + env expansion, `SecretsProvider` interface for secrets
- **Worker registration**: `init()` + `worker.Register(WorkerType, Builder)` pattern via blank imports
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
