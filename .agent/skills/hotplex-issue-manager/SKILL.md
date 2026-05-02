---
name: hotplex-issue-manager
description: "HotPlex issue 批量管理与合并 PR 交付。当需要管理 HotPlex issues、排列优先级、规划批量修复、批量实施多个相关 issue、将多个修复合并到一个 PR、计算 issue 优先级 ROI、或减少合并冲突和审查开销时触发此 skill。即使只说「处理一下 issues」「看看 open issues」「修几个 bug」「批量修复」「issue 优先级」「把这几个 issue 一起做了」也应触发。此 skill 将分散的 GitHub issues 转化为一个合并 PR — 这是对传统一个-issue-一个-PR 工作流的刻意替代，后者经常导致合并冲突和审查疲劳。"
compatibility: Requires gh CLI, Go 1.26+, golangci-lint, make
---

# HotPlex Issue Manager

将分散的 GitHub issues 转化为**一个合并 Pull Request**的工作流。分析、排序、批量实施，一次交付。

**核心模式**：多个 issue → 一个分支 → 一个 PR → 一次合并

> 常见陷阱和反模式见 `references/common-pitfalls.md`，开始前建议快速浏览。

## 工作流

```
Phase 1: 分析与验证 → Phase 2: ROI 评分排序 → Phase 3: 选择批量 → Phase 4: 实施交付
```

## Phase 1: 分析与验证

### 1.1 获取 Issues

```bash
gh issue list --limit 100 --state open \
  --json number,title,body,labels,state,author,createdAt,comments \
  > /tmp/hotplex_issues.json
```

### 1.2 分析每个 Issue

对每个 issue 检查四个维度：

- **完整性** — 能否根据描述实施？（清晰问题陈述、复现步骤/验收标准、环境信息）
- **有效性** — 是真正的 issue 还是模糊不清需要澄清？
- **重复性** — 搜索关键词，是否已被报告或修复？
- **技术可行性** — 是否符合现有架构？有无阻塞性依赖？

### 1.3 标签分类管理（Admin 专属）

> **前置条件**：比较当前 gh 用户与 repo owner，相同则为 Admin。
> ```bash
> REPO_OWNER=$(gh repo view --json owner --jq '.owner.login')
> CURRENT_USER=$(gh api user --jq '.login')
> [ "$REPO_OWNER" = "$CURRENT_USER" ] && echo "ADMIN" || echo "NOT_ADMIN"
> ```

以下操作**仅限 Admin**，普通贡献者仅做分析。

#### 1.3.1 应用分类标签

标签体系共 6 类 27 个标签：

**类型**（选一个）：`bug` | `enhancement` | `documentation` | `performance` | `refactor` | `security`

**优先级**（ROI 计算后分配）：`P1`（关键 ROI≥50）| `P2`（高 ROI 30-49）| `P3`（中 ROI 15-29）

**领域**（可多选）：`architecture` | `race-condition` | `goroutine` | `resource-leak` | `reliability` | `DoS`

**模块**（可多选）：`area/gateway` | `area/session` | `area/messaging` | `area/worker` | `area/cli` | `area/webchat` | `area/config` | `area/updater`

**状态**：`needs-triage` | `blocked` | `breaking-change`

**关闭原因**：`duplicate` | `wontfix` | `invalid` | `fixed` | `not-reproducible`

```bash
gh issue edit <number> --add-label "bug,race-condition,area/gateway"
gh issue edit <number> --remove-label "needs-triage"
```

#### 1.3.2 关闭无效 Issue

| 条件 | 标签 | 评论模板 |
|------|------|---------|
| 已在代码中修复 | `fixed` | `已在 <commit/PR> 中修复，关闭此 issue。` |
| 完全重复 | `duplicate` | `与 #<原issue> 重复，关闭此 issue。` |
| 描述不清且>30天无更新 | `wontfix` | `此 issue 缺少足够信息且长期无更新，关闭。如有新信息请重新打开。` |
| 不在项目范围 | `wontfix` | `此需求不在当前项目范围内，关闭。` |
| 无法复现 | `not-reproducible` | `无法在当前版本复现，关闭。如能提供复现步骤请重新打开。` |
| 已间接解决 | `fixed` | `此问题已通过 <PR/commit> 间接解决，关闭。` |

关闭流程：`gh issue edit <N> --add-label "duplicate"` → `gh issue comment <N> --body "..."` → `gh issue close <N>`

#### 1.3.3 Issue 质量检查

实施前确认：标题 conventional commit 格式、详细描述、Bug 有复现步骤、功能有验收标准、无重复。质量不足时添加 `needs-triage` + 评论请求澄清。

**Phase 1 输出**：`/tmp/issue_analysis.md` — 含分类标签数、关闭无效数、剩余有效数统计。

## Phase 2: 优先级排序与评分

### 2.1 ROI 评分

按三个维度对每个 issue 打分（1-10）：

**影响力 (I)**：10=关键bug/安全/数据丢失，7-8=高影响bug/重大功能，5-6=可感知改进，3-4=小幅，1-2=锦上添花

**紧急度 (U)**：10=生产故障/阻塞发布，7-8=每日影响，5-6=应尽快修，3-4=有空就修，1-2=无截止日期

**工作量 (E)**（反向 — 越高越容易）：10=琐碎1-2h，7-8=容易半天，5-6=中等1-2天，3-4=困难3-5天，1-2=极难1+周

```
ROI = (影响力 × 紧急度 × 工作量) / 10
```

最大值 = 10×10×10/10 = 100。高影响+紧急+简单 → 高 ROI → 优先交付最大价值最快的工作。

### 2.2 分配优先级并检查依赖

```bash
# 应用优先级标签
gh issue edit <number> --add-label "P1"  # ROI ≥ 50
gh issue edit <number> --add-label "P2"  # ROI 30-49
gh issue edit <number> --add-label "P3"  # ROI 15-29

# 检查依赖
gh issue view <number> --json body --jq '.body' | grep -o '#[0-9]\+'
```

被未解决依赖阻塞的 issue 标记 `blocked` 并降低优先级。

**Phase 2 输出**：`/tmp/issue_ranking.md` — 按 ROI 排序的列表，含 I/U/E 分数。

## Phase 3: 选择

### 3.1 为批量 PR 选择 Issue

选择 1-5 个一起实施的 issue。**选择质量比数量更重要**。

按此顺序优先：高 ROI → 连贯性（同一模块/领域）→ 无阻塞依赖 → 总工作量 1-3 天 → 混合快速收益和重要修复。

连贯性很关键 — 在一个 PR 中实施不相关的 issue 会让审查、测试、回滚、git 历史都更困难。

**选择策略**：
- **保守**（2 个）：排名第1的 P1 + 1 个高 ROI P2，适用于不确定时
- **平衡**（3-4 个）：1 P1 + 2 P2 + 1 P3，适用于有 2-3 天时间
- **激进**（5 个）：全部 P1 + 顶级 P2，仅当总工作量 ≤ 3 天且高连贯性

### 3.2 创建实施计划

在 `/tmp/implementation_plan.md` 记录：选定 issues（含 ROI、类型、范围、工作量）、连贯性分析、实施顺序（重构先于修复）、分支策略、测试策略、时间线。

## Phase 4: 实施与交付

详细指南见 `references/implementation-guide.md`，包括分支命名、commit 模板、PR 创建完整模板。

**快速概览**：

1. **准备仓库** — `git fetch origin main && git checkout main && git pull origin main`
2. **创建批量分支** — `batch/<theme>-issues-<numbers>`
3. **按序实施** — 每个 issue 一个 commit（原子提交：`type(scope): description`，footer `Fixes #XX`）
4. **每步验证** — `make lint && make test`
5. **最终集成测试** — `make check`（完整 CI：quality + build）
6. **推送并创建 PR** — 一个 PR 关闭所有 issues，描述按 issue 分别记录 Problem/Solution/Impact

**关键标准**：Go 1.26+ | golangci-lint 频繁运行 | TDD | ≥80% 覆盖率 | Conventional commits | 原子提交

## 输出产物

1. `/tmp/hotplex_issues.json` — 原始 issue 数据
2. `/tmp/issue_analysis.md` — 详细分析 + Phase 1 统计
3. `/tmp/issue_ranking.md` — ROI 排序列表
4. `/tmp/implementation_plan.md` — 批量实施计划
5. **一个合并 PR** — 最终交付物

## Reference 文件

| 文件 | 内容 |
|------|------|
| `references/common-pitfalls.md` | 6 个常见陷阱与对策 |
| `references/implementation-guide.md` | Phase 4 完整指南：仓库准备、分支创建、commit 模板、PR 模板 |
| `references/example-session.md` | 完整演练：从 20 个 issues 到合并 PR |
| `references/troubleshooting.md` | 8 个常见问题的诊断和解决方案 |
| `scripts/calc-roi.py` | ROI 计算辅助脚本：`python calc-roi.py /tmp/hotplex_issues.json` |

## 适用范围

**适用于**：2-5 个相关 issue、触及相似代码区域、1-3 天可完成、想减少合并冲突和审查开销

**不适用于**：完全不相关的 issue、总工作量 > 3 天、有复杂难解的依赖、需要立即 hotfix 的单个关键问题
