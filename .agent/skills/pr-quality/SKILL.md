---
name: pr-quality
version: 2.0.0
description: "HotPlex 项目 PR 质量保证与 CI 达标助手。自动执行完整的质量检查、代码提交、PR 创建/更新和 CI 监控流程。**使用此 skill**：开发完成、提交代码、创建 PR、更新 PR、创建 commit、推送代码、CI 失败、codecov 覆盖率、测试失败、lint 错误、分支管理、git 合并、fork 仓库、增量推送。**HotPlex 专用**，支持多 channel (Slack/飞书/WebChat)、多 worker (CC/OCS/Pi)、跨平台兼容 (Linux/macOS/Windows)。"
metadata:
  requires:
    bins: ["gh", "git"]
    env:
      - GITHUB_TOKEN
  cliHelp: "gh pr --help"
  project: hotplex
---

# HotPlex PR 质量保证与 CI 达标助手

这个 skill 是 **HotPlex 项目专用** 的 PR 质量助手，帮助你在开发完成后创建高质量的 Pull Request，并确保所有 CI 检查通过。

**为什么需要这个 skill？**
HotPlex 是一个复杂的分布式系统，支持多 channel（Slack/飞书/WebChat）、多 worker（CC/OCS/Pi）、跨平台兼容（Linux/macOS/Windows）。手动创建 PR 容易遗漏关键检查（跨平台测试、多 worker 兼容性等），而这个 skill 确保每次都遵循 HotPlex 的最佳实践。

**适用范围**：HotPlex 项目（hrygo/hotplex）的所有贡献者。

## HotPlex 架构特性

HotPlex 是一个支持多 channel、多 worker 和跨平台兼容的分布式 AI Agent Gateway。

**多 Channel**：Slack Socket Mode、飞书 WebSocket (P2)、WebChat
**多 Worker**：Claude Code (CC)、OpenCode Server (OCS)、Pi-mono
**跨平台**：Linux、macOS、Windows 三平台 CI 必须通过

详见：[references/architecture.md](references/architecture.md)

## 前置条件检查

在开始之前，skill 会自动检查：

1. **Git 仓库状态**：未提交的修改、当前分支、fork 远程仓库
2. **工具可用性**：`gh` CLI、Git 仓库
3. **Fork 仓库配置**：自动检测 fork 远程仓库名称、推断用户名、验证 upstream

## 核心流程

### 阶段 1：质量检查

**为什么质量检查很重要？**
HotPlex 项目要求所有 PR 必须通过跨平台测试和 lint 检查。

skill 会自动：

1. **运行测试**：`make test`（含跨平台测试）
2. **运行 lint**：`make lint`（含跨平台检查）
3. **分析架构影响**：检查修改影响的 channel/worker/平台

**如果测试或 lint 失败**，skill 会显示错误日志、分析失败原因、提供修复建议。

### 阶段 2：提交代码

**为什么规范的 commit message 很重要？**
HotPlex 项目使用 Conventional Commits 规范，scope 应该反映影响的架构组件。

skill 会自动：

1. **生成规范的 commit message**
   - 根据 HotPlex 的 commit 规范生成
   - 根据修改内容推断 type 和 scope（架构感知）
   - 生成中文描述（技术术语用英文）
   - 包含 Co-Authored-By 声明

2. **暂存并提交代码**

**Commit message 格式**（HotPlex 标准）：

```
<type>(<scope>): <subject>

<body>

<footer>

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

**Type 类型**：feat, fix, refactor, perf, test, docs, style, chore

**Scope 类型**（架构感知）：

**Gateway 核心**：`gateway`、`session`、`config`、`security`
**Channel**：`messaging/slack`、`messaging/feishu`、`webchat`
**Worker**：`worker/cc`、`worker/ocs`、`worker/pi`
**平台**：`cli`、`service`、`build`

**示例**：
```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 阶段 3：推送代码

**为什么需要自动检测 fork 仓库？**
HotPlex 项目使用 Fork & PR 工作流，每个贡献者的 fork 仓库名称可能不同。

skill 会自动：

1. **检测 fork 远程仓库**：`git remote -v`
2. **识别你的 fork**：查找包含你用户名的远程仓库（`fork`、`origin`、`upstream` 等）
3. **检查现有 PR**：
   ```bash
   gh pr list --head <your-username>:<branch> --repo hrygo/hotplex
   ```
   - 如果找到现有 PR：询问用户是更新现有 PR 还是创建新 PR
   - 如果没有找到 PR：继续创建新 PR

4. **推送代码**：
   ```bash
   git push -u <fork-remote> <branch>
   ```
   - 如果有现有 PR：GitHub 会自动更新 PR
   - 如果没有 PR：准备创建新 PR

**现有 PR 处理**：

如果检测到当前分支已有 PR：
- **选项 1**（推荐）：增量推送更新现有 PR
  - 直接推送代码
  - GitHub 自动更新 PR
  - CI 自动重新运行
  
- **选项 2**：创建新 PR
  - 需要先关闭或重命名现有 PR
  - 创建新的分支和 PR

### 阶段 4：创建或更新 Pull Request

**为什么需要 PR 模板？**
规范的 PR 描述帮助 reviewer 理解变更影响范围（channel/worker/平台）。

**场景 1：更新现有 PR**

skill 会自动：
1. 确认推送成功
2. GitHub 自动更新 PR
3. CI 自动重新运行
4. 无需额外操作

**场景 2：创建新 PR**

skill 会自动：
1. **生成 PR 描述**（含架构影响标注）
2. **创建 PR**：`gh pr create --repo hrygo/hotplex`

参数自动推断：`--repo`（固定 hrygo/hotplex）、`--head`（从你的 fork 用户名和分支推断）、`--title`（commit subject）、`--body`（生成的描述）

**PR 描述模板**（HotPlex 标准）：

```markdown
## Summary

<!-- 2-3 句话描述 PR 目的 -->

### Changes

- Change 1 (影响的 channel/worker/平台)
- Change 2

**架构影响**：
- Channel: Slack / 飞书 / WebChat / N/A
- Worker: CC / OCS / Pi / N/A
- 平台: Linux / macOS / Windows / 跨平台

## Test Plan

- [x] make test - Linux/macOS/Windows CI 通过
- [x] make lint - 零问题
- [ ] Channel 测试: 描述测试的 channel
- [ ] Worker 测试: 描述测试的 worker
- [ ] 手动测试: 描述手动测试步骤

## Related Issues

- Fixes #123
- Closes #456
```

### 阶段 5：监控 CI 状态

**为什么需要智能监控？**
HotPlex 的 CI 检查很多，特别是跨平台 CI。不是所有失败都需要修复。

skill 会自动：

1. **监控 CI 运行**：等待 CI 完成（3-5 分钟）
2. **分析 CI 结果**：
   - **核心检查**（必须通过）：Test（三平台）、Build（三平台）、Coverage Check
   - **次要检查**（可协商）：codecov/patch、codecov/project

3. **智能处理 codecov 失败**

**判断逻辑**（基于 HotPlex 项目实战经验）：

```
codecov 失败
  │
  ├─ 存在实质性障碍？
  │   ├─ 是 → 评估 ROI
  │   │        ├─ 高 ROI → 添加测试
  │   │        └─ 低 ROI → 接受失败
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

**实质性障碍**（可接受的失败原因）：
- 需要真实外部服务（HTTP server、数据库）
- Mock 测试成本过高
- 集成测试不稳定
- 跨平台测试难以在 CI 进行
- ROI 低（投入 > 1 小时，提升 < 5%）

**在 PR 中说明**（如果接受失败）：
```markdown
## Codecov 状态说明

⚠️ codecov 未达标

**原因**：存在实质性障碍

**核心功能测试**：
- ✅ 所有单元测试通过
- ✅ 关键路径已覆盖
- ✅ 手动测试验证正常

**跨平台测试**：
- ✅ Linux/macOS/Windows CI 通过
```

### 阶段 6：修复 CI 问题（如果需要）

**如果 Test 或 Build 失败**，skill 会：

1. **查看详细日志**：`gh run view <run-id> --log-failed`
2. **分析错误模式**：编译错误、测试失败、Lint 错误、**跨平台错误**
3. **提供修复建议**：特别注意跨平台兼容性

### 阶段 7：PR 合并后清理（可选）

skill 会：确认 PR 已合并、切换到 main、更新本地代码、删除本地/远程分支、关闭相关 issues。

## 最佳实践

### Commit Message 最佳实践

**好的示例**（清晰、完整、包含架构信息）：
```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

**不好的示例**（模糊、不完整、缺少架构信息）：
```
fix bugs
update code
```

### CI 失败处理优先级

| 优先级 | 检查项 | 失败处理 | 原因 |
|--------|--------|----------|------|
| P0 | Test (三平台) | 必须修复 | 功能正确性 |
| P0 | Build (三平台) | 必须修复 | 编译成功 |
| P1 | Coverage Check | 通常修复 | 覆盖率要求 |
| P2 | codecov | 可协商 | 存在实质性障碍 |

### Codecov 决策树

```
codecov 失败
  │
  ├─ 存在实质性障碍？
  │   ├─ 是 → 评估 ROI
  │   │        ├─ 高 ROI → 添加测试
  │   │        └─ 低 ROI → 接受失败
  │   └─ 否 → 添加测试
  │
  ├─ ROI 是否合理？
  │   ├─ 是 → 添加测试
  │   └─ 否 → 接受失败
  │
  └─ 影响核心功能？
      ├─ 是 → 必须修复
      └─ 否 → 可协商
```

### 跨平台开发最佳实践

**路径操作**：
```go
// ✅ 正确
path := filepath.Join("dir", "file")

// ❌ 错误
path := "dir/file"  // 硬编码分隔符
```

**文件权限**：
```go
// ✅ 正确
err := os.MkdirAll(dir, 0755)

// ❌ 错误
err := os.MkdirAll(dir, 0700)  // macOS SIP 保护问题
```

**进程管理**：
```go
// ✅ 正确（跨平台）
err := process.Kill(p)

// ❌ 错误（仅 POSIX）
err := syscall.Kill(p.Pid, syscall.SIGTERM)
```

## 常见问题

### Q: 这个 skill 支持哪些项目？

**A:** 这个 skill 是 **HotPlex 项目专用**的。它硬编码了 HotPlex 的配置：
- 测试命令：`make test`
- Lint 命令：`make lint`
- Upstream 仓库：`hrygo/hotplex`
- Commit 规范：Conventional Commits
- 架构特性：多 channel、多 worker、跨平台兼容

### Q: codecov 失败必须修复吗？

**A:** 不一定。优先级：
1. **核心功能测试失败** → 必须修复
2. **新增代码无测试** → 必须添加
3. **跨平台测试失败** → 必须修复
4. **覆盖率下降 > 5%** → 通常需要修复
5. **覆盖率下降 < 5%，有实质性障碍** → 可接受

### Q: 如何判断实质性障碍？

**A:** 检查：
- ✅ 需要真实外部服务
- ✅ Mock 成本过高
- ✅ 集成测试不稳定
- ✅ 跨平台测试难以在 CI 进行
- ✅ ROI < 5% 提升 / 1 小时投入

### Q: 如何标注架构影响？

**A:** 在 commit message 和 PR 描述中：
- **Channel**: `messaging/slack`、`messaging/feishu`、`webchat`
- **Worker**: `worker/cc`、`worker/ocs`、`worker/pi`
- **平台**: 在 PR 描述中单独说明

### Q: 当前分支已有 PR，怎么更新？

**A:** 直接推送代码即可：
```bash
git add -A
git commit -m "fix(scope): 更多修复"
git push  # skill 会自动检测 fork 远程仓库
```
GitHub 会自动更新现有 PR，CI 会自动重新运行。

skill 会自动检测现有 PR 并询问：
- **推荐**：增量推送更新现有 PR
- **可选**：创建新 PR（需要先关闭或重命名现有 PR）

### Q: 跨平台代码如何组织？

**A:** 使用 build tags：
```
file_unix.go    // Linux + macOS
file_windows.go  // Windows
file_other.go    // 通用实现
```

## 为什么这样设计

### HotPlex 专用而非通用

**为什么？**
HotPlex 项目有特定的配置和架构，硬编码这些配置确保 skill 精准匹配 HotPlex 的要求，提供专业级的支持。

### 架构感知的 commit scope

**为什么？**
HotPlex 是分布式系统，修改可能影响多个架构组件。在 commit message 中标注影响的 channel/worker/平台，帮助团队快速理解变更影响范围。

### 泛化 fork 用户名而非硬编码

**为什么？**
每个贡献者的 fork 仓库名称可能不同，自动检测 fork 仓库名称让 skill 适用于所有贡献者，而不需要手动配置。

### 智能 codecov 处理

**为什么？**
盲目追求 100% 覆盖率是浪费时间。基于 HotPlex 项目实战经验的决策树帮助快速判断，避免在低价值的地方投入过多时间。

## 工作流总结

```
开发完成
  ↓
质量检查（make test + make lint）
  ↓
分析架构影响（channel/worker/平台）
  ↓
提交代码（Conventional Commits + 架构 scope）
  ↓
推送代码（自动检测 fork 远程仓库）
  ↓
检查现有 PR
  ├─ 有 PR → 增量推送更新现有 PR
  └─ 无 PR → 创建新 PR
  ↓
监控 CI（跨平台测试、智能处理失败）
  ↓
┌─ CI 全部通过 → 等待合并
└─ CI 失败 → 修复 → 推送 → 自动重新运行
```

## 参考文档

- **架构详情**：[references/architecture.md](references/architecture.md) - 多 channel、多 worker、跨平台详细说明
- **快速开始**：[QUICKSTART.md](QUICKSTART.md) - 一分钟上手
- **使用说明**：[README.md](README.md) - 完整使用指南
- **详细示例**：[EXAMPLES.md](EXAMPLES.md) - 实战示例
- **快速参考**：[CHEATSHEET.md](CHEATSHEET.md) - 常用命令
- **迁移指南**：[MIGRATION.md](MIGRATION.md) - v1.0 → v2.0

## 附录

### Fork 远程仓库配置

**标准配置**：
```bash
origin    https://github.com/<your-username>/hotplex.git  # 你的 fork
upstream  https://github.com/hrygo/hotplex.git            # 上游（可选）
```

**其他常见配置**：
```bash
fork      https://github.com/<your-username>/hotplex.git  # 你的 fork
origin    https://github.com/hrygo/hotplex.git            # 上游
```

skill 会自动检测并适配任何配置。

### 测试和 Lint 命令

**测试**：
```bash
make test  # 运行所有测试，含 -race
```

**Lint**：
```bash
make lint  # golangci-lint 检查
```

**快速检查**：
```bash
make check  # 完整 CI: quality + build
```
