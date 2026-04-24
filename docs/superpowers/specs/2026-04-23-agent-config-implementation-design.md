# Agent Config Implementation Design

> Issue #25 实施设计，基于 `docs/architecture/Agent-Config-Design.md` 理论设计 + 源码核实修正。
> 头脑风暴日期: 2026-04-23

## 1. 澄清的需求

| # | 问题 | 决策 |
|---|------|------|
| 1 | 交付范围 | **包含 CC + OCS 全部**，路线 B (垂直切片) |
| 2 | 配置目录 | `~/.hotplex/agent-configs/` 全局共享；`configs/` 存模板 |
| 3 | 多用户 | 不支持，Agent 是个人助理，单一用户 |
| 4 | 平台变体 | **追加**，不替换；只添加平台认知，context 构建需清晰标注 |
| 5 | 变更生效 | 仅新 session 生效 (resume 除外) |
| 6 | Resume | 重新注入，使 resume 成为"热生效"机制 |
| 7 | 清理 | 不自动清理 `.claude/rules/hotplex-*.md`，用户决策 |
| 8 | 子 Agent | Gateway 层不处理，Worker 内部实现细节 |

## 2. 源码核实发现

### 2.1 Claude Code — B 通道就绪

`internal/worker/claudecode/worker.go` 的 `SessionInfo` 已有:
```go
SystemPrompt        string  // → --append-system-prompt
SystemPromptReplace string  // → --system-prompt
```

`buildCLIArgs()` 已实现注入。**无需改动 Worker 本体，只需在 Bridge 层填充字段。**

### 2.2 OpenCode Server — 端点不匹配

**当前 Worker 使用**:
```
POST /sessions/{id}/input          ← 两个错误: /sessions 应为 /session; /input 端点不存在
Body: {"content": "...", "metadata": {...}}
```

**OCS 实际端点** (核实: `~/opencode/packages/opencode/src/server/routes/instance/`):
- InstanceRoutes (index.ts:58) 挂载 `SessionRoutes()` 在 `/session` 前缀下 (单数)
- SessionRoutes (session.ts:847) 定义 `POST /:sessionID/message`
- 完整路径: `POST /session/:sessionID/message`
```
POST /session/:sessionID/message
Body: {parts: [{type: "text", text: "..."}], system?: "...", agent?: "...", model?: {...}}
```

**两个迁移点**:
1. URL 从 `/sessions/{id}/input` → `/session/{id}/message` (前缀单数 + 路径变更)
2. 请求体从 `InputData{Content, Metadata}` → `PromptInput{parts[], system?}`

另外: 创建 session 路径也应为 `POST /session` (非 `/sessions`)，
返回 `Session.Info` 含 `id` 字段 (非 `session_id`)，hotplex 的 `createSession` 需适配。

### 2.3 OCS System 字段行为 (源码核实修正)

设计文档声称的 "S3 Sticky 持久性" 经源码核实需修正。

**核实依据**: `~/opencode` 本地源码 `llm.ts` + `prompt.ts` (2026-04-24 核实)

实际行为:
- `llm.ts` system prompt 组装: `agent.prompt | provider prompt` + `input.system` + `input.user.system`
- `input.user` 即 `lastUser` — 从消息历史倒序查找的最后一个 user 消息
- **同一 prompt cycle 内** (tool 迭代): `lastUser` 不变 → system 持续生效
- **跨消息时**: 新 user 消息成为新 `lastUser` → 若不带 `system` 则旧注入丢失
- **结论: HotPlex 每条消息都必须携带 `system` 字段** — 不存在跨消息自动继承

影响: 实现方案不变 — 每条 Send() 都附带 system prompt。Compaction 只影响历史回溯，不影响当前轮次。

## 3. Config 加载架构

### 3.1 目录结构

```
~/.hotplex/agent-configs/          ← 运行时目录 (全局，所有 session 共享)
├── SOUL.md                        ← 人格/价值观 (B 通道)
├── AGENTS.md                      ← 工作规则/行为边界 (B 通道)
├── SKILLS.md                      ← 工具使用指南 (B 通道)
├── USER.md                        ← 用户画像/偏好 (C 通道)
├── MEMORY.md                      ← 跨会话记忆 (C 通道)
├── SOUL.slack.md                  ← Slack 平台追加 (可选)
├── SOUL.feishu.md                 ← Feishu 平台追加 (可选)
└── ...

configs/agent-configs/             ← 源码仓库模板 (参考，不直接加载)
├── SOUL.md.example
└── AGENTS.md.example
```

### 3.2 加载规则

1. 基础文件 + 平台变体**追加** (非替换)
2. 平台变体只添加平台特有认知，不重复基础内容
3. YAML frontmatter (`---` 块) 剥离后注入
4. 大小限制: 12K chars/file, 60K total
5. 文件不存在 → 跳过不报错
6. 变更只对新 session 生效
7. configDir 不存在或 Load() 失败 → 静默返回空 configs，session 正常创建（无自定义人格）
8. platform 字符串值: `"slack"` | `"feishu"` | `""` (WebSocket/gateway 直连)

### 3.3 平台变体组装示例

```
SOUL.md 内容:
  "你是 HotPlex 团队的 AI 软件工程搭档..."

SOUL.slack.md 内容:
  "## Slack 平台认知\n- 消息有 40000 字符限制\n- 使用 Slack Markdown..."

最终注入 (追加):
  "你是 HotPlex 团队的 AI 软件工程搭档...\n\n## Slack 平台认知\n- 消息有 40000 字符限制\n..."
```

### 3.4 Loader 接口

```go
// internal/agentconfig/loader.go

// AgentConfigs holds loaded content for all config files.
type AgentConfigs struct {
    Soul   string  // SOUL.md + SOUL.<platform>.md
    Agents string  // AGENTS.md + AGENTS.<platform>.md
    Skills string  // SKILLS.md + SKILLS.<platform>.md
    User   string  // USER.md + USER.<platform>.md
    Memory string  // MEMORY.md + MEMORY.<platform>.md
}

// Load reads all config files from dir, appending platform-specific variants.
// Returns AgentConfigs with frontmatter stripped and size limits enforced.
func Load(dir, platform string) (*AgentConfigs, error)
```

### 3.5 Config 扩展

```go
// internal/config/config.go — 新增 section
type AgentConfig struct {
    Enabled   bool   `mapstructure:"enabled"`
    ConfigDir string `mapstructure:"config_dir"` // 默认 ~/.hotplex/agent-configs/
}
```

## 4. Claude Code 集成

### 4.1 B 通道 — `--append-system-prompt` → S3 尾部

**已有 hook**: `SessionInfo.SystemPrompt` → `buildCLIArgs()` → `--append-system-prompt`

**新增组装函数**:
```go
// internal/agentconfig/cc_prompt.go
func BuildCCBPrompt(configs *AgentConfigs) string
```

组装格式:
```
# Agent Persona
If SOUL.md is present, embody its persona and tone.
[SOUL.md 内容]

# Workspace Rules
[AGENTS.md 内容]

# Tool Usage Guide
[SKILLS.md 内容]
```

### 4.2 C 通道 — `.claude/rules/hotplex-*.md` → M0

```go
// internal/agentconfig/cc_rules.go
func InjectCRules(workdir string, configs *AgentConfigs) error
```

写入:
- `workdir/.claude/rules/hotplex-user.md` ← USER.md
- `workdir/.claude/rules/hotplex-memory.md` ← MEMORY.md

**不清理**: HotPlex 不主动删除文件，用户决策。`/tmp/` workdir 自然回收。

### 4.3 Resume 行为

Worker crash 后 resume 时:
- B 通道: `--append-system-prompt` 重新传入 (新 session 进程)
- C 通道: `InjectCRules()` 覆盖写入，保证内容最新
- **副作用**: 修改设定文件后 resume 可使新设定生效

### 4.4 Bridge 注入点

```go
// internal/gateway/bridge.go — StartSession()
configs := agentconfig.Load(configDir, platform)
switch workerType {
case worker.TypeClaudeCode:
    info.SystemPrompt = agentconfig.BuildCCBPrompt(configs)
    agentconfig.InjectCRules(workDir, configs)
}
```

### 4.5 OCS Configs 传递路径

CC 的 configs 在 Bridge 层一次性注入 `SessionInfo.SystemPrompt`，Worker 启动后不再需要。
但 OCS 需要**每条消息**都带 system 字段，因此 configs 必须存储在 Worker 生命周期内。

**方案**: 在 OCS Worker 的 `conn` 结构中存储组装好的 system prompt 字符串。
Bridge 层加载 configs 后，将 `BuildOCSSystemPrompt(configs)` 结果传入 OCS Worker 的
`SessionInfo` 或启动参数，Worker 内部保存在 conn 字段中，每次 `Send()` 时附带。

```go
// OCS conn 字段
type ocsConn struct {
    // ...
    systemPrompt string  // ← Bridge 在 session 启动时注入，每条消息附带
}
```

## 5. OpenCode Server 集成

### 5.1 端点迁移 (Step 2)

**当前**:
```
POST /sessions/{id}/input
{"content": "...", "metadata": {...}}
```

**目标**:
```
POST /session/{id}/message
{
  "parts": [{"type": "text", "text": "..."}],
  "system": "optional system prompt"
}
```

经源码核实 (`~/opencode` PromptInput schema, prompt.ts:1704-1729)：`model`、`agent`、`system` 均为 **optional**。
OCS 自动 fallback: agent → `agents.defaultAgent()`，model → agent 配置 → session 上次使用。
**HotPlex 只需提供 `parts` + `system`，不传 model/agent。**

改动文件: `internal/worker/opencodeserver/worker.go` 的 `conn.Send()` 方法。

### 5.2 System 注入 (Step 3)

```go
// internal/agentconfig/ocs_prompt.go
func BuildOCSSystemPrompt(configs *AgentConfigs) string
```

B+C 合并组装 (OCS 无 hedging，所有内容同等权重):
```
# Agent Persona
[SOUL.md]

# Workspace Rules
[AGENTS.md]

# Tool Usage Guide
[SKILLS.md]

# User Profile
[USER.md]

# Persistent Memory
[MEMORY.md]
```

**每条消息都附带 `system` 字段** — OCS 无跨消息持久性，不附带则注入丢失。

## 6. 实施计划

### Step 1: 共享基础设施 + CC 端到端

| 改动 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/agentconfig/loader.go` | AgentConfigs + Load() + frontmatter 剥离 + 大小校验 + 平台变体追加 |
| 新增 | `internal/agentconfig/cc_prompt.go` | BuildCCBPrompt() |
| 新增 | `internal/agentconfig/cc_rules.go` | InjectCRules() |
| 修改 | `internal/config/config.go` | 新增 AgentConfig section |
| 修改 | `internal/gateway/bridge.go` | StartSession() 加载 configs + 路由 |
| 修改 | `cmd/hotplex/main.go` | 初始化传递 configDir |

**交付标准**: CC Worker 加载设定文件，B 通道注入 S3，C 通道写入 rules。

### Step 2: OCS 端点迁移

| 改动 | 文件 | 说明 |
|------|------|------|
| 修改 | `internal/worker/opencodeserver/worker.go` | conn.Send() 从 `/input` → `/message`，请求格式适配 |

**交付标准**: OCS Worker 消息发送走 `/message`，功能等价。

### Step 3: OCS System 注入

| 改动 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/agentconfig/ocs_prompt.go` | BuildOCSSystemPrompt() |
| 修改 | `internal/worker/opencodeserver/worker.go` | 每条消息附带 system 字段 |
| 修改 | `internal/gateway/bridge.go` | OCS 路由分支 |

**交付标准**: OCS 每条消息携带 B+C system prompt。

## 7. 不做的事

- 不自动清理 `.claude/rules/hotplex-*.md`
- 不做多用户隔离
- 不做热更新 (仅新 session 生效，resume 除外)
- 不处理子 Agent (Worker 内部实现)
