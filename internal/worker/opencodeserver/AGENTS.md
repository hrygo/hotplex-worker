# OpenCode Server Worker Adapter

## OVERVIEW

HTTP+SSE adapter for `opencode serve` singleton process. Thin session adapters share one lazily-started process via `SingletonProcessManager`. Supports compact (with model auto-resolution), rewind (with message ID auto-resolution), clear, and control requests.

## STRUCTURE
```
singleton.go      # SingletonProcessManager: lazy start, ref counting, 30m idle drain, crash detection
worker.go         # Worker: thin adapter, Start/Resume acquire ref, Terminate/Kill release ref + close SSE
commands.go       # ServerCommander: HTTP REST for Compact/Clear/Rewind + ControlRequest routing
```

## WHERE TO LOOK
| Task | Location |
|------|----------|
| Add REST command | `commands.go` — add method to `ServerCommander`, implement `WorkerCommander` or use `doPost` |
| Change singleton config | `singleton.go` constants (`idleDrainPeriod`, `readyTimeout`) |
| Change session lifecycle | `worker.go` `Start()`/`Resume()`/`Terminate()` |
| Add control request subtype | `commands.go` `SendControlRequest()` switch |
| Fix model resolution | `commands.go` `lastKnownModel()` — queries messages for providerID/modelID |
| Fix rewind resolution | `commands.go` `lastAssistantMessageID()` — queries messages for last assistant info.id |

## KEY PATTERNS

**Singleton lifecycle**: `InitSingleton()` (gateway start) → lazy `opencode serve` → ref-counted Workers → `ShutdownSingleton()` (gateway stop)
- 30m idle drain via `monitorProcess` goroutine
- Crash detection: `monitorProcess` watches process, creates new `crashCh` per lifecycle
- Workers never kill the process — only close their SSE connections

**Compact auto-resolve**: `pendingModel` (from `set_model` control request) used first; falls back to `lastKnownModel()` which queries `/session/{id}/message` for assistant message's `info.providerID`/`info.modelID`

**Rewind auto-resolve**: If no `targetID`, queries `/session/{id}/message` for last assistant message's `info.id`

**OCS message format**: `info.id` (message ID), `info.providerID`, `info.modelID` at `info` level (not nested under `info.model` — that's only on user messages)

**HTTP client**: Shared `http.Client` with 30s timeout; `doGet`/`doPost`/`doRequest` helpers with JSON marshaling

## ANTI-PATTERNS
- ❌ Call `Terminate()`/`Kill()` to stop the singleton process — only releases ref + closes SSE
- ❌ Assume `info.model` exists on assistant messages — model info is at `info.providerID`/`info.modelID` directly
- ❌ Skip model params in compact — OCS API returns 400 without `providerID`+`modelID`
