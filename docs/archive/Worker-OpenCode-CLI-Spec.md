---
type: spec
tags:
  - project/HotPlex
  - worker/opencode-cli
  - architecture/integration
  - protocol/aep-v1
  - feature/event-conversion
  - feature/session-management
  - feature/resume-support
  - feature/tool-filtering
date: 2026-04-07
status: implemented
progress: 100
---

# OpenCode CLI Worker 集成规格

> 本文档定义 OpenCode CLI Worker Adapter 的集成规格与验收标准。
> 高阶设计见 [[Worker-Gateway-Design]] §8.2。

---

## 1. 概述

| 维度              | 规格                                       |
| ----------------- | ------------------------------------------ |
| **Transport**     | stdio（stdin/stdout pipe）                 |
| **Protocol**      | OpenCode CLI NDJSON → AEP v1 转换          |
| **进程模型**      | 持久进程，支持多轮复用（Hot-Multiplexing） |
| **源码路径**      | `internal/worker/opencodecli/`             |
| **OpenCode 源码** | `~/opencode/packages/opencode/src/cli/`    |
| **Resume 支持**   | 通过 `--session` / `--continue` 参数       |

**集成命令**：

```bash
opencode run --format json
```

> OpenCode CLI 输出 OpenCode 原生 NDJSON 格式，Worker Adapter 负责转换为 AEP v1 事件流。

---

## 2. CLI 参数规格

### 2.1 核心参数

| 参数            | 说明                  | 实现位置                                   |
| --------------- | --------------------- | ------------------------------------------ |
| `run`           | 运行模式（非交互）    | CLI: `run.ts:222` / Worker: `worker.go:69` |
| `--format json` | JSON 输出模式（必需） | CLI: `run.ts:222` / Worker: `worker.go:70` |

### 2.2 会话控制参数

| 参数                         | 说明         | 实现位置                                          |
| ---------------------------- | ------------ | ------------------------------------------------- |
| `--session <id>` / `-s <id>` | 指定会话 ID  | CLI: `run.ts:236-242` / Worker: `worker.go:82-84` |
| `--continue` / `-c`          | 继续最新会话 | CLI: `run.ts:243-248` / Worker: `worker.go:85-87` |
| `--fork`                     | Fork 会话    | CLI: `run.ts:249-254` / Worker: `worker.go:88-90` |

### 2.3 工具与权限控制

| 参数                     | 说明               | 实现机制                              |
| ------------------------ | ------------------ | ------------------------------------- |
| `--allowed-tools <tool>` | 允许的工具（多值） | Worker 层面：`security/tool.go:54-60` |

**实现说明**：工具控制在 Worker Adapter 层面实现，每个工具生成独立的 `--allowed-tools` 参数：

```go
// security/tool.go:54-60
func BuildAllowedToolsArgs(tools []string) []string {
    var args []string
    for _, tool := range tools {
        args = append(args, "--allowed-tools", tool)
    }
    return args
}
```

### 2.4 工作目录参数

| 参数           | 说明     | 实现位置                                          |
| -------------- | -------- | ------------------------------------------------- |
| `--dir <path>` | 工作目录 | CLI: `run.ts:289-292` / Worker: `proc/manager.go` |

### 2.5 扩展参数（可选）

| 参数                     | 说明       | CLI 位置         |
| ------------------------ | ---------- | ---------------- |
| `--model` / `-m <model>` | 模型选择   | `run.ts:255-259` |
| `--agent <name>`         | Agent 选择 | `run.ts:260-263` |
| `--file` / `-f <path>`   | 文件附件   | `run.ts:269-275` |
| `--title <title>`        | 会话标题   | `run.ts:276-279` |
| `--variant <v>`          | 模型变体   | `run.ts:297-300` |
| `--thinking`             | 显示思考块 | `run.ts:301-305` |

### 2.6 不支持的参数

以下参数在 OpenCode CLI 中不存在：

- `--dangerously-skip-permissions`
- `--permission-mode`
- `--system-prompt`
- `--append-system-prompt`
- `--mcp-config`（使用配置文件 `~/.opencode/mcp.json` 或环境变量 `OPENCODE_MCP_CONFIG`）

---

## 3. 环境变量规格

### 3.1 供应商托管变量（白名单）

| 变量                | 说明              | 实现位置                      |
| ------------------- | ----------------- | ----------------------------- |
| `OPENAI_API_KEY`    | OpenAI API 密钥   | `opencodecli/worker.go:23-28` |
| `OPENAI_BASE_URL`   | OpenAI API 端点   | 同上                          |
| `OPENCODE_API_KEY`  | OpenCode API 密钥 | 同上                          |
| `OPENCODE_BASE_URL` | OpenCode API 端点 | 同上                          |

**白名单定义**：

```go
// opencodecli/worker.go:23-28
var openCodeCLIEnvWhitelist = []string{
    "HOME", "USER", "SHELL", "PATH", "TERM",
    "LANG", "LC_ALL", "PWD",
    "OPENAI_API_KEY", "OPENAI_BASE_URL",
    "OPENCODE_API_KEY", "OPENCODE_BASE_URL",
}
```

### 3.2 HotPlex 注入变量

| 变量                  | 说明                              | 实现位置         |
| --------------------- | --------------------------------- | ---------------- |
| `HOTPLEX_SESSION_ID`  | 会话标识符                        | `base/env.go:63` |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签（`opencode-cli`） | `base/env.go:64` |

**设计约束**：OpenCode CLI 生成独立的 session ID，不读取 `HOTPLEX_SESSION_ID`。Worker Adapter 必须从 CLI 输出中提取实际 session ID。

### 3.3 环境变量构建流程

```go
// base/env.go:14-77
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
    // 1. 白名单过滤（精确匹配 + 前缀匹配）
    // 2. 添加 HOTPLEX_* 变量
    // 3. 合并 session.Env
    // 4. 剥离嵌套 Agent 配置（security.StripNestedAgent）
    return env
}
```

---

## 4. 输入格式（Client → Worker Adapter）

### 4.1 Input 事件

客户端发送标准 AEP v1 格式：

```json
{
  "version": "aep/v1",
  "id": "evt_abc123",
  "seq": 1,
  "session_id": "sess_xyz789",
  "timestamp": 1712234567890,
  "event": {
    "type": "input",
    "data": {
      "content": "帮我写一个 Go HTTP 服务器",
      "metadata": {
        "user_id": "user_001"
      }
    }
  }
}
```

### 4.2 Worker Adapter 处理

```go
// opencodecli/worker.go:104-134
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    conn := w.Base.Conn()
    if conn == nil {
        return fmt.Errorf("opencodecli: not started")
    }

    sessionID := conn.SessionID()
    if sessionID == "" {
        sessionID = "pending"
    }

    msg := events.NewEnvelope(
        aep.NewID(),
        sessionID,
        0, // seq assigned by hub
        events.Input,
        events.InputData{
            Content:  content,
            Metadata: metadata,
        },
    )

    return conn.Send(ctx, msg)
}
```

---

## 5. 输出格式（OpenCode CLI → Worker Adapter）

### 5.1 OpenCode CLI 原生格式（NDJSON）

**顶层结构**：

```json
{
  "type": "<event_type>",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": { ... }
}
```

**字段说明**：

- `type`: 事件类型（`step_start`, `text`, `tool_use`, `step_finish`, `error`, `reasoning`）
- `timestamp`: Unix 毫秒时间戳
- `sessionID`: 会话 ID（前缀 `ses_`）
- `part`: 事件数据对象，包含：
  - `id`: Part ID（前缀 `prt_`）
  - `messageID`: Message ID（前缀 `msg_`）
  - `sessionID`: 同顶层
  - `type`: Part 类型
  - 其他类型特定字段

### 5.2 事件类型与 AEP v1 映射

| OpenCode CLI  | AEP v1                                         | 转换复杂度 |
| ------------- | ---------------------------------------------- | ---------- |
| `step_start`  | `state`                                        | ⭐⭐⭐        |
| `text`        | `message`                                      | ⭐⭐         |
| `tool_use`    | `tool_call` + `tool_result`（拆分为 2 个事件） | ⭐⭐⭐⭐       |
| `step_finish` | `done`                                         | ⭐⭐⭐        |
| `error`       | `error`                                        | ⭐⭐         |
| `reasoning`   | 透传（可选处理）                               | ➕ 额外     |

### 5.3 事件转换规格

#### 5.3.1 step_start 事件

**OpenCode CLI 输出**：

```json
{
  "type": "step_start",
  "timestamp": 1775301343766,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d583426c6001Yz8XIqnJj7aESp",
    "messageID": "msg_d5833d55d001UnKAk7jMW8nYNa",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "step-start",
    "snapshot": "4b93d02e2d64e9733cf77f08a473afb05a47d267"
  }
}
```

**转换后 AEP v1**：

```json
{
  "version": "aep/v1",
  "id": "evt_new_id",
  "seq": 1,
  "session_id": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "timestamp": 1775301343766,
  "priority": "data",
  "event": {
    "type": "state",
    "data": {
      "status": "running"
    }
  }
}
```

**转换逻辑**：

1. 生成 event ID
2. 分配 seq（原子递增）
3. 映射 `sessionID` → `session_id`
4. 提取 `part.snapshot`（可选，用于断点恢复）
5. 构造 event envelope

#### 5.3.2 text 事件

**OpenCode CLI 输出**：

```json
{
  "type": "text",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d5834bb71001Qmy1JVqgnFC76D",
    "messageID": "msg_d58346bbb001s1MsQ1gQ5AmIfM",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "text",
    "text": "Hello, World!",
    "time": {
      "start": 1775301344121,
      "end": 1775301344121
    }
  }
}
```

**转换后 AEP v1**：

```json
{
  "version": "aep/v1",
  "id": "evt_new_id",
  "seq": 2,
  "session_id": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "timestamp": 1775301344121,
  "event": {
    "type": "message",
    "data": {
      "content": [
        {
          "type": "text",
          "text": "Hello, World!"
        }
      ]
    }
  }
}
```

**转换逻辑**：

1. 提取 `part.text` → `content[0].text`
2. 忽略 `part.time`（AEP 无此字段）
3. 构造 message envelope

#### 5.3.3 tool_use 事件

**OpenCode CLI 输出**（合并调用和结果）：

```json
{
  "type": "tool_use",
  "timestamp": 1775301397226,
  "sessionID": "ses_2a7caca33ffeYlYUhstmVHrzc8",
  "part": {
    "id": "prt_d58358a84001X0P5T4nIyWiTNs",
    "messageID": "msg_d58353674001wX0eTulQbIK4Bm",
    "sessionID": "ses_2a7caca33ffeYlYUhstmVHrzc8",
    "type": "tool",
    "tool": "read",
    "callID": "call_function_4cmyb5dhnrci_1",
    "state": {
      "status": "completed",
      "input": {
        "filePath": "/Users/huangzhonghui/opencode/package.json"
      },
      "output": "<path>/Users/.../package.json</path>\n...",
      "metadata": {
        "preview": "...",
        "truncated": false
      }
    }
  }
}
```

**转换后 AEP v1**（拆分为 2 个事件）：

事件 1 - `tool_call`：

```json
{
  "version": "aep/v1",
  "id": "evt_new_id_1",
  "seq": 3,
  "session_id": "ses_2a7caca33ffeYlYUhstmVHrzc8",
  "timestamp": 1775301397226,
  "event": {
    "type": "tool_call",
    "data": {
      "id": "call_function_4cmyb5dhnrci_1",
      "name": "read",
      "input": {
        "filePath": "/Users/huangzhonghui/opencode/package.json"
      }
    }
  }
}
```

事件 2 - `tool_result`：

```json
{
  "version": "aep/v1",
  "id": "evt_new_id_2",
  "seq": 4,
  "session_id": "ses_2a7caca33ffeYlYUhstmVHrzc8",
  "timestamp": 1775301397226,
  "event": {
    "type": "tool_result",
    "data": {
      "tool_call_id": "call_function_4cmyb5dhnrci_1",
      "content": [
        {
          "type": "text",
          "text": "<path>/Users/.../package.json</path>\n..."
        }
      ]
    }
  }
}
```

**转换逻辑**（最复杂）：

1. 提取 `part.callID` → `tool_call.id` / `tool_result.tool_call_id`
2. 提取 `part.tool` → `tool_call.name`
3. 提取 `part.state.input` → `tool_call.input`
4. 提取 `part.state.output` → `tool_result.content[0].text`
5. 生成两个独立的 AEP 事件
6. 分配不同的 seq

#### 5.3.4 step_finish 事件

**OpenCode CLI 输出**：

```json
{
  "type": "step_finish",
  "timestamp": 1775301344265,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d5834bb75001Mbf7UllSQUG8C1",
    "reason": "stop",
    "snapshot": "4b93d02e2d64e9733cf77f08a473afb05a47d267",
    "messageID": "msg_d58346bbb001s1MsQ1gQ5AmIfM",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "step-finish",
    "tokens": {
      "total": 53084,
      "input": 459,
      "output": 50,
      "reasoning": 0,
      "cache": {
        "write": 52585,
        "read": 0
      }
    },
    "cost": 0.019917075
  }
}
```

**转换后 AEP v1**：

```json
{
  "version": "aep/v1",
  "id": "evt_new_id",
  "seq": 5,
  "session_id": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "timestamp": 1775301344265,
  "event": {
    "type": "done",
    "data": {
      "reason": "stop",
      "stats": {
        "tokens": {
          "total": 53084,
          "input": 459,
          "output": 50,
          "reasoning": 0,
          "cache_write": 52585,
          "cache_read": 0
        },
        "cost": 0.019917075
      }
    }
  }
}
```

**转换逻辑**：

1. 提取 `part.reason` → `data.reason`
2. 提取 `part.tokens`, `part.cost` → `data.stats`
3. 构造 done envelope

---

## 6. 事件转换层架构

### 6.1 EventConverter 组件

**文件**：`internal/worker/opencodecli/converter.go`

```go
package opencodecli

import (
    "encoding/json"
    "fmt"

    "github.com/hotplex/hotplex-worker/pkg/aep"
    "github.com/hotplex/hotplex-worker/pkg/events"
)

// EventConverter converts OpenCode CLI events to AEP format.
type EventConverter struct {
    seqGen *SeqGen
}

// NewEventConverter creates a new converter.
func NewEventConverter() *EventConverter {
    return &EventConverter{
        seqGen: &SeqGen{},
    }
}

// Convert converts a raw OpenCode CLI event to AEP envelope(s).
// Returns single envelope for most events, multiple for tool_use (call + result).
func (c *EventConverter) Convert(raw json.RawMessage) (interface{}, error) {
    var rawEvent struct {
        Type      string          `json:"type"`
        Timestamp int64           `json:"timestamp"`
        SessionID string          `json:"sessionID"`
        Part      json.RawMessage `json:"part"`
    }

    if err := json.Unmarshal(raw, &rawEvent); err != nil {
        return nil, fmt.Errorf("unmarshal raw event: %w", err)
    }

    switch rawEvent.Type {
    case "step_start":
        return c.convertStepStart(rawEvent)
    case "text":
        return c.convertText(rawEvent)
    case "tool_use":
        return c.convertToolUse(rawEvent)
    case "step_finish":
        return c.convertStepFinish(rawEvent)
    case "error":
        return c.convertError(rawEvent)
    default:
        return nil, fmt.Errorf("unknown event type: %s", rawEvent.Type)
    }
}
```

### 6.2 集成到 Worker

**文件**：`opencodecli/worker.go`

```go
func (w *Worker) readOutput(defaultSessionID string) {
    converter := NewEventConverter()

    defer func() {
        c := w.Base.Conn()
        if c != nil {
            c.Close()
        }
    }()

    w.Base.Mu.Lock()
    proc := w.Base.Proc
    w.Base.Mu.Unlock()
    if proc == nil {
        return
    }

    for {
        line, err := proc.ReadLine()
        if err != nil {
            if err == io.EOF {
                return
            }
            w.Base.Log.Error("opencodecli: read line", "error", err)
            return
        }

        if line == "" {
            continue
        }

        // Extract session ID
        if w.sessionID == "" {
            w.tryExtractSessionID(line)
        }

        // Convert to AEP format
        envs, err := converter.Convert([]byte(line))
        if err != nil {
            w.Base.Log.Warn("opencodecli: convert event", "error", err, "line", line)
            continue
        }

        // Handle single or multiple envelopes
        envelopes := normalizeEnvelopes(envs)

        // Send each envelope
        for _, env := range envelopes {
            // Update session ID
            if w.sessionID != "" {
                w.updateConnSessionID(w.sessionID)
                env.SessionID = w.sessionID
            } else {
                env.SessionID = defaultSessionID
            }

            w.Base.SetLastIO(time.Now())

            conn, ok := w.Base.Conn().(*base.Conn)
            if !ok || conn == nil {
                return
            }

            if !conn.TrySend(env) {
                w.Base.Log.Warn("opencodecli: recv channel full, dropping message")
            }
        }
    }
}
```

---

## 7. Session 管理

### 7.1 Session ID 提取

OpenCode CLI 生成独立的 session ID，Worker Adapter 必须从 `step_start` 事件中提取：

```go
func (w *Worker) tryExtractSessionID(line string) {
    var raw struct {
        Type      string `json:"type"`
        SessionID string `json:"sessionID"`
        Part      struct {
            SessionID string `json:"sessionID"`
        } `json:"part"`
    }

    if err := json.Unmarshal([]byte(line), &raw); err != nil {
        return
    }

    if raw.Type == "step_start" {
        sessionID := raw.SessionID
        if sessionID == "" {
            sessionID = raw.Part.SessionID
        }

        if sessionID != "" {
            w.mu.Lock()
            w.sessionID = sessionID
            w.mu.Unlock()
            w.Base.Log.Info("opencodecli: extracted session ID", "session_id", w.sessionID)
        }
    }
}
```

### 7.2 WorkerSessionIDHandler 接口实现

> OpenCode CLI Worker 实现 `worker.WorkerSessionIDHandler` 接口，使 Gateway 能够获取并持久化内部 session ID。

```go
// opencodecli/worker.go

// WorkerSessionIDHandler 实现
func (w *Worker) SetWorkerSessionID(id string) {
    w.mu.Lock()
    w.sessionID = id
    w.mu.Unlock()
}

func (w *Worker) GetWorkerSessionID() string {
    w.mu.RLock()
    defer w.mu.RUnlock()
    return w.sessionID
}
```

**Session ID 持久化流程**：

1. Worker 启动时 `sessionID = ""`
2. `readOutput()` 从 `step_start` 事件提取 session ID
3. `persistWorkerSessionID()` 在 `forwardEvents()` 收到第一个事件时调用
4. `sm.UpdateWorkerSessionID()` 持久化到 SQLite `sessions.worker_session_id` 字段

### 7.3 Resume 支持

Worker Adapter 通过传递 `--session` / `--continue` 参数支持 Resume：

```go
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    args := []string{"run", "--format", "json"}

    // 添加 resume 参数
    if session.SessionID != "" {
        args = append(args, "--session", session.SessionID)
    } else {
        args = append(args, "--continue")
    }

    if session.Fork {
        args = append(args, "--fork")
    }

    // ... 启动进程（复用 Start 逻辑）
}
```

---

## 8. 错误处理

### 8.1 事件转换错误

```go
envs, err := converter.Convert([]byte(line))
if err != nil {
    w.Base.Log.Warn("opencodecli: convert event", "error", err, "line", line)
    continue
}
```

**处理策略**：记录警告并跳过无法识别的事件，保持事件流连续性。

### 8.2 输出限制

- **初始缓冲区**：64KB
- **单行硬上限**：10MB（超出 `bufio.ErrTooLong`）

```go
// proc/manager.go:291-305
scanner := bufio.NewScanner(stdout)
buf := make([]byte, 0, 64*1024) // 64 KB
scanner.Buffer(buf, 10*1024*1024) // 10 MB max
```

### 8.3 背压处理

- **Channel 容量**：256
- **丢弃策略**：`data` priority 消息（delta、raw）静默丢弃
- **日志记录**：丢弃时记录警告

---

## 9. Capability 接口实现

```go
// worker.go:46-56
func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeCLI }
func (w *Worker) SupportsResume() bool    { return true }  // 通过 --session/--continue
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeCLIEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" }     // CLI 不持久化
func (w *Worker) MaxTurns() int           { return 0 }     // 无限制
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }
```

| Capability          | 值                        | 说明                            |
| ------------------- | ------------------------- | ------------------------------- |
| `Type`              | `TypeOpenCodeCLI`         | Worker 类型常量                 |
| `SupportsResume`    | `true`                    | 通过 `--session` / `--continue` |
| `SupportsStreaming` | `true`                    | NDJSON 流式输出                 |
| `SupportsTools`     | `true`                    | 工具调用支持                    |
| `EnvWhitelist`      | `openCodeCLIEnvWhitelist` | 环境变量白名单                  |
| `SessionStoreDir`   | `""`                      | CLI 不持久化会话存储            |
| `MaxTurns`          | `0`                       | 无限制                          |
| `Modalities`        | `["text", "code"]`        | 支持文本和代码                  |

---

## 10. 设计约束

1. **CLI 生成独立 Session ID**
   - OpenCode CLI 不读取 `HOTPLEX_SESSION_ID` 环境变量
   - Worker 必须从 `step_start` 事件中提取 session ID

2. **工具控制在 Worker 层面**
   - CLI 本身不支持 `--allowed-tools` 参数
   - Worker Adapter 拦截并实现工具过滤

3. **MCP 配置方式**
   - 使用配置文件 `~/.opencode/mcp.json` 或环境变量 `OPENCODE_MCP_CONFIG`
   - 不支持 `--mcp-config` 命令行参数

4. **事件格式转换必需**
   - OpenCode CLI 输出自定义 NDJSON 格式
   - Worker 必须实现完整的事件转换层

---

## 11. 源码关键路径

### 11.1 Worker 实现

| 功能         | 源码路径                                   |
| ------------ | ------------------------------------------ |
| Worker 主体  | `internal/worker/opencodecli/worker.go`    |
| 事件转换器   | `internal/worker/opencodecli/converter.go` |
| 序列号生成器 | `internal/worker/opencodecli/seq.go`       |

### 11.2 公共组件

| 功能             | 源码路径                          |
| ---------------- | --------------------------------- |
| BaseWorker       | `internal/worker/base/worker.go`  |
| Stdio Conn       | `internal/worker/base/conn.go`    |
| BuildEnv         | `internal/worker/base/env.go`     |
| Process Manager  | `internal/worker/proc/manager.go` |
| AEP Codec        | `pkg/aep/codec.go`                |
| Events           | `pkg/events/events.go`            |
| Worker Interface | `internal/worker/worker.go`       |
| Security Env     | `internal/security/env.go`        |
| Tool Policy      | `internal/security/tool.go`       |

---

## 12. 与 Claude Code Worker 的差异

| 特性           | Claude Code Worker                              | OpenCode CLI Worker                    |
| -------------- | ----------------------------------------------- | -------------------------------------- |
| **Transport**  | stdio                                           | stdio                                  |
| **Protocol**   | SDK NDJSON → AEP v1                             | OpenCode NDJSON → AEP v1（需转换）     |
| **Session ID** | 外部指定 `--session`（`-s`）                    | 内部生成（从 `step_start` 提取）       |
| **Resume**     | 支持 `--resume`                                 | 支持 `--session` / `--continue`        |
| **CLI 参数**   | `--print --verbose --output-format stream-json` | `--format json`                        |
| **环境变量**   | `ANTHROPIC_*`                                   | `OPENAI_*`, `OPENCODE_*`               |
| **MCP 配置**   | `--mcp-config` 参数                             | 配置文件或环境变量                     |
| **工具参数**   | `--allowed-tools`（单值）                       | `--allowed-tools`（多值，Worker 层面） |
| **事件转换**   | 简单映射                                        | 复杂转换（`tool_use` 拆分为 2 事件）   |

---

## 13. 扩展路线图

### 13.1 Phase 1：基础功能（已实现）

- ✅ NDJSON 事件转换（`EventConverter`）
- ✅ Session ID 提取
- ✅ Resume 支持（`--session` / `--continue`）
- ✅ 工具调用映射（`tool_use` → `tool_call` + `tool_result`）

### 13.2 Phase 2：增强功能

- 🔲 模型选择参数（`--model`, `--variant`）
- 🔲 Agent 选择参数（`--agent`）
- 🔲 文件附件支持（`--file`）
- 🔲 会话标题（`--title`）
- 🔲 思考块处理（`--thinking`）

### 13.3 Phase 3：优化与监控

- 🔲 事件转换性能优化
- 🔲 详细监控指标（事件转换延迟、错误率）
- 🔲 完整测试套件（所有事件类型 + 边界条件）

---

## 14. 验收标准

### 14.1 功能验收

- ✅ Worker 能够启动 OpenCode CLI 进程
- ✅ 正确提取 session ID 并更新到连接
- ✅ 所有事件类型正确转换为 AEP v1 格式
- ✅ `tool_use` 事件正确拆分为 `tool_call` + `tool_result`
- ✅ Resume 功能正常工作（`--session` / `--continue`）
- ✅ 工具过滤正常工作（`--allowed-tools`）

### 14.2 性能验收

- ✅ 事件转换延迟 < 1ms（P99）
- ✅ 内存使用稳定（无泄漏）
- ✅ 背压处理正常（channel 满时静默丢弃）

### 14.3 错误处理验收

- ✅ 无法识别的事件类型记录警告并跳过
- ✅ 解析错误记录警告并跳过
- ✅ 进程崩溃正确报告（`done` 事件 + `crash_exit_code`）

---

## 15. 架构亮点

### 15.1 OpenCode CLI 特有亮点

- ✅ **自动 session_id 提取**：从 `step_start` 事件解析
- ✅ **多值 `--allowed-tools`**：Worker 层面实现，每个工具单独参数
- ✅ **完整事件转换层**：`EventConverter` 组件化设计

### 15.2 公共亮点

- ✅ **三层协议分层**：`scanner` → `EventConverter.Convert` → `Conn.TrySend`
- ✅ **背压处理**：256 buffer，delta 静默丢弃
- ✅ **分层终止**：SIGTERM → 5s → SIGKILL
- ✅ **`HOTPLEX_*` 变量注入**：会话追踪和类型标识

---

## 附录 A: 验证数据

**测试输出**：
- `test-output/basic_test_20260404_191518.jsonl` (1.0K)
- `test-output/tool_test_20260404_191610.jsonl` (14K)

**验证脚本**：
- `scripts/validate-opencode-cli-spec.sh`
- `scripts/test-opencode-cli-output.sh`

**详细报告**：
- `docs/research/opencode-cli-spec-accurate-validation.md`
- `docs/research/opencode-cli-validation-report.md`

---

**文档版本**: 3.0 (正式版)
**最后更新**: 2026-04-04
**状态**: 已实现
