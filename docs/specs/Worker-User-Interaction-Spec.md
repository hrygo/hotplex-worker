---
type: spec
tags: [project/HotPlex, worker/claudecode, worker/opencode-server, messaging, gateway]
date: 2026-04-19
status: implemented
progress: 95
priority: high
estimated_hours: 24
audited: true  # 三源码交叉审查通过 2026-04-19
last_updated: 2026-04-23
note: "有关 Session Stats 的详细设计请参考实际 Session Store 实现；有关 Worker Session Control 的最新状态请参考 Worker-Session-Control-Spec.md（status: in-progress）"
---

# Worker User Interaction Spec

## 概述

HotPlex Worker Gateway 作为 AI Coding Agent 与用户（通过 Slack/Feishu）之间的代理层，
需要在 Agent 主动向用户发起询问或请求授权时，正确地**转发请求**并**回传用户响应**。

本文档定义两种 Worker 类型的用户交互集成方案：

| Worker 类型 | 协议 | 交互类型 |
|-------------|------|---------|
| Claude Code | stream-json stdio (`control_request/response`) | 权限请求 + 问题询问 + MCP Elicitation |
| OpenCode Server | HTTP REST + SSE bus events | 权限请求 + 问题询问 |

### 动机

| 现状 | 问题 |
|------|------|
| Claude Code adapter 已实现 `can_use_tool` → AEP `PermissionRequest` 转发 | Messaging 层无法展示权限 UI，用户无法审批 |
| OpenCode Server adapter 用 `aep.DecodeLine()` 解析 SSE | 非 AEP 格式的 `permission.asked`/`question.asked` 事件被静默丢弃 |
| 用户无法感知 Agent 正在等待输入 | 交互超时后 Agent 行为不可预测 |

### 范围

```
┌──────────┐    ┌──────────────────────────────┐    ┌──────────┐
│  Worker  │    │     HotPlex Worker Gateway    │    │ Platform │
│ (CC/OC)  │    │                              │    │(Slack/飞书)│
│          │◄──►│ 1. 解析交互请求               │    │          │
│          │    │ 2. AEP 事件转发               │◄──►│ 3. 展示 UI│
│          │    │ 4. 路由响应回 Worker           │    │ 5. 收集答案│
└──────────┘    └──────────────────────────────┘    └──────────┘
```

- **In Scope**: Worker → Gateway → Platform 的请求/响应链路
- **Out of Scope**: Platform 端具体 UI 渲染细节（各 adapter spec 自行定义）

### 相关文档

- 会话统计展示: `docs/specs/Session-Stats-Spec.md` — done 事件 stats footer（与本 spec 正交，见下方说明）
- Worker 会话控制: `docs/specs/Worker-Session-Control-Spec.md` — stdio 直达命令（context 查询等）

### 与 Session-Stats-Spec 的关系

本 spec（User Interaction）与 Session-Stats-Spec **完全正交**，解决不同问题：

| 维度 | 本 Spec (User Interaction) | Session-Stats-Spec |
|------|----------------------------|-------------------|
| 核心问题 | Agent **阻塞等待**用户输入（审批/问答） | **只读展示**资源消耗信息 |
| 交互方向 | 双向（Agent → 用户 → Agent） | 单向（Agent → 用户） |
| 触发时机 | 工具执行前 / Agent 提问时 | 每轮 done 事件时 |
| 用户行为 | 需主动操作（点击按钮/输入） | 被动接收，无需操作 |
| 不实施的后果 | Agent 无法执行需授权的工具，请求被丢弃 | 用户无法感知 token/费用消耗 |

实施顺序：**本 spec 优先**（功能性阻塞），Session-Stats-Spec 随后（体验增强）。两者改不同文件/不同逻辑路径，代码冲突风险极低。

---

## 交互类型分类

### Type A: 权限请求 (Permission Request)

Agent 执行工具前请求用户授权（文件写入、命令执行等）。

**特征**：阻塞式 — Agent 暂停执行，等待用户 allow/deny 后继续。

### Type B: 问题询问 (Question)

Agent 主动向用户提问以获取信息或确认意图。

**特征**：阻塞式 — Agent 暂停执行，等待用户回答后继续。

### Type C: MCP Elicitation (仅 Claude Code)

MCP Server 通过 Claude Code 协议请求用户输入表单或跳转 URL。

**特征**：阻塞式 — Agent 暂停执行，用户 accept/decline/cancel。

---

## 1. Claude Code 交互协议

### 1.1 协议背景

Claude Code `--output-format stream-json` stdio 模式下，所有用户交互统一通过
`control_request` / `control_response` NDJSON 消息交换。

**Claude Code 源码参考**:
- AskUserQuestion Tool: `src/tools/AskUserQuestionTool/AskUserQuestionTool.tsx`
- StructuredIO: `src/cli/structuredIO.ts`
- Control Schemas: `src/entrypoints/sdk/controlSchemas.ts`
- Permission Prompt Result: `src/utils/permissions/PermissionPromptToolResultSchema.ts`

**HotPlex 源码参考**:
- Parser: `internal/worker/claudecode/parser.go:299-326`
- Control Handler: `internal/worker/claudecode/control.go`
- Worker Event Dispatch: `internal/worker/claudecode/worker.go:397-431`
- Input Method: `internal/worker/claudecode/worker.go:230-251`
- Types: `internal/worker/claudecode/types.go:68-76`

### 1.2 请求格式（Claude Code stdout → HotPlex）

Claude Code 在 stdout 输出 `control_request` 消息，HotPlex parser 统一解析为 `ControlRequestPayload`，
再由 `worker.go:readOutput()` 按 `Subtype` 分发。

#### 通用解析层 (`parser.go:299-326`)

所有 `control_request` 消息统一解析为 `ControlRequestPayload`（`types.go:68-76`）：

```go
type ControlRequestPayload struct {
    RequestID string          `json:"request_id,omitempty"` // 来自外层 SDKMessage.RequestID
    Subtype   string          `json:"subtype"`
    ToolName  string          `json:"tool_name,omitempty"`
    Input     json.RawMessage `json:"input,omitempty"`
}
```

解析后作为 `EventControl` 事件发送，由 `worker.go:402` 按 `Subtype` 字段分发。

#### Type A: 权限请求

```jsonc
// Claude Code stdout
{
  "type": "control_request",
  "request_id": "req_abc123",
  "response": {                              // 注意：CC 内部字段名是 "response" 不是 "request"
    "subtype": "can_use_tool",
    "tool_name": "Write",
    "input": {
      "file_path": "/src/main.go",
      "content": "package main..."
    },
    "tool_use_id": "toolu_xyz789",
    "description": "Write to /src/main.go",
    "permission_suggestions": [],             // 可选：推荐的权限规则
    "blocked_path": "",                      // 可选
    "decision_reason": "",                   // 可选
    "agent_id": ""                           // 可选：team agent 场景
  }
}
```

**HotPlex 转发逻辑** (`worker.go:403-427`):

所有 `can_use_tool` 请求（包括 `AskUserQuestion`）都走同一条路径：
将 `cr.Input` 反序列化为 `map[string]any`，再 marshal 为 `Args []string`，
构造 `PermissionRequestData` 发送到 gateway。

```go
// worker.go:403-427 — 现有实现
case string(ControlCanUseTool):
    var input map[string]any
    if len(cr.Input) > 0 {
        _ = json.Unmarshal(cr.Input, &input)
    }
    args := []string{`{}`}
    if len(input) > 0 {
        if s, err := json.Marshal(input); err == nil {
            args = []string{string(s)}
        }
    }
    env := events.NewEnvelope(..., events.PermissionRequest,
        events.PermissionRequestData{
            ID:          cr.RequestID,
            ToolName:    cr.ToolName,
            Description: cr.ToolName,
            Args:        args,
        })
    w.trySend(env)
```

#### Type B: 问题询问

```jsonc
// Claude Code stdout — AskUserQuestion 也是 can_use_tool 子类型
{
  "type": "control_request",
  "request_id": "req_def456",
  "response": {
    "subtype": "can_use_tool",
    "tool_name": "AskUserQuestion",
    "input": {
      "questions": [
        {
          "question": "使用哪种认证方式？",
          "header": "Auth method",
          "options": [
            { "label": "JWT", "description": "无状态令牌认证" },
            { "label": "Session", "description": "服务端会话认证" }
          ],
          "multiSelect": false
        }
      ],
      "answers": {},                         // 可选：预填充答案
      "annotations": {},                     // 可选：每题注解 {preview, notes}
      "metadata": { "source": "..." }        // 可选：来源标识
    }
  }
}
```

**当前问题**：所有 `can_use_tool` 统一作为 `PermissionRequest` 转发。
需要区分 `tool_name == "AskUserQuestion"` 的情况，改用 `QuestionRequest` 事件类型。

#### Type C: MCP Elicitation

```jsonc
// Claude Code stdout — subtype 直接为 "elicitation"
{
  "type": "control_request",
  "request_id": "req_ghi789",
  "response": {
    "subtype": "elicitation",
    "mcp_server_name": "memory-server",       // 必需：发起请求的 MCP server 名称
    "message": "MCP Server requests your input",
    "mode": "form",                           // 可选："form" | "url"
    "url": "https://...",                     // 可选：mode=url 时的外部表单 URL
    "elicitation_id": "el_xxx",               // 可选：eliciation 唯一标识
    "requested_schema": { ... }               // 可选：JSON Schema 表单定义
  }
}
```

**当前问题**：`parser.go:317` 的 `default` 分支将 `elicitation` 归入 `EventControl`，
但 `control.go:56` 的 `default` 分支仅打 warn 日志不做处理。elicitation 请求被静默丢弃。

### 1.3 响应格式（HotPlex → Claude Code stdin）

HotPlex 通过 `ControlHandler.SendResponse()` 向 Claude Code stdin 写入 NDJSON。

**通用响应结构** (`control.go:67-78`):

```go
type ControlResponse struct {
    Type     string         `json:"type"`              // "control_response"
    Response ResponsePayload `json:"response"`
}

type ResponsePayload struct {
    Subtype   string         `json:"subtype"`           // "success"
    RequestID string         `json:"request_id"`
    Response  map[string]any `json:"response"`
}
```

#### 权限 — 允许

```jsonc
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_abc123",
    "response": {
      "allowed": true,
      "reason": ""
    }
  }
}
```

**Claude Code 完整 schema**（HotPlex 当前只发送 `allowed` + `reason`）：
- `behavior`: `"allow"` | `"deny"` — CC 的标准权限字段
- `updatedInput`: `map` — 可选：修改后的工具输入
- `updatedPermissions`: `[]` — 可选：持久权限规则
- `toolUseID`: `string` — 可选
- `decisionClassification`: `"user_temporary"` | `"user_permanent"` | `"user_reject"` — 可选

> **注意**：HotPlex 当前使用简化的 `{"allowed": bool, "reason": string}` 格式
> （`control.go:99-104`），而非 Claude Code 的标准 `behavior` 格式。
> 这是因为 `SendPermissionResponse` 直接构造响应体，未走 CC 的 PermissionPrompt 体系。
> 当前格式在实践中被 Claude Code 接受。

#### 权限 — 拒绝

```jsonc
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_abc123",
    "response": {
      "allowed": false,
      "reason": "User denied"
    }
  }
}
```

**Claude Code 完整 deny schema**:
- `message`: `string` — 拒绝原因（必需）
- `interrupt`: `bool` — 可选：是否中断执行

#### 问题 — 带回答

```jsonc
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_def456",
    "response": {
      "behavior": "allow",
      "updatedInput": {
        "questions": [...],
        "answers": {
          "使用哪种认证方式？": "JWT"
        },
        "annotations": {}                    // 可选
      }
    }
  }
}
```

#### MCP Elicitation 响应

```jsonc
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_ghi789",
    "response": {
      "action": "accept",                   // "accept" | "decline" | "cancel"
      "content": { "field1": "value" }      // 可选：accept 时的表单数据
    }
  }
}
```

### 1.4 取消机制

```jsonc
// stdin: control_cancel_request
{
  "type": "control_cancel_request",
  "request_id": "req_abc123"
}
```

### 1.5 现有实现状态

| 组件 | 状态 | 文件 | 说明 |
|------|------|------|------|
| stdout `control_request` 解析 | ✅ 已实现 | `claudecode/parser.go:299-326` | 统一解析为 `ControlRequestPayload` |
| `can_use_tool` → AEP `PermissionRequest` | ✅ 已实现 | `claudecode/worker.go:528-576` | AskUserQuestion 区分为 `QuestionRequest`，其余走 `PermissionRequest` |
| 自动审批 (`set_*`, `mcp_*`) | ✅ 已实现 | `claudecode/control.go:44-60` | `sendAutoSuccess` |
| AEP `PermissionResponse` → stdin | ✅ 已实现 | `claudecode/control.go:106-111` | `SendPermissionResponse` |
| Gateway 路由全部交互事件 | ✅ 已实现 | `gateway/handler.go:62-66` | `passthroughToSession` 含全部 6 种事件 |
| Worker.Input 交互响应分发 | ✅ 已实现 | `claudecode/worker.go:248-284` | permission/question/elicitation 三种响应 |
| Messaging 层展示/收集 | ✅ 已实现 | `messaging/{slack,feishu}/interaction.go` | Slack Interactive Message + Feishu Interactive Card |
| `AskUserQuestion` 问题区分转发 | ✅ 已实现 | `claudecode/worker.go:529-550` | 区分为 `QuestionRequest` 事件 |
| 问题响应回传 | ✅ 已实现 | `claudecode/control.go:114-121` | `SendQuestionResponse` |
| MCP Elicitation 处理 | ✅ 已实现 | `claudecode/worker.go:577-610` | 解析 elicitation 字段，转发为 `ElicitationRequest` |
| Elicitation 响应回传 | ✅ 已实现 | `claudecode/control.go:124-129` | `SendElicitationResponse` |
| PermissionRequestData.InputRaw | ✅ 已实现 | `events/events.go:214-219` | 原始工具输入（结构化） |

### 1.6 关键代码路径（已验证）

**请求路径**:
```
CC stdout → Parser.ParseLine() → parseControlRequest() → EventControl{ControlRequestPayload}
→ readOutput() → Subtype=="can_use_tool" → PermissionRequestData{ID,ToolName,Description,Args}
→ trySend(env) → RecvCh → Bridge.forwardEvents() → Hub.SendToSession() → PlatformConn.WriteCtx()
```

**响应路径**:
```
PlatformConn.WriteCtx(PermissionResponse) → Hub → Bridge → Worker.Input(content, metadata)
→ metadata["permission_response"] → ControlHandler.SendPermissionResponse(reqID, allowed, reason)
→ sendResponse() → stdin write
```

**Worker.Input 实际签名** (`worker.go:230`):
```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error
```

**权限响应通过 metadata 传递** (`worker.go:237-250`):
```go
if metadata != nil {
    if permResp, ok := metadata["permission_response"].(map[string]any); ok {
        reqID, _ := permResp["request_id"].(string)
        allowed, _ := permResp["allowed"].(bool)
        reason, _ := permResp["reason"].(string)
        return w.control.SendPermissionResponse(reqID, allowed, reason)
    }
}
```

---

## 2. OpenCode Server 交互协议

### 2.1 协议背景

OpenCode Server 模式通过 HTTP REST API + SSE 事件流处理用户交互。
权限和问题各自有独立的 Service（Effect Deferred 实现）。

**OpenCode 源码参考**:
- Permission Service: `packages/opencode/src/permission/index.ts`
- Question Service: `packages/opencode/src/question/index.ts`
- Question Tool: `packages/opencode/src/tool/question.ts`
- Permission Routes: `packages/opencode/src/server/routes/instance/permission.ts`
- Question Routes: `packages/opencode/src/server/routes/instance/question.ts`
- SSE Event Route: `packages/opencode/src/server/routes/instance/event.ts`
- Instance Routes: `packages/opencode/src/server/routes/instance/index.ts`

**HotPlex 源码参考**:
- SSE Reader: `internal/worker/opencodeserver/worker.go:594-680`
- Input Method: `internal/worker/opencodeserver/worker.go:282-311`

### 2.2 关键架构问题：SSE 协议不匹配

**OpenCode Server 原生 SSE 输出** (`event.ts:69-81`):

OpenCode 将内部 Bus 事件直接序列化到 SSE 流：

```typescript
const unsub = Bus.subscribeAll((event) => {
  q.push(JSON.stringify(event))
})
```

输出的是原生 Bus 事件，格式为：
```jsonc
{"type": "permission.asked", "properties": {...}}
```

**HotPlex 当前 SSE 解析** (`worker.go:642-655`):

```go
// 尝试用 AEP 解码器解析 SSE data
env, err := aep.DecodeLine([]byte(data))
if err != nil {
    w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
    continue // ← 非 AEP 格式的交互事件在这里被静默丢弃
}
```

**问题**：OpenCode 的 `permission.asked` / `question.asked` 事件不是 AEP envelope 格式，
`aep.DecodeLine()` 会失败并 warn，然后被 `continue` 跳过。

**解决方案**：需要在 AEP 解码失败时，尝试解析为 OpenCode 原生 bus 事件。

### 2.3 交互检测（OpenCode Bus 事件格式）

#### Type A: 权限请求事件

```jsonc
// SSE data line（原生 Bus 事件，非 AEP 格式）
{
  "type": "permission.asked",
  "properties": {
    "id": "perm_abc123",                     // PermissionID — 回复时用此 ID
    "sessionID": "sess_xyz",
    "permission": "file_write",              // 权限类型标识
    "patterns": ["src/**/*.go"],             // 文件 glob 模式
    "metadata": {                            // 工具详情（用于展示）
      "tool": "write",
      "file_path": "src/main.go",
      "content": "package main..."
    },
    "tool": {                                // 触发权限的消息/工具信息
      "messageID": "msg_123",
      "callID": "call_456"
    },
    "always": []                             // 可用的持久授权选项
  }
}
```

#### Type B: 问题请求事件

```jsonc
// SSE data line
{
  "type": "question.asked",
  "properties": {
    "id": "q_def456",                       // QuestionID
    "sessionID": "sess_xyz",
    "questions": [
      {
        "question": "使用哪种数据库？",
        "options": [
          { "label": "PostgreSQL", "description": "关系型数据库" },
          { "label": "MongoDB", "description": "文档型数据库" }
        ],
        "multiSelect": false
      }
    ]
  }
}
```

### 2.4 响应格式（HotPlex → OpenCode Server）

**REST 路由注册** (`instance/index.ts:58-59`):
```typescript
.route("/permission", PermissionRoutes())
.route("/question", QuestionRoutes())
```

**注意**：REST 路径不包含 `/session/{sessionID}/` 前缀。

#### 权限回复 (`permission.ts:13`)

```
POST /permission/{requestID}/reply
Content-Type: application/json

{
  "reply": "once",                           // "once" | "always" | "reject"
  "message": "optional feedback"             // 可选
}
```

| reply 值 | 含义 |
|-----------|------|
| `once` | 本次允许，下次仍需询问 |
| `always` | 持久允许该模式（写入规则） |
| `reject` | 拒绝本次请求 |

#### 问题回复 (`question.ts:43`)

```
POST /question/{requestID}/reply
Content-Type: application/json

{
  "answers": [["PostgreSQL"], ["optionA", "optionB"]]  // 每题一个数组
}
```

- `answers` 为二维数组，`answers[i]` 对应第 i 个问题的选中项
- `multiSelect: true` 时可包含多个选项
- 支持自定义文本作为答案

#### 问题拒绝 (`question.ts:79`)

```
POST /question/{requestID}/reject
```

#### 已弃用端点（仍存在但不应使用）

```
POST /session/{sessionID}/permissions/{permissionID}
Content-Type: application/json
deprecated: true

{ "response": "once" | "always" | "reject" }
```

### 2.5 事件确认

OpenCode 在收到回复后广播确认事件到 SSE：

```jsonc
// permission.replied
{ "type": "permission.replied", "properties": { "sessionID": "...", "requestID": "...", "reply": "once" } }

// question.replied
{ "type": "question.replied", "properties": { "id": "...", "answers": [...] } }

// question.rejected
{ "type": "question.rejected", "properties": { "id": "..." } }
```

### 2.6 现有实现状态

| 组件 | 状态 | 文件 | 说明 |
|------|------|------|------|
| SSE 监听基础 | ✅ 已实现 | `opencodeserver/worker.go:594` | `readSSE()` |
| SSE AEP 解码 | ✅ 已实现 | `opencodeserver/worker.go:717-718` | `aep.DecodeLine()` |
| SSE 双路径解析（AEP + bus 事件） | ✅ 已实现 | `opencodeserver/worker.go:720-735` | AEP 失败后尝试 bus 事件解析 |
| `permission.asked` 解析 | ✅ 已实现 | `opencodeserver/worker.go:776-799` | `handlePermissionAsked`，含 `InputRaw` |
| `question.asked` 解析 | ✅ 已实现 | `opencodeserver/worker.go:803-822` | `handleQuestionAsked` |
| HTTP POST 权限回复 | ✅ 已实现 | `opencodeserver/worker.go:303-312` | `POST /permission/{id}/reply` |
| HTTP POST 问题回复 | ✅ 已实现 | `opencodeserver/worker.go:313-318` | `POST /question/{id}/reply`，含 `answersToArrays` |
| Worker.Input 交互响应 | ✅ 已实现 | `opencodeserver/worker.go:302-319` | permission_response + question_response |

---

## 3. AEP 事件模型

### 3.1 现有事件类型（`pkg/events/events.go`）

```go
// 已存在（events.go:30-31）
PermissionRequest  Kind = "permission_request"
PermissionResponse Kind = "permission_response"

// 已存在（events.go:206-218）
type PermissionRequestData struct {
    ID          string   `json:"id"`
    ToolName    string   `json:"tool_name"`
    Description string   `json:"description,omitempty"`
    Args        []string `json:"args,omitempty"`
}

type PermissionResponseData struct {
    ID      string `json:"id"`
    Allowed bool   `json:"allowed"`
    Reason  string `json:"reason,omitempty"`
}
```

### 3.2 需新增的事件类型

```go
// pkg/events/events.go — 新增
QuestionRequest    Kind = "question_request"
QuestionResponse   Kind = "question_response"
ElicitationRequest  Kind = "elicitation_request"
ElicitationResponse Kind = "elicitation_response"
```

### 3.3 新增数据结构

#### QuestionRequest

```go
type QuestionOption struct {
    Label       string `json:"label"`
    Description string `json:"description,omitempty"`
    Preview     string `json:"preview,omitempty"`
}

type Question struct {
    Question    string           `json:"question"`
    Header      string           `json:"header"`
    Options     []QuestionOption `json:"options"`
    MultiSelect bool             `json:"multi_select"`
}

type QuestionRequestData struct {
    ID       string     `json:"id"`
    ToolName string     `json:"tool_name,omitempty"` // "AskUserQuestion" | "question"
    Questions []Question `json:"questions"`
}
```

#### QuestionResponse

```go
type QuestionResponseData struct {
    ID     string            `json:"id"`
    Answers map[string]string `json:"answers"` // question text → selected label
}
```

#### ElicitationRequest

```go
type ElicitationRequestData struct {
    ID              string         `json:"id"`
    MCPServerName   string         `json:"mcp_server_name"`
    Message         string         `json:"message"`
    Mode            string         `json:"mode,omitempty"`            // "form" | "url"
    URL             string         `json:"url,omitempty"`
    ElicitationID   string         `json:"elicitation_id,omitempty"`
    RequestedSchema map[string]any `json:"requested_schema,omitempty"`
}
```

#### ElicitationResponse

```go
type ElicitationResponseData struct {
    ID      string         `json:"id"`
    Action  string         `json:"action"`   // "accept" | "decline" | "cancel"
    Content map[string]any `json:"content,omitempty"`
}
```

### 3.4 PermissionRequestData 扩展

现有结构体不含工具输入详情（`Args` 为序列化后的 JSON string）。
考虑增加可选字段以支持更丰富的展示：

```go
type PermissionRequestData struct {
    ID          string   `json:"id"`
    ToolName    string   `json:"tool_name"`
    Description string   `json:"description,omitempty"`
    Args        []string `json:"args,omitempty"`
    // 新增可选字段
    InputRaw    json.RawMessage `json:"input_raw,omitempty"` // 原始工具输入（结构化）
}
```

### 3.5 Gateway 路由扩展

在 `internal/gateway/handler.go` 中注册新事件类型：

```go
case events.QuestionRequest, events.QuestionResponse,
     events.ElicitationRequest, events.ElicitationResponse:
    return h.passthroughToSession(ctx, env)
```

---

## 4. Worker Adapter 集成

### 4.1 Claude Code Adapter

**现有实现基础**: `can_use_tool` 权限请求已完整实现。
需扩展以区分 `AskUserQuestion` 并支持 `Elicitation`。

#### 4.1.1 事件分发扩展 (`worker.go`)

现有分发逻辑在 `worker.go:402-431`。需在 `case ControlCanUseTool` 中增加 `ToolName` 判断：

```go
case string(ControlCanUseTool):
    if cr.ToolName == "AskUserQuestion" {
        // → QuestionRequest 事件
        var questions []Question
        if len(cr.Input) > 0 {
            var input struct {
                Questions []Question `json:"questions"`
            }
            _ = json.Unmarshal(cr.Input, &input)
            questions = input.Questions
        }
        env := events.NewEnvelope(..., events.QuestionRequest,
            QuestionRequestData{
                ID:        cr.RequestID,
                ToolName:  cr.ToolName,
                Questions: questions,
            })
        w.trySend(env)
    } else {
        // → 现有 PermissionRequest 逻辑（不变）
        ...
    }

case "elicitation":
    // → ElicitationRequest 事件
    ...
```

#### 4.1.2 解析层调整 (`parser.go`)

当前 `parseControlRequest` 将 `elicitation` 归入 `default` 分支的 `EventControl`。
需在 `worker.go` 的分发中增加 `elicitation` case，或调整 `parser.go` 单独处理。

由于 `ControlRequestPayload` 已包含 `Subtype`/`Input` 字段，建议在 `worker.go` 层扩展分发，
保持 parser 的统一性。

#### 4.1.3 响应回传扩展 (`control.go`)

```go
// SendQuestionResponse — 新增
func (h *ControlHandler) SendQuestionResponse(
    ctx context.Context,
    requestID string,
    answers map[string]string,
) error {
    return h.sendResponse(requestID, map[string]any{
        "behavior": "allow",
        "updatedInput": map[string]any{
            "answers": answers,
        },
    })
}

// SendElicitationResponse — 新增
func (h *ControlHandler) SendElicitationResponse(
    ctx context.Context,
    requestID string,
    action string,
    content map[string]any,
) error {
    return h.sendResponse(requestID, map[string]any{
        "action":  action,
        "content": content,
    })
}
```

#### 4.1.4 Worker.Input 扩展 (`worker.go:230`)

实际签名：`Input(ctx context.Context, content string, metadata map[string]any) error`

```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // 现有：permission_response 处理 (worker.go:236-251)
    if metadata != nil {
        if permResp, ok := metadata["permission_response"].(map[string]any); ok {
            // ... 现有逻辑不变
        }
        // 新增：question_response
        if qResp, ok := metadata["question_response"].(map[string]any); ok {
            reqID, _ := qResp["id"].(string)
            answers, _ := qResp["answers"].(map[string]string)
            return w.control.SendQuestionResponse(ctx, reqID, answers)
        }
        // 新增：elicitation_response
        if eResp, ok := metadata["elicitation_response"].(map[string]any); ok {
            reqID, _ := eResp["id"].(string)
            action, _ := eResp["action"].(string)
            content, _ := eResp["content"].(map[string]any)
            return w.control.SendElicitationResponse(ctx, reqID, action, content)
        }
    }
    // Normal input... (现有逻辑)
}
```

### 4.2 OpenCode Server Adapter

**核心问题**: 当前 SSE 解析器只接受 AEP 格式，OpenCode 原生 bus 事件被丢弃。
需增加 dual-path 解析。

#### 4.2.1 SSE 双路径解析 (`worker.go`)

```go
// 在 readSSE() 中，AEP 解码失败后尝试 bus 事件解析

// 现有：AEP 解码
env, err := aep.DecodeLine([]byte(data))
if err == nil {
    // AEP envelope — 现有处理逻辑不变
    env.SessionID = sessionID
    w.SetLastIO(time.Now())
    // ... 发送到 recvCh
    continue
}

// 新增：尝试解析为 OpenCode bus 事件
var busEvent struct {
    Type       string          `json:"type"`
    Properties json.RawMessage `json:"properties"`
}
if jsonErr := json.Unmarshal([]byte(data), &busEvent); jsonErr != nil {
    w.Log.Warn("opencodeserver: decode SSE data (not AEP or bus event)", "data", data)
    continue
}

switch busEvent.Type {
case "permission.asked":
    w.handlePermissionAsked(busEvent.Properties)
case "question.asked":
    w.handleQuestionAsked(busEvent.Properties)
default:
    w.Log.Debug("opencodeserver: unhandled bus event", "type", busEvent.Type)
}
```

#### 4.2.2 Bus 事件处理

```go
func (w *Worker) handlePermissionAsked(props json.RawMessage) {
    var data struct {
        ID        string         `json:"id"`
        SessionID string         `json:"sessionID"`
        Metadata  map[string]any `json:"metadata"`
    }
    if err := json.Unmarshal(props, &data); err != nil {
        w.Log.Warn("opencodeserver: parse permission.asked", "error", err)
        return
    }

    toolName, _ := data.Metadata["tool"].(string)
    args, _ := json.Marshal(data.Metadata)
    env := events.NewEnvelope(
        aep.NewID(), w.sessionID, w.nextSeq(),
        events.PermissionRequest,
        events.PermissionRequestData{
            ID:          data.ID,
            ToolName:    toolName,
            Description: toolName,
            Args:        []string{string(args)},
        },
    )
    w.trySend(env)
}

func (w *Worker) handleQuestionAsked(props json.RawMessage) {
    var data struct {
        ID        string     `json:"id"`
        SessionID string     `json:"sessionID"`
        Questions []Question `json:"questions"`
    }
    if err := json.Unmarshal(props, &data); err != nil {
        w.Log.Warn("opencodeserver: parse question.asked", "error", err)
        return
    }

    env := events.NewEnvelope(
        aep.NewID(), w.sessionID, w.nextSeq(),
        events.QuestionRequest,
        QuestionRequestData{
            ID:        data.ID,
            Questions: data.Questions,
        },
    )
    w.trySend(env)
}
```

#### 4.2.3 响应回传 (`worker.go:282`)

实际签名：`Input(ctx context.Context, content string, metadata map[string]any) error`

```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // ... 现有 HTTP 发送逻辑

    if metadata != nil {
        // 新增：permission_response
        if permResp, ok := metadata["permission_response"].(map[string]any); ok {
            reqID, _ := permResp["id"].(string)
            allowed, _ := permResp["allowed"].(bool)
            reply := "once"
            if !allowed {
                reply = "reject"
            }
            return w.httpPost(ctx, fmt.Sprintf("/permission/%s/reply", reqID),
                map[string]string{"reply": reply})
        }
        // 新增：question_response
        if qResp, ok := metadata["question_response"].(map[string]any); ok {
            reqID, _ := qResp["id"].(string)
            answers, _ := qResp["answers"].(map[string]string)
            // map → 二维数组
            arrays := aepAnswersToOpenCodeArrays(answers)
            return w.httpPost(ctx, fmt.Sprintf("/question/%s/reply", reqID),
                map[string][][]string{"answers": arrays})
        }
    }

    // 现有：普通 input 发送
    ...
}
```

---

## 5. Messaging Adapter 集成指南

### 5.1 交互事件处理接口

在 `internal/messaging/` 中定义交互处理器接口：

```go
type InteractionHandler interface {
    HandlePermissionRequest(ctx context.Context, req *events.PermissionRequestData) (*events.PermissionResponseData, error)
    HandleQuestionRequest(ctx context.Context, req *QuestionRequestData) (*QuestionResponseData, error)
}
```

### 5.2 Slack 展示方案

#### 权限请求

```
┌─────────────────────────────────────────┐
│ ⚠️ Tool Approval Required               │
│                                         │
│ Claude Code wants to:                   │
│ 📝 Write → src/main.go                  │
│                                         │
│ ```diff                                 │
│ +package main                           │
│ +func main() { ... }                    │
│ ```                                     │
│                                         │
│ React with:                             │
│   👍 = Allow                            │
│   ❌ = Deny                             │
└─────────────────────────────────────────┘
```

**实现方式**: Interactive Message + Reaction 等待

#### 问题询问

```
┌─────────────────────────────────────────┐
│ ❓ Auth method                          │
│                                         │
│ 使用哪种认证方式？                       │
│                                         │
│ [JWT] — 无状态令牌认证                  │
│ [Session] — 服务端会话认证              │
└─────────────────────────────────────────┘
```

**实现方式**: Block Kit Buttons

### 5.3 Feishu 展示方案

#### 权限请求

```json
{
  "elements": [
    { "tag": "markdown", "content": "**⚠️ 工具执行授权**\nClaude Code 请求：\n📝 **Write** → `src/main.go`" },
    { "tag": "hr" },
    { "tag": "action", "actions": [
      { "tag": "button", "text": "允许", "value": "allow", "type": "primary" },
      { "tag": "button", "text": "拒绝", "value": "deny", "type": "danger" }
    ]}
  ]
}
```

#### 问题询问

```json
{
  "elements": [
    { "tag": "markdown", "content": "**❓ Auth method**\n使用哪种认证方式？" },
    { "tag": "action", "actions": [
      { "tag": "button", "text": "JWT", "value": "JWT", "type": "primary" },
      { "tag": "button", "text": "Session", "value": "Session" }
    ]}
  ]
}
```

### 5.4 交互生命周期

```
1. Worker 发出交互请求
   └→ Adapter 解析 → AEP 事件 → Gateway 路由 → Messaging 层

2. Messaging 层展示交互 UI
   └→ Slack: Interactive Message / Feishu: Interactive Card
   └→ 启动超时定时器（默认 5 分钟）

3. 用户响应（三种路径）
   a. 用户点击按钮 → 平台回调 → 解析响应 → AEP 事件回传
   b. 超时无响应 → 自动 deny/reject → AEP 事件回传
   c. 用户发送文本取消 → 检测取消关键词 → deny/reject

4. 响应路由回 Worker
   └→ AEP 事件 → Gateway → Worker.Input(content, metadata)

5. 清理
   └→ 删除交互 UI 消息 / 更新为最终状态
```

### 5.5 超时与取消

| 场景 | 处理 |
|------|------|
| 用户 5 分钟未响应 | 自动 `deny`/`reject`，更新 UI 显示超时 |
| Session 结束（GC/Reset） | 取消所有待处理交互，自动 `deny` |
| 用户发送 `/cancel` | 取消当前交互，`deny` |
| Worker 进程退出 | 丢弃待处理响应，清理 UI |

---

## 6. 协议差异映射表

### 6.1 权限请求映射

| 维度 | Claude Code | OpenCode Server | AEP 统一层 |
|------|-------------|-----------------|------------|
| 触发 | stdout `control_request` | SSE `permission.asked` | `PermissionRequest` |
| 请求 ID | `request_id` | `properties.id` | `ID` |
| 工具名 | `response.tool_name` | `properties.metadata.tool` | `ToolName` |
| 工具输入 | `response.input` (JSON) | `properties.metadata` (object) | `Args []string` (JSON 序列化) |
| 允许 | `{"allowed": true}` | `{"reply": "once"}` | `Allowed: true` |
| 持久允许 | `decisionClassification: "user_permanent"` | `{"reply": "always"}` | 待扩展 |
| 拒绝 | `{"allowed": false, "reason": "..."}` | `{"reply": "reject"}` | `Allowed: false` |
| 响应通道 | stdin `control_response` | `POST /permission/{id}/reply` | `PermissionResponse` |

### 6.2 问题询问映射

| 维度 | Claude Code | OpenCode Server | AEP 统一层 |
|------|-------------|-----------------|------------|
| 触发 | `can_use_tool` + `tool_name=AskUserQuestion` | SSE `question.asked` | `QuestionRequest` |
| 请求 ID | `request_id` | `properties.id` | `ID` |
| 问题结构 | `input.questions[]` | `properties.questions[]` | `Questions []Question` |
| 答案格式 | `answers: {question: label}` map | `answers: [[label1], [label2]]` array | `Answers: map[string]string` |
| 多选 | `multiSelect: true` | `multiSelect: true` | `MultiSelect: bool` |
| 响应通道 | stdin `control_response` (behavior=allow) | `POST /question/{id}/reply` | `QuestionResponse` |

---

## 7. 实现优先级

### Phase 1: Claude Code 权限请求（端到端打通）

**目标**: 让 Claude Code 的 `can_use_tool` 权限请求在 Slack/Feishu 上可见且可审批。

| 任务 | 文件 | 工作量 |
|------|------|--------|
| Messaging 层 `HandlePermissionRequest` | `messaging/interaction.go` | 4h |
| Slack 权限 UI（Interactive Message） | `messaging/slack/permission.go` | 4h |
| Feishu 权限 UI（Interactive Card） | `messaging/feishu/permission.go` | 4h |
| 超时自动 deny | `messaging/interaction.go` | 2h |
| 端到端测试 | `e2e/` | 2h |

### Phase 2: 问题询问支持

**目标**: 支持 `AskUserQuestion` / `question` tool 的双向问答。

| 任务 | 文件 | 工作量 |
|------|------|--------|
| 新增 AEP `QuestionRequest/Response` 事件 | `pkg/events/events.go` | 1h |
| Claude Code `AskUserQuestion` 区分 | `worker/claudecode/worker.go:402` | 2h |
| Claude Code 问题响应回传 | `worker/claudecode/control.go` | 2h |
| OpenCode SSE 双路径解析 | `worker/opencodeserver/worker.go:642` | 3h |
| OpenCode 问题 HTTP 回复 | `worker/opencodeserver/worker.go:282` | 2h |
| Messaging 层问题 UI | `messaging/{slack,feishu}/question.go` | 4h |

### Phase 3: OpenCode Server 权限 + MCP Elicitation

| 任务 | 文件 | 工作量 |
|------|------|--------|
| OpenCode `permission.asked` 解析 | `worker/opencodeserver/worker.go` | 2h |
| OpenCode 权限 HTTP 回复 | `worker/opencodeserver/worker.go` | 2h |
| MCP Elicitation 支持 | `worker/claudecode/parser.go` + `worker.go` | 3h |
| Elicitation UI | `messaging/{slack,feishu}/` | 3h |

---

## 8. 风险与约束

### 8.1 平台限制

| 平台 | 限制 | 应对 |
|------|------|------|
| Slack | Interactive Message 5 分钟过期 | 设置超时 ≤4 分钟，提前更新 |
| Slack | Block Kit 最多 100 个 blocks | diff 截断到合理长度 |
| Feishu | 交互卡片回调需注册 URL | 确保 callback URL 已配置 |
| Feishu | 按钮最多 4 个 | 多选项用下拉选择器替代 |

### 8.2 安全考虑

| 风险 | 缓解 |
|------|------|
| 恶意用户伪造权限响应 | 验证响应来源为交互 UI 回调（非自由文本） |
| 权限提升（always 授权） | 持久授权需二次确认或限制为特定 session |
| 超时窗口内的竞态 | 交互请求 ID 匹配 + 一次性消费 |

### 8.3 性能

| 场景 | 影响 | 应对 |
|------|------|------|
| 单 session 多交互排队 | 串行处理，后续请求排队 | 展示队列状态，允许跳过 |
| 大 diff 内容 | 消息过长 | 截断 + 提供 "查看完整内容" 链接 |
| 高频工具调用 | 大量权限请求 | 支持批量 auto-approve 模式 |

### 8.4 协议兼容性

| 风险 | 说明 |
|------|------|
| HotPlex 简化响应 vs Claude Code 标准 schema | 当前用 `{"allowed": bool}` 而非 `{"behavior": "allow"}`，实测可行但未来 CC 版本可能要求标准格式 |
| OpenCode SSE 事件格式变更 | Bus 事件格式是内部实现，跨版本可能变化 |
