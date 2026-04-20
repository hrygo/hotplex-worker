---
paths:
  - "**/worker/**/*.go"
  - "**/session/**/*.go"
---

# 进程管理规范

## 启动
```go
cmd := exec.CommandContext(ctx, binary, args...)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // PGID 隔离
cmd.Env = append(os.Environ(), extraEnv...)
// 移除 CLAUDECODE= 防止嵌套
```

### 内存限制设置

**平台兼容性**：
```go
// RLIMIT_AS 只在支持的平台上设置
if runtime.GOOS != "darwin" && cmd.Process != nil {
    const memLimit = 512 * 1024 * 1024 // 512 MB
    if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
        Cur: memLimit,
        Max: memLimit,
    }); err != nil {
        m.log.Warn("proc: setrlimit RLIMIT_AS failed", "error", err)
        // Non-fatal: log and continue
    }
}
```

**平台差异**：
- **Linux/POSIX**: 支持 `RLIMIT_AS`，限制进程地址空间
- **macOS (Darwin)**: 不支持 `RLIMIT_AS`（实现不符合 POSIX）
  - 调用会返回 `EINVAL` (invalid argument)
  - 通过 `runtime.GOOS != "darwin"` 检测并跳过
- **其他平台**: Windows 不支持 POSIX `setrlimit`

**设计原则**：
- 内存限制是**优化特性**，失败不阻止进程启动
- 警告级别日志，不中断流程
- 平台检测优先于错误处理

**相关文件**：`internal/worker/proc/manager.go:138`

## Stdin / Stdout
- stdin 写入：JSON + `\n`
- stdout：`bufio.Scanner`，初始 64KB，上限 10MB（超限 → `WORKER_OUTPUT_LIMIT` error）

## 分层终止（必须严格遵循）
1. `syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)` — 优雅终止
2. 等待最多 5s
3. `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)` — 强制终止

## Goroutine 泄漏防护
每个启动的 goroutine 必须有明确退出路径：
- ctx cancel：`select { case <-ctx.Done(): return; default: }`
- channel close：sender 关闭，receiver 用 `range` 或 `for v := range ch`
- `sync.WaitGroup`：启动时 `wg.Add(1)`，退出时 `wg.Done()`

## exec.Cmd 清理
```go
// 存活判断
cmd.ProcessState == nil  // true = 存活

// 兜底清理（defer）
defer func() {
    if cmd.ProcessState == nil {
        _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
    }
}()
```

---

## InputRecoverer 接口

Worker crash recovery 需要从已死 Worker 提取最后输入用于重投递：

```go
// worker.go
type InputRecoverer interface {
    LastInput() string
}

// base.Conn 实现 — 记录每次 Send 的内容
func (c *Conn) Send(ctx context.Context, env Envelope) error {
    c.mu.Lock()
    if env.Event.Type == events.KindInput {
        c.lastInput = extractContent(env)
    }
    c.mu.Unlock()
    // ... 实际发送
}
```

**使用场景**：
- Worker 崩溃后 bridge 通过 `conn.(worker.InputRecoverer)` 提取 `LastInput()`
- Fresh start fallback 时将最后输入重投递到新 Worker
- 确保 crash recovery 后用户不丢失最后一次操作
