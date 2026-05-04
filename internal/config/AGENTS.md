# Config Package

## OVERVIEW
Central configuration via Viper + YAML with config inheritance, secrets provider chain, hot-reload watcher with audit log and rollback, and atomic ConfigStore with observer pattern.

## STRUCTURE
```
config/
  config.go          # Config struct (20+ sub-configs), Load, Validate, SecretsProvider chain, path normalization (923 lines)
  store.go           # ConfigStore: atomic Pointer[Config], Observer registration, Swap/Get
  watcher.go         # Watcher: fsnotify debounce, diffConfigs, hot/static change split, audit trail, rollback
  paths_unix.go      # DataDir/LogDir/PIDDir for Linux/macOS
  paths_windows.go   # DataDir/LogDir/PIDDir for Windows
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add config field | `config.go` Config struct | Add field + mapstructure tag + default in `applyDefaults()` |
| Config inheritance | `config.go` Load | `Inherits` field → recursive merge with cycle detection |
| Secrets loading | `config.go` SecretsProvider | EnvSecretsProvider (HOTPLEX_*) → ChainedSecretsProvider |
| Hot reload | `watcher.go` Watcher | fsnotify → debounce 500ms → diffConfigs → hot/static split |
| Audit trail | `watcher.go` ConfigChange | Field-level diff with old/new values, hot flag |
| Rollback | `watcher.go` Rollback() | History ring buffer, latestIdx tracking |
| Atomic access | `store.go` ConfigStore | `atomic.Pointer[Config]` for lock-free reads |
| Observer pattern | `store.go` Observer | Register observers for config changes |
| Path defaults | `paths_*.go` | Platform-specific data/log/PID directories |
| Validation | `config.go` Validate() | Required fields, value ranges, cross-field checks |

## KEY PATTERNS

**Config hierarchy**: `Config` contains 12 sub-configs (Gateway, DB, Worker, Security, Session, Pool, Log, Admin, WebChat, Messaging, AgentConfig, Skills). Each has mapstructure tags for Viper binding.

**Secrets provider chain**: `EnvSecretsProvider` reads HOTPLEX_* env vars → `ChainedSecretsProvider` tries multiple providers in order. JWT secret never from config file (mapstructure:"-").

**Hot reload flow**: fsnotify event → debounce timer → reload() → Load() → Validate() → diffConfigs() → split hot/static → apply via ConfigStore.Swap() → notify observers. Static changes logged but not applied (require restart).

**Audit + rollback**: Every change recorded as ConfigChange. History ring buffer (default 10 snapshots). Rollback() decrements latestIdx and applies previous snapshot.

**Inheritance**: `inherits: path/to/parent.yaml` → recursive Load with cycle detection (visited map). Child values override parent.

## ANTI-PATTERNS
- ❌ Read config without ConfigStore.Get() — direct field access bypasses atomic updates
- ❌ Add secrets to YAML config — use SecretsProvider or env vars only
- ❌ Skip Validate() after Load — invalid configs silently accepted
- ❌ Hot-reload static fields (addr, db.path) — they require restart
- ❌ Forget `mapstructure:"-"` for sensitive fields — they'd appear in config dump
