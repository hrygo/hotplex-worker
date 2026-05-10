---
type: design
tags:
  - project/HotPlex
  - security/resource-management
  - admin/authorization
---

# Resource Management & Permission Control

> HotPlex v1.0 资源管理与权限控制设计。

---

## 1. Session Ownership 模型

### 1.1 所有权定义

每个 Session 有一个明确的 Owner：

```go
type Session struct {
    ID        string
    OwnerID   string    // JWT sub claim
    BotID     string    // JWT bot_id claim（可选）
    State     SessionState
    CreatedAt int64
}
```

### 1.2 权限矩阵

| 操作 | Owner | Admin | 说明 |
|------|-------|-------|------|
| `input` | ✅ | ❌ | 仅 Owner 可发送输入 |
| `control.terminate` | ✅ | ❌ | 仅 Owner 可终止自己的 Session |
| `control.delete` | ✅ | ❌ | 仅 Owner 可删除自己的 Session |
| Admin API `DELETE /admin/sessions/{id}` | ❌ | ✅ | Admin 可强制终止任何 Session |
| Admin API `GET /admin/sessions` | ❌ | ✅ | Admin 可查看所有 Session |

### 1.3 Ownership 验证

```go
func (sm *SessionManager) ValidateOwnership(sessionID, userID string) error {
    session, err := sm.GetSession(sessionID)
    if err != nil {
        return ErrSessionNotFound
    }

    if session.OwnerID != userID {
        // 记录安全日志
        log.Warn("session ownership mismatch",
            "session_id", sessionID,
            "expected_owner", session.OwnerID,
            "actual_owner", userID,
        )
        return ErrSessionOwnershipMismatch
    }

    return nil
}
```

---

## 2. GC 权限控制

### 2.1 GC 操作权限

> ⚠️ **GC 操作是系统行为，不需要 Ownership 验证**。

GC 操作基于系统策略（idle_timeout、max_lifetime），不是用户发起的操作：

```go
func (gc *GCManager) RunGC() error {
    expiredSessions, err := gc.findExpiredSessions()
    if err != nil {
        return err
    }

    for _, session := range expiredSessions {
        // GC 是系统行为，无需验证 Ownership
        if err := gc.terminateSession(session); err != nil {
            log.Error("GC: failed to terminate session", "session_id", session.ID)
        }
    }

    return nil
}
```

### 2.2 Admin API 权限控制

**必须验证 Admin Token**：

```go
func (a *AdminAPI) DeleteSession(sessionID string, adminToken *AdminToken) error {
    // 1. 验证 Admin Token
    if err := a.validateAdminToken(adminToken); err != nil {
        return err
    }

    // 2. 验证权限
    if !contains(adminToken.Permissions, "session:delete") {
        log.Warn("admin delete denied",
            "admin_id", adminToken.ID,
            "required_perm", "session:delete",
        )
        return ErrPermissionDenied
    }

    // 3. 记录审计日志
    a.audit.Log(&AuditEvent{
        Action:      "admin_delete",
        ResourceID:  sessionID,
        AdminID:     adminToken.ID,
        Result:      "success",
    })

    return sm.TerminateSession(sessionID, "admin_force_kill")
}
```

---

## 3. 资源限制

### 3.1 输出限制

**分层限制策略**：

| 层级 | 限制 | 说明 |
|------|------|------|
| **单行** | 10MB | 防止单行过大 |
| **单轮总输出** | **20MB** | 防止单轮（Turn）无限循环或巨大文本导致内存耗尽 |
| **Envelope** | 1MB | JSON 协议限制 |

```go
const (
    MaxLineBytes     = 10 * 1024 * 1024  // 10MB per line
    MaxTurnBytes     = 20 * 1024 * 1024  // 20MB per turn
    MaxEnvelopeSize  = 1 * 1024 * 1024   // 1MB
)

type OutputLimiter struct {
    mu         sync.Mutex
    turnBytes  int64
}

// ResetTurn 在每一轮(Turn)输入开始时调用，防备热复用会话触及累计天花板
func (l *OutputLimiter) ResetTurn() {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.turnBytes = 0
}

func (l *OutputLimiter) Check(data []byte) error {
    l.mu.Lock()
    defer l.mu.Unlock()

    if int64(len(data)) > MaxLineBytes {
        return &OutputLimitError{
            Type:  "line",
            Limit: MaxLineBytes,
        }
    }

    if l.turnBytes+int64(len(data)) > MaxTurnBytes {
        l.truncateTurn()
        return &OutputLimitError{
            Type:  "turn",
            Limit: MaxTurnBytes,
        }
    }

    l.turnBytes += int64(len(data))
    return nil
}
```

### 3.2 并发限制

**配置**：

```yaml
worker:
  global:
    max_concurrent: 20
    per_user:
      max_concurrent: 5
      max_total_memory_mb: 2048
```

**实现**：

```go
type WorkerPool struct {
    mu            sync.RWMutex
    maxConcurrent int
    activeWorkers map[string]*Worker

    perUserLimit int
    perUserCount map[string]int
}

func (p *WorkerPool) Acquire(userID string) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    if len(p.activeWorkers) >= p.maxConcurrent {
        return ErrPoolExhausted
    }

    if p.perUserCount[userID] >= p.perUserLimit {
        return ErrUserQuotaExceeded
    }

    p.activeWorkers[workerID] = worker
    p.perUserCount[userID]++

    return nil
}
```

### 3.3 内存限制

```go
type WorkerConfig struct {
    MemoryLimitMB uint64
}

func (p *WorkerProcess) SetMemoryLimit(maxBytes uint64) error {
    limit := &syscall.Rlimit{
        Cur: maxBytes,
        Max: maxBytes,
    }
    return syscall.Setrlimit(syscall.RLIMIT_AS, limit)
}
```

---

## 4. Backpressure 机制

### 4.1 队列容量

```go
const (
    InputQueueSize  = 100
    OutputQueueSize = 50
)
```

### 4.2 背压策略

```go
func (s *Session) HandleInput(input *InputEvent) error {
    select {
    case s.inputQueue <- input:
        return nil
    default:
        return ErrInputQueueFull
    }
}

func (s *Session) HandleOutput(output *OutputEvent) error {
    select {
    case s.outputQueue <- output:
        return nil
    default:
        // message.delta 可丢弃（不消耗 seq）
        if output.Kind == "message.delta" {
            return nil
        }
        return ErrOutputQueueFull
    }
}
```

---

## 5. 错误码

```go
var (
    // Session 错误
    ErrSessionNotFound          = errors.New("session not found")
    ErrSessionOwnershipMismatch = errors.New("session ownership mismatch")
    ErrSessionBusy              = errors.New("session busy")

    // 资源错误
    ErrPoolExhausted           = errors.New("worker pool exhausted")
    ErrUserQuotaExceeded       = errors.New("user quota exceeded")
    ErrLineExceedsLimit        = errors.New("output line exceeds 10MB limit")
    ErrTurnOutputLimitExceeded  = errors.New("turn output exceeds 20MB limit")
    ErrInputQueueFull          = errors.New("input queue full")
    ErrOutputQueueFull         = errors.New("output queue full")
    ErrMemoryLimitExceeded     = errors.New("memory limit exceeded")
)
```

---

## 6. 配置

```yaml
# configs/worker.yaml
worker:
  global:
    max_concurrent: 20
    startup_timeout: 30s
    shutdown_timeout: 10s

    resources:
      memory_limit_mb: 512
      memory_limit_per_user_mb: 2048

    output_limit:
      max_line_bytes: 10485760      # 10MB
      max_turn_bytes: 20971520      # 20MB (per turn)

    queue_limit:
      input_queue_size: 100
      output_queue_size: 50

    per_user:
      max_concurrent: 5
      max_total_memory_mb: 2048
```