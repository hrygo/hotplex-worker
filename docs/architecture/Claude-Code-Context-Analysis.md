---
type: research
tags:
  - research/claude-code
  - architecture/context
  - reference/prompt-engineering
related:
  - Agent-Config-Design.md
---

# Claude Code Context Architecture Analysis

> 源码分析 `~/claude-code-src` — Claude Code 如何将 System Prompt、User Context、对话历史组装为一次 Anthropic API 请求。
> 扩展点、竞态分析、Token 预算影响。为 [[Agent-Config-Design]] 提供基础研究。

---

# Part I: Context 架构

## 1. API 请求总览

Claude Code 调用 `anthropic.beta.messages.create()` 时，Context 分布在三个核心参数中：

```
anthropic.beta.messages.create({
  system:   [S0..S4],          // 5 段 system prompt blocks
  messages: [M0..Mn],          // 对话历史（含注入的上下文）
  tools:    [...toolSchemas],  // 工具定义
  ...otherParams
})
```

---

## 2. System Prompt 槽位图

```
parameter: system[]  →  TextBlockParam[]

┌──────────────────────────────────────────────────────────────────────────┐
│ S0  ATTRIBUTION HEADER                                                   │
│   x-anthropic-billing-header: ...                                       │
│   来源: splitSysPromptPrefix() → attributionHeader                      │
│   作用: Anthropic 内部计费追踪                                           │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S1  CLI PREFIX                                                           │
│   "You are Claude Code, Anthropic's official CLI for Claude.\          │
│    You are an interactive agent that helps users with software          │
│    engineering tasks. Use the instructions below and the tools          │
│    available to you to assist the user."                                │
│   来源: CLI_SYSPROMPT_PREFIXES 集合匹配                                 │
│   作用: 产品身份声明 + 基本角色定位                                      │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S2  STATIC CONTENT                    (稳定前缀，变更极少)              │
│ ═════════════════════════════════════════════════════════════════════════ │
│  ┌─ # System ─────────────────────────────────────────────────────────┐ │
│  │  · Markdown 渲染规则 (CommonMark, monospace)                       │ │
│  │  · 权限模式交互说明 (用户可 approve/deny 工具调用)                   │ │
│  │  · <system-reminder> 标签语义解释                                  │ │
│  │  · Prompt injection 防护指引                                       │ │
│  │  · Hooks 回调机制说明                                              │ │
│  │  · 自动上下文压缩通知                                              │ │
│  ├───────────────────────────────────────────────────────────────────┤ │
│  ├─ # Doing Tasks ───────────────────────────────────────────────────┤ │
│  │  · 软件工程任务优先（bug/feature/refactor/explain）                │ │
│  │  · 安全编码（OWASP Top 10）                                        │ │
│  │  · 代码风格: 不加多余功能/注释/abstraction                         │ │
│  │  · 避免向后兼容 hack，移除死代码                                   │ │
│  │  · 忠实报告结果（不谎称测试通过）                                   │ │
│  ├───────────────────────────────────────────────────────────────────┤ │
│  ├─ # Executing Actions with Care ───────────────────────────────────┤ │
│  │  · 可逆性 + 影响范围评估框架                                       │ │
│  │  · 高风险操作清单 (删除/force-push/外部发布)                       │ │
│  │  · 授权范围匹配原则                                                │ │
│  ├───────────────────────────────────────────────────────────────────┤ │
│  ├─ # Using Your Tools ──────────────────────────────────────────────┤ │
│  │  · 专用工具优先: Read > cat, Edit > sed, Write > heredoc          │ │
│  │  · Agent/Skill/Discovery/Verification Agent 指南                   │ │
│  ├───────────────────────────────────────────────────────────────────┤ │
│  ├─ # Tone and Style ────────────────────────────────────────────────┤ │
│  │  · 简洁直接、结构化输出、不使用 emoji                              │ │
│  ├───────────────────────────────────────────────────────────────────┤ │
│  ├─ # Output Efficiency ─────────────────────────────────────────────┤ │
│  │  · 直奔主题、倒金字塔结构、匹配任务复杂度                          │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│   来源: getSystemPrompt() → 前 6 个固定 section (~15K tokens)          │
│   作用: 稳定的行为规范，变更极少，作为稳定前缀获得最高缓存命中率           │
└──────────────────────────────────────────────────────────────────────────┘

    ┄┄┄┄ SYSTEM_PROMPT_DYNAMIC_BOUNDARY ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄
    （CC 内部的静态/动态内容分界线，非缓存机制的分界）

┌──────────────────────────────────────────────────────────────────────────┐
│ S3  DYNAMIC CONTENT                   (会话级可变)                      │
│  ┌─ session_guidance ── 工具选择/Agent/Skill 指引 (~500 tok)          │ │
│  ├─ memory ──────────── loadMemoryPrompt() auto-memory (~300 tok)     │ │
│  ├─ env_info ────────── Platform/OS/Model/CWD (~100 tok)             │ │
│  ├─ language ────────── settings.language (~100 tok)                  │ │
│  ├─ output_style ────── 自定义输出风格                                │ │
│  ├─ MCP instructions ── 已连接 MCP Server 的 instructions (~500-5K)  │ │
│  ├─ scratchpad ──────── 草稿板功能说明                                │ │
│  ├─ frc ─────────────── 工具结果清理规则                              │ │
│  ├─ summarize ───────── 长工具结果摘要策略                            │ │
│  └─ feature-gated ───── Token Budget / Length Anchors / Brief         │ │
│   来源: getSystemPrompt() → dynamicSections 数组 (~2K tokens)          │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S4  SYSTEM CONTEXT APPEND                                                │
│  gitStatus: branch + main branch + git user + status + 5 commits       │
│  cacheBreaker: [CACHE_BREAKER: ...]  (ant-only)                        │
│   来源: getSystemContext() → appendSystemContext() 追加到 system[] 尾部 │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Messages 槽位图

```
parameter: messages[]  →  MessageParam[]

┌──────────────────────────────────────────────────────────────────────────┐
│ M0  USER CONTEXT (prependUserContext)   isMeta: true                    │
│   <system-reminder>                                                     │
│     # claudeMd                                                          │
│     ├─ ~/.claude/CLAUDE.md          ← 全局用户指令                      │
│     ├─ ./CLAUDE.md                  ← 项目级指令                        │
│     └─ .claude/rules/*.md           ← 规则文件                          │
│     # currentDate: 2026/04/23                                           │
│     IMPORTANT: this context may or may not be relevant...               │
│   </system-reminder>                                                    │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ M1  DEFERRED TOOLS LIST (tool_search)   isMeta: true                    │
│ M2  SESSION START REMINDER    local-command-caveat                      │
│ M3+ CONVERSATION HISTORY (多轮对话, auto-compact 压缩)                  │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 4. 组装流程

```
① fetchSystemPromptParts()                    queryContext.ts:44
   getSystemPrompt() + getUserContext() + getSystemContext()  (并行)

② buildEffectiveSystemPrompt()                systemPrompt.ts:41
   Priority: override > coordinator > agent > custom > default
   + appendSystemPrompt (always appended)

③ appendSystemContext()                       api.ts:437
   systemPrompt + systemContext → fullSystemPrompt

④ splitSysPromptPrefix()                      api.ts:321
   → S0 + S1 + S2 + S3 + S4 (按前缀顺序分割为独立 system blocks)

⑤ prependUserContext()                        api.ts:449
   userContext → <system-reminder> 包裹 → messages[] 头部

⑥ normalizeMessagesForAPI()                   messages.ts
   格式转换 + tool_use/tool_result 配对 + 裁剪 + cache breakpoints

⑦ anthropic.beta.messages.create()            claude.ts:1822
   { system: [S0..S4], messages: [M0..Mn], tools, model, thinking, stream }
```

---

## 5. 缓存策略

> Anthropic Prompt Caching 是**前缀匹配**机制，层级为 `tools → system → messages`。
> CC 通过 `cache_control` 断点标记稳定前缀，系统自动回溯 ~20 个 block 寻找最长匹配。
> 缓存 per-organization 隔离，TTL 5min (默认) 或 1h，每次命中自动刷新。
> Cache read 费用为 base input 的 10%，非免费。

```
CC 的 cache_control 断点策略 (推测，基于源码观察):

  tools[]          ← 可能标记 cache_control (定义稳定)
  system[]:
    S0 + S1 + S2   ← 稳定前缀，CC 可能在 S2 末尾标记 cache_control (较长 TTL)
    S3 + S4        ← 会话级可变，S4 中 gitStatus 每轮变化
  messages[]:
    M0 + History   ← 多轮对话中形成自然前缀，前序内容命中缓存

多轮对话缓存示意:
  Turn 1: [tools][S0+S1+S2][S3][S4][M0]         → 全量计算 (cache write)
  Turn 2: [tools][S0+S1+S2][S3][S4][M0][M1][M2] → S0~S2 前缀命中 (cache read)
  Turn 3: [tools][S0+S1+S2][S3][S4][M0]...[M4]  → S0~M2 前缀命中 (cache read)

  注意: S4 (gitStatus) 每轮变化时，从 S4 起需要重新计算，
        但 S0~S3 若不变仍然命中缓存。

首轮: ~25K-45K tokens 全量计算 (cache write)
后续: 稳定前缀命中缓存，仅新增部分需要计算
```

---

## 6. System Prompt 优先级

```
buildEffectiveSystemPrompt() 决策链:

  overrideSystemPrompt? ──yes──→ [override] + append
       │ no
  coordinator mode? ──yes──→ [coordinator] + append
       │ no
  agent definition? ──yes──→ [agent] (或 proactive 时 [default+agent]) + append
       │ no
  customSystemPrompt? ──yes──→ [custom] + append
       │ no
  └──→ [defaultSystemPrompt] + append
```

---

## 7. 关键源码索引

| 功能 | 文件 | 核心函数 |
|------|------|----------|
| 默认 System Prompt | `src/constants/prompts.ts` | `getSystemPrompt()` L444 |
| Section 管理 | `src/constants/systemPromptSections.ts` | `resolveSystemPromptSections()` |
| 优先级选择 | `src/utils/systemPrompt.ts` | `buildEffectiveSystemPrompt()` L41 |
| User Context | `src/context.ts` | `getUserContext()` L155 |
| System Context | `src/context.ts` | `getSystemContext()` L116 |
| 入口 | `src/utils/queryContext.ts` | `fetchSystemPromptParts()` L44 |
| System → API Blocks | `src/services/api/claude.ts` | `buildSystemPromptBlocks()` L3213 |
| 缓存分割 | `src/utils/api.ts` | `splitSysPromptPrefix()` L321 |
| User Context 注入 | `src/utils/api.ts` | `prependUserContext()` L449 |
| System Context 追加 | `src/utils/api.ts` | `appendSystemContext()` L437 |
| 消息标准化 | `src/utils/messages.ts` | `normalizeMessagesForAPI()` |
| 查询引擎 | `src/QueryEngine.ts` | `submitMessage()` L209 |
| API 调用 | `src/services/api/claude.ts` | `queryModelWithStreaming()` L1266 |

---

## 8. 设计启示

### 8.1 Boundary 为何重要

`SYSTEM_PROMPT_DYNAMIC_BOUNDARY` 前 S2 对所有用户完全相同，作为最稳定的缓存前缀。边界后 S3/S4 含运行时变量，变化频率更高。但这不是缓存机制的硬分界——实际缓存效果取决于 `cache_control` 断点位置和前缀匹配。

### 8.2 CLAUDE.md 的注入位置

CLAUDE.md 放在 `messages` 头部（M0），不在 `system` 参数中。优势：system 缓存不受用户指令变更影响；语义隔离（"may not be relevant"）；灵活更新。多轮对话中 M0 作为前缀的一部分仍可命中缓存。

### 8.3 关键洞察

- **system prompt 内的内容优先级高于 messages 中的内容**
- **`--append-system-prompt` 注入 S3 尾部，优先级高于 S2 硬编码规范**
- **`--system-prompt` 完全替换 S1~S3，获得最大控制权**
- **CLAUDE.md 的 "OVERRIDE" 声明是软约束，被 "may not be relevant" 削弱**

---

---

# Part II: 扩展点与竞态分析

## 9. CLI 参数 → Context 槽位映射

```
┌──────────────────────────────┬───────────┬───────────────────────────────┐
│ CLI 参数                     │ 目标槽位  │ 作用                          │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ System Prompt 控制 ═══   │           │                               │
│ --system-prompt <text>       │ S1~S3     │ 完全替换 defaultSystemPrompt  │
│ --system-prompt-file <file>  │ S1~S3     │ 同上，从文件读取              │
│ --append-system-prompt <txt> │ S3 尾部   │ 追加到 system prompt 末尾     │
│ --append-system-prompt-file  │ S3 尾部   │ 同上，从文件读取              │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 模型与推理 ═══           │           │                               │
│ --model <model>              │ API param │ 模型选择                      │
│ --effort <level>             │ API param │ low/medium/high/max           │
│ --thinking <mode>            │ API param │ enabled/adaptive/disabled     │
│ --task-budget <tokens>       │ API param │ API 侧 token 预算            │
│ --max-turns <n>              │ API param │ 最大 agentic 轮次             │
│ --max-budget-usd <amount>    │ API param │ 最大花费限额                  │
│ --betas <headers...>         │ API param │ 实验性 beta headers           │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 工具控制 ═══             │           │                               │
│ --allowedTools <tools...>    │ tools[]   │ 白名单                        │
│ --disallowedTools <tools...> │ tools[]   │ 黑名单                        │
│ --tools <tools...>           │ tools[]   │ 精确指定可用集合              │
│ --mcp-config <configs...>    │ tools[]   │ 加载 MCP 服务器               │
│ --strict-mcp-config          │ tools[]   │ 忽略其他 MCP 配置             │
│ --permission-prompt-tool     │ tools[]   │ SDK 权限提示工具              │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 权限 ═══                 │           │                               │
│ --permission-mode <mode>     │ API param │ default/plan/auto/accept-edits│
│ --dangerously-skip-perms     │ 行为      │ 跳过所有权限检查              │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 目录 ═══                 │           │                               │
│ --add-dir <dirs...>          │ M0 + S3   │ 额外目录 + CLAUDE.md 加载     │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ Agent ═══                │           │                               │
│ --agent <agent>              │ S1~S3     │ 加载 .claude/agents/<name>.md │
│ --agents <json>              │ S1~S3     │ JSON 定义自定义 Agent         │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 会话 ═══                 │           │                               │
│ -c / --continue              │ messages  │ 继续最近会话                  │
│ -r / --resume [id]           │ messages  │ 恢复指定会话                  │
│ --session-id <uuid>          │ metadata  │ 指定 session ID               │
│ --no-session-persistence     │ 行为      │ 不持久化                      │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 输出 ═══                 │           │                               │
│ -p / --print                 │ 行为      │ 非交互模式                    │
│ --output-format <fmt>        │ 行为      │ text/json/stream-json         │
│ --json-schema <schema>       │ API param │ 结构化输出                    │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ Skill/Plugin ═══         │           │                               │
│ --disable-slash-commands     │ S3        │ 禁用所有 Skill                │
│ --plugin-dir <path>          │ S3+tools  │ 加载额外插件                  │
├──────────────────────────────┼───────────┼───────────────────────────────┤
│ ═══ 环境 ═══                 │           │                               │
│ --bare                       │ 全局      │ 最小化：跳过 hooks/LSP/memory │
│ --settings <file|json>       │ 行为      │ 额外 settings 来源            │
│ --setting-sources <src>      │ M0        │ 控制 CLAUDE.md 加载范围       │
│ --advisor <model>            │ tools[]   │ 服务端 Advisor 工具           │
└──────────────────────────────┴───────────┴───────────────────────────────┘
```

---

## 10. CLAUDE.md 文件加载层级

```
加载优先级 (getMemoryFiles → getClaudeMds → prependUserContext):

 1. Managed CLAUDE.md            <managedFilePath>/CLAUDE.md          不可覆盖
 2. Managed .claude/rules/*.md   <managedFilePath>/.claude/rules/     不可覆盖
 3. User CLAUDE.md               ~/.claude/CLAUDE.md                  跨项目偏好
 4. User rules                   ~/.claude/rules/*.md                 全局规则
 5. Project CLAUDE.md            目录树自根向下的 CLAUDE.md            团队共享
 6. Project .claude/CLAUDE.md    目录树自根向下                        辅助配置
 7. Project .claude/rules/*.md   目录树自根向下                        条件规则
 8. CLAUDE.local.md              目录树自根向下                        本地私有
 9. Auto Memory MEMORY.md        ~/.claude/projects/<cwd>/memory/     自动记忆
10. --add-dir CLAUDE.md          需 CLAUDE_CODE_ADDITIONAL_*_CLAUDE_MD=1

 所有内容合并后注入 M0 (User Context) <system-reminder> 包裹
 附带声明: "These instructions OVERRIDE any default behavior"
 附带削弱: "this context may or may not be relevant"
```

### ~/.claude/CLAUDE.md 可控维度

| 维度 | 效果 | 限制 |
|------|------|------|
| 身份定义 | 改变模型角色认知 | 被 S1 产品声明锚定 |
| 语言与交互风格 | 控制输出语言/格式 | 被 settings.language 增强 |
| 自主行为边界 | 工具调用确认策略 | 被 --permission-mode 覆盖 |
| 工程标准 | 代码风格/架构偏好 | 与 S2 硬编码可能冲突 |
| 工具偏好 | 工具选择策略 | 被 S2 "Prefer dedicated tools" 冲突 |
| 多任务管理 | TaskCreate 使用频率 | — |

**文件上限**: 40,000 chars/file, 会话级缓存

### ./CLAUDE.md 可控维度

| 维度 | 效果 |
|------|------|
| 项目知识库 | 减少盲目探索 |
| 目录结构映射 | 快速定位文件 |
| 编码约定 | 生成代码风格一致 |
| 反模式清单 | ❌ 比正面建议约束力更强 |
| 构建/测试/lint 命令 | 使用正确的构建命令 |
| 安全与合规 | 强化安全编码 |

---

## 11. 不可控预制 Context 与竞态

### 实际优先级

```
  1. S2 静态硬编码规范        ← 不可变, 稳定前缀, ~15K tok
  2. S3 append-system-prompt  ← 可控, 注入 system prompt 尾部
  3. <system-reminder> 内容   ← 系统注入, 被声明 "可能不相关"
  4. CLAUDE.md 用户指令       ← 用户控制, 有 OVERRIDE 声明但被削弱
  5. hooks 输出               ← 用户配置但往往无意识
  6. 运行时 system-reminder   ← 完全不可控, 动态注入
```

### 关键冲突场景

| 冲突 | S2 硬编码 | 用户期望 | 实际结果 |
|------|----------|---------|---------|
| 注释风格 | "Default to writing no comments" | "所有公共函数必须写 JSDoc" | 模型倾向 S2（减少注释） |
| 输出详细度 | "Keep text output brief" | "详细解释每个决策" | 模型倾向简短 |
| 工具选择 | "Prefer dedicated tools" | "优先使用 Bash" | 模型遵循 S2 专用工具优先 |
| 安全约束 | "Be careful with security" | "跳过输入验证加速" | 模型拒绝不安全要求（设计意图） |
| Hooks 覆盖 | "Treat hooks as user feedback" | 用户不知 hook 注入了什么 | Hook 输出等同于用户指令 |

### Token 预算

```
固定开销 (首轮, 不含历史):

  S0+S1          ~150 tok      不可控
  S2 Static    ~15,000 tok      不可控, 稳定前缀
  S3 Dynamic    ~2,000 tok      部分可控 (MCP 可变: 500-5K)
  S4 SysCtx     ~1,000 tok      不可控
  M0 User Ctx 1,000-10,000 tok  用户可控 (CLAUDE.md)
  tools[]      5,000-20,000 tok CLI 参数可控 (MCP 可变)

  合计: ~25K-45K tokens (占 200K window 的 12.5%-22.5%)
  剩余: 78%-87.5% 用于对话
```

---

## 12. 控制策略与最佳实践

### 控制力递增

| Level | 方式 | 适合场景 |
|-------|------|---------|
| 0 默认 | CLAUDE.md + rules/ | 日常开发 |
| 1 追加 | + --append-system-prompt | 项目特定约束 |
| 2 精简 | + --bare | CI/CD 自动化 |
| 3 全控 | --system-prompt + --bare + --tools | 专用 Agent |
| 4 SDK | initialize { systemPrompt, agents, hooks } | 生产级 Agent |

### CLAUDE.md 编写

**DO**: 明确的 MUST/NEVER/ALWAYS · 文件路径→代码映射 · ❌ 反模式清单 · <10K tokens · .claude/rules/ 条件规则

**DON'T**: 对抗 S2 硬编码 · 临时性指令 · 超长代码片段 · ~/.claude/CLAUDE.md 放项目内容
