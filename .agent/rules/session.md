---
paths:
  - "**/session/*.go"
  - "**/gateway/bridge.go"
---

# Session 管理规范

> mutex / 锁顺序 / 反模式规范 → 见 AGENTS.md 约定与规范

## Session ID 生命周期

- Session ID 由服务端在 `init` 握手时生成，`init_ack` 返回的为权威 ID
- 客户端可在 `init` 中提供 `session_id` 用于重连恢复，服务端决定最终值
- 实现：`conn.go performInit` — 空 sessionID 时使用 conn 创建时的 ID

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

## Fast Reconnect 优化（conn.go）

WebSocket 重连时，若 worker 仍然存活，跳过 terminate + resume 周期：

```go
// conn.go — performTransition
if ms.info.State == RUNNING && ms.worker != nil {
    return nil // Worker 存活，直接复用
}
return ms.sm.Transition(target)
```

---

## TransitionWithInput 原子性

状态转换和 input 处理**必须在同一 mutex 内完成**，防止 done/input 竞态。实现：`ms.mu.Lock` 内先检查状态（非 Active → `ErrSessionNotActive`，Running → `ErrSessionBusy`），再原子转换 + 记录 input。

---

## SESSION_BUSY 硬拒绝

Session 不处于 `CREATED/RUNNING/IDLE` 状态时，**硬拒绝** input，不排队。返回 `error.code="SESSION_BUSY"`。

---

## GC 策略

### 清理规则
| 条件 | 操作 |
|------|------|
| IDLE session idle_expires_at ≤ now | → TERMINATED（idle_timeout） |
| session expires_at ≤ now（max_lifetime） | → TERMINATED（max_lifetime） |
| RUNNING session LastIO() > execution_timeout | → TERMINATED（zombie, 默认 30 分钟） |
| TERMINATED session updated_at ≤ now - retention_period | → DELETE FROM sessions |

## DeletePhysical 幂等删除

绕过状态机强制删除，用于 API idempotent session 创建：

- `DeletePhysical` 跳过所有状态检查，直接从 DB 和内存 map 删除
- 仅限 API 层用于 idempotent 保障；业务逻辑用 `Transition(DELETED)`
- 幂等性：`DeletePhysical` 对已不存在的 session 返回 `nil`

---

## PoolManager 配额

```go
MaxPoolSize    = 20  // 全局最大活跃 Worker
MaxIdlePerUser = 5   // per-user 最大空闲 Session
```

---

## SQLite WAL 模式

必须启用 `PRAGMA journal_mode=WAL` + `PRAGMA busy_timeout=5000`，写入通过单写 goroutine 串行化。

---

## Crash Recovery（InputRecoverer + Fresh Start）

### InputRecoverer 接口
Worker 崩溃后，bridge 通过 `InputRecoverer` 提取最后一条输入用于重投递：

```go
type InputRecoverer interface {
    LastInput() string
}
```

### Fresh Start Fallback 流程
1. Worker 崩溃，bridge 检测到退出
2. 尝试 resume（重启 Worker 进程恢复对话）
3. Resume 失败 → fresh start：创建全新 Worker + 重投递最后输入
4. Fresh start 也失败 → 返回错误给客户端
