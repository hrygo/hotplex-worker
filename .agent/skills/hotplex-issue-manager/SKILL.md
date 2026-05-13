---
name: hotplex-issue-manager
description: "HotPlex issue 批量管理与合并 PR 交付。当需要管理 HotPlex issues、排列优先级、规划批量修复、批量实施多个相关 issue、将多个修复合并到一个 PR、计算 issue 优先级 ROI、或减少合并冲突和审查开销时触发此 skill。即使只说「处理一下 issues」「看看 open issues」「修几个 bug」「批量修复」「issue 优先级」「把这几个 issue 一起做了」也应触发。此 skill 将分散的 GitHub issues 转化为一个合并 PR — 这是对传统一个-issue-一个-PR 工作流的刻意替代，后者经常导致合并冲突和审查疲劳。"
compatibility: Requires gh CLI, Go 1.26+, golangci-lint, make
---

# HotPlex Issue Manager

将分散的 GitHub issues 转化为**一个合并 Pull Request**。分析、排序、批量实施，一次交付。

**核心模式**：多个 issue → 一个分支 → 一个 PR → 一次合并

> 陷阱和反模式见 `references/common-pitfalls.md`（7 个常见错误，含 PR 冲突）。

## 工作流

```
Phase 1: 分析验证 → 呈现结果 → Phase 2: ROI 评分 → 呈现排名 → Phase 3: PR 冲突检查 + 选择 → 确认计划 → Phase 4: 实施
```

每个 Phase 结束后**呈现结果让用户确认**，再进入下一阶段。不要一口气跑完全部 Phase。

## Phase 1: 分析与验证

### 1.1 获取与分析

```bash
gh issue list --limit 100 --state open \
  --json number,title,body,labels,state,author,createdAt,comments \
  > /tmp/hotplex_issues.json
```

对每个 issue 检查四个维度：

- **完整性** — 能否根据描述实施？（清晰问题陈述、复现步骤/验收标准）
- **有效性** — 是真正的 issue 还是模糊不清？
- **重复性** — 搜索关键词，是否已被报告或修复？
- **技术可行性** — 是否符合现有架构？有无阻塞性依赖？

### 1.2 标签与关闭（Admin 专属）

标签体系（6 类 27 个标签）、关闭无效 issue 的完整流程见 `references/label-taxonomy.md`。

### 1.3 呈现阶段结果

输出 `/tmp/issue_analysis.md`，向用户呈现：
- 总数统计：分类标签数、关闭无效数、剩余有效数
- **按模块/领域分组**的 issue 概览
- 发现的重复、无效、已修复 issue

等用户确认后进入 Phase 2。

## Phase 2: ROI 评分

### 2.1 评分公式

三个维度（1-10）：

**影响力 (I)**：10=关键bug/安全/数据丢失，7-8=高影响bug/重大功能，5-6=可感知改进，3-4=小幅，1-2=锦上添花

**紧急度 (U)**：10=生产故障/阻塞发布，7-8=每日影响，5-6=应尽快修，3-4=有空就修，1-2=无截止日期

**工作量 (E)**（反向 — 越高越容易）：10=琐碎1-2h，7-8=容易半天，5-6=中等1-2天，3-4=困难3-5天，1-2=极难1+周

```
ROI = (I × U × E) / 10    最大值 = 100
```

### 2.2 优先级与依赖

| 优先级 | ROI 范围 | 标签 |
|--------|----------|------|
| P1 | ≥ 50 | `P1` |
| P2 | 30-49 | `P2` |
| P3 | 15-29 | `P3` |

检查 issue body 中的 `#XX` 引用识别依赖。被未解决依赖阻塞的标记 `blocked` 并降低优先级。

### 2.3 呈现排名

输出 `/tmp/issue_ranking.md`，向用户呈现按 ROI 排序的完整列表（含 I/U/E 分数），以及优先级分布统计。

**边缘情况处理**：
- **全是低 ROI（< 15）**：告知用户当前无高价值 issue，建议关注意愿驱动的重构
- **只有 0-1 个有效 issue**：跳过批量模式，直接实施或告知无需批量
- **用户指定了具体 issue 编号**：只分析指定 issue，跳过全量扫描

## Phase 3: PR 冲突检查 + 选择

### 3.0 PR 冲突检查（必须，在选择前执行）

选择前必须排除已有 PR 覆盖的 issue，避免重复工作和合并冲突。

**步骤**：

1. 获取 open PR 及其覆盖的 issue 编号和 diff 文件
2. 对每个候选 issue 做三级评估：

| 级别 | 条件 | 处理 |
|------|------|------|
| 直接覆盖 | Issue 编号出现在 PR title/body 中 | 排除 |
| 文件重叠 | Issue 涉及的文件与 PR diff 重叠 | 排除或延后 |
| 安全可选 | 无重叠 | 可选 |

3. 输出 `/tmp/pr_conflicts.md`，向用户呈现排除结果

### 3.1 从安全可选池中选择

从 3.0 的安全可选池中选 1-5 个 issue。优先顺序：高 ROI → 连贯性（同一模块/领域）→ 无阻塞依赖 → 总工作量 1-3 天。

连贯性很关键 — 不相关的 issue 混在一个 PR 里让审查、测试、回滚都更困难。

**选择策略**：
- **保守**（2 个）：排名第1的 P1 + 1 个高 ROI P2
- **平衡**（3-4 个）：1 P1 + 2 P2 + 1 P3
- **激进**（5 个）：全部 P1 + 顶级 P2，仅当工作量 ≤ 3 天且高连贯性

### 3.2 呈现选择方案，等用户确认

向用户呈现候选方案（含 ROI、类型、范围、预估工作量、PR 冲突排除说明），让用户选择或调整。确认后输出 `/tmp/implementation_plan.md` 进入 Phase 4。

## Phase 4: 实施与交付

详细指南见 `references/implementation-guide.md`（分支命名、commit 模板、PR 模板）。

**快速概览**：

1. **准备仓库** — `git fetch origin main && git checkout main && git pull origin main`
2. **创建批量分支** — `batch/<theme>-issues-<numbers>`
3. **按序实施** — 每个 issue 一个原子 commit（`type(scope): description`，footer `Fixes #XX`）
4. **每步验证** — `make lint && make test`
5. **最终集成测试** — `make check`（完整 CI：quality + build）
6. **推送并创建 PR** — 一个 PR 关闭所有 issues

**标准**：Go 1.26+ | golangci-lint | TDD | ≥80% 覆盖率 | Conventional commits | 原子提交

## 输出产物

1. `/tmp/hotplex_issues.json` — 原始 issue 数据
2. `/tmp/issue_analysis.md` — Phase 1 分析结果
3. `/tmp/issue_ranking.md` — Phase 2 ROI 排名
4. `/tmp/pr_conflicts.md` — Phase 3 PR 冲突检查
5. `/tmp/implementation_plan.md` — 批量实施计划
6. **一个合并 PR** — 最终交付物

## Reference 文件

| 文件 | 内容 |
|------|------|
| `references/label-taxonomy.md` | 标签体系（6 类 27 个）、关闭无效 issue 流程、质量检查 |
| `references/common-pitfalls.md` | 7 个常见陷阱与对策（含 PR 冲突） |
| `references/implementation-guide.md` | Phase 4 完整指南：仓库准备、分支创建、commit 模板、PR 模板 |
| `references/example-session.md` | 完整演练：从 20 个 issues 到合并 PR |
| `references/troubleshooting.md` | 8 个常见问题的诊断和解决方案 |

## 适用范围

**适用于**：2-5 个相关 issue、触及相似代码区域、1-3 天可完成、想减少合并冲突和审查开销

**不适用于**：完全不相关的 issue、总工作量 > 3 天、有复杂难解的依赖、需要立即 hotfix 的单个关键问题
