---
type: spec
tags:
  - project/HotPlex
  - worker/opencode-cli
  - architecture/integration
date: 2026-04-04
status: implemented
progress: 100
completion_date: 2026-04-04
---

# OpenCode CLI Worker 集成规格

> 本文档详细定义 OpenCode CLI Worker Adapter 与 OpenCode CLI 的集成规格。
> 高阶设计见 [[Worker-Gateway-Design]] §8.2。

---

## 1. 概述

| 维度 | 设计 |
|------|------|
| **Transport** | stdio（stdin/stdout pipe） |
| **Protocol** | AEP v1 NDJSON |
| **进程模型** | 持久进程，多轮复用（Hot-Multiplexing） |
| **源码路径** | `internal/worker/opencodecli/` |
| **OpenCode 源码** | `~/opencode/packages/opencode/src/cli/` |
| **不支持** | Resume（CLI 模式无持久化会话） |

**集成命令**：

```bash
opencode run --format json
```

> OpenCode CLI 通过 `--format json` 输出 NDJSON 事件流，stdin 接收 NDJSON 指令。

---

## 2. CLI 参数

### 2.1 核心参数（v1.0 必须）

| 参数 | 说明 | Impl |
|------|------|------|
| `run` | 运行模式（非交互） | ✅ `worker.go:63` |
| `--format json` | JSON 输出模式（必需） | ✅ `worker.go:64` |

### 2.2 会话控制参数

| 参数 | 说明 | Impl |
|------|------|------|
| `--session-id <id>` | 指定 session ID | ⚠️ CLI 不持久化，仅透传 |
| `--continue` / `-c` | 继续最新会话 | ⚠️ CLI 不支持 |
| `--resume` | 恢复会话 | ⚠️ CLI 不支持 |

### 2.3 工具与权限参数

| 参数 | 说明 | Impl |
|------|------|------|
| `--allowed-tools <list>` | 允许的工具列表（逗号分隔） | ✅ `worker.go:74-76` |
| `--disallowed-tools <list>` | 禁止的工具列表 | ✅ `worker.go:78-80` |
| `--dangerously-skip-permissions` | 跳过所有权限检查 | ⚠️ 需验证 |
| `--permission-mode <mode>` | 权限模式 | ⚠️ 需验证 |

### 2.4 系统提示参数

| 参数 | 说明 | Impl |
|------|------|------|
| `--system-prompt <prompt>` | **替换**默认系统提示 | ⚠️ 需验证 |
| `--append-system-prompt <prompt>` | **追加**到现有系统提示末尾 | ⚠️ 需验证 |

### 2.5 MCP 配置参数

| 参数 | 说明 | Impl |
|------|------|------|
| `--mcp-config <path>` | MCP 服务器配置 JSON 文件 | ⚠️ 需验证 |
| `--strict-mcp-config` | 仅使用指定的 MCP 服务器 | ⚠️ 需验证 |

### 2.6 扩展参数

| 参数 | 说明 | 优先级 | Impl |
|------|------|--------|------|
| `--bare` | 最小化模式 | P2 | ⚠️ 需验证 |
| `--add-dir <dirs>` | 允许工具访问的额外目录 | P2 | ⚠️ 需验证 |
| `--max-budget-usd <amount>` | API 调用最大花费 | P3 | ⚠️ 需验证 |
| `--json-schema <schema>` | 结构化输出验证 | P3 | ⚠️ 需验证 |
| `--include-hook-events` | 包含 hook 生命周期事件 | P3 | ⚠️ 需验证 |
| `--include-partial-messages` | 包含部分消息块 | P3 | ⚠️ 需验证 |
| `--max-turns <n>` | 最大 agentic 轮次 | P2 | ⚠️ 需验证 |

---

## 3. 环境变量

> 详见 [[Worker-Common-Protocol]] §6。

### 3.1 供应商托管变量（白名单）

| 变量 | 说明 | Impl |
|------|------|------|
| `OPENAI_API_KEY` | OpenAI API 密钥 | ✅ 白名单 |
| `OPENAI_BASE_URL` | OpenAI API 端点 | ✅ 白名单 |
| `OPENCODE_API_KEY` | OpenCode API 密钥 | ✅ 白名单 |
| `OPENCODE_BASE_URL` | OpenCode API 端点 | ✅ 白名单 |

### 3.2 HotPlex 注入变量

| 变量 | 说明 | Impl |
|------|------|------|
| `HOTPLEX_SESSION_ID` | 会话标识符 | ✅ `base/env.go` |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签（`opencode-cli`） | ✅ `base/env.go` |

### 3.3 安全集成要求

| 要求 | 说明 | Impl |
|------|------|------|
| **移除 `CLAUDECODE=`** | 防止嵌套调用 | ✅ `security.StripNestedAgent()` |
| **StripNestedAgent** | 嵌套防护 | ✅ `internal/security/env.go` |

---

## 4. 输入格式（stdin → OpenCode CLI）

### 4.1 基本格式

每行一个 JSON 对象（必须使用 `\n` 换行），AEP v1 编码：

```json
{
  "version": "aep/v1",
  "id": "evt_xxx",
  "seq": 1,
  "session_id": "session_xxx",
  "timestamp": 1712234567890,
  "event": {
    "type": "input",
    "data": {
      "content": "user prompt here"
    }
  }
}
```

### 4.2 输入消息字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `event.type` | `input` | 消息类型 |
| `event.data.content` | `string` | 用户输入内容 |
| `event.data.metadata` | `object` | 可选元数据 |

### 4.3 AEP NDJSON 安全序列化

> 详见 [[Worker-Common-Protocol]] §3。

**必须转义 U+2028（行分隔符）和 U+2029（段分隔符）**：
- 实现：`pkg/aep/codec.go` 的 `escapeJSTerminators()` 函数
-背压处理：256 channel，delta 静默丢弃

---

## 5. 输出格式（stdout → Worker Adapter）

### 5.1 NDJSON 事件流

OpenCode CLI 输出多类型 NDJSON 事件，每行一个 JSON 对象：

```json
{"type":"step_start","data":{"id":"step_xxx","session_id":"sess_xxx",...}}
{"type":"message","data":{"role":"assistant","content":[...]}}
{"type":"step_end","data":{"id":"step_xxx",...}}
```

### 5.2 SDK 消息类型

| `type` | `subtype` | 说明 | AEP 映射 |
|--------|-----------|------|----------|
| `step_start` | — | 步骤开始（含 session_id） | `message.start` |
| `message` | — | 完整助手消息 | `message` |
| `message.part.delta` | — | 流式增量（文本/代码） | `message.delta` |
| `message.part.updated` | — | 部分更新 | `message.delta` |
| `tool_use` | — | 工具调用 | `tool_call` |
| `tool_result` | — | 工具结果 | `tool_result` |
| `step_end` | — | 步骤结束 | `step` |
| `error` | — | 错误 | `error` |
| `system` | — | 系统消息 | — |
| `session_created` | — | 会话创建 | `state` |

### 5.3 关键消息示例

**step_start（会话启动）**：

```json
{
  "type": "step_start",
  "data": {
    "id": "step_xxx",
    "session_id": "sess_xxx",
    "metadata": {}
  }
}
```

**message（完整消息）**：

```json
{
  "type": "message",
  "data": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Hello world" },
      { "type": "tool_use", "id": "call_123", "name": "read_file", "input": { "path": "/app/main.go" } }
    ]
  }
}
```

**tool_result（工具结果）**：

```json
{
  "type": "tool_result",
  "data": {
    "tool_use_id": "call_123",
    "content": [{ "type": "text", "text": "file content..." }]
  }
}
```

**error（错误）**：

```json
{
  "type": "error",
  "data": {
    "message": "error message",
    "code": "error_code"
  }
}
```

---

## 6. 事件映射（OpenCode CLI → AEP）

| OpenCode Event | AEP Event Kind | 说明 | Impl |
|----------------|---------------|------|------|
| `step_start` | `message.start` | 步骤/会话开始 | ⚠️ 需实现 |
| `message` | `message` | 完整消息 | ⚠️ 需实现 |
| `message.part.delta` | `message.delta` | 流式文本/代码 | ⚠️ 需实现 |
| `tool_use` | `tool_call` | 工具调用 | ⚠️ 需实现 |
| `tool_result` | `tool_result` | 工具结果 | ⚠️ 需实现 |
| `step_end` | `step` | 步骤结束 | ⚠️ 需实现 |
| `error` | `error` | 错误 | ⚠️ 需实现 |
| `system` | — | 系统消息 | ⚠️ 需实现 |
| `session_created` | `state` | 会话状态 | ⚠️ 需实现 |

---

## 7. Session 管理

### 7.1 Session ID 提取

OpenCode CLI 不接受外部 session ID，会在 `step_start` 事件中返回生成的 session_id。Worker Adapter 自动提取并缓存：

```go
// worker.go:238-271
func (w *Worker) tryExtractSessionID(line string) {
    var raw map[string]json.RawMessage
    if err := json.Unmarshal([]byte(line), &raw); err != nil {
        return
    }
    if typ, ok := raw["type"]; ok {
        if string(typ) == `"step_start"` || string(typ) == "step_start" {
            if data, ok := raw["data"]; ok {
                var stepData struct {
                    SessionID string `json:"session_id"`
                    ID        string `json:"id"`
                }
                if err := json.Unmarshal(data, &stepData); err != nil {
                    return
                }
                if stepData.SessionID != "" {
                    w.mu.Lock()
                    w.sessionID = stepData.SessionID
                    w.mu.Unlock()
                }
            }
        }
    }
}
```

### 7.2 Session 持久化

**CLI 模式不持久化会话**。所有会话状态在进程生命周期内有效。

| 项目 | 路径 |
|------|------|
| 存储位置 | 无（内存） |
| Gateway 追踪 | `~/.hotplex/sessions/<id>.lock` |

### 7.3 Resume 流程

**不支持**。OpenCode CLI Worker 总是启动新会话：

```go
// worker.go:137-139
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    return fmt.Errorf("opencodecli: resume not supported")
}
```

---

## 8. 优雅终止（Graceful Shutdown）

> 详见 [[Worker-Common-Protocol]] §5。

- **终止流程**：SIGTERM → 5s grace → SIGKILL
- **实现**：`base.BaseWorker.Terminate()` 委托 `proc.Terminate()`
- **PGID 隔离**：`Setpgid: true` 确保信号传播到进程组

---

## 9. 错误处理模式

### 9.1 输出解析错误

```go
// worker.go:201-204
env, err := aep.DecodeLine([]byte(line))
if err != nil {
    w.Base.Log.Warn("opencodecli: decode line", "error", err, "line", line)
    continue
}
```

### 9.2 Worker 输出限制

```go
// proc/manager.go:291-305
func() {
    defer func() {
        if p := recover(); p != nil {
            if p == bufio.ErrTooLong {
                scanErr = fmt.Errorf("worker output limit exceeded (10 MB line)")
            } else {
                panic(p)
            }
        }
    }()
}()
```

- **初始缓冲区**：64KB
- **单行硬上限**：10MB（超出 `bufio.ErrTooLong`）

### 9.3 背压处理

> 详见 [[Worker-Common-Protocol]] §4。

- **Channel 容量**：256
- **静默丢弃**：`data` priority 消息（delta、raw）
- **日志记录**：静默丢弃时记录警告

### 9.4 Worker 崩溃检测

```go
// bridge.go:195-218
if exitCode != 0 {
    crashDone := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Done, events.DoneData{
        Success: false,
        Stats:   map[string]any{"crash_exit_code": exitCode},
    })
    _ = b.hub.SendToSession(context.Background(), crashDone)
}
```

---

## 10. Worker Adapter 核心代码

### 10.1 Worker 结构

```go
// worker.go:27-44
type Worker struct {
    *base.BaseWorker
    sessionID string  // 提取自 step_start 事件
    sessionIDMu sync.Mutex
}

func (w *Worker) buildCLIArgs(session worker.SessionInfo) []string {
    args := []string{
        "run",
        "--format", "json",
    }

    // AllowedTools 作为多值 --allowed-tools 参数追加
    // 每个工具单独一个 --allowed-tools 参数
    for _, tool := range session.AllowedTools {
        args = append(args, "--allowed-tools", tool)
    }

    return args
}
```

### 10.2 会话启动

```go
// worker.go:60-102
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // 1. 构建 CLI 参数
    args := w.buildCLIArgs(session)

    // 2. 构建环境变量
    env := base.BuildEnv(session, openCodeCLIEnvWhitelist, "opencode-cli")

    // 3. 创建进程
    proc, err := proc.NewManager(ctx, "opencode", args...)
    if err != nil {
        return fmt.Errorf("opencodecli: new process: %w", err)
    }
    proc.AddEnv(env...)
    proc.SetAllowedTools(session.AllowedTools)

    // 4. 启动进程
    if err := proc.Start(); err != nil {
        return fmt.Errorf("opencodecli: start: %w", err)
    }

    // 5. 创建 stdio 连接
    w.Conn = base.NewConn(proc.Stdin(), proc.Stdout())
    w.Base = &base.BaseWorker{
        Proc: proc,
        Log:  w.Log,
    }

    // 6. 启动输出读取 goroutine
    go w.readOutput()

    return nil
}
```

### 10.3 事件处理循环

```go
// worker.go:180-220
func (w *Worker) readOutput() {
    scanner := bufio.NewScanner(proc.Stdout())
    buf := make([]byte, 64*1024)
    scanner.Buffer(buf, 10*1024*1024)  // 64KB - 10MB

    for scanner.Scan() {
        line := scanner.Text()

        // 尝试提取 session_id
        w.tryExtractSessionID(line)

        // 解析 AEP 事件
        env, err := aep.DecodeLine([]byte(line))
        if err != nil {
            w.Base.Log.Warn("opencodecli: decode line", "error", err)
            continue
        }

        // 发送到 hub
        if err := w.Conn.TrySend(env); err != nil {
            w.Base.Log.Warn("opencodecli: send", "error", err)
        }
    }
}
```

---

## 11. Capability 接口实现

> 详见 [[Worker-Common-Protocol]] §7。

```go
// worker.go:46-56
func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeCLI }
func (w *Worker) SupportsResume() bool    { return false }  // CLI 不支持 Resume
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeCLIEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" }     // CLI 不持久化
func (w *Worker) MaxTurns() int           { return 0 }     // 无限制
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }
```

| Capability | 值 | 说明 |
|------------|---|------|
| `Type` | `TypeOpenCodeCLI` | Worker 类型常量 |
| `SupportsResume` | `false` | CLI 无持久化会话 |
| `SupportsStreaming` | `true` | NDJSON 流式输出 |
| `SupportsTools` | `true` | 工具调用支持 |
| `EnvWhitelist` | `openCodeCLIEnvWhitelist` | 环境变量白名单 |
| `SessionStoreDir` | `""` | CLI 不持久化 |
| `MaxTurns` | `0` | 无限制 |
| `Modalities` | `["text", "code"]` | 支持文本和代码 |

---

## 12. 与 Claude Code Worker 的差异

| 特性 | Claude Code Worker | OpenCode CLI Worker |
|------|-------------------|---------------------|
| **Transport** | stdio | stdio |
| **Protocol** | SDK NDJSON | AEP v1 NDJSON |
| **Session ID** | 外部指定 `--session-id` | 内部生成（从 `step_start` 提取） |
| **Resume** | 支持 `--resume` | **不支持** |
| **CLI 参数** | `--print --verbose --output-format stream-json` | `--format json` |
| **环境变量** | `ANTHROPIC_*` | `OPENAI_*`, `OPENCODE_*` |
| **MCP 支持** | `--mcp-config` | `--mcp-config`（需验证） |
| **工具参数** | `--allowed-tools` | `--allowed-tools`（多值） |

---

## 13. 实现优先级

> 详见 [[Worker-Common-Protocol]] §11（背压、终止、环境变量）

### P0（必须实现，v1.0 MVP）

| 项目 | 说明 |
|------|------|
| NDJSON 编码/解码 | AEP v1 格式 |
| `step_start` session_id 提取 | 自动提取并缓存 |
| 工具调用映射 | `tool_use` → `tool_call` |
| 消息映射 | `message` → AEP 事件 |

### P1（重要，v1.0 完整支持）

| 项目 | 说明 |
|------|------|
| `--mcp-config` | MCP 服务器配置 |
| 流式增量映射 | `message.part.delta` → `message.delta` |
| 错误映射 | `error` → `error` |
| 步骤映射 | `step_start/step_end` → `message.start/step` |

### P2（增强，v1.1）

| 项目 | 说明 |
|------|------|
| `--bare` 模式 | 最小化配置 |
| `--add-dir` | 额外目录访问 |
| `--max-budget-usd` | 预算控制 |
| 系统消息处理 | `system` 类型 |

---

## 14. 源码关键路径

| 功能 | 源码路径 |
|------|---------|
| Worker 实现 | `internal/worker/opencodecli/worker.go` |

### 公共组件

> 详见 [[Worker-Common-Protocol]] §9。

| 功能 | 源码路径 |
|------|---------|
| BaseWorker | `internal/worker/base/worker.go` |
| Stdio Conn | `internal/worker/base/conn.go` |
| BuildEnv | `internal/worker/base/env.go` |
| Process Manager | `internal/worker/proc/manager.go` |
| AEP Codec | `pkg/aep/codec.go` |
| Events | `pkg/events/events.go` |
| Worker Interface | `internal/worker/worker.go` |
| Security Env | `internal/security/env.go` |

---

## 15. 实现状态跟踪

> 更新于 2026-04-04

### 15.1 汇总

| 类别 | ✅ | ⚠️ | ❌ | 总计 |
|------|---|---|---|------|
| **CLI 参数** | 1 | 14 | 0 | 15 |
| **环境变量白名单** | 11 | 0 | 0 | 11 |
| **事件映射** | 0 | 8 | 0 | 8 |
| **Capability 接口** | 5 | 0 | 0 | 5 |
| **安全集成** | 3 | 0 | 0 | 3 |

### 15.2 待完成项目

| 优先级 | 项目 | 说明 |
|--------|------|------|
| ⚠️ P0 | **NDJSON 编解码** | AEP v1 格式需验证 |
| ⚠️ P0 | **session_id 提取** | `step_start` 解析需验证 |
| ⚠️ P0 | **事件类型映射** | 需对照 OpenCode SDK 事件类型 |
| ⚠️ P1 | **MCP 配置** | `--mcp-config` 参数需验证 |
| ⚠️ P2 | **扩展参数** | `--bare`, `--add-dir` 等需验证 |

---

## 16. 架构亮点

> 详见 [[Worker-Common-Protocol]] §11。

### CLI 特有亮点

- ✅ **自动 session_id 提取**：从 `step_start` 事件解析
- ✅ **多值 `--allowed-tools`**：每个工具单独参数
- ❌ **不支持 Resume**：CLI 模式无持久化

### 公共亮点

- ✅ **三层协议分层**：`scanner` → `aep.DecodeLine` → `Conn.TrySend`
- ✅ **背压处理**：256 buffer，delta 静默丢弃
- ✅ **分层终止**：SIGTERM → 5s → SIGKILL
- ✅ **`HOTPLEX_*` 变量注入**：会话追踪和类型标识
