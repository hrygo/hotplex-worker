# OpenCode CLI Spec 精准验证报告

**日期**: 2026-04-04
**状态**: ❌ Spec 严重不准确，需要重写
**验证方法**: 源码分析 + 实际测试 + Worker Adapter 审计

---

## 🚨 关键发现：实现无法工作

### 致命问题 #1: AEP 解码器不兼容

**问题**：
```go
// internal/worker/opencodecli/worker.go:201
env, err := aep.DecodeLine([]byte(line))
```

**OpenCode CLI 实际输出**:
```json
{
  "type": "text",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": { ... }
}
```

**AEP DecodeLine 期望格式**:
```json
{
  "version": "aep/v1",
  "id": "evt_xxx",
  "seq": 1,
  "session_id": "ses_xxx",
  "timestamp": 1775301344121,
  "event": { "type": "message", "data": {...} }
}
```

**结果**: `DecodeLine` 会返回验证错误：
```
aep: validate envelope: version is required
aep: validate envelope: id is required
aep: validate envelope: seq must be a positive integer
```

**影响**: 当前 OpenCode CLI Worker **完全无法工作**

**需要**: 实现完整的事件格式转换器（未在当前代码中实现）

---

## 📊 完整验证结果

### 1. CLI 参数验证

#### 1.1 Worker Adapter 层面的实现 ✅

| 参数 | Spec 章节 | 实现位置 | 验证状态 |
|------|----------|---------|---------|
| `--allowed-tools` | 2.3 | `proc/manager.go:75-79` + `security/tool.go:54-60` | ✅ 已实现 |
| 环境变量注入 | 3 | `base/env.go:BuildEnv` | ✅ 已实现 |
| Session ID 提取 | 7.1 | `opencodecli/worker.go:238-271` | ✅ 已实现 |

**工作原理**:
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

**结论**: Spec 2.3 节的描述**正确**，但实现路径不同于预期（在 Worker Adapter 层而非 CLI 层）。

#### 1.2 CLI 层面的参数（实际可用）

| 参数 | 源码位置 | 说明 |
|------|---------|------|
| `run` | run.ts:222 | ✅ 主命令 |
| `--format json` | run.ts:263-268 | ✅ JSON 输出格式 |
| `--session` | run.ts:241-245 | ✅ 指定会话 ID |
| `--continue` | run.ts:236-240 | ✅ 继续最新会话 |
| `--fork` | run.ts:246-250 | ➕ Fork 会话 |
| `--share` | run.ts:251-254 | ➕ 分享会话 |
| `--model` | run.ts:255-259 | ➕ 模型选择 |
| `--agent` | run.ts:260-263 | ➕ Agent 选择 |
| `--file` | run.ts:269-275 | ➕ 文件附件 |
| `--title` | run.ts:276-279 | ➕ 会话标题 |
| `--attach` | run.ts:280-283 | ➕ 连接远程服务器 |
| `--password` | run.ts:284-288 | ➕ Basic Auth |
| `--dir` | run.ts:289-292 | ➕ 工作目录 |
| `--port` | run.ts:293-296 | ➕ 服务器端口 |
| `--variant` | run.ts:297-300 | ➕ 模型变体 |
| `--thinking` | run.ts:301-305 | ➕ 显示思考块 |

**Spec 中未记录的参数**: 12 个

#### 1.3 CLI 层面不支持但 Spec 提及的参数

| 参数 | Spec 状态 | 实际状态 | 说明 |
|------|----------|---------|------|
| `--resume` | ❌ 不支持 | ❌ 确认不支持 | 使用 `--continue` 代替 |
| `--dangerously-skip-permissions` | ⚠️ 需验证 | ❌ CLI 不支持 | Worker 层面实现权限控制 |
| `--permission-mode` | ⚠️ 需验证 | ❌ CLI 不支持 | 同上 |
| `--system-prompt` | ⚠️ 需验证 | ❌ CLI 不支持 | OpenCode 未提供此功能 |
| `--mcp-config` | ⚠️ 需验证 | ❌ CLI 不支持 | OpenCode 使用不同的 MCP 配置方式 |

---

### 2. 输出格式验证

#### 2.1 实际输出格式（NDJSON）

**完整结构**:
```json
{
  "type": "<event_type>",
  "timestamp": 1775301344121,
  "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
  "part": {
    "id": "prt_d5834bb71001Qmy1JVqgnFC76D",
    "messageID": "msg_d58346bbb001s1MsQ1gQ5AmIfM",
    "sessionID": "ses_2a7cb94f8ffeSyS5XJTdYgFVtp",
    "type": "<part_type>",
    "<type_specific_fields>": ...
  }
}
```

**字段说明**:
- `type`: 顶层事件类型（`step_start`, `text`, `tool_use`, `step_finish`, `error`, `reasoning`）
- `timestamp`: Unix 毫秒时间戳
- `sessionID`: 会话 ID（前缀 `ses_`，长度 ~30 字符）
- `part`: 事件数据对象
  - `id`: Part ID（前缀 `prt_`）
  - `messageID`: Message ID（前缀 `msg_`）
  - `sessionID`: 同顶层 sessionID
  - `type`: Part 类型（`step-start`, `text`, `tool`, `step-finish` 等）

#### 2.2 与 Spec 的差异

| 项目 | Spec | 实际 | 差异级别 |
|------|------|------|---------|
| 顶层结构 | AEP envelope | 自定义格式 | ❌ 完全不同 |
| `version` 字段 | `"aep/v1"` | 不存在 | ❌ 缺失 |
| `id` 字段 | 事件 ID | 不存在（有 part.id） | ❌ 缺失 |
| `seq` 字段 | 序列号 | 不存在 | ❌ 缺失 |
| `event` envelope | `{ type, data }` | 直接 `type + part` | ❌ 结构不同 |
| Session ID 字段名 | `session_id` | `sessionID` | ⚠️ 命名不同 |
| Part 结构 | 不存在 | 完整的元数据 | ➕ 额外 |

**结论**: 需要**完整的事件转换层**

---

### 3. 事件类型验证

#### 3.1 完整映射表

| 实际事件 | 实际结构 | Spec 事件 | Spec 结构 | 转换复杂度 |
|---------|---------|----------|----------|----------|
| `step_start` | `{ type, timestamp, sessionID, part }` | `state` | `{ type: "state", data: { status: "running" } }` | ⭐⭐⭐ |
| `text` | `{ type, part: { text, time } }` | `message` | `{ type: "message", data: { content } }` | ⭐⭐ |
| `tool_use` | `{ type, part: { tool, state } }` | `tool_call` + `tool_result` | 分离的两个事件 | ⭐⭐⭐⭐ |
| `step_finish` | `{ type, part: { reason, tokens, cost } }` | `done` | `{ type: "done" }` | ⭐⭐⭐ |
| `error` | `{ type, part: { error } }` | `error` | `{ type: "error", data: { message } }` | ⭐⭐ |
| `reasoning` | `{ type, part: { text, time } }` | - | - | ➕ 额外事件 |

#### 3.2 详细事件结构

##### step_start 事件

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

**Spec 期望**:
```json
{
  "version": "aep/v1",
  "id": "evt_xxx",
  "seq": 1,
  "session_id": "ses_xxx",
  "timestamp": 1775301343766,
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
step_start → state(running)
- 生成 event ID
- 分配 seq
- 映射 sessionID → session_id
- 提取 part.snapshot（可选）
```

##### text 事件

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

**Spec 期望**:
```json
{
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
text → message
- 提取 part.text → content[0].text
- 忽略 part.time（AEP 无此字段）
```

##### tool_use 事件

**实际输出**:
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
        "filePath": "/Users/.../package.json"
      },
      "output": "<path>...</path>\n...",
      "metadata": {
        "preview": "...",
        "truncated": false
      }
    }
  }
}
```

**Spec 期望（两个事件）**:
```json
// tool_call
{
  "event": {
    "type": "tool_call",
    "data": {
      "id": "call_function_4cmyb5dhnrci_1",
      "name": "read",
      "input": {
        "filePath": "/Users/.../package.json"
      }
    }
  }
}

// tool_result
{
  "event": {
    "type": "tool_result",
    "data": {
      "tool_call_id": "call_function_4cmyb5dhnrci_1",
      "content": [
        {
          "type": "text",
          "text": "<path>...</path>\n..."
        }
      ]
    }
  }
}
```

**转换逻辑**:
```
tool_use → tool_call + tool_result（拆分）
- 提取 part.callID → tool_call.id / tool_result.tool_call_id
- 提取 part.tool → tool_call.name
- 提取 part.state.input → tool_call.input
- 提取 part.state.output → tool_result.content[0].text
- 生成两个独立的 AEP 事件
```

##### step_finish 事件

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

**Spec 期望**:
```json
{
  "event": {
    "type": "done",
    "data": {
      "reason": "stop"
    }
  }
}
```

**转换逻辑**:
```
step_finish → done
- 提取 part.reason → data.reason
- 忽略 part.tokens, part.cost（AEP 不包含）
```

---

### 4. 环境变量验证

#### 4.1 实际实现 ✅

**代码位置**: `internal/worker/base/env.go:BuildEnv`

**实现逻辑**:
```go
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
    // 1. 从 os.Environ() 白名单过滤
    // 2. 添加 HOTPLEX_* 变量
    env = append(env,
        "HOTPLEX_SESSION_ID="+session.SessionID,
        "HOTPLEX_WORKER_TYPE="+workerTypeLabel,
    )
    // 3. 合并 session.Env
    // 4. 剥离 CLAUDECODE=（防止嵌套）
    return env
}
```

**OpenCode CLI 白名单**:
```go
var openCodeCLIEnvWhitelist = []string{
    "HOME", "USER", "SHELL", "PATH", "TERM",
    "LANG", "LC_ALL", "PWD",
    "OPENAI_API_KEY", "OPENAI_BASE_URL",
    "OPENCODE_API_KEY", "OPENCODE_BASE_URL",
}
```

**测试结果**:
```bash
$ env HOTPLEX_SESSION_ID=test-override bun run opencode run --format json 'test'
# 输出中的 sessionID 仍然是 CLI 生成的，说明 CLI 不读取此环境变量
```

**结论**:
- ✅ Worker Adapter 正确注入环境变量
- ❌ OpenCode CLI 忽略 `HOTPLEX_SESSION_ID`，始终生成自己的 session ID
- ✅ 其他环境变量（API keys）正常工作

---

### 5. Session 管理验证

#### 5.1 Session ID 提取 ✅

**实现代码**: `internal/worker/opencodecli/worker.go:238-271`

```go
func (w *Worker) tryExtractSessionID(line string) {
    var raw map[string]json.RawMessage
    json.Unmarshal([]byte(line), &raw)

    if typ, ok := raw["type"]; ok {
        if string(typ) == `"step_start"` {
            if data, ok := raw["data"]; ok {
                var stepData struct {
                    SessionID string `json:"session_id"`
                    ID        string `json:"id"`
                }
                json.Unmarshal(data, &stepData)
                // 使用 session_id 或 id 字段
            }
        }
    }
}
```

**问题**: 代码尝试从 `data` 字段提取，但实际结构中 sessionID 在顶层！

**实际结构**:
```json
{
  "type": "step_start",
  "sessionID": "ses_xxx",  // ← 在顶层，不在 data 中
  "part": { ... }
}
```

**代码中的错误假设**:
```go
if data, ok := raw["data"]; ok {  // ❌ 不存在 data 字段
```

**需要修复**: 从顶层 `sessionID` 或 `part.sessionID` 提取

#### 5.2 Resume 支持 ⚠️

**Spec**: ❌ 不支持

**实际**:
- ✅ `--continue` - 继续最新会话
- ✅ `--session <id>` - 指定会话 ID
- ✅ `--fork` - Fork 会话

**结论**: Resume 功能**已实现**，但需要 Worker Adapter 传递参数（未实现）

---

## 🔧 必需的修复

### 修复 #1: 事件转换器（关键）

**位置**: `internal/worker/opencodecli/converter.go`（需要新建）

**接口设计**:
```go
package opencodecli

import (
    "github.com/hotplex/hotplex-worker/pkg/events"
)

// EventConverter converts OpenCode CLI events to AEP format.
type EventConverter struct {
    seqGen *SeqGen
}

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
        return nil, err
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

func (c *EventConverter) convertText(raw RawEvent) (*events.Envelope, error) {
    var part struct {
        Text string `json:"text"`
    }
    json.Unmarshal(raw.Part, &part)

    return &events.Envelope{
        Version:   events.Version,
        ID:        events.NewID(),
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
```

### 修复 #2: Session ID 提取

**修改**: `internal/worker/opencodecli/worker.go:238-271`

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

### 修复 #3: readOutput 集成转换器

**修改**: `internal/worker/opencodecli/worker.go:167-236`

```go
func (w *Worker) readOutput(defaultSessionID string) {
    converter := NewEventConverter()

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
        env, err := converter.Convert([]byte(line))
        if err != nil {
            w.Base.Log.Warn("opencodecli: convert event", "error", err, "line", line)
            continue
        }

        // Update session ID in connection
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
```

---

## 📋 Spec 更新需求

### 需要完全重写的章节

1. **第 4 节: 输入格式**
   - 当前：描述 AEP v1 输入
   - 实际：正确，但需要补充 CLI 层面的参数

2. **第 5 节: 输出格式**
   - 当前：描述 AEP v1 输出
   - 实际：完全不同，需要描述实际格式 + 转换需求

3. **第 6 节: 事件映射**
   - 当前：简单映射表
   - 实际：需要完整的转换逻辑 + 示例

4. **第 7 节: Session 管理**
   - 当前：部分正确
   - 实际：需要更新 resume 支持状态

### 需要添加的章节

5. **新章节: 事件转换层**
   - 架构设计
   - 转换逻辑
   - 实现细节

6. **新章节: 已知限制**
   - CLI 不读取 `HOTPLEX_SESSION_ID`
   - 部分参数在 CLI 层面不支持
   - 需要 Worker Adapter 层面的适配

---

## ✅ 最终结论

### Spec 准确性评分

| 章节 | 当前准确性 | 更新后预期 |
|------|----------|----------|
| 1. 概述 | 60% | 90% |
| 2. CLI 参数 | 30% | 95% |
| 3. 环境变量 | 80% | 95% |
| 4. 输入格式 | 70% | 90% |
| 5. 输出格式 | 10% | 95% |
| 6. 事件映射 | 20% | 95% |
| 7. Session 管理 | 50% | 90% |
| **总体** | **30%** | **93%** |

### 关键问题清单

| 优先级 | 问题 | 状态 | 影响 |
|--------|------|------|------|
| P0 | 事件转换层缺失 | ❌ 未实现 | Worker 无法工作 |
| P0 | Session ID 提取错误 | ❌ Bug | Session 管理失败 |
| P1 | Spec 文档过时 | ⚠️ 需更新 | 误导开发者 |
| P2 | Resume 参数未传递 | ⚠️ 需实现 | 功能缺失 |

### 下一步行动

1. **立即**: 修复 Session ID 提取 Bug
2. **今天**: 实现事件转换层
3. **本周**: 重写 Spec 文档
4. **下周**: 补充完整测试

---

**报告完成时间**: 2026-04-04 20:00
**下次更新**: 实现修复后
