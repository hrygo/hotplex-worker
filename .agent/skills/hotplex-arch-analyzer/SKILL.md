---
name: hotplex-arch-analyzer
description: HotPlex 项目架构和代码健康深度审计 — **立即调用此 skill 进行**：架构分析、代码质量审查、SOLID/DRY 合规检查、并发/性能审计、安全扫描、非功能分析、代码健康度改进、模块质量评估、存量 issue 审计与清理。**专为 HotPlex Gateway 多层架构优化**（WebSocket/Session/Worker/Messaging），自动创建/验证 GitHub Issue。增量式模块分析 + 存量 issue 审计 + 跨会话进度追踪 + 优先级排序 = **最有效的 /loop 循环执行工具**，适用于大型 Go 代码库的系统性审计。
---

# 架构深度分析器

## 为什么使用此 skill？

架构分析是复杂且易出错的工作 — 容易遗漏重要问题、产生误报、或浪费时间在低价值发现上。此 skill 通过以下方式解决这些问题：

**渐进式分析** — 每次 2-3 个方面，避免信息过载，让每个发现都经过深思熟虑
**智能优先级** — 自动优先分析最少审计模块，确保覆盖均衡
**置信度 + ROI 分诊** — 过滤误报和低价值发现，只创建高影响力 issue
**存量 Issue 审计** — 当 open issues 超过阈值时，自动切换为审计模式：核实、关闭、更正
**持久化进度** — 跨会话追踪，支持 `/loop` 连续执行，不会丢失工作
**HotPlex 专用优化** — 针对 Gateway 多层架构的特殊模式定制

**核心设计**：每次调用 = **一个分析周期**在**一个模块**上覆盖**2-3 个方面**。专为 `/loop` 设计 — 重复调用在 4 个深度阶段中渐进覆盖所有模块 × 所有方面。

**状态持久化在** `.claude/arch-analysis/progress.json` — 跨会话重启存活，启用 `/loop` 连续性。

### 为什么使用 /loop？

**手动执行的问题**：
- 需要记住调用 skill 数十次
- 容易丢失分析进度
- 难以跟踪覆盖哪些模块
- 容易过早停止

**使用 /loop 的好处**：
- **自动化**：设置后无需干预，持续分析所有模块
- **持久化**：进度文件保存状态，中断后可以恢复
- **可视化**：每次调用显示覆盖矩阵，清楚看到进度
- **均衡覆盖**：智能优先级确保最少分析的模块优先处理

**推荐 /loop 设置**：
```bash
/loop 10m /hotplex-arch-analyzer
```

这每 10 分钟运行一次分析，适合大型代码库的渐进式分析。调整间隔基于代码库大小和分析深度需求。

---

## 如何阅读本文档

本文档使用渐进式信息披露 — 快速开始只需阅读工作流概览和步骤 1-3。详细参考材料（模块发现、分析方面、进度文件模式）在后面供需要时查阅。

**快速路径**（新用户）：
1. 阅读工作流概览
2. 理解步骤 1-3（加载进度、选择模块、选择方面）
3. 跳到步骤 5（Issue 模板）开始使用

**深度路径**（经验用户）：
1. 完整阅读所有步骤
2. 参考常见陷阱故障排除
3. 自定义模块发现和方面选择逻辑

---

## 工作流概览

每次调用遵循两种模式之一，由**步骤 1.5（模式检测）**决定：

**分析模式**（默认）：
```
加载进度 → 选择模块 → 选择方面 → 深度分析 → 分诊 → 去重检查 → 创建/更新 Issue → 更新进度 → 报告
```

**审计模式**（open issues ≥ 阈值时激活）：
```
加载进度 → 选择 Issue → 验证代码 → 分类处理 → 更新进度 → 报告
```

**为什么需要双模式**：分析模式持续发现新问题，但问题积累后会降低团队信任度和审查效率。审计模式定期清理存量 issue，关闭误报、更正偏差，保持 issue backlog 的健康。两个模式交替运行，既不停止分析，也不让 issue 失控。

### 步骤 1：加载进度

读取 `.claude/arch-analysis/progress.json`。

**如果未找到** — 运行初始化：
1. 发现模块（见下面的模块发现）
2. 创建进度文件，所有模块的所有 aspects 待处理
3. 输出发现的模块列表
4. 继续到步骤 2

### 步骤 1.5：模式检测

每次调用开始时，检查是否应该进入审计模式：

```bash
# 获取当前 open architecture issues 数量
OPEN_COUNT=$(gh issue list --label "architecture" --state open --json number --limit 200 | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
echo "Open architecture issues: $OPEN_COUNT"
```

**决策规则**（按优先级）：

1. **用户显式指定模式** → 遵循用户指令（如 "审计 issue"、"review issues"、"audit mode"）
2. **Open issues ≥ 30** → 进入**审计模式**，审计 3-5 个最旧的 issue
3. **Open issues < 30** → 继续**分析模式**（步骤 2-7）

**审计与分析的交替**：当 open issues 在阈值附近波动时，审计和分析交替执行。每审计一轮后检查：如果 open issues 仍 ≥ 30 → 继续审计；否则切回分析模式。

**为什么阈值是 30**：经验表明，超过 30 个 open issues 时：
- 团队审查负担过重，新 issue 被忽略的概率激增
- 存量 issue 中积累了足够多的误报和过时发现值得清理
- 清理 backlog 比创建新 issue 产生更大的实际价值

### 步骤 2：选择目标模块

优先级（第一个匹配胜出）：
1. `analysis_count == 0` — 从未分析，最高优先级
2. 最低 `analysis_count` — 最少覆盖
3. 在平局中：优先核心模块（gateway > session > messaging > worker > config > others）
4. 仍然平局：按字母顺序

### 步骤 3：选择方面

每个模块在 4 个阶段中有 12 个方面。从 `aspects_pending` 中选择 2-3 个：
- 如果阶段 1 方面仍存在 → 从阶段 1 中选择
- 如果阶段 1 完成，阶段 2 仍存在 → 从阶段 2 中选择
- 以此类推
- 如果所有方面都覆盖 → 重新分析最旧的阶段以获取更深入的见解（增量深度）

### 步骤 4：深度分析

对于每个选定的方面，**读取目标模块中的所有源文件**（包括 `_test.go`）。

**为什么读取所有文件**：架构问题往往跨文件显现 — 只看单个文件会遗漏耦合模式、重复逻辑、或不一致的错误处理。

分析特定方面时，读取 `references/analysis-checklist.md` 中的相应部分，以获取要检查的内容的详细清单。清单提供了要验证的具体项目 — 将其用作指南，而非强制性逐项清单。

**分析产出标准**：每个发现必须包含四个要素：
- **什么**：具体问题描述
- **哪里**：`file.go:line_range` 引用（通常 2-3 个文件位置提供充分上下文）
- **为什么**：影响/风险量化（例如："影响 N 个调用点"，"每个请求持有锁约 ~200ms"）
- **如何**：重构方向 + **代码片段**显示 before/after 模式

**为什么需要代码片段**：没有代码示例的发现难以理解和实施。片段让问题立即可操作，无需审查者重新阅读源代码。

#### 代码片段指南

每个发现必须包含一个最小的代码示例，演示问题和修复方向。这使得问题可立即操作，无需重新阅读源代码。

**好的片段** — 显示问题模式和提议的替代方案：
```go
// 当前：静默错误丢失
if err != nil {
    log.Error("failed", "err", err)
    return nil  // 调用者不知道这失败了
}

// 提议：带上下文传播
if err != nil {
    return fmt.Errorf("handleMessage: %w", err)
}
```

**太冗长** — 不要粘贴整个函数。提取重要的 5-15 行模式。
**太模糊** — 不要写"重构使用接口"而不显示接口签名。

对于结构/设计发现（SOLID、DRY），显示提议的接口或类型分解：
```go
// 提议的提取
type MessagePipeline interface {
    Parse(ctx context.Context, raw any) (*ParsedMessage, error)
    Filter(ctx context.Context, msg *ParsedMessage) (bool, error)
    Route(ctx context.Context, msg *ParsedMessage) error
}
```

**严重性级别**：Critical（数据丢失 / 安全 / 死锁）> High（可靠性 / 性能）> Medium（可维护性 / DRY）> Low（风格 / 命名 / 次要）

如果方面没有产生真正的发现，则跳过 — 不要制造问题。

### 步骤 4.5：发现分诊（置信度 + ROI）

**为什么分诊很重要**：未经分诊的分析会产生大量误报和低价值发现，浪费审查时间并降低对架构分析的信任。通过评估置信度和 ROI，只创建真正重要的问题。

在创建 issue 之前，通过两个过滤器对每个发现进行分诊：

#### 置信度评估

评估您对这是真正问题而非误报的确定性程度：

| 级别 | 标准 | 行动 |
|-------|----------|------|
| **高** | 多个代码引用确认问题；模式明确；可用具体示例轻松演示 | **创建 issue** |
| **中** | 单个实例但模式清晰；匹配众所周知的反模式；中等代码证据 | **创建 issue**（在 issue 中注明置信度） |
| **低** | 推测性；可能是有意的设计；代码证据不足；需要更深入的调查以确认 | **丢弃**（在进度中记为"观察到但不可操作"） |

**为什么丢弃低置信度发现**：它们浪费审查者时间，并降低对架构分析过程的信任。在进度中记录它们以便后续重新分析时重新评估。

#### ROI 评估

评估修复每个发现的投入产出比 — 这决定是否值得花费工程时间：

| 维度 | 问题要问 |
|-----------|-----------------|
| **修复投入** | 要更改的代码行？回归风险？是否需要架构更改？ |
| **修复后影响** | 它会解锁其他改进吗？减少 bug 表面积吗？提高可测试性吗？ |
| **延迟影响** | 它会进一步衰减吗？阻止其他工作吗？导致生产问题吗？ |

**ROI 分类和行动**：
- **高 ROI** → 小投入，大影响（快速获胜，解锁改进）→ **总是创建 issue**
- **中 ROI** → 中等投入，明确影响（重构模式，减少重复）→ **创建 issue**
- **低 ROI** → 大投入，边际影响（风格问题，过早抽象）→ **丢弃**，除非是 Critical/High 严重性

**为什么 ROI 比严重性更重要**：一个 Low 严重性但高 ROI 的问题（如简单的 DRY 重构）比 High 严重性但低 ROI 的问题（如需要重写的大型重构）更值得优先处理。

#### 分诊输出

分诊后，将发现组织到 issue 中：
- 以**高置信度 + 高 ROI**发现开头（最具影响力）
- 然后是**中置信度或中 ROI**发现
- 省略低置信度或低 ROI 发现（在进度文件中注明）

### 步骤 4.8：Issue 去重与合并检查

**为什么需要去重**：长期运行的分析器容易对同一模块重复创建 issue，尤其是：
- 不同分析方面（如 SOLID 和 DRY）发现同一代码模式的不同表现
- 重新分析时再次发现已被 issue 跟踪的问题
- 大问题被拆分为多个子问题，导致碎片化

去重检查确保：每个真实问题只有一个 issue，避免审查疲劳和重复工作。

#### 去重检查流程

**第一步：按模块搜索现有 issue**

```bash
# 搜索当前模块相关的所有 open architecture issues
gh issue list --label "architecture" --state open \
  --json number,title,body,createdAt \
  --limit 200 | python3 -c "
import json, sys
issues = json.load(sys.stdin)
module = '$MODULE'  # 当前模块，如 internal/gateway
module_short = module.split('/')[-1]  # 如 gateway
for i in issues:
    title = i['title'].lower()
    body = (i.get('body') or '').lower()
    if module in title or module in body or module_short in title:
        print(f\"{i['number']}: {i['title'][:80]}\")
"
```

**同时检查进度文件**：读取 `progress.json` 中当前模块的 `issues_created` 数组，获取该模块历史上创建的所有 issue 编号（含已关闭的），用于语义匹配。

**第二步：语义重叠检测**

对第一步返回的每个相关 issue，读取其 body，与本周期发现逐一比较：

| 比较维度 | 检查方法 |
|----------|----------|
| **文件重叠** | 本发现引用的文件是否在已有 issue 的 Key files 中出现 |
| **位置重叠** | 行号范围是否有交集（±50 行容差，考虑代码变动） |
| **方面重叠** | 是否覆盖同一分析方面（如都是 error-handling） |
| **问题模式** | 描述的问题模式是否本质相同（如"静默吞掉错误"vs"错误未传播"= 同一问题） |

**重叠判定规则**：
- 文件重叠 + 位置重叠 → **高概率重复**
- 文件重叠 + 方面重叠 + 相似问题模式 → **中概率重复**，需读取已有 issue body 确认
- 仅方面重叠 → **低概率重复**，通常可创建新 issue

**第三步：判定与处理**

对每个发现，给出以下判定之一：

| 判定 | 条件 | 处理方式 |
|------|------|----------|
| **new** | 无任何重叠，全新发现 | 进入步骤 5 正常创建 |
| **duplicate** | 与已有 issue 描述完全相同的问题 | 跳过创建，进度中记录 |
| **subsumed** | 本发现是已有 issue 的子集或特殊情况 | 更新已有 issue，添加此发现为补充 |
| **supersedes** | 本发现更全面，已有 issue 是其子集 | 扩展已有 issue 的标题和内容 |
| **related** | 相关但独立，共享部分代码但根本原因不同 | 创建新 issue，引用已有 issue |

**第四步：执行处理**

- **new** → 进入步骤 5 正常创建
- **duplicate** → 在进度 `findings_dropped` 中记录：`{"finding": "name", "reason": "Duplicate of issue N — <overlap reason>"}`
- **subsumed** → 更新已有 issue（添加补充发现）：
  ```bash
  gh issue comment <EXISTING_NUMBER> --body "$(cat <<'EOF'
  arch-analysis (cycle N, <aspect>): 补充发现。

  #### <finding-name>

  **Location**: \`file.go:line_range\`
  **问题**: <简述 — 2-3 句>

  **代码片段**:
  \`\`\`go
  <5-10 行代码>
  \`\`\`

  **补充说明**: <此发现如何与原 issue 关联>
  EOF
  )"
  ```
- **supersedes** → 更新已有 issue 标题和 Finding Summary 表，扩展范围
- **related** → 创建新 issue，在 Background 中引用：`Related: issue N`

#### 去重优先级

当发现可能与多个已有 issue 相关时，按以下优先级匹配：
1. 同一模块 + 同一文件 + 同一方面 → **最可能是重复**
2. 同一模块 + 同一文件 + 不同方面 → **可能是子集**（如 SOLID 和 DRY 发现同一代码段）
3. 同一模块 + 不同文件 + 同一方面 → **可能相关**
4. 不同模块 → **大概率独立**

### 步骤 5：创建 GitHub Issue

将此周期中经过步骤 4.8 判定为 **new** 或 **related** 的发现合并到**一个** GitHub issue 中。被判定为 duplicate/subsumed/supersedes 的发现已在步骤 4.8 中处理，不进入此步骤。

**为什么合并而不是拆分**：相关问题放在一个 issue 中提供上下文 — 审查者可以看到模式的完整图景，实施者可以在一次 PR 中解决相关问题，减少上下文切换。

**重要的格式规则**：
- **避免 `#` 数字**：GitHub 将 `#1` 解释为 issue 引用。使用描述性标题或基于 bullet 的编号
- **使用 `#### finding-name` 或 `- **Finding Name**:`** 作为子标题，永远不要用 `#### 1. Title`
- **写"cycle N"或"cycle number N"** 而不是"cycle #N"

**格式规则**：避免 `#` 数字（GitHub 会解析为 issue 引用），使用 `#### finding-name` 作为子标题，写"cycle N"而不是"cycle #N"（`#N` 会被 GitHub 解析为 issue 引用）。

**Issue 结构**：Background → Finding Summary → Findings（含 Severity/Confidence/ROI/Location/Current Pattern/Proposed Fix/AC）→ Implementation Priority → Out of Scope → Verification。完整模板和 AC 撰写指南见 `references/issue-template.md`。

**标题格式**：`<type>(<module>): <scope>` — 类型：`refactor` / `fix` / `perf` / `security` / `chore`

**标签映射**：Critical → `P1`，High → `P2`，Medium/Low → `P3`

如果没有值得 issue 的发现，跳过创建并在进度中注明。

### 步骤 6：更新进度

更新 `.claude/arch-analysis/progress.json`：
- 增加模块的 `analysis_count`
- 将分析的方面移至 `aspects_covered`
- 将 issue 号添加到 `issues_created`
- 将丢弃的发现添加到 `findings_dropped`（带原因）
- 更新 `last_analyzed` 时间戳
- 增加 `total_cycles`
- 添加到 `recent_activity` 日志（保留最后 20 个）

### 步骤 7：报告

输出包含进度矩阵的简洁摘要：

```
## Analysis Cycle N Complete

**Module**: `internal/<module>` (分析通过 M)
**Aspects**: <aspect1>, <aspect2>
**Findings**: X Critical, Y High, Z Medium, W Low
**Issue**: <issue URL 或"跳过 — 无可操作发现">

### Coverage Matrix

| Module | Ph1 | Ph2 | Ph3 | Ph4 | Issues | Status |
|--------|-----|-----|-----|-----|--------|--------|
| gateway | 3/3 | 2/3 | — | — | 2 | 进行中 |
| session | 3/3 | 3/3 | 2/2 | — | 3 | Ph3 完成 |
| messaging | 1/3 | — | — | — | 1 | Ph1 已开始 |
| ... | | | | | | |

### Stats
- 总周期：N
- 总 issues：M
- 完全覆盖的模块：A/总计
- **下一个目标**：`internal/<next-module>` (analysis_count=N, Phase P 待处理)
```

覆盖矩阵给出了每个模块在 4 个阶段中位置的快速可视化。使用 `n/3`（或 Ph3/4 的 `n/2`）格式进行中的阶段，`—` 表示未开始，✓ 表示完成。

---

## Issue 审计模式

当步骤 1.5 决定进入审计模式时，执行 5 步审计流程替代分析模式（步骤 2-7）。

**审计流程概览**：选择审计目标 → 逐个验证代码现状 → 判定处理（fixed/invalid/outdated/valid/duplicate）→ 更新进度 → 审计报告。

**每次审计 3-5 个 issue**，优先从未审计过的最旧 issue 开始，同模块 issue 放在一起审计。

完整审计步骤（含 bash 命令、判定表、处理操作、进度更新、报告格式）见 `references/audit-mode.md`。

---

## 模块发现

**主要来源**：`CLAUDE.md` STRUCTURE 部分 — 如果存在，使用记录的模块边界。

**后备**：扫描 `internal/`、`pkg/`、`cmd/` 目录。每个带有 `.go` 文件的子目录 = 一个模块。

### 模块分组

分组紧密耦合的子包：
- `messaging/slack/` + `messaging/feishu/` → 作为子模块在 `messaging` 下分析
- `worker/claudecode/` + `worker/opencodeserver/` + `worker/pi/` → 在 `worker` 下的子模块
- `cli/checkers/` + `cli/onboard/` → 在 `cli` 下的子模块

父模块（`messaging`、`worker`、`cli`）获得自己的分析通过，涵盖共享代码（bridge、接口、基本类型）。

### 标准模块列表（HotPlex）

```
internal/gateway     — WebSocket hub, conn, handler, bridge, API
internal/session     — 状态机, store, pool, key derivation
internal/messaging   — 平台适配器, bridge, interaction, STT
  internal/messaging/slack   — Slack Socket Mode 适配器
  internal/messaging/feishu  — Feishu WS 适配器
internal/worker      — 基础 worker, proc manager
  internal/worker/opencodeserver — OCS 单例 + worker
internal/config      — Viper 配置, 热重载
internal/agentconfig — Agent 个性/上下文加载器
internal/security    — JWT, SSRF, 路径安全, 命令白名单
internal/admin       — Admin API 处理器
internal/aep         — AEP v1 编解码器
internal/cli         — Checker 注册表, onboard 向导
internal/skills      — Skills 发现
internal/metrics     — Prometheus 计数器
internal/tracing     — OpenTelemetry 设置
pkg/events           — AEP 包络 + 事件类型
cmd/hotplex          — Cobra CLI 入口点
```

---

## 分析方面

12 个方面跨 4 个阶段。每个阶段更深入 — 阶段 1 是结构性的，阶段 4 是细粒度的。

### 阶段 1：架构与设计
| # | 方面 | 重点 |
|---|--------|-------|
| 1 | **SOLID** | SRP 违规、接口隔离、依赖倒置、开/闭 |
| 2 | **DRY** | 模块内的重复逻辑、值得提取的跨模块模式 |
| 3 | **耦合** | 导入图、循环依赖、稳定依赖原则 |

### 阶段 2：可靠性
| # | 方面 | 重点 |
|---|--------|-------|
| 4 | **错误处理** | 静默失败、错误吞咽、哨兵错误、包装一致性 |
| 5 | **并发** | 竞争条件、互斥锁排序、goroutine 生命周期、通道泄漏 |
| 6 | **资源管理** | 连接/文件/goroutine 泄漏、defer 清理、关闭路径 |

### 阶段 3：性能与规模
| # | 方面 | 重点 |
|---|--------|-------|
| 7 | **性能** | 热路径、不必要的分配、N+1 模式、缓冲区重用 |
| 8 | **可扩展性** | 在 10x/100x 负载下的单点故障、瓶颈 |

### 阶段 4：安全与质量
| # | 方面 | 重点 |
|---|--------|-------|
| 9 | **安全** | 输入验证缺口、注入风险、认证/授权绕过 |
| 10 | **可观测性** | 结构化日志缺口、指标覆盖、跟踪跨度 |
| 11 | **可测试性** | DI 覆盖、可模拟性、错误路径的测试缺口 |
| 12 | **代码质量** | 圈复杂度、上帝对象、死代码、命名一致性 |

### 为什么这个顺序？

**阶段 1（架构与设计）优先**：因为结构性问题影响所有后续代码。在修复 SOLID/DRY 违规后再优化性能，避免在错误设计上浪费时间。

**阶段 2（可靠性）其次**：并发和错误处理问题是常见的生产故障根源。修复它们可以提高系统稳定性。

**阶段 3（性能与规模）第三**：只有在架构稳定和可靠性问题解决后才优化。过早优化是万恶之源。

**阶段 4（安全与质量）最后**：这些是"卫生因素" — 重要但通常不阻塞功能。在最后阶段确保代码库健康。

**为什么每次 2-3 个方面**：更多方面会导致表面分析。更少方面虽然深度更好，但需要更多调用才能覆盖。2-3 个方面是深度和速度的最佳平衡。

---

## 参考文件索引

以下详细内容已提取到 `references/` 目录，按需读取：

| 文件 | 内容 | 何时读取 |
|------|------|----------|
| `references/issue-template.md` | 完整 issue bash 模板 + 标题/标签格式 + AC 撰写指南 | 创建 issue 前（步骤 5） |
| `references/audit-mode.md` | 审计模式完整流程（5 步 + bash 命令 + 判定表） | 进入审计模式时（步骤 1.5） |
| `references/analysis-checklist.md` | 12 个方面的详细分析清单 | 深度分析时（步骤 4） |
| `references/progress-and-metrics.md` | 进度文件 schema + 边缘情况 + 成功指标 + 持续改进 | 需要查阅 schema 或调优时 |
| `references/troubleshooting.md` | 9 个常见陷阱及解决方案 | 遇到问题或结果不理想时 |

**进度文件关键字段**：`findings_dropped` 跟踪分诊丢弃和去重判为 duplicate 的发现。`audited_issues` 跟踪已审计 issue 避免重复审计。完整 schema 见 `references/progress-and-metrics.md`。

**健康指标速查**：Issue 创建率 60-80%，有效率 ≥70%，审计回收率 10-30%，去重拦截率 10-25%。详细指标和调整建议见 `references/progress-and-metrics.md`。
