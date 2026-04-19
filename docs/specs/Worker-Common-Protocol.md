---
type: spec
tags:
  - project/HotPlex
  - worker/common
  - architecture/protocol
date: 2026-04-04
status: implemented
progress: 100
completion_date: 2026-04-04
---

# Worker 公共协议规范

> 本文档定义了所有 HotPlex Worker Adapter 共享的公共协议层规范。
> 每个具体 Worker 的规格应引用本文档的对应章节，而非重复定义。
>
> **适用范围**：Claude Code Worker、OpenCode Server Worker
>
> 高阶设计见 [[Worker-Gateway-Design]] §8。

---

## 1. 概述

### 1.1 三层架构

```
┌─────────────────────────────────────────────────────────────────┐
│                     Worker Adapter 层                             │
│              (每个 Worker 特有实现)                               │
├─────────────────────────────────────────────────────────────────┤
│  • Transport: stdio / HTTP+SSE / WebSocket                     │
│  • Protocol: 私有 NDJSON / AEP v1 / SDK                         │
│  • 会话模型: 创建 → 运行 → 终止                                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ 本文档定义
┌─────────────────────────────────────────────────────────────────┐
│                     公共协议层                                     │
│              (所有 Worker 共享)                                   │
├─────────────────────────────────────────────────────────────────┤
│  • AEP v1 Envelope 结构                                         │
│  • NDJSON 安全序列化                                             │
│  • 背压处理策略                                                  │
│  • 分层终止流程                                                  │
│  • 环境变量白名单规范                                            │
│  • Capability 接口定义                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     BaseWorker 层                                 │
│              (hotplex-worker 内部共享)                            │
├─────────────────────────────────────────────────────────────────┤
│  • BaseWorker: Terminate/Kill/Wait/Health/LastIO                 │
│  • proc.Manager: PGID 隔离进程管理                               │
│  • base.Conn: TrySend 非阻塞发送                                 │
│  • base.BuildEnv: 白名单 + HOTPLEX_* 注入                        │
│  • Security: StripNestedAgent()                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 Worker 类型矩阵

| Worker | Transport | Protocol | Resume | Control |
|--------|-----------|----------|--------|---------|
| **Claude Code** | stdio | SDK NDJSON | ✅ | ✅ |
| **OpenCode Server** | HTTP+SSE | AEP v1 NDJSON | ✅ | ❌ |

---

## 2. AEP v1 Envelope 结构

### 2.1 格式定义

所有 Worker 使用统一的 AEP v1 Envelope 结构进行消息封装：

```go
// pkg/events/events.go:72-85
type Envelope struct {
    Version   string   `json:"version"`    // "aep/v1"
    ID        string   `json:"id"`         // "evt_<uuid>"
    Seq       int64    `json:"seq"`        // 单调递增序号
    Priority  Priority `json:"priority,omitempty"` // "control" | "data"
    SessionID string   `json:"session_id"` // 会话标识符
    Timestamp int64    `json:"timestamp"`  // Unix ms
    Event     Event    `json:"event"`      // { Type, Data }
}
```

### 2.2 Event 类型

```go
// pkg/events/events.go:15-34
type EventType string

const (
    // 双向消息
    Input       EventType = "input"        // C→S: 用户输入
    Message     EventType = "message"      // S→C: 完整消息
    MessageDelta EventType = "message.delta" // S→C: 流式增量

    // 工具调用
    ToolCall    EventType = "tool_call"    // S→C: 工具调用通知
    ToolResult  EventType = "tool_result"  // S→C: 工具执行结果

    // 会话状态
    State       EventType = "state"        // S→C: 会话状态变更
    Done        EventType = "done"         // S→C: Turn 结束

    // 错误与控制
    Error       EventType = "error"        // S→C: 错误通知
    Ping        EventType = "ping"         // 双向: 心跳
    Pong        EventType = "pong"         // 双向: 心跳响应
    Control     EventType = "control"       // 双向: 控制动作
    Raw         EventType = "raw"           // S→C: 透传

    // 扩展
    MessageStart EventType = "message.start" // S→C: 消息开始
    MessageEnd   EventType = "message.end"   // S→C: 消息结束
    Step        EventType = "step"           // S→C: 执行步骤
    Reasoning   EventType = "reasoning"       // S→C: 思考过程
)
```

### 2.3 Priority 语义

| Priority | 用途 | 处理方式 |
|----------|------|----------|
| `control` | 控制消息（权限请求等） | **不得丢弃**，背压时阻塞 |
| `data` | 数据消息（delta、raw 等） | **可丢弃**，背压时静默丢弃 |
| (空) | 默认为 `data` | 同上 |

---

## 3. NDJSON 安全序列化

### 3.1 安全要求

**必须转义 U+2028（行分隔符）和 U+2029（段分隔符）**，否则解析器会在这些字符处截断。

### 3.2 实现代码

```go
// pkg/aep/codec.go
var lineTerminators = regexp.MustCompile(`[\u2028\u2029]`)

func escapeJSTerminators(data []byte) []byte {
    return lineTerminators.ReplaceAllFunc(data, func(b []byte) []byte {
        switch {
        case bytes.Equal(b, []byte{0xE2, 0x80, 0xA8}):
            return []byte("\\u2028")
        case bytes.Equal(b, []byte{0xE2, 0x80, 0xA9}):
            return []byte("\\u2029")
        }
        return b
    })
}

func Encode(w io.Writer, env *events.Envelope) error {
    env.Version = events.Version
    if env.Timestamp == 0 {
        env.Timestamp = nowMillis()
    }
    data, err := json.Marshal(env)
    if err != nil {
        return fmt.Errorf("aep: marshal envelope: %w", err)
    }
    data = escapeJSTerminators(data)  // 转义 U+2028/U+2029
    data = append(data, '\n')
    _, err = w.Write(data)
    return err
}
```

### 3.3 行格式

每行一个 JSON 对象，以 `\n` 换行：

```json
{"version":"aep/v1","id":"evt_xxx","seq":1,"session_id":"sess_xxx","timestamp":1712234567890,"event":{"type":"input","data":{"content":"..."}}}
{"version":"aep/v1","id":"evt_yyy","seq":2,"session_id":"sess_xxx","timestamp":1712234567900,"event":{"type":"message.delta","data":{"content":"..."}}}
```

---

## 4. 背压处理策略

### 4.1 Channel 配置

| 参数 | 值 | 说明 |
|------|-----|------|
| **Channel 容量** | 256 | recvCh buffer 大小 |
| **超时处理** | 静默丢弃 | 背压时不阻塞，写入方不报错 |

### 4.2 实现代码

```go
// internal/worker/base/conn.go:61-68
func (c *Conn) TrySend(env *events.Envelope) bool {
    select {
    case c.recvCh <- env:
        return true
    default:
        return false  // 静默丢弃，不阻塞
    }
}
```

### 4.3 丢弃策略

| Priority | 丢弃行为 |
|----------|----------|
| `data` | **静默丢弃**（delta、raw 等流式数据） |
| `control` | **不得丢弃**（权限请求必须送达） |

### 4.4 日志记录

静默丢弃时应记录警告日志：

```go
if !w.Conn.TrySend(env) {
    w.Log.Warn("worker: recv channel full, dropping message",
        "session_id", env.SessionID,
        "event_type", env.Event.Type)
}
```

---

## 5. 分层终止流程

### 5.1 进程生命周期

```
Gateway SIGTERM
    ↓
proc.Terminate(ctx, SIGTERM, 5s)
    ↓
SIGTERM → 进程组（Worker 子进程）
    ↓ (5s 超时)
SIGKILL → 进程组
```

### 5.2 实现代码

```go
// internal/worker/base/worker.go - BaseWorker.Terminate
func (w *BaseWorker) Terminate(ctx context.Context) error {
    w.Mu.Lock()
    proc := w.Proc
    w.Mu.Unlock()

    if proc == nil {
        return nil
    }

    // proc.Terminate: SIGTERM → 5s grace → SIGKILL
    if err := proc.Terminate(ctx, syscall.SIGTERM, gracefulShutdownTimeout); err != nil {
        return fmt.Errorf("base: terminate: %w", err)
    }

    w.Mu.Lock()
    w.Proc = nil
    w.Mu.Unlock()

    return nil
}

// internal/worker/proc/manager.go - 分层终止实现
func (m *Manager) Terminate(ctx context.Context, sig syscall.Signal, timeout time.Duration) error {
    // 1. 发送 SIGTERM
    if err := m.signalProcessGroup(sig); err != nil {
        return err
    }

    // 2. 等待进程退出（带超时）
    done := make(chan struct{})
    go func() {
        m.Wait()
        close(done)
    }()

    select {
    case <-done:
        return nil  // 正常退出
    case <-time.After(timeout):
        // 3. 超时后强制 SIGKILL
        return m.Kill()
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### 5.3 PGID 隔离

进程使用 `Setpgid: true` 创建，确保信号能传播到整个进程组：

```go
// internal/worker/proc/manager.go
attr := &syscall.SysProcAttr{
    Setpgid: true,  // 创建独立的进程组
}
cmd := exec.CommandContext(ctx, name, args...)
cmd.SysProcAttr = attr
```

---

## 6. 环境变量白名单规范

### 6.1 白名单分层

每个 Worker 必须定义三层环境变量白名单：

| 层级 | 变量类型 | 说明 |
|------|----------|------|
| **系统层** | `HOME`, `USER`, `PATH`, `TERM`, `LANG`, `LC_ALL`, `PWD` | 基础系统环境 |
| **供应商层** | `OPENAI_*`, `ANTHROPIC_*`, `OPENCODE_*` | API 密钥和端点 |
| **HotPlex 层** | `HOTPLEX_SESSION_ID`, `HOTPLEX_WORKER_TYPE` | 会话追踪 |

### 6.2 BuildEnv 实现

```go
// internal/worker/base/env.go
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
    env := os.Environ()

    // 1. 过滤白名单
    whitelistSet := make(map[string]bool)
    for _, v := range whitelist {
        whitelistSet[v] = true
    }

    // 支持前缀匹配（如 OPENAI_*）
    filtered := make([]string, 0, len(env))
    for _, e := range env {
        key := strings.SplitN(e, "=", 2)[0]
        if whitelistSet[key] || hasPrefixMatch(key, whitelist) {
            filtered = append(filtered, e)
        }
    }

    // 2. 注入 HotPlex 变量
    filtered = append(filtered,
        "HOTPLEX_SESSION_ID="+session.SessionID,
        "HOTPLEX_WORKER_TYPE="+workerTypeLabel,
    )

    // 3. 追加 Session 环境变量
    for k, v := range session.Env {
        filtered = append(filtered, k+"="+v)
    }

    return filtered
}

func hasPrefixMatch(key string, patterns []string) bool {
    for _, p := range patterns {
        if strings.HasSuffix(p, "_*") && strings.HasPrefix(key, p[:len(p)-1]) {
            return true
        }
    }
    return false
}
```

### 6.3 HotPlex 注入变量

| 变量 | 说明 | 示例 |
|------|------|------|
| `HOTPLEX_SESSION_ID` | 会话唯一标识 | `sess_abc123` |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签 | `claude-code`, `opencode-server` |

### 6.4 StripNestedAgent

必须移除 `CLAUDECODE=` 防止嵌套调用：

```go
// internal/security/env.go
func StripNestedAgent(env []string) []string {
    prefix := "CLAUDECODE="
    filtered := make([]string, 0, len(env))
    for _, e := range env {
        if strings.HasPrefix(e, prefix) {
            continue  // 防止嵌套 agent 调用
        }
        filtered = append(filtered, e)
    }
    return filtered
}
```

---

## 7. Capability 接口定义

### 7.1 接口规范

```go
// internal/worker/worker.go:84-100
type Worker interface {
    // 类型标识
    Type() WorkerType

    // 能力查询
    SupportsResume() bool      // 是否支持会话恢复
    SupportsStreaming() bool   // 是否支持流式输出
    SupportsTools() bool      // 是否支持工具调用

    // 环境与资源
    EnvWhitelist() []string    // 环境变量白名单
    SessionStoreDir() string   // 本地会话存储目录（无则为空）
    MaxTurns() int            // 最大轮次（0=无限制）
    Modalities() []string     // 支持的模态（text, code, image）

    // 生命周期
    Start(ctx context.Context, session SessionInfo) error
    Input(ctx context.Context, content string, metadata map[string]any) error
    Resume(ctx context.Context, session SessionInfo) error
    Terminate(ctx context.Context) error

    // 连接
    Conn() SessionConn
}
```

### 7.2 WorkerType 常量

```go
// internal/worker/worker.go:10-13
const (
    TypeClaudeCode  WorkerType = "claude-code"
    TypeOpenCodeSrv WorkerType = "opencode-server"
    TypePimon       WorkerType = "pimon"
)
```

### 7.3 Capability 对比矩阵

| Capability | Claude Code | OpenCode Server |
|------------|:-----------:|:---------------:|
| `Type()` | `claude-code` | `opencode-server` |
| `SupportsResume()` | ✅ | ✅ |
| `SupportsStreaming()` | ✅ | ✅ |
| `SupportsTools()` | ✅ | ✅ |
| `EnvWhitelist()` | `ANTHROPIC_*` | `OPENAI_*`, `OPENCODE_*` |
| `SessionStoreDir()` | `~/.claude/...` | `""` |
| `MaxTurns()` | 可配置 | 0（无限制） |
| `Modalities()` | `["text", "code"]` | `["text", "code"]` |

---

## 8. 错误处理模式

### 8.1 Worker 输出限制

```go
// internal/worker/proc/manager.go:291-305
scanner := bufio.NewScanner(proc.Stdout())
buf := make([]byte, 64*1024)           // 初始 64KB
scanner.Buffer(buf, 10*1024*1024)     // 最大 10MB

for scanner.Scan() {
    line := scanner.Text()
    // 处理每一行...
}

if err := scanner.Err(); err != nil {
    if err == bufio.ErrTooLong {
        // 单行超过 10MB
        return fmt.Errorf("worker output limit exceeded (10 MB line)")
    }
    return err
}
```

### 8.2 解析错误处理

```go
// 典型模式：解析错误仅记录警告，继续处理后续行
env, err := aep.DecodeLine([]byte(line))
if err != nil {
    w.Log.Warn("worker: decode line", "error", err, "line", line)
    continue  // 不中断，继续处理
}
```

### 8.3 崩溃检测

```go
// internal/gateway/bridge.go:195-218
exitCode, _ := m.Proc.Wait()
if exitCode != 0 {
    crashDone := events.NewEnvelope(
        aep.NewID(),
        sessionID,
        b.hub.NextSeq(sessionID),
        events.Done,
        events.DoneData{
            Success: false,
            Stats:   map[string]any{"crash_exit_code": exitCode},
        },
    )
    _ = b.hub.SendToSession(context.Background(), crashDone)
}
```

---

## 9. 源码关键路径

### 9.1 公共组件

| 组件 | 路径 | 说明 |
|------|------|------|
| BaseWorker | `internal/worker/base/worker.go` | 共享生命周期管理 |
| base.Conn | `internal/worker/base/conn.go` | stdio 连接（TrySend） |
| base.BuildEnv | `internal/worker/base/env.go` | 白名单 + HOTPLEX_* 注入 |
| proc.Manager | `internal/worker/proc/manager.go` | PGID 隔离进程管理 |
| AEP Codec | `pkg/aep/codec.go` | NDJSON 编解码 |
| StripNestedAgent | `internal/security/env.go` | 嵌套调用防护 |
| Worker Interface | `internal/worker/worker.go` | Worker 接口定义 |
| Events | `pkg/events/events.go` | AEP 事件类型定义 |

### 9.2 Worker 特定实现

| Worker | 路径 |
|--------|------|
| Claude Code | `internal/worker/claudecode/` |
| OpenCode Server | `internal/worker/opencodeserver/` |

---

## 10. 与 Worker Spec 的引用关系

本文档是所有 Worker Spec 的公共基础，各 Worker Spec 应引用对应章节：

| Worker Spec | 引用章节 |
|-------------|---------|
| `Worker-ClaudeCode-Spec.md` | §2（Envelope）、§3（NDJSON）、§4（背压）、§5（终止）、§6（环境变量）、§7（Capability） |
| `Worker-OpenCode-Server-Spec.md` | §2（Envelope）、§3（NDJSON）、§4（背压）、§5（终止）、§6（环境变量）、§7（Capability） |

---

## 11. 实现状态

> 更新于 2026-04-04

### 11.1 汇总

| 组件 | 状态 | 说明 |
|------|------|------|
| AEP v1 Envelope | ✅ 已实现 | `pkg/events/events.go` |
| NDJSON 安全序列化 | ✅ 已实现 | `pkg/aep/codec.go` |
| 背压处理策略 | ✅ 已实现 | `base/conn.go` |
| 分层终止流程 | ✅ 已实现 | `base/worker.go`, `proc/manager.go` |
| 环境变量白名单 | ✅ 已实现 | `base/env.go` |
| Capability 接口 | ✅ 已实现 | `internal/worker/worker.go` |
| StripNestedAgent | ✅ 已实现 | `security/env.go` |

### 11.2 架构亮点

- ✅ **统一 Envelope 结构**：所有 Worker 使用相同的 AEP v1 封装
- ✅ **NDJSON 安全**：U+2028/U+2029 自动转义，防止解析截断
- ✅ **背压策略统一**：256 channel，delta 静默丢弃，control 不得丢弃
- ✅ **分层终止**：SIGTERM → 5s → SIGKILL，优雅关闭
- ✅ **环境变量隔离**：白名单 + 前缀匹配 + HOTPLEX_* 注入
- ✅ **PGID 隔离**：信号正确传播到所有子进程
- ✅ **防嵌套调用**：StripNestedAgent 移除 CLAUDECODE=
