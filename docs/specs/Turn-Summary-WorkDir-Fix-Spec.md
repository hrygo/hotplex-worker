# Turn Summary WorkDir/GitBranch Fix Spec

> 版本: v1.0
> 日期: 2026-05-04
> 状态: Draft
> 关联: #117 (Turn Summary), `Session-Stats-Spec.md`

---

## 1. 问题描述

Turn Summary 的 Environment 行（`📂 Dir · 🌿 Branch · ⏳ Session`）始终缺少 `📂 Dir` 和 `🌿 Branch`，仅显示 `⏳ Session Duration`。

### 根因

`sessionAccumulator.WorkDir` 和 `GitBranch` 从未被赋值，由两个 bug 叠加导致：

**Bug 1: `getOrInitAccum` 对已存在的 accumulator 不更新字段**

```go
// bridge_forward.go:458
func (b *Bridge) getOrInitAccum(sessionID, workDir string) *sessionAccumulator {
    if acc, ok := b.accum[sessionID]; ok {
        return acc  // ← 直接返回，即使 acc.WorkDir 为空且 workDir 非空
    }
    // ... 仅在首次创建时设置 WorkDir/GitBranch
}
```

**Bug 2: `StartSession` 和 crash fallback路径未传递 workDir**

| 调用路径 | forwardOpts.workDir | 问题 |
|----------|---------------------|------|
| `StartSession` (bridge.go:142) | `nil` → `""` | 未传递 workDir |
| `ResumeSession` (bridge.go:174) | `workDir` | ✓ |
| `ResetSession` (bridge.go:398) | `forwardOpts{}` → `""` | OK（accumulator 已有旧值） |
| LLM Retry (bridge_worker.go:133) | `p.workDir` | ✓ |
| Fresh start fallback (bridge_worker.go:174) | `nil` → `""` | 未传递 workDir |

### 事件时序分析

典型首次 Slack 会话：

```
StartSession → forwardOpts{workDir:""} → forwardEvents opts.workDir=""
  ToolCall → getOrInitAccum(sid, "") → 创建 accumulator{WorkDir:""}
  Done     → getOrInitAccum(sid, "") → 已存在 → 返回 → WorkDir=""
  snapshot() → work_dir="", git_branch="" → 格式化跳过
```

即使 `Done` 传递了 `opts.workDir`（当前为空），`getOrInitAccum` 也不会更新已存在的 accumulator。

---

## 2. 修复方案

### Fix 1: `getOrInitAccum` 惰性更新

**文件**: `internal/gateway/bridge_forward.go:458-471`

accumulator 已存在时，若 `workDir` 非空且 `acc.WorkDir` 为空，更新 `WorkDir` 和 `GitBranch`：

```go
func (b *Bridge) getOrInitAccum(sessionID, workDir string) *sessionAccumulator {
    b.accumMu.Lock()
    defer b.accumMu.Unlock()
    if acc, ok := b.accum[sessionID]; ok {
        if workDir != "" && acc.WorkDir == "" {
            acc.WorkDir = workDir
            acc.GitBranch = gitBranchOf(workDir)
        }
        return acc
    }
    acc := &sessionAccumulator{StartedAt: time.Now()}
    if workDir != "" {
        acc.WorkDir = workDir
        acc.GitBranch = gitBranchOf(workDir)
    }
    b.accum[sessionID] = acc
    return acc
}
```

**为什么需要惰性更新**：ToolCall 事件先于 Done 到达，以空 workDir 创建 accumulator。Done 携带 `opts.workDir` 到达时，需补填。

### Fix 2: `StartSession` 传递 workDir

**文件**: `internal/gateway/bridge.go` StartSession 内的 `createAndLaunchWorker` 调用

```go
b.createAndLaunchWorker(workerLaunchParams{
    ctx:         ctx,
    wt:          wt,
    workerInfo:  workerInfo,
    platform:    platform,
    botID:       botID,
    forwardOpts: &forwardOpts{workDir: workDir}, // ← 新增
}, ...)
```

### Fix 3: Fresh start fallback 传递 workDir

**文件**: `internal/gateway/bridge_worker.go:174` `attemptResumeFallback` 内

```go
w, err := b.createAndLaunchWorker(workerLaunchParams{
    ctx:         context.Background(),
    wt:          si.WorkerType,
    workerInfo:  workerInfo,
    platform:    si.Platform,
    botID:       si.BotID,
    forwardOpts: &forwardOpts{workDir: p.workDir}, // ← 新增
}, ...)
```

### 三处修复的必要性

| 修复 | 缺失时影响 |
|------|-----------|
| Fix 1 | ToolCall 先创建 accumulator，Done 携带 workDir 无法更新 |
| Fix 2 | 初始 session 的 `opts.workDir` 始终为空，Fix 1 的惰性更新无数据可填 |
| Fix 3 | Worker crash 后 fresh start 重建，accumulator 丢失 workDir |

### 修复后预期时序

```
StartSession → forwardOpts{workDir:"/path"}
  ToolCall → getOrInitAccum(sid, "") → 创建 accumulator{WorkDir:""}
  Done     → getOrInitAccum(sid, "/path") → 已存在, 惰性更新 → WorkDir="/path"
  snapshot() → work_dir="/path", git_branch="feat/xxx"
  FormatTurnSummaryRich → "📂 hotplex · 🌿 feat/xxx · ⏳ 3m42s"
```

---

## 3. 文件影响

| 文件 | 变更 | 行数 |
|------|------|------|
| `internal/gateway/bridge_forward.go` | `getOrInitAccum` 惰性更新 | ~3 行新增 |
| `internal/gateway/bridge.go` | StartSession 传递 forwardOpts | ~1 行修改 |
| `internal/gateway/bridge_worker.go` | Fresh start fallback 传递 forwardOpts | ~1 行修改 |
| `internal/gateway/bridge_test.go` | 新增惰性更新测试 | ~20 行新增 |

**总计**: ~25 行变更，0 行删除。

---

## 4. 测试计划

### 4.1 单元测试

| 测试 | 场景 |
|------|------|
| `TestGetOrInitAccum_LazyUpdate` | 已有 accumulator + WorkDir 为空 + workDir 非空 → 更新 WorkDir/GitBranch |
| `TestGetOrInitAccum_NoOverwrite` | 已有 accumulator + WorkDir 非空 + workDir 不同 → 不覆盖 |
| `TestGetOrInitAccum_EmptyWorkDir` | 已有 accumulator + workDir 为空 → 不更新 |

### 4.2 集成验证

1. `make quality` 全量通过
2. 现有 `TestGetOrInitAccum` / `TestInjectSessionStats` 不受影响

### 4.3 手动验证

通过 Slack 发起对话，检查 Done 后 Turn Summary 是否显示：
```
🔄 #1 · 🤖 Sonnet · 🧠 24% · ⏱ 42s · 🔧 5 (Bash×3, Read×2)
💎 in 45.2K · out 3.8K
📂 hotplex · 🌿 main · ⏳ 42s
```

---

## 5. 附带发现：accumulator 内存泄漏

`b.accum` map 只增不删，session 终止后不清理。长期运行网关的 accumulator 数量等于历史 session 总数。

**建议**：在 session TERMINATED/DELETED 时 `delete(b.accum, sessionID)`。作为独立优化处理，不纳入本次修复。
