---
type: spec
tags: [project/HotPlex, worker/stdio, worker/claudecode, messaging, gateway]
date: 2026-04-19
status: draft
progress: 0
priority: high
estimated_hours: 12
---

# Worker Session Control Spec

## 概述

新增 Worker **stdio 直达**控制能力：context 查询、原地 compaction、原地清空。
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
| 协议层 | AEP Control Event | Claude Code stream-json stdio |
| Worker 接口 | `Terminate()` `Kill()` | `Input()` `SendControlRequest()` |
| 适用 Worker | 所有（通用生命周期） | 仅 Claude Code（协议相关） |

### 动机

| 现状 | 问题 |
|------|------|
| `/reset` = 杀进程 + 新建 | 延迟 5-10s 冷启动，丢失 session 连续性 |
| 无 context 可见性 | 用户无法感知 token 消耗，直到 API 报错 |
| 无法原地压缩 | 对话变长后只能全量 reset，浪费已有上下文 |

### 验证基础

通过 `scripts/test_cc_context.py` 验证了三项能力在 Claude Code stream-json 协议中可用：

```
Context Usage: 76,282 / 200,000 tokens (38%)
Categories: System prompt (603), Tools (4,335), Memory (3,351),
            Skills (9,073), Messages (55,228), Autocompact buffer (33,000)
/compact → 原地执行成功，无需重启进程
/clear   → 同理可用（验证脚本 skip）
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
│  "/compact"  "/clear"  "/context"                            │
│                    →  ParseWorkerCommand()   ← 新增          │
│                         → handleWorkerCommand()              │
│                             → Worker 直接 stdin 交互         │  ← 本 spec
│                             (不经过 SM 状态机)               │
└──────────────────────────────────────────────────────────────┘
```

### 三项能力的数据流

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

| 命令 | Slash 触发 | 自然语言 | stdin 协议 | 返回方式 | 进程影响 |
|------|-----------|---------|-----------|---------|---------|
| compact | `/compact` | `压缩` `精简` | user message `/compact` | assistant + result | 原地 |
| clear | `/clear` | `清空` `清屏` | user message `/clear` | system/init | 原地 |
| context | `/context` | `上下文` `容量` `token` | control_request `get_context_usage` | control_response | 只读 |

---

## 详细设计

### Phase 1: 事件类型与数据结构

#### 1.1 新增 EventType

文件：`pkg/events/events.go`

```go
const (
    // ... existing ...
    ContextUsage EventType = "context_usage"  // Worker context usage report
)
```

**不新增 ControlAction**。compact/clear/context 不是生命周期控制，不走 ControlData。

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
    StdioCompact      WorkerStdioCommand = "compact"       // In-place context compaction
    StdioClear        WorkerStdioCommand = "clear"          // In-place conversation clear
    StdioContextUsage WorkerStdioCommand = "context_usage"  // Query context usage (read-only)
)
```

#### 1.4 WorkerCommandData 事件载荷

文件：`pkg/events/events.go`

```go
// WorkerCommandData is the payload for worker stdio command events.
// Carried in AEP Event.Data when a client requests a worker-level operation.
type WorkerCommandData struct {
    Command WorkerStdioCommand `json:"command"`
    Args    string             `json:"args,omitempty"` // e.g. compact instructions
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
}

// workerSlashMap maps slash commands to worker stdio commands.
var workerSlashMap = map[string]WorkerCommandResult{
    "/compact": {events.StdioCompact, "compact"},
    "/clear":   {events.StdioClear, "clear"},
    "/context": {events.StdioContextUsage, "context"},
}

// workerNLMap maps natural language triggers to worker stdio commands.
var workerNLMap = map[string]WorkerCommandResult{
    "压缩":   {events.StdioCompact, "compact"},
    "精简":   {events.StdioCompact, "compact"},
    "清空":   {events.StdioClear, "clear"},
    "清屏":   {events.StdioClear, "clear"},
    "上下文": {events.StdioContextUsage, "context"},
    "容量":   {events.StdioContextUsage, "context"},
    "token":  {events.StdioContextUsage, "context"},
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
) error {
    info, err := h.validateOwner(ctx, env)
    if err != nil {
        return err
    }
    if info.State != events.StateRunning {
        return h.sendErrorf(ctx, env, events.ErrCodeInvalidState,
            "worker command requires running session, current: %s", info.State)
    }

    w := h.sm.GetWorker(env.SessionID)
    if w == nil {
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "no worker attached")
    }

    switch cmd {
    case events.StdioCompact:
        // Passthrough: send "/compact" as user message to stdin
        if err := w.Input(ctx, "/compact", nil); err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "compact: %v", err)
        }
        // Result flows back through normal assistant → result pipeline

    case events.StdioClear:
        // Passthrough: send "/clear" as user message to stdin
        if err := w.Input(ctx, "/clear", nil); err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear: %v", err)
        }
        // CC emits system/init, normal event pipeline handles it

    case events.StdioContextUsage:
        // Control request: query context via CC's get_context_usage protocol
        type ContextQuerier interface {
            QueryContextUsage(ctx context.Context) (*events.ContextUsageData, error)
        }
        cq, ok := w.(ContextQuerier)
        if !ok {
            return h.sendErrorf(ctx, env, events.ErrCodeNotSupported,
                "worker type %s does not support context query", w.Type())
        }
        data, err := cq.QueryContextUsage(ctx)
        if err != nil {
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "context query: %v", err)
        }
        // Broadcast result to session subscribers
        respEnv := events.NewEnvelope(
            aep.NewID(), env.SessionID,
            h.hub.NextSeq(env.SessionID),
            events.ContextUsage, data,
        )
        return h.hub.SendToSession(ctx, respEnv)

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

#### 4.1 Claude Code Worker — ContextQuerier

文件：`internal/worker/claudecode/worker.go`

```go
// QueryContextUsage sends get_context_usage control request to Claude Code
// and returns the parsed response. Implements ContextQuerier interface.
func (w *Worker) QueryContextUsage(ctx context.Context) (*events.ContextUsageData, error) {
    if w.control == nil {
        return nil, fmt.Errorf("claudecode: control handler not initialized")
    }

    resp, err := w.control.SendControlRequest(ctx, "get_context_usage", nil)
    if err != nil {
        return nil, fmt.Errorf("claudecode: context query: %w", err)
    }

    return mapContextUsageResponse(resp), nil
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

#### 4.5 compact / clear — 无需 Worker 层改动

`compact` 和 `clear` 通过 `worker.Input()` 发送，走的是已有的 `SendUserMessage` 路径。
Claude Code 将其解析为 slash command 并原地执行。结果通过正常的 `assistant` → `result` 流式管道返回。

Worker 层**零改动**即可支持 compact 和 clear。

---

### Phase 5: 非 Claude Code Worker 兼容

#### 5.1 接口隔离

```go
// ContextQuerier is implemented by workers that support context usage queries.
// Unsupported workers fail the type assertion with ErrCodeNotSupported.
type ContextQuerier interface {
    QueryContextUsage(ctx context.Context) (*events.ContextUsageData, error)
}

// StdioCommander is implemented by workers that support in-place commands.
type StdioCommander interface {
    Input(ctx context.Context, content string, metadata map[string]any) error
    // Input already exists on the Worker interface — no new method needed
}
```

#### 5.2 Worker 能力矩阵

| Worker | compact | clear | context_usage | 备注 |
|--------|---------|-------|---------------|------|
| claudecode | stdin passthrough | stdin passthrough | control request | 完整支持 |
| opencodecli | stdin passthrough | stdin passthrough | N/A | 取决于 opencode 协议 |
| opencodeserver | N/A | N/A | N/A | HTTP 协议 |
| acpx | N/A | N/A | N/A | stdio 协议不同 |
| pi | N/A | N/A | N/A | 私有协议 |
| noop | N/A | N/A | N/A | 测试用 |

compact/clear 走 `worker.Input()`（Worker interface 已有），理论上支持所有 stdio worker。
context_usage 需要各 worker 独立实现 `ContextQuerier` 接口。

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

#### 6.3 Compact/Clear 反馈

- **compact**：CC 自带 assistant summary 回复，走正常流式管道，无需额外渲染
- **clear**：CC 发出 `system/init`，平台侧发简短确认消息

---

## 改动范围

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `pkg/events/events.go` | 修改 | +ContextUsage EventType, +ContextUsageData struct, +WorkerStdioCommand type, +WorkerCommandData struct |
| `internal/messaging/control_command.go` | 修改 | +ParseWorkerCommand, +WorkerCommandResult, +映射表 |
| `internal/gateway/handler.go` | 修改 | +handleWorkerCommand, +Handle() 新 case |
| `internal/worker/claudecode/control.go` | 修改 | +SendControlRequest, +DeliverResponse, +pendingRequests |
| `internal/worker/claudecode/worker.go` | 修改 | +QueryContextUsage, +readOutput 扩展 control_response 路由 |
| `internal/worker/claudecode/mapper.go` | 修改 | +mapContextUsageResponse |
| `internal/messaging/feishu/adapter.go` | 修改 | +context_usage 事件渲染 |
| `internal/messaging/slack/adapter.go` | 修改 | +context_usage 事件渲染 |
| `scripts/test_cc_context.py` | 已存在 | 验证脚本 |

---

## 测试策略

### 单元测试

| 测试 | 文件 | 覆盖 |
|------|------|------|
| ParseWorkerCommand 映射 | `control_command_test.go` | `/compact`, `压缩`, `/context`, `上下文`, `token` |
| ParseWorkerCommand 与 ParseControlCommand 不冲突 | `control_command_test.go` | `/reset` 走 control, `/compact` 走 worker |
| handleWorkerCommand compact | `handler_test.go` | mock worker Input 验证收到 `/compact` |
| handleWorkerCommand context | `handler_test.go` | mock ContextQuerier, 验证 broadcast ContextUsage |
| handleWorkerCommand 非 running 状态 | `handler_test.go` | 返回 ErrCodeInvalidState |
| QueryContextUsage | `worker_test.go` | mock control SendControlRequest → DeliverResponse |
| mapContextUsageResponse | `mapper_test.go` | CC JSON → ContextUsageData 全字段 |
| DeliverResponse 路由 | `control_test.go` | pending request_id 匹配 |

### 集成测试

| 测试 | 说明 |
|------|------|
| E2E compact | CC worker → handleWorkerCommand(compact) → 验证 context 下降 |
| E2E context | CC worker → handleWorkerCommand(context) → 验证返回完整结构 |
| 非 CC worker | noop worker → handleWorkerCommand(context) → 验证 ErrCodeNotSupported |
| compact 后 context | compact → context query → 验证 totalTokens 下降 |

---

## 实施顺序

```
Phase 1  类型定义（pkg/events）     ← 无外部依赖
Phase 2  Messaging 解析（control_command.go） ← 无外部依赖
Phase 3  Worker 层（claudecode/）    ← 依赖 Phase 1
Phase 4  Gateway Handler            ← 依赖 Phase 1 + 3
Phase 5  Messaging 渲染             ← 依赖 Phase 4
```

Phase 1-2 无依赖可先行。Phase 3 和 4 顺序依赖。Phase 5 最后。

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| CC idle 时 `get_context_usage` 不响应 | 中 | 挂起 | 查询前检查 state==running；30s ctx 超时 |
| `/compact` 触发多轮 API 调用 | 低 | 成本 | CC 内部保护，compact 只在有足够上下文时触发 summary |
| compact/clear 被识别为普通用户消息泄露到对话 | 低 | UX | CC 内部 parseSlashCommand 会识别，不产生普通回复 |
| pendingRequests map 泄漏 | 低 | 内存泄漏 | ctx cancel 时清理；DeliverResponse 后 delete |
| 多 Worker 并发查询 | 低 | 响应串路 | request_id 前缀 `ctx_` + UUID，per-request channel |

---

## 未来扩展

1. **自动 context 监控**：Bridge 层定期 QueryContextUsage，超过阈值自动 compact 或通知用户
2. **Context budget**：session 配置 `max_context_pct`，到达阈值自动触发 compact
3. **Done 事件附加 context_pct**：每轮结束查询 context usage，注入 DoneData.Stats
4. **多 Worker 支持**：opencodecli 实现 ContextQuerier（如果协议支持）
5. **Admin API 暴露**：`GET /api/sessions/:id/context` 调用 QueryContextUsage
6. **更多 CC slash command**：`/model`、`/cost` 等 CC 原生 slash command 均可通过此 passthrough 机制支持
