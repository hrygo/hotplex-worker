# Session 管理

> 面向开发者的 Session 生命周期、状态机、资源管理深度指南

## 概述

Session 是 HotPlex Gateway 的核心抽象。每个 Session 代表一次用户与 AI Worker 之间的持续对话，由 5 状态机管理生命周期，SQLite 持久化，后台 GC 自动回收资源。

Session ID 使用 UUIDv5 确定性生成（`session.DeriveSessionKey`），确保相同输入参数始终映射到同一 Session。

## 5 状态机

```
CREATED → RUNNING ⟷ IDLE → TERMINATED → DELETED
   ↑                    ↓            ↑
   └─── RESUME ←────────┘    │
          └──────────────────────┘
```

| 状态 | IsActive | 含义 | 典型停留时间 |
|------|----------|------|-------------|
| `CREATED` | true | 已创建，未启动 Worker | 瞬态（<1s） |
| `RUNNING` | true | Worker 正在执行 | 整个 Turn 执行期间 |
| `IDLE` | true | 等待用户输入 | `idle_timeout` 到期前 |
| `TERMINATED` | false | Worker 已终止，历史保留 | `retention_period` 到期前 |
| `DELETED` | false | 终态，记录已删除 | 永久 |

### 合法状态转换

| From → To | 触发条件 |
|-----------|---------|
| `CREATED → RUNNING` | `init` 握手完成，Worker 启动成功 |
| `RUNNING → IDLE` | Worker 执行完毕，Turn 结束（`done` 事件） |
| `IDLE → RUNNING` | 收到新 `input`（`TransitionWithInput` 原子操作） |
| `IDLE → TERMINATED` | `idle_timeout` / `max_lifetime` / GC 回收 |
| `RUNNING → TERMINATED` | `/gc`、Worker 崩溃、`max_turns` 限制 |
| `RUNNING → DELETED` | Admin API 强制删除 |
| `TERMINATED → RUNNING` | Resume（重启 Worker 进程，`--resume` 恢复对话） |
| `TERMINATED → DELETED` | GC retention_period 过期 / Admin API 删除 |
| `IDLE → DELETED` | Admin API 强制删除 |

## /gc 与 /reset：何时使用

### /gc（归档会话）

- **行为**：终止 Worker 进程，Session 进入 `TERMINATED`，**保留完整对话历史**
- **适用场景**：暂时离开对话、释放系统资源、下班前归档
- **Resume 行为**：下次发送消息时，Gateway 通过 `--resume` 恢复完整上下文，Worker 自动重建对话状态
- **资源影响**：Worker 进程被终止（释放 ~512MB 内存），Session 记录保留在 SQLite

### /reset（重置会话）

- **行为**：清空 `SessionInfo.Context`，Worker 自行决定 in-place 清空或 terminate+restart
- **适用场景**：对话方向完全错误、需要全新开始、上下文已严重污染
- **Resume 行为**：不保留任何历史，等同于全新对话
- **资源影响**：可能复用已有 Worker 进程（in-place reset），无需重新 fork

### 选择建议

```
需要保留上下文？ → /gc
需要全新开始？   → /reset
```

## Resume 行为详解

当 Session 处于 `TERMINATED` 状态时，发送新 `input` 会自动触发 Resume 流程：

1. Gateway 检测到 `TERMINATED` 状态 + 有新 input
2. 通过 `TERMINATED → RUNNING` 合法转换
3. 重新 fork Worker 进程，传入 `--session-id` + `--resume` 参数
4. Worker 从磁盘恢复对话历史（Claude Code 使用 `~/.claude/projects/<hash>/sessions/` 目录）
5. 将用户 input 投递到恢复的 Worker

**Fast Reconnect 优化**：如果 WebSocket 断线重连时 Worker 进程仍然存活，直接复用，跳过 terminate + resume 周期。

## 工作目录管理

使用 `/cd <path>` 切换工作目录：

1. **安全验证**：`ExpandAndAbs`（展开环境变量 + 绝对路径）+ `ValidateWorkDir`（路径安全检查）
2. **派生新 SessionKey**：基于新目录生成新的确定性 Session ID
3. **终止旧 Worker**：不删除原 Session 记录
4. **启动新 Session**：在新 key 下创建 Session，启动 Worker
5. **注入上下文**：自动将最后输入注入新 Session

工作目录持久化在 `SessionInfo.WorkDir` 字段，跨 Resume 保持一致。

## Turn 追踪

每个 "用户输入 → Worker 完成" 周期算一个 Turn：

- **开始**：收到 `input`，`TurnCount++`（在 `TransitionWithInput` 中原子递增）
- **结束**：收到 `done` 事件
- **限制**：`max_turns` 可配置（0 = 无限），超出时自动触发 anti-pollution restart

```go
// TransitionWithInput 内部
ms.TurnCount++
if maxTurns > 0 && ms.TurnCount > maxTurns {
    // 自动终止，防止无限循环
}
```

## 多 Session 与 Pool 管理

### PoolManager 配额控制

| 配额维度 | 配置项 | 说明 |
|---------|--------|------|
| 全局最大 Worker 数 | `pool.max_size` | 所有用户的 Worker 总上限（0 = 无限） |
| 单用户最大 Session 数 | `pool.max_idle_per_user` | 防止单用户占用过多资源（0 = 无限） |
| 单用户最大内存 | `pool.max_memory_per_user` | 每个 Worker 估算 512MB（RLIMIT_AS） |

### 配额错误

| 错误 | 触发条件 |
|------|---------|
| `ErrPoolExhausted` | 全局 Worker 数已达上限 |
| `ErrUserQuotaExceeded` | 该用户 Session 数已达上限 |
| `ErrMemoryExceeded` | 该用户总内存估算超限 |

### 并发安全

- **锁顺序**：`Manager.mu → managedSession.mu`（固定顺序，防止死锁）
- **原子操作**：状态转换和 input 处理在同一 mutex 内完成（`TransitionWithInput`）
- **CAS 语义**：`DetachWorkerIf` 使用 compare-and-swap 防止过期 goroutine 覆盖新 Worker

## Session 调试

### /context 命令

通过 `worker_command` 事件请求 Worker 报告当前 context 使用情况：

```
/context
```

返回 `context_usage` 事件，包含：
- `total_tokens`：当前总 token 数
- `max_tokens`：模型最大 token 数
- `percentage`：使用百分比
- `categories`：按类别细分的 token 用量

### Admin API

Gateway 暴露 Admin API（默认 `localhost:9999`），支持深度 Session 检查：

- **列出所有 Session**：`GET /admin/sessions`
- **查看 Session 详情**：`GET /admin/sessions/:id`
- **查看 Worker 健康状态**：`GET /admin/workers/health`
- **Pool 利用率**：`GET /admin/stats`

### 调试快照

`Manager.DebugSnapshot()` 安全获取 Session 调试信息（不暴露 mutex）：

```go
type DebugSessionSnapshot struct {
    TurnCount    int
    WorkerHealth worker.WorkerHealth
    HasWorker    bool
}
```

## GC 自动回收

后台 GC goroutine 按 `gc_scan_interval`（默认 60s）定期扫描：

| 检查项 | 条件 | 动作 |
|--------|------|------|
| Zombie 检测 | `RUNNING` Session 的 `LastIO()` 超过 `execution_timeout` | → TERMINATED |
| Max Lifetime | `expires_at ≤ now` | → TERMINATED |
| Idle Timeout | `idle_expires_at ≤ now` | → TERMINATED |

**注意**：`TERMINATED` Session 的记录不会被自动删除。它们作为 "resume 决策标记"，告诉 Gateway 之前的 Session 存在过，Worker 的 session 文件可能仍在磁盘上。物理删除应通过 Admin API 显式执行。
