---
paths:
  - "**/*_test.go"
  - "**/testutil/**/*.go"
---

# 测试规范

> 断言库 / table-driven / race 检测规范 → 见 AGENTS.md 约定与规范

## 全局单例并发隔离（OCS Worker）

涉及 `atomic.Pointer` 全局单例（如 OCS `SingletonProcessManager`）的测试必须防止并发状态污染：

```go
t.Run("acquire then release", func(t *testing.T) {
    ResetSingletonForTest()  // 将 atomic.Pointer 置为 nil
    defer CleanSingletonForTest()

    sm := GetSingleton()
    err := sm.Acquire(context.Background())
    require.NoError(t, err)
    sm.Release()
})
```

**并发模式下禁用**：`if testing.Short() { t.Skip("skipping singleton test in short mode") }`

**禁止**：
- 在 `t.Parallel()` 子测试中共享同一单例引用（除非测试的是单例自身行为）
- 跨测试修改全局单例后不还原，导致后续测试被污染

## 资源清理

```go
// 使用 t.Cleanup() 确保资源释放
db, err := sql.Open("sqlite", ":memory:")
require.NoError(t, err)
t.Cleanup(func() { db.Close() })

// 临时目录
dir := t.TempDir()  // 自动清理，无需 t.Cleanup
```

## 测试工具

```
internal/gateway/testutil/  — WebSocket mock helpers（MockConn, WriteEnvelope, ReadEnvelope）
internal/messaging/mock/    — Mock messaging adapter（bridge/handler 集成测试）
```

## E2E 测试

- `e2e/` 目录：端到端集成测试
- 需要 gateway 运行的测试用 `// +build e2e` 或短 flag 跳过
