---
name: hotplex-arch-analyzer
description: HotPlex 项目深度架构和非功能分析，系统性代码库审计。增量式模块分析、跨会话进度追踪、优先分析最少审计模块、自动创建 GitHub Issue 和验收标准。**使用此 skill**：架构分析、代码审计、质量审查、非功能性检查、SOLID/DRY 合规检查、代码健康度改进、模块质量评估。**HotPlex 专用**，针对 Gateway 多层架构（WebSocket/Session/Worker/Messaging）优化分析。支持 /loop 循环执行，适用于大型 Go 代码库的渐进式分析。
---

# 架构深度分析器

增量式、逐模块的架构和非功能分析，具有持久化进度追踪。

每次调用 = **一个分析周期**在**一个模块**上覆盖**2-3 个方面**。为 `/loop` 设计 — 重复调用在 4 个深度阶段中渐进覆盖所有模块 × 所有方面。

**状态持久化在** `.claude/arch-analysis/progress.json` — 跨会话重启存活，启用 `/loop` 连续性。

## 工作流（每次调用）

```
加载进度 → 选择模块 → 选择方面 → 深度分析 → 分诊（置信度+ROI）→ 创建 Issue → 更新进度 → 报告
```

### 步骤 1：加载进度

读取 `.claude/arch-analysis/progress.json`。

**如果未找到** — 运行初始化：
1. 发现模块（见下面的模块发现）
2. 创建进度文件，所有模块的所有 aspects 待处理
3. 输出发现的模块列表
4. 继续到步骤 2

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

分析特定方面时，读取 `references/analysis-checklist.md` 中的相应部分，以获取要检查的内容的详细清单。清单提供了要验证的具体项目 — 将其用作指南，而非强制性逐项清单。

分析必须产生**具体的、可操作的发现**，每个都有：
- **什么**：具体问题
- **哪里**：`file.go:line_range` 引用（包括足够的上下文 — 通常 2-3 个文件位置）
- **为什么**：影响/风险（为什么这很重要 — 尽可能量化："影响 N 个调用点"，"每个请求持有锁约 ~200ms"）
- **如何**：重构方向以及**代码片段**显示 before/after 模式（见代码片段指南）

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

在创建 issue 之前，通过两个过滤器对每个发现进行分诊：

#### 置信度评估

评估您对这是真正问题而非误报的确定性程度：

| 级别 | 标准 |
|-------|----------|
| **高** | 多个代码引用确认问题；模式明确；可用具体示例轻松演示 |
| **中** | 单个实例但模式清晰；匹配众所周知的反模式；中等代码证据 |
| **低** | 推测性；可能是有意的设计；代码证据不足；需要更深入的调查以确认 |

**丢弃低置信度发现** — 它们浪费审查者时间。在进度中记为"观察到但不可操作"。

#### ROI 评估

评估修复每个发现的投入产出比：

| 维度 | 问题要问 |
|-----------|-----------------|
| **修复投入** | 要更改的代码行？回归风险？是否需要架构更改？ |
| **修复后影响** | 它会解锁其他改进吗？减少 bug 表面积吗？提高可测试性吗？ |
| **延迟影响** | 它会进一步衰减吗？阻止其他工作吗？导致生产问题吗？ |

分类 ROI：
- **高 ROI** → 小投入，大影响（快速获胜，解锁改进）
- **中 ROI** → 中等投入，明确影响（重构模式，减少重复）
- **低 ROI** → 大投入，边际影响（风格问题，过早抽象）

**丢弃低 ROI 发现**，除非它们是 Critical/High 严重性。

#### 分诊输出

分诊后，将发现组织到 issue 中：
- 以**高置信度 + 高 ROI**发现开头（最具影响力）
- 然后是**中置信度或中 ROI**发现
- 省略低置信度或低 ROI 发现（在进度文件中注明）

### 步骤 5：创建 GitHub Issue

将此周期的所有发现合并到**一个** GitHub issue 中。

**重要的格式规则**：
- 不要在 issue 正文的任何地方使用 `#` 后跟数字 — GitHub 将这些解释为 issue 引用（例如 `#1` 链接到 issue 1）。使用描述性标题或基于 bullet 的编号。
- 使用 `#### finding-name` 或 `- **Finding Name**:` 作为子标题，永远不要用 `#### 1. Title`
- 写"cycle N"或"cycle number N"而不是"cycle #N"

#### Issue 模板

Issue 遵循结构化格式，包含 6 个部分：Background → Finding Summary → Findings → Implementation Priority → Out of Scope → Verification。

```bash
gh issue create --title "<type>(<module>): <concise-scope-description>" \
  --label "architecture" --label "<severity-label>" \
  --body "$(cat <<'EOF'
## Background

<1-2 句话：模块的角色、当前规模（文件数、行数）、为什么进行此分析>

**Scope**: <aspect1>, <aspect2> — cycle N (模块分析通过 M)
**Key files**: `<file1.go>`, `<file2.go>`, `<file3.go>`

---

## Finding Summary

| Category | Critical | High | Medium | Low |
|----------|----------|------|--------|-----|
| <aspect1> | <n> | <n> | <n> | <n> |
| <aspect2> | <n> | <n> | <n> | <n> |
| **合计** | **<n>** | **<n>** | **<n>** | **<n>** |

---

## Findings

### <Aspect Name>

#### <descriptive-finding-name-without-numbers>

**Severity**: Critical | **Confidence**: High | **ROI**: High
**Location**: `file.go:123-145`, `file2.go:67-89`

**Problem**: <什么错了，为什么重要 — 量化："影响 N 个调用点"，"持有锁约 ~Xms">

**Current Pattern**:
```go
// file.go:123-145
<5-15 行摘录显示问题代码>
```

**Proposed Fix**:
```go
<提议的代码显示修复方向 — 接口、类型分解或更正的逻辑>
```

**Estimated Impact**: <量化："~N 行减少"，"防止 X 类 bug"，"启用 Y">

**Acceptance Criteria**:
- [ ] <具体的、可验证的更改 — 文件 + 预期行为>
- [ ] <添加/更新测试以验证>
- [ ] <无回归：什么绝不能更改>

---

<重复每个发现，用 --- 分隔>

---

## Implementation Priority

| Finding | Priority | Effort | Risk | Impact |
|---------|----------|--------|------|--------|
| <finding-name-1> | P0 | Small | Low | ~N 行，解锁 X |
| <finding-name-2> | P1 | Medium | Medium | 防止 Y 类 bug |
| <finding-name-3> | P2 | Large | Low | 改进 Z |

**Recommended starting point**: <首先解决哪个发现以及为什么 — 通常是 P0/High-ROI>

---

## Out of Scope

以下区域有意不更改：
- <area1>: <原因 — 例如，"平台 API 差异使抽象适得其反">
- <area2>: <原因 — 例如，"已经通过现有接口很好地抽象">

---

## Verification

- [ ] `make test` 通过，无回归
- [ ] `make lint` 不产生新警告
- [ ] <模块特定的行为验证>
EOF
)"
```

#### 标题格式

使用 conventional commit 风格：`<type>(<module>): <scope>`

| 类型 | 何时 |
|------|------|
| `refactor` | DRY/SOLID/耦合发现 |
| `fix` | 错误处理/并发/资源泄漏发现 |
| `perf` | 性能/可扩展性发现 |
| `security` | 安全发现 |
| `chore` | 可观测性/可测试性/代码质量发现 |

#### 标签映射

按所有发现的最大严重性：
- Critical → `P1`
- High → `P2`
- Medium/Low → `P3`

如果没有值得 issue 的发现（都低/信息性），跳过 issue 创建并在进度中注明。

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

---

## 进度文件模式

```json
{
  "version": 1,
  "last_updated": "2026-04-29T21:36:00+08:00",
  "total_cycles": 0,
  "modules": {
    "internal/gateway": {
      "analysis_count": 0,
      "aspects_covered": [],
      "aspects_pending": [
        "solid", "dry", "coupling",
        "error-handling", "concurrency", "resource-mgmt",
        "performance", "scalability",
        "security", "observability", "testability", "code-quality"
      ],
      "issues_created": [],
      "findings_total": 0,
      "findings_dropped": [],
      "last_analyzed": null
    }
  },
  "issues": [],
  "recent_activity": []
}
```

`findings_dropped` 数组跟踪分诊期间丢弃的低置信度或低 ROI 发现 — 便于重新分析通过中重新访问。每个条目：`{"finding": "name", "reason": "Low confidence — may be intentional"}`。

---

## AC 撰写指南

好的 AC 是**具体的、可验证的和可测试的**。每个发现的 AC 应该：

1. **陈述更改**：必须修改什么代码/文件
2. **定义成功**：如何验证修复有效（测试用例、行为）
3. **指定约束**：绝不能更改什么（向后兼容、性能预算）

**模式**：对于每个发现，写 2-4 个 AC 项目，涵盖更改、其验证和边界条件。

**示例**：

坏："修复错误处理"
好：
```
- [ ] `bridge.go:HandleMessage` 返回错误而不是记录并返回 nil
- [ ] 错误通过 SessionConn.Send 使用 AEP 错误事件传播到调用者
- [ ] 添加测试：`TestHandleMessage_ErrorPropagation` 验证错误到达客户端
```

坏："改进性能"
好：
```
- [ ] 在 `WritePump` 中用 sync.Pool 替换每个消息的 `bytes.Buffer` 分配
- [ ] 基准 `BenchmarkWritePump_Throughput` 显示 <10ns/op 改进
- [ ] 在 `go test -count=5 -bench=.` pprof 下无分配增加
```

坏："重构适配器以共享代码"
好（受 issue 65 风格启发）：
```
- [ ] 在 `messaging/platform_adapter.go` 中将共享适配器字段提取到 `PlatformAdapter` 基础结构
- [ ] Slack 和 Feishu 适配器嵌入 `*PlatformAdapter` 而不是重复字段
- [ ] `make test` 通过，两个适配器测试套件零回归
- [ ] 添加新平台适配器需要实现 3 个接口 + 1 个 StreamingAPI，而不是 15+ 文件
```

---

## 边缘情况

- **无发现**：如果分析没有产生可操作的结果，更新进度（将方面标记为已覆盖）并跳过 issue 创建。在进度中注明"无重要发现"。
- **跨模块发现**：如果分析揭示跨越多个模块的问题，在 issue 中注明它们但标记主模块。添加 `cross-cutting` 注释。
- **模块太大**（>1500 行）：将拆分为子模块进行分析（例如，`gateway` → `gateway/core` + `gateway/bridge` + `gateway/api`）。独立跟踪子模块。
- **重新分析**：当模块的所有方面都覆盖时，重新分析会更深入 — 查找首次通过中遗漏的问题，检查以前的发现是否已解决，识别新模式。
