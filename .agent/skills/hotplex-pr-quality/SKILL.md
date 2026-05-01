---
name: hotplex-pr-quality
version: 2.0.0
description: "HotPlex 项目的 PR 质量保证与 CI 达标助手。当你完成开发、需要提交代码、创建或更新 PR、遇到 CI 失败、需要 codecov 分析、处理测试或 lint 错误、管理分支或推送代码时，使用此 skill。它自动执行质量检查、代码提交、PR 创建/更新和 CI 监控，确保你的变更符合 HotPlex 的多 channel (Slack/飞书/WebChat)、多 worker (CC/OCS/Pi)、跨平台 (Linux/macOS/Windows) 架构要求。**HotPlex 项目专用**，hrygo/hotplex 仓库。"
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

## 快速开始

**最简单的使用方式**：开发完成后，直接调用 skill，它会自动完成所有步骤。

**典型场景**：
- ✅ 开发完成，需要提交 PR
- ✅ 修改了代码，需要更新现有 PR
- ✅ CI 失败，需要分析和修复
- ✅ 不确定是否通过所有检查

**skill 会自动**：
1. 运行测试和 lint（`make test` + `make lint`）
2. 生成规范的 commit message（Conventional Commits）
3. 推送到你的 fork 仓库
4. 创建或更新 PR
5. 监控 CI 状态
6. 智能处理 codecov 失败

**你只需要**：确认 commit message 和 PR 描述，其他都交给 skill。

## HotPlex 架构特性

HotPlex 是一个支持多 channel、多 worker 和跨平台兼容的分布式 AI Agent Gateway。

**多 Channel**：Slack Socket Mode、飞书 WebSocket (P2)、WebChat
**多 Worker**：Claude Code (CC)、OpenCode Server (OCS)、Pi-mono
**跨平台**：Linux、macOS、Windows 三平台 CI 必须通过

详见：[references/architecture.md](references/architecture.md)

## 前置条件检查

在开始之前，skill 会自动检查这些前提条件，这样可以避免后续流程中遇到意外错误：

1. **Git 仓库状态**：检查未提交的修改、当前分支、fork 远程仓库
2. **工具可用性**：确认 `gh` CLI 和 Git 仓库可用
3. **Fork 仓库配置**：自动检测 fork 远程仓库名称、推断用户名、验证 upstream

**为什么需要这些检查？** 提前发现问题比在流程中途失败更高效。例如，如果 `gh` CLI 未安装，我们在开始就能知道，而不是在创建 PR 时才发现。

## 核心流程

### 阶段 1：质量检查

**为什么质量检查很重要？**
HotPlex 项目要求所有 PR 必须通过跨平台测试和 lint 检查，因为代码会在多种 channel（Slack/飞书/WebChat）、多种 worker（CC/OCS/Pi）和三个平台（Linux/macOS/Windows）上运行。早期发现问题比在 PR review 时被发现更高效。

skill 会自动：

1. **运行测试**：`make test`（含跨平台测试）
2. **运行 lint**：`make lint`（含跨平台检查）
3. **分析架构影响**：检查修改影响的 channel/worker/平台

**如果测试或 lint 失败**，skill 会显示错误日志、分析失败原因、提供修复建议。这样做可以让你快速定位问题，而不是在 CI 失败后反复尝试。

### 阶段 2：提交代码

**为什么规范的 commit message 很重要？**
HotPlex 项目使用 Conventional Commits 规范，因为这样可以：
- 自动生成 changelog
- 快速理解变更的目的和范围
- 在 commit history 中轻松搜索特定类型的变更
- 让 scope 反映影响的架构组件（channel/worker/平台）

skill 会自动：

1. **生成规范的 commit message**
   - 根据 HotPlex 的 commit 规范生成
   - 根据修改内容推断 type 和 scope（架构感知）
   - 生成中文描述（技术术语用英文）
   - 包含 Co-Authored-By 声明

2. **暂存并提交代码**

这样做可以保持 commit history 的一致性和可读性，方便团队成员和未来的自己理解变更历史。

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
HotPlex 项目使用 Fork & PR 工作流，每个贡献者的 fork 仓库名称可能不同（可能是 `origin`、`fork`、`upstream` 等）。自动检测可以避免每次都手动指定远程仓库名称，减少出错的可能性。

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
规范的 PR 描述帮助 reviewer 快速理解：
- 变更的目的和影响范围
- 涉及的架构组件（channel/worker/平台）
- 测试覆盖情况
- 相关 issues

这可以加速 review 流程，减少来回询问的时间。

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

**为什么自动推断参数？** 减少手动输入错误，确保 PR 创建成功。例如，`--head` 格式必须是 `username:branch`，手动输入容易出错。

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
HotPlex 的 CI 检查很多（特别是跨平台 CI），不是所有失败都需要修复。盲目修复所有失败会浪费时间，而智能监控可以帮你区分：
- **关键失败**：影响功能的错误（测试失败、编译失败）→ 必须修复
- **次要失败**：覆盖率下降、codecov 警告 → 可根据情况处理

skill 会自动：

1. **监控 CI 运行**：等待 CI 完成（3-5 分钟）
2. **分析 CI 结果**：
   - **核心检查**（必须通过）：Test（三平台）、Build（三平台）、Coverage Check
   - **次要检查**（可协商）：codecov/patch、codecov/project

3. **智能处理 codecov 失败**

这样做可以让你专注于真正重要的问题，而不是被 codecov 的警告分散注意力。

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

**为什么查看详细日志？** 错误信息往往包含关键线索，例如：
- 跨平台错误可能是因为使用了硬编码的路径分隔符
- 测试失败可能是因为缺少测试依赖
- Lint 错误可能提示代码风格问题

### 阶段 7：PR 合并后清理（可选）

skill 会：确认 PR 已合并、切换到 main、更新本地代码、删除本地/远程分支、关闭相关 issues。

**为什么需要清理？** 保持本地仓库整洁，避免过期分支积累过多。这样可以：
- 减少混淆（不知道哪些分支还在使用）
- 节省磁盘空间
- 让 git log 更清晰

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

## 常见陷阱与故障排除

### 陷阱 1：忽略跨平台兼容性

**症状**：CI 在某个平台失败（例如 macOS），但在本地（Linux）运行正常。

**原因**：使用了平台特定的代码或路径分隔符。

**解决方案**：
```go
// ❌ 错误：硬编码路径分隔符
path := "dir/file"

// ✅ 正确：使用 filepath.Join
path := filepath.Join("dir", "file")
```

**为什么这样做？** Windows 使用反斜杠 `\`，Unix 系统使用正斜杠 `/`。`filepath.Join` 会自动选择正确的分隔符。

### 陷阱 2：Fork 远程仓库检测失败

**症状**：skill 无法找到你的 fork 仓库，推送失败。

**原因**：
- Fork 远程仓库名称不包含你的用户名
- 远程仓库 URL 配置错误
- 未添加 fork 远程仓库

**解决方案**：
1. 检查远程仓库配置：
   ```bash
   git remote -v
   ```
2. 确保有一个远程仓库指向你的 fork：
   ```bash
   git remote add fork https://github.com/<your-username>/hotplex.git
   ```
3. 或者使用包含你用户名的名称（如 `origin`）

**预防措施**：首次使用 skill 前，确保已添加 fork 远程仓库。

### 陷阱 3：现有 PR 未被检测到

**症状**：当前分支已有 PR，但 skill 仍然尝试创建新 PR。

**原因**：
- PR 的 head 分支格式不匹配（`username:branch`）
- PR 已关闭或合并
- 仓库名称不正确

**解决方案**：
1. 手动检查现有 PR：
   ```bash
   gh pr list --head <your-username>:<branch> --repo hrygo/hotplex
   ```
2. 如果 PR 存在但未检测到，skill 会询问你是更新现有 PR 还是创建新 PR
3. 选择"更新现有 PR"即可

### 陷阱 4：Codecov 失败被误判

**症状**：Codecov 报告失败，但实际上所有核心功能都已测试。

**原因**：
- 新增代码缺少测试
- 测试覆盖率计算方式变更
- 覆盖率阈值设置过严

**解决方案**：
使用 skill 提供的决策树判断：
1. 是否存在实质性障碍？（例如需要真实外部服务）
2. ROI 是否合理？（投入 > 1 小时，提升 < 5% → 可接受）
3. 是否影响核心功能？（核心功能必须修复）

**在 PR 中说明**：
```markdown
## Codecov 状态说明

⚠️ codecov 未达标

**原因**：存在实质性障碍（需要真实 HTTP server）

**核心功能测试**：
- ✅ 所有单元测试通过
- ✅ 关键路径已覆盖
- ✅ 手动测试验证正常
```

### 陷阱 5：Commit Message 格式错误

**症状**：Commit message 不符合 Conventional Commits 规范，导致自动化工具失败。

**原因**：
- 缺少 type 或 scope
- Subject 过长或不清晰
- 缺少 Co-Authored-By 声明

**解决方案**：
让 skill 自动生成 commit message，它会：
- 根据 HotPlex 规范生成格式
- 推断正确的 type 和 scope
- 添加 Co-Authored-By 声明

**如果手动编写**，确保格式正确：
```
<type>(<scope>): <subject>

<body>

<footer>

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 陷阱 6：CI 超时或卡住

**症状**：CI 运行时间过长（> 10 分钟）或一直不完成。

**原因**：
- 测试用例有死锁或无限循环
- 跨平台测试在某个平台卡住
- CI 服务器负载过高

**解决方案**：
1. 使用 `gh run list` 查看 CI 状态
2. 如果 CI 卡住，可以取消重新运行：
   ```bash
   gh run cancel <run-id>
   gh run rerun <run-id>
   ```
3. 检查本地测试是否有性能问题：
   ```bash
   go test -v -race -timeout 5m
   ```

### 陷阱 7：PR 描述缺少架构影响

**症状**：Reviewer 询问变更影响了哪些 channel/worker/平台。

**原因**：PR 描述未包含架构影响标注。

**解决方案**：
Skill 会自动生成包含架构影响的 PR 描述，但你可以手动补充：

```markdown
**架构影响**：
- Channel: Slack / 飞书 / WebChat / N/A
- Worker: CC / OCS / Pi / N/A
- 平台: Linux / macOS / Windows / 跨平台
```

**为什么需要标注？** HotPlex 是分布式系统，变更可能影响多个组件。明确标注可以帮助 reviewer：
- 识别需要测试的区域
- 评估变更的风险范围
- 协调跨团队 review

### 陷阱 8：分支名称冲突

**症状**：推送时提示"分支已存在"或"非快进推送"。

**原因**：
- 远程分支有新的提交（例如 upstream 更新）
- 多个开发者使用相同分支名

**解决方案**：
1. 拉取最新代码：
   ```bash
   git pull --rebase <fork-remote> <branch>
   ```
2. 解决冲突（如果有）
3. 重新推送：
   ```bash
   git push <fork-remote> <branch>
   ```

**预防措施**：使用描述性的分支名（例如 `feature/slack-sse-timeout` 而不是 `fix-bug`）。

### 陷阱 9：Linter 配置不一致

**症状**：本地 lint 通过，但 CI 的 lint 失败。

**原因**：
- 本地和 CI 使用不同的 linter 版本
- 本地未运行完整的 lint 检查
- Linter 配置文件未提交

**解决方案**：
1. 确保使用 `make lint` 而不是直接运行 `golangci-lint`
2. 检查 linter 版本：
   ```bash
   golangci-lint --version
   ```
3. 提交所有配置文件（`.golangci.yml`）

**为什么使用 make lint？** Makefile 确保本地和 CI 使用相同的命令和参数。

### 陷阱 10：测试依赖缺失

**症状**：本地测试通过，CI 测试失败（例如 "cannot find package"）。

**原因**：
- Go modules 未更新
- 依赖未提交到 `go.mod` 或 `go.sum`
- CI 环境缺少系统依赖

**解决方案**：
1. 更新 go modules：
   ```bash
   go mod tidy
   go mod vendor
   ```
2. 提交 `go.mod` 和 `go.sum` 的变更
3. 检查是否有系统依赖（例如 `make test` 前需要 `make setup`）

## 设计理念

### HotPlex 专用而非通用

**为什么 HotPlex 专用？**
HotPlex 项目有特定的配置和架构（测试命令 `make test`、lint 命令 `make lint`、upstream 仓库 `hrygo/hotplex`、Conventional Commits 规范、多 channel/多 worker/跨平台架构）。硬编码这些配置让 skill 可以：
- 精准匹配 HotPlex 的要求
- 提供架构感知的 commit scope
- 智能处理跨平台 CI 失败
- 减少 user 需要手动配置的内容

如果做成通用 skill，user 需要配置很多参数，反而增加了使用成本。

### 架构感知的 commit scope

**为什么标注架构影响？**
HotPlex 是分布式系统，修改可能影响多个架构组件（channel/worker/平台）。在 commit message 中标注这些影响，可以：
- 帮助团队快速理解变更影响范围
- 方便后续搜索特定组件的变更历史
- 协调跨团队的 review 和测试
- 降低引入回归 bug 的风险

例如，`fix(worker/ocs): resolve SSE timeout` 明确告诉 reviewer 这个变更只影响 OCS worker，不需要测试 Slack 或飞书。

### 自动检测 fork 仓库

**为什么不硬编码 fork 仓库名？**
每个贡献者的 fork 仓库名称可能不同（`origin`、`fork`、`upstream` 等），自动检测可以：
- 适用于所有贡献者，无需手动配置
- 避免推送失败（例如推送到错误的远程仓库）
- 减少使用门槛（新贡献者不需要学习配置）

skill 会遍历所有远程仓库，找到包含你用户名的那个，然后推送到那里。

### 智能 codecov 处理

**为什么不追求 100% 覆盖率？**
盲目追求 100% 覆盖率是浪费时间，因为：
- 有些代码难以测试（例如需要真实 HTTP server）
- Mock 测试成本可能高于收益
- 覆盖率不是质量的唯一指标（测试质量更重要）

基于 HotPlex 项目实战经验的决策树帮助快速判断：
- 是否存在实质性障碍（例如需要外部服务）
- ROI 是否合理（投入 > 1 小时，提升 < 5% → 可接受）
- 是否影响核心功能（核心功能必须修复）

这样可以让你专注于真正重要的测试，而不是被 codecov 的警告分散注意力。

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
