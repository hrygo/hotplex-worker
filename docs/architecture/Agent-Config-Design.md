---
type: design
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
| **缓存分界线** | 静态文件在 boundary 上方，HEARTBEAT.md 在下方 | 稳定内容可缓存，动态内容不破坏缓存 |
| **子 Agent 裁剪** | subagent 只加载 AGENTS.md + TOOLS.md + SOUL.md + IDENTITY.md + USER.md | 节省 token，避免子 agent 看到 MEMORY |
| **文件大小限制** | 单文件 4K chars，总计 20K chars | 防止 context 爆炸 |
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
  S2  Static Content     (不可控, ~15K tok, global cache)
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
| **S2** | 硬编码行为规范：安全/工具/输出格式/Git 操作 (~15K tok, global cache) | ❌ | 无 | — |
| **S3** | 动态内容：session guidance / env / language / MCP 指令 (~1-3K tok, ephemeral cache) | ⚠️ | 无 | **B 通道** — `--append-system-prompt` 追加尾部 |
| **S4** | 运行时状态：git status / cache breaker (~200 tok, 无缓存) | ❌ | 无 | — |
| **M0** | 用户上下文：CLAUDE.md + `.claude/rules/*.md`，以 `<system-reminder>` 包裹 | ✅ | **hedged** — "may or may not be relevant" | **C 通道** — `.claude/rules/hotplex-*.md` |
| **M1+** | 对话轮次 + 工具调用结果 (随对话增长) | ✅ | 无 | — |

> **要点**: system[] 优先级高于 messages[]；B 通道 (S3) 无削弱，C 通道 (M0) 被 hedging 削弱；B 通道触发时 S1 自动从 "official CLI" 切换为 "Agent SDK"，模型更易接受外部规则。S2 虽不可控但包含安全基线兜底。

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
| 缓存策略 | global/ephemeral 分级缓存 | Provider 自管理 |
| Provider 支持 | Anthropic only | 20+ (AI SDK) |
| 文件自动发现 | `.claude/rules/` | `AGENTS.md` (cwd 向上查找) |
| 当前 Worker 状态 | B/C 通道均未实现 (CLI args ready) | B/C 通道均未实现 (需切换到 /message API) |

---

## 3. 统一通道映射

### 3.1 统一映射表

> 每个 Config 文件 → 通道 → Worker 具体机制 → 槽位 → 强度

| Config 文件 | 通道 | CC 机制 | CC 槽位 | CC 强度 | OCS 机制 | OCS 槽位 | OCS 强度 |
|------------|------|---------|---------|---------|----------|---------|----------|
| **SOUL.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **AGENTS.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **SKILLS.md** | B | `--append-system-prompt` | S3 尾 | 强 (无 hedging) | `system` field (S2) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **USER.md** | C | `.claude/rules/hotplex-user.md` | M0 | 弱 (hedged) | `system` field (S2 合并) | S0+S2+S3 拼接 | 强 (无 hedging) |
| **MEMORY.md** | C | `.claude/rules/hotplex-memory.md` | M0 | 弱 (hedged) | `system` field (S2 合并) | S0+S2+S3 拼接 | 强 (无 hedging) |

**OCS 补充 C 通道路径** (文件方式):

| Config 文件 | OCS 文件机制 | OCS 槽位 | 说明 |
|------------|-------------|---------|------|
| USER.md | 写入 workdir AGENTS.md (HotPlex section) | S6 | 与项目 AGENTS.md 共存 |
| MEMORY.md | 写入 workdir AGENTS.md (HotPlex section) | S6 | 同上 |

### 3.1.1 OCS 的 B 和 C 为何"合并在同一机制"？

读者可能会疑惑：**如果 B 和 C 在 OpenCode Server 中使用同一个 `system` 字段 (S2)、拼接进同一个 system[0] 块、且都没有削弱，那为什么还要区分 B 和 C？**

原因有三：

**① 设计统一性**: HotPlex 同时服务于 Claude Code 和 OpenCode Server。B/C 的分类是从语义效果出发的 (行为指令 vs 参考数据)，这种分类在 CC 上体现为不同的机制 (S3 vs M0)，在 OCS 上体现为同一 `system` 字段内的不同 section 标签。分类是逻辑的，不是机制的。

**② 内容标签依然保留**: 即使 OCS 将所有内容合并到同一个 `system` 字段 (S2)，内容的 section 标签仍然区分了 B 和 C 的语义：
```
# Agent Persona (B — SOUL.md)      ← "我是谁"
# Workspace Rules (B — AGENTS.md)   ← "我怎么做"
# Tool Usage Guide (B — SKILLS.md)  ← "我用什么"
# User Profile (C — USER.md)        ← "用户偏好"
# Persistent Memory (C — MEMORY.md) ← "历史记忆"
```
B 类内容在前 ("规则优先")，C 类内容在后 ("参考补充") — 模型仍按位置区分优先级。

**③ OCS 实际上 B/C 效果更强而非更弱**: 在 Claude Code 中，C 通道被 hedging 削弱 ("可能不相关")。在 OpenCode Server 中，C 通道的 USER.md 和 MEMORY.md 通过 `system` field (S2) 到达，获得与 B 通道同等的权重 — 这意味着 OCS 上的 Agent 会**更好地**遵循用户画像和记忆。

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
│  │  # Agent Persona                     ← SOUL.md (~500 tok)        │ │
│  │  # Workspace Rules                   ← AGENTS.md (~2K tok)       │ │
│  │  # Tool Usage Guide                  ← SKILLS.md (~1K tok)       │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌─ OpenCode Server: system field → S2 (无 hedging, 拼接进 system[0]) ─┐ │
│  │  # Agent Persona                     ← SOUL.md                   │ │
│  │  # Workspace Rules                   ← AGENTS.md                 │ │
│  │  # Tool Usage Guide                  ← SKILLS.md                 │ │
│  └────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ C 通道 (Context-level injection) — "参考上下文"                        │
│                                                                         │
│  ┌─ Claude Code: .claude/rules/hotplex-*.md → M0 (hedged) ─────────┐ │
│  │  hotplex-user.md    ← USER.md   (附 "may not be relevant")      │ │
│  │  hotplex-memory.md  ← MEMORY.md (附 "may not be relevant")      │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌─ OpenCode Server (主): system field → S2 (无 hedging, 进 system[0]) ──┐ │
│  │  # User Profile     ← USER.md   (与 B 通道合并注入)             │ │
│  │  # Persistent Memory ← MEMORY.md (与 B 通道合并注入)             │ │
│  └────────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌─ OpenCode Server (备选): workdir AGENTS.md → S4 自动发现 ───────┐ │
│  │  (见 §6.3 AGENTS.md workdir 共存方案)                            │ │
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
  M0 C-项目知识        "Here's the codebase map"             参考信息 (hedged)
  M0 C-用户画像        "User prefers concise replies"       参考信息 (hedged)
  M0 C-记忆            "Last time we decided X"             参考信息 (hedged)

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

## 4. Claude Code B 通道实现: `--append-system-prompt`

> 本节描述 Claude Code Worker 的 B 通道具体实现。OpenCode Server 的 B 通道见 §6。

Claude Code 通过 `--append-system-prompt` CLI 参数将 B 通道内容注入 S3 尾部。
该参数在 S2 硬编码规范之后追加，语义为 "开发者指令"，**无削弱声明**。

当使用 `--append-system-prompt` 时，Claude Code 自动将 S1 前缀切换为 SDK 模式
(`src/constants/system.ts:39-43`):

```
默认:      "You are Claude Code, Anthropic's official CLI for Claude."
+ append:  "You are Claude Code, ..., running within the Claude Agent SDK."

模型已知自己运行在 SDK 环境中 → 更容易接受外部注入的人格和规则。
```

B 通道内容组装逻辑:

```go
func BuildClaudeCodeBPrompt(configs *AgentConfigs) string {
    parts := []string{}

    if configs.Soul != "" {
        parts = append(parts, fmt.Sprintf(`# Agent Persona
If SOUL.md is present, embody its persona and tone.
Follow its guidance unless higher-priority instructions
override it. Avoid stiff, generic replies.

%s`, configs.Soul))
    }

    if configs.Agents != "" {
        parts = append(parts, "# Workspace Rules\n"+configs.Agents)
    }

    if configs.Skills != "" {
        parts = append(parts, "# Tool Usage Guide\n"+configs.Skills)
    }

    return strings.Join(parts, "\n\n")
}
```

---

## 5. Claude Code C 通道实现: `.claude/rules/` 注入

> 本节描述 Claude Code Worker 的 C 通道实现。OpenCode Server 的 C 通道见 §6。

### 5.1 Claude Code 的 .claude/rules/ 自动发现

源码确认 (`src/utils/claudemd.ts:910-918`):

```typescript
// Claude Code 自动扫描 workdir/.claude/rules/ 下所有 .md 文件
const rulesDir = join(dir, '.claude', 'rules')
result.push(
  ...(await processMdRules({
    rulesDir,
    type: 'Project',
    conditionalRule: false,  // 无 frontmatter = 无条件全局生效
  })),
)
```

- 无 frontmatter 的 `.md` 文件全局生效
- 不需要额外 CLI 参数或环境变量
- 不干扰现有 CLAUDE.md 和 rules 文件
- Claude Code 自动合并到 M0 User Context 的 `<system-reminder>` 中

### 5.2 C 通道注入流程

```
Step 1: Worker 启动前 — 写入 rules 文件

  ~/.hotplex/agent-configs/USER.md
    → strip frontmatter
    → workdir/.claude/rules/hotplex-user.md

  ~/.hotplex/agent-configs/MEMORY.md
    → strip frontmatter
    → workdir/.claude/rules/hotplex-memory.md


Step 2: 启动 Claude Code 子进程

  claude \
    --append-system-prompt "$(buildAppendPrompt)" \
    --permission-mode auto \
    --model claude-sonnet-4-6

  Claude Code 自动发现 .claude/rules/hotplex-*.md → 注入 M0


Step 3: Worker 会话结束 — 可选清理

  rm workdir/.claude/rules/hotplex-*.md
  (或保留复用，内容会话间通常不变)


M0 User Context 最终结构:

  # claudeMd
  ## ~/.claude/CLAUDE.md               ← 用户全局指令 (不动)
  ## workdir/CLAUDE.md                 ← 项目 CLAUDE.md (不动)
  ## workdir/.claude/rules/linting.md  ← 项目现有规则 (不动)
  ## workdir/.claude/rules/hotplex-user.md    ← USER.md
  ## workdir/.claude/rules/hotplex-memory.md  ← MEMORY.md
  # currentDate: 2026/04/23
  IMPORTANT: this context may or may not be relevant...
```

### 5.3 实现代码

```go
// internal/messaging/agent_config.go

const hotplexRulesPrefix = "hotplex-"

// InjectCRules writes C-channel content to workdir/.claude/rules/
func InjectCRules(workdir string, configs *AgentConfigs) error {
    rulesDir := filepath.Join(workdir, ".claude", "rules")
    if err := os.MkdirAll(rulesDir, 0755); err != nil {
        return fmt.Errorf("mkdir rules: %w", err)
    }

    files := map[string]string{
        "hotplex-user.md":   configs.User,
        "hotplex-memory.md": configs.Memory,
    }
    for name, content := range files {
        if content == "" {
            continue
        }
        content = stripYAMLFrontmatter(content)
        if err := os.WriteFile(filepath.Join(rulesDir, name), []byte(content), 0644); err != nil {
            return fmt.Errorf("write %s: %w", name, err)
        }
    }
    return nil
}

// CleanupCRules removes HotPlex rule files from workdir
func CleanupCRules(workdir string) error {
    rulesDir := filepath.Join(workdir, ".claude", "rules")
    matches, err := filepath.Glob(filepath.Join(rulesDir, hotplexRulesPrefix+"*.md"))
    if err != nil {
        return err
    }
    for _, f := range matches {
        if err := os.Remove(f); err != nil {
            log.Warn("failed to remove rule file", "path", f, "error", err)
        }
    }
    return nil
}
```

---

## 6. OpenCode Server 通道实现

### 6.1 API-Based Context 注入 (B + C → S2, 拼接进 system[0])

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

### 6.2 B + C 合并注入实现

```go
// BuildOCSSystemPrompt 构建 OpenCode Server system field (S2) 内容。
// B 通道和 C 通道内容合并到同一个 system 字段。
// 注入到 llm.ts 的 S2 (Call-level System), 与 S0 (Provider/Agent) 拼接为 system[0]。
//
// 重要：OCS system 字段无跨消息持久性 — lastUser 在新消息到来时切换。
// HotPlex 必须在每条消息都附带 system 字段，否则注入的 context 丢失。
func BuildOCSSystemPrompt(configs *AgentConfigs) string {
    parts := []string{}

    // ── B 通道: 行为框架 (System-level) ──
    if configs.Soul != "" {
        parts = append(parts, fmt.Sprintf(`# Agent Persona
If SOUL.md is present, embody its persona and tone.
Follow its guidance unless higher-priority instructions override it.
Avoid stiff, generic replies.

%s`, configs.Soul))
    }
    if configs.Agents != "" {
        parts = append(parts, "# Workspace Rules\n"+configs.Agents)
    }
    if configs.Skills != "" {
        parts = append(parts, "# Tool Usage Guide\n"+configs.Skills)
    }

    // ── C 通道: 参考上下文 (Context-level) ──
    // OpenCode 无 hedging — C 通道内容在 system[] 中与 B 通道同等权重
    if configs.User != "" {
        parts = append(parts, "# User Profile\n"+configs.User)
    }
    if configs.Memory != "" {
        parts = append(parts, "# Persistent Memory\n"+configs.Memory)
    }

    return strings.Join(parts, "\n\n")
}
```

### 6.3 AGENTS.md Workdir 共存方案 (C 通道备选)

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
| 推荐 | ✅ 首选方案 | 备选方案 |

### 6.4 S0 Provider Prompt 与 SOUL.md 身份冲突

OpenCode Server 的 S0 (Provider Prompt) 根据模型 ID 自动选择模板 (anthropic.txt, gpt.txt 等)，
每个模板都包含身份声明 ("You are OpenCode / opencode")。
HotPlex 的 SOUL.md 注入到 S2 时也会声明身份 ("你是 HotPlex 团队的 AI 软件工程搭档")。

这构成了 **双重身份声明**: S0 说 "You are OpenCode"，S2 说 "You are HotPlex Agent"。

**应对策略**:
- S2 与 S0 拼接为 system[0]，模型根据 **recency bias** 倾向于遵循更近的内容
- SOUL.md 的 "embody its persona and tone" 指令强化了身份覆盖
- SOUL.md 的 "embody its persona and tone" 指令强化了身份覆盖
- 实测验证: 不同 Provider 对身份冲突的处理可能不同 (Claude/GPT/Gemini 行为各异)
- 如果 S0 身份覆盖不可接受: 考虑 OCS Agent 定义中覆盖 `prompt` 字段 (完全替代 S0)

### 6.5 OCS Agent 系统与子 Agent 场景

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

### 6.6 对比: Claude Code vs OpenCode Server 通道实现

| 维度 | Claude Code | OpenCode Server |
|------|-------------|-----------------|
| B 通道机制 | `--append-system-prompt` CLI 参数 | `system` 字段 → S2 (Call-level System) |
| C 通道机制 | `.claude/rules/hotplex-*.md` 文件 | `system` 字段 (与 B 合并, S2+S3 拼接) |
| C 通道备选 | 无 | AGENTS.md workdir 写入 → S6 (Instructions) |
| B/C 是否分离 | 是 (不同机制，S3 尾 + M0) | 合并 (system[0] = S0+S2+S3) |
| 削弱 | C 通道有 hedging | 无 hedging |
| 清理 | 删除 hotplex-*.md 文件 | 无需清理 (API 方式) / 移除 AGENTS.md section |

---

## 7. 完整 Context 组装流程

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
  │  · frontmatter (YAML) 剥离后注入                                       │
  │  · 单文件上限 4K chars，总计上限 20K chars                             │
  │  · 文件不存在 → 跳过 (不报错)                                          │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 2: Worker 路由决策 (Bridge 层)
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  bridge.StartSession / StartPlatformSession:                           │
  │                                                                         │
  │  configs := loadAgentConfigs(configDir, platform)                      │
  │                                                                         │
  │  switch workerType {                                                    │
  │  case worker.TypeClaudeCode:                                            │
  │    sessionInfo.SystemPrompt = BuildClaudeCodeBPrompt(configs)    // B  │
  │    InjectClaudeCodeCRules(workdir, configs)                      // C  │
  │                                                                         │
  │  case worker.TypeOpenCodeSrv:                                           │
  │    sessionInfo.SystemPrompt = BuildOCSSystemPrompt(configs)     // B+C │
  │    // C 通道备选: InjectOCSAGENTSMD(workdir, configs)                │
  │  }                                                                      │
  │                                                                         │
  │  w.Start(ctx, sessionInfo)                                              │
  └─────────────────────────────────────────────────────────────────────────┘

  Step 3a: Claude Code Worker 组装
  ┌─────────────────────────────────────────────────────────────────────────┐
  │  B 通道: --append-system-prompt → S3 尾部                              │
  │    APPEND_PROMPT = BuildClaudeCodeBPrompt(SOUL + AGENTS + SKILLS)      │
  │                                                                         │
  │  C 通道: .claude/rules/ 文件写入 → M0                                  │
  │    InjectCRules(workdir, configs)                                       │
  │      → workdir/.claude/rules/hotplex-user.md   (USER.md)              │
  │      → workdir/.claude/rules/hotplex-memory.md (MEMORY.md)            │
  │                                                                         │
  │  CLI 调用:                                                              │
  │    claude \                                                             │
  │      --append-system-prompt "$APPEND_PROMPT" \                          │
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
  │  3. 每条消息附带 B+C 通道 (system field → S2):                  │
  │     POST /session/:id/message {                                         │
  │       parts:  [{ type: "text", text: "user message" }],                 │
  │       system: BuildOCSSystemPrompt(configs)   ← B + C 合并             │
  │     }                                                                   │
  │     ↑ 必须每条消息都附带 system 字段，否则注入上下文在后续轮次丢失    │
  │                                                                         │
  │  备选 C 通道: 写入 workdir AGENTS.md (见 §6.3)                        │
  └─────────────────────────────────────────────────────────────────────────┘
```

---

## 8. Context 分布图

### 8.1 Claude Code Context 结构

```
HotPlex 启动的 Claude Code 完整 Context 结构:

  system[] (System Prompt)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ S0  Attribution                      (不可控)                            │
  │ S1  CLI Prefix (SDK模式)             (不可控, 自动切换)                  │
  │ S2  Static Content (~15K tok)        (不可控, global cache)              │
  │     # System / # Doing Tasks / # Executing Actions / ...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S3  Dynamic Content                  (部分可控)                          │
  │     session_guidance / env_info / language / MCP instructions / ...     │
  │     ─────────────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex B 通道 (--append-system-prompt) ↓↓↓                    │
  │     ─────────────────────────────────────────────────────────────────── │
  │                                                                         │
  │     # Agent Persona                  ← SOUL.md (~500 tok)              │
  │     If SOUL.md is present, embody its persona and tone.                │
  │     [人格/语气/价值观/红线...]                                         │
  │                                                                         │
  │     # Workspace Rules                ← AGENTS.md (~2K tok)             │
  │     [自主行为边界/反模式/工具偏好/...]                                  │
  │                                                                         │
  │     # Tool Usage Guide               ← SKILLS.md (~1K tok)             │
  │     [消息平台操作/STT/构建命令/...]                                     │
  │                                                                         │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ S4  System Context                   (不可控)                            │
  │     gitStatus / cacheBreaker                                            │
  └──────────────────────────────────────────────────────────────────────────┘

  messages[] (对话)
  ══════════════════════════
  ┌──────────────────────────────────────────────────────────────────────────┐
  │ M0  User Context <system-reminder>                                      │
  │     ─────────────────────────────────────────────────────────────────── │
  │     ↓↓↓ HotPlex C 通道 (.claude/rules/hotplex-*.md) ↓↓↓                │
  │     ─────────────────────────────────────────────────────────────────── │
  │                                                                         │
  │     ## workdir/CLAUDE.md             ← 项目知识 (不动)                  │
  │     Overview / Structure / Code Map / Conventions / Commands            │
  │                                                                         │
  │     ## workdir/.claude/rules/        ← 项目现有规则 (不动)              │
  │                                                                         │
  │     ## workdir/.claude/rules/hotplex-user.md   ← USER.md               │
  │     [称呼/角色/时区/偏好/沟通风格/...]                                  │
  │                                                                         │
  │     ## workdir/.claude/rules/hotplex-memory.md ← MEMORY.md             │
  │     [跨会话记忆/反馈纠正/项目上下文/...]                                │
  │                                                                         │
  │     currentDate: 2026/04/23                                             │
  │     IMPORTANT: this context may or may not be relevant...               │
  ├──────────────────────────────────────────────────────────────────────────┤
  │ M1  Deferred Tools                                                      │
  │ M2  Session Start                                                       │
  │ M3+ Conversation History                                                │
  └──────────────────────────────────────────────────────────────────────────┘
```

### 8.2 OpenCode Server Context 结构

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
  │     ↓↓↓ HotPlex B+C 通道 (system field) ↓↓↓                     │
  │     ─────────────────────────────────────────────────────────── │
  │                                                                  │
  │     # Agent Persona                  ← SOUL.md (~500 tok)       │
  │     # Workspace Rules                ← AGENTS.md (~2K tok)      │
  │     # Tool Usage Guide               ← SKILLS.md (~1K tok)      │
  │     # User Profile                   ← USER.md (C 通道)         │
  │     # Persistent Memory              ← MEMORY.md (C 通道)       │
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
  │     (备选: 也可写入 HotPlex Agent Context section, 见 §6.3)     │
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
  │     (Compaction: 摘要生成 + Prune 截断 双策略, 见 §11)          │
  └──────────────────────────────────────────────────────────────────┘

与 Claude Code 的关键差异:
  1. 两层组装 — S0+S2+S3 拼接为 system[0], S4+S5+S6 追加为 system[1..]
  2. 无削弱声明 — system field (S2) 无 hedging, 所有内容等权
  3. Last User System — S2 通过 lastUser.system 在同一 cycle 内持续生效，跨消息需每条带 system
  4. S6 项目 AGENTS.md 与 HotPlex S2 注入共存 — 互不干扰
```

---

## 9. 设定文件模板

### 9.1 SOUL.md — Agent 人格 (→ B 通道)

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

### 9.2 AGENTS.md — 工作空间规则 (→ B 通道)

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

### 9.3 SKILLS.md — 工具使用指南 (→ B 通道)

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

### 9.4 USER.md — 用户画像 (→ C 通道)

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

## 10. 实施路径

### 10.1 阶段规划

```
Phase 1: 共享基础设施 (1-2 天)
├── 实现 agent-configs/ 目录的文件加载器 (通用，两种 Worker 共享)
├── 实现 frontmatter 解析与文件大小限制 (4K / file, 20K total)
├── 实现 stripYAMLFrontmatter 通用工具函数
└── 实现 loadAgentConfigs(dir, platform) → AgentConfigs 结构

Phase 2: Claude Code 集成 (1-2 天)
├── 实现 SOUL.md → --append-system-prompt 组装 (BuildClaudeCodeBPrompt)
├── 实现 AGENTS.md + SKILLS.md → append-system-prompt 追加
├── 实现 USER.md + MEMORY.md → .claude/rules/ 注入 (InjectCRules)
├── 在 ClaudeCode Worker 启动流程中集成 (bridge 层路由)
└── Worker 会话结束时清理 .claude/rules/hotplex-*.md

Phase 3: OpenCode Server 集成 (2-3 天)
├── 修正 createSession URL: POST /sessions → POST /session (单数前缀)
├── 修正 createSession 响应解析: session_id → id (Session.Info 字段)
├── 切换 Worker conn.Send 从 POST /sessions/{id}/input 到 POST /session/{id}/message
├── 实现 BuildOCSSystemPrompt (B + C 合并注入)
├── 每条消息附带 system 字段 (无跨消息持久性)
├── (可选) 实现 AGENTS.md workdir 共存方案 (C 通道备选)
└── 在 OpenCode Server Worker 启动流程中集成

Phase 4: Bridge 集成与路由 (1-2 天)
├── bridge.StartSession 按 workerType 路由到不同 Prompt Builder
├── 修复 SessionInfo.SystemPrompt 字段的传播链 (InitConfig → SessionInfo)
├── 子 Agent 场景裁剪 (仅加载 SOUL.md + AGENTS.md, 不加载 MEMORY)
└── 端到端验证 (CC + OCS 双通道)

Phase 5: 动态能力 (1 周)
├── 按平台/通道动态选择配置变体
│   (SOUL.slack.md / SOUL.feishu.md / SOUL.cli.md)
│   (AGENTS.slack.md / AGENTS.feishu.md)
├── 运行时热更新 (文件变更 → 下次会话生效)
├── 用户画像自动学习 (从对话中提取偏好更新 USER.md)
└── MEMORY.md 自动管理 (类似 OpenClaw 的 daily log 压缩)
```

### 10.2 Worker 集成点

```
Claude Code Worker — internal/worker/claudecode/worker.go:

  Start() 方法:

  func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // B 通道: session.SystemPrompt 已由 Bridge 层设置
    // C 通道: .claude/rules/ 已由 Bridge 层写入
    // (Worker 只负责使用，不负责加载)
    return w.startLocked(ctx, session)
  }

OpenCode Server Worker — internal/worker/opencodeserver/worker.go:

  Start() 方法:

  func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // 1. 启动 opencode serve 进程 (同现有)
    if err := w.startServerProcess(ctx, session); err != nil {
      return err
    }

    // 2. 创建 session
    // 注意: POST /session (非 /sessions), 返回 { id: "..." } (非 session_id)
    sessionID, err := w.createSession(ctx, session.ProjectDir)
    // ...

    // 3. 保存 systemPrompt — 不在此注入
    // system prompt 通过 conn.Send 附加在每条真实用户消息上 (见 conn 修改)
    w.systemPrompt = session.SystemPrompt
    // ...
  }

  conn.Send() 修改 — 每条消息附带 system 字段:

  func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
    // 提取用户消息内容 (同现有)
    inputData := extractInputData(msg)

    // 构建请求体 — 切换到 /session/:id/message 端点
    body := map[string]any{
      "parts": []map[string]any{{"type": "text", "text": inputData.Content}},
    }

    // ⚠️ 关键: system 字段无跨消息持久性
    // lastUser 在新消息到来时切换为新的 user 消息
    // 每条消息都必须附带 system 字段，否则注入 context 丢失
    if c.systemPrompt != "" {
      body["system"] = c.systemPrompt
    }

    // 发送到 /session/:id/message (注意: 单数 session)
    // POST /session/:id/message → body.system → S2 (Call-level System)
    // llm.ts: 与 S0 (Provider) + S3 (lastUser.system) 拼接为 system[0]
    // 跨消息 lastUser 切换, 每条消息必须带 system
    url := fmt.Sprintf("%s/session/%s/message", c.httpAddr, c.sessionID)
    return c.httpPost(ctx, url, body)
  }

  注意: URL 从 /sessions/{id}/input (复数) 切换到 /session/{id}/message (单数)
  请求体从 InputData{Content, Metadata} 重构为 PromptInput{parts[], system?}

Bridge 层 — internal/gateway/bridge.go:

  StartSession / StartPlatformSession:

  func (b *Bridge) StartSession(ctx context.Context, sessionID string, init InitConfig) error {
    // 加载 agent configs (共享逻辑)
    configs := loadAgentConfigs(b.cfg.AgentConfig.ConfigDir, platform)

    // 按 Worker 类型路由
    session := worker.SessionInfo{ /* ... */ }

    switch init.WorkerType {
    case worker.TypeClaudeCode:
      session.SystemPrompt = BuildClaudeCodeBPrompt(configs)    // B 通道
      if err := InjectCRules(workdir, configs); err != nil {         // C 通道
        log.Warn("inject C-rules failed", "error", err)
      }

    case worker.TypeOpenCodeSrv:
      session.SystemPrompt = BuildOCSSystemPrompt(configs)      // B + C 通道
    }

    return b.sm.Start(ctx, session)
  }
```

### 10.3 配置结构

```go
// internal/config/config.go 新增

type AgentConfig struct {
    // Agent 配置文件目录 (默认: ~/.hotplex/agent-configs/)
    ConfigDir string `yaml:"config_dir" mapstructure:"config_dir"`

    // 各文件路径 (覆盖默认路径)
    SoulPath    string `yaml:"soul_path"    mapstructure:"soul_path"`
    AgentsPath  string `yaml:"agents_path"  mapstructure:"agents_path"`
    UserPath    string `yaml:"user_path"    mapstructure:"user_path"`
    SkillsPath  string `yaml:"skills_path"  mapstructure:"skills_path"`
    MemoryPath  string `yaml:"memory_path"  mapstructure:"memory_path"`

    // 大小限制
    MaxFileChars  int `yaml:"max_file_chars"  mapstructure:"max_file_chars"`   // 默认 4000
    MaxTotalChars int `yaml:"max_total_chars" mapstructure:"max_total_chars"`  // 默认 20000

    // Claude Code C 通道清理策略
    CleanupRulesOnExit bool `yaml:"cleanup_rules_on_exit" mapstructure:"cleanup_rules_on_exit"` // 默认 false (保留复用)

    // OpenCode Server 特定配置
    OCSUseSystemField bool   `yaml:"ocs_use_system_field"` // 使用 system field 注入 S2 (默认 true)
    OCSUseAGENTSMD    bool   `yaml:"ocs_use_agents_md"`    // C 通道使用 AGENTS.md 文件注入 S6 (默认 false)
}
```

### 10.4 按平台选择配置变体

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

## 11. 总结

**设计原则 (按优先级排序):**

1. **注入位置效果优先**
   需覆盖 Worker 预设默认值的内容 → B (system-level, 无 hedging)；纯增量上下文内容 → C (context-level)
   B = 行为框架 (SOUL+AGENTS+SKILLS), C = 上下文数据 (USER+MEMORY)
   OpenCode Server 无 hedging — C 通道的 USER.md 和 MEMORY.md 通过 S2 到达，也获得同等权重

2. **语义分层**
   B 承载 "必须遵循" 的指令性内容 (system role)；C 承载 "参考信息" 的事实性内容
   两个 Worker 保持相同语义分层但不同实现机制：CC 分离注入 (S3 尾 + M0)，OCS 合并注入 (S2 进 system[0])

3. **职责分离** (借鉴 OpenClaw)
   SOUL (人格) / AGENTS (规则) / SKILLS (工具) / USER (用户) / MEMORY
   每个文件只承载一个关注点，便于独立维护和按平台选择变体

4. **非侵入式注入**
   Claude Code: `.claude/rules/hotplex-*.md` 不修改现有项目文件，`hotplex-*` 前缀确保精确清理
   OpenCode Server: API 方式 (`system` 字段) 不接触项目文件；文件方式仅追加标记 section

5. **保留 Worker 默认能力**
   Claude Code: `--append-system-prompt` 而非 `--system-prompt`，S2 安全规范作为基线保留
   OpenCode Server: S0 Provider Prompt 作为基线保留，B+C 内容通过 S2 (Call-level System) 注入，与 S0 拼接为 system[0]

6. **平台适配**
   `SOUL.slack.md` / `SOUL.feishu.md` 按平台选择人格变体
   AGENTS/SKILLS 同理，不同平台不同规则和工具指南

7. **安全边界**
   文件大小限制 (4K / file, 20K total)；frontmatter 剥离 (元数据不注入 prompt)
   MEMORY.md 仅主会话加载 (防止隐私泄漏)；子 Agent 场景裁剪 (仅加载 SOUL + AGENTS, 跳过 MEMORY)

8. **Worker 路由**
   Bridge 层根据 `worker.WorkerType` 选择注入机制 — 同一 Config 文件，不同交付路径
   Claude Code → CLI args + 文件写入；OpenCode Server → HTTP API system 字段

9. **OCS 全量注入**
   OpenCode Server 的 B + C 通道合并到 S2 (Call-level System)，S2 无 hedging
   所有内容以同等权重送达，无需像 Claude Code 那样分离机制
   ✅ S2 在同一 prompt cycle 内通过 lastUser.system 持续生效
   ⚠️ 跨消息 lastUser 切换 — 每条消息必须附带 system 字段
