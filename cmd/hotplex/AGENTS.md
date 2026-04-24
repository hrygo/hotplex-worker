# cmd/hotplex — Gateway CLI Commands

## OVERVIEW
Cobra CLI entry points for the HotPlex gateway binary. Root command in main.go, subcommands in dedicated files. Gateway startup/DI concentrated in gateway_run.go + serve.go.

## STRUCTURE
```
cmd/hotplex/
  main.go            (~54 lines)  cobra root: register gateway, doctor, security, onboard, version subcommands
  gateway_run.go     (~393 lines) gateway run: full DI pipeline, signal handler, hub/session/bridge creation, HTTP server start
  serve.go           (~110 lines) serve subcommand: flag parsing, config loading, GatewayDeps assembly, startMessagingAdapters
  routes.go          (~197 lines) HTTP route registration: /ws (gateway), /admin/*, /health, /metrics
  messaging_init.go  (~233 lines) messaging adapter lifecycle: iterate enabled platforms → New → Configure → SetHub/SetSM → Start; STT engine setup
  doctor.go          (~150 lines) doctor subcommand: run DefaultRegistry checkers, render structured report
  security.go        (~182 lines) security subcommand: path validation, env isolation checks
  onboard.go         (~105 lines) onboard subcommand: launch interactive wizard, generate config
  config_cmd.go      (~61 lines)  config subcommand: validate, dump, show current config
  status.go          (~95 lines)  status subcommand: check gateway process status via PID file
  banner.go          (~167 lines) startup banner: ASCII art + config summary + endpoint URLs
  dev.go             (~29 lines)  dev subcommand: start gateway + webchat concurrently
  pid.go             (~50 lines)  PID file helpers: write/remove PID for gateway process management
  version.go         (~46 lines)  version subcommand: print version string
  banner_art.txt                  ASCII art banner content
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new CLI subcommand | `main.go` | Create `<name>.go`, register via `rootCmd.AddCommand()` |
| Gateway DI pipeline | `gateway_run.go` | GatewayDeps struct: Hub, SessionManager, Bridge, LLMRetryController creation |
| Signal handler | `gateway_run.go` | SIGINT/SIGTERM → cancel ctx → ordered shutdown |
| Shutdown order | `gateway_run.go` | signal → cancel → tracing → hub → configWatcher → sessionMgr → HTTP server |
| HTTP route registration | `routes.go` | `/ws`, `/admin/*`, `/health`, `/metrics` — add new routes here |
| Messaging adapter wiring | `messaging_init.go` | `startMessagingAdapters()`: iterate config → Register → Configure → Start |
| STT engine setup | `messaging_init.go` | STT engine creation per platform: local/persistent/fallback |
| Gateway flags | `serve.go` | `-config`, `-addr`, `-log-level` etc. → Viper binding |
| GatewayDeps struct | `serve.go` | Holds all runtime deps: Hub, SM, Bridge, ConfigStore, LLMRetryController |
| Startup banner | `banner.go` | ASCII art + resolved config summary (addr, admin, platforms) |
| PID management | `pid.go` | Write PID on start, remove on stop, detect stale PIDs |

## KEY PATTERNS

**GatewayDeps (DI container)**
```go
// serve.go
type GatewayDeps struct {
    Hub         *gateway.Hub
    SM          *session.Manager
    Bridge      *gateway.Bridge
    ConfigStore *config.ConfigStore
    LLMRetry    *gateway.LLMRetryController
    // ... admin, metrics, tracing
}
```

**Messaging init sequence**
1. Iterate `config.Messaging.Slack/Feishu` → skip if `!Enabled`
2. `messaging.New(platformType)` → adapter instance
3. `adapter.Configure(platformConfig)` → apply credentials/policies
4. `adapter.SetHub/SetSM/SetHandler/SetBridge` → inject dependencies
5. `adapter.Start(ctx)` → connect to platform (Socket Mode / WS)

**Route registration (routes.go)**
- `/ws` → Hub.HandleHTTP (WebSocket upgrade)
- `/admin/*` → AdminAPI.Mux() (scoped auth)
- `/health` → liveness probe
- `/metrics` → Prometheus handler

**Signal handling (gateway_run.go)**
- `notifyContext(ctx, SIGINT, SIGTERM)` → context cancellation
- Ordered shutdown via defer chain (reverse of init order)

## ANTI-PATTERNS
- ❌ Add DI wiring outside `gateway_run.go` — all startup logic centralized there
- ❌ Import adapter packages directly — use `messaging.New()` registry pattern
- ❌ Skip PID file management — needed for `status` and `stop` subcommands
- ❌ Register routes outside `routes.go` — single file for all HTTP routing
