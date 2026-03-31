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
