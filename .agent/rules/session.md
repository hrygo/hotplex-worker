---
paths:
  - "**/session/*.go"
---

# Session 管理规范

> Session 状态机、GC 策略、并发控制、mutex 规范
> 参考：`docs/specs/Acceptance-Criteria.md` §SM-001 ~ §SM-008

## Session ID 生命周期

**生成规则**：
- Session ID 由服务端在 `init` 握手时生成
- 客户端在 `init` 中可选提供 `session_id`（用于重连恢复）
- 服务端决定最终使用的 `session_id`
- `init_ack` 中返回的 `session_id` 是唯一可信来源

**客户端行为**：
```typescript
// 发送 init
const initEnv = {
    id: generateId(),
    version: 'aep/v1',
    session_id: existingSessionId || undefined,  // 重连时提供
    event: { type: 'init', data: { worker_type, config, auth } }
}

// 使用 init_ack 返回的 session_id
onMessage((env) => {
    if (env.event.type === 'init_ack') {
        this.sessionId = env.session_id  // 服务端分配的权威 ID
    }
})
```

**服务端行为**：
```go
// conn.go performInit
sessionID := initData.SessionID
if sessionID == "" {
    sessionID = c.sessionID  // 使用 conn 创建时的 ID
}

// 创建或恢复 session
si, err := handler.sm.Get(sessionID)
// ...

// 返回 session_id 给客户端
ack := BuildInitAck(sessionID, si.State, initData.WorkerType)
```

---

## 5 状态机

```
CREATED → RUNNING → IDLE → TERMINATED → DELETED
   ↑                    ↓            ↑
   └─── RESUME ←────────┘    │
          └──────────────────────┘
```

| 状态 | IsActive() | 语义 | 持续时间 |
|------|-----------|------|---------|
| `CREATED` | true | Session 创建，未开始执行 | 瞬态（<1s） |
| `RUNNING` | true | 正在执行 Worker，处理输入 | 业务执行期间 |
| `IDLE` | true | Worker 暂停，等待重连或新输入 | `idle_timeout` GC 前 |
| `TERMINATED` | false | Worker 已终止，保留元数据 | `retention_period` GC 前 |
| `DELETED` | false | 终态，DB 记录已删除 | 永久 |

### 状态语义

**StateIdle - 暂停状态**：
- Worker 进程暂停（paused），未终止
- 保留对话上下文和状态
- WebSocket 断开时自动进入
- 等待客户端重连恢复

**StateTerminated - 终止状态**：
- Worker 进程已终止
- 对话结束，但保留历史记录
- 需要完全重启（新 session）

**StateDeleted - 清理状态**：
- 数据库记录已删除
- 所有资源已释放
- 管理员操作或 GC 触发

### 合法转换规则
```go
var ValidTransitions = map[State][]State{
    CREATED:    {RUNNING, TERMINATED},
    RUNNING:    {IDLE, TERMINATED},
    IDLE:       {RUNNING, TERMINATED},
    TERMINATED: {RUNNING, DELETED}, // resume / GC
    DELETED:    {},                  // 终态
}
```

### Turn 生命周期
- `CREATED → RUNNING`：fork+exec 成功或 resume
- `RUNNING → IDLE`：Worker 执行完毕
- `IDLE → RUNNING`：收到新 input
- `IDLE → TERMINATED`：idle_timeout / max_lifetime / GC kill
- `TERMINATED → RUNNING`：resume（重启 runtime）
- `TERMINATED → DELETED`：GC retention_period 过期

---

## TransitionWithInput 原子性

**核心原则**：状态转换和 input 处理**必须在同一 mutex 内完成**，防止竞态。

```go
func (ms *managedSession) TransitionWithInput(ctx context.Context, content string) error {
    ms.mu.Lock()
    defer ms.mu.Unlock()

    // 1. 状态检查
    if !IsActive(ms.info.State) {
        return ErrSessionNotActive
    }
    if ms.info.State == RUNNING {
        return ErrSessionBusy
    }

    // 2. 原子转换 + 记录 input
    if err := ms.sm.Transition(RUNNING); err != nil {
        return err
    }
    return ms.store.RecordInput(ms.info.ID, content)
}
```

### done/input 竞态防护
当 Worker 发送 `done` 同时 Client 发送 `input`：
- 两者共享 `ms.mu.Lock`，input 的 state 检查和转换原子完成
- 第二个并发 input 收到 `SESSION_BUSY`

---

## SESSION_BUSY 硬拒绝

Session 不处于 `CREATED/RUNNING/IDLE` 状态时，**硬拒绝** input，不排队。

```go
func (sm *SessionManager) HandleInput(sessionID, content string) error {
    ms, err := sm.Get(sessionID)
    if err != nil {
        return err
    }
    return ms.TransitionWithInput(ctx, content)
    // err == ErrSessionBusy → 回复 error.code="SESSION_BUSY"
}
```

---

## mutex 规范

```go
// ✅ 正确：显式命名、零值、不 embedding
type managedSession struct {
    mu   sync.RWMutex
    info *SessionInfo
}

// ✅ 正确：写锁用于 TransitionWithInput
func (ms *managedSession) TransitionWithInput(...) error {
    ms.mu.Lock()
    defer ms.mu.Unlock()
}

// ✅ 正确：读锁用于 Get
func (ms *managedSession) Get() *SessionInfo {
    ms.mu.RLock()
    defer ms.mu.RUnlock()
    return ms.info
}

// ❌ 禁止：禁止指针传递
func foo(mu *sync.Mutex) {}  // 禁止

// ❌ 禁止：禁止 embedding
type Bad struct {
    sync.Mutex  // 禁止
}
```

---

## GC 策略

### 触发间隔
```go
scanInterval := cfg.Session.GCScanInterval // 默认 60s
```

### 清理规则
| 条件 | 操作 |
|------|------|
| IDLE session idle_expires_at ≤ now | → TERMINATED（idle_timeout） |
| session expires_at ≤ now（max_lifetime） | → TERMINATED（max_lifetime） |
| RUNNING session LastIO() > execution_timeout | → TERMINATED（zombie） |
| TERMINATED session updated_at ≤ now - retention_period | → DELETE FROM sessions |

### GC goroutine shutdown
```go
func (sm *SessionManager) runGC(ctx context.Context) {
    ticker := time.NewTicker(scanInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            sm.scan()
        }
    }
}
```

---

## PoolManager 配额

```go
// 全局配额
MaxPoolSize    = 20  // 全局最大活跃 Worker
MaxIdlePerUser = 5   // per-user 最大空闲 Session

func (p *PoolManager) Acquire(userID string) error {
    if p.totalCount.Load() >= MaxPoolSize {
        return ErrPoolExhausted
    }
    if p.perUserCount(userID) >= MaxIdlePerUser {
        return ErrUserQuotaExceeded
    }
    p.totalCount.Add(1)
    p.userCounts[userID].Add(1)
    return nil
}
```

---

## SQLite WAL 模式

```go
func NewSQLiteStore(path string) (*SQLiteStore, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }
    // 必须启用 WAL + busy_timeout
    if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
        return nil, err
    }
    if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
        return nil, err
    }
    // 写入通过单写 goroutine 串行化
}
```
