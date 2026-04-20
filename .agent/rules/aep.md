---
paths:
  - "**/aep/*.go"
---

# AEP v1 协议规范

> hotplex-worker 对外暴露的统一 WebSocket 全双工通信协议
> 参考文档：`docs/specs/Acceptance-Criteria.md` §AEP-001 ~ §AEP-030

## Envelope 结构
每条 AEP v1 消息必须包含以下字段：

| 字段 | 类型 | 要求 |
|------|------|------|
| `id` | string | non-empty，消息唯一标识 |
| `version` | string | 必须为 `aep/v1`，否则返回 `VERSION_MISMATCH` |
| `session_id` | string | non-empty |
| `seq` | int64 | 从 1 开始严格递增，同 session 内原子分配 |
| `timestamp` | int64 | Unix ms，> 0 |
| `event` | object | non-null，包含 `type` 字段 |
| `priority` | string | 缺失默认为 `data`；`control` 跳过 backpressure |

### 编解码约束
```go
// DecodeLine 必须验证所有必填字段
func DecodeLine(line []byte) (*Envelope, error) {
    dec := json.NewDecoder(bytes.NewReader(line))
    dec.DisallowUnknownFields() // 拒绝未知字段
    // ...
}

// EncodeLine 使用 json.Encoder，避免 []byte→string 复制
func EncodeLine(w io.Writer, env *Envelope) error {
    enc := json.NewEncoder(w)
    return enc.Encode(env)
}
```

## 消息类型

### C→S（Client → Server）
- `init`：握手，必须是 WS 连接建立后第一帧
- `input`：用户任务，Session 繁忙时硬拒绝
- `control`：terminate / delete
- `ping`：心跳，回复 pong

### S→C（Server → Client）
- `init_ack`：握手响应
- `state`：状态变更（created/running/idle/terminated）
- `message.delta`：流式输出（text/code/image）
- `message`：Turn 结束时完整消息聚合
- `tool_call` / `tool_result`：Tool 调用通知（AUTONOMOUS 模式）
- `done`：Turn 终止符
- `error`：错误通知
- `pong`：ping 响应
- `control`：reconnect / throttle（Server 发起）

## Seq 分配规则

### 序号分配范围

**需要序号的消息类型**：
- 业务消息：`input`, `message`, `message.delta`, `message.done`, `done`
- 状态消息：`state`, `error`, `reasoning`
- 工具消息：`tool_call`, `tool_result`
- 原始消息：`raw`
- 控制消息：`control`

**不需要序号的消息类型**：
- 心跳消息：`ping`, `pong`
  - 这些是 WebSocket 层面的控制消息
  - 与业务流程无关
  - Seq 字段为 0（未分配）

### 实现方式

```go
// conn.go ReadPump
env.SessionID = c.sessionID
env.OwnerID = c.userID

// Only assign seq to business messages, not heartbeat
if env.Event.Type != events.Ping {
    env.Seq = c.hub.NextSeq(c.sessionID)
}
// Ping/Pong messages have seq=0 (unassigned)
```

### Seq 生成算法

```go
// hub.go NextSeq - 原子分配，保证 session 内单调递增
func (g *SeqGen) NextSeq(sessionID string) int64 {
    g.mu.Lock()
    defer g.mu.Unlock()
    n := g.seq[sessionID]
    g.seq[sessionID] = n + 1
    return n
}
```

**序号特性**：
- 每个 session 独立的序号空间
- 从 1 开始严格递增
- 原子操作保证并发安全
- 0 表示"未分配序号"

### Backpressure 丢弃规则

```go
// hub.go SendToSession
if env.Event.Type == "message.delta" || env.Event.Type == "raw" {
    // Non-blocking send, drop if channel full
    select {
    case ch <- env:
        return nil
    default:
        sessionDropped[sessionID] = true
        return nil  // Don't return error
    }
}
// Critical events (state/done/error) block until sent
ch <- env
```

**丢弃标记**：
- `sessionDropped[sessionID] = true` 表示有 delta 被丢弃
- `done` 事件会检查此标记，在 `stats.dropped` 中体现
- 丢弃的 delta **不消耗 seq**（保持序号连续性）

## Backpressure — 有界通道与 delta 丢弃
```go
// hub.broadcast 通道容量由 broadcastQueueSize 决定（默认 256）
ch := make(chan *Envelope, cfg.BroadcastQueueSize)

func SendToSession(sessionID string, env *Envelope) error {
    if env.Event.Type == "message.delta" || env.Event.Type == "raw" {
        // 非阻塞 select，通道满时静默丢弃
        select {
        case ch <- env:
            return nil
        default:
            sessionDropped[sessionID] = true
            return nil // 不返回错误
        }
    }
    // 关键事件不可丢弃
    ch <- env
    return nil
}
```

## 时序约束
- Turn 开始：`state(running)` 必须是第一个 S→C event（seq=1）
- Turn 结束：`done` 必须是最后一个 S→C event
- `error` 必须在 `done` 之前
- `tool_result.tool_call_id` 必须与对应 `tool_call.id` 匹配

## Init 握手
```go
// performInit 必须在 30s 内完成
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// 第一帧类型必须为 init
if env.Event.Type != "init" {
    return sendInitError(conn, "PROTOCOL_VIOLATION")
}
```

## Worker 类型映射
| Worker | 事件映射 |
|--------|---------|
| Claude Code | tool_use → tool_call, permission_request → user interaction |
| | step_start → 提取 sessionID |
| OpenCode Server | NDJSON/stdio protocol |
| pi-mono (raw stdout) | 每行 stdout → 一条 message.delta |

## 用户交互事件

### 交互类型
- `permission_request` — S→C: 请求用户授权（tool 执行等）
- `question_request` — S→C: 请求用户回答问题
- `elicitation_request` — S→C: MCP server 请求用户输入

### 交互超时
- 默认 5 分钟自动拒绝（auto-deny）
- 通过 `InteractionManager` 管理，支持 Register/Complete/CancelAll
- 超时后自动发送拒绝响应，避免无限阻塞

### Control 事件
- C→S: `terminate` / `delete` / `gc` / `reset` / `park` / `restart`
- Messaging 通道触发方式：slash 命令 (`/gc`, `/reset`) 或自然语言 (`$gc`, `$休眠`)
- 自然语言触发**必须**带 `$` 前缀，防止误匹配
