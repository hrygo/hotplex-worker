---
name: hotplex-pr-quality
version: 3.0.0
description: "HotPlex 项目（hrygo/hotplex）专用 PR 质量保证。开发完成后提交代码、创建/更新 PR、推送 fork、审查代码质量、修复 CI 失败（测试/lint/codecov/跨平台构建）时使用。即使只说「帮我提交」「CI 红了」「推代码」也应触发。覆盖：质量检查 → 提交 → 推送 → PR → 代码审查 → CI 监控与修复。"
metadata:
  requires:
    bins: ["gh", "git"]
    env:
      - GITHUB_TOKEN
  cliHelp: "gh pr --help"
  project: hotplex
---

# HotPlex PR 质量保证

HotPlex 项目专用的 PR 提交与 CI 达标工作流。

## HotPlex 架构速查

代码运行在多 channel（Slack/飞书/WebChat）、多 worker（CC/OCS/Pi）、跨平台（Linux/macOS/Windows）环境下，CI 必须三平台通过。

架构详情：[references/architecture.md](references/architecture.md)

## 核心工作流

```
质量检查 → 提交代码 → 推送 fork → 创建/更新 PR → 监控 CI → 修复（如需）
```

按以下阶段顺序执行。如果用户只要求部分步骤（如「帮我推代码」），从对应阶段开始。

### 阶段 1：前置检查

1. 确认 `gh` CLI 可用（`gh auth status`）
2. 检查当前分支和未提交修改（`git status`）
3. 检测 fork 远程仓库（`git remote -v`），找到包含用户 GitHub 用户名的远程仓库

### 阶段 2：质量检查

运行以下命令，失败则停止并分析原因：

```bash
make test    # 含 -race，三平台
make lint    # golangci-lint
```

如果失败，分析错误日志并提供修复建议。跨平台失败优先检查 `filepath.Join()` 和 build tags。

### 阶段 3：提交代码

生成 Conventional Commits 格式的 commit message：

```
<type>(<scope>): <subject>

<body>

<footer>

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
```

**Type**: feat, fix, refactor, perf, test, docs, style, chore

**Scope**（架构感知，根据 git diff 推断）：
- Gateway 核心：`gateway`、`session`、`config`、`security`
- Channel：`messaging/slack`、`messaging/feishu`、`webchat`
- Worker：`worker/cc`、`worker/ocs`、`worker/pi`
- 平台：`cli`、`service`、`build`

**示例**：
```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
```

### 阶段 4：推送代码

1. 检查是否已有 PR：`gh pr list --head <user>:<branch> --repo hrygo/hotplex`
2. 推送到 fork：`git push -u <fork-remote> <branch>`
3. 已有 PR 则增量推送更新，否则准备创建新 PR

### 阶段 5：创建 PR

使用 `gh pr create --repo hrygo/hotplex`，参数从 fork 用户名和分支自动推断。

PR 描述模板：

```markdown
## Summary

<!-- 2-3 句话 -->

### Changes

- Change 1 (影响的 channel/worker/平台)

**架构影响**：
- Channel: Slack / 飞书 / WebChat / N/A
- Worker: CC / OCS / Pi / N/A
- 平台: Linux / macOS / Windows / 跨平台

## Test Plan

- [x] make test
- [x] make lint

## Related Issues

- Fixes #<N>
```

### 阶段 6：PR 代码审查（按需）

当用户请求审查时：

1. 检查 PR 状态（OPEN、非 draft）
2. 获取 PR diff + CLAUDE.md
3. 启动并行审查：CLAUDE.md 合规、bug 扫描（读实际源码验证）、git 历史上下文、历史 PR 评论
4. 汇总去重，每个 issue 独立评分（0-100），≥80 阈值
5. 发布审查评论或报告无问题

关键：区分 `m.mu`（map 锁）和 `ms.mu`（session 锁）；预存行为不算新 issue；data race 区分理论 TOCTOU 和 Go race detector 实际会报的。

### 阶段 7：监控 CI

```bash
gh pr checks <pr-number> --repo hrygo/hotplex
```

**CI 优先级**：

| 级别 | 检查 | 处理 |
|------|------|------|
| P0 | Test / Build（三平台） | 必须修复 |
| P1 | Coverage Check | 通常修复 |
| P2 | codecov | 可协商 |

**codecov 失败处理**：

存在实质性障碍（需外部服务/Mock 成本高/ROI 低）→ 评估 ROI → 核心功能必须修，< 5% 下降且有障碍可接受。接受时在 PR 说明原因。

### 阶段 8：修复 CI（如需）

查看失败日志：`gh run view <run-id> --log-failed`

修复后推送，CI 自动重新运行。

### 阶段 9：合并后清理（可选）

```bash
git checkout main && git pull origin main
git branch -d <branch>
git push fork --delete <branch>
```

## 参考文档

- [references/architecture.md](references/architecture.md) - 多 channel/worker/跨平台架构详解
- [references/examples.md](references/examples.md) - 完整 PR 创建流程示例
- [references/troubleshooting.md](references/troubleshooting.md) - 常见陷阱、FAQ、命令速查
