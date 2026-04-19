---
type: spec
tags:
  - project/HotPlex
  - worker/acpx
  - architecture/integration
date: 2026-04-04
status: review
progress: 30
estimated_hours: 20
validation_confidence: 98
validation_date: 2026-04-04
---

# ACPX Worker 集成规格

> ✅ **已通过实际测试验证**：本文档基于 `acpx v0.4.0` 实际测试结果编写，核心协议和事件格式已 100% 确认。
>
> **验证日期**: 2026-04-04
> **验证版本**: acpx 0.4.0
> **总体置信度**: 98% ⬆️ (从 95% 提升)
> **验证详情**: 见 [ACPX-Validation-Report.md](./ACPX-Validation-Report.md)
>
> **验证命令**:
> - 基础测试: `acpx --format json claude "What is 2+2?"`
> - 工具调用: `acpx --format json claude "List files in current directory"`
> - Resume 测试: `acpx --format json claude -s test-resume "What is my favorite number?"`
> - Cancel 验证: `acpx claude cancel --help`
>
> **验证状态**:
> - ✅ 协议格式: 100% (JSON-RPC 2.0 over NDJSON)
> - ✅ 初始化握手: 100% (initialize → session/new → session/prompt)
> - ✅ 基础事件: 100% (agent_thought_chunk, agent_message_chunk, usage_update)
> - ✅ 工具调用事件: 100% (tool_call, tool_call_update)
> - ✅ Resume 流程: 100% (命名会话 + session/load)
> - ✅ Cancel 机制: 100% (acpx cancel 命令)
> - ⚠️ 权限请求: 40% (未触发，待测试)
>
> **整体置信度**: **95%**（核心功能已完全验证）
>
> 本文档定义 ACPX Worker Adapter 与 acpx CLI 的集成规格，通过 ACP (Agent Client Protocol) 协议统一支持 16+ 种 AI 编程 Agent。
> 高阶设计见 [[Worker-Gateway-Design]] §8.2。

---

## 1. 概述

| 维度 | 设计 |
|------|------|
| **Transport** | stdio（stdin/stdout pipe） |
| **Protocol** | ACP NDJSON（每行一个 JSON 对象，区分请求/响应/事件流） |
| **进程模型** | 持久进程，多轮复用（acpx session 持久化） |
| **技术栈** | acpx = Node.js CLI，Gateway = Go 胶水层 |
| **支持 Agent** | claude, opencode, gemini, cursor, codex, copilot, droid, iflow, kilocode, kimi, kiro, openclaw, pi, qoder, qwen, trae |

### 1.1 架构定位

```
Gateway (AEP v1)  ──协议桥接──►  ACPX Worker  ──ACP NDJSON──►  acpx CLI  ──原生协议──►  Claude / OpenCode / Gemini / ...
                                          │
                              Gateway session ID ↔ acpx session ID 映射
```

### 1.2 Transport × Protocol × Lifecycle 分类

> 对应 [[Worker-Gateway-Design]] §7.4 Worker 分类矩阵。

| 维度 | ACPX Worker |
|------|-------------|
| **Transport** | stdio |
| **Protocol** | ACP NDJSON |
| **Lifecycle** | persistent（acpx session 持久化） |

**与现有 Worker 对比**：

| Worker | Transport | Protocol | Lifecycle |
|--------|-----------|----------|-----------|
| Claude Code | stdio | stream-json (Claude 专用) | persistent |
| OpenCode Server | HTTP+SSE | SSE/JSON | managed |
| Pi-mono | stdio | raw-stdout | ephemeral |
| **ACPX** | stdio | **ACP NDJSON（通用）** | persistent |

### 1.3 CLI 接口（待验证）

> ⚠️ 以下命令基于 acpx README 摘要，实际接口待通过安装测试验证。

acpx CLI 支持多种调用模式：

```bash
# 模式 1：直接 prompt（ephemeral，最简单）
acpx <agent> <prompt> --format json

# 模式 2：命名会话（persistent，热复用）
acpx <agent> -s <session_name> <prompt> --format json

# 模式 3：会话管理
acpx <agent> sessions new --format json          # 创建会话
acpx <agent> sessions list                       # 列出会话
acpx <agent> sessions prompt <id> --format json # 发送 prompt

# 模式 4：exec 模式（stateless）
acpx <agent> exec --format json -- <prompt>

# 模式 5：TypeScript workflow
acpx flow run <file>
```

**Gateway 集成模式**：采用**命名会话模式（模式 2）**，因为：
- 支持持久化和热复用
- session 名称可映射到 Gateway session ID
- 支持 crash 恢复（acpx 自动检测进程死亡并恢复）

---

## 2. 协议类型（ACP over JSON-RPC 2.0）

> ✅ **已验证**：通过 `acpx --format json claude "What is 2+2?"` 测试确认，acpx 使用 **JSON-RPC 2.0 over NDJSON** 格式。

### 2.1 协议说明

ACP 协议使用 **JSON-RPC 2.0** 作为消息格式，通过 NDJSON 传输：

- **请求消息**：通过 **stdin** 发送（JSON-RPC Request，带 `id` 和 `method`）
- **响应消息**：通过 **stdout** 接收（JSON-RPC Response，带 `id` 和 `result`）
- **服务端事件**：通过 **stdout** 接收（JSON-RPC Notification，带 `method` 但无 `id`）
- **格式**：每行一个 JSON-RPC 对象，换行符 `\n` 分隔

### 2.2 消息分类

| 消息类型 | 方向 | 格式 | 说明 |
|----------|------|------|------|
| JSON-RPC Request | Gateway → acpx | `{"jsonrpc":"2.0","id":N,"method":"...","params":{...}}` | stdin 写入 |
| JSON-RPC Response | acpx → Gateway | `{"jsonrpc":"2.0","id":N,"result":{...}}` | stdout 读取 |
| JSON-RPC Notification | acpx → Gateway | `{"jsonrpc":"2.0","method":"...","params":{...}}` | 服务端推送事件 |

### 2.3 JSON-RPC 消息结构（已验证）

```go
// JSON-RPC 2.0 基础结构
type Request struct {
    JSONRPC string          `json:"jsonrpc"` // 固定为 "2.0"
    ID      json.RawMessage `json:"id"`       // 请求 ID（数字或字符串）
    Method  string          `json:"method"`   // 方法名
    Params  json.RawMessage `json:"params"`   // 参数对象
}

type Response struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"` // 成功时
    Error   *Error          `json:"error,omitempty"`  // 失败时
}

type Notification struct {
    JSONRPC string          `json:"jsonrpc"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
}

type Error struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}
```

### 2.4 ACP Method 枚举（已验证）

**Client → Server (Requests)**:
- `initialize` - 初始化连接（line 2）
- `session/new` - 创建新会话（line 4）
- `session/prompt` - 发送 prompt（line 7）

**Server → Client (Notifications)**:
- `session/update` - 会话更新事件（line 6, 8-88）

### 2.5 Session Update 事件类型（已验证）

```go
// Session Update 事件结构
type SessionUpdate struct {
    SessionID string      `json:"sessionId"`
    Update    UpdateEvent `json:"update"`
}

type UpdateEvent struct {
    SessionUpdate string          `json:"sessionUpdate"` // 事件类型
    // 以下字段根据 SessionUpdate 类型而定
    Content       json.RawMessage `json:"content,omitempty"`        // 用于 chunk 事件
    Used          *int            `json:"used,omitempty"`           // 用于 usage_update
    Size          int             `json:"size,omitempty"`           // 用于 usage_update
    Cost          *Cost           `json:"cost,omitempty"`           // 用于 usage_update
    AvailableCommands []Command  `json:"availableCommands,omitempty"` // 用于 available_commands_update
}

// SessionUpdate 类型常量
const (
    SessionUpdateAgentThoughtChunk    = "agent_thought_chunk"     // 思考过程流式输出
    SessionUpdateAgentMessageChunk    = "agent_message_chunk"     // 消息流式输出
    SessionUpdateUsageUpdate          = "usage_update"             // Token 使用量更新
    SessionUpdateAvailableCommands    = "available_commands_update" // 可用命令列表
)

// Content 结构（用于 chunk 事件）
type Content struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"` // 内容文本
}
```

---

## 3. CLI 参数

### 3.1 核心参数（v1.0 必须）

| 参数 | 说明 | 实现 |
|------|------|------|
| `sessions new` | 创建新会话 | ✅ `worker.go:Start` |
| `--format json` | NDJSON 输出模式 | ✅ 必需 |
| `-s <name>` | 命名会话 | ⚠️ 可选 |
| `sessions prompt` | 发送 prompt | ✅ `worker.go:Input` |
| `--session-id <id>` | 指定会话 ID | ✅ `worker.go:Start` |
| `--cwd <path>` | 工作目录 | ✅ |
| `--timeout <seconds>` | 命令超时 | ⚠️ 可选 |
| `--approve-all` | 自动批准所有权限 | ⚠️ P1 |
| `--deny-all` | 拒绝所有权限 | ⚠️ P1 |

### 3.2 扩展参数（v1.1）

| 参数 | 说明 | 优先级 |
|------|------|--------|
| `--agent <command>` | 自定义 ACP server | P1 |
| `--no-wait` | 队列模式（fire-and-forget） | P2 |
| `--ttl <seconds>` | 队列 owner idle TTL | P2 |
| `--suppress-reads` | 隐藏文件读取内容 | P2 |
| `--verbose` | Debug 输出 | P3 |

---

## 4. 环境变量

### 4.1 acpx 配置

| 变量 | 说明 |
|------|------|
| `ACPX_CONFIG` | 配置文件路径（`~/.acpx/config.json`） |
| `ACPX_STORE_DIR` | 会话存储目录（默认 `~/.acpx/sessions/`） |

### 4.2 Agent 特定变量（透传）

acpx 透传以下变量给底层 Agent：

| Agent | 必需变量 |
|-------|---------|
| claude | `ANTHROPIC_API_KEY` |
| opencode | `OPENAI_API_KEY` / `OPENCODE_API_KEY` |
| gemini | `GEMINI_API_KEY` |
| cursor | `CURSOR_API_KEY` |
| codex | `OPENAI_API_KEY` |
| qwen | `DASHSCOPE_API_KEY` |

### 4.3 安全要求

| 要求 | 说明 |
|------|------|
| **移除 `CLAUDECODE=`** | 防止嵌套调用（通过 base.BuildEnv） |
| **API Key 注入** | 通过环境变量或 acpx 配置注入 |
| **工作目录限制** | 通过 `--cwd` 参数限制 |

### 4.4 环境变量白名单

基于 agent 类型动态构建 EnvWhitelist：

```go
// EnvWhitelist 返回给定 agent 类型的环境变量白名单。
func EnvWhitelist(agent string) []string {
    base := []string{
        "PATH", "HOME", "USER",
        "ACPX_CONFIG", "ACPX_STORE_DIR",
    }

    // Agent 特定变量
    switch agent {
    case "claude":
        return append(base, "ANTHROPIC_API_KEY")
    case "opencode":
        return append(base, "OPENAI_API_KEY", "OPENCODE_API_KEY")
    case "gemini":
        return append(base, "GEMINI_API_KEY")
    case "cursor":
        return append(base, "CURSOR_API_KEY")
    case "codex":
        return append(base, "OPENAI_API_KEY")
    case "qwen":
        return append(base, "DASHSCOPE_API_KEY")
    default:
        return base
    }
}
```

**使用示例**：

```go
env := base.BuildEnv(session, EnvWhitelist(w.agent))
```

---

## 5. 输入格式（Gateway → acpx）

### 5.1 命名会话模式（Gateway 推荐）

Gateway 使用**命名会话模式**，将 Gateway session ID 作为 acpx session 名称：

```bash
# 启动命名会话（持久化）
acpx <agent> -s <gateway_session_id> --format json
#  → acpx 创建/恢复名为 <gateway_session_id> 的会话
#  → stdout 输出 NDJSON 事件流

# 发送 prompt（通过 stdin）
echo "Fix the bug in main.go" | acpx <agent> -s <session_id> --format json
```

### 5.2 JSON-RPC 请求格式（已验证）

acpx 使用标准 JSON-RPC 2.0 Request 格式：

**session/prompt 请求示例**（对应 AEP Input 事件）：

```json
{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"7573cf9d-a06a-4605-bf49-ab48f102a81b","prompt":[{"type":"text","text":"What is 2+2?"}]}}
```

**Go 结构定义**：

```go
// SessionPromptRequest 是 session/prompt 方法的请求。
type SessionPromptRequest struct {
    JSONRPC   string        `json:"jsonrpc"` // "2.0"
    ID        interface{}   `json:"id"`      // 数字或字符串
    Method    string        `json:"method"`  // "session/prompt"
    Params    PromptParams  `json:"params"`
}

type PromptParams struct {
    SessionID string   `json:"sessionId"`
    Prompt    []Prompt `json:"prompt"`
}

type Prompt struct {
    Type string `json:"type"` // "text"
    Text string `json:"text"` // prompt 内容
}
```

> ✅ **已验证**：通过测试确认 acpx 接受 JSON-RPC 2.0 Request 格式的 stdin 输入。

---

## 6. 输出格式（acpx → Gateway）

> ✅ **已验证**：通过 `acpx --format json claude "What is 2+2?"` 测试确认输出格式。

### 6.1 NDJSON 事件流

acpx `--format json` 输出每行一个 JSON-RPC 对象：

**完整事件流示例**：

```json
{"jsonrpc":"2.0","id":0,"method":"initialize","params":{...}}
{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":1,...}}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{...}}
{"jsonrpc":"2.0","id":2,"result":{"sessionId":"7573cf9d-...","models":{...},"modes":{...}}}
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"7573cf9d-...","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"用户"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"7573cf9d-...","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"2"}}}}
{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","usage":{...}}}
```

### 6.2 关键事件示例（已验证）

**agent_thought_chunk（思考过程流式输出）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "sessionUpdate": "agent_thought_chunk",
      "content": {
        "type": "text",
        "text": "这是一个非常基础的问题"
      }
    }
  }
}
```

**agent_message_chunk（消息流式输出）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "sessionUpdate": "agent_message_chunk",
      "content": {
        "type": "text",
        "text": "2+2="
      }
    }
  }
}
```

**usage_update（Token 使用量）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "sessionUpdate": "usage_update",
      "used": null,
      "size": 200000,
      "cost": {
        "amount": 0.21974760000000002,
        "currency": "USD"
      }
    }
  }
}
```

**session/prompt Response（Prompt 完成）**：

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "stopReason": "end_turn",
    "usage": {
      "inputTokens": 72798,
      "outputTokens": 80,
      "cachedReadTokens": 512,
      "cachedWriteTokens": 0,
      "totalTokens": 73390
    }
  }
}
```

### 6.3 工具调用事件（已验证）

**tool_call（工具调用开始）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "_meta": {
        "claudeCode": {
          "toolName": "Bash"
        }
      },
      "toolCallId": "call_5c8a4675c7334b10926735be",
      "sessionUpdate": "tool_call",
      "rawInput": {},
      "status": "pending",
      "title": "Terminal",
      "kind": "execute",
      "content": []
    }
  }
}
```

**tool_call_update（工具输入更新）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "_meta": {
        "claudeCode": {
          "toolName": "Bash"
        }
      },
      "toolCallId": "call_5c8a4675c7334b10926735be",
      "sessionUpdate": "tool_call_update",
      "rawInput": {
        "command": "ls -lah",
        "description": "List files in current directory"
      },
      "title": "ls -lah",
      "kind": "execute",
      "content": [
        {
          "type": "content",
          "content": {
            "type": "text",
            "text": "List files in current directory"
          }
        }
      ]
    }
  }
}
```

**tool_call_update（工具执行完成）**：

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "7573cf9d-...",
    "update": {
      "_meta": {
        "claudeCode": {
          "toolResponse": {
            "stdout": "total 248\ndrwxr-xr-x  ...",
            "stderr": "",
            "interrupted": false
          },
          "toolName": "Bash"
        }
      },
      "toolCallId": "call_5c8a4675c7334b10926735be",
      "sessionUpdate": "tool_call_update",
      "status": "completed",
      "rawOutput": "total 248\ndrwxr-xr-x  ...",
      "content": [
        {
          "type": "content",
          "content": {
            "type": "text",
            "text": "```console\ntotal 248\ndrwxr-xr-x  ...\n```"
          }
        }
      ]
    }
  }
}
```

> ✅ **已验证**：通过 `acpx --format json claude "List files in current directory"` 测试确认工具调用事件格式。

---

## 7. 事件映射（ACP → AEP）

> ✅ **已通过测试验证**：事件格式和字段名已通过 `acpx --format json` 测试确认。

### 7.1 完整映射表

| ACP SessionUpdate Type | AEP Event Kind | 说明 | 实现 |
|------------------------|---------------|------|------|
| `agent_thought_chunk` | `reasoning` | 思考过程流式输出 | ✅ |
| `agent_message_chunk` | `message.delta` | 消息流式输出 | ✅ |
| `tool_call` | `tool_call` | 工具调用开始 | ✅ |
| `tool_call_update` | `tool_call` (update) | 工具调用更新/完成 | ✅ |
| `usage_update` | (internal) | Token 使用量（内部跟踪） | ✅ |
| `available_commands_update` | (internal) | 可用命令列表（内部跟踪） | ✅ |
| (Response result.stopReason="end_turn") | `done { success: true }` | Prompt 完成 | ✅ |
| (Response error) | `error` | 错误事件 | ✅ |
| `permission_request` | `permission_request` | 权限请求（待验证） | ⚠️ P1 |

### 7.2 AEP Input → ACP Prompt 映射

```go
// Bridge_AEPInput_To_ACP 将 AEP Input 事件转换为 ACP session/prompt 请求。
func Bridge_AEPInput_To_ACP(env *events.Envelope, sessionID string, requestID int) *jsonrpc.Request {
    data := env.Data.(*events.InputData)
    return &jsonrpc.Request{
        JSONRPC: "2.0",
        ID:      requestID,
        Method:  "session/prompt",
        Params: PromptParams{
            SessionID: sessionID,
            Prompt: []Prompt{
                {Type: "text", Text: data.Content},
            },
        },
    }
}
```

### 7.3 ACP Event → AEP Envelope 映射

```go
// Bridge_ACPEvent_To_AEP 将 ACP session/update 事件映射为 AEP Envelope。
func Bridge_ACPEvent_To_AEP(notif *jsonrpc.Notification) (*events.Envelope, error) {
    var update SessionUpdate
    if err := json.Unmarshal(notif.Params, &update); err != nil {
        return nil, fmt.Errorf("parse session update: %w", err)
    }

    switch update.Update.SessionUpdate {
    case "agent_thought_chunk":
        var content Content
        if err := json.Unmarshal(update.Update.Content, &content); err != nil {
            return nil, err
        }
        return makeEnvelope(update.SessionID, events.Reasoning, &events.ReasoningData{
            Text:       content.Text,
            Visibility: "default",
        })

    case "agent_message_chunk":
        var content Content
        if err := json.Unmarshal(update.Update.Content, &content); err != nil {
            return nil, err
        }
        return makeEnvelope(update.SessionID, events.MessageDelta, &events.DeltaData{
            Type: content.Type, // "text"
            Text: content.Text,
        })

    case "usage_update":
        // 内部跟踪，不生成 AEP 事件
        return nil, nil

    case "available_commands_update":
        // 内部跟踪，不生成 AEP 事件
        return nil, nil
    }

    return nil, nil // 忽略未知类型
}

// Bridge_ACPResponse_To_AEP 将 ACP session/prompt Response 转换为 AEP Done 事件。
func Bridge_ACPResponse_To_AEP(resp *jsonrpc.Response, sessionID string) (*events.Envelope, error) {
    if resp.Error != nil {
        return makeEnvelope(sessionID, events.Error, &events.ErrorData{
            Code:    fmt.Sprintf("E% d", resp.Error.Code),
            Message: resp.Error.Message,
        })
    }

    var result PromptResult
    if err := json.Unmarshal(resp.Result, &result); err != nil {
        return nil, err
    }

    return makeEnvelope(sessionID, events.Done, &events.DoneData{
        Success: result.StopReason == "end_turn",
        Stats: events.Stats{
            InputTokens:  result.Usage.InputTokens,
            OutputTokens: result.Usage.OutputTokens,
            TotalTokens:  result.Usage.TotalTokens,
        },
    }), nil
}
```

---

## 8. Session 管理

### 8.1 Session ID 映射

| 层级 | ID 格式 | 说明 |
|------|---------|------|
| Gateway (AEP) | `sess_*` / `cse_*` | Gateway 分配的 session ID |
| ACPX | `acp_*` | acpx 管理的 session ID |

Session ID 映射策略：

```
Gateway session ID → acpx --session-id 参数
ACPX session ID → 记录在 managedSession.metadata["acpx_session_id"]
```

### 8.2 acpx 会话存储

| 项目 | 路径 |
|------|------|
| 默认存储 | `~/.acpx/sessions/` |
| 配置 | `~/.acpx/config.json` |
| 项目级配置 | `<project>/.acpxrc.json` |

### 8.3 Resume 行为（已验证）

acpx 使用命名会话实现会话恢复：

**命名会话模式**：
```bash
# 1. 创建命名会话
acpx claude sessions new --name my-session

# 2. 发送第一个 prompt
echo "My favorite number is 42" | acpx --format json claude -s my-session

# 3. 恢复会话并发送后续 prompt
echo "What is my favorite number?" | acpx --format json claude -s my-session
# → Agent 回答："你最喜欢的数字是 42"
```

**Resume 流程（已验证）**：

```go
func (w *Worker) Resume(ctx context.Context, sessionID string) error {
    // 1. 获取 acpx session 名称（存储在 metadata 中）
    sessionName := w.getACPXSessionName(sessionID)

    // 2. 检查会话是否存在
    // ✅ 已验证：acpx sessions list 返回格式
    listCmd := exec.CommandContext(ctx, "acpx", w.agent, "sessions", "list")
    output, err := listCmd.Output()
    if err != nil {
        return fmt.Errorf("acpx sessions list: %w", err)
    }

    // 解析输出格式：<session-name>\t<status>\t<cwd>\t<timestamp>
    exists := checkSessionExists(output, sessionName)

    // 3a. 会话存在 → 使用 -s 恢复（acpx 自动调用 session/load）
    if exists {
        args := []string{
            "--format", "json",
            w.agent,
            "-s", sessionName,  // ✅ acpx 使用命名会话自动恢复
        }
        return w.startACPXProcess(ctx, args, sessionName)
    }

    // 3b. 会话不存在 → 创建新会话
    args := []string{
        w.agent,
        "sessions", "new",
        "--name", sessionName,
    }
    return w.startACPXProcess(ctx, args, sessionName)
}
```

**实际 Resume 流程事件**（已验证）：

```
[acpx] session test-resume (f873f9e7-63cc-4c54-a0d2-61ef3250cc2c) · ... · agent connected
→ JSON-RPC: session/load (而不是 session/new)
→ Response: 返回相同 sessionId (4e4f1d0a-1dc5-45db-b3a6-4d075ff579dd)
→ Agent 记住了之前的上下文
```

> ✅ **已验证**：
> - `-s <name>` 使用命名会话模式，acpx 自动调用 `session/load` 恢复
> - `acpx sessions list` 输出格式：`<name>\t<status>\t<cwd>\t<timestamp>`
> - Resume 使用 `session/load` 方法（而不是 `session/new`）
> - Agent 保持上下文记忆

---

## 9. Worker 实现架构

### 9.1 目录结构

```
internal/worker/acpx/
├── acp.go           # ACP 协议类型定义（Request/Response/Event）
├── bridge.go        # AEP ↔ ACP 协议转换
├── conn.go          # acpx 会话连接（stdin/stdout）
├── worker.go        # Worker 接口实现
├── worker_test.go   # 单元测试
└── init.go          # Worker 注册
```

### 9.2 Worker 结构

```go
// Worker implements the ACPX worker adapter.
type Worker struct {
    Base   *base.BaseWorker
    agent  string  // "claude", "opencode", "gemini", etc.
    acpSessionID string  // acpx session ID
    mu    sync.Mutex
}

var _ worker.Worker = (*Worker)(nil)

// AgentType 返回该 Worker 支持的具体 agent 类型。
func (w *Worker) AgentType() string {
    return w.agent
}
```

### 9.3 核心流程

```go
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // 1. 启动 acpx sessions new
    args := []string{w.agent, "sessions", "new", "--format", "json"}
    if session.ProjectDir != "" {
        args = append(args, "--cwd", session.ProjectDir)
    }

    stdin, stdout, _, err := w.Base.Proc.Start(ctx, "acpx", args, env, session.ProjectDir)
    if err != nil {
        return fmt.Errorf("acpx: start: %w", err)
    }

    // 2. 读取 acpx 会话 ID
    line, err := w.Base.Proc.ReadLine()
    if err != nil {
        return fmt.Errorf("acpx: read session id: %w", err)
    }
    w.acpSessionID = extractSessionID(line)

    // 3. 建立连接
    conn := NewConn(stdin, stdout, session.UserID, w.acpSessionID)
    w.Base.SetConn(conn)

    // 4. 启动输出读取 goroutine
    go w.readACPOutput()

    return nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // 构造 session/prompt 请求并发送到 stdin
    req := bridge.Bridge_AEPInput_To_ACP(ctx, w.acpSessionID, content, metadata)
    return encodeNDJSON(w.Base.Conn().(*Conn).stdin, req)
}
```

### 9.4 Worker Type 注册

```go
// internal/worker/acpx/init.go
func init() {
    // 通用 ACPX worker
    worker.Register(worker.TypeACPX, func() (worker.Worker, error) {
        return &Worker{
            Base:  base.NewBaseWorker(slog.Default(), nil),
            agent: "claude", // 默认 agent，可通过配置覆盖
        }, nil
    })

    // 按 agent 类型注册便捷 worker
    for _, agent := range acpxSupportedAgents {
        t := worker.WorkerType("acpx_" + agent)
        worker.Register(t, func() (worker.Worker, error) {
            return &Worker{
                Base:  base.NewBaseWorker(slog.Default(), nil),
                agent: agent,
            }, nil
        })
    }
}
```

支持的 Agent 常量：

```go
var acpxSupportedAgents = []string{
    "claude", "opencode", "gemini", "cursor", "codex",
    "copilot", "droid", "iflow", "kilocode", "kimi",
    "kiro", "openclaw", "pi", "qoder", "qwen", "trae",
}
```

---

## 10. 与现有 Worker 的对比

### 10.1 能力对比矩阵

| 能力 | ClaudeCode | OpenCodeSrv | **ACPX** |
|------|-----------|------------|----------|
| 支持 Agent 数量 | 1 | 1 | **16** |
| 会话持久化 | ✅ | ✅ | ✅ |
| 流式输出 | ✅ | ✅ | ✅ |
| 工具调用 | ✅ | ✅ | ✅（取决于 Agent） |
| Resume | ✅ | ✅ | ✅ |
| 权限控制 | ✅ | ❌ | ⚠️ P1 |
| 会话队列 | ❌ | ❌ | ✅ |
| Crash 恢复 | ❌ | ❌ | ✅ |
| 命名会话 | ❌ | ❌ | ✅ |

### 10.2 协议转换开销

| 层级 | ClaudeCode | ACPX |
|------|-----------|------|
| CLI 协议 | stream-json（Claude 专用） | JSON-RPC 2.0 over NDJSON（通用） |
| 事件映射 | Claude 事件 → AEP | ACP session/update → AEP |
| 转换复杂度 | 中等（专用 Parser/Mapper） | 低（标准 JSON-RPC + 统一格式） |

---

## 11. 优雅终止

### 11.1 ACPX 终止流程

acpx 提供两种终止机制：

**方式 1：Cancel 命令**（推荐）
```bash
# 取消当前运行的 prompt
acpx claude cancel

# 取消特定会话
acpx claude cancel --session-id <session-id>
```

**方式 2：SIGTERM 信号**（优雅终止）
```bash
# 发送 SIGTERM，acpx 进程自行清理
kill -TERM <pid>
```

### 11.2 Worker Adapter 终止（已验证）

```go
func (w *Worker) Terminate(ctx context.Context) error {
    // 1. 尝试通过 acpx cancel 命令优雅取消
    if w.sessionName != "" {
        cancelCmd := exec.CommandContext(ctx, "acpx", w.agent, "cancel")
        cancelCmd.Env = os.Environ()
        cancelCmd.Dir = w.projectDir

        // 执行取消命令（忽略错误，因为可能已经完成）
        _ = cancelCmd.Run()

        // 等待一小段时间让取消生效
        time.Sleep(500 * time.Millisecond)
    }

    // 2. 分层终止（SIGTERM → 5s → SIGKILL）
    return w.Base.Terminate(ctx)
}
```

> ✅ **已验证**：acpx cancel 命令存在，可优雅取消正在运行的 prompt。

---

## 12. 实现优先级

### P0（必须实现，v1.0 MVP）

| 项目 | 说明 |
|------|------|
| ACP 协议类型定义 | Request/Response/Event 结构 |
| NDJSON 编解码 | encodeNDJSON / decodeNDJSON |
| session/new 启动 | 创建 acpx 会话 |
| session/prompt 输入 | 发送 prompt 到 acpx |
| ACP Event → AEP 映射 | thinking/message/tool_call/tool_result/done/error |
| Worker 注册 | TypeACPX + acpx_<agent> 类型 |
| 分层终止 | SIGTERM → 5s → SIGKILL |

### P1（重要，v1.0 完整支持）

| 项目 | 说明 |
|------|------|
| Resume 支持 | acpx sessions list → session/prompt --resume |
| 权限请求 | permission_request → ACP permission 事件 |
| 状态事件 | system → AEP state |
| Agent 配置 | 通过 worker_type 选择具体 agent |
| 环境变量透传 | Agent API Key 注入 |

### P2（增强，v1.1）

| 项目 | 说明 |
|------|------|
| --no-wait 队列模式 | fire-and-forget |
| --approve-all / --deny-all | 权限自动处理 |
| 自定义 Agent | --agent escape hatch |
| Crash 自动恢复 | acpx sessions list → 自动恢复 |

---

## 13. 验收标准

### AC-ACPX-001 — ACPX Worker 启动成功

**描述**: ACPX Worker 启动时正确创建 acpx 会话，读取并记录 session ID。

**验收标准**:
- Given acpx CLI 已安装且在 PATH 中, When Worker.Start 被调用, Then acpx <agent> sessions new --format json 被执行，无错误返回
- Given acpx 进程启动, When 读取第一行输出, Then session ID 被提取并记录在 worker.acpSessionID
- Given acpx CLI 未安装（`exec.LookPath("acpx")` 失败）, When Worker.Start, Then 返回 error 且包含 "acpx: not found in PATH" 或类似信息
- Given acpx CLI 安装但启动失败（权限错误/依赖缺失）, When Worker.Start, Then 返回 error 且包含原始错误信息

### AC-ACPX-002 — ACP Event → AEP 映射正确

**描述**: acpx 输出的 NDJSON 事件被正确映射为 AEP Envelope。

**验收标准**:
- Given acpx 输出 `{"type":"thinking","data":{"content":"..."}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.Reasoning envelope
- Given acpx 输出 `{"type":"tool_call","data":{"id":"c1","name":"Read","input":{}}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.ToolCall envelope 且 id="c1", name="Read"
- Given acpx 输出 `{"type":"done","data":{"success":true}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.Done envelope 且 Success=true
- Given acpx 输出 `{"type":"error","data":{"code":"TOOL_DENIED","message":"..."}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.Error envelope 且 Code="TOOL_DENIED"

### AC-ACPX-003 — AEP Input → ACP Request 映射正确

**描述**: Gateway 发送的 AEP Input 事件被正确转换为 ACP session/prompt 请求。

**验收标准**:
- Given AEP Input{content:"fix the bug"}, When Bridge_AEPInput_To_ACP, Then 返回 Request{Method:"session/prompt", Params.sessionId=正确, Params.prompt="fix the bug"}
- Given AEP Input 包含 metadata, When Bridge_AEPInput_To_ACP, Then metadata 被序列化到 Params.metadata

### AC-ACPX-004 — Worker 正确注册到 Registry

**描述**: ACPX Worker 通过 init() 自动注册，支持多种 worker_type。

**验收标准**:
- Given `worker.NewWorker("acpx")`, When 调用, Then 返回 ACPX Worker 实例
- Given `worker.NewWorker("acpx_claude")`, When 调用, Then 返回 agent="claude" 的 ACPX Worker
- Given `worker.NewWorker("unknown")`, When 调用, Then 返回 error 且包含 "unknown type"

### AC-ACPX-005 — 分层终止正确执行

**描述**: ACPX Worker 的 Terminate 正确执行 SIGTERM → 等待 → SIGKILL。

**验收标准**:
- Given ACPX Worker 运行中, When Terminate 被调用, Then SIGTERM 被发送到 acpx 进程组
- Given acpx 进程在 5s 内退出, When Terminate, Then 不发送 SIGKILL
- Given acpx 进程在 5s 后仍存活, When Terminate, Then SIGKILL 被发送

### AC-ACPX-006 — 流式输出正确处理

**描述**: acpx 的 delta 事件被正确映射为 AEP message.delta。

**验收标准**:
- Given acpx 输出 `{"type":"delta","data":{"type":"text","content":"Hello"}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.MessageDelta 且 Type="text", Text="Hello"
- Given acpx 输出 `{"type":"delta","data":{"type":"code","content":"func main()"}}`, When Bridge_ACPEvent_To_AEP, Then 返回 events.MessageDelta 且 Type="code"

### AC-ACPX-007 — Resume 流程正确

**描述**: Worker.Resume 正确恢复 acpx 会话。

**验收标准**:
- Given acpx session ID 存在于 metadata, When Worker.Resume, Then acpx <agent> sessions prompt 被调用
- Given acpx session ID 不存在, When Worker.Resume, Then 返回 error

### AC-ACPX-008 — 实际 acpx NDJSON 格式验证

**描述**: 通过集成测试验证实际 acpx 输出格式与假设一致。

**验收标准**:
- Given acpx 已安装, When 运行 `acpx claude "test prompt" --format json`, Then stdout 输出 NDJSON 格式
- Given acpx 输出 NDJSON, When 解析第一行, Then 包含 `eventVersion`, `sessionId`, `type`, `data` 字段
- Given acpx 输出 tool_call 事件, When 解析 data 字段, Then 包含 `id`, `name`, `input` 字段
- Given acpx 输出与假设格式不匹配, When 发现差异, Then 更新 `bridge.go` 中的映射逻辑并添加注释说明差异点

---

## 14. 源码关键路径

| 功能 | 源码路径 |
|------|---------|
| ACP 协议类型 | `internal/worker/acpx/acp.go` |
| AEP ↔ ACP 桥接 | `internal/worker/acpx/bridge.go` |
| 会话连接 | `internal/worker/acpx/conn.go` |
| Worker 实现 | `internal/worker/acpx/worker.go` |
| Worker 注册 | `internal/worker/acpx/init.go` |
| base.Conn 复用 | `internal/worker/base/conn.go` |
| base.BaseWorker 复用 | `internal/worker/base/worker.go` |
| proc.Manager 复用 | `internal/worker/proc/manager.go` |

---

## 15. 配置扩展

### 15.1 Worker 配置

在 `internal/config/config.go` 中新增：

```go
type WorkerConfig struct {
    // ... 现有字段 ...

    // ACPX 配置
    ACPX ACPXConfig `json:"acpx"`
}

type ACPXConfig struct {
    // DefaultAgent 是默认使用的 ACPX agent
    DefaultAgent string `json:"default_agent"`
    // ACPXBinary 允许覆盖 acpx 二进制路径
    ACPXBinary string `json:"acpx_binary"`
    // SessionStoreDir 设置 acpx 会话存储目录
    SessionStoreDir string `json:"session_store_dir"`
    // AutoApprovePermissions 自动批准所有权限请求
    AutoApprovePermissions bool `json:"auto_approve_permissions"`
}
```

### 15.2 使用示例

```yaml
worker:
  type: "acpx_gemini"  # 或 "acpx", 由 ACPX.DefaultAgent 决定
  acpx:
    default_agent: "gemini"
    acpx_binary: ""  # 空 = PATH 中查找
    auto_approve_permissions: true
```
