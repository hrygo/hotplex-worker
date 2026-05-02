# Phase 4: 详细实施指南

本文档提供 Phase 4: Implementation & Delivery 的完整实施细节。

## 4.1 准备仓库

```bash
cd $(git rev-parse --show-toplevel)
git fetch origin main
git checkout main
git pull origin main
```

确保工作目录干净，基于最新的 main 分支。

## 4.2 创建批量分支

**命名约定**: `batch/<theme>-issues-<numbers>`

```bash
git checkout -b batch/messaging-cli-fixes-issues-90-78-89-88
```

**为什么描述性名称很重要**：
- 易于理解分支包含的内容
- 易于在 git 历史中查找
- 在写 PR 标题前就清晰了

## 4.3 按顺序实施 Issues

对每个 issue 重复以下步骤：

### 步骤 1: 理解 Issue

```bash
gh issue view <number> --comments
```

仔细阅读。理解问题。如果不清楚，在实施前在 issue 评论中询问。

### 步骤 2: 实施修复

遵循 HotPlex 开发标准有助于保持代码质量：

- **Go 1.26+** — 使用最新语言特性
- **golangci-lint** — 频繁运行，立即修复问题（尽早发现问题）
- **测试优先** — 实施前编写测试（尽可能 TDD）
  - **为什么**：测试作为可执行规范并防止回归
- **表驱动测试** — 标准 Go 模式
- **≥80% 覆盖率** — 安全/关键路径更高
  - **为什么**：高覆盖率让更改按预期工作的信心

### 步骤 3: 使用 Conventional Commits 提交

```bash
git commit -m "refactor(messaging): extract BaseAdapter to eliminate duplication

- Extract common adapter logic into BaseAdapter struct
- Reduce ~300 lines of duplication between Slack and Feishu
- Improve testability and maintainability
- Add table-driven tests for adapter methods

Fixes #88"
```

**Commit message 结构**：
- **Type**: `refactor`, `fix`, `feat`, `perf`, `docs`
- **Scope**: `messaging`, `cli`, `webchat` 等
- **Subject**: 简短描述（祈使语气）
- **Body**: 详细更改说明
- **Footer**: 引用 issue（Fixes #XX）

**为什么每个 issue 一个提交**（这种模式回报丰厚）：
- **原子更改** — 每个提交独立有效，可以单独合并
- **清晰历史** — git bisect 有效，未来开发人员理解更改了什么
- **易于回滚** — 如果一个修复有问题，可以只回滚该提交而不丢失其他
- **逻辑分组** — 相关更改保持在一起，讲述连贯的故事

### 步骤 4: 验证

```bash
make lint
make test
go test -coverprofile=/tmp/coverage.out ./...
```

**不要每次提交后推送** — 先实施所有 issues，然后推送一次。

## 4.4 最终集成测试

所有 issues 实施后：

```bash
# 运行完整测试套件
make test

# 运行 linter
make lint

# 构建项目
make build

# Smoke test
./hotplex version
./hotplex --help
```

**为什么集成测试很重要**：单元测试通过不意味着系统工作。集成测试捕获：
- 模块交互 bug
- 配置问题
- 运行时问题
- 性能回归

## 4.5 推送批量分支

```bash
git push origin batch/messaging-cli-fixes-issues-90-78-89-88
```

## 4.6 创建合并 PR

创建**一个 PR**关闭所有选定的 issues：

```bash
gh pr create \
  --base main \
  --title "Batch: fixes and improvements for issues #90, #78, #89, #88" \
  --body "$(cat <<'EOF'
## Summary

This PR implements 4 high-priority issues in a consolidated batch:

- **#90**: Add `hotplex update` subcommand for self-update
- **#78**: Fix error handling and concurrency in messaging adapters
- **#89**: Optimize webchat bundle with code splitting and virtualization
- **#88**: Refactor messaging adapters to extract BaseAdapter

## Fixes

Closes #90, #78, #89, #88

## Changes by Issue

### #88 — Refactor messaging adapters

**Problem**: Slack and Feishu adapters have ~300 lines of duplicated code.

**Solution**: Extract common logic into BaseAdapter struct.

**Changes**:
- Extract BaseAdapter with common connection, message handling, events
- Reduce duplication from ~300 lines to ~50 lines
- Add comprehensive table-driven tests
- Improve error handling consistency

**Impact**: Improved maintainability, easier to add new platforms.

### #78 — Fix error handling in messaging adapters

**Problem**: Silent error swallowing, inconsistent error propagation.

**Solution**: Implement structured error handling with proper propagation.

**Changes**:
- Wrap errors with context using fmt.Errorf
- Propagate errors to Session level
- Add error type classification
- Improve logging with structured fields

**Impact**: Better debugging, fewer silent failures.

### #90 — Add self-update subcommand

**Problem**: Users need manual update process.

**Solution**: Add `hotplex update` subcommand for automated updates.

**Changes**:
- Add update subcommand to CLI
- Implement binary download and replace logic
- Add version check and verification
- Support rollback capability

**Impact**: Improved user experience, easier deployments.

### #89 — Optimize webchat bundle

**Problem**: Large bundle size (2.3MB), slow initial load.

**Solution**: Code splitting and virtualization.

**Changes**:
- Implement React.lazy for component splitting
- Add virtual scrolling for long lists
- Optimize dependency tree
- Reduce bundle to ~800KB

**Impact**: 65% size reduction, faster load times.

## Testing

- [x] All unit tests pass (make test)
- [x] Linter passes (make lint)
- [x] Integration tests pass
- [x] Manual testing completed:
  - [x] Slack adapter connection and messaging
  - [x] Feishu adapter connection and messaging
  - [x] `hotplex update` command works correctly
  - [x] Webchat loads and functions properly
  - [x] Error scenarios handled correctly

## Performance Impact

- **#89**: Webchat bundle reduced from 2.3MB to 800KB (65% reduction)
- **#78**: No performance regression, improved error handling
- **#88**: No runtime performance impact
- **#90**: Update subcommand adds minimal CLI overhead

## Breaking Changes

None. All changes are backwards compatible.

## Checklist

- [x] Code follows project style guidelines
- [x] All tests pass (≥80% coverage)
- [x] Linter passes (golangci-lint)
- [x] Documentation updated (if needed)
- [x] Commits follow conventional commit format
- [x] Each commit atomic and independently valid
- [x] PR description references all closed issues

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

## PR 模板说明

### Summary 部分
- 简短描述（2-3 句话）
- 列出所有 issues 及其简短描述

### Fixes 部分
- 使用 `Closes #XX, #YY, #ZZ` 格式
- 这会在合并时自动关闭这些 issues

### Changes by Issue 部分
- 每个 issue 一个子章节
- 包含：Problem, Solution, Changes, Impact
- 让审查者快速理解每个更改

### Testing 部分
- 列出所有测试类型
- 包含手动测试清单
- 展示测试完整性

### Performance Impact 部分
- 量化性能改进（如适用）
- 说明无回归

### Breaking Changes 部分
- 明确说明是否有破坏性更改
- 如果有，提供迁移指南

### Checklist 部分
- 标准化清单
- 确保所有质量检查完成
