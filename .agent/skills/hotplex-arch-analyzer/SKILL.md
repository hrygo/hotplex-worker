---
name: hotplex-arch-analyzer
description: HotPlex 项目架构和代码健康深度审计 — **立即调用此 skill 进行**：架构分析、代码质量审查、SOLID/DRY 合规检查、并发/性能审计、安全扫描、非功能分析、代码健康度改进、模块质量评估。**专为 HotPlex Gateway 多层架构优化**（WebSocket/Session/Worker/Messaging），自动创建 GitHub Issue 和验收标准。增量式模块分析 + 跨会话进度追踪 + 优先级排序 = **最有效的 /loop 循环执行工具**，适用于大型 Go 代码库的系统性审计。
---

# 架构深度分析器

## 为什么使用此 skill？

架构分析是复杂且易出错的工作 — 容易遗漏重要问题、产生误报、或浪费时间在低价值发现上。此 skill 通过以下方式解决这些问题：

**渐进式分析** — 每次 2-3 个方面，避免信息过载，让每个发现都经过深思熟虑
**智能优先级** — 自动优先分析最少审计模块，确保覆盖均衡
**置信度 + ROI 分诊** — 过滤误报和低价值发现，只创建高影响力 issue
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

每次调用遵循 7 步流程，从加载进度到创建 GitHub issue：

```
加载进度 → 选择模块 → 选择方面 → 深度分析 → 分诊（置信度+ROI）→ 创建 Issue → 更新进度 → 报告
```

**为什么这样设计**：这种流程确保每次调用都产生可操作的结果，同时保持长期分析轨迹。分诊步骤防止创建低价值 issue，节省团队审查时间。

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

### 步骤 5：创建 GitHub Issue

将此周期的所有发现合并到**一个** GitHub issue 中。

**为什么合并而不是拆分**：相关问题放在一个 issue 中提供上下文 — 审查者可以看到模式的完整图景，实施者可以在一次 PR 中解决相关问题，减少上下文切换。

**重要的格式规则**：
- **避免 `#` 数字**：GitHub 将 `#1` 解释为 issue 引用。使用描述性标题或基于 bullet 的编号
- **使用 `#### finding-name` 或 `- **Finding Name**:`** 作为子标题，永远不要用 `#### 1. Title`
- **写"cycle N"或"cycle number N"** 而不是"cycle #N"

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

### 为什么这个顺序？

**阶段 1（架构与设计）优先**：因为结构性问题影响所有后续代码。在修复 SOLID/DRY 违规后再优化性能，避免在错误设计上浪费时间。

**阶段 2（可靠性）其次**：并发和错误处理问题是常见的生产故障根源。修复它们可以提高系统稳定性。

**阶段 3（性能与规模）第三**：只有在架构稳定和可靠性问题解决后才优化。过早优化是万恶之源。

**阶段 4（安全与质量）最后**：这些是"卫生因素" — 重要但通常不阻塞功能。在最后阶段确保代码库健康。

**为什么每次 2-3 个方面**：更多方面会导致表面分析。更少方面虽然深度更好，但需要更多调用才能覆盖。2-3 个方面是深度和速度的最佳平衡。

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

好的 AC 是**具体的、可验证的和可测试的**。

**为什么 AC 很重要**：没有清晰 AC 的 issue 往往导致不完整的实现或需要多次返工。好的 AC 让实施者知道"完成"是什么样子，让审查者可以验证实现。

每个发现的 AC 应该：

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

## 常见陷阱和故障排除

### 陷阱 1：过度分析

**症状**：每个模块产生 20+ 发现，issue 长达数千行，审查者不知所措

**原因**：没有应用置信度 + ROI 分诊，或设置了过低的分诊阈值

**解决**：
- 提高分诊阈值 — 只保留高置信度 + 高/中 ROI 的发现
- 专注于每个方面最重要的 2-3 个问题，而不是所有问题
- 记住：目标是可操作的改进，不是完整的代码审计报告

### 陷阱 2：误报

**症状**：创建的 issue 后来被标记为"有意设计"或"不会修复"

**原因**：低置信度发现被包含，或缺乏对模块设计意图的上下文

**解决**：
- 严格执行置信度评估 — 低置信度发现应该被丢弃，而不是创建 issue
- 在 issue 中注明"可能是设计选择" — 让审查者决定
- 查看模块的测试文件以理解预期行为

### 陷阱 3：低 ROI 发现

**症状**：issue 创建后数月无人处理，或被标记为"good first issue"但优先级低

**原因**：发现了真实问题，但修复投入太大或影响太小

**解决**：
- 在创建 issue 前问："这值得花费工程时间吗？"
- 大投入 + 小影响 = 丢弃，除非是安全/数据丢失问题
- 专注于"快速获胜" — 小改动产生大影响的发现

### 陷阱 4：缺少代码示例

**症状**：issue 中只有描述性文字，审查者要求"请展示代码示例"

**原因**：跳过了代码片段指南，或示例太冗长/太模糊

**解决**：
- 每个发现必须包含 before/after 代码片段（5-15 行）
- 显示具体问题模式，不只是"重构使用接口"
- 对于设计问题，显示提议的接口签名

### 陷阱 5：进度文件损坏

**症状**：`.claude/arch-analysis/progress.json` 格式错误，skill 无法加载

**原因**：手动编辑或并发写入冲突

**解决**：
- 不要手动编辑进度文件 — skill 会管理它
- 如果损坏，删除文件让 skill 重新初始化
- 使用 `jq` 验证 JSON：`jq . .claude/arch-analysis/progress.json`

### 陷阱 6：模块边界不清

**症状**：分析覆盖了不该包含的代码，或遗漏了相关文件

**原因**：模块发现逻辑与实际代码结构不匹配

**解决**：
- 优先使用 CLAUDE.md 中的 STRUCTURE 部分
- 对于大型模块（>1500 行），拆分为子模块
- 在进度文件中手动调整模块列表

### 陷阱 7：/loop 执行中断

**症状**：`/loop` 在中途停止，或重复分析同一模块

**原因**：未处理的错误或进度文件更新失败

**解决**：
- 检查最近的 `recent_activity` 日志以查找失败模式
- 确保每次调用都更新 `last_analyzed` 时间戳
- 如果卡住，手动增加模块的 `analysis_count` 以跳过它

### 陷阱 8：Issue 格式错误

**症状**：GitHub 错误渲染 issue，或数字变成链接

**原因**：使用了 `#` 数字或违反了其他格式规则

**解决**：
- 严格遵循 issue 模板
- 使用"cycle N"而不是"cycle #N"
- 验证：创建测试 issue 检查渲染

---

## 边缘情况处理

### 无发现

**场景**：分析完成后没有产生可操作的结果

**推荐处理**：更新进度（将方面标记为已覆盖）并跳过 issue 创建。在进度中注明"无重要发现"。

**为什么这是好事**：说明模块在这方面很健康。不需要强制创建 issue。

### 跨模块发现

**场景**：分析揭示跨越多个模块的问题

**推荐处理**：在 issue 中注明所有受影响的模块，但标记主模块。添加 `cross-cutting` 注释以便后续跟踪。

**为什么标记主模块**：保持进度文件简单，同时确保问题不会在裂缝中遗漏。

### 模块太大（>1500 行）

**场景**：单个模块过大，难以在一次分析中覆盖

**推荐处理**：将拆分为子模块进行分析（例如，`gateway` → `gateway/core` + `gateway/bridge` + `gateway/api`）。独立跟踪子模块。

**为什么拆分**：大型模块的分析质量下降。拆分后可以更深入地关注每个部分。

### 重新分析

**场景**：模块的所有方面都已覆盖，需要重新分析

**推荐处理**：重新分析会更深入 — 查找首次通过中遗漏的问题，检查以前的发现是否已解决，识别新模式。

**为什么重新分析有价值**：代码库在演化，以前的发现可能已解决，新问题可能出现。重新分析是健康检查。

---

## 成功指标

如何判断架构分析是否有效？

### 好的信号

- **Issue 创建率**：60-80% 的分析周期创建 issue（不过多，也不过少）
- **Issue 解决率**：创建的 issue 在 2-4 周内解决或纳入路线图
- **发现质量**：审查者反馈"有用的发现"、"可操作的问题"
- **重复分析价值**：第二次分析发现新问题或确认旧问题已解决
- **覆盖进展**：覆盖矩阵显示持续进展，没有模块被遗漏

### 需要调整的信号

- **太多低质量 issue**：收紧置信度 + ROI 分诊阈值
- **太少 issue**：放宽分诊阈值，或检查是否过于保守
- **重复的误报**：提高置信度要求，添加更多上下文检查
- **模块分析卡住**：检查进度文件，可能需要手动调整模块边界
- **Issue 长期未解决**：重新评估 ROI 评估，可能创建的优先级错误

### 持续改进

**定期回顾**（建议每 2-4 周）：
- 检查创建的 issue 的状态
- 收集审查者和实施者的反馈
- 调整分诊阈值和方面选择逻辑
- 更新模块边界和分组

**为什么需要持续改进**：架构分析不是一劳永逸的过程。代码库演化，团队标准变化，分析技能应该随之调整。
