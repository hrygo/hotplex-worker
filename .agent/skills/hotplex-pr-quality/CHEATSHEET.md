# PR Quality Cheatsheet

## 快速命令

### 质量检查
```bash
make test           # 运行测试
make lint           # 代码规范检查
git status          # 查看修改状态
git diff --stat     # 查看修改统计
```

### 提交代码
```bash
git add <files>                                    # 暂存文件
git commit -m "type(scope): subject"              # 提交
git push -u fork <branch>                          # 推送到 fork
```

### 创建 PR
```bash
gh pr create \
  --repo hrygo/hotplex \
  --head aaronwong1989:fix-xxx \
  --title "feat(scope): description" \
  --body "$(cat pr.md)"
```

### 监控 CI
```bash
gh pr checks --watch                              # 实时监控
gh pr view <number> --json checks                 # 查看状态
gh run view <run-id> --log-failed                 # 查看失败日志
```

## Commit Message 模板

```bash
git commit -m "$(cat <<'EOF'
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Changes:
- Add sseClient field to SingletonProcessManager
- Use cancellable context in readSSE
- Call sseCancel in Terminate/Kill

Fixes #85
Fixes #79

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

## PR 描述模板

```markdown
## Summary

修复 OpenCode Server worker 的 SSE 超时和服务器启动问题。

### Changes

- 添加独立的 sseClient（无 timeout）用于 SSE 长连接
- 使用可取消的 context 实现 readSSE 优雅关闭
- 修复 serverErr channel 未消费导致的静默启动失败

## Test Plan

- [x] `make test` - All tests pass
- [x] `make lint` - Zero issues
- [x] Manual test: SSE 连接保持超过 30s
- [x] Manual test: Terminate/Kill 解除 goroutine 阻塞

## Related Issues

- Fixes #85 (SSE timeout)
- Fixes #79 (silent startup failure)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

## CI 检查优先级

| 检查项 | 优先级 | 失败处理 |
|--------|--------|----------|
| Test | P0 | 必须修复 |
| Build | P0 | 必须修复 |
| Coverage Check | P1 | 通常需要修复 |
| codecov/patch | P2 | 可协商 |
| codecov/project | P2 | 可协商 |

## Codecov 决策树

```
codecov 失败
  │
  ├─ 存在实质性障碍？
  │   ├─ 是 → 评估 ROI
  │   │        ├─ 高 ROI → 添加测试
  │   │        └─ 低 ROI → 接受失败，说明原因
  │   └─ 否 → 添加测试
  │
  ├─ 影响核心功能？
  │   ├─ 是 → 必须修复
  │   └─ 否 → 可协商
  │
  └─ 覆盖率下降幅度？
      ├─ > 5% → 通常需要修复
      └─ < 5% → 可接受
```

## 实质性障碍判断

### ✅ 可接受的障碍

- 需要真实外部服务（HTTP server、数据库）
- Mock 测试成本过高
- 集成测试不稳定
- ROI < 5% 覆盖率提升 / 1 小时投入

### ❌ 不可接受的障碍

- 纯计算逻辑未测试
- 简单的函数未覆盖
- 核心业务逻辑缺失测试

## 常见 CI 错误处理

### Test 失败
```bash
# 查看详细日志
gh run view <run-id> --log-failed

# 常见原因
- 编译错误 → 修复语法/类型/导入
- 测试超时 → 检查死锁/无限循环
- 断言失败 → 修复逻辑或更新测试
```

### Lint 失败
```bash
# 常见问题
- gofmt → make fmt
- goimports → make fmt
- unused → 删除未使用代码
- errorlint → 使用 %w 包装错误
```

### Codecov 失败
```bash
# 查看报告
gh pr view <pr-number> --json comments \
  | jq '.comments[] | select(.body | contains("Codecov")) | .body'

# 添加测试
go test -v -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep "未覆盖的函数"
```

## PR 创建后流程

```
创建 PR
  ↓
等待 CI（3-5 分钟）
  ↓
┌─ CI 全部通过 → 通知 reviewer
└─ CI 失败 → 修复 → 推送 → 自动重新运行
```

## 分支清理

```bash
# PR 合并后
git checkout main
git pull origin main
git branch -d <branch>
git push fork --delete <branch>
gh issue close <number> --comment "已通过 PR #<pr> 修复"
```

## 快速诊断

### 查看修改文件
```bash
git status                  # 简短列表
git diff --stat             # 统计信息
git diff <file>             # 详细差异
```

### 查看 PR 状态
```bash
gh pr list                  # PR 列表
gh pr view <number>          # PR 详情
gh pr checks <number>        # CI 状态
gh pr comments <number>      # 评论列表
```

### 查看 CI 日志
```bash
gh run list                  # 运行列表
gh run view <id>             # 运行详情
gh run view <id> --log       # 完整日志
gh run view <id> --log-failed # 失败日志
```

## 时间参考

| 操作 | 预计时间 |
|------|----------|
| make test | 2-3 分钟 |
| make lint | 30 秒 |
| git push | 10 秒 |
| CI 运行 | 3-5 分钟 |
| codecov 分析 | 30 秒 |
| **总计** | **约 5 分钟** |

## 注意事项

1. **提交前检查**
   - ✅ 确保测试通过
   - ✅ 确保 lint 通过
   - ✅ 确认修改的文件

2. **Commit message**
   - ✅ 使用中文
   - ✅ 技术术语用英文
   - ✅ 包含 Co-Authored-By

3. **PR 描述**
   - ✅ Summary 简洁
   - ✅ Test Plan 完整
   - ✅ 关联 Issues

4. **CI 失败**
   - ✅ 优先修复 Test/Build
   - ✅ codecov 可协商
   - ✅ 说明原因
