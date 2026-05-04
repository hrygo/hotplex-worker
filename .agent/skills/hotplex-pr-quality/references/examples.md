# PR Quality Skill 使用示例

## 示例 1：标准 PR 创建流程

### 用户请求
```
开发完成了，帮我创建 PR
```

### AI 执行流程

#### 步骤 1：质量检查
```bash
$ make test
ok  	github.com/hrygo/hotplex/cmd/hotplex	1.033s
ok  	github.com/hrygo/hotplex/internal/worker/opencodeserver	1.064s
✓ Tests passed

$ make lint
0 issues.
```

#### 步骤 2：查看变更
```bash
$ git status
On branch fix/ocs-sse-timeout
Changes not staged for commit:
	modified:   internal/worker/opencodeserver/worker.go
	modified:   internal/worker/opencodeserver/singleton.go
```

#### 步骤 3：创建 commit
```bash
$ git add -A
$ git commit -m "fix(worker/ocs): resolve SSE timeout issues"
```

#### 步骤 4：推送代码
```bash
$ git push -u fork fix/ocs-sse-timeout
```

#### 步骤 5：创建 PR
```bash
$ gh pr create --repo hrygo/hotplex \
  --head aaronwong1989:fix/ocs-sse-timeout \
  --title "fix(worker/ocs): resolve SSE timeout issues" \
  --body "..."
```

#### 步骤 6：监控 CI
```bash
$ gh pr checks --watch
Test	pass	2m7s
Build	pass	44s
Coverage Check	pass	58s
```

**结果**：PR 创建成功，等待 review

---

## 示例 2：修复 CI 失败

### 用户请求
```
PR 的 CI 失败了，帮我分析并修复
```

### AI 执行流程

#### 步骤 1：查看 CI 状态
```bash
$ gh pr checks 96
Test	fail	2m3s
Build	pass	45s
```

#### 步骤 2：查看失败日志
```bash
$ gh run view 25209331075 --log-failed
```

**日志显示**：
```
--- FAIL: TestOpenCodeServerWorker_New (0.00s)
    worker_test.go:39: 
        	Error:      	Expected value not to be nil
        	Test:       	TestOpenCodeServerWorker_New
```

#### 步骤 3：分析原因
- 问题：测试期望 `w.singleton` 不为 nil
- 实际：`singleton.Load()` 在未初始化时返回 nil
- 原因：测试假设 singleton 已初始化，但没有调用 `InitSingleton()`

#### 步骤 4：修复测试
```go
func TestOpenCodeServerWorker_New(t *testing.T) {
	t.Parallel()
	w := New()

	require.NotNil(t, w)
	require.NotNil(t, w.BaseWorker)
	// Remove this assertion: singleton may be nil
	// require.NotNil(t, w.singleton)
	require.NotNil(t, w.client)
}
```

#### 步骤 5：重新提交
```bash
$ git add -A
$ git commit -m "test(opencodeserver): fix singleton nil check"
$ git push fork fix/ocs-sse-timeout
```

#### 步骤 6：验证 CI
```bash
$ gh pr checks --watch
Test	pass	2m7s
Build	pass	44s
```

**结果**：CI 全部通过 ✅

---

## 示例 3：完整的端到端流程

### 用户请求
```
请执行完整的 PR 创建流程
```

### AI 执行流程

#### 阶段 1：质量检查（1 分钟）
```bash
$ make test
✓ Tests passed

$ make lint
0 issues.

$ git diff --stat
internal/worker/opencodeserver/worker.go | 50 +++++++++++++++++++
internal/worker/opencodeserver/singleton.go | 34 ++++++++++--
```

**AI 反馈**：
```
✅ 质量检查通过
- 测试：全部通过
- Lint：零问题
- 修改：84 行新增，符合预期
```

#### 阶段 2：提交代码（30 秒）
```bash
$ git add -A
$ git commit -m "fix(worker/ocs): resolve SSE timeout and server startup issues"
```

**AI 创建的 commit message**：
```
fix(worker/ocs): resolve SSE timeout and server startup issues

Add separate sseClient without Timeout for SSE connections and use
cancellable context in readSSE for clean shutdown.

Fixes #85
Fixes #79

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

#### 阶段 3：推送代码（10 秒）
```bash
$ git push -u fork fix/ocs-sse-timeout
```

#### 阶段 4：创建 PR（20 秒）
```bash
$ gh pr create --repo hrygo/hotplex --title "..." --body "..."
```

**AI 创建的 PR 描述**：
```markdown
## Summary

This PR fixes two high-priority issues affecting OpenCode Server worker
stability and observability.

### Changes

- Add separate sseClient without Timeout for SSE connections
- Use cancellable context in readSSE for clean shutdown
- Fix serverErr channel consumption to prevent silent startup failure

## Test Plan

- [x] make test - All tests pass
- [x] make lint - Zero issues
- [x] Manual test: SSE connection remains responsive beyond 30s

## Related Issues

- Fixes #85 (SSE timeout)
- Fixes #79 (silent startup failure)
```

#### 阶段 5：监控 CI（5 分钟）
```bash
$ gh pr checks --watch
```

**CI 进度**：
```
1:00 - Test pending
2:00 - Test pass ✅
2:30 - Coverage Check pass ✅
```

#### 阶段 6：最终确认
```bash
$ gh pr checks 96
Test	pass	✅
Build	pass	✅
Coverage Check	pass	✅
Gate	pass	✅
```

**AI 总结**：
```
✅ PR 创建成功
- 核心检查全部通过
- PR #96 已就绪，等待 review

链接：https://github.com/hrygo/hotplex/pull/96
```

---

## 示例 5：增量推送更新现有 PR

### 用户请求
```
我刚才的 PR 还有 CI 失败，现在修复了，帮我推送
```

### AI 执行流程

#### 步骤 1：检查现有 PR
```bash
$ gh pr list --head aaronwong1989:fix-ocs-sse-timeout --repo hrygo/hotplex
#96  fix(worker/ocs): resolve SSE timeout issues  1 hour ago
```

**AI 反馈**：
```
✅ 检测到现有 PR #96
- 推荐：增量推送更新现有 PR
- GitHub 会自动更新 PR 并重新运行 CI
```

#### 步骤 2：质量检查
```bash
$ make test
ok  	github.com/hrygo/hotplex/internal/worker/opencodeserver	1.064s
✓ Tests passed

$ make lint
0 issues.
```

#### 步骤 3：提交修复
```bash
$ git add -A
$ git commit -m "test(opencodeserver): add singleton nil check"

**AI 生成的 commit message**：
```
test(opencodeserver): add singleton nil check

Remove nil assertion for singleton.Load() in test since
singleton may be nil in uninitialized state.

Fixes test failure in PR #96

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

#### 步骤 4：推送更新
```bash
$ git push fork fix-ocs-sse-timeout
```

**GitHub 自动更新 PR #96**
- CI 自动重新运行
- PR 描述自动更新（新增 commit）
- 无需手动操作

#### 步骤 5：监控 CI
```bash
$ gh pr checks --watch
Test	pass	2m7s  ✅
Build	pass	44s   ✅
Coverage Check	pass	58s   ✅
```

#### 步骤 6：确认更新
```bash
$ gh pr view 96
Open  aaronwong1989:fix-ocs-sse-timeout → hrygo:main

✅ 所有核心检查通过
✅ PR #96 已更新，等待 review
```

**结果**：现有 PR 成功更新，CI 全部通过

---

## 示例 6：PR 合并后清理

### 用户请求
```
PR 已经合并了，清理一下分支
```

### AI 执行流程

#### 步骤 1：确认 PR 状态
```bash
$ gh pr view 96 --json merged,mergedAt --jq '.merged'
true
```

#### 步骤 2：切换到 main
```bash
$ git checkout main
Switched to branch 'main'
Your branch is up to date with 'origin/main'.
```

#### 步骤 3：更新 main
```bash
$ git pull origin main
Already up to date.
```

#### 步骤 4：删除本地分支
```bash
$ git branch -d fix/ocs-sse-timeout
Deleted branch fix/ocs-sse-timeout (was 9b7fc35).
```

#### 步骤 5：删除远程分支
```bash
$ git push fork --delete fix-ocs-sse-timeout
To https://github.com/aaronwong1989/hotplex-1.git
 - [deleted]             fix-ocs-sse-timeout
```

#### 步骤 6：关闭相关 issues
```bash
$ gh issue close 85 --comment "✅ 已通过 PR #96 修复"
$ gh issue close 79 --comment "✅ 已通过 PR #96 修复"
```

**AI 总结**：
```
✅ 清理完成
- 本地分支已删除
- 远程分支已删除
- 相关 issues 已关闭
- 工作目录干净，可以开始下一个任务
```

---

## 使用技巧

### 技巧 1：批量监控多个 PR
```bash
# 监控所有 open PR
gh pr list --state open \
  | jq -r '.[] | .number' \
  | xargs -I {} gh pr checks {}
```

### 技巧 2：快速查看失败原因
```bash
# 只显示失败的检查
gh pr checks <number> \
  | jq -r '.[] | select(.status == "failure")'
```

### 技巧 3：自动修复常见 lint 问题
```bash
# 格式化代码
make fmt

# 重新检查
make lint
```

### 技巧 4：查看 PR 的所有评论
```bash
gh pr view <number> --json comments \
  | jq -r '.comments[] | .body'
```

### 技巧 5：查看 PR 的覆盖率变化
```bash
# 本地查看覆盖率
go tool cover -func=coverage.out | tail -1
```
