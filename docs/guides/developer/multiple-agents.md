---
title: 多 Agent 协作
weight: 15
description: HotPlex Worker 类型、并发 Session 与多项目工作流指南
---

# 多 Agent 协作

> HotPlex 的 Worker 类型、并发 Session、多项目工作流指南

## 概述

HotPlex Gateway 支持同时运行多个 AI Worker 实例，通过 Session 隔离和 PoolManager 配额控制实现安全的多路复用。理解 Worker 类型和资源模型是高效使用多 Agent 的关键。

## Worker 类型

### Claude Code（`claude_code`）

- **进程模型**：per-session，每个 Session 独立 fork 一个 `claude` 进程
- **通信方式**：stdio（`--print --session-id`）
- **生命周期**：与 Session 绑定，Session 终止时进程随之终止
- **特点**：
  - 功能最完整（文件操作、代码执行、MCP server、tool 调用）
  - 支持对话 resume（`--resume`）
  - 内存占用较高（~512MB per process，通过 RLIMIT_AS 限制）
  - 通过 PGID（Linux/macOS）或 Job Object（Windows）实现进程隔离

### OpenCode Server（`opencode_server`）

- **进程模型**：单例模式，全局共享一个 `opencode` 进程
- **通信方式**：HTTP + SSE（内置 server）
- **生命周期**：首次使用时启动，全局复用
- **特点**：
  - 轻量级任务（问答、简单代码生成、文本处理）
  - 多 Session 共享同一进程，资源开销低
  - 通过 `atomic.Pointer` 实现并发安全的单例管理
  - 不支持 per-session 进程隔离

### 如何选择

| 场景 | 推荐 Worker | 原因 |
|------|------------|------|
| 代码编写/重构 | `claude_code` | 完整的文件操作和代码执行能力 |
| 长对话/复杂项目 | `claude_code` | 支持 resume，上下文持久化 |
| 快速问答 | `opencode_server` | 轻量、响应快 |
| 大量并发用户 | `opencode_server` | 单进程共享，资源开销小 |
| 需要 MCP server | `claude_code` | 完整的 MCP 支持 |

## Hot-Multiplexing

HotPlex 的核心设计理念：**单个 Worker 进程服务多个对话轮次**。

### 工作原理

1. Worker 进程在 Session `RUNNING` 状态时保持活跃
2. 用户 input → Worker 执行 → `done` 事件 → Session 进入 `IDLE`
3. 下一个 input → Session 恢复 `RUNNING` → **复用同一个 Worker 进程**
4. 只有 `/gc`、`/reset` 或超时才会终止 Worker

### 资源优势

```
传统模式：每次 input → fork 新进程 → 执行 → 终止
HotPlex：首次 input → fork → 执行 → IDLE → 复用 → 执行 → ... → /gc → 终止
```

避免频繁 fork 的开销（进程创建、模型加载、context 重建），Turn 间延迟接近零。

## 多项目工作流

### 使用 /cd 切换项目

`/cd <path>` 命令通过切换工作目录实现项目切换：

```
# 切换到项目 A
/cd ~/projects/frontend-app

# 在项目 A 中工作...
请帮我重构 login 组件

# 切换到项目 B
/cd ~/projects/backend-api

# 在项目 B 中工作...
检查一下数据库连接池配置
```

**底层机制**：
1. 安全验证路径（防止路径穿越）
2. 基于新目录派生新 Session ID（`DeriveSessionKey`）
3. 终止旧 Worker，创建新 Session
4. 自动注入最后的用户输入到新 Session

### 每个 Session 独立工作目录

`SessionInfo.WorkDir` 持久化每个 Session 的工作目录。Resume 时 Worker 在正确的目录下启动，保持项目上下文。

## 并发 Session 管理

### PoolManager 配额

```yaml
# config.yaml
pool:
  max_size: 20              # 全局最大 Worker 数
  max_idle_per_user: 5      # 单用户最大 Session 数
  max_memory_per_user: 0    # 单用户最大内存（0=无限，每 Worker ~512MB）
```

### 配额行为

当达到配额限制时：

| 错误 | HTTP 状态 | 客户端行为 |
|------|----------|-----------|
| `POOL_EXHAUSTED` | 503 | 等待其他 Session 释放 |
| `USER_QUOTA_EXCEEDED` | 429 | 减少 Session 数量或联系管理员 |
| `MEMORY_EXCEEDED` | 429 | 减少 Session 数量 |

### 动态配额调整

PoolManager 支持运行时热更新配额（通过配置热重载）：

```go
pool.UpdateLimits(newMaxSize, newMaxIdlePerUser)
```

**注意**：缩小配额不会驱逐已有 Session，只会拒绝新的 Acquire 请求。

## 资源考虑

### 内存估算

| 组件 | 估算内存 |
|------|---------|
| Claude Code Worker | ~512MB（RLIMIT_AS） |
| OpenCode Server（单例） | ~512MB（共享） |
| Gateway 进程 | ~100MB |
| SQLite | ~50MB |

### 进程隔离

每个 Claude Code Worker 进程通过以下机制隔离：

- **POSIX**：PGID（Process Group ID），确保 SIGTERM/SIGKILL 能终止整个进程树
- **Windows**：Job Object，进程退出时自动清理子进程
- **资源限制**：`RLIMIT_AS` 限制虚拟内存（默认 512MB）

### 并发安全保证

- **锁顺序**：`Manager.mu → managedSession.mu`（固定顺序防死锁）
- **CAS Worker 替换**：`DetachWorkerIf(expected)` 防止过期 goroutine 覆盖新 Worker
- **原子 Turn 计数**：`TransitionWithInput` 在同一 mutex 内完成状态转换 + input 处理

## 最佳实践

1. **优先 /gc 而非 /reset**：保留对话历史，Resume 时无需重建上下文
2. **设置合理的 max_turns**：防止无限循环消耗资源
3. **监控 Pool 利用率**：通过 Admin API 的 `/admin/pool/stats` 端点
4. **单用户限制**：根据实际内存设置 `max_memory_per_user`
5. **开发环境**：`max_size` 设为 5-10，避免开发机资源耗尽
6. **生产环境**：根据服务器内存计算 `max_size`（每个 Worker ~512MB）

---

## 延伸阅读

- [Session 生命周期](../../explanation/session-lifecycle.md) — 5 状态机、UUIDv5 Key 派生、GC 策略的设计原理
