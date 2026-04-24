# Admin API Package

## OVERVIEW
HTTP admin API with token-scoped auth, rate limiting, IP whitelisting, and provider interfaces to avoid circular imports with gateway/session packages.

## STRUCTURE
```
admin.go         # AdminAPI struct, Deps, provider interfaces, middleware
handlers.go      # HTTP handlers: stats, health, worker-health, logs, config-validate/rollback, debug-session
sessions.go      # Session CRUD handlers: list, get, delete, transition
middleware.go     # Scope extraction, IP/CIDR checking
ratelimit.go     # Simple token-bucket rate limiter
logbuf.go        # Ring buffer for recent log entries
response.go      # JSON response helpers
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add admin endpoint | `handlers.go` | Follow Handle* pattern, check scopes |
| Provider interfaces | `admin.go:27-52` | SessionManagerProvider, HubProvider, BridgeProvider, ConfigProvider, ConfigWatcherProvider |
| Token auth + scopes | `admin.go:136` | validateToken: TokenScopes map → per-token scopes, fallback to DefaultScopes |
| Rate limiting | `ratelimit.go` | simpleRateLimiter: RequestsPerSec + Burst |
| IP whitelisting | `middleware.go` | CIDR-based allow list |
| Session handlers | `sessions.go` | List/Get/Delete/Transition with scope checks |
| Config hot-reload | `handlers.go:132` | Validate + Rollback with version tracking |

## KEY PATTERNS

**Provider interfaces (anti-circular-import)**
- admin.go defines Provider interfaces (SessionManagerProvider, HubProvider, etc.)
- cmd/hotplex/main.go has adapter structs (sessionManagerAdapter, hubAdapter, bridgeAdapter) that bridge concrete types
- AdminAPI never imports gateway/session packages directly

**Scope-based access control**
- 8 scopes: session:read/write/delete, stats:read, health:read, config:read, admin:read/write
- TokenScopes map: per-token scope list
- DefaultScopes: fallback when token found but no specific scopes

**Mux setup**
- New(deps) creates AdminAPI
- Mux() returns *http.ServeMux (routes registered externally)
- Middleware() wraps with rate-limit → IP check → token auth → scope injection

## ANTI-PATTERNS
- ❌ Import internal/gateway or internal/session directly — use Provider interfaces
- ❌ Skip scope check on admin endpoints
- ❌ Hard-code admin tokens in source code — use config only
