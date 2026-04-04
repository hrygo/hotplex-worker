---
type: spec
tags:
  - project/HotPlex
  - worker/acpx
  - architecture/integration
---

# ACPX Worker 集成规格

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
| OpenCode CLI | stdio | json-lines (OpenCode 专用) | persistent |
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

## 2. 协议类型（ACP NDJSON）

> ⚠️ 以下协议结构基于 acpx README 摘要和 ACP 协议规范，**实际 NDJSON 格式待验证**。验证方法：`acpx claude "hello" --format json` 观察输出。

### 2.1 协议说明

ACP 不是标准的 JSON-RPC 2.0，而是一个**会话层 NDJSON 协议**：

- **请求消息**：通过 **stdin** 发送（JSON 单行）
- **响应/事件流**：通过 **stdout** 接收（NDJSON 多行）
- **格式**：每行一个 JSON 对象，换行符 `\n` 分隔

### 2.2 消息分类

| 消息类型 | 方向 | 格式 | 说明 |
|----------|------|------|------|
| Prompt 请求 | Gateway → acpx | JSON 单行 | stdin 写入 |
| 事件流 | acpx → Gateway | NDJSON 多行 | stdout 读取 |

### 2.3 ACP 事件结构（待验证）

> ⚠️ 以下结构基于 ACP 协议文档，**实际字段名和层级待验证**。

```go
// ACP 事件（NDJSON 输出，每行一个）
type Event struct {
    EventVersion int               `json:"eventVersion"` // 协议版本号
    SessionID    string          `json:"sessionId"`   // 会话 ID
    RequestID    string          `json:"requestId"`    // 请求 ID（用于配对响应）
    Seq          int             `json:"seq"`          // 序列号
    Stream       string          `json:"stream"`      // "prompt" | "result" | "error"
    Type         string          `json:"type"`        // 事件类型
    Data         json.RawMessage `json:"data"`        // 事件载荷
}

// Stream 枚举
const (
    ACPStreamPrompt  = "prompt"  // 流式输出进行中
    ACPStreamResult  = "result"  // 最终结果
    ACPStreamError   = "error"   // 错误流
)

// Event Type 枚举（待验证）
const (
    ACPTypeMessage   = "message"    // 文本消息
    ACPTypeDelta    = "delta"      // 流式增量
    ACPTypeThinking  = "thinking"   // 思考过程
    ACPTypeToolCall  = "tool_call"  // 工具调用
    ACPTypeToolResult = "tool_result" // 工具结果
    ACPTypeDone      = "done"       // 执行完成
    ACPTypeError     = "error"      // 错误
    ACPTypeState     = "state"      // 状态变更
)

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

### 5.2 NDJSON 请求格式（待验证）

> ⚠️ 以下为假设格式，实际请求格式待通过 `acpx --format json` 观察验证。

```json
{"prompt":"Fix the bug in main.go","metadata":{}}
```

### 5.3 环境变量注入

acpx 透传以下环境变量给底层 Agent：

```bash
# API Key 通过环境变量注入（不通过 CLI 参数）
ANTHROPIC_API_KEY=sk-... acpx claude -s <session_id> --format json
OPENAI_API_KEY=sk-... acpx opencode -s <session_id> --format json
GEMINI_API_KEY=... acpx gemini -s <session_id> --format json
```

---

## 6. 输出格式（acpx → Gateway）

> ⚠️ **待验证**：以下 NDJSON 格式基于 ACP 协议规范推断，**实际 acpx 输出格式需通过测试验证**。
>
> **验证方法**：
> ```bash
> # 运行 acpx 并观察输出
> acpx claude "What is 2+2?" --format json
> # 检查 stdout 输出的 NDJSON 结构和字段名
> ```
>
> **关键验证点**：
> - 字段命名风格：`camelCase` 还是 `snake_case`
> - `stream` 字段是否存在
> - `data` 字段结构是否与 ACP 规范一致
> - 工具调用事件的参数格式

### 6.1 NDJSON 事件流

acpx `--format json` 输出每行一个 NDJSON 事件：

```json
{"eventVersion":1,"sessionId":"acp_abc123","requestId":"req_42","seq":1,"stream":"prompt","type":"thinking","data":{"content":"Let me analyze..."}}
{"eventVersion":1,"sessionId":"acp_abc123","requestId":"req_42","seq":2,"stream":"prompt","type":"tool_call","data":{"id":"call_1","name":"read_file","input":{"path":"main.go"}}}
{"eventVersion":1,"sessionId":"acp_abc123","requestId":"req_42","seq":3,"stream":"prompt","type":"message","data":{"content":"Reading main.go..."}}
{"eventVersion":1,"sessionId":"acp_abc123","requestId":"req_42","seq":4,"stream":"prompt","type":"tool_call","data":{"id":"call_2","name":"edit_file","input":{}}}
{"eventVersion":1,"sessionId":"acp_abc123","requestId":"req_42","seq":5,"stream":"result","type":"done","data":{"success":true,"stats":{"duration_ms":5200,"tool_calls":2}}}
```

### 6.2 关键事件示例

**thinking（思考过程）**：

```json
{
  "eventVersion": 1,
  "type": "thinking",
  "data": {
    "content": "I need to understand the codebase structure first."
  }
}
```

**tool_call（工具调用）**：

```json
{
  "type": "tool_call",
  "data": {
    "id": "call_123",
    "name": "read_file",
    "input": { "path": "/app/main.go" }
  }
}
```

**tool_result（工具结果）**：

```json
{
  "type": "tool_result",
  "data": {
    "tool_call_id": "call_123",
    "content": "file contents..."
  }
}
```

**done（执行完成）**：

```json
{
  "stream": "result",
  "type": "done",
  "data": {
    "success": true,
    "stats": {
      "duration_ms": 5200,
      "tool_calls": 3,
      "total_cost_usd": 0.05
    }
  }
}
```

**error（错误）**：

```json
{
  "stream": "error",
  "type": "error",
  "data": {
    "code": "TOOL_PERMISSION_DENIED",
    "message": "Permission denied for tool: exec"
  }
}
```

---

## 7. 事件映射（ACP → AEP）

> ⚠️ **待验证**：以下映射基于 ACP 协议规范推断，**实际 ACP 事件字段可能与示例不同**。
>
> **实现策略**：
> 1. 先实现基于当前假设的映射
> 2. 通过集成测试验证实际 acpx 输出
> 3. 根据测试结果调整字段名和层级
> 4. 补充缺失的事件类型处理

### 7.1 完整映射表

| ACP Event | AEP Event Kind | 说明 | 实现 |
|-----------|---------------|------|------|
| `thinking` | `reasoning` | 思考过程 | ✅ |
| `message` (文本) | `message` | 完整消息 | ✅ |
| `delta` | `message.delta` | 流式增量 | ✅ |
| `tool_call` | `tool_call` | 工具调用 | ✅ |
| `tool_result` | `tool_result` | 工具结果 | ✅ |
| `done` (success) | `done { success: true }` | 执行成功 | ✅ |
| `done` (error) | `error` + `done { success: false }` | 执行错误 | ✅ |
| `error` | `error` | 错误事件 | ✅ |
| `permission_request` | `permission_request` | 权限请求 | ⚠️ P1 |
| `system` | `state` | 系统状态 | ⚠️ P1 |

### 7.2 AEP Input → ACP Prompt 映射

```go
func Bridge_AEPInput_To_ACP(env *events.Envelope) *Request {
    data := env.Data.(*events.InputData)
    return &Request{
        JSONRPC: "2.0",
        ID:      []byte(`"` + env.ID + `"`),
        Method:  "session/prompt",
        Params: json.RawMessage(fmt.Sprintf(
            `{"sessionId":"%s","prompt":%s,"metadata":%s}`,
            env.SessionID,
            escapeJSONString(data.Content),
            marshalJSON(data.Metadata),
        )),
    }
}
```

### 7.3 ACP Event → AEP Envelope 映射

```go
func Bridge_ACPEvent_To_AEP(ev *Event) (*events.Envelope, error) {
    switch ev.Type {
    case "thinking":
        return makeEnvelope(ev, events.Reasoning, &events.ReasoningData{
            Text:       getDataString(ev, "content"),
            Visibility: "default",
        })

    case "message":
        return makeEnvelope(ev, events.Message, &events.MessageData{
            Role:    "assistant",
            Content: getDataString(ev, "content"),
        })

    case "delta":
        deltaType := getDataString(ev, "type") // "text" | "code" | "image"
        return makeEnvelope(ev, events.MessageDelta, &events.DeltaData{
            Type: deltaType,
            Text: getDataString(ev, "content"),
        })

    case "tool_call":
        return makeEnvelope(ev, events.ToolCall, &events.ToolCallData{
            ID:     getDataString(ev, "id"),
            Name:   getDataString(ev, "name"),
            Status: "pending",
            Args:   getDataAny(ev, "input"),
        })

    case "tool_result":
        return makeEnvelope(ev, events.ToolResult, &events.ToolResultData{
            ToolCallID: getDataString(ev, "tool_call_id"),
            Content:    getDataString(ev, "content"),
        })

    case "done":
        success := getDataBool(ev, "success")
        return makeEnvelope(ev, events.Done, &events.DoneData{
            Success: success,
            Stats:   extractStats(ev),
        })

    case "error":
        return makeEnvelope(ev, events.Error, &events.ErrorData{
            Code:    getDataString(ev, "code"),
            Message:  getDataString(ev, "message"),
        })
    }

    return nil, nil // 忽略未知类型
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

### 8.3 Resume 行为

acpx 原生支持会话恢复：

```bash
# acpx 自动恢复同名会话
acpx claude -s my-session "continue the work"

# Gateway Resume 流程：
# 1. 获取 acpx_session_id 从 metadata
# 2. 检查 acpx sessions list 是否存在
# 3. 存在 → acpx sessions prompt --resume
# 4. 不存在 → session/new
```

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

| 能力 | ClaudeCode | OpenCodeCLI | OpenCodeSrv | **ACPX** |
|------|-----------|-------------|------------|----------|
| 支持 Agent 数量 | 1 | 1 | 1 | **16** |
| 会话持久化 | ✅ | ❌ | ✅ | ✅ |
| 流式输出 | ✅ | ✅ | ✅ | ✅ |
| 工具调用 | ✅ | ✅ | ✅ | ✅（取决于 Agent） |
| Resume | ✅ | ❌ | ❌ | ✅ |
| 权限控制 | ✅ | ❌ | ❌ | ⚠️ P1 |
| 会话队列 | ❌ | ❌ | ❌ | ✅ |
| Crash 恢复 | ❌ | ❌ | ❌ | ✅ |
| 命名会话 | ❌ | ❌ | ❌ | ✅ |

### 10.2 协议转换开销

| 层级 | ClaudeCode | ACPX |
|------|-----------|------|
| CLI 协议 | stream-json（Claude 专用） | JSON-RPC 2.0（通用） |
| 事件映射 | Claude 事件 → AEP | ACP 事件 → AEP |
| 转换复杂度 | 中等（专用 Parser/Mapper） | 低（统一 ACP 格式） |

---

## 11. 优雅终止

### 11.1 ACPX 终止流程

```bash
# 优雅终止：acpx cancel
acpx <agent> cancel --session-id <id>

# 或 SIGTERM → acpx 进程自行处理
```

### 11.2 Worker Adapter 终止

```go
func (w *Worker) Terminate(ctx context.Context) error {
    // 1. 发送 acpx cancel（优雅取消）
    if w.acpSessionID != "" {
        cancelReq := &Request{
            JSONRPC: "2.0",
            ID:      []byte(`"cancel"`),
            Method:  "session/cancel",
            Params:  json.RawMessage(fmt.Sprintf(`{"sessionId":"%s"}`, w.acpSessionID)),
        }
        _ = encodeNDJSON(w.Base.Conn().(*Conn).stdin, cancelReq)
    }

    // 2. 分层终止（SIGTERM → 5s → SIGKILL）
    return w.Base.Terminate(ctx)
}
```

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
- Given acpx 进程启动失败（CLI 不存在）, When Worker.Start, Then 返回 error 且包含 "acpx" 关键字

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
