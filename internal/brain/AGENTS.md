# Brain Orchestration Package

## OVERVIEW
LLM orchestration layer providing safety guard (input/output validation), intent routing (chat/command/task), context compression (session memory), and multi-provider LLM client with decorator chain (retry, cache, rate limit, circuit breaker, metrics). Optional — graceful degradation when no API key configured.

## STRUCTURE
```
brain/
  brain.go       # Core interfaces: Brain, StreamingBrain, RoutableBrain, ObservableBrain + global singleton (106 lines)
  init.go        # Init() orchestration + enhancedBrainWrapper (middleware pipeline assembly) (412 lines)
  config.go      # 13 sub-configs + 4-tier API key discovery + env loading (484 lines)
  guard.go       # SafetyGuard: threat detection, per-user rate limit, Chat2Config, self-healing (781 lines)
  router.go      # IntentRouter: message classification, LRU cache, fast-path detection (606 lines)
  memory.go      # ContextCompressor + MemoryManager: session compression, preferences, TTL cleanup (653 lines)
  extractor.go   # ConfigExtractor: Claude Code / OpenCode credential extraction from config files (152 lines)
  util.go        # UTF-8 safe string truncation (21 lines)
  llm/           # LLM client subpackage (see below)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Brain interfaces | `brain.go:24` Brain, `brain.go:37` StreamingBrain, `brain.go:47` RoutableBrain, `brain.go:58` ObservableBrain |
| Init + middleware chain | `init.go:20` Init() | Assembles: baseClient → Retry → Cache → (RateLimit per-call) |
| enhancedBrainWrapper | `init.go:174` | Satisfies all 4 interfaces; applies timeout, rate limit, metrics, model routing |
| 4-tier API key discovery | `config.go:231` systemKeySources | Dedicated key → worker configs → system env → disabled |
| Safety guard input check | `guard.go:214` CheckInput | Fast regex → deep AI analysis if sensitivity > low |
| Safety guard output check | `guard.go:371` CheckOutput | Output sanitization + threat classification |
| Chat2Config (NLU) | `guard.go:564` ParseConfigIntent | Natural language config changes via Brain, disabled by default |
| Intent routing | `router.go:147` Route() | Classifies: chat/command/task; fast-path for greetings/status |
| Group chat relevance | `router.go:421` IsRelevant() | Determines if message needs agent response |
| Quick response generation | `router.go:462` GenerateResponse() | Non-agent responses for greetings/status queries |
| Context compression | `memory.go:197` CheckAndCompress | Wait until tokens > 8K → summarize older turns → replace |
| User preference extraction | `memory.go:546` ExtractPreferences | Brain-powered preference learning from conversations |
| Config credential extraction | `extractor.go:18` ConfigExtractor | Reads ~/.claude/settings.json, ~/.config/opencode/opencode.json |
| LLM client interface | `llm/client.go` LLMClient | Chat, Analyze, ChatStream, HealthCheck |
| Model router | `llm/router.go` | 4 strategies: cost/latency/quality/balanced |
| Circuit breaker | `llm/circuit.go` | gobreaker wrapper + CircuitClient decorator |
| Rate limiter | `llm/ratelimit.go` | Token bucket + queue + per-model limiting |
| Cost calculator | `llm/cost.go` | CJK-aware token estimation, per-session tracking, budget alerts |
| Metrics collector | `llm/metrics.go` | OpenTelemetry integration, RequestTimer |

## KEY PATTERNS

**Decorator chain (LLM clients)**
```
baseClient (OpenAI | Anthropic)
  → RetryClient (cenkalti/backoff exponential)
    → CachedClient (hashicorp/golang-lru)
      → [RateLimit applied per-call in wrapper, not as decorator]
```
7 LLMClient implementations: OpenAIClient, AnthropicClient, RetryClient, CachedClient, RateLimitedClient, CircuitClient, MetricsClient.

**Global singletons (lazy init)**
- `globalBrain` — `brain.Global()` accessor, `brain.SetGlobal()` setter
- `globalGuard` — `brain.GlobalGuard()` accessor, `brain.InitGuard()` setter
- `globalIntentRouter` — `brain.GlobalIntentRouter()` accessor
- `globalCompressor` / `globalMemoryMgr` — memory management singletons

**Two-stage threat detection (SafetyGuard)**
1. Fast regex patterns (zero-allocation) — ban patterns + sensitive patterns
2. Deep AI analysis only if sensitivity > "low" — uses Brain.Analyze()

**Intent fast-path (IntentRouter)**
- Greetings, thanks, status commands → skip LLM, return cached response
- Code-related keywords → immediate `IntentTypeChat` classification
- LRU cache (configurable size, default 100) for repeated queries

**Context compression algorithm (Memory)**
1. Record turns via `RecordTurn()` → accumulate in session history
2. `CheckAndCompress()` → when tokens > threshold (8K), compress older turns
3. Keep last N turns (5), summarize rest via Brain, replace with summary
4. Background TTL cleanup daemon with `sync.WaitGroup`

**CJK-aware token estimation**
- ASCII: 4.0 chars/token, CJK: 1.5 chars/token, Other Unicode: 3.0 chars/token

**4-tier API key discovery (config.go)**
1. `HOTPLEX_BRAIN_API_KEY` (dedicated)
2. Worker config files (`~/.claude/settings.json`, `~/.config/opencode/opencode.json`)
3. System env vars (`ANTHROPIC_API_KEY` → `OPENAI_API_KEY` → `SILICONFLOW_API_KEY` → `DEEPSEEK_API_KEY`)
4. Disabled (graceful degradation, no LLM features)

**Atomic metrics**
- Guard and Router use `atomic.Int64` for lock-free counters (totalChecks, blockedInputs, cacheHits)

## ANTI-PATTERNS
- ❌ Access `globalBrain` directly — use `brain.Global()` accessor
- ❌ Skip `Init()` before using any brain feature — singletons must be initialized
- ❌ Call `CheckInput` without per-user rate limiting — always use `CheckInputWithUser()`
- ❌ Enable `Chat2ConfigEnabled` in production — natural language config changes are a security risk
- ❌ Close `req.done` channel in rate limiter — `WaitModel` owns cleanup
- ❌ Forget `sensitivity` threshold in guard config — "low" skips deep analysis for performance
