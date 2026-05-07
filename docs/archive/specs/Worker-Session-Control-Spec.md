---
type: spec
tags: [project/HotPlex, worker/stdio, worker/claudecode, messaging, gateway]
date: 2026-04-19
status: verified
progress: 100
priority: high
estimated_hours: 16
verification: scripts/test_cc_context.py 10/10 passed 2026-04-19 (CC context commands)
last_updated: 2026-04-21
---

# Worker Session Control Spec

## 概述

新增 Worker **stdio 直达**控制能力：10 项已验证命令（4 项 Control Request + 6 项 Slash Command Passthrough）。
利用 Claude Code `--output-format stream-json` stdio 协议的原生能力，
**绕过 Gateway Session Manager 状态机**，直接通过 stdin 与 Worker 子进程交互。

### 核心架构区别

本 spec 定义的能力与现有 control command 是**完全不同的技术路线**：

```
现有 Control Command（进程级）：
  Messaging → ParseControlCommand → handleControl → SM.Transition() → Worker.Terminate/Start
  特征：改变 session 状态，进程重启/终止

本 Spec：Worker Stdio Command（协议级）：
  Messaging → ParseWorkerCommand → handleWorkerCommand → Worker.Input/SendControlRequest
  特征：session 状态不变，stdin/stdout 原地交互
```

| 维度 | 现有 Control Command | Worker Stdio Command（本 spec） |
|------|---------------------|-------------------------------|
| 触发路径 | Gateway handler → SM 状态机 | Gateway handler → **直接调 Worker 方法** |
| Session 状态 | 变化（Running→Terminated 等） | **不变**（始终 Running） |
| 进程影响 | 杀进程/重启 | **原地**，进程不退出 |
| 协议层 | AEP Control Event | CC: stream-json stdio / OpenCode: HTTP REST API |
| Worker 接口 | `Terminate()` `Kill()` | `Input()` `SendControlRequest()` / HTTP 调用 |
| 适用 Worker | 所有（通用生命周期） | Claude Code (10/10) + OpenCode Server (8/10)，其他 Worker 返回 ErrCodeNotSupported |

### 动机

| 现状 | 问题 |
|------|------|
| `/reset` = 杀进程 + 新建 | 延迟 5-10s 冷启动，丢失 session 连续性 |
| 无 context 可见性 | 用户无法感知 token 消耗，直到 API 报错 |
| 无法原地压缩 | 对话变长后只能全量 reset，浪费已有上下文 |

### 验证基础

通过 `scripts/test_cc_context.py` 验证了 **10/10** 命令在 Claude Code stream-json 协议中全部可用：

```
=== Control Requests (4/4 PASS) ===
get_context_usage  → 76,284 / 200,000 tokens (38%), 8 categories, model confirmed
mcp_status         → 12 MCP servers, all connected
set_model          → success, model changed
set_permission_mode → success, mode accepted

=== User Message Passthrough (6/6 PASS) ===
/compact  → context reduced 38% → 18%, 原地执行
/clear    → context reduced 38% → 26%, 对话清空
/model    → model changed, verifiable via get_context_usage
/effort   → accepted, no structured output
/rewind   → conversation state rewound (filesystem not affected)
/commit   → triggers full commit workflow (git status → diff → commit)
```

---

## 架构设计

### 两类命令的分治

```
┌──────────────────────────────────────────────────────────────┐
│                      Messaging Layer                         │
│                                                              │
│  "/gc"  "/reset"  →  ParseControlCommand()                  │
│                         → handleControl()                    │
│                             → SM.Transition()                │  ← 现有
│                             → Worker.Terminate()             │
│                                                              │
│  "/compact"  "/clear"  "/model"  "/effort"                   │
│  "/rewind"   "/commit"  "/context"  "/mcp"  "/perm"          │
│                    →  ParseWorkerCommand()   ← 新增          │
│                         → handleWorkerCommand()              │
│                             → Worker 直接 stdin 交互         │  ← 本 spec
│                             (不经过 SM 状态机)               │
└──────────────────────────────────────────────────────────────┘
```

### 两类命令的 stdin 协议路径

```
路径 A: User Message Passthrough（6 项）
  stdin ← {"type":"user","message":{"role":"user","content":"/compact"}}
  stdout → assistant / result / system 事件流（走已有 readOutput 管道）
  Worker 层：零改动，复用 Input()

路径 B: Control Request（4 项）
  stdin ← {"type":"control_request","request_id":"ctx_xxx","request":{"subtype":"get_context_usage"}}
  stdout → {"type":"control_response","response":{"request_id":"ctx_xxx","subtype":"success",...}}
  Worker 层：需 SendControlRequest() + pendingRequests + readOutput 扩展
```

### 数据流总览

```
                    ┌─ compact / clear ─┐
                    │  User Message     │
                    │  透传到 stdin     │
                    │                   │
Messaging ────────► │                   │ ───► Worker stdin ───► CC 子进程
  "/compact"       │                   │        NDJSON            │
  "/clear"         │                   │                          ▼
  "/context"       │ context_usage     │                   CC 原地执行
                    │ Control Request   │                   (不重启进程)
                    │ 到 stdin          │
                    └───────────────────┘
                                               ◄── stdout (control_response / assistant / result)
                                                    │
                                                    ▼
                                              parser → mapper
                                                    │
                                                    ▼
                                             AEP Envelope → hub.Broadcast
                                                    │
                                                    ▼
                                            PlatformConn.WriteCtx()
                                                    │
                                                    ▼
                                          Feishu Card / Slack Block
```

### 能力矩阵

#### Control Requests（路径 B — 结构化请求/响应）

| 命令 | Slash 触发 | 自然语言 | CC subtype | 返回方式 | 进程影响 |
|------|-----------|---------|-----------|---------|---------|
| context_usage | `/context` | `上下文` `容量` `token` | `get_context_usage` | control_response | 只读 |
| skills | `/skills` | `技能` `插件` `能力` | `get_context_usage` | control_response | 只读 |
| mcp_status | `/mcp` | `MCP状态` `工具状态` | `mcp_status` | control_response | 只读 |
| set_model | `/model <name>` | `切换模型` | `set_model` | control_response | 原地 |
| set_permission | `/perm <mode>` | `权限模式` | `set_permission_mode` | control_response | 原地 |

#### User Message Passthrough（路径 A — slash command 透传）

| 命令 | Slash 触发 | 自然语言 | stdin 内容 | 返回方式 | 进程影响 |
|------|-----------|---------|-----------|---------|---------|
| compact | `/compact` | `压缩` `精简` | `/compact` | assistant + result | 原地 |
| clear | `/clear` | `清空` `清屏` | `/clear` | system/init | 原地 |
| model | `/model <name>` | — | `/model <name>` | system/init | 原地 |
| effort | `/effort <level>` | — | `/effort <level>` | result | 原地 |
| rewind | `/rewind` | `回退` `撤销` | `/rewind` | result | 原地 |
| commit | `/commit` | `提交` | `/commit` | assistant + tool_use | 原地 |

---

## 详细设计

### Phase 1: 事件类型与数据结构

#### 1.1 新增 EventType

文件：`pkg/events/events.go`

```go
const (
    // ... existing ...
    ContextUsage EventType = "context_usage"  // Worker context usage report
    MCPStatus    EventType = "mcp_status"     // Worker MCP server status
    WorkerCmd    EventType = "worker_command"  // Gateway → Worker stdio command trigger
)
```

**不新增 ControlAction**。本 spec 的所有命令不是生命周期控制，不走 ControlData。

#### 1.2 ContextUsageData 结构

文件：`pkg/events/events.go`

```go
// ContextUsageData carries context window usage breakdown from a worker.
// Produced by worker context query (get_context_usage control request),
// broadcast to all session subscribers.
type ContextUsageData struct {
    TotalTokens int               `json:"total_tokens"`
    MaxTokens   int               `json:"max_tokens"`
    Percentage  int               `json:"percentage"` // 0-100
    Model       string            `json:"model,omitempty"`

    Categories  []ContextCategory  `json:"categories,omitempty"`

    // Aggregated counts
    MemoryFiles int               `json:"memory_files,omitempty"`
    MCPTools    int               `json:"mcp_tools,omitempty"`
    Agents      int               `json:"agents,omitempty"`
    Skills      ContextSkillInfo  `json:"skills,omitempty"`
}

type ContextCategory struct {
    Name   string `json:"name"`
    Tokens int    `json:"tokens"`
}

type ContextSkillInfo struct {
    Total    int `json:"total"`
    Included int `json:"included"`
    Tokens   int `json:"tokens"`
}
```

**不包含 GridRows**：grid 是 CC 内部 UI 渲染概念，Gateway 只需聚合数据。

#### 1.3 WorkerStdioCommand 类型

文件：`pkg/events/events.go`

```go
// WorkerStdioCommand identifies a stdio-level command sent directly
// to the worker subprocess. Unlike ControlAction, these do NOT change
// session state — they are in-place operations on a running worker.
type WorkerStdioCommand string

const (
    // Control Requests (路径 B — structured request/response)
    StdioContextUsage WorkerStdioCommand = "context_usage"    // Query context tokens (read-only)
    StdioMCPStatus    WorkerStdioCommand = "mcp_status"       // Query MCP server status (read-only)
    StdioSetModel     WorkerStdioCommand = "set_model"        // Change model
    StdioSetPermMode  WorkerStdioCommand = "set_permission"   // Change permission mode

    // User Message Passthrough (路径 A — slash command forwarded as user message)
    StdioCompact WorkerStdioCommand = "compact"  // In-place context compaction
    StdioClear   WorkerStdioCommand = "clear"    // In-place conversation clear
    StdioModel   WorkerStdioCommand = "model"    // Switch model via /model command
    StdioEffort  WorkerStdioCommand = "effort"   // Set reasoning effort level
    StdioRewind  WorkerStdioCommand = "rewind"   // Rewind last exchange
    StdioCommit  WorkerStdioCommand = "commit"   // Trigger git commit workflow
)

// IsPassthrough returns true if the command is sent as a user message (路径 A).
func (c WorkerStdioCommand) IsPassthrough() bool {
    switch c {
    case StdioCompact, StdioClear, StdioModel, StdioEffort, StdioRewind, StdioCommit:
        return true
    default:
        return false
    }
}
```

#### 1.4 WorkerCommandData 事件载荷

文件：`pkg/events/events.go`

```go
// WorkerCommandData is the payload for worker stdio command events.
// Carried in AEP Event.Data when a client requests a worker-level operation.
type WorkerCommandData struct {
    Command WorkerStdioCommand `json:"command"`
    Args    string             `json:"args,omitempty"`    // e.g. model name, effort level
    Extra   map[string]any     `json:"extra,omitempty"`   // e.g. {"mode": "bypassPermissions"}
}
```

#### 1.5 MCPStatusData 结构

```go
// MCPStatusData carries MCP server connection status from a worker.
type MCPStatusData struct {
    Servers []MCPServerInfo `json:"servers"`
}

type MCPServerInfo struct {
    Name   string `json:"name"`
    Status string `json:"status"` // "connected", "disconnected", "error"
}
```

---

### Phase 2: Messaging 层命令解析

#### 2.1 新增 WorkerCommand 解析器

文件：`internal/messaging/control_command.go`

与现有 `ParseControlCommand` 平行，新增 `ParseWorkerCommand`：

```go
// WorkerCommandResult holds the parsed worker stdio command and label.
type WorkerCommandResult struct {
    Command events.WorkerStdioCommand
    Label   string
    Args    string            // captured args (e.g. model name from "/model claude-sonnet-4")
    Extra   map[string]any    // structured args
}

// workerSlashMap maps slash commands to worker stdio commands.
// Prefix-only matches: "/model", "/effort", "/perm" capture remaining text as Args.
var workerSlashMap = map[string]WorkerCommandResult{
    // Control Requests
    "/context": {events.StdioContextUsage, "context"},
    "/mcp":     {events.StdioMCPStatus, "mcp_status"},
    "/model":   {events.StdioSetModel, "model"},    // args: model name
    "/perm":    {events.StdioSetPermMode, "perm"},   // args: mode name
    // User Message Passthrough
    "/compact": {events.StdioCompact, "compact"},
    "/clear":   {events.StdioClear, "clear"},
    "/effort":  {events.StdioEffort, "effort"},      // args: level
    "/rewind":  {events.StdioRewind, "rewind"},
    "/commit":  {events.StdioCommit, "commit"},
}

// workerNLMap maps natural language triggers to worker stdio commands.
var workerNLMap = map[string]WorkerCommandResult{
    // Control Requests
    "上下文": {events.StdioContextUsage, "context"},
    "容量":   {events.StdioContextUsage, "context"},
    "token":  {events.StdioContextUsage, "context"},
    "MCP状态": {events.StdioMCPStatus, "mcp_status"},
    "工具状态": {events.StdioMCPStatus, "mcp_status"},
    "切换模型": {events.StdioSetModel, "model"},
    "权限模式": {events.StdioSetPermMode, "perm"},
    // User Message Passthrough
    "压缩": {events.StdioCompact, "compact"},
    "精简": {events.StdioCompact, "compact"},
    "清空": {events.StdioClear, "clear"},
    "清屏": {events.StdioClear, "clear"},
    "回退": {events.StdioRewind, "rewind"},
    "撤销": {events.StdioRewind, "rewind"},
    "提交": {events.StdioCommit, "commit"},
}

// ParseWorkerCommand checks whether text is a worker stdio command.
// Returns nil if not a worker command.
// Check ParseControlCommand FIRST — it takes priority (e.g. "/reset" vs future conflicts).
func ParseWorkerCommand(text string) *WorkerCommandResult {
    t := strings.TrimSpace(strings.ToLower(text))
    t = trimTrailingPunct(t)

    if result, ok := workerSlashMap[t]; ok {
        return &result
    }
    if result, ok := workerNLMap[t]; ok {
        return &result
    }
    return nil
}
```

**优先级**：`ParseControlCommand` > `ParseWorkerCommand`。
在 messaging adapter 的 HandleTextMessage 中先检查 control command，再检查 worker command。

---

### Phase 3: Gateway Handler 新路径

#### 3.1 新增 handleWorkerCommand

文件：`internal/gateway/handler.go`

```go
// handleWorkerCommand processes worker-level stdio commands.
// Unlike handleControl, this does NOT change session state.
// The command is routed directly to the worker subprocess via stdin.
func (h *Handler) handleWorkerCommand(
    ctx context.Context,
    env *events.Envelope,
    cmd events.WorkerStdioCommand,
    args string,
    extra map[string]any,
) error {
    info, err := h.validateOwner(ctx, env)
    if err != nil {
        return err
    }
    if !info.State.IsActive() {
        return h.sendErrorf(ctx, env, events.ErrCodeInvalidState,
            "worker command requires active session, current: %s", info.State)
    }

    w := h.sm.GetWorker(env.SessionID)
    if w == nil {
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "no worker attached")
    }

    // Path A: User Message Passthrough — forward slash command as user message
    if cmd.IsPassthrough() {
        content := "/" + string(cmd)
        if args != "" {
            content += " " + args
        }
        if err := w.Input(ctx, content, nil); err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "%s: %v", cmd, err)
        }
        return nil
    }

    // Path B: Control Request — structured request/response via ControlRequester interface
    type ControlRequester interface {
        SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
    }
    cr, ok := w.(ControlRequester)
    if !ok {
        return h.sendErrorf(ctx, env, events.ErrCodeNotSupported,
            "worker type does not support control requests")
    }

    switch cmd {
    case events.StdioContextUsage:
        resp, err := cr.SendControlRequest(ctx, "get_context_usage", nil)
        if err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "context query: %v", err)
        }
        data := mapContextUsageResponse(resp)
        respEnv := events.NewEnvelope(
            aep.NewID(), env.SessionID,
            h.hub.NextSeq(env.SessionID),
            events.ContextUsage, data,
        )
        return h.hub.SendToSession(ctx, respEnv)

    case events.StdioMCPStatus:
        resp, err := cr.SendControlRequest(ctx, "mcp_status", nil)
        if err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "mcp status: %v", err)
        }
        data := mapMCPStatusResponse(resp)
        respEnv := events.NewEnvelope(
            aep.NewID(), env.SessionID,
            h.hub.NextSeq(env.SessionID),
            events.MCPStatus, data,
        )
        return h.hub.SendToSession(ctx, respEnv)

    case events.StdioSetModel:
        modelName := args
        if modelName == "" {
            modelName, _ = extra["model"].(string)
        }
        if modelName == "" {
            return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "model name required")
        }
        _, err := cr.SendControlRequest(ctx, "set_model", map[string]any{"model": modelName})
        if err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "set model: %v", err)
        }
        // Confirmation flows through normal event pipeline

    case events.StdioSetPermMode:
        mode := args
        if mode == "" {
            mode, _ = extra["mode"].(string)
        }
        if mode == "" {
            return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "permission mode required")
        }
        _, err := cr.SendControlRequest(ctx, "set_permission_mode", map[string]any{"mode": mode})
        if err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "set permission: %v", err)
        }

    default:
        return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation,
            "unknown worker command: %s", cmd)
    }
    return nil
}
```

#### 3.2 AEP Event 分发入口

在 `Handle()` 方法中，`case events.Control` 之外新增 worker command 的处理入口：

**方案 A** — 复用 Control 事件类型，通过 Data 区分：
```go
// In Handle():
case events.Control:
    // Check if this is a worker stdio command or a lifecycle control
    if wcd, ok := env.Event.Data.(events.WorkerCommandData); ok {
        return h.handleWorkerCommand(ctx, env, wcd.Command, wcd.Args)
    }
    return h.handleControl(ctx, env)
```

**方案 B** — 新增 EventType `worker_command`：
```go
// In Handle():
case events.WorkerCommand:
    wcd := env.Event.Data.(events.WorkerCommandData)
    return h.handleWorkerCommand(ctx, env, wcd.Command, wcd.Args)
```

**推荐方案 B**：职责更清晰，避免 Control 事件承担两种语义。

---

### Phase 4: Worker 层实现

#### 4.1 Claude Code Worker — ControlRequester

文件：`internal/worker/claudecode/worker.go`

```go
// SendControlRequest sends a control_request to Claude Code via stdin
// and returns the parsed response. Implements ControlRequester interface.
func (w *Worker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
    if w.control == nil {
        return nil, fmt.Errorf("claudecode: control handler not initialized")
    }
    return w.control.SendControlRequest(ctx, subtype, body)
}
```

#### 4.2 ControlHandler 扩展

文件：`internal/worker/claudecode/control.go`

新增 `SendControlRequest` — 发送任意 control_request 到 CC stdin 并等待 response：

```go
// pendingRequests tracks in-flight control requests awaiting responses.
// Key: request_id, Value: channel to deliver the response.
pendingRequests map[string]chan map[string]any

// SendControlRequest sends a control_request to Claude Code via stdin
// and blocks until the matching control_response arrives or ctx expires.
func (h *ControlHandler) SendControlRequest(
    ctx context.Context,
    subtype string,
    body map[string]any,
) (map[string]any, error) {
    reqID := "ctx_" + uuid.New().String() // prefix avoids collision with permission request_ids

    // Register pending response channel
    respCh := make(chan map[string]any, 1)
    h.mu.Lock()
    h.pendingRequests[reqID] = respCh
    h.mu.Unlock()
    defer func() {
        h.mu.Lock()
        delete(h.pendingRequests, reqID)
        h.mu.Unlock()
    }()

    // Build and send request
    req := map[string]any{
        "type":       "control_request",
        "request_id": reqID,
        "request": mergeMaps(map[string]any{"subtype": subtype}, body),
    }
    data, _ := json.Marshal(req)
    data = append(data, '\n')

    h.mu.Lock()
    _, err := h.stdin.Write(data)
    h.mu.Unlock()
    if err != nil {
        return nil, fmt.Errorf("control: write request: %w", err)
    }

    // Wait for response with timeout
    select {
    case resp := <-respCh:
        return resp, nil
    case <-ctx.Done():
        return nil, fmt.Errorf("control: context query timed out: %w", ctx.Err())
    }
}

// DeliverResponse routes a control_response to the pending requester.
// Called from readOutput when a control_response is received on stdout.
func (h *ControlHandler) DeliverResponse(reqID string, resp map[string]any) {
    h.mu.Lock()
    ch, ok := h.pendingRequests[reqID]
    h.mu.Unlock()
    if ok {
        ch <- resp
    }
}
```

#### 4.3 readOutput 扩展

文件：`internal/worker/claudecode/worker.go`

在 `readOutput` 的 NDJSON 解析循环中，新增 `control_response` 路由：

```go
// In readOutput, after determining msgType:
case "control_response":
    respWrap, _ := parsed["response"].(map[string]any)
    if respWrap == nil {
        continue
    }
    reqID, _ := respWrap["request_id"].(string)
    if reqID != "" && w.control != nil {
        w.control.DeliverResponse(reqID, respWrap)
    }
    // Do NOT forward to gateway — internal protocol response
    // Only permission responses (can_use_tool) are forwarded
```

#### 4.4 响应映射

文件：`internal/worker/claudecode/mapper.go`

```go
func mapContextUsageResponse(raw map[string]any) *events.ContextUsageData {
    data := &events.ContextUsageData{
        TotalTokens: intFloat(raw["totalTokens"]),
        MaxTokens:   intFloat(raw["maxTokens"]),
        Percentage:  intFloat(raw["percentage"]),
        Model:       strVal(raw["model"]),
        MemoryFiles: intFloat(raw["memoryFiles"]),
        MCPTools:    intFloat(raw["mcpTools"]),
        Agents:      intFloat(raw["agents"]),
    }

    // Parse categories
    for _, c := range sliceVal(raw["categories"]) {
        m, _ := c.(map[string]any)
        data.Categories = append(data.Categories, events.ContextCategory{
            Name:   strVal(m["name"]),
            Tokens: intFloat(m["tokens"]),
        })
    }

    // Parse skills
    if s, ok := raw["skills"].(map[string]any); ok {
        data.Skills = events.ContextSkillInfo{
            Total:    intFloat(s["totalSkills"]),
            Included: intFloat(s["includedSkills"]),
            Tokens:   intFloat(s["tokens"]),
        }
    }

    return data
}

// helpers
func intFloat(v any) int    { f, _ := v.(float64); return int(f) }
func strVal(v any) string   { s, _ := v.(string); return s }
func sliceVal(v any) []any  { s, _ := v.([]any); return s }
```

#### 4.5 Passthrough 命令 — 无需 Worker 层改动

所有 6 项 passthrough 命令 (`/compact`, `/clear`, `/model`, `/effort`, `/rewind`, `/commit`)
通过 `worker.Input()` 发送，走的是已有的 `SendUserMessage` 路径。
Claude Code 将其解析为 slash command 并原地执行。结果通过正常的 `assistant` → `result` 流式管道返回。

Worker 层**零改动**即可支持所有 passthrough 命令。

---

### Phase 5: 非 Claude Code Worker 兼容

#### 5.1 接口隔离

```go
// ControlRequester is implemented by workers that support structured control queries.
// Claude Code: stdin control_request/control_response protocol.
// OpenCode Server: HTTP REST API calls.
type ControlRequester interface {
    SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
}

// WorkerCommander is implemented by workers that support worker-level commands
// beyond the basic Input() passthrough. Each method returns an AEP-serializable result.
type WorkerCommander interface {
    // Compact performs in-place context compaction.
    Compact(ctx context.Context, args map[string]any) error
    // Clear clears conversation context in-place.
    Clear(ctx context.Context) error
    // Rewind reverts the last conversation exchange.
    Rewind(ctx context.Context, targetID string) error
}
```

#### 5.2 Worker 能力矩阵

| Worker | Passthrough (6) | Control Requests (4) | 总计 | 备注 |
|--------|----------------|----------------------|------|------|
| claudecode | 6/6 stdin passthrough | 4/4 SendControlRequest | **10/10** | 完整支持 |
| opencodeserver | 5/6 HTTP API | 3/4 HTTP API | **8/10** | effort ❌, mcp_status ⚠️ 间接 |
| acpx | 0/6 | 0/4 | **0/10** | stdio 协议不同 |
| pi | 0/6 | 0/4 | **0/10** | 私有协议 |
| noop | 0/6 | 0/4 | **0/10** | 测试用 |

---

### Phase 5B: OpenCode Server Worker 详细设计

OpenCode Server 模式（`opencode serve`）通过 **HTTP REST API** 实现等价能力，
底层协议与 Claude Code 的 stdin control_request 完全不同，但对外暴露相同的 `ControlRequester` + `WorkerCommander` 接口。

#### 5B.1 架构映射

```
Claude Code Worker:
  handleWorkerCommand → w.Input("/compact") → stdin NDJSON → CC 子进程
  handleWorkerCommand → w.SendControlRequest("get_context_usage") → stdin → stdout 响应

OpenCode Server Worker:
  handleWorkerCommand → w.Compact(ctx, args) → HTTP POST /session/{id}/summarize → opencode 进程
  handleWorkerCommand → w.SendControlRequest("get_context_usage") → HTTP GET /session/{id}/message → 聚合 tokens
```

#### 5B.2 OpenCode Server REST API 对照表

| Worker Stdio Command | OpenCode Server API | HTTP 方法 | 请求体 | 响应 |
|----------------------|-------------------|-----------|--------|------|
| `StdioCompact` | `/session/{id}/summarize` | POST | `{providerID, modelID, auto}` | `true` |
| `StdioClear` | `/session/{id}` DELETE + `/session` POST | DELETE+POST | `{title}` | new session |
| `StdioModel` | message 的 `model` 字段 | — (存储) | `{providerID, modelID}` | — |
| `StdioSetModel` | 同 StdioModel | — (存储) | — | — |
| `StdioEffort` | **无 API 等价** | — | — | **ErrCodeNotSupported** |
| `StdioRewind` | `/session/{id}/revert` | POST | `{messageID, partID?}` | `{revert, summary}` |
| `StdioCommit` | `/session/{id}/message` | POST | `{parts: [{text: "/commit"}]}` | streaming message |
| `StdioContextUsage` | `/session/{id}/message` | GET | `?limit=N` | 聚合 tokens |
| `StdioMCPStatus` | `/experimental/tool` | GET | — | tool list (无连接状态) |
| `StdioSetPermMode` | `/session/{id}` | PATCH | `{permission: [...]}` | — |

#### 5B.3 文件：`internal/worker/opencodeserver/commands.go`

```go
package opencodeserver

import (
    "context"
    "fmt"
    "net/http"

    "github.com/hrygo/hotplex/pkg/events"
)

// ServerCommander implements ControlRequester + WorkerCommander for OpenCode Server.
// Routes worker commands to OpenCode's HTTP REST API.
type ServerCommander struct {
    client    *http.Client
    baseURL   string        // e.g. "http://127.0.0.1:4096"
    sessionID string
    authToken string        // from OPENCODE_SERVER_PASSWORD
    // pendingModel stores model selection for subsequent message requests.
    pendingModel *ModelRef
}

type ModelRef struct {
    ProviderID string `json:"providerID"`
    ModelID    string `json:"modelID"`
}

// ─── ControlRequester interface ────

func (c *ServerCommander) SendControlRequest(
    ctx context.Context,
    subtype string,
    body map[string]any,
) (map[string]any, error) {
    switch subtype {
    case "get_context_usage":
        return c.queryContextUsage(ctx)
    case "set_model":
        return c.setModel(ctx, body)
    case "set_permission_mode":
        return c.setPermissionMode(ctx, body)
    case "mcp_status":
        return c.queryMCPStatus(ctx)
    default:
        return nil, fmt.Errorf("opencode server: unsupported control request: %s", subtype)
    }
}

// ─── WorkerCommander interface ────

func (c *ServerCommander) Compact(ctx context.Context, args map[string]any) error {
    // POST /session/{id}/summarize
    reqBody := map[string]any{
        "auto": false,
    }
    if c.pendingModel != nil {
        reqBody["providerID"] = c.pendingModel.ProviderID
        reqBody["modelID"] = c.pendingModel.ModelID
    }
    var result bool
    if err := c.doPost(ctx, "/session/"+c.sessionID+"/summarize", reqBody, &result); err != nil {
        return fmt.Errorf("opencode compact: %w", err)
    }
    return nil
}

func (c *ServerCommander) Clear(ctx context.Context) error {
    // OpenCode has no in-place clear. Equivalent: delete session, create new one.
    // NOTE: This changes session ID — caller must update references.
    if err := c.doDelete(ctx, "/session/"+c.sessionID); err != nil {
        return fmt.Errorf("opencode clear (delete): %w", err)
    }
    // Create new session — returns new session ID
    var newSession struct{ ID string }
    if err := c.doPost(ctx, "/session", map[string]any{}, &newSession); err != nil {
        return fmt.Errorf("opencode clear (create): %w", err)
    }
    c.sessionID = newSession.ID
    return nil
}

func (c *ServerCommander) Rewind(ctx context.Context, targetID string) error {
    // POST /session/{id}/revert
    // OpenCode revert is MORE capable than CC rewind:
    //   - Reverts file system changes (snapshots + diff)
    //   - Supports reverting to specific message part
    //   - Can be undone via /session/{id}/unrevert
    reqBody := map[string]any{
        "messageID": targetID,
    }
    var result any
    if err := c.doPost(ctx, "/session/"+c.sessionID+"/revert", reqBody, &result); err != nil {
        return fmt.Errorf("opencode rewind: %w", err)
    }
    return nil
}
```

#### 5B.4 Context Usage 查询（间接聚合）

OpenCode Server **无直接 context usage 端点**。通过聚合 message token 数据实现：

```go
// queryContextUsage aggregates token usage from all session messages.
// OpenCode stores per-message token counts but has no global context query.
func (c *ServerCommander) queryContextUsage(ctx context.Context) (map[string]any, error) {
    var messages []openCodeMessage
    if err := c.doGet(ctx, "/session/"+c.sessionID+"/message?limit=100", &messages); err != nil {
        return nil, fmt.Errorf("opencode context query: %w", err)
    }

    var totalInput, totalOutput, totalReasoning, totalCacheRead, totalCacheWrite int
    var model string
    for _, msg := range messages {
        if msg.Info.Role == "assistant" && msg.Info.Tokens != nil {
            totalInput += msg.Info.Tokens.Input
            totalOutput += msg.Info.Tokens.Output
            totalReasoning += msg.Info.Tokens.Reasoning
            totalCacheRead += msg.Info.Tokens.Cache.Read
            totalCacheWrite += msg.Info.Tokens.Cache.Write
            if msg.Info.Model != nil {
                model = msg.Info.Model.ProviderID + "/" + msg.Info.Model.ModelID
            }
        }
    }

    totalTokens := totalInput + totalOutput + totalReasoning + totalCacheRead + totalCacheWrite

    // Map to gateway's ContextUsageData structure.
    // NOTE: maxTokens and percentage are approximations — OpenCode doesn't expose
    // the model's context window limit via API. Use known model limits.
    return map[string]any{
        "totalTokens": totalTokens,
        "maxTokens":   0,    // unknown from API
        "percentage":  0,    // unknown from API
        "model":       model,
        "categories": []map[string]any{
            {"name": "Input tokens", "tokens": totalInput},
            {"name": "Output tokens", "tokens": totalOutput},
            {"name": "Reasoning tokens", "tokens": totalReasoning},
            {"name": "Cache read", "tokens": totalCacheRead},
            {"name": "Cache write", "tokens": totalCacheWrite},
        },
    }, nil
}

type openCodeMessage struct {
    Info struct {
        Role   string `json:"role"`
        Tokens *struct {
            Input     int `json:"input"`
            Output    int `json:"output"`
            Reasoning int `json:"reasoning"`
            Cache     struct {
                Read  int `json:"read"`
                Write int `json:"write"`
            } `json:"cache"`
        } `json:"tokens"`
        Cost  float64 `json:"cost"`
        Model *struct {
            ProviderID string `json:"providerID"`
            ModelID    string `json:"modelID"`
        } `json:"model"`
    } `json:"info"`
}
```

#### 5B.5 Model 切换（per-message 注入）

OpenCode 不支持全局 model 切换，而是每条 message 独立指定 model。

```go
// setModel stores the model reference for subsequent message requests.
// OpenCode uses per-message model selection, not a global session model.
func (c *ServerCommander) setModel(ctx context.Context, body map[string]any) (map[string]any, error) {
    providerID, _ := body["providerID"].(string)
    modelID, _ := body["modelID"].(string)
    // Also accept single "model" string: "provider/model"
    if model, ok := body["model"].(string); ok && providerID == "" {
        parts := strings.SplitN(model, "/", 2)
        if len(parts) == 2 {
            providerID, modelID = parts[0], parts[1]
        } else {
            modelID = model
        }
    }
    c.pendingModel = &ModelRef{ProviderID: providerID, ModelID: modelID}
    return map[string]any{"success": true, "model": modelID}, nil
}

// PendingModel returns the stored model for injection into message requests.
// Called by the OpenCode Server worker's Input/prompt method.
func (c *ServerCommander) PendingModel() *ModelRef {
    return c.pendingModel
}
```

#### 5B.6 Permission 模式切换

```go
// setPermissionMode updates session permission rules via PATCH.
func (c *ServerCommander) setPermissionMode(ctx context.Context, body map[string]any) (map[string]any, error) {
    mode, _ := body["mode"].(string)

    // Map Gateway permission modes to OpenCode permission rulesets.
    var rules []map[string]any
    switch mode {
    case "bypassPermissions":
        rules = []map[string]any{
            {"permission": "*", "action": "allow", "pattern": "*"},
        }
    default:
        rules = []map[string]any{} // default: ask for everything
    }

    if err := c.doPatch(ctx, "/session/"+c.sessionID, map[string]any{
        "permission": rules,
    }); err != nil {
        return nil, fmt.Errorf("opencode set permission: %w", err)
    }
    return map[string]any{"success": true, "mode": mode}, nil
}
```

#### 5B.7 Rewind 增强（超越 CC）

OpenCode 的 revert API 比 Claude Code 的 `/rewind` 更强大：

```go
// OpenCode revert response structure
type RevertResponse struct {
    Revert struct {
        MessageID string `json:"messageID"`
        PartID    string `json:"partID,omitempty"`
        Snapshot  string `json:"snapshot"` // File system snapshot before revert
        Diff      string `json:"diff"`     // Diff of changes being undone
    } `json:"revert"`
    Summary struct {
        Additions int `json:"additions"`
        Deletions int `json:"deletions"`
        Files     int `json:"files"`
    } `json:"summary"`
}
```

**对比**：

| 能力 | Claude Code `/rewind` | OpenCode `revert` |
|------|----------------------|-------------------|
| 对话回退 | ✅ | ✅ |
| 文件系统回退 | ❌ | ✅ (snapshot + diff) |
| 精确到 part | ❌ | ✅ (partID 参数) |
| 恢复 (undo revert) | ❌ | ✅ (`/unrevert`) |
| diff 统计 | ❌ | ✅ (additions/deletions/files) |

#### 5B.8 HTTP 辅助方法

```go
func (c *ServerCommander) doGet(ctx context.Context, path string, result any) error {
    return c.doRequest(ctx, http.MethodGet, path, nil, result)
}

func (c *ServerCommander) doPost(ctx context.Context, path string, body any, result any) error {
    return c.doRequest(ctx, http.MethodPost, path, body, result)
}

func (c *ServerCommander) doPatch(ctx context.Context, path string, body any) error {
    return c.doRequest(ctx, http.MethodPatch, path, body, nil)
}

func (c *ServerCommander) doDelete(ctx context.Context, path string) error {
    return c.doRequest(ctx, http.MethodDelete, path, nil, nil)
}

func (c *ServerCommander) doRequest(ctx context.Context, method, path string, body any, result any) error {
    var bodyReader io.Reader
    if body != nil {
        data, err := json.Marshal(body)
        if err != nil {
            return err
        }
        bodyReader = bytes.NewReader(data)
    }

    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    if c.authToken != "" {
        req.SetBasicAuth("opencode", c.authToken)
    }

    resp, err := c.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        return fmt.Errorf("opencode API %s %s: HTTP %d", method, path, resp.StatusCode)
    }
    if result != nil {
        return json.NewDecoder(resp.Body).Decode(result)
    }
    return nil
}
```

#### 5B.9 Worker adapter 集成

文件：`internal/worker/opencodeserver/worker.go`

```go
// 在现有 Worker struct 中嵌入 ServerCommander
type Worker struct {
    *base.BaseWorker
    cmd *ServerCommander
    // ... existing fields ...
}

// SendControlRequest delegates to ServerCommander (implements ControlRequester).
func (w *Worker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
    if w.cmd == nil {
        return nil, fmt.Errorf("opencode server: commander not initialized")
    }
    return w.cmd.SendControlRequest(ctx, subtype, body)
}

// Compact implements WorkerCommander.
func (w *Worker) Compact(ctx context.Context, args map[string]any) error {
    return w.cmd.Compact(ctx, args)
}

// Clear implements WorkerCommander.
// NOTE: OpenCode clear creates a new session, changing the session ID.
func (w *Worker) Clear(ctx context.Context) error {
    return w.cmd.Clear(ctx)
}

// Rewind implements WorkerCommander.
func (w *Worker) Rewind(ctx context.Context, targetID string) error {
    return w.cmd.Rewind(ctx, targetID)
}
```

#### 5B.10 handleWorkerCommand 扩展（适配 OpenCode Server）

Gateway handler 需要区分两种 Worker 类型的不同执行路径：

```go
// In handleWorkerCommand, Path A (passthrough) 分支扩展:

if cmd.IsPassthrough() {
    // Claude Code: send as stdin text
    // OpenCode Server: some commands need API calls instead
    if commander, ok := w.(WorkerCommander); ok {
        switch cmd {
        case events.StdioCompact:
            return commander.Compact(ctx, extra)
        case events.StdioClear:
            if err := commander.Clear(ctx); err != nil {
                return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear: %v", err)
            }
            // Notify session of new state (OpenCode clear changes session ID)
            // ... emit state event ...
            return nil
        case events.StdioRewind:
            return commander.Rewind(ctx, "")  // empty = last exchange
        case events.StdioEffort:
            return h.sendErrorf(ctx, env, events.ErrCodeNotSupported,
                "effort not supported by this worker type")
        default:
            // model, commit — still passthrough via Input()
        }
    }
    // Default: send as Input() text
    content := "/" + string(cmd)
    if args != "" {
        content += " " + args
    }
    return w.Input(ctx, content, nil)
}
```

---

### Phase 6: Messaging 平台渲染

#### 6.1 Feishu Context Usage Card

收到 `context_usage` 事件时渲染为 CardKit v2 互动卡片：

```
┌──────────────────────────────────┐
│  📊 Context Usage     38%       │
│  ████████░░░░░░░░░░░░           │
│                                  │
│  System Prompt      603 tokens  │
│  System Tools     4,335 tokens  │
│  Custom Agents    3,692 tokens  │
│  Memory Files     3,351 tokens  │
│  Skills           9,073 tokens  │
│  Messages        55,228 tokens  │
│  ─────────────────────────────  │
│  Autocompact buffer 33,000      │
│  Free space        90,718       │
│  ─────────────────────────────  │
│  Model: claude-sonnet-4-20250514│
│  Memory: 5 | MCP: 124          │
│  Agents: 157 | Skills: 147     │
└──────────────────────────────────┘
```

#### 6.2 Slack Context Block

```
📊 *Context Usage* — 38% (76,282 / 200,000)
████████░░░░░░░░░░░░

• System: 5K | Memory: 3K | Skills: 9K
• Messages: 55K | Autocompact buffer: 33K
• Free: 91K | Model: sonnet-4
• MCP: 124 | Agents: 157 | Skills: 147
```

#### 6.3 MCP Status Card (Feishu)

```
┌──────────────────────────────────┐
│  🔌 MCP Server Status           │
│                                  │
│  ✅ chrome-devtools     connected│
│  ✅ claude-mem          connected│
│  ✅ context7            connected│
│  ✅ playwright          connected│
│  ✅ slack               connected│
│  ... and 7 more                  │
│                                  │
│  12 servers total                │
└──────────────────────────────────┘
```

#### 6.4 Passthrough 命令反馈

**Claude Code Worker**：
- **compact**：CC 自带 assistant summary 回复，走正常流式管道，无需额外渲染
- **clear**：CC 发出 `system/init`，平台侧发简短确认消息
- **model**：CC 输出 `<local-command-stdout>Set model to xxx</local-command-stdout>`，解析后确认
- **effort**：同 model，CC 输出确认消息
- **rewind**：CC 原地回退对话，result 正常返回（不回退文件）
- **commit**：CC 触发完整 commit workflow（git status → diff → commit），走正常 assistant 管道

**OpenCode Server Worker**：
- **compact**：POST /summarize 后通过 SSE 接收 `session.compacted` 事件，平台侧发确认消息
- **clear**：session 重建后发确认消息（注意 session ID 变化，需通知客户端）
- **model**：无即时反馈，下次 message 使用的 model 可通过 GET /session 确认
- **effort**：**不支持**，平台侧直接返回"当前 Worker 不支持此命令"
- **rewind**：返回 `{additions, deletions, files}` diff 统计，渲染为文件变更卡片（**超越 CC**）
- **commit**：作为 prompt 发送，走正常 assistant 流式管道

---

## 改动范围

### Claude Code Worker (Phase 3-4)

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `pkg/events/events.go` | 修改 | +ContextUsage/MCPStatus/WorkerCmd EventType, +ContextUsageData/MCPStatusData struct, +WorkerStdioCommand type (10 consts) +IsPassthrough(), +WorkerCommandData struct |
| `internal/messaging/control_command.go` | 修改 | +ParseWorkerCommand, +WorkerCommandResult, +10 项 slash 映射 +14 项 NL 映射 |
| `internal/gateway/handler.go` | 修改 | +handleWorkerCommand (路径 A + 路径 B + OpenCode 路径), +Handle() WorkerCmd case |
| `internal/worker/claudecode/control.go` | 修改 | +SendControlRequest, +DeliverResponse, +pendingRequests |
| `internal/worker/claudecode/worker.go` | 修改 | +SendControlRequest (delegate to control), +readOutput 扩展 control_response 路由 |
| `internal/worker/claudecode/mapper.go` | 修改 | +mapContextUsageResponse, +mapMCPStatusResponse |
| `internal/messaging/feishu/adapter.go` | 修改 | +context_usage/mcp_status 事件渲染 |
| `internal/messaging/slack/adapter.go` | 修改 | +context_usage/mcp_status 事件渲染 |
| `scripts/test_cc_context.py` | 已存在 | 10/10 验证通过 |

### OpenCode Server Worker (Phase 5B)

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/worker/opencodeserver/commands.go` | **新增** | +ServerCommander, +HTTP 辅助方法, +queryContextUsage, +setModel, +setPermissionMode, +Compact, +Clear, +Rewind |
| `internal/worker/opencodeserver/worker.go` | 修改 | 嵌入 ServerCommander, 实现 ControlRequester + WorkerCommander 接口 |
| `internal/gateway/handler.go` | 修改 | handleWorkerCommand passthrough 分支增加 WorkerCommander 类型断言 |

---

## 测试策略

### 单元测试

#### Claude Code Worker

| 测试 | 文件 | 覆盖 |
|------|------|------|
| ParseWorkerCommand slash 映射 (10 项) | `control_command_test.go` | `/compact`, `/clear`, `/model`, `/effort`, `/rewind`, `/commit`, `/context`, `/mcp`, `/model sonnet`, `/perm bypass` |
| ParseWorkerCommand NL 映射 | `control_command_test.go` | `压缩`, `清空`, `上下文`, `回退`, `提交` 等 |
| ParseWorkerCommand 与 ParseControlCommand 不冲突 | `control_command_test.go` | `/reset` 走 control, `/compact` 走 worker |
| handleWorkerCommand passthrough | `handler_test.go` | mock worker Input 验证收到 `/compact`, `/model xxx` |
| handleWorkerCommand control_request | `handler_test.go` | mock ControlRequester, 验证 context_usage/mcp_status broadcast |
| handleWorkerCommand 非 active 状态 | `handler_test.go` | 返回 ErrCodeInvalidState |
| SendControlRequest | `worker_test.go` | mock stdin → DeliverResponse → pending map |
| mapContextUsageResponse | `mapper_test.go` | CC JSON → ContextUsageData 全字段 |
| mapMCPStatusResponse | `mapper_test.go` | CC JSON → MCPStatusData 全字段 |
| DeliverResponse 路由 | `control_test.go` | pending request_id 匹配 |
| IsPassthrough | `events_test.go` | passthrough 返回 true, control request 返回 false |

#### OpenCode Server Worker

| 测试 | 文件 | 覆盖 |
|------|------|------|
| ServerCommander.queryContextUsage | `commands_test.go` | mock HTTP → 聚合 message tokens → ContextUsageData |
| ServerCommander.Compact | `commands_test.go` | mock HTTP POST /summarize |
| ServerCommander.Clear | `commands_test.go` | mock HTTP DELETE + POST → session ID 更新 |
| ServerCommander.Rewind | `commands_test.go` | mock HTTP POST /revert → diff 统计 |
| ServerCommander.setModel | `commands_test.go` | 存储 pendingModel → 后续 message 注入 |
| ServerCommander.setPermissionMode | `commands_test.go` | mock HTTP PATCH → permission ruleset |
| WorkerCommander 分支路由 | `handler_test.go` | mock WorkerCommander → Compact/Clear/Rewind 路径 |
| effort 返回 ErrCodeNotSupported | `handler_test.go` | OpenCode Server worker → effort → 错误 |

### 集成测试

| 测试 | Worker | 说明 |
|------|--------|------|
| E2E compact | CC | handleWorkerCommand(compact) → 验证 context 下降 |
| E2E context | CC | handleWorkerCommand(context) → 验证返回完整结构 |
| E2E mcp_status | CC | handleWorkerCommand(mcp) → 验证 server 列表 |
| E2E set_model | CC | handleWorkerCommand(model sonnet) → 验证 model 变更 |
| E2E effort | CC | handleWorkerCommand(effort high) → 验证命令通过 |
| E2E compact | OpenCode Server | POST /summarize → SSE compacted 事件 |
| E2E rewind | OpenCode Server | POST /revert → diff + file undo |
| E2E context | OpenCode Server | GET messages → token 聚合 → ContextUsageData |
| E2E effort | OpenCode Server | 验证返回 ErrCodeNotSupported |
| 非 CC worker | noop | handleWorkerCommand(context) → 验证 ErrCodeNotSupported |
| compact 后 context | CC | compact → context query → 验证 totalTokens 下降 |

---

## 实施顺序

```
Phase 1   类型定义（pkg/events）                          ← 无外部依赖
Phase 2   Messaging 解析（control_command.go）             ← 无外部依赖
Phase 3   CC Worker ControlRequester（claudecode/）        ← 依赖 Phase 1
Phase 4   CC Worker readOutput 扩展（claudecode/）         ← 依赖 Phase 3
Phase 5   Gateway Handler + handleWorkerCommand            ← 依赖 Phase 1 + 4
Phase 5B  OpenCode Server Worker commands.go               ← 依赖 Phase 1 (可与 Phase 5 并行)
Phase 6   Messaging 渲染（feishu/slack adapter.go）        ← 依赖 Phase 5
```

Phase 1-2 无依赖可先行。Phase 3-4 是 CC Worker 层改动。Phase 5 是 Gateway 集成。
Phase 5B 是 OpenCode Server Worker 独立改动，可与 Phase 5 并行开发。
Phase 6 是 UI 渲染，最后执行。

**优先级**：CC Worker (Phase 1-5) → OpenCode Server Worker (Phase 5B) → 渲染 (Phase 6)。
Passthrough 命令（6 项）在 Phase 5 后即可工作。Control Requests（4 项）需 Phase 3-4 完成。

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| CC idle 时 `get_context_usage` 不响应 | 中 | 挂起 | 验证确认 idle 时也可查询；30s ctx 超时兜底 |
| `/compact` 触发多轮 API 调用 | 低 | 成本 | CC 内部保护，compact 只在有足够上下文时触发 summary |
| compact/clear 被识别为普通用户消息泄露到对话 | 低 | UX | CC 内部 parseSlashCommand 会识别，不产生普通回复 |
| pendingRequests map 泄漏 | 低 | 内存泄漏 | ctx cancel 时清理；DeliverResponse 后 delete |
| 多 Worker 并发查询 | 低 | 响应串路 | request_id 前缀 `ctx_` + UUID，per-request channel |
| `/commit` 在自动化上下文误触发 | 中 | git 污染 | 谨慎暴露给消息平台，或需二次确认 |
| `/rewind` CC 只回退对话不回退文件 | 低 | 用户困惑 | 渲染时明确提示 rewind 范围 |
| OpenCode clear 重建 session ID 变化 | 高 | 连接断开 | clear 后需通知 Gateway 更新 session 映射 |
| OpenCode context usage 无总量/上限 | 中 | 数据不完整 | 渲染时标注 "累计 token"，不显示百分比 |
| OpenCode revert 文件系统回退 | 低 | 意外丢失文件 | 确认 revert 是有快照的，可 unrevert 恢复 |
| OpenCode Server API 版本变化 | 中 | 接口失效 | 基于 OpenAPI spec 生成类型；API 版本检测 |
| 两类 Worker 共用 handleWorkerCommand 路径 | 低 | 逻辑耦合 | WorkerCommander 接口隔离，handler 只做分发 |

---

## 未来扩展

1. **自动 context 监控**：Bridge 层定期 SendControlRequest("get_context_usage")，超过阈值自动 compact 或通知用户
2. **Context budget**：session 配置 `max_context_pct`，到达阈值自动触发 compact
3. **Done 事件附加 context_pct**：每轮结束查询 context usage，注入 DoneData.Stats
4. **Admin API 暴露**：`GET /api/sessions/:id/context` 调用 SendControlRequest
6. **更多 CC slash command**：CC 原生 slash command（如 `/cost`、`/bug`、`/doctor`、`/help`）均可通过此 passthrough 机制支持
7. **`/commit` 安全限制**：对 commit 类命令增加二次确认或限制特定场景
8. **权限模式动态切换**：结合 platform 用户角色，自动 set_permission_mode
9. **OpenCode revert diff 渲染**：利用 OpenCode revert 返回的 diff 统计，在 Feishu/Slack 展示文件变更明细
10. **OpenCode context usage 增强**：缓存 model context window limit，计算近似百分比
11. **OpenCode MCP tool 状态**：`GET /experimental/tool` 返回的工具列表映射为 MCPStatusData
