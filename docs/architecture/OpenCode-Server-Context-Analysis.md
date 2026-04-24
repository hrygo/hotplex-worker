---
type: research
tags:
  - research/opencode
  - architecture/context
  - reference/prompt-engineering
  - research/standalone
---

# OpenCode Server Context Architecture Analysis

> 源码级分析 `~/opencode/packages/opencode/src/`。全面剖析 OpenCode Server 如何将 System Prompt、Instruction 文件、对话历史、Agent 定义组装为一次 LLM API 请求。
> 涵盖：Server 架构、Context 槽位图、Provider Prompt 路由、Instruction 加载算法、Compaction 策略、Overflow 检测、Plugin Hooks。
> 本文件专注 OpenCode Server 自身实现，不与其他 Agent 对比。

---

# Part I: Server 架构

## 1. HTTP Server 概览

OpenCode 基于 **Hono** 框架构建 HTTP Server，通过 **Effect-TS** 管理依赖注入和生命周期。

```
opencode server
  ├── Hono App
  │   ├── Middleware Stack
  │   │   ├── ErrorMiddleware        → 统一错误处理
  │   │   ├── AuthMiddleware         → Basic Auth (可选, OPENCODE_SERVER_PASSWORD)
  │   │   ├── LoggerMiddleware       → 请求日志
  │   │   ├── CompressionMiddleware  → gzip 压缩
  │   │   ├── CorsMiddleware         → CORS 白名单
  │   │   ├── InstanceMiddleware     → 解析 directory → 初始化 Instance
  │   │   └── FenceMiddleware        → 请求隔离
  │   │
  │   ├── /global/*                  → 全局路由 (health, event SSE, config, upgrade)
  │   ├── /session/*                 → 会话 CRUD + 消息发送
  │   ├── /config/*                  → 实例级配置
  │   ├── /mcp/*                     → MCP 服务器管理
  │   ├── /provider/*                → Provider 管理
  │   ├── /permission/*              → 权限请求/应答
  │   ├── /question/*                → 用户交互式问答
  │   ├── /pty/*                     → 终端 WebSocket
  │   ├── /project/*                 → 项目信息
  │   ├── /experimental/*            → 实验性 HTTP API
  │   └── /                          → 文件路由 + 事件路由 + UI 路由
  │
  └── Runtime
      ├── Bun Adapter / Node Adapter
      ├── WebSocket Upgrade (pty)
      ├── SSE Streaming (events)
      └── mDNS Discovery (可选)
```

### 核心路由与 Context 的关系

| 路由 | 方法 | Context 影响 | 源码 |
|------|------|-------------|------|
| `POST /session` | 创建会话 | 新建 Session 实例 | `session.ts` |
| `POST /session/:id/message` | 发送消息 | **核心入口** → 组装完整 Context → 调用 LLM | `session.ts:847-892` |
| `POST /session/:id/prompt_async` | 异步发送 | 同上，异步执行 | `session.ts` |
| `POST /session/:id/command` | 执行命令 | 解析 Skill 模板 → 注入 system parts | `session.ts` |
| `POST /session/:id/shell` | 执行 Shell | 直接执行，不经过 LLM | `session.ts` |
| `POST /session/:id/summarize` | 压缩会话 | 触发 Compaction → 重构 Context | `compaction.ts` |
| `POST /session/:id/revert` | 回滚消息 | 回退文件变更 + 清理 Context | `session.ts` |
| `PATCH /session/:id` | 更新会话 | 修改 permission ruleset | `session.ts` |
| `GET /event` | SSE 事件流 | 实时推送 Session/Permission/Tool 事件 | `global.ts` |

---

## 2. Instance 与目录绑定

```
InstanceMiddleware 解析链:

  ① 请求参数提取
     directory = query("directory") || header("x-opencode-directory") || process.cwd()

  ② WorkspaceContext.provide()
     workspaceID = OPENCODE_WORKSPACE_ID (可选)

  ③ Instance.provide({ directory })
     → 初始化/复用 Instance
     → 绑定 worktree, config, project 信息
     → 启动 LSP, MCP, Plugin 等子服务

  每个请求都会绑定到一个具体的 Instance (directory)
  Instance 内含: session, agent, provider, lsp, mcp, plugin, tool-registry
```

---

# Part II: Context 组装架构

## 3. API 请求总览

OpenCode 使用 **Vercel AI SDK** (`ai` 包) 的 `streamText()` 调用 LLM。

```typescript
streamText({
  system:   [systemParts],          // AI SDK system messages (role: "system")
  messages: [modelMessages],        // 对话历史 (ModelMessage[])
  tools:    {...toolSchemas},       // AI SDK tool 定义
  model:    wrappedLanguageModel,   // Provider 适配层
  ...otherParams
})
```

**架构要点**: OpenCode 通过 AI SDK 抽象层统一调用各 Provider。system prompt 被打包为 `ModelMessage` 的 `role: "system"` 消息（OpenIA OAuth 和 GitLab Workflow 除外，它们有特殊的 system 传递方式）。

---

## 4. System Prompt 槽位图

OpenCode 的 System Prompt 由 **两层组装** 完成：
- **第一层**（`llm.ts:99-111`）: Provider/Prompt + call-level system + sticky user system
- **第二层**（`prompt.ts:1473-1479`）: Environment + Skills + Instructions

```
parameter: messages[] (role: "system") → ModelMessage[]

┌──────────────────────────────────────────────────────────────────────────┐
│ S0  PROVIDER PROMPT                   由 model.api.id 路由选择           │
│ ═════════════════════════════════════════════════════════════════════════ │
│  路由逻辑 (SystemPrompt.provider(), system.ts:19-33):                    │
│    gpt-4* / o1* / o3*          → beast.txt    (~4K tok, 高能力模式)      │
│    gpt-codex*                  → codex.txt    (~3K tok, Codex 专用)      │
│    gpt*                        → gpt.txt      (~3K tok, OpenAI 标准)      │
│    gemini-*                    → gemini.txt   (~5K tok, Google 专用)      │
│    claude*                     → anthropic.txt (~3K tok, Anthropic 专用)  │
│    trinity*                    → trinity.txt  (~3K tok, Trinity 专用)     │
│    kimi*                       → kimi.txt     (~3K tok, Kimi 专用)        │
│    (default)                   → default.txt  (~3K tok, 通用)             │
│                                                                          │
│  每个模板包含:                                                            │
│   · 产品身份声明 ("You are OpenCode / opencode")                         │
│   · Tone & Style 规范 (简洁、markdown、无 emoji)                         │
│   · Task Management 指引 (TodoWrite 使用)                                │
│   · Tool Usage Policy (并行调用、专用工具优先)                            │
│   · Code Style (无注释、安全编码)                                        │
│   · Doing Tasks 流程 (搜索→实现→验证)                                    │
│   · Code References 格式 (file_path:line_number)                        │
│                                                                          │
│  来源: SystemPrompt.provider(model) → prompt/<provider>.txt              │
│  特殊: Agent 定义可覆盖 prompt 字段，优先级更高 (见 §11)                 │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S1  AGENT CUSTOM PROMPT               由 Agent.prompt 覆盖 S0           │
│  如果 Agent 定义了 prompt 字段 (如 explore agent):                       │
│    → 完全替代 S0 (不叠加)                                                │
│  如果 Agent 无 prompt 字段:                                              │
│    → 使用 S0 (Provider 默认 prompt)                                     │
│                                                                          │
│  系统内置 Agent:                                                         │
│    build     → 无自定义 prompt, 使用 Provider 默认                       │
│    plan      → 无自定义 prompt, 权限受限 (deny edit)                      │
│    explore   → PROMPT_EXPLORE (搜索专用)                                 │
│    compaction→ PROMPT_COMPACTION (压缩专用)                              │
│    title     → PROMPT_TITLE (标题生成)                                   │
│    summary   → PROMPT_SUMMARY (摘要生成)                                 │
│    general   → 无自定义 prompt, 通用 subagent                             │
│                                                                          │
│  用户自定义 Agent:                                                       │
│    AGENTS.md (agent 语法) / config.agent.<name>.prompt → 可定义任意 prompt│
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S2  CALL-LEVEL SYSTEM                 每条消息可变                      │
│  由 prompt() 调用者通过 input.system 传入                               │
│  来源: input.system (string[], 可多段拼接)                              │
│  用途: 单次调用的自定义指令                                             │
│  Token: 0-5000 tok                                                      │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S3  LAST USER SYSTEM                  从消息历史倒序查找 lastUser       │
│  来源: input.user.system (存储在 MessageV2.User 对象的 system 字段)     │
│  机制: prompt 循环每轮从消息历史倒序查找最后一个 role=user 的消息        │
│        将其 system 字段注入到 system[0]                                 │
│  ⚠️ 不是独立持久化机制:                                                │
│    · 同一 prompt cycle 内 (tool 迭代): lastUser 不变 → system 持续生效  │
│    · 跨消息时: 新 user 消息成为新 lastUser → 若不带 system 则旧注入丢失 │
│    · 结论: 外部系统 (如 HotPlex) 必须每条消息都携带 system 字段         │
│  Token: 0-5000 tok                                                      │
└──────────────────────────────────────────────────────────────────────────┘

↑ 以上 S0-S3 在 llm.ts 中组装为 system[0]（单个字符串块）↓

┌──────────────────────────────────────────────────────────────────────────┐
│ S4  ENVIRONMENT INFO                   每次调用生成                      │
│  "You are powered by the model named {model.api.id}..."                 │
│  <env>                                                                   │
│    Working directory: /path/to/project                                   │
│    Workspace root folder: /path/to/worktree                              │
│    Is directory a git repo: yes/no                                       │
│    Platform: darwin/linux/win32                                          │
│    Today's date: Wed Apr 23 2026                                        │
│  </env>                                                                  │
│                                                                          │
│  来源: SystemPrompt.environment(model) (system.ts:48-62)                │
│  Token: ~150 tok                                                        │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S5  SKILLS LIST                        可变, 由 Agent 权限过滤           │
│  "Skills provide specialized instructions and workflows..."             │
│  + Skill.fmt(list, { verbose: true })                                   │
│    → 每个 Skill 的 name, description, 触发条件                          │
│                                                                          │
│  来源: SystemPrompt.skills(agent) (system.ts:65-77)                     │
│  条件: agent 未 deny "skill" 权限                                       │
│  Token: ~500-3000 tok (取决于 Skill 数量)                                │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S6  INSTRUCTION FILES                                                   │
│  ═════════════════════════════════════════════════════════════════════════ │
│                                                                          │
│  加载策略 (Instruction.systemPaths(), instruction.ts:120-161):           │
│  ① 项目级 (从 cwd 向上查找至 worktree, 取第一个匹配):                    │
│     AGENTS.md          ← 主 instruction 文件                             │
│     CLAUDE.md          ← Claude Code 兼容 (可被 Flag 禁用)               │
│     CONTEXT.md         ← 已废弃                                          │
│     (第一个匹配即停止, 不叠加祖先后代中的同名文件)                        │
│                                                                          │
│  ② 全局级:                                                               │
│     $OPENCODE_CONFIG_DIR/AGENTS.md                                       │
│     ~/.opencode/AGENTS.md                                                │
│     ~/.claude/CLAUDE.md          ← Claude Code 兼容                      │
│                                                                          │
│  ③ 配置级 (config.instructions):                                         │
│     本地文件路径 (支持 glob)                                               │
│     远程 URL (https://..., 5s 超时)                                       │
│                                                                          │
│  上下文感知加载 (Instruction.resolve(), instruction.ts:186-228):         │
│    当 Tool 读取文件时 → 从该文件目录向上查找 AGENTS.md                    │
│    → 按消息去重 (claims Map: MessageID → Set<filepath>)                  │
│    → 已在 messages 中加载过的文件 (通过 Read tool)跳过                    │
│    → 已在 systemPaths 中的文件跳过                                        │
│    → 仅在项目根目录下查找                                                 │
│                                                                          │
│  格式: "Instructions from: {filepath}\n{content}"                        │
│  Token: 0-10,000 tok (取决于文件数量和内容)                              │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ S7  CONDITIONAL INJECTIONS                                               │
│  · JSON Schema 输出: STRUCTURED_OUTPUT_SYSTEM_PROMPT (~100 tok)          │
│    (当 format.type === "json_schema" 时注入)                             │
│  · Plugin Hook: "experimental.chat.system.transform"                     │
│    (允许插件修改 system[] 数组)                                           │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Messages 槽位图

```
parameter: messages[] → ModelMessage[] (via AI SDK)

┌──────────────────────────────────────────────────────────────────────────┐
│ M0  SYSTEM MESSAGES                    由 S0-S7 转换而来                 │
│  system[] 中所有内容被包装为:                                             │
│    { role: "system", content: "..." }                                    │
│  OpenAI OAuth Provider 特殊处理:                                         │
│    → system 内容放入 options.instructions (非 messages)                   │
│  GitLab Workflow 特殊处理:                                               │
│    → system 内容放入 workflowModel.systemPrompt                          │
│                                                                          │
│  缓存优化 (llm.ts:113-124):                                               │
│    if (system.length > 2 && system[0] === header)                        │
│      system = [header, system.slice(1).join("\n")]                       │
│    → 维持 2 段结构以利用 Provider 的缓存断点                             │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ M1  USER MESSAGE                       PromptInput 解析                  │
│  多种 Part 类型:                                                         │
│   · TextPart      → { type: "text", text: "..." }                       │
│   · FilePart      → data: URL (图片/文件/base64)                        │
│   · AgentPart     → 触发 Task tool 调用 (subagent)                       │
│   · SubtaskPart   → 直接执行 subagent (不经过对话)                       │
│   · Synthetic     → 系统注入文本 (Read tool 模拟/plan 提醒等)            │
│   · CompactionPart→ 触发 compaction 流程                                │
│   · SnapshotPart  → 文件快照引用                                        │
│   · PatchPart     → 文件变更补丁                                        │
│   · ReasoningPart → 思考链 (extended thinking)                          │
│   · ToolPart      → 工具调用状态 (pending/running/completed/error)       │
└──────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────────────┐
│ M2+ CONVERSATION HISTORY               多轮对话 + 工具结果               │
│  消息持久化到 SQLite (MessageTable + PartTable)                          │
│  通过 MessageV2.toModelMessagesEffect() 转换为 AI SDK 格式               │
│                                                                          │
│  特殊处理:                                                               │
│   · 第 2 轮起: 用户文本消息包装为 <system-reminder>                      │
│   · Compaction: 历史消息被压缩为摘要 + 只保留最近 N 轮                    │
│   · Tool 输出: 截断到 2K 字符 (compaction) / 可配置上限                   │
│   · providerExecuted 标记: 跳过已由 Provider 处理的工具调用               │
└──────────────────────────────────────────────────────────────────────────┘
```

---

## 6. 组装流程

```
① HTTP Request → SessionRoutes.prompt()
   POST /session/:sessionID/message
   body: PromptInput { parts[], agent?, model?, tools?, format?, system? }
   (schema 定义: prompt.ts:1704-1742)

② SessionPrompt.Service.prompt(input)
   ├─ createUserMessage(input)           prompt.ts:921
   │   ├─ 解析 agent 名称 → 获取 Agent 定义
   │   ├─ 解析 model → 获取 Provider.Model
   │   ├─ 解析 parts[] → resolvePart()
   │   │   ├─ text   → TextPart
   │   │   ├─ file   → 读取文件内容 / base64 编码 / data URL
   │   │   │           → 触发 Read tool / MCP resource
   │   │   ├─ agent  → AgentPart + "call task tool" 提示
   │   │   └─ subtask → SubtaskPart (直接执行)
   │   ├─ Plugin.trigger("chat.message")  → 可修改消息
   │   └─ 保存到 SQLite (MessageTable + PartTable)
   │      注意: input.system 存入 MessageV2.User.system 字段
   │
   └─ loop({ sessionID })                prompt.ts:1535
       └─ state.ensureRunning(sessionID, runLoop)

③ runLoop(sessionID)                     prompt.ts:1305
   while (true) {
     ├─ 获取消息历史 MessageV2.filterCompactedEffect()
     ├─ 检查退出条件 (finish !== "tool-calls", 无待处理工具)
     ├─ 第 1 步: fork title 生成
     ├─ 获取 Agent 定义
     ├─ insertReminders() → 注入 plan/build 模式提示
     ├─ 创建 Assistant Message
     ├─ 创建 Processor Handle
     │
     ├─ resolveTools()                   prompt.ts:354
     │   ├─ 遍历 ToolRegistry.tools()
     │   ├─ 转换 schema → ProviderTransform.schema()
     │   ├─ 包装 execute() → plugin hooks + permission check
     │   └─ 遍历 MCP.tools() → 同上
     │
     ├─ 第 2 轮起: 用户消息包装为 <system-reminder>
     ├─ Plugin.trigger("experimental.chat.messages.transform")
     │
     ├─ 并行获取 (Effect.all, 并发执行):
     │   ├─ sys.skills(agent)            → S5 Skills 列表
     │   ├─ sys.environment(model)       → S4 环境信息
     │   ├─ instruction.system()         → S6 Instruction 文件
     │   └─ MessageV2.toModelMessagesEffect() → 对话历史转换
     │
     ├─ 合并 system = [...env, ...skills, ...instructions]  → S4-S6
     │
     └─ handle.process({ system, messages, tools, model })
         └─ LLM.Service.stream(input)    llm.ts:414

④ LLM.Service.stream(input)              llm.ts:72
   ├─ 获取 Provider, Config, Auth
   ├─ 组装 system[]:                     llm.ts:99-111
   │   ├─ [0] Agent.prompt ?? SystemPrompt.provider(model)  → S0/S1
   │   ├─ [1] input.system              → S2
   │   ├─ [2] input.user.system         → S3 (sticky)
   │   ══════════════════════════════════════ 以上拼接为一个字符串
   │   ├─ [+env] environment info        → S4
   │   ├─ [+skills] skills list          → S5
   │   └─ [+instructions] instruction files → S6
   │
   ├─ Plugin.trigger("experimental.chat.system.transform")
   ├─ 缓存优化: 如果 system[0] 未变, 合并其余部分为 system[1]
   ├─ 组装 messages:
   │   OpenAI OAuth: messages = input.messages (无 system)
   │   GitLab Workflow: messages = input.messages (无 system)
   │   默认: system.map(role: "system") + input.messages
   ├─ ProviderTransform.message() → 消息格式适配
   ├─ streamText({ model, messages, tools, ...params })
   │   ├─ wrapLanguageModel → middleware 拦截
   │   ├─ Provider 特定 headers
   │   └─ AI SDK → Provider SDK → HTTP API
   └─ 返回 Stream<LLM.Event>

⑤ SessionProcessor 处理 Stream 事件
   ├─ text-delta      → 更新 TextPart
   ├─ reasoning       → 更新 ReasoningPart
   ├─ tool-call       → 创建 ToolPart (pending → running)
   ├─ tool-result     → 执行工具 → 更新 ToolPart (completed)
   ├─ finish           → 设置 message.finish = "stop"/"tool-calls"/"length"
   └─ error            → 设置 message.error

⑥ 循环判断
   ├─ finish === "tool-calls" || hasToolCalls → continue (重新调用 LLM)
   ├─ overflow → 触发 Compaction → continue
   └─ finish === "stop"/"error" → break (退出循环)
```

### 关键组装代码

**llm.ts:99-111** — 第一层组装 (S0-S3 拼接):

```typescript
const system: string[] = []
system.push(
  [
    // S0: use agent prompt otherwise provider prompt
    ...(input.agent.prompt ? [input.agent.prompt] : SystemPrompt.provider(input.model)),
    // S2: any custom prompt passed into this call
    ...input.system,
    // S3: any custom prompt from last user message
    ...(input.user.system ? [input.user.system] : []),
  ]
    .filter((x) => x)
    .join("\n"),
)
```

**prompt.ts:1473-1479** — 第二层组装 (S4-S6 追加):

```typescript
const [skills, env, instructions, modelMsgs] = yield* Effect.all([
  sys.skills(agent),                    // S5 Skills
  Effect.sync(() => sys.environment(model)),  // S4 Environment
  instruction.system().pipe(Effect.orDie),    // S6 Instructions
  MessageV2.toModelMessagesEffect(msgs, model),
])
const system = [...env, ...(skills ? [skills] : []), ...instructions]
// → 传给 handle.process() 后与 S0-S3 再次拼接
```

**llm.ts:147-160** — 最终消息组装:

```typescript
const messages = isOpenaiOauth
  ? input.messages  // OpenAI OAuth: system → options.instructions
  : isWorkflow
    ? input.messages  // GitLab: system → workflowModel.systemPrompt
    : [
        ...system.map(
          (x): ModelMessage => ({
            role: "system",
            content: x,
          }),
        ),
        ...input.messages,
      ]
```

---

## 7. Provider Prompt路由与内容详解

### 7.1 路由逻辑

```typescript
// system.ts:19-33
export function provider(model: Provider.Model) {
  if (model.api.id.includes("gpt-4") || model.api.id.includes("o1") || model.api.id.includes("o3"))
    return [PROMPT_BEAST]
  if (model.api.id.includes("gpt")) {
    if (model.api.id.includes("codex"))
      return [PROMPT_CODEX]
    return [PROMPT_GPT]
  }
  if (model.api.id.includes("gemini-")) return [PROMPT_GEMINI]
  if (model.api.id.includes("claude")) return [PROMPT_ANTHROPIC]
  if (model.api.id.toLowerCase().includes("trinity")) return [PROMPT_TRINITY]
  if (model.api.id.toLowerCase().includes("kimi")) return [PROMPT_KIMI]
  return [PROMPT_DEFAULT]
}
```

路由完全基于 `model.api.id` 字符串匹配，无任何 Provider 信息参与。

### 7.2 各模板内容特征

| 模板 | 大小 | 身份声明 | 风格关键词 | 特殊指令 |
|------|------|---------|-----------|---------|
| **beast.txt** | ~5K | "opencode, an agent" | 自主、研究、持久 | 必须上网查资料、递归抓取链接、Memory 系统 |
| **gemini.txt** | ~5K | "opencode, an interactive CLI agent" | 安全、规范、验证 | 双工作流 (工程任务/新应用)、5 步验证流程 |
| **anthropic.txt** | ~4K | "OpenCode, the best coding agent" | 专业、客观、任务管理 | TodoWrite 重度使用、Task tool 优先 |
| **gpt.txt** | ~4K | "OpenCode, You and the user share the same workspace" | 务实、资深工程师 | 并行工具调用、自主决策、commentary/final 频道 |
| **codex.txt** | ~3K | "OpenCode, the best coding agent" | 编辑最小化 | 前端防 AI slop、React 现代模式 |
| **default.txt** | ~3K | "opencode, an interactive CLI tool" | 极简 | 1-3 行回答、一字答案最佳、不加注释 |
| **kimi.txt** | ~3K | (未读取) | | |
| **trinity.txt** | ~3K | (未读取) | | |

### 7.3 default.txt 关键片段

```
You MUST answer concisely with fewer than 4 lines of text (not including tool use or code generation),
unless user asks for detail.
```

### 7.4 gpt.txt 关键片段

```
You are a deeply pragmatic, effective software engineer.
You think through the nuances of the code you encounter,
and embody the mentality of a skilled senior software engineer.
...
Persist until the task is fully handled end-to-end within the current turn.
```

### 7.5 beast.txt 关键片段

```
You are opencode, an agent - please keep going until the user's query is completely resolved.
...
THE PROBLEM CAN NOT BE SOLVED WITHOUT EXTENSIVE INTERNET RESEARCH.
...
You MUST iterate and keep going until the problem is solved.
```

---

## 8. Instruction 文件加载机制

### 8.1 文件类型与优先级

```
FILES = ["AGENTS.md", "CLAUDE.md", "CONTEXT.md"]

加载优先级 (Instruction.systemPaths(), instruction.ts:120-161):

1. 项目级 — 自 cwd 向上查找至 worktree (只取第一个匹配)
   ├── AGENTS.md     (始终搜索)
   ├── CLAUDE.md     (受 Flag.OPENCODE_DISABLE_CLAUDE_CODE_PROMPT 控制)
   └── CONTEXT.md    (已废弃)

2. 全局级
   ├── $OPENCODE_CONFIG_DIR/AGENTS.md  (如设置了该环境变量)
   ├── ~/.opencode/AGENTS.md
   └── ~/.claude/CLAUDE.md            (同上, 受 Flag 控制)

3. 配置级 (config.instructions 配置的额外路径)
   ├── 本地文件路径 (支持 glob 展开)
   └── 远程 URL (HTTPS, 5s 超时)
```

**搜索行为**: 对 FILES 中的每个文件，一旦在项目路径中找到，**立即停止** (不继续搜索 FILES 中的下一个文件)。这意味着 AGENTS.md 优先级最高，CONTEXT.md 最低。

### 8.2 上下文感知加载 (Instruction.resolve)

```
触发: 当 Tool 读取一个文件时

算法 (instruction.ts:186-228):
  1. 从被读文件的目录开始, 逐层向上walk
  2. 在每个层级调用 find(dir) 搜索 FILES (AGENTS.md > CLAUDE.md > CONTEXT.md)
  3. 找到后检查去重:
     a. 不能是正在被读取的文件本身
     b. 不能在 systemPaths 中 (已在静态加载的)
     c. 不能已在 messages 的 Read tool 调用中加载过 (通过 extract() 扫描)
     d. 不能在当前消息 ID 的 claims Map 中 (每消息去重)
  4. 通过去重后, 读取内容, 添加到 results
  5. 记录到 claims[messageID].add(filepath)
  6. 继续向上walk, 直到项目根目录
```

**Claims 机制**: 一个 `Map<MessageID, Set<string>>` 追踪每条消息已附加的指令文件。每次 assistant 消息组装完成后调用 `instruction.clear(messageID)` 清理，防止跨轮次累积。

### 8.3 注入格式

每个指令文件以如下格式注入到 system prompt 中:

```
Instructions from: /absolute/path/to/AGENTS.md
[文件内容]
```

此格式在 `instruction.ts:174` 中定义，所有加载路径 (文件 + URL) 统一使用。

---

## 9. Agent 系统

### 9.1 Agent 定义结构

```typescript
{
  name: string,           // 唯一标识
  description: string,    // 描述 (用于 Task tool 调度)
  mode: "primary" | "subagent" | "all",
  prompt?: string,        // 自定义 system prompt (覆盖 Provider 默认)
  model?: {               // 指定模型
    providerID, modelID
  },
  variant?: string,       // 模型变体
  permission: Ruleset,    // 工具权限规则
  temperature?: number,   // 温度
  topP?: number,          // Top-P
  steps?: number,         // 最大 agentic 步数
  options: Record<string, any>,  // Provider 特定选项
  hidden?: boolean,       // UI 不可见
  native?: boolean,       // 系统内置
}
```

### 9.2 内置 Agent

| Agent | mode | prompt 覆盖 | 特殊权限 |
|-------|------|------------|---------|
| build | primary | 无 → 使用 Provider 默认 | 全工具可用 |
| plan | primary | 无 → 使用 Provider 默认 | deny "edit" |
| general | subagent | 无 | 无 TodoWrite |
| explore | subagent | PROMPT_EXPLORE | 只读工具 |
| compaction | primary | PROMPT_COMPACTION | 无工具, hidden |
| title | primary | PROMPT_TITLE | 无工具, hidden |
| summary | primary | PROMPT_SUMMARY | 无工具, hidden |

### 9.3 Agent Prompt 覆盖机制

```typescript
// llm.ts:103
...(input.agent.prompt ? [input.agent.prompt] : SystemPrompt.provider(input.model))
```

如果 Agent 定义了 `prompt` 字段，**完全替代** S0 Provider Prompt（不叠加）。否则使用 Provider 默认模板。

---

## 10. Overflow 检测

```typescript
// overflow.ts
const COMPACTION_BUFFER = 20_000

function usable(input: { cfg: Config.Info; model: Provider.Model }) {
  const context = input.model.limit.context
  if (context === 0) return 0

  // reserved = 20K 与 maxOutputTokens 取最小值 (可被 config 覆盖)
  const reserved =
    input.cfg.compaction?.reserved ?? Math.min(COMPACTION_BUFFER, ProviderTransform.maxOutputTokens(input.model))

  return input.model.limit.input
    ? Math.max(0, input.model.limit.input - reserved)
    : Math.max(0, context - ProviderTransform.maxOutputTokens(input.model))
}

function isOverflow(input: { cfg: Config.Info; tokens: MessageV2.Assistant["tokens"]; model: Provider.Model }) {
  if (input.cfg.compaction?.auto === false) return false
  if (input.model.limit.context === 0) return false

  const count =
    input.tokens.total || input.tokens.input + input.tokens.output + input.tokens.cache.read + input.tokens.cache.write
  return count >= usable(input)
}
```

**计算公式**:
```
usable = model.limit.input - reserved
reserved = min(20_000, maxOutputTokens)  // 除非 config.compaction.reserved 覆盖

对于 200K context 模型:
  reserved ≈ 20K
  usable ≈ 180K
  
  当 tokens.total >= 180K → 触发 Compaction
```

**可配置参数**:
| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `compaction.auto` | true | 自动触发 |
| `compaction.reserved` | 20K | 预留 token 缓冲 |
| `compaction.tail_turns` | 2 | 保护最近 N 轮 |
| `compaction.prune` | — | 启用工具输出截断 |

---

## 11. Compaction 策略

### 11.1 触发流程

```
每次 LLM 返回后:
  1. 检查 isOverflow() → 如果 true
  2. 调用 compaction.create({ auto: true, overflow: true })
  3. Compaction 生成摘要
  4. 压缩完成后继续 runLoop
  5. 最后触发 prune() (后台异步, forkIn scope)
```

### 11.2 摘要生成

Compaction 使用独立的 prompt 模板 (compaction.txt)，生成结构化摘要:

```markdown
## Goal
- [single-sentence task summary]

## Constraints & Preferences
- [...]

## Progress
### Done / In Progress / Blocked
- [...]

## Key Decisions
- [...]

## Next Steps
- [...]

## Critical Context
- [...]

## Relevant Files
- [...]
```

**增量更新**: 若存在之前的摘要 (prior summary)，新摘要会基于旧摘要 + 新对话生成，保持信息连续性。

### 11.3 保护策略

| 常量 | 值 | 说明 |
|------|-----|------|
| `DEFAULT_TAIL_TURNS` | 2 | 默认保留最近 2 轮对话 |
| `PRUNE_PROTECT` | 40,000 | 工具输出保护阈值 (最近 40K token 不截断) |
| `PRUNE_MINIMUM` | 20,000 | 最小截断量 (少于 20K token 不执行) |
| `TOOL_OUTPUT_MAX_CHARS` | 2,000 | 工具输出截断上限 |
| `PRUNE_PROTECTED_TOOLS` | ["skill"] | Skill 工具输出永不截断 |
| `MIN_PRESERVE_RECENT_TOKENS` | 2,000 | 最近轮次最小保护量 |
| `MAX_PRESERVE_RECENT_TOKENS` | 8,000 | 最近轮次最大保护量 |

### 11.4 Prune 策略 (工具输出截断)

Prune 独立于摘要 Compaction，是**另一种上下文优化**:

```
① 从最近消息向后遍历 (不含最近 2 轮)
② 跳过已压缩摘要覆盖的区域
③ 跳过 PRUNE_PROTECTED_TOOLS (skill) 的输出
④ 累计 ToolPart 输出的 token 量
⑤ 超过 PRUNE_PROTECT (40K) 后, 继续累计
⑥ 若总截断量 > PRUNE_MINIMUM (20K) → 执行截断
⑦ 标记被截 ToolPart 的 compacted = Date.now()
```

截断后的 ToolPart 在下次转换为 ModelMessage 时，输出内容被限制为 `TOOL_OUTPUT_MAX_CHARS` (2K 字符)。

### 11.5 Compaction 后的消息过滤

```typescript
// 过滤已压缩的消息,只保留:
// - 最近的 N 轮完整对话 (tail)
// - 压缩后的摘要 (summary msg)
// - 未压缩的重要消息
MessageV2.filterCompactedEffect() → 过滤出非 compacted 标记的消息
```

---

## 12. Plugin Hooks 扩展点

OpenCode 在关键流程中埋入了 Plugin Hook，允许运行时修改 Context：

| Hook | 位置 | 能力 |
|------|------|------|
| `chat.message` | createUserMessage() | 修改用户消息 + parts |
| `experimental.chat.messages.transform` | toModelMessages() 前 | 修改消息历史 (去重/格式) |
| `experimental.chat.system.transform` | system 组装后、LLM 调用前 | 修改 system[] 数组 |
| `chat.params` | streamText() 前 | 修改 temperature/topP/maxOutputTokens |
| `chat.headers` | streamText() 前 | 修改 HTTP headers |
| `tool.execute.before / after` | 工具执行前后 | 拦截/修改工具调用 |
| `command.execute.before` | 命令执行前 | 拦截 slash 命令 |
| `experimental.session.compacting` | Compaction prompt 生成前 | 注入上下文或替换 compaction prompt |

**`experimental.chat.system.transform` 是最强大的扩展** — 可以完全替换、修改或追加 system prompt 中的任何内容。

---

# Part III: 关键设计洞察

## 13. Context 槽位层级关系

```
两层组装架构:

  第一层 (llm.ts) → 核心 Prompt
  ┌───────────────────────────────────────┐
  │ system[0] = S0 + S2 + S3              │ ← 拼接为一个字符串
  │   S0 = Provider 或 Agent prompt        │ ← 产品身份声明
  │   S2 = input.system (调用级)           │ ← HotPlex B+C 通道注入点
  │   S3 = input.user.system (lastUser)   │ ← 来自消息历史倒序的最后 user 消息
  └───────────────────────────────────────┘

  第二层 (prompt.ts) → 动态上下文
  ┌───────────────────────────────────────┐
  │ system += [S4, S5, S6]                │ ← 追加到数组
  │   S4 = environment info               │ ← 模型/路径/日期
  │   S5 = skills list                    │ ← 可用 Skill 目录
  │   S6 = instruction files              │ ← AGENTS.md 等
  │   S7 = structured output prompt       │ ← 条件注入
  └───────────────────────────────────────┘

  最终: system[0] + system[1..] = 完整 system prompt
  缓存优化: 若 system[0] 未变 → 合并为 [header, rest] 两段
```

## 14. System Field 行为 (源码核实: prompt.ts + llm.ts)

`input.user` 即 `lastUser` — 从消息历史倒序查找的最后一条 user 消息 (prompt.ts:1319-1331)。

```
POST /session/:id/message { system: "..." }
  → PromptInput.schema 验证
  → createUserMessage() → 存入 MessageV2.User.system 字段
  → 持久化到 SQLite

llm.ts:99-111 组装时:
  input.user = lastUser (倒序查找)
  input.user.system → 拼入 system[0]
```

**实际行为 (非 sticky)**:
- **同一 prompt cycle 内** (tool 迭代): `lastUser` 不变 → system 持续生效
- **跨消息时**: 新 user 消息成为新 `lastUser` → 若不带 `system` 则旧注入丢失
- **结论: HotPlex 每条消息都必须携带 `system` 字段** — 不存在跨消息自动继承

## 15. <system-reminder> 包裹

```typescript
// prompt.ts (第 2 轮起)
if (round > 1) {
  userMessage = [
    "<system-reminder>",
    ...userMessage,
    "</system-reminder>",
  ].join("\n")
}
```

从第 2 轮开始，每条用户文本消息被 `<system-reminder>` 标签包裹。这告知模型这些消息包含系统级元信息，可能不直接与用户意图相关。不影响 HotPlex 注入的 system 字段（system 字段在系统层组装，不受此包裹影响）。