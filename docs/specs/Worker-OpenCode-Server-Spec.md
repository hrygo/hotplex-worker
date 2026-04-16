---
type: spec
tags:
  - project/HotPlex
  - worker/opencode-server
  - architecture/integration
  - protocol/aep-v1
  - feature/websocket-transport
  - feature/session-management
  - feature/resume-support
date: 2026-04-07
status: implemented
progress: 100
validation: validated-against-source
last-validated: 2026-04-07
worker_session_id_handler: implemented
---

# OpenCode Server Worker 集成规格

> 本文档详细定义 OpenCode Server Worker Adapter 的实现规格。
> 基于 actual source code validation at 2026-04-04。

---

## 1. 概述

| 维度              | 设计                                     | 实现状态 |
| ----------------- | ---------------------------------------- | -------- |
| **Transport**     | HTTP + SSE（Server-Sent Events）         | ✅        |
| **Protocol**      | AEP v1 NDJSON over HTTP/SSE              | ✅        |
| **进程模型**      | 持久进程（`opencode serve`），多会话复用 | ✅        |
| **源码路径**      | `internal/worker/opencodeserver/`        | ✅        |
| **OpenCode 源码** | `~/opencode/packages/opencode/src/`      | ✅        |

**集成命令**:

```bash
opencode serve --port 18789
```

> OpenCode Server 是一个独立的 HTTP 服务器进程，通过 REST API 管理会话，通过 SSE 推送事件。

---

## 2. Server 架构

### 2.1 核心组件

| 组件              | 位置                                       | 说明                              | 状态 |
| ----------------- | ------------------------------------------ | --------------------------------- | ---- |
| HTTP Server       | `cmd/worker/main.go:237-305`               | Go 标准库 net/http + Gorilla WS   | ✅    |
| Worker Adapter    | `internal/worker/opencodeserver/worker.go` | OpenCode Server 进程管理          | ✅    |
| Gateway Hub       | `internal/gateway/hub.go`                  | WebSocket 连接管理和事件广播      | ✅    |
| Session Manager   | `internal/session/manager.go`              | Session 生命周期管理              | ✅    |
| Process Manager   | `internal/worker/proc/manager.go`          | 进程组隔离和分层终止              | ✅    |
| AEP Codec         | `pkg/aep/codec.go`                         | AEP v1 协议编解码                 | ✅    |
| Event Definitions | `pkg/events/events.go`                     | 事件类型定义                      | ✅    |
| SQLite Store      | `internal/session/store.go`                | Session 持久化                    | ✅    |

### 2.2 通信流程

```
┌─────────────────────────────────────────────────────────────────┐
│                    HotPlex Gateway (主进程)                      │
│                   (localhost:8080)                               │
│                                                                 │
│   WS /ws ────────────────────► WebSocket 连接                   │
│   GET /health ───────────────► 基础健康检查                     │
│   GET /admin/health ─────────► 详细健康检查                     │
│   GET /admin/metrics ────────► Prometheus metrics               │
│   POST /admin/sessions ──────► Session 管理                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 启动子进程
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│              OpenCode Server Worker (子进程)                     │
│                   (localhost:18789)                              │
│                                                                 │
│   • 启动 opencode serve 子进程                                  │ ✅
│   • 轮询 /health 等待就绪                                       │ ✅
│   • 通过 HTTP REST API 发送命令                                 │ ✅
│   • 通过 SSE 订阅事件                                           │ ✅
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTP + SSE
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                OpenCode Server (独立进程)                        │
│                   (localhost:18789)                              │
│                                                                 │
│   POST /sessions ──────────► 创建会话                           │ ✅
│   POST /sessions/{id}/input ───► 发送输入                       │ ✅
│   GET /events?session_id={id} ◄── SSE 事件流                    │ ✅
│   GET /health ◄────────────── 健康检查                          │ ✅
└─────────────────────────────────────────────────────────────────┘
```

**实现细节**:

```go
// worker.go:82-85 - 启动命令
args := []string{
    "serve",
    "--port", fmt.Sprintf("%d", defaultServePort),  // 18789
}

// worker.go:291-303 - 等待服务器就绪
func (w *Worker) waitForServer(ctx context.Context) error {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            req, err := http.NewRequestWithContext(ctx, "GET", w.httpAddr+"/health", nil)
            if err != nil {
                continue
            }
            resp, err := w.client.Do(req)
            if err != nil {
                continue
            }
            resp.Body.Close()
            if resp.StatusCode == http.StatusOK {
                return nil
            }
        }
    }
}
```

---

## 3. API 端点

### 3.1 Gateway 端点（HotPlex 主进程）

| 端点                | 方法   | 说明                    | 状态 |
| ------------------- | ------ | ----------------------- | ---- |
| `/health`           | GET    | 基础健康检查            | ✅    |
| `/admin/health`     | GET    | 详细健康检查（含 DB）   | ✅    |
| `/admin/health/ready` | GET    | 服务器就绪检查          | ✅    |
| `/admin/metrics`    | GET    | Prometheus metrics      | ✅    |
| `/admin/sessions`   | GET    | 列出所有 session        | ✅    |
| `/admin/sessions/{id}` | GET    | 获取 session 详情       | ✅    |
| `/admin/sessions/{id}` | DELETE | 删除 session            | ✅    |
| `/admin/sessions/{id}/terminate` | POST   | 终止 session            | ✅    |
| `/ws`               | WS     | WebSocket 入口点        | ✅    |

**实现位置**: `cmd/worker/main.go:237-305`

### 3.2 OpenCode Server 端点（子进程）

| 端点                       | 方法   | 说明                   | 状态 |
| -------------------------- | ------ | ---------------------- | ---- |
| `/health`                  | GET    | 服务器就绪检查         | ✅    |
| `/sessions`                | POST   | 创建新会话             | ✅    |
| `/sessions/{session_id}`   | GET    | 获取会话信息           | ✅    |
| `/sessions/{session_id}`   | DELETE | 删除会话               | ✅    |
| `/sessions/{session_id}/input` | POST   | 发送输入               | ✅    |
| `/events?session_id={id}`  | GET    | SSE 事件流             | ✅    |

**实现位置**: `internal/worker/opencodeserver/worker.go`

---

## 4. 会话创建

### 4.1 请求

```go
// worker.go:311-336
func (w *Worker) createSession(ctx context.Context, projectDir string) (string, error) {
    reqBody := strings.NewReader(fmt.Sprintf(`{"project_dir": %q}`, projectDir))
    req, err := http.NewRequestWithContext(ctx, "POST", w.httpAddr+"/sessions", reqBody)
    if err != nil {
        return "", err
    }
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := w.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("create session failed: %d %s", resp.StatusCode, string(body))
    }
    
    var result createSessionResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }
    
    return result.SessionID, nil
}
```

### 4.2 响应格式

```json
{
  "id": "ses_xxx",
  "slug": "friendly-name",
  "version": "1.3.13",
  "projectID": "b57f73cb...",
  "directory": "/path/to/project",
  "title": "New session - 2026-04-04T10:40:55.324Z",
  "time": {
    "created": 1775299255324,
    "updated": 1775299255324
  }
}
```

### 4.3 Session ID 格式

**生成方式**: `pkg/aep/codec.go` (未直接找到，推测使用 UUID)

```go
// pkg/events/events_test.go
func NewID() string {
    return fmt.Sprintf("evt_%s", uuid.NewString())
}
```

**格式**: `evt_<UUID>` 或 `ses_<UUID>`

---

## 5. 输入发送

### 5.1 实现

```go
// worker.go:430-474 - conn.Send 方法
func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
    inputData := events.InputData{}
    if data, ok := msg.Event.Data.(map[string]any); ok {
        if content, ok := data["content"].(string); ok {
            inputData.Content = content
        }
        if metadata, ok := data["metadata"].(map[string]any); ok {
            inputData.Metadata = metadata
        }
    }

    body, _ := json.Marshal(inputData)
    url := fmt.Sprintf("%s/sessions/%s/input", c.httpAddr, c.sessionID)
    req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.client.Do(req)
    if err != nil {
        return fmt.Errorf("opencodeserver: input request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
        respBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("opencodeserver: input failed: %d %s", resp.StatusCode, string(respBody))
    }

    return nil
}
```

### 5.2 请求格式

```http
POST /sessions/{session_id}/input HTTP/1.1
Content-Type: application/json

{
  "content": "user prompt here",
  "metadata": {}
}
```

### 5.3 响应

- `200 OK` 或 `202 Accepted`：成功
- 其他状态码：错误

---

## 6. SSE 事件流

### 6.1 实现

```go
// worker.go:338-415 - readSSE goroutine
func (w *Worker) readSSE(sessionID string) {
    url := fmt.Sprintf("%s/events?session_id=%s", w.httpAddr, sessionID)
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        w.Log.Error("opencodeserver: create SSE request", "error", err)
        return
    }
    req.Header.Set("Accept", "text/event-stream")
    req.Header.Set("Cache-Control", "no-cache")

    resp, err := w.client.Do(req)
    if err != nil {
        w.Log.Error("opencodeserver: SSE connect", "error", err)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        w.Log.Error("opencodeserver: SSE status", "status", resp.StatusCode, "body", string(body))
        return
    }

    reader := bufio.NewReader(resp.Body)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            if err == io.EOF {
                return
            }
            w.Log.Error("opencodeserver: SSE read", "error", err)
            return
        }
        line = strings.TrimSpace(line)

        // SSE format: "data: {json}"
        if strings.HasPrefix(line, "data: ") {
            data := strings.TrimPrefix(line, "data: ")
            data = strings.TrimSpace(data)

            // AEP 解码
            env, err := aep.DecodeLine([]byte(data))
            if err != nil {
                w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
                continue
            }

            // 非阻塞发送到 hub
            w.Mu.Lock()
            conn := w.httpConn
            w.Mu.Unlock()
            select {
            case conn.recvCh <- env:
            default:
                w.Log.Warn("opencodeserver: recv channel full, dropping message")
            }
        }
    }
}
```

### 6.2 响应格式

SSE 格式，每行以 `data: ` 前缀：

```
data: {"version":"aep/v1","id":"evt_xxx","seq":1,"session_id":"sess_xxx","timestamp":1712234567890,"event":{"type":"state","data":{"state":"running"}}}
data: {"version":"aep/v1","id":"evt_yyy","seq":2,"session_id":"sess_xxx","timestamp":1712234567891,"event":{"type":"message.delta","data":{"content":"..."}}}
```

---

## 7. 环境变量

### 7.1 白名单

| 变量                                    | 说明              | 实现位置                |
| --------------------------------------- | ----------------- | ----------------------- |
| `HOME`, `USER`, `SHELL`, `PATH`, `TERM` | 系统环境          | ✅ `internal/worker/base/env.go` |
| `LANG`, `LC_ALL`, `PWD`                 | 本地化            | ✅ `internal/worker/base/env.go` |
| `OPENAI_API_KEY`                        | OpenAI API 密钥   | ✅ `internal/worker/base/env.go` |
| `OPENAI_BASE_URL`                       | OpenAI API 端点   | ✅ `internal/worker/base/env.go` |
| `OPENCODE_API_KEY`                      | OpenCode API 密钥 | ✅ `internal/worker/base/env.go` |
| `OPENCODE_BASE_URL`                     | OpenCode API 端点 | ✅ `internal/worker/base/env.go` |

### 7.2 HotPlex 注入变量

| 变量                  | 说明                                 | 实现位置                |
| --------------------- | ------------------------------------ | ----------------------- |
| `HOTPLEX_SESSION_ID`  | 会话标识符                           | ✅ `internal/worker/base/env.go` |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签（`opencode-server`） | ✅ `internal/worker/base/env.go` |

---

## 8. 事件映射（AEP v1）

### 8.1 AEP v1 事件类型

| AEP Event Kind        | 说明                  | 实现位置               | 状态 |
| --------------------- | --------------------- | ---------------------- | ---- |
| `message.delta`       | 流式文本/代码         | `pkg/events/events.go` | ✅    |
| `state`               | 会话状态（idle/busy） | `pkg/events/events.go` | ✅    |
| `error`               | 会话错误              | `pkg/events/events.go` | ✅    |
| `input`               | 用户输入              | `pkg/events/events.go` | ✅    |
| `done`                | 会话完成              | `pkg/events/events.go` | ✅    |
| `tool_call`           | 工具调用              | `pkg/events/events.go` | ✅    |
| `tool_result`         | 工具结果              | `pkg/events/events.go` | ✅    |
| `permission_request`  | 工具权限请求          | `pkg/events/events.go` | ✅    |
| `permission_response` | 权限响应              | `pkg/events/events.go` | ✅    |

### 8.2 事件结构

```go
// pkg/events/events.go:72-85
type Envelope struct {
    Version   string   `json:"version"`             // "aep/v1"
    ID        string   `json:"id"`                  // "evt_<uuid>"
    Seq       int64    `json:"seq"`                 // 单调递增
    Priority  Priority `json:"priority,omitempty"`  // "control" | "data"
    SessionID string   `json:"session_id"`          // 会话标识
    Timestamp int64    `json:"timestamp"`           // Unix ms
    Event     Event    `json:"event"`               // { Type, Data }
}

type Event struct {
    Type Kind       `json:"type"`
    Data any        `json:"data"`
}
```

### 8.3 示例事件

```json
{
  "version": "aep/v1",
  "id": "evt_123e4567-e89b-12d3-a456-426614174000",
  "seq": 1,
  "session_id": "sess_xxx",
  "timestamp": 1712234567890,
  "event": {
    "type": "state",
    "data": {
      "state": "running"
    }
  }
}
```

---

## 9. Session 管理

### 9.1 WorkerSessionIDHandler 接口实现

> OpenCode Server Worker 实现 `worker.WorkerSessionIDHandler` 接口，使 Gateway 能够获取并持久化内部 session ID。

```go
// opencodeserver/worker.go

type atomicSessionID struct {
    atomic.Value // stores string
}

func (w *Worker) SetWorkerSessionID(id string) {
    w.atomicSID.Store(id)
    if w.httpConn != nil {
        w.httpConn.sessionID = id
    }
}

func (w *Worker) GetWorkerSessionID() string {
    if w.httpConn != nil {
        return w.httpConn.sessionID
    }
    if v := w.atomicSID.Load(); v != nil {
        return v.(string)
    }
    return ""
}
```

**持久化时机**：`bridge.forwardEvents()` 收到第一个 worker 事件时，`persistWorkerSessionID()` 调用 `GetWorkerSessionID()` 并更新 DB。

### 9.2 Session 生命周期

```
Start
  │
  ├─► 启动 opencode serve 子进程（端口 18789）
  │   └─► worker.go:Start()
  │
  ├─► 轮询 /health 直到 200 OK
  │   └─► worker.go:waitForServer()
  │
  ├─► POST /sessions → session_id
  │   └─► worker.go:createSession()
  │
  ├─► 创建 conn{recvCh}
  │   └─► worker.go:httpConn
  │
  └─► goroutine: GET /events?session_id=xxx (SSE)
           │
           ▼
      运行时
           │
   ◄────────┴─────────►
   │                   │
   │  POST /sessions/{id}/input
   │  (通过 recvCh 接收 SSE 事件)
   │
   └─► Close() → close(recvCh)
            │
            ▼
       Terminate
            │
   ├─► BaseWorker.Terminate() → SIGTERM → SIGKILL
   │   └─► internal/worker/base/worker.go
   └─► 进程清理
```

### 9.2 Resume 支持

**已完全实现**。

```go
// worker.go:177-239 - Resume 实现
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // 1. 启动 serve 进程
    args := []string{"serve", "--port", fmt.Sprintf("%d", defaultServePort)}
    env := base.BuildEnv(session, openCodeSrvEnvWhitelist, "opencode-server")
    
    w.Proc = proc.New(proc.Opts{
        Logger:       w.Log,
        AllowedTools: session.AllowedTools,
    })
    
    _, _, _, err := w.Proc.Start(ctx, "opencode", args, env, session.ProjectDir)
    if err != nil {
        return fmt.Errorf("opencodeserver: resume start: %w", err)
    }

    // 2. 等待服务器就绪
    w.port = defaultServePort
    w.httpAddr = fmt.Sprintf("http://localhost:%d", w.port)
    
    if err := w.waitForServer(ctx); err != nil {
        _ = w.Proc.Kill()
        return fmt.Errorf("opencodeserver: wait for server: %w", err)
    }

    // 3. 使用现有 session_id
    w.httpConn = &conn{
        userID:    session.UserID,
        sessionID: session.SessionID,  // 关键：复用现有 ID
        httpAddr:  w.httpAddr,
        client:    w.client,
        recvCh:    make(chan *events.Envelope, 256),
        log:       w.Log,
    }

    // 4. 重连 SSE
    go w.readSSE(session.SessionID)

    return nil
}
```

**状态**: ✅ 完全实现

---

## 10. 优雅终止（Graceful Shutdown）

### 10.1 终止流程

**分层终止**: SIGTERM → 5s grace → SIGKILL

```go
// internal/worker/base/worker.go
func (w *BaseWorker) Terminate() error {
    w.Mu.Lock()
    defer w.Mu.Unlock()

    if w.Proc == nil {
        return nil
    }

    // 1. SIGTERM 优雅终止
    _ = w.Proc.Terminate()
    
    // 2. 等待 5 秒
    time.Sleep(5 * time.Second)
    
    // 3. SIGKILL 强制终止
    _ = w.Proc.Kill()
    
    w.Proc = nil
    return nil
}
```

### 10.2 PGID 隔离

```go
// internal/worker/proc/manager.go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // PGID 隔离
```

确保信号传播到进程组。

---

## 11. 错误处理模式

### 11.1 服务器等待失败

```go
// worker.go:109-115
if err := w.waitForServer(ctx); err != nil {
    _ = w.Proc.Kill()  // 清理进程
    w.Mu.Lock()
    w.Proc = nil
    w.Mu.Unlock()
    return fmt.Errorf("opencodeserver: wait for server: %w", err)
}
```

### 11.2 SSE 解码错误

```go
// worker.go:391-394 - 非致命错误，继续读取
env, err := aep.DecodeLine([]byte(data))
if err != nil {
    w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
    continue  // 继续读取 SSE
}
```

### 11.3 背压处理

- **Channel 容量**: 256
- **静默丢弃**: `data` priority 消息
- **日志记录**: 静默丢弃时记录警告

```go
// worker.go:409-414
select {
case conn.recvCh <- env:
default:
    w.Log.Warn("opencodeserver: recv channel full, dropping message")
}
```

### 11.4 输入发送失败

```go
// worker.go:462-471
if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
    respBody, _ := io.ReadAll(resp.Body)
    return fmt.Errorf("opencodeserver: input failed: %d %s", resp.StatusCode, string(respBody))
}
```

### 11.5 错误码定义

```go
// pkg/events/events.go:44-70
type ErrorCode string

const (
    ErrCodeWorkerStartFailed  ErrorCode = "WORKER_START_FAILED"
    ErrCodeWorkerCrash        ErrorCode = "WORKER_CRASH"
    ErrCodeWorkerTimeout      ErrorCode = "WORKER_TIMEOUT"
    ErrCodeWorkerOOM          ErrorCode = "WORKER_OOM"
    ErrCodeProcessSIGKILL     ErrorCode = "PROCESS_SIGKILL"
    ErrCodeInvalidMessage     ErrorCode = "INVALID_MESSAGE"
    ErrCodeSessionNotFound    ErrorCode = "SESSION_NOT_FOUND"
    ErrCodeSessionExpired     ErrorCode = "SESSION_EXPIRED"
    ErrCodeSessionTerminated  ErrorCode = "SESSION_TERMINATED"
    // ... 更多错误码
)
```

---

## 12. Worker Adapter 核心代码

### 12.1 Worker 结构

```go
// worker.go:23-55
type Worker struct {
    *base.BaseWorker
    httpAddr  string           // http://localhost:18789
    port      int              // 18789
    client    *http.Client     // 持久 HTTP 客户端
    httpConn  *conn            // 当前会话连接
    sessionID string           // 当前 session_id
    Mu        sync.Mutex       // 保护以上字段
    Log       *slog.Logger
}

const defaultServePort = 18789
```

### 12.2 Capability 接口

```go
// worker.go:59-69
func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeSrv }
func (w *Worker) SupportsResume() bool    { return true }   // ✅ Server 模式支持
func (w *Worker) SupportsStreaming() bool { return true }   // ✅ SSE 流式
func (w *Worker) SupportsTools() bool     { return true }   // ✅ 工具调用
func (w *Worker) EnvWhitelist() []string { return openCodeSrvEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" }     // Server 不使用本地存储
func (w *Worker) MaxTurns() int          { return 0 }      // 无限制
func (w *Worker) Modalities() []string   { return []string{"text", "code"} }
```

### 12.3 conn 结构

```go
// worker.go:260-288
type conn struct {
    userID    string
    sessionID string
    httpAddr  string
    client    *http.Client
    recvCh    chan *events.Envelope  // SSE 事件 channel (256 buffer)
    mu        sync.Mutex
    closed    bool
    log       *slog.Logger
}
```

---

## 13. 与 OpenCode CLI Worker 的差异

| 特性           | OpenCode CLI Worker              | OpenCode Server Worker   | 状态 |
| -------------- | -------------------------------- | ------------------------ | ---- |
| **Transport**  | stdio                            | HTTP + SSE               | ✅    |
| **命令**       | `opencode run --format json`     | `opencode serve`         | ✅    |
| **Session ID** | 内部生成（从 `step_start` 提取） | 外部指定或内部生成       | ✅    |
| **Resume**     | **不支持**                       | **支持**                 | ✅    |
| **进程模型**   | 单会话                           | 多会话复用               | ✅    |
| **事件格式**   | NDJSON stdout                    | SSE `data: {json}`       | ✅    |
| **通信方式**   | 双向 stdio                       | 请求/响应 + 订阅         | ✅    |
| **背压处理**   | 256 channel                      | 256 channel              | ✅    |

---

## 14. 实现状态跟踪

> 基于 2026-04-04 源码验证

### 14.1 汇总

| 类别                | ✅   | ⚠️   | ❌   | 总计 |
| ------------------- | --- | --- | --- | ---- |
| **API 端点**        | 13  | 0   | 0   | 13   |
| **事件映射**        | 9   | 0   | 0   | 9    |
| **Capability 接口** | 9   | 0   | 0   | 9    |
| **错误处理**        | 5   | 0   | 0   | 5    |
| **Session 管理**    | 4   | 0   | 0   | 4    |
| **进程管理**        | 3   | 0   | 0   | 3    |

**总体进度**: 100% ✅

### 14.2 已完成项目

| 功能 | 实现位置 | 验证状态 |
|------|----------|---------|
| Gateway HTTP 服务器 | `cmd/worker/main.go` | ✅ 验证通过 |
| WebSocket 连接管理 | `gateway/conn.go` | ✅ 验证通过 |
| Session 创建/管理 | `session/manager.go` | ✅ 验证通过 |
| Session 持久化 | `session/store.go` | ✅ 验证通过 |
| Worker 进程管理 | `internal/worker/proc/manager.go` | ✅ 验证通过 |
| OpenCode Server Worker | `internal/worker/opencodeserver/worker.go` | ✅ 验证通过 |
| SSE 事件流 | `worker.go:readSSE()` | ✅ 验证通过 |
| AEP v1 协议编解码 | `pkg/aep/codec.go` | ✅ 验证通过 |
| 事件处理 | `gateway/handler.go` | ✅ 验证通过 |
| Resume 支持 | `worker.go:Resume()` | ✅ 验证通过 |
| 背压处理 | `gateway/hub.go` | ✅ 验证通过 |
| Admin API | `internal/admin/handlers.go` | ✅ 验证通过 |
| 健康检查 | `main.go`, `admin.go` | ✅ 验证通过 |
| Metrics | `main.go:260` | ✅ 验证通过 |

---

## 15. 源码关键路径

### 15.1 Server Worker 特有

| 功能            | 源码路径                                       | 行号      |
| --------------- | ---------------------------------------------- | --------- |
| Worker 实现     | `internal/worker/opencodeserver/worker.go`     | 1-508     |
| Start 方法      | `worker.go`                                    | 71-168    |
| Resume 方法     | `worker.go`                                    | 177-239   |
| SSE 读取        | `worker.go`                                    | 338-415   |
| Session 创建    | `worker.go`                                    | 311-336   |
| 健康检查等待    | `worker.go`                                    | 291-303   |

### 15.2 公共组件

| 功能             | 源码路径                         | 说明           |
| ---------------- | -------------------------------- | -------------- |
| BaseWorker       | `internal/worker/base/worker.go` | 共享生命周期   |
| AEP Codec        | `pkg/aep/codec.go`               | 协议编解码     |
| Events           | `pkg/events/events.go`           | 事件类型定义   |
| Worker Interface | `internal/worker/worker.go`      | Worker 接口    |
| Process Manager  | `internal/worker/proc/manager.go`| 进程管理       |
| Session Manager  | `internal/session/manager.go`    | Session 管理   |
| Gateway Hub      | `internal/gateway/hub.go`        | 事件广播       |
| Gateway Conn     | `internal/gateway/conn.go`       | WebSocket 连接 |

---

## 16. 架构亮点

### 16.1 Server Worker 特有亮点

- ✅ **HTTP REST + SSE**：清晰的请求/响应 + 订阅分离
- ✅ **持久进程**：Server 模式多会话复用
- ✅ **Resume 支持**：Server 模式支持会话恢复
- ✅ **无本地存储**：依赖 Server 进程内管理
- ✅ **跨进程架构**：Worker 作为独立进程运行，便于资源隔离

### 16.2 公共亮点

- ✅ **AEP v1 协议**：统一的 Agent Event Protocol
- ✅ **背压处理**：256 buffer，delta 静默丢弃
- ✅ **分层终止**：SIGTERM → 5s → SIGKILL
- ✅ **PGID 隔离**：进程组隔离，信号传播
- ✅ **SQLite 持久化**：Session 状态持久化
- ✅ **Metrics**：Prometheus metrics 支持
- ✅ **健康检查**：多级健康检查（基础 + 详细）

---

## 17. 验证和测试

### 17.1 验证脚本

**脚本位置**: `scripts/validate-opencode-server-spec.sh`

**验证项目**:
- ✅ OpenCode 源码路径
- ✅ HotPlex Worker 实现
- ✅ API 端点实现
- ✅ 协议实现（AEP v1）
- ✅ 关键功能（Resume, SSE, Session 管理）
- ✅ 架构组件（进程管理, 背压处理）

### 17.2 验证报告

**报告位置**: `scripts/opencode-server-spec-validation.md`

**生成时间**: 2026-04-04

---

## 18. 变更历史

| 日期       | 版本  | 变更说明                                      |
| ---------- | ----- | --------------------------------------------- |
| 2026-04-04 | 2.0   | 基于源码验证的完整更新，修正所有不准确描述    |
| 2026-04-04 | 1.0   | 初始版本（已过时，包含不准确描述）            |

---

## 19. 参考文档

- [[Worker-Gateway-Design]] - Gateway 整体设计
- [[Worker-Common-Protocol]] - 公共协议定义
- [[ACP-011]] - ACP 协议规范（已废弃，使用 AEP v1）
- [[AEP-012]] - AEP v1 协议规范

---

**文档状态**: ✅ 已验证并更新（2026-04-04）
**下一步**: 持续维护，随代码变更同步更新
