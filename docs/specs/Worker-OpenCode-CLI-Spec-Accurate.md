---
type: spec
tags:
  - project/HotPlex
  - worker/opencode-cli
  - architecture/integration
date: 2026-04-04
status: needs-rewrite
progress: 30
completion_date:
validation_date: 2026-04-04
validation_source: actual-testing
---

# OpenCode CLI Worker 集成规格（验证版）

> **⚠️ 警告**: 本文档基于 2026-04-04 的实际验证更新。
> 原始 Spec 文档准确性仅 30%，已发现严重实现问题。
> 详见：`docs/research/opencode-cli-spec-accurate-validation.md`

---

## 1. 概述

| 维度 | 设计 | 实际 | 状态 |
|------|------|------|------|
| **Transport** | stdio（stdin/stdout pipe） | stdio | ✅ |
| **Protocol** | AEP v1 NDJSON | **自定义 NDJSON** | ❌ 不兼容 |
| **进程模型** | 持久进程，多轮复用 | 持久进程 | ✅ |
| **源码路径** | `internal/worker/opencodecli/` | 同左 | ✅ |
| **OpenCode 源码** | `~/opencode/packages/opencode/src/cli/` | 同左 | ✅ |
| **Resume** | ❌ 不支持 | ⚠️ CLI 支持，Worker 未实现 | 需补充 |

**集成命令**：

```bash
opencode run --format json
```

> **⚠️ 关键发现**: OpenCode CLI 输出**不是** AEP v1 格式！
> Worker Adapter 需要完整的事件转换层。

---

## 2. CLI 参数

### 2.1 核心参数（v1.0 必须）

| 参数 | 说明 | CLI 层面 | Worker 层面 | 总状态 |
|------|------|---------|-----------|--------|
| `run` | 运行模式（非交互） | ✅ | ✅ | ✅ |
| `--format json` | JSON 输出模式（必需） | ✅ | ✅ | ✅ |

**实现位置**:
- CLI: `~/opencode/packages/opencode/src/cli/cmd/run.ts:222`
- Worker: `internal/worker/opencodecli/worker.go:69-72`

### 2.2 会话控制参数

| 参数 | 说明 | CLI 支持 | Worker 支持 | 总状态 |
|------|------|---------|-----------|--------|
| `--session` / `-s <id>` | 指定会话 ID | ✅ | ⚠️ 需实现 | ⚠️ |
| `--continue` / `-c` | 继续最新会话 | ✅ | ⚠️ 需实现 | ⚠️ |
| `--fork` | Fork 会话 | ✅ | ❌ 未实现 | ⚠️ |
| `--share` | 分享会话 | ✅ | ❌ 未实现 | ⚠️ |

**代码位置**:
- CLI: `run.ts:236-254`
- Worker: **需要实现参数传递**

**Resume 功能**:
- Spec 原标记: ❌ 不支持
- 实际情况: ✅ CLI 支持（`--continue`, `--session`）
- Worker 状态: ❌ 未实现参数传递

**需要修复**:
```go
// worker.go:68-72 （需要添加）
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    args := []string{"run", "--format", "json"}

    // 添加会话控制参数
    if session.Resume {
        if session.SessionID != "" {
            args = append(args, "--session", session.SessionID)
        } else {
            args = append(args, "--continue")
        }
        if session.Fork {
            args = append(args, "--fork")
        }
    }

    // ... 启动进程
}
```

### 2.3 工具与权限控制

| 参数 | 说明 | CLI 层面 | Worker 层面 | 总状态 |
|------|------|---------|-----------|--------|
| `--allowed-tools <tool>` | 允许的工具 | ❌ | ✅ | ✅ |
| `--disallowed-tools <tool>` | 禁用的工具 | ❌ | ⚠️ 可选 | ⚠️ |
| `--dangerously-skip-permissions` | 跳过权限检查 | ❌ | ❌ | ❌ |
| `--permission-mode <mode>` | 权限模式 | ❌ | ❌ | ❌ |

**实现机制**（关键发现）:
```go
// proc/manager.go:75-79
if len(m.allowedTools) > 0 {
    toolsArgs := security.BuildAllowedToolsArgs(m.allowedTools)
    args = append(args, toolsArgs...)
}

// security/tool.go:54-60
func BuildAllowedToolsArgs(tools []string) []string {
    var args []string
    for _, tool := range tools {
        args = append(args, "--allowed-tools", tool)
    }
    return args
}
```

**结论**: 工具控制在 **Worker Adapter 层面**实现，CLI 本身不支持这些参数。

### 2.4 系统提示参数

| 参数 | 说明 | 状态 |
|------|------|------|
| `--system-prompt <prompt>` | 系统提示 | ❌ CLI 不支持 |
| `--append-system-prompt <prompt>` | 追加系统提示 | ❌ CLI 不支持 |

### 2.5 MCP 配置参数

| 参数 | 说明 | 状态 |
|------|------|------|
| `--mcp-config <path>` | MCP 配置文件 | ❌ CLI 使用不同方式 |
| `--strict-mcp-config` | 严格 MCP 配置 | ❌ CLI 不支持 |

**OpenCode MCP 方式**:
- 配置文件: `~/.opencode/mcp.json`
- 环境变量: `OPENCODE_MCP_CONFIG`

### 2.6 扩展参数（CLI 特有，Spec 未记录）

| 参数 | 说明 | CLI 位置 | Worker 状态 |
|------|------|---------|-----------|
| `--model` / `-m <model>` | 模型选择 | run.ts:255-259 | ⚠️ 可选实现 |
| `--agent <name>` | Agent 选择 | run.ts:260-263 | ⚠️ 可选实现 |
| `--file` / `-f <path>` | 文件附件 | run.ts:269-275 | ⚠️ 可选实现 |
| `--title <title>` | 会话标题 | run.ts:276-279 | ⚠️ 可选实现 |
| `--attach <url>` | 连接远程服务器 | run.ts:280-283 | ❌ 不需要 |
| `--password` / `-p <pass>` | Basic Auth 密码 | run.ts:284-288 | ❌ 不需要 |
| `--dir <path>` | 工作目录 | run.ts:289-292 | ✅ 已实现 |
| `--port <num>` | 服务器端口 | run.ts:293-296 | ❌ 不需要 |
| `--variant <v>` | 模型变体 | run.ts:297-300 | ⚠️ 可选实现 |
| `--thinking` | 显示思考块 | run.ts:301-305 | ⚠️ 可选实现 |

---

## 3. 环境变量

> 详见 [[Worker-Common-Protocol]] §6。

### 3.1 供应商托管变量（白名单）

| 变量 | 说明 | 实现 | 测试 |
|------|------|------|------|
| `OPENAI_API_KEY` | OpenAI API 密钥 | ✅ `opencodecli/worker.go:23-28` | ⚠️ 需测试 |
| `OPENAI_BASE_URL` | OpenAI API 端点 | ✅ 同上 | ⚠️ 需测试 |
| `OPENCODE_API_KEY` | OpenCode API 密钥 | ✅ 同上 | ⚠️ 需测试 |
| `OPENCODE_BASE_URL` | OpenCode API 端点 | ✅ 同上 | ⚠️ 需测试 |

**白名单定义**:
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

| 变量 | 说明 | 实现 | CLI 读取 |
|------|------|------|---------|
| `HOTPLEX_SESSION_ID` | 会话标识符 | ✅ `base/env.go` | ❌ **CLI 忽略** |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签 | ✅ 同上 | ❌ CLI 忽略 |

**⚠️ 关键发现**: OpenCode CLI **不读取** `HOTPLEX_SESSION_ID`，始终生成自己的 session ID。

**实际测试**:
```bash
$ env HOTPLEX_SESSION_ID=test-override bun run opencode run --format json 'test'
# 输出中的 sessionID 仍然是 CLI 生成的（如 ses_2a7ac77dfffeKdBiZQ5Vt4CLnR）
```

**影响**: Worker 无法通过环境变量强制指定 session ID，必须从 CLI 输出中提取。

### 3.3 环境变量构建逻辑

```go
// base/env.go:14-77
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
    env := make([]string, 0, len(os.Environ()))

    // 1. 从 os.Environ() 白名单过滤
    whitelistSet := make(map[string]bool)
    prefixKeys := make([]string, 0)
    for _, k := range whitelist {
        if strings.HasSuffix(k, "_") {
            prefixKeys = append(prefixKeys, k) // 前缀匹配（如 OTEL_）
        } else {
            whitelistSet[k] = true
        }
    }

    for _, e := range os.Environ() {
        parts := strings.SplitN(e, "=", 2)
        key := parts[0]

        // 精确匹配或前缀匹配
        if whitelistSet[key] || hasAnyPrefix(key, prefixKeys) {
            env = append(env, e)
            continue
        }

        // 或在 session.Env 中
        if _, ok := session.Env[key]; ok {
            env = append(env, e)
        }
    }

    // 2. 添加 HOTPLEX_* 变量
    env = append(env,
        "HOTPLEX_SESSION_ID="+session.SessionID,
        "HOTPLEX_WORKER_TYPE="+workerTypeLabel,
    )

    // 3. 合并 session.Env
    for k, v := range session.Env {
        if k != "" && !whitelistSet[k] {
            env = append(env, k+"="+v)
        }
    }

    // 4. 剥离嵌套 Agent 配置
    env = security.StripNestedAgent(env)

    return env
}
```

---

## 4. 输入格式（Client → Worker Adapter）

> 输入使用标准 AEP v1 格式，由 Worker Adapter 处理。

### 4.1 Input 事件

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

**Worker Adapter 处理**:
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

**⚠️ 问题**: 当前实现**未转换**为 OpenCode CLI 的输入格式！
需要转换为 CLI 期望的格式（待确认）。

---

## 5. 输出格式（OpenCode CLI → Worker Adapter）

### 5.1 实际输出格式（NDJSON）

> **⚠️ 关键发现**: OpenCode CLI 输出**不是** AEP v1 格式！

**顶层结构**:
```json
{
  "type": "<event_type>",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": { ... }
}
```

**字段说明**:
- `type`: 事件类型（`step_start`, `text`, `tool_use`, `step_finish`, `error`, `reasoning`）
- `timestamp`: Unix 毫秒时间戳
- `sessionID`: 会话 ID（前缀 `ses_`）
- `part`: 事件数据对象，包含：
  - `id`: Part ID（前缀 `prt_`）
  - `messageID`: Message ID（前缀 `msg_`）
  - `sessionID`: 同顶层
  - `type`: Part 类型
  - 其他类型特定字段

**与 AEP v1 的差异**:

| 项目 | AEP v1 | OpenCode CLI | 差异级别 |
|------|--------|-------------|---------|
| 顶层字段 | `version`, `id`, `seq`, `event` | `type`, `timestamp`, `sessionID`, `part` | ❌ 完全不同 |
| Version 字段 | `"aep/v1"` | 不存在 | ❌ 缺失 |
| Event ID | `evt_xxx` | 不存在（有 part.id） | ❌ 缺失 |
| Seq 字段 | 序列号 | 不存在 | ❌ 缺失 |
| Event envelope | `{ type, data }` | 直接 `type + part` | ❌ 结构不同 |
| Session ID 字段名 | `session_id` | `sessionID` | ⚠️ 命名不同 |

### 5.2 事件类型完整映射

| OpenCode CLI | CLI 结构 | AEP v1 | AEP 结构 | 转换复杂度 |
|-------------|---------|--------|---------|----------|
| `step_start` | `{ part: { type: "step-start", snapshot } }` | `state` | `{ data: { status: "running" } }` | ⭐⭐⭐ |
| `text` | `{ part: { type: "text", text, time } }` | `message` | `{ data: { content: [...] } }` | ⭐⭐ |
| `tool_use` | `{ part: { type: "tool", tool, state } }` | `tool_call` + `tool_result` | 两个分离事件 | ⭐⭐⭐⭐ |
| `step_finish` | `{ part: { type: "step-finish", reason, tokens, cost } }` | `done` | `{ data: { reason } }` | ⭐⭐⭐ |
| `error` | `{ part: { error } }` | `error` | `{ data: { message } }` | ⭐⭐ |
| `reasoning` | `{ part: { type: "reasoning", text, time } }` | — | — | ➕ 额外 |

### 5.3 完整事件示例

#### 5.3.1 step_start 事件

**实际输出**:
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

**期望 AEP v1 输出**:
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

**转换逻辑**:
```
1. 生成 event ID
2. 分配 seq（原子递增）
3. 映射 sessionID → session_id
4. 提取 part.snapshot（可选，用于断点恢复）
5. 构造 event envelope
```

#### 5.3.2 text 事件

**实际输出**:
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

**期望 AEP v1 输出**:
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

**转换逻辑**:
```
1. 提取 part.text → content[0].text
2. 忽略 part.time（AEP 无此字段）
3. 构造 message envelope
```

#### 5.3.3 tool_use 事件（复杂）

**实际输出**（合并了调用和结果）:
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

**期望 AEP v1 输出（拆分为两个事件）**:

事件 1 - `tool_call`:
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

事件 2 - `tool_result`:
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

**转换逻辑**（最复杂）:
```
1. 提取 part.callID → tool_call.id / tool_result.tool_call_id
2. 提取 part.tool → tool_call.name
3. 提取 part.state.input → tool_call.input
4. 提取 part.state.output → tool_result.content[0].text
5. 生成两个独立的 AEP 事件
6. 分配不同的 seq
```

#### 5.3.4 step_finish 事件

**实际输出**:
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

**期望 AEP v1 输出**:
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

**转换逻辑**:
```
1. 提取 part.reason → data.reason
2. 提取 part.tokens, part.cost → data.stats（可选）
3. 构造 done envelope
```

---

## 6. 事件转换层（必需实现）

> **⚠️ 致命问题**: 当前 Worker Adapter **缺少**事件转换层，导致无法工作！

### 6.1 当前实现的问题

```go
// opencodecli/worker.go:201-205
env, err := aep.DecodeLine([]byte(line))
if err != nil {
    w.Base.Log.Warn("opencodecli: decode line", "error", err, "line", line)
    continue
}
```

**问题**: `aep.DecodeLine` 期望 AEP v1 格式，但 OpenCode CLI 输出自定义格式。

**实际错误**:
```
aep: validate envelope: version is required
aep: validate envelope: id is required
aep: validate envelope: seq must be a positive integer
aep: validate envelope: event is required
```

**结果**: **所有事件解码失败，Worker 完全无法工作！**

### 6.2 必需的修复：EventConverter

**文件**: `internal/worker/opencodecli/converter.go`（需要新建）

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

// Convert converts a raw OpenCode CLI event to AEP envelope.
func (c *EventConverter) Convert(raw json.RawMessage) (*events.Envelope, error) {
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

// convertText converts text event to AEP message.
func (c *EventConverter) convertText(raw RawEvent) (*events.Envelope, error) {
    var part struct {
        Text string `json:"text"`
    }
    if err := json.Unmarshal(raw.Part, &part); err != nil {
        return nil, err
    }

    return &events.Envelope{
        Version:   events.Version,
        ID:        aep.NewID(),
        Seq:       c.seqGen.Next(raw.SessionID),
        SessionID: raw.SessionID,
        Timestamp: raw.Timestamp,
        Event: events.Event{
            Type: events.Message,
            Data: events.MessageData{
                Content: []events.ContentPart{
                    {Type: "text", Text: part.Text},
                },
            },
        },
    }, nil
}

// convertToolUse converts tool_use event to AEP tool_call + tool_result.
func (c *EventConverter) convertToolUse(raw RawEvent) ([]*events.Envelope, error) {
    var part struct {
        Tool   string `json:"tool"`
        CallID string `json:"callID"`
        State  struct {
            Status   string                 `json:"status"`
            Input    map[string]interface{} `json:"input"`
            Output   string                 `json:"output"`
            Metadata map[string]interface{} `json:"metadata"`
        } `json:"state"`
    }

    if err := json.Unmarshal(raw.Part, &part); err != nil {
        return nil, err
    }

    // 生成两个事件
    call := &events.Envelope{
        Version:   events.Version,
        ID:        aep.NewID(),
        Seq:       c.seqGen.Next(raw.SessionID),
        SessionID: raw.SessionID,
        Timestamp: raw.Timestamp,
        Event: events.Event{
            Type: events.ToolCall,
            Data: events.ToolCallData{
                ID:    part.CallID,
                Name:  part.Tool,
                Input: part.State.Input,
            },
        },
    }

    result := &events.Envelope{
        Version:   events.Version,
        ID:        aep.NewID(),
        Seq:       c.seqGen.Next(raw.SessionID),
        SessionID: raw.SessionID,
        Timestamp: raw.Timestamp,
        Event: events.Event{
            Type: events.ToolResult,
            Data: events.ToolResultData{
                ToolCallID: part.CallID,
                Content: []events.ContentPart{
                    {Type: "text", Text: part.State.Output},
                },
            },
        },
    }

    return []*events.Envelope{call, result}, nil
}
```

### 6.3 集成到 Worker

**修改**: `opencodecli/worker.go:167-236`

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

        // Convert to AEP format（关键修改）
        envs, err := converter.Convert([]byte(line))
        if err != nil {
            w.Base.Log.Warn("opencodecli: convert event", "error", err, "line", line)
            continue
        }

        // Handle single or multiple envelopes
        if single, ok := envs.(*events.Envelope); ok {
            envs = []*events.Envelope{single}
        }

        envelopes, ok := envs.([]*events.Envelope)
        if !ok {
            w.Base.Log.Warn("opencodecli: unexpected envelope type")
            continue
        }

        // Send each envelope
        for _, env := range envelopes {
            // Update session ID
            if w.sessionID != "" {
                w.Base.Mu.Lock()
                if c, ok := w.Base.Conn().(*base.Conn); ok {
                    c.SetSessionID(w.sessionID)
                }
                w.Base.Mu.Unlock()
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

**当前实现**（有 Bug）:
```go
// worker.go:238-271
func (w *Worker) tryExtractSessionID(line string) {
    var raw map[string]json.RawMessage
    if err := json.Unmarshal([]byte(line), &raw); err != nil {
        return
    }

    if typ, ok := raw["type"]; ok {
        if string(typ) == `"step_start"` || string(typ) == "step_start" {
            if data, ok := raw["data"]; ok {  // ❌ 不存在 data 字段！
                var stepData struct {
                    SessionID string `json:"session_id"` // ❌ 实际是 sessionID
                    ID        string `json:"id"`
                }
                // ...
            }
        }
    }
}
```

**问题**:
1. 尝试从 `data` 字段提取，但实际结构中**没有** `data`
2. 字段名应该是 `sessionID` 而非 `session_id`

**正确的实现**:
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

### 7.2 Resume 支持

**当前状态**: ❌ 未实现

**CLI 支持**: ✅ 支持（`--continue`, `--session`）

**需要实现**:
```go
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // 不再返回 "not supported"

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

### 8.1 解码错误

**当前**:
```go
env, err := aep.DecodeLine([]byte(line))
if err != nil {
    w.Base.Log.Warn("opencodecli: decode line", "error", err, "line", line)
    continue  // 静默丢弃
}
```

**问题**: 所有事件都解码失败，但只记录警告！

**修复后**:
```go
envs, err := converter.Convert([]byte(line))
if err != nil {
    w.Base.Log.Warn("opencodecli: convert event", "error", err, "line", line)
    continue
}
```

### 8.2 输出限制

- **初始缓冲区**: 64KB
- **单行硬上限**: 10MB（超出 `bufio.ErrTooLong`）

```go
// proc/manager.go:291-305
scanner := bufio.NewScanner(stdout)
buf := make([]byte, 0, 64*1024) // 64 KB
scanner.Buffer(buf, 10*1024*1024) // 10 MB max
```

### 8.3 背压处理

- **Channel 容量**: 256
- **静默丢弃**: `data` priority 消息（delta、raw）
- **日志记录**: 静默丢弃时记录警告

---

## 9. 实现优先级

### P0 - 致命问题（立即修复）

- [ ] **实现 EventConverter**（converter.go）
  - 所有事件类型转换
  - Session ID 修复
  - 集成到 Worker

- [ ] **修复 Session ID 提取**
  - 正确的字段名
  - 正确的结构访问

### P1 - 功能缺失（本周完成）

- [ ] **实现 Resume 支持**
  - 传递 `--continue` / `--session`
  - 实现 Resume 方法

- [ ] **测试环境变量**
  - 验证 API keys
  - 验证注入变量

### P2 - 增强（下周完成）

- [ ] **传递额外参数**
  - `--model`, `--agent`, `--file`
  - `--title`, `--variant`

- [ ] **完整测试套件**
  - 所有事件类型
  - 错误场景
  - 边界条件

---

## 10. 已知限制

1. **CLI 不读取 `HOTPLEX_SESSION_ID`**
   - 必须从输出提取 session ID
   - 无法强制指定

2. **部分参数 CLI 不支持**
   - `--dangerously-skip-permissions`
   - `--permission-mode`
   - `--system-prompt`

3. **MCP 配置方式不同**
   - 使用配置文件或环境变量
   - 不支持 `--mcp-config` 参数

4. **需要完整的事件转换层**
   - 当前实现缺失
   - Worker 无法工作

---

## 附录 A: 完整验证数据

**测试输出**:
- `test-output/basic_test_20260404_191518.jsonl` (1.0K)
- `test-output/tool_test_20260404_191610.jsonl` (14K)

**验证脚本**:
- `scripts/validate-opencode-cli-spec.sh`
- `scripts/test-opencode-cli-output.sh`

**详细报告**:
- `docs/research/opencode-cli-spec-accurate-validation.md`
- `docs/research/opencode-cli-validation-report.md`

---

**文档版本**: 2.0 (验证版)
**最后更新**: 2026-04-04
**下次审查**: 实现 P0 修复后
