---
name: hotplex-issue-manager
description: HotPlex issue 批量管理与合并 PR 交付。当需要管理 HotPlex issues、排列优先级、规划批量修复、批量实施多个相关 issue、将多个修复合并到一个 PR、计算 issue 优先级 ROI、或减少合并冲突和审查开销时触发此 skill。此 skill 将分散的 GitHub issues 转化为一个合并 PR — 这是对传统一个-issue-一个-PR 工作流的刻意替代，后者经常导致合并冲突和审查疲劳。
compatibility: Requires gh CLI, Go 1.26+, golangci-lint, make
---

# HotPlex Issue Manager

HotPlex 的批量 issue 管理工作流，将分散的 GitHub issues 转化为**一个合并 Pull Request**。此 skill 分析、排序并批量实施多个 issue，减少合并冲突和审查开销。

## 常见陷阱（务必先读！）

深入工作流之前，避免这些毁掉批量 PR 的常见错误：

**❌ 将不相关的 issue 放在一起**
- 把 CLI 功能、webchat 修复和 messaging 重构混在一个批量里
- **为什么不行**：缺乏主题连贯性，审查痛苦、测试复杂
- **✅ 正确做法**：按领域分组 — 全部 messaging 修复，或全部 CLI 改进

**❌ 批量过载（太多 issue）**
- 试图把 8+ 个 issue 塞进"一个巨型 PR"
- **为什么不行**：变成无法审查的怪物；一个 issue 卡住，全部卡住
- **✅ 正确做法**：最多 2-5 个 issue，或按主题拆分（messaging 批次 1、批次 2）

**❌ 忽略 issue 间的依赖**
- 实施 issue B 时发现它依赖 issue A 的重构
- **为什么不行**：代码跑不起来，浪费实施时间
- **✅ 正确做法**：先检查依赖，先实施重构再修 bug

**❌ 每个 issue 只跑单元测试**
- 隔离测试每个修复，从不一起测试
- **为什么不行**：错过 issue 交互产生的集成 bug
- **✅ 正确做法**：所有 issue 实施完毕后运行完整测试套件

**❌ PR 描述含糊**
- 写 "Fixes several issues" 作为 PR 描述
- **为什么不行**：审查者无法理解改了什么或为什么改
- **✅ 正确做法**：按 issue 分别文档化，包含 problem/solution/impact 结构

## 为什么批量 PR 重要

传统的"一个 issue 一个 PR"工作流会产生问题：
- 触及相似代码时产生多次合并冲突
- 多个小而相关的 PR 造成审查疲劳
- 多次单独合并带来部署开销
- 碎片化的 git 历史

**批量 PR 解决这些问题**，通过将相关修复分组：
- 一个分支 = 一次合并冲突解决
- 一次审查 = 对变更的全局理解
- 一次部署 = 协调发布
- 清洁历史 = 相关工作的逻辑分组

## 工作流概览

从原始 issues 到合并 PR，四个阶段引导你：

1. **分析与验证** — 理解 issue 到底需要什么
2. **优先级排序** — 按 ROI 评分，聚焦高影响工作
3. **选择** — 挑选 1-5 个能良好协作的 issue
4. **实施与交付** — 构建并交付一个合并 PR

## Phase 1: 分析与验证

### 1.1 获取 Issues

```bash
cd /home/hotplex/.hotplex/workspace/hotplex
gh issue list --limit 100 --state open \
  --json number,title,body,labels,state,author,createdAt,comments \
  > /tmp/hotplex_issues.json
```

### 1.2 分析每个 Issue

对每个 issue 检查：

**完整性** — 能否根据描述实施？
- 有清晰的问题陈述或功能请求？
- 有复现步骤（bug）？
- 有验收标准（功能）？
- 有环境/上下文信息？

**有效性** — 这是真正的 issue 吗？
- 能否复现？
- 功能需求是否明确且可执行？
- 还是模糊不清、需要澄清？

**重复性** — 是否已被报告？
- 按关键词搜索相似 issue
- 检查是否已在其他分支修复
- 链接重复 issue 而非重新实施

**技术可行性** — 能否实施？
- 是否符合现有架构？
- 有阻塞性依赖？
- 是否需要调研/存在未知因素？

### 1.3 标签分类管理（Admin 专属）

> **前置条件**：比较当前 gh 用户与 repo owner，相同则为 Admin。
> ```bash
> REPO_OWNER=$(gh repo view --json owner --jq '.owner.login')
> CURRENT_USER=$(gh api user --jq '.login')
> [ "$REPO_OWNER" = "$CURRENT_USER" ] && echo "ADMIN" || echo "NOT_ADMIN"
> # 输出 ADMIN 时执行以下管理操作，NOT_ADMIN 则跳过。
> ```

以下操作**仅限 Admin 用户**执行，普通贡献者仅做分析不做标签/关闭操作。

#### 1.3.1 应用分类标签

标签体系共 6 类 27 个标签，遵循 GitHub 最佳实践：

**类型标签**（选一个最合适的，紫色 `#5319e7`）：
- `bug` — 功能损坏、崩溃、行为异常
- `enhancement` — 新功能或能力
- `documentation` — 文档、README、示例
- `performance` — 速度、内存、优化
- `refactor` — 代码质量、DRY、SOLID 改进
- `security` — 漏洞、安全加固

**优先级标签**（ROI 计算后分配，红→黄色阶）：
- `P1` — 关键（ROI ≥ 50，`#b60205`）
- `P2` — 高（ROI 30-49，`#d93f0b`）
- `P3` — 中（ROI 15-29，`#fbca04`）

**领域标签**（可多选，灰色 `#c5def5`）：
- `architecture` — 设计模式、耦合、关注点分离
- `race-condition` — 并发 bug、数据竞争
- `goroutine` — Goroutine 泄漏、生命周期管理
- `resource-leak` — 内存泄漏、文件描述符泄漏
- `reliability` — 可用性、错误处理、可恢复性
- `DoS` — 拒绝服务攻击面

**模块标签**（可多选，蓝色 `#0075ca`）：
- `area/gateway` — WebSocket 网关、Hub、Conn
- `area/session` — Session 状态机、GC、配额
- `area/messaging` — Slack/飞书消息适配器
- `area/worker` — Worker 进程管理（Claude Code/OCS/Pi）
- `area/cli` — Cobra CLI 命令
- `area/webchat` — Next.js Web Chat UI
- `area/config` — 配置加载、热重载
- `area/updater` — 自更新机制

**状态标签**（特殊状态标记，黄色 `#fbca04`）：
- `needs-triage` — 需要初步分类
- `blocked` — 被外部依赖阻塞
- `breaking-change` — 包含破坏性变更

**关闭原因标签**（关闭 issue 时附加，灰色 `#cfd3d7`）：
- `duplicate` — 重复 issue
- `wontfix` — 不予修复
- `invalid` — 无效 issue
- `fixed` — 已修复
- `not-reproducible` — 无法复现

```bash
# 标签组合示例：类型 + 领域 + 模块
gh issue edit <number> --add-label "bug,race-condition,area/gateway"
gh issue edit <number> --add-label "enhancement,area/cli"
gh issue edit <number> --remove-label "needs-triage"
```

#### 1.3.2 关闭无效 Issue

对以下类型的 issue，Admin 应**直接关闭**并附带说明评论：

**关闭条件**（满足任一即关闭）：

| 条件 | 关闭标签 | 评论模板 |
|------|---------|---------|
| 已在代码中修复，只是 issue 未关闭 | `fixed` | `已在 <commit/PR> 中修复，关闭此 issue。` |
| 与现有 issue 完全重复 | `duplicate` | `与 #<原issue> 重复，关闭此 issue。` |
| 描述不清且长期无回应（>30天无更新） | `wontfix` | `此 issue 缺少足够信息且长期无更新，关闭。如有新信息请重新打开。` |
| 不属于项目范围或不合理的需求 | `wontfix` | `此需求不在当前项目范围内，关闭。` |
| 无法复现且无足够信息排查 | `not-reproducible` | `无法在当前版本复现此问题，关闭。如能提供复现步骤请重新打开。` |
| 已通过其他重构/改进间接解决 | `fixed` | `此问题已通过 <PR/commit> 间接解决，关闭。` |

**关闭流程**：

```bash
# 1. 添加关闭标签
gh issue edit <number> --add-label "duplicate"

# 2. 添加关闭评论（说明原因）
gh issue comment <number> --body "与 #<原issue> 重复，关闭此 issue。"

# 3. 关闭 issue
gh issue close <number>
```

**关闭统计**：在分析报告中记录关闭数量：
```
Phase 1 完成: 分析 20 个 open issues
- 分类标签: 15 个已标注
- 关闭无效: 5 个（duplicate: 2, wontfix: 2, fixed: 1）
- 剩余有效: 15 个进入 Phase 2 排序
```

#### 1.3.3 Issue 质量检查清单

**实施前逐条检查**：
- [ ] 标题使用 conventional commit 格式：`scope: description`
- [ ] 详细描述问题或功能
- [ ] Bug 包含：复现步骤、预期 vs 实际行为
- [ ] 功能包含：验收标准、使用场景
- [ ] 提供环境/上下文信息
- [ ] 无重复（链接相关 issue）

**质量不足时的处理**：
- 信息可补充 → 添加 `needs-triage` 标签 + 评论请求澄清
- 长期无回应 → Admin 直接关闭（见 1.3.2）

**为什么重要**：实施不清晰的 issue 会浪费时间 — 你会在工作中途发现边界情况或构建错误的东西。Admin 的标签分类和无效 issue 清理确保后续 Phase 只处理真正有价值的工作。

## Phase 2: 优先级排序与评分

### 2.1 ROI 评分体系

ROI（投资回报率）帮助聚焦高影响工作。按三个维度对每个 issue 打分（1-10）：

**影响力 (I)**：对用户有多重要？
- 10：影响所有用户的关键 bug、安全问题、数据丢失
- 7-8：高影响 bug、重大性能提升、高频需求的功能
- 5-6：中等影响、可感知的改进
- 3-4：低影响、小幅改进
- 1-2：锦上添花、用户感知极小

**紧急度 (U)**：时间敏感度如何？
- 10：生产故障、安全漏洞、阻塞发布
- 7-8：每日影响大量用户、体验持续恶化
- 5-6：应该尽快修复，但不紧急
- 3-4：有空就修
- 1-2：无截止日期

**工作量 (E)**：实施复杂度？（反向 — 越高越容易）
- 10：琐碎（1-2 小时，简单修复，思路清晰）
- 7-8：容易（半天，最小复杂度）
- 5-6：中等（1-2 天，一定复杂度）
- 3-4：困难（3-5 天，显著复杂度）
- 1-2：非常困难（1+ 周，需要调研、架构变更）

**ROI 公式**：
```
ROI = (影响力 × 紧急度 × 工作量) / 100
```

**为什么这个公式有效**：
- 高影响 issue 优先（影响力在分子）
- 紧急 issue 优先（紧急度在分子）
- 简单修复优先（工作量在分子 — 反向评分）
- 结果：优先交付最大价值最快的工作

### 2.2 分配优先级标签

根据 ROI 分数和 issue 类型：

- **P1**（关键）：ROI ≥ 50，或安全/崩溃/数据丢失 issue
- **P2**（高）：ROI 30-49，或高影响 bug
- **P3**（中）：ROI 15-29，或技术债/重构

```bash
gh issue edit <number> --add-label "P1"  # 关键
gh issue edit <number> --add-label "P2"  # 高
gh issue edit <number> --add-label "P3"  # 中
```

### 2.3 检查依赖

Issue 之间常有依赖关系：

```bash
# 在 issue body 中查找引用的 issue
gh issue view <number> --json body --jq '.body' | grep -o '#[0-9]\+'
```

构建依赖图。被未解决依赖阻塞的 issue 应标记 `blocked` 并降低优先级 — 反正现在也无法实施。

### 2.4 生成排序列表

创建 `/tmp/issue_ranking.md`：

```markdown
# HotPlex Issues — 按 ROI 排序

## P1 — 关键 (ROI 50+)
- [ ] #90 — feat(cli): add `hotplex update` subcommand (ROI: 72)
  - 影响力: 8 (高频需求功能)
  - 紧急度: 9 (用户要求)
  - 工作量: 10 (琐碎, 4 小时)
  
- [ ] #78 — fix(messaging): error handling (ROI: 68)
  - 影响力: 8 (影响大量用户)
  - 紧急度: 8 (每日发生)
  - 工作量: 9 (容易, 6 小时)

## P2 — 高优先级 (ROI 30-49)
- [ ] #89 — perf(webchat): bundle code split (ROI: 45)
  - 影响力: 7 (所有 webchat 用户的性能提升)
  - 紧急度: 6 (持续恶化)
  - 工作量: 8 (中等, 1 天)
```

## Phase 3: 选择

### 3.1 为批量 PR 选择 Issue

选择 1-5 个将在一个 PR 中一起实施的 issue。**选择质量比数量更重要** — 干净交付 3 个 issue 比混乱交付 5 个更好。

**选择标准**（按此顺序优先）：

1. **高 ROI** — 优先选择最大影响力
   - **原因**：你希望时间投入产出最大价值
2. **连贯性** — issue 之间应该相互关联
   - 同一模块（如：全部 messaging issue）
   - 同一层级（如：全部 adapter 重构）
   - 相关领域（如：全部性能 issue）
   - **原因**：连贯的批量更容易审查、测试和理解
3. **无阻塞依赖** — 全部可以独立实施
   - **原因**：依赖会复杂化实施顺序并可能阻塞进展
4. **可控范围** — 总工作量应在 1-3 天
   - **原因**：更大的批量变得无法审查且风险更高
5. **战略平衡** — 混合快速收益和重要修复
   - **原因**：快速收益建立势能，重要修复交付长期价值

**为什么连贯性重要**：在一个 PR 中实施不相关的 issue 会导致：
- 代码审查更难（审查者需要频繁切换上下文）
- 测试更难（需要测试不相关的东西）
- 回滚更难（无法只回滚一个修复而不影响其他）
- 理解更难（git 历史讲不出清晰的故事）

**选择策略**：

**保守策略**（2 个 issue，低风险）：
- 排名第 1 的 P1 + 1 个高 ROI 的 P2
- 快速收益，充分测试
- 适用于：不确定时，想验证工作流

**平衡策略**（3-4 个 issue，混合复杂度）：
- 1 个 P1 + 2 个 P2 + 1 个 P3
- 影响力和工作量的良好平衡
- 适用于：对 issue 有信心，有 2-3 天时间

**激进策略**（5 个 issue，最大化吞吐量）：
- 全部 P1 + 排名靠前的 P2
- 仅当总工作量 ≤ 3 天且高连贯性时
- 适用于：issue 简单且高度相关

### 3.2 创建实施计划

在 `/tmp/implementation_plan.md` 中记录批量计划：

```markdown
# HotPlex 批量实施计划

## 选定 Issues（4 个，总 ROI: 198）

1. **#90** — feat(cli): add `hotplex update` subcommand (ROI: 72)
   - 类型: enhancement
   - 范围: CLI
   - 工作量: 4 小时
   
2. **#78** — fix(messaging): error handling (ROI: 68)
   - 类型: bug
   - 范围: messaging/adapters
   - 工作量: 6 小时
   
3. **#89** — perf(webchat): bundle code split (ROI: 45)
   - 类型: performance
   - 范围: webchat
   - 工作量: 8 小时
   
4. **#88** — refactor(messaging): extract BaseAdapter (ROI: 28)
   - 类型: refactor
   - 范围: messaging/adapters
   - 工作量: 4 小时

## 连贯性分析

- #88 和 #78: 都是 messaging adapter，高度连贯
- #90: CLI 工作，独立但工作量低
- #89: Webchat 性能，独立

**风险**: 中等 — 混合模块（messaging + CLI + webchat）

## 实施顺序

1. #88 (refactor) — 基础重构，其他 issue 可能依赖清晰结构
2. #78 (bug fix) — 依赖 #88 的重构
3. #90 (feature) — 独立，随时可做
4. #89 (performance) — 独立，随时可做

## 分支策略

- 分支: `batch/messaging-cli-webchat-fixes-issues-90-78-89-88`
- 基准: `main`
- 所有修复在同一分支
- 每个 issue 一个 commit

## 测试策略

- 每个修复的单元测试
- messaging 变更的集成测试
- CLI 和 webchat 的手动测试
- 覆盖率目标: ≥80%

## 时间线

- 总工作量: 22 小时（约 3 天）
- 实施: 18 小时
- 测试: 4 小时
```

## Phase 4: 实施与交付

**📚 详细实施指南**：完整 Phase 4 细节见 `references/implementation-guide.md`，包括：
- 逐步的仓库准备工作
- 分支创建和命名约定
- 按序实施 issue 的工作流
- Conventional commit 消息模板
- 集成测试流程
- 附带完整模板的 PR 创建

**快速概览**：

1. **准备仓库** — 拉取最新 main，确保干净状态
2. **创建批量分支** — `batch/<theme>-issues-<numbers>`
3. **按序实施 issue** — 每个 issue 一个 commit，遵循 HotPlex 标准
4. **最终集成测试** — 完整测试套件、linter、构建验证
5. **推送并创建 PR** — 一个附带完整描述的合并 PR

**关键实施标准**：
- **Go 1.26+** 使用最新语言特性
- **golangci-lint** — 频繁运行，立即修复
- **测试优先** — 实施前编写测试（TDD）
- **≥80% 覆盖率** — 安全/关键路径更高
- **Conventional commits** — type(scope): description 格式
- **原子提交** — 每个 commit 独立有效

## 最佳实践

### 连贯性优于数量

干净交付 3 个连贯的 issue 胜过混乱交付 5 个不相关的。连贯的批量讲述清晰的故事，更容易审查。

### 测试优先

实施前编写测试。测试作为可执行规范并防止回归。

### 每个 issue 一个 Commit

每个 commit 应该是原子的、独立有效的。这支持：
- 方便 git bisect 调试
- 需要时可选择性回滚
- 清晰的 git 历史

### 运行完整测试套件

不要只依赖单元测试。所有 issue 实施完毕后运行集成测试，捕获交互 bug。

### 清晰记录变更

PR 描述应按 issue 分别记录变更，包含 Problem/Solution/Impact 结构。帮助审查者理解改了什么和为什么改。

## 输出产物

批量 issue 管理产生以下产物：

1. `/tmp/hotplex_issues.json` — 原始 issue 数据
2. `/tmp/issue_analysis.md` — 每个 issue 的详细分析
3. `/tmp/issue_ranking.md` — 附 ROI 分数的排序列表
4. `/tmp/implementation_plan.md` — 批量实施计划
5. `/tmp/pr_tracking.md` — 单个 PR 状态追踪器
6. **一个合并 PR** — 包含所有 issue 修复的最终交付物

## 示例会话

**📚 完整演练**：完整示例会话见 `references/example-session.md`，展示：
- 真实 issue 分析和 ROI 计算
- 连贯批量选择（4 个 issue，ROI 278）
- 附 commit 消息的按序实施
- 完整 PR 描述模板
- 最终结果和时间节省（比传统工作流快 30%）

**快速预览**：
```
用户: "分析 HotPlex issues 并交付最高优先级的修复"

1. 获取 20 个 open issues
2. 分析、分类、计算 ROI
3. 选择 top 4: #90 (CLI update), #78 (messaging errors), 
   #89 (webchat perf), #88 (adapter refactor)
4. 验证连贯性: 3 个 messaging issue + 1 个 CLI issue = 可接受
5. 创建分支: batch/messaging-cli-fixes-issues-90-78-89-88
6. 实施 #88 → commit (先重构)
7. 实施 #78 → commit (修复依赖重构)
8. 实施 #90 → commit (独立功能)
9. 实施 #89 → commit (独立性能优化)
10. 运行所有测试 → 验证集成
11. 推送分支
12. 创建一个关闭 4 个 issue 的 PR
13. CI 通过、审查、合并 ✅
结果: 一次合并，解决 4 个 issue，节省 30% 时间
```

## 故障排除

**📚 完整故障排除指南**：详细解决方案见 `references/troubleshooting.md`：

1. **无法实施 issue A 因为它依赖 issue B**
   - 选择前检查依赖
   - 在批量中优先实施依赖项
   - 或推迟到下一批

2. **实施所有 issue 后测试失败**
   - 每次 commit 后运行测试，而非只在最后
   - 使用 git bisect 查找问题 commit
   - 专门修复那个 commit

3. **批量 PR 的 CI 失败**
   - 检查 GitHub Actions 日志
   - 确保 Go 版本匹配（1.26）
   - 如果某个 issue 导致失败，从批量中移除

4. **审查反馈要求拆分 PR**
   - 解释批量 PR 的好处
   - 如果审查者坚持，按逻辑主题拆分
   - 但如果 issue 连贯，优先保持批量

5. **批量变得太大（>5 个 issue 或 >3 天）**
   - 按主题拆分为多个批次
   - 例如："messaging 修复批次 1"、"messaging 修复批次 2"

## 核心洞察

**传统工作流**：一个 issue → 一个分支 → 一个 PR → 一次合并
**此 skill**：多个 issue → 一个分支 → 一个 PR → 一次合并

**为什么这个模式有效**：
- ✅ **减少合并冲突** — 一次冲突解决代替多次
- ✅ **更快审查** — 一次全局审查 vs 多次碎片化审查
- ✅ **全面测试** — 一起做集成测试，捕获交互 bug
- ✅ **单次部署** — 协调发布，需要时更容易回滚
- ✅ **清洁历史** — 逻辑分组讲述连贯的故事

**何时使用此 skill**：
- 有 2-5 个相关 issue 可以在 1-3 天内一起实施
- Issue 触及相似的代码区域（同一模块、层级或领域）
- 想减少合并冲突和审查开销
- 需要按 ROI 排列 issue 优先级并聚焦高影响工作

**何时不用此 skill**：
- Issue 完全不相关（不同模块、无主题关联）
- 总工作量超过 3 天
- Issue 有复杂的、难以解决的依赖
- 需要立即对单个关键问题做 hotfix

**记住**：此 skill 是工具，不是强制要求。在适合你的情况时使用它。
