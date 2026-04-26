---
type: design
status: implemented
implemented_at: 2026-04-25
implementation_branch: feat/25-agent-config-implementation
tags:
  - design/agent-config
  - architecture/context-injection
  - reference/prompt-engineering
  - worker/claude-code
  - worker/opencode-server
related:
  - Claude-Code-Context-Analysis.md
  - OpenCode-Server-Context-Analysis.md
---

# HotPlex Agent Context 设定文件方案 (Dual-Worker)

> **实施状态**: ✅ Phase 1-4 已实现 (`feat/25-agent-config-implementation`)
> 实现包: `internal/agentconfig/` — `AgentConfig{Enabled, ConfigDir}`
> 大小限制: 8K/文件、40K/总计。平台变体采用追加模式。
> Phase 5（动态能力）待后续版本实现。
> CC 子 Agent 裁剪受上游限制 (§5.5.1)。两个 Worker 统一使用 system-level 注入 (§4, §5)。

> 基于 [[Claude-Code-Context-Analysis]] + [[OpenCode-Server-Context-Analysis]] 双研究报告 + OpenClaw SOUL.md/AGENTS.md/USER.md 体系分析，
> 设计 HotPlex 如何通过统一设定文件同时控制 Claude Code 和 OpenCode Server 的行为框架。

### 术语对照

| 缩写 | 全称 | 说明 |
|------|------|------|
| **CC** | Claude Code | Anthropic CLI 编码 Agent |
| **OCS** | OpenCode Server | OpenCode 的 HTTP Server 模式 |
| **B 通道** | B Channel (System-level) | 行为指令 "必须遵循"，高优先级，无削弱 |
| **C 通道** | C Channel (Context-level) | 上下文数据 "参考信息"，辅助性 |
| **hedging** | 削弱声明 | Claude Code M0 中 "this context may or may not be relevant" |
| **系统 Agent** | 指 Claude Code / OpenCode Server 内部的工作 Agent（explore、general 等） | 非 HotPlex |

---

## 1. OpenClaw 设定文件体系研究

### 1.1 架构总览

OpenClaw 将 Agent 的 "人格 + 行为 + 记忆" 拆解为独立的 Markdown 文件，存放在 workspace 目录（默认 `~/.openclaw/workspace/`）中：

```
~/.openclaw/workspace/
├── AGENTS.md       ← 工作空间规则、行为红线、记忆策略
├── SOUL.md         ← 人格、语气、价值观、风格
├── IDENTITY.md     ← 名字、头像、自我认知
├── USER.md         ← 用户画像、偏好、时区
├── TOOLS.md        ← 本地工具配置笔记
├── BOOTSTRAP.md    ← 首次运行仪式 (完成后自动删除)
├── HEARTBEAT.md    ← 定时任务指令 (放在 cache boundary 下方)
└── MEMORY.md       ← 长期记忆 (仅主会话加载)
```

### 1.2 注入位置与优先级

OpenClaw 将这些文件注入到 **system prompt** 中（而非 messages），作为一个名为 `# Project Context` 的 section：

```
OpenClaw System Prompt 结构:

  [Tooling Section]          ← 硬编码工具说明
  [Safety Section]           ← 硬编码安全规范
  [Skills Section]           ← 技能目录
  [Memory Section]           ← 记忆检索指南

  # Project Context          ← 设定文件注入点 (cache boundary 上方)
  The following project context files have been loaded:
  If SOUL.md is present, embody its persona and tone.

  ## AGENTS.md              ← priority 10
  [内容...]

  ## SOUL.md                ← priority 20
  [内容...]

  ## IDENTITY.md            ← priority 30
  [内容...]

  ## USER.md                ← priority 40
  [内容...]

  ## TOOLS.md               ← priority 50
  [内容...]

  ## MEMORY.md              ← priority 70
  [内容...]

  ─── CACHE BOUNDARY ───    ← 缓存分界线

  # Dynamic Project Context  ← 动态内容 (cache boundary 下方)
  ## HEARTBEAT.md
  [内容...]

  ## Runtime
  Model: ..., OS: ..., Shell: ...
```

### 1.3 关键设计特征

| 特征 | OpenClaw 做法 | 效果 |
|------|--------------|------|
| **注入位置** | system prompt 内，`# Project Context` section | 高优先级，直接塑造模型行为 |
| **SOUL.md 特殊处理** | 额外注入 "embody its persona and tone" 指令 | 人格指令得到强化 |
| **缓存分界线** | 静态文件在 boundary 上方，HEARTBEAT.md 在下方 | 利用 Anthropic Prompt Caching 的前缀匹配机制：稳定前缀可缓存 (KV 复用)，动态内容放下方避免频繁失效 |
| **子 Agent 裁剪** | subagent 只加载 AGENTS.md + TOOLS.md + SOUL.md + IDENTITY.md + USER.md | 节省 token，避免子 agent 看到 MEMORY (**HotPlex 不可复用** — CC 子 Agent 继承全部 system prompt) |
| **文件大小限制** | 单文件 12K chars，总计 60K chars | 防止 context 爆炸 |
| **HotPlex 最佳实践** | **单文件 8K chars，总计 40K chars** | HotPlex 选择更严格的限制 — agent config 是行为框架而非知识库，精简即有效 |
| **MEMORY 隔离** | MEMORY.md 仅主会话加载，不在群聊/共享会话加载 | 防止隐私泄漏 |
| **排序机制** | `CONTEXT_FILE_ORDER` Map 定义数字优先级 | 确定性顺序，避免随机性 |
| **frontmatter 剥离** | YAML frontmatter 加载时 strip | 元数据不注入到 prompt |

### 1.4 与 Claude Code / OpenCode Server 的对比

| 维度 | OpenClaw | Claude Code | OpenCode Server |
|------|----------|-------------|-----------------|
| **注入目标** | system prompt 内<br>直接作为 system section<br>"embody its persona" **(强化)** | `messages[]` 头部<br>`<system-reminder>` 包裹<br>"may or may not be relevant" **(削弱)** | `system` field (S2)<br>拼接为 messages[role: "system"]<br>**无削弱** |
| **行为规范** | `AGENTS.md` (用户可编辑)<br>红线/权限/记忆策略都可定制 | 硬编码在 S2 static<br>用户只能在 messages 层<br>尝试覆盖 (被削弱) | Provider Prompt (S0) 硬编码<br>Agent prompt 可覆盖 S0<br>`system` field 可追加 (S2) |
| **文件粒度** | 按职责拆分<br>SOUL / AGENTS / USER / IDENTITY / TOOLS / MEMORY | `CLAUDE.md` (全合一)<br>一个文件承载所有内容 | `AGENTS.md` (项目级+全局级)<br>可选拆分 `.claude/agents/*.md` |

**关键差异**:
- OpenClaw 和 OpenCode Server 都通过 **system prompt 内注入**，无削弱声明
- Claude Code 通过 **messages 头部注入**，有 `<system-reminder>` 包裹和 hedging
- OpenClaw 是纯文件驱动的架构，而 CC 和 OCS 都支持 CLI/API 注入

> 两个 Worker 的 Context 架构详细对比见 [[§2 Worker Context 架构对比]](#2-worker-context-架构对比)。

---

## 2. Worker Context 架构对比

> 基于 [[Claude-Code-Context-Analysis]] 与 [[OpenCode-Server-Context-Analysis]] 的源码研究，
> 对比两个 Worker 的 Context 注入架构，为统一通道映射提供基础。

### 2.1 通道语义定义

HotPlex 定义两个抽象通道，按注入位置和语义效果区分：

| 通道 | 语义 | 注入位置 | 特征 |
|------|------|---------|------|
| **B 通道** (System-level) | 行为指令 "必须遵循" | system prompt 内 | 高优先级，无削弱声明 |
| **C 通道** (Context-level) | 上下文数据 "参考信息" | messages 或等效位置 | 辅助信息，可能被削弱 |

B 通道承载指令性内容 (人格/规则/工具指南)，C 通道承载事实性内容 (用户画像/记忆)。
两个通道的具体实现机制因 Worker 而异，但语义分层保持一致。

### 2.2 Claude Code Context 槽位

Claude Code 调用 `anthropic.beta.messages.create()` 时，Context 分布在：

```
system[] (System Prompt — 5 段)
  S0  Attribution        (不可控)
  S1  CLI Prefix         (不可控, --append-system-prompt 自动切换为 SDK 模式)
  S2  Static Content     (不可控, ~15K tok)
  S3  Dynamic Content    (部分可控)
      ┃ ↓↓↓ B 通道注入点 (--append-system-prompt) ↓↓↓
  S4  System Context     (不可控)

messages[] (对话)
  M0  User Context       <system-reminder>
      ┃ ↓↓↓ C 通道注入点 (.claude/rules/hotplex-*.md) ↓↓↓
      附带削弱: "this context may or may not be relevant"
  M1+ Conversation History
```

B 通道机制: `--append-system-prompt` → 注入 S3 尾部，无削弱声明
C 通道机制: `.claude/rules/*.md` → 自动发现注入 M0，带 hedging 声明

#### 2.2.1 Claude Code 槽位特性表

| 槽位 | 说明 | 可控 | 削弱 | HotPlex 注入 |
|------|------|:----:|------|-------------|
| **S0** | 归属声明 (~50 tok) | ❌ | 无 | — |
| **S1** | CLI 身份声明；`--append-system-prompt` 时自动切换为 SDK 模式 (~100 tok) | ❌ | 无 | — |
| **S2** | 硬编码行为规范：安全/工具/输出格式/Git 操作 (~15K tok) | ❌ | 无 | — |
| **S3** | 动态内容：session guidance / env / language / MCP 指令 (~1-3K tok) | ⚠️ | 无 | **B 通道** — `--append-system-prompt` 追加尾部 |
| **S4** | 运行时状态：git status 等 (~200 tok) | ❌ | 无 | — |
| **M0** | 用户上下文：CLAUDE.md + `.claude/rules/*.md`，以 `<system-reminder>` 包裹 | ✅ | **hedged** — "may or may not be relevant" | **C 通道** — `.claude/rules/hotplex-*.md` |
| **M1+** | 对话轮次 + 工具调用结果 (随对话增长) | ✅ | 无 | — |

> **要点**: system[] 优先级高于 messages[]；B 通道 (S3) 无削弱，C 通道 (M0) 被 hedging 削弱；B 通道触发时 S1 自动从 "official CLI" 切换为 "Agent SDK"，模型更易接受外部规则。S2 虽不可控但包含安全基线兜底。
>
> **关于 Prompt Caching**: Anthropic API 的缓存是前缀匹配机制，层级为 `tools → system → messages`。CC 内部通过 `cache_control` 断点标记稳定前缀（如 S2 静态内容用较长 TTL）。多轮对话中，所有前序内容（包括 system 和已有 messages）都参与前缀匹配并命中缓存——不存在 "仅 system 被缓存" 或 "messages 无缓存" 的区分。缓存对 HotPlex 是透明的，不影响注入位置选择。

### 2.3 OpenCode Server Context 槽位

OpenCode Server 使用 Vercel AI SDK `streamText()` 调用 LLM，system prompt 被打包为
`messages[role: "system"]` (非 Anthropic API 原生 `system` 参数)。

```
messages[role: "system"] (System Messages — 两层组装)
  S0  Provider Prompt       (由 model ID 路由选择，~3-5K tok)
  S1  Agent Custom Prompt   (覆盖 S0，可选)
      ↑ 以上为 LLM 调用第一层 (llm.ts:99-111) 与 S2/S3 拼接为 system[0]

  S2  Call-level System     ← input.system (per-call 传入)
  S3  Last User System      ← input.user.system (从消息历史倒序查找 lastUser.system)
      ↑ S0+S2+S3 拼接为 system[0] (单个字符串块)

  S4  Environment Info      (每次调用生成，~150 tok)
  S5  Skills List           (可变，~500-3K tok)
  S6  Instruction Files     ← AGENTS.md 自动发现 (从 workdir 向上查找)
      ┃ ↓↓↓ C 通道注入点 (AGENTS.md workdir 写入) ↓↓↓
  S7  Conditional Injections (Plan Mode / Structured Output / Plugin Hooks)
      ↑ 以上为 LLM 调用第二层 (prompt.ts:1473-1479): S4 + S5 + S6 追加到 system 数组
      ↑ 最终: system[0] + system[1..] = 完整 system prompt

messages[role: "user"/"assistant"] (对话)
  M1  User Message
  M2+ Conversation History + Tool Results
```

**两层组装架构** (详见 [[OpenCode-Server-Context-Analysis.md#4-system-prompt-槽位图]]):
- **第一层** (`llm.ts:99-111`): S0 (Provider/Agent) + S2 (call-level system) + S3 (last user system) → 拼接为单个字符串 → `system[0]`
- **第二层** (`prompt.ts:1473-1479`): S4 (Environment) + S5 (Skills) + S6 (Instructions) → 追加到 `system[]` 数组
- **缓存优化**: 若 `system[0]` 未变 → 合并为 `[header, rest]` 两段以利用 Provider 缓存断点

B 通道注入点: `POST /session/:id/message { system: "..." }` → 进入 S2 (Call-level System)
C 通道注入点: 同 B 合并; 文件方式写入 workdir AGENTS.md → 自动发现到 S6

#### 2.3.1 OpenCode Server 槽位特性表

**第一层 — `system[0]`** (llm.ts:99-111, S0+S2+S3 拼接为单个字符串块):

| 槽位 | 说明 | 可控 | 削弱 | HotPlex 注入 |
|------|------|:----:|------|-------------|
| **S0** | Provider 模板：由模型 ID 自动选择 (anthropic.txt / gpt.txt 等, ~3-5K tok) | ❌ | 无 | — |
| **S1** | Agent 专用 prompt：内置 Agent (explore/compaction) 定义，存在时覆盖 S0 (~500-2K tok) | ⚠️ | 无 | — |
| **S2** | Call-level System：每次 API 调用传入的 `system` 字段，与 S0/S3 拼接为 `system[0]` | ✅ | 无 | **B + C 通道** — `system` field 合并注入 |
| **S3** | Last User System：从消息历史倒序查找最后一个 user 消息的 system 字段 (llm.ts `input.user.system`)。**不是独立持久化机制** — 只是 `lastUser` 对象的字段读取。在同一 prompt cycle (tool 迭代) 内 `lastUser` 不变所以持续生效，但跨消息时 `lastUser` 切换为新消息，旧 system 丢失 | 自动 (单 cycle) | 无 | S2 注入后在该 cycle 内有效 |

**第二层 — `system[1..]`** (prompt.ts:1473-1479, 追加到 system 数组):

| 槽位 | 说明 | 可控 | 削弱 | HotPlex 注入 |
|------|------|:----:|------|-------------|
| **S4** | 环境信息：工作目录/平台/日期 (~150 tok, 每次生成) | ❌ | 无 | — |
| **S5** | Skills 目录：当前 Agent 可用技能 (由 Agent 权限过滤, ~500-3K tok) | ⚠️ | 无 | — |
| **S6** | Instruction Files：从 workdir 向上查找 AGENTS.md 自动发现 | ✅ | 无 | **C 通道备选** — workdir AGENTS.md |
| **S7** | 条件注入：Plan Mode / JSON Schema / Plugin Hooks (~100-1K tok) | ❌ | 无 | — |

**对话层 — `messages[role: "user"/"assistant"]`**:

| 槽位 | 说明 | 可控 | 削弱 | HotPlex 注入 |
|------|------|:----:|------|-------------|
| **M1** | 用户消息；第 2 轮起被 `<system-reminder>` 标签包裹 | ✅ | 第 2 轮起 hedged | — |
| **M2+** | 对话历史 + 工具结果，支持 Compaction (摘要+截断双策略) | ✅ | 无 | — |

> **要点**: 两层组装 — 第一层 (S0+S2+S3) 拼接为 `system[0]`，第二层 (S4+S5+S6+S7) 追加为 `system[1..]`。S3 的 "sticky" 效果仅限于同一 prompt cycle (tool 迭代间 `lastUser` 不变)：**跨消息时 `lastUser` 切换为新 user 消息，若新消息不带 `system` 则旧注入丢失**。因此 HotPlex 必须在每条消息都附带 `system` 字段。S2 全部内容无 hedging。S6 (项目 AGENTS.md) 与 S2 (HotPlex 注入) 分处不同 system 元素，天然隔离。

### 2.4 架构差异对照

| 维度 | Claude Code | OpenCode Server |
|------|-------------|-----------------|
| LLM 调用方式 | `anthropic` SDK 原生 | Vercel AI SDK 抽象层 |
| System Prompt 位置 | API `system[]` 参数 | `messages[role: "system"]` |
| B 通道实现 | CLI `--append-system-prompt` | HTTP API `system` 字段 |
| C 通道实现 (文件) | `.claude/rules/*.md` | AGENTS.md workdir 写入 |
| C 通道实现 (API) | N/A | HTTP API `system` 字段 (与 B 合并) |
| 削弱声明 | M0 有 hedging | S2 system field 无 hedging；用户消息第 2 轮起有 `<system-reminder>` 包裹 |
| Prompt Caching | Anthropic 前缀匹配 (tools→system→messages) | Provider 自管理 |
| Provider 支持 | Anthropic only | 20+ (AI SDK) |
| 文件自动发现 | `.claude/rules/` | `AGENTS.md` (cwd 向上查找) |
| 实施状态 | ✅ B+C 统一注入 (`--append-system-prompt`) | ✅ B+C 统一注入 (`system` field) |

---

## 3. 统一通道映射

### 3.1 统一映射表

> 每个 Config 文件 → 通道 → Worker 具体机制 → 槽位 → 强度

| Config 文件 | 通道 | CC 机制 | CC 槽位 | CC 强度 | OCS 机制 | OCS 槽位 | OCS 强度 |
|------------|------|---------|---------|---------|----------|---------|----------|
| **SOUL.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **AGENTS.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **SKILLS.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **USER.md** | C | `--append-system-prompt` (与 B 合并) | S3 尾 | 强 (无 hedging) | `system` field (S2 合并) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **MEMORY.md** | C | `--append-system-prompt` (与 B 合并) | S3 尾 | 强 (无 hedging) | `system` field (S2 合并) | S0+S2+S3 拼接 | 强 (无 hedging) |

### 3.1.1 B/C 通道为何合并在同一注入机制？

读者可能会疑惑：**如果 B 和 C 都合并到同一个注入机制 (CC 的 `--append-system-prompt` / OCS 的 `system` 字段)，那为什么还要区分 B 和 C？**

原因有三：

**① 设计统一性**: HotPlex 同时服务于 Claude Code 和 OpenCode Server。B/C 的分类是从语义效果出发的 (行为指令 vs 参考数据)，在两个 Worker 上体现为同一注入机制内的不同 section 标签。分类是逻辑的，不是机制的。

**② 内容标签依然保留**: 即使所有内容合并到同一个注入机制，内容的 section 标签仍然区分了 B 和 C 的语义：
```
<directives>                          ← B 通道: 行为约束
  <persona>  (B — SOUL.md)            ← "我是谁"
  <rules>    (B — AGENTS.md)          ← "我怎么做"
  <skills>   (B — SKILLS.md)          ← "我用什么"

<context>                             ← C 通道: 参考信息
  <user>     (C — USER.md)            ← "用户偏好"
  <memory>   (C — MEMORY.md)          ← "历史记忆"
```
XML 嵌套结构自然传达 B/C 优先级：`<directives>` 为行为约束，`<context>` 为参考补充。

**③ 统一注入消除了 CC 的 hedging**: 早期设计中 CC 的 C 通道通过 `.claude/rules/` 注入 M0，被 hedging 削弱 ("可能不相关")。统一注入后，USER.md 和 MEMORY.md 通过 S3 到达，获得与 B 通道同等的权重 — 所有内容无削弱。

**④ OCS 两层组装中的位置优势**: HotPlex 注入的 S2 (Call-level System) 与 S0 (Provider Prompt) 拼接到 `system[0]`，而项目级 Instruction Files (S6) 在 `system[1..]` 中 — 这意味着 HotPlex 注入的行为框架在系统 prompt 中拥有更高优先级。

### 3.2 分配原则：按注入位置效果决定归属

```
核心判断标准 (按优先级排序):

  ① 是否需要覆盖 Worker 预设默认值？
     Claude Code S2: 无注释 / 简短输出 / 专用工具优先
     OpenCode S0: 无注释 / 简洁输出 / 专用工具优先
     → 需覆盖的内容必须进 B 通道

  ② 被削弱后果的严重程度？(主要影响 Claude Code)
     Claude Code C 通道有 hedging → 评估被削弱后果
     OpenCode Server 无 hedging → C 通道内容不被削弱

  ③ 内容性质：行为指令 vs 上下文数据？
     "你必须怎样" → B (指令性)
     "这是相关信息" → C (事实性)
```

### 3.3 逐内容评估

| 内容 | 需覆盖预设? | CC 被削弱后果 | OCS 影响 | 性质 | 结论 |
|------|-----------|-------------|---------|------|------|
| **SOUL.md** 人格 | ✅ 覆盖 S1/S0 身份声明 | 🔴 人格丧失 | 不适用 (B 通道无削弱) | 行为指令 | **→ B** |
| **AGENTS.md** 工作规则 | ✅ 覆盖 S2/S0 默认行为 | 🔴 规则失效 | 不适用 | 行为指令 | **→ B** |
| **SKILLS.md** 工具指南 | ✅ 覆盖 "专用工具优先" | 🟡 平台行为次优 | 不适用 | 行为指引 | **→ B** |
| **USER.md** 用户画像 | ❌ 纯增量信息 | 🟢 回复风格稍偏 | S2 中无削弱 (反而更强) | 上下文数据 | **→ C** |
| **MEMORY.md** 持久记忆 | ❌ 纯增量信息 | 🟢 遗忘偏好 | S2 中无削弱 (反而更强) | 上下文数据 | **→ C** |

### 3.4 最终分配方案

```
┌──────────────────────────────────────────────────────────────────────────┐
│ B 通道 (System-level injection) — "必须遵循的行为框架"                  │
│                                                                         │
│  ┌─ Claude Code: --append-system-prompt → S3 尾部 (无 hedging) ──────┐ │
│  │  <persona>   ← SOUL.md (~500 tok)                          │ │
│  │  <rules> ← AGENTS.md (~2K tok)                         │ │
│  │  <skills>      ← SKILLS.md (~1K tok)                        │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌─ OpenCode Server: system field → S2 (无 hedging, 拼接进 system[0]) ─┐ │
│  │  <persona>   ← SOUL.md                                     │ │
│  │  <rules> ← AGENTS.md                                   │ │
│  │  <skills>      ← SKILLS.md                                   │ │
│  └────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ C 通道 (Context-level injection) — "参考上下文"                        │
│                                                                         │
│  ┌─ Claude Code: --append-system-prompt → S3 尾部 (无 hedging) ──────┐ │
│  │  <user>      ← USER.md   (与 B 通道合并注入)             │ │
│  │  <memory> ← MEMORY.md (与 B 通道合并注入)             │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌─ OpenCode Server: system field → S2 (无 hedging, 拼接进 system[0]) ─┐ │
│  │  <user>      ← USER.md   (与 B 通道合并注入)             │ │
│  │  <memory> ← MEMORY.md (与 B 通道合并注入)             │ │
│  └────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

### 3.5 语义分层总览

```
Claude Code 注入位置效果 (从强到弱):

  S2 静态硬编码        "Be careful with security"           基线安全网
  S3 B-行为框架        "MUST follow these rules"  ← HotPlex  项目强制规则
  S3 B-人格            "embody its persona"       ← HotPlex  Agent 身份
  ─── 注意力分界 ───
  S3 C-用户画像        "User prefers concise replies"       ← HotPlex  参考信息
  S3 C-记忆            "Last time we decided X"             ← HotPlex  参考信息

OpenCode Server 注入位置效果 (无削弱，两层组装):

  S0 Provider Prompt   "You are OpenCode..."                 产品身份
  S2 HotPlex 注入       "MUST follow these rules"  ← B+C 合并, 每条消息带 system
  S3 Last User System    (同 cycle 内 lastUser 不变)
  ───────────────────────────────────────────────────────────
  S4 Environment         Working dir / Platform
  S5 Skills              可用 Skill 目录
  S6 项目 AGENTS.md      "Project knowledge..."             项目知识
```

---

## 4. Claude Code 通道实现

Claude Code 通过 `--append-system-prompt` CLI 参数将 B + C 通道统一注入 S3 尾部。
该参数在 S2 硬编码规范之后追加，语义为 "开发者指令"，**无削弱声明**。

当使用 `--append-system-prompt` 时，Claude Code 自动将 S1 前缀切换为 SDK 模式
(`src/constants/system.ts:39-43`):

```
默认:      "You are Claude Code, Anthropic's official CLI for Claude."
+ append:  "You are Claude Code, ..., running within the Claude Agent SDK."

模型已知自己运行在 SDK 环境中 → 更容易接受外部注入的人格和规则。
```

Prompt 组装使用两层嵌套 XML 标签（遵循 Anthropic 提示工程最佳实践），
通过 `<directives>` / `<context>` 分组传达 B/C 优先级区分：

```xml
<agent-configuration>

<directives>
Core behavioral parameters — follow unless overridden by explicit user instructions.

<persona>
Embody this persona naturally in all interactions.
[SOUL.md]
</persona>

<rules>
Treat as mandatory workspace constraints.
[AGENTS.md]
</rules>

<skills>
Apply these capabilities when relevant.
[SKILLS.md]
</skills>

</directives>

<context>
Reference material to inform your responses.

<user>
Tailor responses to this user's preferences and expertise.
[USER.md]
</user>

<memory>
Recall relevant past context when applicable.
[MEMORY.md]
</memory>

</context>

</agent-configuration>
```

设计要点：
- **两层嵌套**: `<directives>` (B 通道行为约束) 与 `<context>` (C 通道参考信息) 传达优先级
- **每段附行为指令**: 1 行 prose 告诉模型如何处理该 section
- **根标签语义**: `agent-configuration` 传达 "配置参数" 而非 "辅助上下文"
- 空组省略 — 仅有 B 内容时不输出 `<context>` wrapper，反之亦然

组装逻辑见 `internal/agentconfig.BuildSystemPrompt()`。

---

## 5. OpenCode Server 通道实现

### 5.1 API-Based Context 注入 (B + C → S2, 拼接进 system[0])

OpenCode Server 的核心上下文注入点是 `POST /session/:id/message` 端点的 `system` 字段。
该端点是 OpenCode 的完整上下文组装入口 (对应 `SessionPrompt.Service.prompt()`)：

```
POST /session/:id/message
{
  "parts": [{"type": "text", "text": "user message"}],
  "system": "# Agent Persona\n[SOUL.md]\n\n# Workspace Rules\n[AGENTS.md]\n..."
}
```

`system` 字段内容被注入到 S2 槽位 (Call-level System)，
作为 `messages[role: "system"]` 送达 LLM，**无削弱声明**。

**System 字段行为** (源码核实 `~/opencode` llm.ts + prompt.ts):
OCS 的 `system` 字段通过两层路径进入 LLM system prompt：
- **S2 (Call-level)**: API 请求中的 `system` 字段 → 直接进入 `input.system` 数组
- **S3 (Last User)**: 存储在 `MessageV2.User.system` 中，prompt 循环从消息历史倒序查找最后一个 user 消息 (`lastUser`)，读取其 `system` 字段

关键行为：`lastUser` 在同一 prompt cycle (tool 调用迭代) 内不变 → system 持续生效。
但 **跨消息时 `lastUser` 切换为新的 user 消息**，若新消息不带 `system` 则旧注入丢失。
**结论：HotPlex 必须在每条消息都附带 `system` 字段，不存在"发送一次即持久"的机制。**

**迁移说明**: 当前 OpenCode Server Worker 使用 `POST /sessions/{id}/input` 端点
(该端点在 OCS 源码中不存在，需迁移)。迁移路径：
1. Worker `conn.Send()` 切换到 `POST /session/:id/message` 端点
2. **每条消息都附带 `system` 字段** — 不存在自动继承机制
3. 若需更新内容，发送新的 `system` 值即可覆盖
4. 不使用空消息注入 — 系统提示附加在真实的用户消息上，避免产生 phantom turn

> ⚠️ **Compaction 风险**: 当会话触发 Compaction 时，被压缩的用户消息可能被移除。
> 若携带 `system` 字段的用户消息被压缩，该消息从历史中消失，`lastUser` 指向更早的不含 system 的消息。
> 由于 HotPlex 每条消息都带 `system`，Compaction 只影响历史回溯，不影响当前轮次。

### 5.2 B + C 合并注入实现

```go
// OCS 使用与 CC 相同的 BuildSystemPrompt() 组装逻辑。
// 区别仅在注入方式: OCS 通过 HTTP API system 字段而非 CLI 参数。
//
// 重要：OCS system 字段无跨消息持久性 — lastUser 在新消息到来时切换。
// HotPlex 必须在每条消息都附带 system 字段，否则注入的 context 丢失。
```

### 5.3 AGENTS.md Workdir 共存方案 (C 通道备选)

OpenCode Server 自动从 workdir 加载 AGENTS.md 到 S6 槽位 (Instruction Files)。
HotPlex 可利用此机制作为 C 通道的备选注入路径。

```
方案: 将 USER.md + MEMORY.md 内容追加到 workdir/AGENTS.md

  workdir/AGENTS.md (项目原有)
  ├── 项目知识库
  ├── 目录结构
  ├── 编码约定
  └── ...

  ─── HotPlex Agent Context (以下由 HotPlex 注入) ───

  ## HotPlex User Profile         ← USER.md
  [用户画像/偏好/时区/...]

  ## HotPlex Persistent Memory    ← MEMORY.md
  [跨会话记忆/反馈纠正/...]
```

**冲突缓解策略**:
- 使用 `HotPlex` 前缀的 section header 标识 HotPlex 注入内容
- Worker 启动时追加，停止时清理 (仅移除 HotPlex 标记的 section)
- 检测现有 AGENTS.md 是否包含 HotPlex section，避免重复追加
- 如果 AGENTS.md 不存在，创建仅包含 HotPlex section 的文件

**⚠️ 语义混淆风险**: S4 将 HotPlex 用户画像/记忆与项目指令混合在同一个 slot 中，
LLM 可能无法区分 "项目规则" 和 "HotPlex 用户偏好" 的来源/权威性。
因此 API 注入 (S2) 是首选 — S2 与 S6 天然隔离，不存在语义混淆。

**API 注入 vs 文件注入选择**:

| 维度 | API 注入 (S2 system field) | 文件注入 (S6 AGENTS.md) |
|------|--------------------------|------------------------|
| 实现复杂度 | 低 (HTTP API 调用) | 中 (文件读写 + 解析) |
| 冲突风险 | 无 (不接触项目文件) | 有 (需管理 section) |
| 持久性 | per-message (需每次注入) | 进程生命周期 (写入后自动加载) |
| **当前选择** | ✅ 已采用 | 未采用 |

### 5.4 S0 Provider Prompt 与 SOUL.md 身份冲突

OpenCode Server 的 S0 (Provider Prompt) 根据模型 ID 自动选择模板 (anthropic.txt, gpt.txt 等)，
每个模板都包含身份声明 ("You are OpenCode / opencode")。
HotPlex 的 SOUL.md 注入到 S2 时也会声明身份 ("你是 HotPlex 团队的 AI 软件工程搭档")。

这构成了 **双重身份声明**: S0 说 "You are OpenCode"，S2 说 "You are HotPlex Agent"。

**应对策略**:
- S2 与 S0 拼接为 system[0]，模型根据 **recency bias** 倾向于遵循更近的内容
- SOUL.md 的 "embody its persona and tone" 指令强化了身份覆盖
- 实测验证: 不同 Provider 对身份冲突的处理可能不同 (Claude/GPT/Gemini 行为各异)
- 如果 S0 身份覆盖不可接受: 考虑 OCS Agent 定义中覆盖 `prompt` 字段 (完全替代 S0)

### 5.5 OCS Agent 系统与子 Agent 场景

OpenCode Server 内置 Agent 系统 (explore, general, compaction 等)，子 Agent 由 OCS 自行管理。
HotPlex 的 S2 system field 注入仅影响 **主 Agent 的对话轮次** — OCS 内部创建的子 Agent
(explore, general) **不会自动继承** S2 注入的内容。

**影响评估**:
- `explore` 子 Agent 有自己的专用 prompt (PROMPT_EXPLORE) — 不受 HotPlex SOUL.md 影响 ✅
- `general` 子 Agent 使用 Provider 默认 prompt — 不受 HotPlex S2 注入影响 ✅
- 子 Agent 的工具权限由 OCS Agent 定义的 `permission` 控制，与 HotPlex 无关 ✅
- 结论: OCS 子 Agent 场景裁剪 **由 OCS 自行管理**，HotPlex 无需干预

**已知限制**: HotPlex 的 MEMORY.md 和 USER.md 不会传播到 OCS 子 Agent。
子 Agent 在搜索/研究时无法参考用户的偏好和历史记忆。这是可接受的 — 子 Agent 执行的是
短期、特定任务的上下文搜索，不需要用户画像。

### 5.5.1 Claude Code 子 Agent 上下文继承 (upstream-blocked)

> **状态**: `upstream-blocked` — Claude Code 当前不支持按 Agent 类型限制规则作用域。

Claude Code 通过内置 `Agent` 工具派生子 Agent（如 Explore、Plan、General-purpose）执行并行子任务。
根据 [CC 官方文档](https://code.claude.com/docs/en/features-overview)：

> Subagents load a fresh, isolated context that includes the **system prompt**, full content
> of specified **skills**, **CLAUDE.md**, **git status**, and any context passed by the lead agent.
> Subagents **do not inherit** conversation history or invoked skills from the main session.

**子 Agent 上下文继承矩阵**:

| 内容 | 主 Agent | 子 Agent | 继承机制 |
|------|---------|---------|---------|
| System prompt (`--append-system-prompt`) | ✅ | ✅ | system prompt 全量继承 |
| CLAUDE.md | ✅ | ✅ | 项目级自动发现 |
| `.claude/rules/*.md` | ✅ | ✅ | 项目级自动发现，无条件加载 |
| Skills | ✅ | ✅（指定的） | 显式传递 |
| 对话历史 | ✅ | ❌ | 隔离 |
| 已调用的 Skills | ✅ | ❌ | 隔离 |
| Auto memory | ✅ | ❌ | 隔离 |

**裁剪不可行的原因**:

1. `.claude/rules/` 文件 frontmatter 仅支持 `paths`（文件 glob 限定），无 agent-scoped 字段
2. `--append-system-prompt` 属于 system prompt，子 Agent 全量继承
3. HotPlex 不控制 CC 子 Agent 的创建和上下文加载——那是 CC 内部调度行为
4. OpenClaw 能做裁剪是因为它自己控制子 Agent 创建，HotPlex 无此能力

**实际风险评估**: 低。子 Agent 在同一进程树内，结果回到主 Agent 摘要后展示，不对外暴露。
MEMORY.md 内容（用户偏好、项目上下文）对搜索/研究类子 Agent 无害，不会产生误导性行为。

**决策**: 接受 MEMORY.md 对 CC 子 Agent 可见。若未来 CC 上游支持 agent-scoped rules，
可重新评估实现裁剪。

### 5.6 对比: Claude Code vs OpenCode Server 通道实现

| 维度 | Claude Code | OpenCode Server |
|------|-------------|-----------------|
| B 通道机制 | `--append-system-prompt` CLI 参数 | `system` 字段 → S2 (Call-level System) |
| C 通道机制 | `--append-system-prompt` (与 B 合并) | `system` 字段 (与 B 合并, S2+S3 拼接) |
| B/C 是否分离 | 否 (统一注入 S3) | 否 (合并为 system[0] = S0+S2+S3) |
| 削弱 | 无 (B+C 均无 hedging) | 无 hedging |
| 清理 | 无需清理 (无文件写入) | 无需清理 (API 方式) |

---

## 6. 完整 Context 组装流程

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    HotPlex → Dual-Worker Context 注入流程                   │
└─────────────────────────────────────────────────────────────────────────────┘

  Step 1: 加载设定文件 (共享 — 两种 Worker 相同)
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  ~/.hotplex/agent-configs/                                              │
  │  ├── SOUL.md    → soulContent     (人格/语气/价值观)       → B        │
  │  ├── AGENTS.md  → agentsContent   (工作规则/红线/记忆策略) → B        │
  │  ├── SKILLS.md  → skillsContent   (工具使用指南)           → B        │
  │  ├── USER.md    → userContent     (用户画像/偏好/时区)     → C        │
  │  └── MEMORY.md  → memoryContent   (跨会话记忆)             → C        │
  │                                                                         │
  │  加载规则:                                                              │
  │  · 按平台选择变体: SOUL.slack.md > SOUL.md (优先平台特定版本)          │
  │  · 平台变体采用追加模式 (非替换)                                       │
  │  · frontmatter (YAML) 剥离后注入                                       │
  │  · 大小限制: 8K/文件, 40K/总计                                      │
  │  · 文件不存在 → 跳过 (不报错)                                          │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 2: Worker 路由决策 (Bridge 层)
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  bridge.injectAgentConfig:                                             │
  │                                                                         │
  │  configs := agentconfig.Load(configDir, platform)                      │
  │  prompt  := agentconfig.BuildSystemPrompt(configs)  // B + C 统一组装  │
  │                                                                         │
  │  // 按 Worker 类型设置注入方式                                         │
  │  sessionInfo.SystemPrompt = prompt                                     │
  │  w.Start(ctx, sessionInfo)                                              │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 3a: Claude Code Worker 组装
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  B + C 通道统一注入: --append-system-prompt → S3 尾部                  │
  │    SYSTEM_PROMPT = BuildSystemPrompt(SOUL + AGENTS + SKILLS             │
  │                                      + USER + MEMORY)                  │
  │                                                                         │
  │  CLI 调用:                                                              │
  │    claude \                                                             │
  │      --append-system-prompt "$SYSTEM_PROMPT" \                          │
  │      --permission-mode auto \                                           │
  │      --model claude-sonnet-4-6                                          │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 3b: OpenCode Server Worker 组装
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  1. 启动 opencode serve 进程 (无 system prompt CLI 参数)               │
  │     opencode serve --port 18789 --dangerously-skip-permissions         │
  │                                                                         │
  │  2. 创建 Session via POST /sessions → 获取 session_id                  │
  │                                                                         │
  │  3. 每条消息附带 B+C 通道 (system field → S2):                        │
  │     POST /session/:id/message {                                         │
  │       parts:  [{ type: "text", text: "user message" }],                 │
  │       system: BuildSystemPrompt(configs)     ← B + C 统一              │
  │     }                                                                   │
  │     ↑ 必须每条消息都附带 system 字段，否则注入上下文在后续轮次丢失    │
  └─────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Context 分布图

### 7.1 Claude Code Context 结构

```
HotPlex 启动的 Claude Code 完整 Context 结构:

  system[] (System Prompt)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ S0  Attribution                      (不可控)                            │
  │ S1  CLI Prefix (SDK模式)             (不可控, 自动切换)                  │
  │ S2  Static Content (~15K tok)        (不可控, 稳定前缀)                  │
  │     # System / # Doing Tasks / # Executing Actions / ...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S3  Dynamic Content                  (部分可控)                          │
  │     session_guidance / env_info / language / MCP instructions / ...     │
  │     ─────────────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex <agent-configuration> (--append-system-prompt) ↓↓↓    │
  │     ─────────────────────────────────────────────────────────────────── │
  │     <directives>                                                       │
  │     <persona>  ← SOUL.md (~500 tok)                                   │
  │     Embody this persona naturally.                                     │
  │     [人格/语气/价值观/红线...]                                         │
  │                                                                        │
  │     <rules>    ← AGENTS.md (~2K tok)                                   │
  │     Treat as mandatory workspace constraints.                           │
  │     [自主行为边界/反模式/工具偏好/...]                                  │
  │                                                                        │
  │     <skills>   ← SKILLS.md (~1K tok)                                   │
  │     Apply these capabilities when relevant.                             │
  │     [消息平台操作/STT/构建命令/...]                                     │
  │     </directives>                                                      │
  │                                                                        │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S4  System Context                   (不可控)                            │
  │     gitStatus 等运行时状态                                                │
  └──────────────────────────────────────────────────────────────────────────┘

  messages[] (对话)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ M0  User Context <system-reminder>                                      │
  │     ## ~/.claude/CLAUDE.md               ← 用户全局指令                │
  │     ## workdir/CLAUDE.md                 ← 项目 CLAUDE.md              │
  │     ## workdir/.claude/rules/*.md        ← 项目现有规则                │
  │     currentDate: 2026/04/23                                             │
  │     IMPORTANT: this context may or may not be relevant...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ M1  Deferred Tools                                                      │
  │ M2  Session Start                                                       │
  │ M3+ Conversation History                                                │
  └──────────────────────────────────────────────────────────────────────────┘
```

### 7.2 OpenCode Server Context 结构

```
HotPlex 启动的 OpenCode Server 完整 Context 结构:

  messages[role: "system"] (System Messages — 两层组装)
  ══════════════════════════════════════════

  ┌── 第一层 (llm.ts:99-111) → system[0] (S0 + S2 + S3 拼接) ────────┐
  │ S0  Provider Prompt                   (由 model 选择，不可控)     │
  │     "You are OpenCode / opencode..."                             │
  │     anthropic.txt / gpt.txt / gemini.txt / beast.txt / ...       │
  ├──────────────────────────────────────────────────────────────────┤
  │ S1  Agent Custom Prompt               (覆盖 S0，条件注入)         │
  │     内置: explore / compaction / title / summary                 │
  ├──────────────────────────────────────────────────────────────────┤
  │ S2  Call-level System                 (per-call 传入, 可控)      │
  │     ─────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex <agent-configuration> (system field) ↓↓↓              │
  │     ─────────────────────────────────────────────────────────────────── │
  │                                                                  │
  │     <directives>                                                 │
  │     <persona>  ← SOUL.md (~500 tok)                              │
  │     <rules>    ← AGENTS.md (~2K tok)                             │
  │     <skills>   ← SKILLS.md (~1K tok)                             │
  │     </directives>                                                │
  │     <context>                                                    │
  │     <user>     ← USER.md (C 通道)                                │
  │     <memory>   ← MEMORY.md (C 通道)                              │
  │     </context>                                                   │
  ├──────────────────────────────────────────────────────────────────┤
  │ S3  Last User System                  (从 lastUser.system 读取)   │
  │     同一 cycle 内持续生效; 跨消息 lastUser 切换,需每条带 system │
  └──────────────────────────────────────────────────────────────────┘

  ┌── 第二层 (prompt.ts:1473-1479) → system[1..] (追加) ───────────────┐
  │ S4  Environment Info                   (每次调用生成)            │
  │     Working directory / Platform / Date                          │
  ├──────────────────────────────────────────────────────────────────┤
  │ S5  Skills List                        (可变, 由 Agent 权限过滤) │
  │     Skill name / description / triggers                          │
  ├──────────────────────────────────────────────────────────────────┤
  │ S6  Instruction Files                  (自动发现, cwd 向上查找)  │
  │     ## workdir/AGENTS.md              ← 项目知识                │
  │     + CLAUDE.md (Claude Code 兼容, Flag 可禁用)                 │
  │     + CONTEXT.md (已废弃)                                         │
  │                                                                  │
  │     (备选: 也可写入 HotPlex Agent Context section, 见 §5.3)     │
  ├──────────────────────────────────────────────────────────────────┤
  │ S7  Conditional Injections                                      │
  │     JSON Schema output / Plugin Hooks                           │
  └──────────────────────────────────────────────────────────────────┘

  messages[role: "user"/"assistant"] (对话)
  ══════════════════════════════════════════
  ┌──────────────────────────────────────────────────────────────────┐
  │ M1  User Message                                                 │
  │     (第 2 轮起: 包装为 <system-reminder> 标签, 不影响 system[]) │
  │ M2+ Conversation History + Tool Results                          │
  │     (Compaction: 摘要生成 + Prune 截断 双策略, 见 §10)          │
  └──────────────────────────────────────────────────────────────────┘

与 Claude Code 的关键差异:
  1. 两层组装 — S0+S2+S3 拼接为 system[0], S4+S5+S6 追加为 system[1..]
  2. 无削弱声明 — system field (S2) 无 hedging, 所有内容等权
  3. Last User System — S2 通过 lastUser.system 在同一 cycle 内持续生效，跨消息需每条带 system
  4. 两个 Worker 均使用统一 system-level 注入 — 无文件写入、无 hedging、无清理
```

---

## 8. 设定文件模板

### 8.1 SOUL.md — Agent 人格 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex Agent 人格定义"
---

# SOUL.md - Agent 人格

## 身份

你是 HotPlex 团队的 AI 软件工程搭档，专注于 {{project_type}} 领域。

## 核心特质

- **主动思考**: 不只是执行指令，而是像资深同事一样提出假设和风险预警
- **技术敏感**: 关注 SOTA 技术，主动识别技术债务和安全风险
- **务实高效**: 语义理解优先，DRY & SOLID，异常路径全覆盖

## 沟通风格

- 语言: {{language}} 交流，技术术语保留英文
- 格式: Markdown 结构化，简洁直接
- 风格: 像资深同事协作，不是被动执行器
- 边界: 不确定时提出假设而非猜测

## 价值观

- 代码质量 > 开发速度 (但不过度工程化)
- 安全 > 便利 (OWASP Top 10 零容忍)
- 可观测性 > 静默运行
- 用户意图 > 字面指令 (理解 WHY)

## 红线

- 绝不泄露 API key、token、密码等敏感信息
- 绝不执行未经确认的 destructive 操作
- 绝不向外部服务发送未审查的敏感数据
- 遇到安全漏洞立即修复，不推迟
```

### 8.2 AGENTS.md — 工作空间规则 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex 工作空间行为规范"
---

# AGENTS.md - 工作规则

## 自主行为边界

**✅ 无需确认即可执行:**
- 读取/搜索/分析文件
- 运行测试/lint/构建
- Git commit/branch 操作
- 自动修复 lint 错误

**⚠️ 需要确认:**
- 首次方案设计
- 删除操作
- 依赖变更
- 远程推送 (git push)
- 外部服务调用

**❌ 绝对禁止:**
- 直接 push main/master 分支
- rm -rf 等破坏性操作
- 泄露敏感信息

## 记忆策略

- 用户明确要求 "记住" → 写入 MEMORY.md
- 修正错误行为 → 写入 MEMORY.md 反馈区
- 每次会话开始 → 隐式读取 MEMORY.md
- 用户说 "忘记" → 从 MEMORY.md 移除

## 工具使用偏好

| 任务 | 首选工具 |
|:-----|:---------|
| 探索代码库 | Task(Explore) |
| 查找文件 | Glob |
| 搜索内容 | Grep |
| 读取文件 | Read |
| 编辑文件 | Edit |

## 反模式

- ❌ `sync.Mutex` 嵌入或指针传递 — 显式 `mu` 字段
- ❌ `math/rand` 用于加密 — 使用 `crypto/rand`
- ❌ Shell 执行 — 仅允许 `claude` 二进制
- ❌ 跳过 WAL mode 的 SQLite
```

### 8.3 SKILLS.md — 工具使用指南 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex 工具使用指南"
---

# SKILLS.md - 工具使用指南

## 消息平台

### Slack
- 流式输出 → 150ms flush 间隔, 20-rune 阈值
- 长消息 → chunker 分割 → dedup 去重 → rate limiter → send
- 图片 → image block rendering + file upload
- 状态更新 → StatusManager threadState 管理

### 飞书
- 流式卡片 → 4 层防御: TTL → integrity → retry → IM Patch fallback
- 语音消息 → STT 转写 (SenseVoice-Small ONNX)
- 交互卡片 → InteractionManager 权限请求

## STT 语音转文字

- 引擎: SenseVoice-Small via funasr-onnx (ONNX FP32)
- 模式: PersistentSTT (长驻子进程, JSON-over-stdio)
- 配置: stt_provider / stt_local_cmd / stt_local_mode

## 构建/测试

```bash
make build          # 构建
make test           # 测试 (含 -race)
make lint           # golangci-lint
make check          # 完整 CI: fmt + vet + lint + test + build
```
```

### 8.4 USER.md — 用户画像 (→ C 通道)

```markdown
---
version: 1
description: "HotPlex 用户画像"
---

# USER.md - 用户画像

## 基本信息

- **称呼**: {{user_name}}
- **角色**: {{user_role}}
- **时区**: {{user_timezone}}
- **语言偏好**: {{language}}

## 技术背景

- **主要语言**: Go, TypeScript
- **框架经验**: React, Echo, Gin
- **基础设施**: Docker, Kubernetes, PostgreSQL

## 工作偏好

- 喜欢原子提交 + Conventional Commits
- 偏好代码审查式反馈 (指出问题 + 建议方案)
- 不喜欢过度解释基础概念
- 多任务时使用 TODO LIST 追踪

## 沟通偏好

- 简短直接，不要总结已完成的工作
- 使用 file:line 格式引用代码
- 技术决策需要说明 WHY
- 不确定时直接说 "需要调查"
```

---

## 9. 实施路径

### 9.1 阶段规划

```
Phase 1: 共享基础设施 ✅
├── agent-configs/ 目录的文件加载器 (通用，两种 Worker 共享)
├── frontmatter 解析与文件大小限制 (8K / file, 40K / total)
├── stripYAMLFrontmatter 通用工具函数
└── Load(dir, platform) → AgentConfigs 结构

Phase 2: Claude Code 集成 ✅
├── B+C 统一注入 via --append-system-prompt (BuildSystemPrompt)
├── XML 标签分隔各 section
└── 在 ClaudeCode Worker 启动流程中集成 (bridge 层 injectAgentConfig)

Phase 3: OpenCode Server 集成 ✅
├── 切换 Worker conn.Send 到 POST /session/{id}/message 端点
├── 每条消息附带 system 字段 (无跨消息持久性)
└── 在 OpenCode Server Worker 启动流程中集成

Phase 4: Bridge 集成与路由 ✅
├── bridge.injectAgentConfig 按 workerType 选择注入方式
├── SessionInfo.SystemPrompt 字段传播链
└── 端到端验证 (CC + OCS)

Phase 5: 动态能力 (待实现)
├── 按平台/通道动态选择配置变体
│   (SOUL.slack.md / SOUL.feishu.md / SOUL.cli.md)
│   (AGENTS.slack.md / AGENTS.feishu.md)
├── 运行时热更新 (文件变更 → 下次会话生效)
├── 用户画像自动学习 (从对话中提取偏好更新 USER.md)
└── MEMORY.md 自动管理 (类似 OpenClaw 的 daily log 压缩)
```

### 9.2 Worker 集成点

```
Claude Code Worker — internal/worker/claudecode/worker.go:

  Start() 方法:
  func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // session.SystemPrompt 已由 Bridge 层通过 injectAgentConfig 设置
    return w.startLocked(ctx, session)
  }

OpenCode Server Worker — internal/worker/opencodeserver/worker.go:

  Start() 方法:
  func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    if err := w.startServerProcess(ctx, session); err != nil {
      return err
    }
    sessionID, err := w.createSession(ctx, session.ProjectDir)
    // 保存 systemPrompt — 通过 conn.Send 附加在每条用户消息上
    w.systemPrompt = session.SystemPrompt
    // ...
  }

  conn.Send() — 每条消息附带 system 字段:
  func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
    inputData := extractInputData(msg)
    body := map[string]any{
      "parts": []map[string]any{{"type": "text", "text": inputData.Content}},
    }
    // system 字段无跨消息持久性 — 每条消息都必须附带
    if c.systemPrompt != "" {
      body["system"] = c.systemPrompt
    }
    url := fmt.Sprintf("%s/session/%s/message", c.httpAddr, c.sessionID)
    return c.httpPost(ctx, url, body)
  }

Bridge 层 — internal/gateway/bridge.go:

  injectAgentConfig:
  func (b *Bridge) injectAgentConfig(session *worker.SessionInfo, platform string) {
    configs := agentconfig.Load(b.cfg.AgentConfig.ConfigDir, platform)
    session.SystemPrompt = agentconfig.BuildSystemPrompt(configs)
  }
```

### 9.3 配置结构

```go
// internal/config/config.go

type AgentConfig struct {
    // Agent 配置文件目录 (默认: ~/.hotplex/agent-configs/)
    ConfigDir string `yaml:"config_dir" mapstructure:"config_dir"`

    // 启用开关 (默认: false)
    Enabled bool `yaml:"enabled" mapstructure:"enabled"`
}
```

### 9.4 按平台选择配置变体

```
~/.hotplex/agent-configs/
├── SOUL.md              ← 默认人格 (CLI / API 模式)
├── SOUL.slack.md        ← Slack 模式 (更简短, emoji 友好)
├── SOUL.feishu.md       ← 飞书模式 (正式, 企业场景)
├── AGENTS.md
├── AGENTS.slack.md      ← Slack 特定规则 (消息分割/去重/格式)
├── AGENTS.feishu.md     ← 飞书特定规则 (卡片/交互)
├── SKILLS.md
├── SKILLS.slack.md      ← Slack 工具指南
├── SKILLS.feishu.md     ← 飞书工具指南
├── USER.md
└── MEMORY.md

加载逻辑:
  func selectConfigFile(baseDir, baseName, platform string) string {
      if platform != "" {
          platformFile := filepath.Join(baseDir, baseName+"."+platform+".md")
          if fileExists(platformFile) {
              return platformFile
          }
      }
      return filepath.Join(baseDir, baseName+".md")
  }
```

---

## 10. 总结

**设计原则 (按优先级排序):**

1. **注入位置效果优先**
   B = 行为框架 (SOUL+AGENTS+SKILLS)，C = 上下文数据 (USER+MEMORY)
   两个 Worker 统一使用 system-level 注入，所有内容无 hedging 削弱

2. **语义分层**
   B 承载 "必须遵循" 的指令性内容，C 承载 "参考信息" 的事实性内容
   两层嵌套 XML 标签 (`<directives>` / `<context>`) 传达 B/C 优先级
   每段附带 1 行行为指令，引导模型正确处理各 section

3. **职责分离** (借鉴 OpenClaw)
   SOUL (人格) / AGENTS (规则) / SKILLS (工具) / USER (用户) / MEMORY (记忆)
   每个文件只承载一个关注点，便于独立维护和按平台选择变体

4. **非侵入式注入**
   Claude Code: `--append-system-prompt` 不修改项目文件，不写入 `.claude/rules/`
   OpenCode Server: API 方式 (`system` 字段) 不接触项目文件

5. **保留 Worker 默认能力**
   Claude Code: `--append-system-prompt` 而非 `--system-prompt`，S2 安全规范作为基线保留
   OpenCode Server: S0 Provider Prompt 作为基线保留，B+C 内容通过 S2 (Call-level System) 注入

6. **平台适配**
   `SOUL.slack.md` / `SOUL.feishu.md` 按平台选择人格变体，采用追加模式 (非替换)
   AGENTS/SKILLS 同理，不同平台不同规则和工具指南

7. **安全边界**
   文件大小限制 (8K / file, 40K / total)；frontmatter 剥离 (元数据不注入 prompt)
   CC 子 Agent 继承全部 system prompt (upstream-blocked，见 §5.5.1)；OCS 子 Agent 天然隔离

8. **Worker 路由**
   Bridge 层 `injectAgentConfig` 统一调用 `BuildSystemPrompt`，按 Worker 类型适配注入方式：
   Claude Code → CLI `--append-system-prompt`；OpenCode Server → HTTP API `system` 字段

9. **OCS 消息级注入**
   S2 (Call-level System) 无 hedging，所有内容以同等权重送达
   ✅ S2 在同一 prompt cycle 内通过 lastUser.system 持续生效
   ⚠️ 跨消息 lastUser 切换 — 每条消息必须附带 system 字段
