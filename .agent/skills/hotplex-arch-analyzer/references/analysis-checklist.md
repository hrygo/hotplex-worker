# HotPlex Architecture Analysis Checklist

HotPlex Worker Gateway 专用架构分析检查清单。针对以下核心架构层次优化分析：

**核心架构层次**：
- Gateway 层：WebSocket 连接管理、事件分发、AEP 协议
- Session 层：5 状态机、SQLite 持久化、PoolManager
- Worker 层：Claude Code、OpenCode Server、Pi-mono 适配器
- Messaging 层：Slack、飞书双向消息适配
- Agent Config 层：B/C 通道人格注入

**关键设计模式**：
- DI 手动注入（无 wire/dig）
- BaseWorker 嵌入模式
- PlatformConn 接口抽象
- SharedTranscriber 引用计数
- atomic.Value/atomic.Pointer 无锁单例

---

## 1. SOLID Principles

### Single Responsibility (SRP)
- [ ] Does each struct/file have one clear responsibility?
- [ ] Are there "god objects" handling too many concerns?
- [ ] Do methods mix business logic with infrastructure (DB, HTTP, WS)?

### Open/Closed (OCP)
- [ ] Can new behavior be added without modifying existing code?
- [ ] Are switch/type-assertion chains that grow with new types?
- [ ] Strategy pattern opportunities?

### Liskov Substitution (LSP)
- [ ] Do interface implementations respect contracts?
- [ ] Any panics or "not implemented" errors in interface methods?

### Interface Segregation (ISP)
- [ ] Are interfaces small and focused? (ideally 1-3 methods)
- [ ] Do implementations have unused interface methods?
- [ ] Fat interfaces that should be split?

### Dependency Inversion (DIP)
- [ ] Do high-level modules depend on abstractions, not concretions?
- [ ] Are dependencies injected (constructor/function params)?
- [ ] Global state / package-level vars that should be injected?

## 2. DRY Violations

- [ ] Repeated error handling patterns (could be helper/middleware)
- [ ] Duplicated struct definitions (especially request/response)
- [ ] Copy-pasted validation logic
- [ ] Similar goroutine lifecycle patterns (could be shared)
- [ ] Repeated config loading/reading patterns
- [ ] Similar channel/buffer management across files
- [ ] Cross-module duplication (check adapters for copy-paste)

## 3. Coupling & Cohesion

- [ ] Import graph: circular dependencies?
- [ ] Stable dependencies: do volatile packages depend on stable ones?
- [ ] Hidden coupling via shared state (global vars, package-level maps)
- [ ] Interface coupling: too many params in function signatures?
- [ ] Event coupling: tight coupling via channel types
- [ ] Module cohesion: do all exports relate to a single concept?

## 4. Error Handling

- [ ] Silent error swallowing: `if err != nil { return nil }` without logging
- [ ] Lost error context: `fmt.Errorf("failed")` without `%w` wrapping
- [ ] Inconsistent error types: mix of sentinel errors and custom types
- [ ] Missing error checks (especially on `io.Close`, `Write` calls)
- [ ] Panic in goroutines without recovery
- [ ] Error messages lacking context (which operation, which resource)
- [ ] Retry without backoff on transient errors

## 5. Concurrency Safety

- [ ] Shared mutable state without mutex protection
- [ ] Mutex lock ordering violations (different order in different goroutines)
- [ ] Goroutine leaks: goroutines that never exit (missing ctx.Done check)
- [ ] Channel operations: unbounded channels, missing close, write to closed
- [ ] Race conditions on shared slices/maps
- [ ] WaitGroup: Add/Done imbalance
- [ ] Select without default: potential deadlock
- [ ] Time.After in select: timer leaks in loops

## 6. Resource Management

- [ ] defer Close() on all io.Closer resources
- [ ] HTTP response body not closed on error paths
- [ ] File handles left open
- [ ] WebSocket connections not cleaned up on error
- [ ] Goroutine count unbounded (needs semaphore/pool)
- [ ] Memory held by stale entries in caches/maps (need TTL/eviction)
- [ ] Shutdown: ordered cleanup respecting dependencies

## 7. Performance

- [ ] Hot path allocations (string concatenation in loops, fmt.Sprintf)
- [ ] N+1 patterns (repeated DB/API calls in loop)
- [ ] Unnecessary copies of large structs (should use pointers)
- [ ] sync.Pool opportunities for frequently allocated objects
- [ ] Channel buffer sizing: unbuffered channels on hot paths
- [ ] JSON marshal/unmarshal on every call (could cache)
- [ ] Regex compilation inside loops (should be package-level)

## 8. Scalability

- [ ] Single goroutine bottleneck (e.g., single-writer channel)
- [ ] Lock contention: global mutex on hot path
- [ ] In-memory state that should be shared (distributed lock, external store)
- [ ] Unbounded queues: need backpressure mechanism
- [ ] Fixed limits that should be configurable
- [ ] Startup/shutdown time grows with number of X

## 9. Security

- [ ] Input validation: user input reaching DB/commands without sanitization
- [ ] Command injection: user input in exec.Command args
- [ ] Path traversal: user-controlled file paths
- [ ] SSRF: user-controlled URLs in HTTP requests
- [ ] Auth bypass: endpoints missing auth middleware
- [ ] Secrets in logs: sensitive data logged at Info level
- [ ] Token/session handling: proper invalidation on logout

## 10. Observability

- [ ] Structured logging: using slog with key-value pairs
- [ ] Error logs include sufficient context (operation, resource, IDs)
- [ ] Metrics: counter/gauge for key operations
- [ ] Tracing: spans for cross-service calls
- [ ] Health checks: dependency health verified
- [ ] Slow operation logging: threshold-based warnings
- [ ] Log levels appropriate (Debug for development, Info for operations)

## 11. Testability

- [ ] Dependencies injectable (not hardcoded imports)
- [ ] Interfaces for external dependencies (DB, HTTP clients)
- [ ] Test coverage for error paths (not just happy path)
- [ ] Table-driven tests for multi-scenario functions
- [ ] Mock-friendly: can replace real dependencies in tests
- [ ] Integration tests for critical paths
- [ ] Race detector: `go test -race` passing

## 12. Code Quality

- [ ] Cyclomatic complexity: functions > 15 lines with > 10 branches
- [ ] Dead code: unexported functions/types never referenced
- [ ] Naming: consistent conventions (Go idioms)
- [ ] God objects: structs with > 20 methods
- [ ] Magic numbers: unnamed constants
- [ ] Commented-out code (should be deleted)
- [ ] TODO/FIXME/HACK comments without issue references
