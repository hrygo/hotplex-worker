---
type: design
status: implemented
implemented_at: 2026-04-25
implementation_branch: feat/25-agent-config-implementation
tags:
  - design/agent-config
  - architecture/context-injection
  - worker/claude-code
  - worker/opencode-server
  - meta-cognition
related:
  - Claude-Code-Context-Analysis.md
  - OpenCode-Server-Context-Analysis.md
  - internal/agentconfig/META-COGNITION.md
---

# HotPlex Agent Context 设定文件方案 (Dual-Worker)

> **实施状态**: ✅ Phase 1-4 已实现 | 🔄 Phase 5（动态能力）规划中

## 1. 概述与目标

HotPlex Gateway 作为一个统一接入层，需要为底层不同的 Worker（Claude Code / OpenCode Server）提供一致的人格（Persona）和工作上下文。本方案旨在建立一套统一的、可扩展的设定文件架构，实现「一套配置，多端同步」。

### 1.1 核心设计思想：双通道注入
为了确保 Agent 既能严格遵守规则，又能灵活参考背景信息，我们采用了双通道注入模型：
- **B 通道 (Behavioral/Directives)**：定义 Agent「必须遵循」的行为规范、性格特质和工具使用准则。
- **C 通道 (Contextual/Facts)**：提供「参考性」的用户画像、项目背景和跨会话记忆。

---

## 2. 核心概念

### 2.1 术语对照

| 缩写 | 全称 | 说明 |
|------|------|------|
| **CC** | Claude Code | Anthropic CLI 编码 Agent |
| **OCS** | OpenCode Server | OpenCode 的 HTTP Server 模式 |
| **B 通道** | B Channel (System-level) | 行为指令 "必须遵循"，高优先级，无削弱 |
| **C 通道** | C Channel (Context-level) | 上下文数据 "参考信息"，辅助性 |
| **hedging** | 削弱声明 | CC 通过 `prependUserContext()` 注入的 user message 中带有 "may or may not be relevant" |

### 2.2 元认知与 Agent Config 的关系

Agent Config 是 Agent 的**可配置人格层**，而 `META-COGNITION.md` 是 Agent 的**不可配置元认知层**（自我意识基础）。

| 维度 | Agent Config | META-COGNITION.md |
|------|--------------|-------------------|
| **位置** | `~/.hotplex/agent-configs/` (用户可编辑) | `internal/agentconfig/` (代码库内) |
| **可配置性** | 用户可编辑 SOUL/AGENTS/SKILLS/USER/MEMORY | 不可配置，内置硬编码 |
| **注入方式** | `BuildSystemPrompt()` 组装为 `<directives>`/`<context>` | `go:embed` 在 init 时计算 |
| **内容类型** | 人格(B)、规则(B)、工具(B)、用户(C)、记忆(C) | 身份、系统架构、状态机、配置架构、控制命令 |
| **用途** | 用户定制 Agent 行为 | Agent 自我认知 |

两者通过 `BuildSystemPrompt()` **合并注入**到 Worker 的 System Prompt：
- `SOUL.md/AGENTS.md/SKILLS.md` 内容注入到 `<directives>`（B 通道，高优先级）
- `META-COGNITION.md` 内容注入到 `<context>` 顶部（C 通道，Agent 自我认知）
- `USER.md/MEMORY.md` 内容注入到 `<context>` 底部（用户上下文）


---

## 3. Worker Context 架构对比

> 基于 [[Claude-Code-Context-Analysis]] 与 [[OpenCode-Server-Context-Analysis]] 的源码研究。

### 3.1 通道语义定义

| 通道 | 语义 | 注入位置 | 特征 |
|------|------|---------|------|
| **B 通道** (System-level) | 行为指令 "必须遵循" | system prompt 内 | 高优先级，无削弱声明 |
| **C 通道** (Context-level) | 上下文数据 "参考信息" | messages 或等效位置 | 辅助信息，可能被削弱 |

B 通道承载指令性内容 (人格/规则/工具指南)，C 通道承载事实性内容 (用户画像/记忆)。

### 3.2 Claude Code Context 槽位

```
system[] (System Prompt — 5 段)
  S0  Attribution        (不可控)
  S1  CLI Prefix         (不可控, --append-system-prompt 自动切换为 SDK 模式)
  S2  Static Content     (不可控, ~15K tok)
  S3  Dynamic Content    (部分可控)
      ┃ ↓↓↓ B 通道注入点 (--append-system-prompt) ↓↓↓
  S4  System Context     (不可控, gitStatus 等)

messages[] (对话)
  M0  User Context (prependUserContext 追加的 user message)
      附带削弱: "IMPORTANT: may or may not be relevant"
  M1+ Conversation History
```

**要点**: `--append-system-prompt` 注入 S3 尾部，无削弱。同时 S1 自动切换为 SDK 模式 ("running within the Claude Agent SDK")，模型更易接受外部规则。

**实际标签**: CC 通过 `--append-system-prompt` 注入的内容使用 `<persona>` / `<rules>` / `<skills>` 标签（B 通道）和 `<hotplex>` / `<user>` / `<memory>` 标签（C 通道），由 `prompt.go` 的 `BuildSystemPrompt()` 统一组装。

### 3.3 OpenCode Server Context 槽位

OCS 使用两层组装架构：

```
messages[role: "system"] — 两层组装

第一层 (llm.ts:99-111) → system[0] (S0+S2+S3 拼接为单个字符串块):
  S0  Provider Prompt       (由 model ID 路由选择, ~3-5K tok)
  S1  Agent Custom Prompt   (覆盖 S0, 内置 explore/compaction 等)
  S2  Call-level System     ← input.system (per-call 传入)
      ┃ ↓↓↓ B+C 通道注入点 (system field) ↓↓↓
  S3  Last User System      ← input.user.system (从 lastUser 读取)

第二层 (prompt.ts:1473-1479) → system[1..] (追加到 system 数组):
  S4  Environment Info      (每次调用生成, ~150 tok)
  S5  Skills List           (可变, ~500-3K tok)
  S6  Instruction Files     ← AGENTS.md 自动发现 (cwd 向上查找)
  S7  Conditional Injections (Plan Mode / JSON Schema / Plugin Hooks)
```

**要点**: S2 (Call-level System) 无 hedging，所有内容等权。但 S3 的 `lastUser` 在跨消息时切换为新消息 — **HotPlex 必须每条消息都附带 `system` 字段**，不存在"发送一次即持久"的机制。

### 3.4 架构差异对照

| 维度 | Claude Code | OpenCode Server |
|------|-------------|-----------------|
| B 通道实现 | `--append-system-prompt` → S3 尾 | HTTP API `system` 字段 → S2 |
| C 通道实现 | 与 B 合并注入 S3 | 与 B 合并注入 S2 |
| 削弱声明 | M0 有 hedging | S2 无 hedging |
| Provider 支持 | Anthropic only | 20+ (AI SDK) |
| System Prompt 位置 | API `system[]` 参数 | `messages[role: "system"]` |
| 文件自动发现 | `.claude/rules/` | `AGENTS.md` (cwd 向上查找) |

---

## 4. 统一通道映射

### 4.1 映射表

| Config 文件 | 通道 | CC 机制 | OCS 机制 | 强度 |
|------------|------|---------|----------|------|
| **SOUL.md** | B | `--append-system-prompt` → S3 | `system` field → S2 | 强 (无 hedging) |
| **AGENTS.md** | B | 同上 | 同上 | 强 |
| **SKILLS.md** | B | 同上 | 同上 | 强 |
| **USER.md** | C | 与 B 合并注入 S3 | 与 B 合并注入 S2 | 强 (合并后无 hedging) |
| **MEMORY.md** | C | 与 B 合并注入 S3 | 与 B 合并注入 S2 | 强 (合并后无 hedging) |

B/C 合并注入同一机制的原因：两个 Worker 统一使用 system-level 注入，B/C 分类通过 `<directives>`/`<context>` XML 标签在内容层面传达语义优先级，而非通过不同的注入机制。合并注入消除了 CC C 通道的 hedging 削弱。

### 4.2 分配原则

- 需覆盖 Worker 预设默认值 → B 通道 (SOUL 覆盖身份声明，AGENTS 覆盖默认行为)
- 行为指令性内容 → B (SOUL/AGENTS/SKILLS)
- 事实性上下文数据 → C (USER/MEMORY)

### 4.3 语义分层总览

```
Claude Code:
  S2 静态硬编码        基线安全网
  S3 B-行为框架        ← HotPlex "MUST follow these rules"
  S3 B-人格            ← HotPlex "embody its persona"
  ─── 注意力分界 ───
  S3 C-用户画像        ← HotPlex 参考信息
  S3 C-记忆            ← HotPlex 参考信息

OpenCode Server:
  S0 Provider Prompt   产品身份
  S2 HotPlex 注入      ← B+C 合并, 每条消息带 system
  ───────────────────────────────────────────────────
  S4-S6                环境/Skills/项目 AGENTS.md
```

---

## 5. Prompt 组装

### 5.1 XML 结构

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

<hotplex>
Agent self-identity, operating constraints, and key operational rules.
[internal/agentconfig/META-COGNITION.md — go:embed, C-channel]
</hotplex>

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
- `<directives>` (B 通道) 与 `<context>` (C 通道) 传达优先级
- `<hotplex>` 位于 `<context>` 顶部，为 Agent 提供自我认知基础（C 通道，参考性）
- 每段附 1 行行为指令
- 空组省略 — 仅有 B 内容时不输出 `<context>` wrapper
- 组装逻辑: `internal/agentconfig/prompt.go` → `BuildSystemPrompt()`

---

## 6. Context 分布图

### 6.1 Claude Code

```
system[]
┌──────────────────────────────────────────────────────────────────────┐
│ S0  Attribution                              (不可控)                │
│ S1  CLI Prefix (SDK模式)                     (不可控, 自动切换)      │
│ S2  Static Content (~15K tok)                (不可控)                │
├──────────────────────────────────────────────────────────────────────┤
│ S3  Dynamic Content                          (部分可控)              │
│     ↓↓↓ HotPlex <agent-configuration> (--append-system-prompt) ↓↓↓  │
│     <directives>                                                     │
│     <persona>  ← SOUL.md (~500 tok)                                  │
│     <rules>    ← AGENTS.md (~2K tok)                                 │
│     <skills>   ← SKILLS.md (~1K tok)                                 │
│     </directives>                                                    │
│     <context>                                                        │
│     <hotplex> ← META-COGNITION.md (go:embed, ~3K tok, C-channel)   │
│     <user>     ← USER.md                                             │
│     <memory>   ← MEMORY.md                                           │
│     </context>                                                       │
├──────────────────────────────────────────────────────────────────────┤
│ S4  System Context (gitStatus 等)            (不可控)                │
└──────────────────────────────────────────────────────────────────────┘

messages[]
┌──────────────────────────────────────────────────────────────────────┐
│ M0  User Context <system-reminder>                                   │
│     CLAUDE.md + .claude/rules/*.md + "may or may not be relevant"   │
│ M1+ Conversation History                                             │
└──────────────────────────────────────────────────────────────────────┘
```

### 6.2 OpenCode Server

```
messages[role: "system"] — 两层组装

第一层 → system[0]:
┌──────────────────────────────────────────────────────────────────────┐
│ S0  Provider Prompt "You are OpenCode..."    (不可控)                │
│ S2  Call-level System                        (可控)                  │
│     ↓↓↓ HotPlex <agent-configuration> (system field) ↓↓↓            │
│     <directives>                                                     │
│     <persona> / <rules> / <skills>                                   │
│     </directives>                                                    │
│     <context>                                                        │
│     <hotplex> ← META-COGNITION.md (go:embed, ~3K tok, C-channel)   │
│     <user> / <memory>                                                │
│     </context>                                                       │
│ S3  Last User System (同 cycle 内 lastUser 不变; 跨消息需每条带 system)│
└──────────────────────────────────────────────────────────────────────┘

第二层 → system[1..]:
┌──────────────────────────────────────────────────────────────────────┐
│ S4  Environment / S5  Skills / S6  Instruction Files (AGENTS.md)    │
│ S7  Conditional Injections                                           │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 7. 子 Agent 上下文继承

### 7.1 Claude Code (upstream-blocked)

CC 子 Agent 继承**全部 system prompt**（含 HotPlex B+C 注入），不支持按 Agent 类型裁剪。MEMORY.md 对搜索/研究类子 Agent 无害，风险低。若未来 CC 上游支持 agent-scoped rules 可重新评估。

### 7.2 OpenCode Server

OCS 子 Agent (explore, general) 有自己的专用 prompt，不继承 S2 注入。子 Agent 执行短期特定任务，不需要用户画像。**由 OCS 自行管理，HotPlex 无需干预。**

---

## 8. 完整 Context 组装流程

```
Step 1: 加载设定文件 (共享)
  ~/.hotplex/agent-configs/
  ├── SOUL.md    → B 通道 (人格/语气/价值观)
  ├── AGENTS.md  → B 通道 (工作规则/红线/记忆策略)
  ├── SKILLS.md  → B 通道 (工具使用指南)
  ├── USER.md    → C 通道 (用户画像/偏好/时区)
  └── MEMORY.md  → C 通道 (跨会话记忆)

  加载规则:
  · 按平台选择变体: SOUL.slack.md / SOUL.feishu.md / SOUL.webchat.md 等追加到 SOUL.md (追加模式，非替换)
  · frontmatter (--- 包裹的 YAML) 剥离后注入
  · 大小限制: 8K/文件, 40K/总计
  · 文件不存在 → 跳过

Step 2: 元认知加载 (编译时 go:embed，运行时组装)
  internal/agentconfig/
  ├── META-COGNITION.md → 元认知核心 (5 节: 身份/系统架构/状态机/配置架构/控制命令)
  └── `//go:embed META-COGNITION.md` 在编译时嵌入内容，init() 时拼接为 `hotplexMetacognition` 变量；
      真正的 prompt 组装在 `BuildSystemPrompt()` 调用时完成（会话启动时），注入到 `<context>` 顶部 (C 通道)

Step 3: Worker 路由 (Bridge 层)
  configs := agentconfig.Load(configDir, platform)
  prompt  := agentconfig.BuildSystemPrompt(configs)  // 内含 META-COGNITION.md
  sessionInfo.SystemPrompt = prompt

Step 3a: Claude Code Worker
  claude --append-system-prompt "$SYSTEM_PROMPT" \
         --permission-mode auto --model claude-sonnet-4-6

Step 3b: OpenCode Server Worker
  POST /session/:id/message {
    parts:  [{ type: "text", text: "user message" }],
    system: BuildSystemPrompt(configs)  ← 每条消息都附带
  }
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
- 边界: 不确定时提出假设而非猜测

## 红线

- 绝不泄露 API key、token、密码等敏感信息
- 绝不执行未经确认的 destructive 操作
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

**✅ 无需确认:** 读写搜索文件、运行测试/lint/构建、Git commit/branch
**⚠️ 需确认:** 首次方案设计、删除操作、依赖变更、远程推送、外部服务调用
**❌ 禁止:** push main/master、rm -rf、泄露敏感信息

## 记忆策略

- "记住 X" → 写入 MEMORY.md | "忘记 X" → 从 MEMORY 删
- 行为纠正 → MEMORY 反馈区

## 工具偏好

| 任务 | 首选工具 |
|:-----|:---------|
| 探索代码库 | Task(Explore) |
| 查找文件 | Glob |
| 搜索内容 | Grep |
| 读取文件 | Read |
| 编辑文件 | Edit |
```

### 9.3 SKILLS.md — 工具使用指南 (→ B 通道)

```markdown
---
version: 1
description: "HotPlex 工具使用指南"
---

# SKILLS.md - 工具使用指南

## 架构

用户 → 消息平台 (Slack/飞书/WebChat) → HotPlex 网关 → Worker (你)

## 平台特性

| 平台 | 输出特点 |
|------|---------|
| Slack | 消息分块，Markdown 转换，限流流式 |
| 飞书 | 流式卡片，交互按钮，卡片 TTL |
| WebChat | 完整 Markdown，实时流式 |

## 构建/测试

所有操作必须用 `make` 目标：`make build` / `make test` / `make lint` / `make check`
```

### 9.4 USER.md — 用户画像 (→ C 通道)

```markdown
---
version: 1
description: "HotPlex 用户画像"
---

# USER.md - 用户画像

## 技术背景

- **主要语言**: {{languages}}
- **框架**: {{frameworks}}
- **基础设施**: {{infra}}

## 工作偏好

- 提交风格：原子提交 + Conventional Commits
- 反馈风格：代码审查格式（指出问题 + 给出建议）
- 不要过度解释基础概念

## 沟通偏好

- 保持简洁——不要总结已完成的工作
- 代码用 file:line 格式引用
- 解释技术决策的 WHY
- 不确定时直接说"需要调查"
```

---

## 10. 实施路径

```
Phase 1: 共享基础设施 ✅
├── agent-configs/ 目录的文件加载器 (通用，两种 Worker 共享)
├── frontmatter 解析与文件大小限制 (8K / file, 40K / total)
└── Load(dir, platform) → AgentConfigs 结构

Phase 2: Claude Code 集成 ✅
├── B+C 统一注入 via --append-system-prompt (BuildSystemPrompt)
└── XML 标签分隔各 section，bridge 层 injectAgentConfig 集成

Phase 3: OpenCode Server 集成 ✅
├── Worker conn.Send 切换到 POST /session/{id}/message 端点
└── 每条消息附带 system 字段 (无跨消息持久性)

Phase 4: Bridge 集成与路由 ✅
├── bridge.injectAgentConfig 按 workerType 选择注入方式
└── SessionInfo.SystemPrompt 字段传播链

Phase 5: 动态能力 (规划中)
├── 按平台/通道动态选择配置变体 (SOUL.slack.md 等)
├── 运行时热更新 (文件变更 → 下次会话生效)
├── 用户画像自动学习 (从对话中提取偏好更新 USER.md)
└── MEMORY.md 自动管理 (daily log 压缩)
```

---

## 11. 设计原则总结

1. **注入位置效果优先** — B = 行为框架，C = 上下文数据；统一 system-level 注入，无 hedging
2. **语义分层** — `<directives>`/`<context>` XML 标签传达 B/C 优先级，每段附 1 行行为指令
3. **职责分离** — SOUL (人格) / AGENTS (规则) / SKILLS (工具) / USER (用户) / MEMORY (记忆)
4. **非侵入式** — CC: `--append-system-prompt` 不写文件；OCS: API `system` 字段不接触项目文件
5. **保留 Worker 基线** — CC S2 安全规范、OCS S0 Provider Prompt 均保留
6. **平台适配** — `SOUL.slack.md` / `SOUL.feishu.md` / `SOUL.webchat.md` 等追加模式（非替换）
7. **安全边界** — 8K/文件, 40K/总计；frontmatter 剥离；CC 子 Agent 全量继承 (upstream-blocked)；OCS 子 Agent 天然隔离
8. **OCS 消息级注入** — S2 无 hedging，同 cycle 内持续生效，跨消息需每条带 `system` 字段
9. **元认知与 Agent Config 分离** — `META-COGNITION.md` (不可配置) 定义 Agent 自我认知，属于 C 通道；`~/.hotplex/agent-configs/` (可配置) 定义用户偏好；两者合并注入，`<hotplex>` 位于 `<context>` 顶部
10. **元认知 go:embed + 运行时组装** — `META-COGNITION.md` 内容在编译时通过 `go:embed` 嵌入二进制，init() 时拼接为 `<hotplex>` 变量，Session 启动时由 `BuildSystemPrompt()` 组装进 C-channel；Agent Config 文件在会话启动时 `Load()` 读取
