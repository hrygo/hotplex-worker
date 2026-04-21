# Worker Adapter Package

## OVERVIEW
Go worker adapter package with 3 runtime adapters (ClaudeCode, OpenCodeSrv, Pi) + 1 noop reference implementation + shared process lifecycle management. ACPX type constant exists but has no implementation.

## STRUCTURE
```
internal/worker/
  worker.go          # Core interfaces: SessionConn, Capabilities, Worker, WorkerType, WorkerHealth
  registry.go        # Builder pattern: Register/NewWorker/RegisteredTypes
  noop/              # Reference implementation (compile-time assertions)
  claudecode/        # Claude Code adapter (claude --print --session-id, 631 lines)
  opencodeserver/    # OpenCode Server adapter (HTTP+SSE, 952 lines)
  pi/                # Pi-mono adapter (stdio/raw stdout, ~300 lines)
  acpx/              # EMPTY — only TypeACPX constant exists in worker.go
  base/
    worker.go        # BaseWorker shared lifecycle: Terminate/Kill/Wait/Health/LastIO
    conn.go          # stdin SessionConn: NDJSON over stdio, WriteAll, InputRecoverer
    env.go           # env construction: whitelist + session vars
  proc/
    manager.go       # Process lifecycle: PGID isolation, layered SIGTERM→SIGKILL termination
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new Worker adapter | `internal/worker/<name>/` | Implement `Worker` + `SessionConn` + `Capabilities`, register via `init()` |
| Core adapter interfaces | `worker.go` | SessionConn (line 19), Capabilities (line 40), Worker (line 84) |
| Worker type constants | `worker.go:70` | TypeClaudeCode, TypeOpenCodeSrv, TypeACPX, TypePimon, TypeUnknown |
| Process lifecycle | `proc/manager.go` | Start/Terminate/Kill/Wait/ReadLine |
| Worker registration | `registry.go` | `Register(t WorkerType, b Builder)`, blank import in main.go |
| Compile-time interface checks | `noop/worker.go` | `var _ worker.Worker = (*Worker)(nil)` assertions |
| InputRecoverer | `worker.go:141` | LastInput() for crash recovery input re-delivery |

## KEY PATTERNS

### Registry Pattern (self-registration via blank imports)
```go
// internal/worker/registry.go
type Builder func() (Worker, error)
func Register(t WorkerType, b Builder)
func NewWorker(t WorkerType) (Worker, error)

// Each adapter calls Register in its init():
// func init() { Register(TypeClaudeCode, func() (Worker, error) { return New(), nil }) }
// main.go blank imports: _ "hotplex-worker/internal/worker/claudecode"
```

### Process Lifecycle (proc/manager.go)
- **Start**: `exec.CommandContext` + `Setpgid:true` (PGID isolation) + env setup
- **Terminate**: 3-layer — SIGTERM → wait 5s gracefulShutdownTimeout → SIGKILL
- **Kill**: Direct SIGKILL with PGID targeting
- **ReadLine**: `bufio.Scanner` (init 64KB, max 10MB)

### Adapter Transport Comparison
| Adapter | Transport | Resume | Session ID |
|---------|-----------|--------|------------|
| ClaudeCode | stdio (`claude --print --session-id`) | `--resume` flag | External (gateway) |
| OpenCodeSrv | HTTP+SSE (`opencode serve`) | Process managed | Via HTTP API |
| Pi | stdio (raw stdout) | No (ephemeral) | N/A |
| Noop | N/A | N/A | Testing only |
| ACPX | N/A | N/A | Type constant only, no implementation |

## ANTI-PATTERNS
- Do NOT use `math/rand` for crypto — use `crypto/rand` for JTI, tokens
- Do NOT skip `Setpgid:true` — child process cleanup depends on PGID isolation
- Do NOT skip graceful shutdown — always attempt SIGTERM before SIGKILL
- Do NOT use shell execution — only call `claude`/`opencode` binaries directly
- Do NOT register ACPX adapter — directory is empty, only TypeACPX constant exists
