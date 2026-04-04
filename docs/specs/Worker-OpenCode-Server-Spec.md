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
date: 2026-04-04
status: needs-implementation
progress: 0
---

# OpenCode Server Worker 集成规格

> 本文档详细定义 OpenCode Server Worker Adapter 与 OpenCode Server 的集成规格。
> 高阶设计见 [[Worker-Gateway-Design]] §8.3。

---

## 1. 概述

| 维度              | 设计                                     |
| ----------------- | ---------------------------------------- |
| **Transport**     | HTTP + SSE（Server-Sent Events）         |
| **Protocol**      | AEP v1 NDJSON over HTTP/SSE              |
| **进程模型**      | 持久进程（`opencode serve`），多会话复用 |
| **源码路径**      | `internal/worker/opencodeserver/`        |
| **OpenCode 源码** | `~/opencode/packages/opencode/src/`      |

**集成命令**：

```bash
opencode serve --port 18789
```

> OpenCode Server 是一个基于 Hono 的 HTTP 服务器，通过 REST API 管理会话，通过 SSE 推送事件。

---

## 2. Server 架构

### 2.1 核心组件

| 组件         | 位置                                             | 说明                          |
| ------------ | ------------------------------------------------ | ----------------------------- |
| HTTP Server  | `packages/opencode/src/server/server.ts`         | Hono 应用，含路由、CORS、压缩 |
| Session API  | `packages/opencode/src/server/routes/session.ts` | 会话 CRUD                     |
| Event Stream | `packages/opencode/src/server/routes/event.ts`   | SSE 事件推送                  |
| Instance     | `packages/opencode/src/server/instance.ts`       | 实例管理                      |
| MCP Config   | `packages/opencode/src/server/routes/mcp.ts`     | MCP 服务器配置                |

### 2.2 通信流程

```
┌─────────────────────────────────────────────────────────────────┐
│                    opencode serve进程                         │
│                   (localhost:18789)                             │
│                                                                 │
│   HTTP POST /session ────────────► 创建会话                      │
│   HTTP POST /session/{id}/input ──► 发送输入                    │
│   HTTP GET /global/event?session_id={id} ◄── SSE 事件流         │
│   HTTP GET /global/health ◄───────── 健康检查                    │
└─────────────────────────────────────────────────────────────────┘
                              ▲
                              │ HTTP + SSE
                              │
┌─────────────────────────────────────────────────────────────────┐
│                 OpenCode Server Worker                          │
│              (internal/worker/opencodeserver/)                  │
│                                                                 │
│   • 启动 opencode serve 子进程                                  │
│   • 轮询 /global/health 等待就绪                                │
│   • 通过 HTTP REST API 发送命令                                 │
│   • 通过 SSE 订阅事件                                           │
└─────────────────────────────────────────────────────────────────┘
```
┌─────────────────────────────────────────────────────────────────┐
│                    opencode serve 进程                         │
│                   (localhost:18789)                             │
│                                                                 │
│   HTTP POST /sessions ──────────► 创建会话                      │
│   HTTP POST /sessions/{id}/input ───► 发送输入                  │
│   HTTP GET /events?session_id={id} ◄── SSE 事件流               │
│   HTTP GET /health ◄────────────── 健康检查                     │
└─────────────────────────────────────────────────────────────────┘
                              ▲
                              │ HTTP + SSE
                              │
┌─────────────────────────────────────────────────────────────────┐
│                 OpenCode Server Worker                          │
│              (internal/worker/opencodeserver/)                  │
│                                                                 │
│   • 启动 opencode serve 子进程                                  │
│   • 轮询 /health 等待就绪                                       │
│   • 通过 HTTP REST API 发送命令                                 │
│   • 通过 SSE 订阅事件                                           │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. API 端点

> ⚠️ **实际端点与文档不符**：OpenCode Server 使用 ACP 协议，端点为 `/global/health`、`/session` 等，而非文档中早期设计的 `/health`、`/sessions`。

### 3.1 健康检查

| 端点             | 方法 | 说明                       |
| ---------------- | ---- | -------------------------- |
| `/global/health` | GET  | 服务器就绪检查（ACP 协议） |

**响应**：
```json
{
  "healthy": true,
  "version": "1.3.13"
}
```

### 3.2 会话管理

| 端点                           | 方法   | 说明                   |
| ------------------------------ | ------ | ---------------------- |
| `/session`                     | POST   | 创建新会话（ACP 协议） |
| `/session/{session_id}`        | GET    | 获取会话信息           |
| `/session/{session_id}`        | DELETE | 删除会话               |
| `/session/{session_id}/input`  | POST   | 发送输入               |
| `/session/{session_id}/status` | GET    | 获取会话状态           |
| `/session/{session_id}/fork`   | POST   | Fork 会话              |
| `/session/{session_id}/abort`  | POST   | 中止会话               |
| `/session/{session_id}/share`  | POST   | 分享会话               |
| `/session/{session_id}/diff`   | GET    | 获取会话差异           |

### 3.3 事件流

| 端点                 | 方法 | 说明                                          |
| -------------------- | ---- | --------------------------------------------- |
| `/global/event`      | GET  | SSE 事件流（`session_id` 查询参数，ACP 协议） |
| `/global/sync-event` | GET  | 同步事件流                                    |

---

## 4. 会话创建

### 4.1 请求

```http
POST /session HTTP/1.1
Content-Type: application/json

{
  "project_dir": "/path/to/project"
}
```

### 4.2 响应

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

### 4.3 实现

```go
// worker.go:311-336
func (w *Worker) createSession(ctx context.Context, projectDir string) (string, error) {
    reqBody := strings.NewReader(fmt.Sprintf(`{"project_dir": %q}`, projectDir))
    req, err := http.NewRequestWithContext(ctx, "POST", w.httpAddr+"/session", reqBody)
    req.Header.Set("Content-Type", "application/json")

    resp, err := w.client.Do(req)
    if err != nil {
        return "", fmt.Errorf("opencodeserver: create session: %w", err)
    }
    defer resp.Body.Close()

    var result createSessionResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", fmt.Errorf("opencodeserver: decode session response: %w", err)
    }

    return result.SessionID, nil
}
```

---

## 5. 输入发送

### 5.1 请求

```http
POST /session/{session_id}/input HTTP/1.1
Content-Type: application/json

{
  "content": "user prompt here",
  "metadata": {}
}
```

### 5.2 响应

- `200 OK` 或 `202 Accepted`：成功
- 其他状态码：错误

### 5.3 实现

```go
// worker.go:430-474 - conn.Send method
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

---

## 6. SSE 事件流

### 6.1 请求

```http
GET /global/event?session_id={session_id} HTTP/1.1
Accept: text/event-stream
Cache-Control: no-cache
```

### 6.2 响应格式

SSE 格式，每行以 `data: ` 前缀：

```
data: {"type":"step_start","data":{...}}
data: {"type":"text","data":{"role":"assistant","content":[...]}}
data: {"type":"step_finish","data":{...}}
```

### 6.3 实现

```go
// worker.go:338-415 - readSSE goroutine
func (w *Worker) readSSE(sessionID string) {
    url := fmt.Sprintf("%s/global/event?session_id=%s", w.httpAddr, sessionID)
    req, err := http.NewRequest("GET", url, nil)
    req.Header.Set("Accept", "text/event-stream")
    req.Header.Set("Cache-Control", "no-cache")

    resp, err := w.client.Do(req)
    if err != nil {
        w.Log.Error("opencodeserver: SSE connect", "error", err)
        return
    }

    reader := bufio.NewReader(resp.Body)
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            if err == io.EOF {
                break
            }
            w.Log.Error("opencodeserver: SSE read", "error", err)
            break
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

---

## 7. 环境变量

> 详见 [[Worker-Common-Protocol]] §6。

### 7.1 白名单

| 变量                                    | 说明              | Impl     |
| --------------------------------------- | ----------------- | -------- |
| `HOME`, `USER`, `SHELL`, `PATH`, `TERM` | 系统环境          | ✅ 白名单 |
| `LANG`, `LC_ALL`, `PWD`                 | 本地化            | ✅ 白名单 |
| `OPENAI_API_KEY`                        | OpenAI API 密钥   | ✅ 白名单 |
| `OPENAI_BASE_URL`                       | OpenAI API 端点   | ✅ 白名单 |
| `OPENCODE_API_KEY`                      | OpenCode API 密钥 | ✅ 白名单 |
| `OPENCODE_BASE_URL`                     | OpenCode API 端点 | ✅ 白名单 |

### 7.2 HotPlex 注入变量

| 变量                  | 说明                                 | Impl            |
| --------------------- | ------------------------------------ | --------------- |
| `HOTPLEX_SESSION_ID`  | 会话标识符                           | ✅ `base/env.go` |
| `HOTPLEX_WORKER_TYPE` | Worker 类型标签（`opencode-server`） | ✅ `base/env.go` |

---

## 8. 事件映射（OpenCode Server → AEP）

### 8.1 AEP v1 事件类型

| OpenCode Event         | AEP Event Kind       | 说明                  | Impl     |
| ---------------------- | -------------------- | --------------------- | -------- |
| `message.part.delta`   | `message.delta`      | 流式文本/代码         | ⚠️ 需实现 |
| `message.part.updated` | `message.delta`      | 部分更新              | ⚠️ 需实现 |
| `session.status`       | `state`              | 会话状态（idle/busy） | ⚠️ 需实现 |
| `permission.asked`     | `permission_request` | 工具权限请求          | ⚠️ 需实现 |
| `question.asked`       | —                    | 用户问题请求          | ⚠️ 需实现 |
| `session.error`        | `error`              | 会话错误              | ⚠️ 需实现 |
| `session.idle`         | `state`              | 会话空闲              | ⚠️ 需实现 |

### 8.2 SDK 事件类型

OpenCode Server 使用 AEP v1 协议，事件类型定义在 SDK 中：

```typescript
// SDK 事件类型 (packages/sdk/js/)
type EventType =
  | 'message.part.delta'
  | 'message.part.updated'
  | 'session.status'
  | 'permission.asked'
  | 'question.asked'
  | 'session.error'
  | 'session.idle'
```

---

## 9. Session 管理

### 9.1 Session 生命周期

```
Start
  │
  ├─► 启动 opencode serve 子进程（端口 18789）
  │
  ├─► 轮询 /global/health 直到 200 OK
  │
  ├─► POST /session → session_id
  │
  ├─► 创建 conn{recvCh}
  │
  └─► goroutine: GET /global/event?session_id=xxx (SSE)
           │
           ▼
      运行时
           │
   ◄────────┴─────────►
   │                   │
   │  POST /session/{id}/input
   │  (通过 recvCh 接收 SSE 事件)
   │
   └─► Close() → close(recvCh)
            │
            ▼
       Terminate
            │
   ├─► BaseWorker.Terminate() → SIGTERM → SIGKILL
   └─► 进程清理
```

### 9.2 Resume 支持

**支持**。Server Worker 支持恢复现有会话：

```go
// worker.go:177-239 - Resume 实现（需验证实际行号）
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // 1. 启动 serve 进程
    args := []string{"serve", "--port", fmt.Sprintf("%d", defaultServePort)}
    // ...

    // 2. 等待服务器就绪
    if err := w.waitForServer(ctx); err != nil {
        return err
    }

    // 3. 使用现有 session_id
    w.httpConn = &conn{
        userID:    session.UserID,
        sessionID: session.SessionID,  // 复用现有 ID
        httpAddr:  w.httpAddr,
        client:    w.client,
        recvCh:    make(chan *events.Envelope, 256),
    }

    // 4. 重连 SSE
    go w.readSSE(session.SessionID)

    return nil
}
```

---

## 10. 优雅终止（Graceful Shutdown）

> 详见 [[Worker-Common-Protocol]] §5。

- **终止流程**：SIGTERM → 5s grace → SIGKILL
- **实现**：`base.BaseWorker.Terminate()` 委托 `proc.Terminate()`
- **PGID 隔离**：`Setpgid: true` 确保信号传播到进程组

---

## 11. 错误处理模式

### 11.1 服务器等待失败

> 详见 [[Worker-Common-Protocol]] §8。

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

> 详见 [[Worker-Common-Protocol]] §4（背压策略）。

```go
// worker.go:391-394 - 非致命错误，继续读取
env, err := aep.DecodeLine([]byte(data))
if err != nil {
    w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
    continue  // 继续读取 SSE
}
```

### 11.3 背压处理

> 详见 [[Worker-Common-Protocol]] §4。

- **Channel 容量**：256
- **静默丢弃**：`data` priority 消息
- **日志记录**：静默丢弃时记录警告

### 11.4 输入发送失败

```go
// worker.go:462-471
if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
    respBody, _ := io.ReadAll(resp.Body)
    return fmt.Errorf("opencodeserver: input failed: %d %s", resp.StatusCode, string(respBody))
}
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
}

const defaultServePort = 18789
```

### 12.2 Capability 接口

> 详见 [[Worker-Common-Protocol]] §7。

```go
// worker.go:59-69
func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeSrv }
func (w *Worker) SupportsResume() bool    { return true }   // Server 模式支持
func (w *Worker) SupportsStreaming() bool { return true }   // SSE 流式
func (w *Worker) SupportsTools() bool     { return true }   // 工具调用
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
    recvCh    chan *events.Envelope  // SSE 事件 channel
    mu        sync.Mutex
    closed    bool
}
```

### 12.4 服务器就绪等待

```go
// worker.go:291-303
func (w *Worker) waitForServer(ctx context.Context) error {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            req, err := http.NewRequestWithContext(ctx, "GET", w.httpAddr+"/global/health", nil)
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

## 13. 与 OpenCode CLI Worker 的差异

| 特性           | OpenCode CLI Worker              | OpenCode Server Worker |
| -------------- | -------------------------------- | ---------------------- |
| **Transport**  | stdio                            | HTTP + SSE             |
| **命令**       | `opencode run --format json`     | `opencode serve`       |
| **Session ID** | 内部生成（从 `step_start` 提取） | 外部指定或内部生成     |
| **Resume**     | **不支持**                       | **支持**               |
| **进程模型**   | 单会话                           | 多会话复用             |
| **事件格式**   | NDJSON stdout                    | SSE `data: {json}`     |
| **通信方式**   | 双向 stdio                       | 请求/响应 + 订阅       |
| **背压处理**   | 256 channel                      | 256 channel            |

---

## 14. 实现优先级

> 详见 [[Worker-Common-Protocol]] §11（背压、终止、环境变量）

### P0（必须实现，v1.0 MVP）

| 项目                       | 说明                               |
| -------------------------- | ---------------------------------- |
| `opencode serve` 进程启动  | 端口 18789                         |
| `/global/health` 轮询      | 服务器就绪检测                     |
| `/session` POST            | 会话创建（ACP 协议）               |
| SSE 事件读取               | `GET /global/event?session_id=xxx` |
| AEP v1 编解码              | NDJSON over SSE                    |
| `/session/{id}/input` POST | 输入发送                           |

### P1（重要，v1.0 完整支持）

| 项目               | 说明                                   |
| ------------------ | -------------------------------------- |
| Resume 支持        | 复用现有 session_id                    |
| 事件类型映射       | `message.part.delta` → `message.delta` |
| `session.status`   | 会话状态映射                           |
| `permission.asked` | 权限请求映射                           |
| 错误处理           | `session.error` → `error`              |

### P2（增强，v1.1）

| 项目             | 说明                     |
| ---------------- | ------------------------ |
| `question.asked` | 用户问题请求             |
| MCP 配置         | 通过 API 配置 MCP 服务器 |
| 会话列表         | `GET /sessions`          |

---

## 15. 源码关键路径

### Server Worker 特有

| 功能            | 源码路径                                                    |
| --------------- | ----------------------------------------------------------- |
| Worker 实现     | `internal/worker/opencodeserver/worker.go`                  |
| OpenCode Server | `~/opencode/packages/opencode/src/server/`                  |
| Session Routes  | `~/opencode/packages/opencode/src/server/routes/session.ts` |
| Event Routes    | `~/opencode/packages/opencode/src/server/routes/event.ts`   |
| Serve Command   | `~/opencode/packages/opencode/src/cli/cmd/serve.ts`         |

### 公共组件

> 详见 [[Worker-Common-Protocol]] §9。

| 功能             | 源码路径                         |
| ---------------- | -------------------------------- |
| BaseWorker       | `internal/worker/base/worker.go` |
| AEP Codec        | `pkg/aep/codec.go`               |
| Events           | `pkg/events/events.go`           |
| Worker Interface | `internal/worker/worker.go`      |

---

## 16. 实现状态跟踪

> 更新于 2026-04-04

### 16.1 汇总

| 类别                | ✅   | ⚠️   | ❌   | 总计 |
| ------------------- | --- | --- | --- | ---- |
| **API 端点**        | 4   | 0   | 0   | 4    |
| **事件映射**        | 0   | 7   | 0   | 7    |
| **Capability 接口** | 6   | 0   | 0   | 6    |
| **错误处理**        | 4   | 0   | 0   | 4    |

### 16.2 待完成项目

| 优先级 | 项目                 | 说明                         |
| ------ | -------------------- | ---------------------------- |
| ⚠️ P0   | **事件类型映射**     | 需对照 OpenCode SDK AEP 事件 |
| ⚠️ P0   | **SSE → AEP 转换**   | SSE `data:` 前缀处理         |
| ⚠️ P1   | **Resume 完整实现**  | 需验证 session_id 复用       |
| ⚠️ P1   | **permission.asked** | 权限请求映射                 |
| ⚠️ P2   | **question.asked**   | 用户问题请求                 |

---

## 17. 架构亮点

> 详见 [[Worker-Common-Protocol]] §11。

### Server Worker 特有亮点

- ✅ **HTTP REST + SSE**：清晰的请求/响应 + 订阅分离
- ✅ **持久进程**：Server 模式多会话复用
- ✅ **Resume 支持**：Server 模式支持会话恢复
- ⚠️ **无本地存储**：依赖 Server 进程内管理

### 公共亮点

- ✅ **AEP v1 协议**：与 Claude Code Worker 协议统一
- ✅ **背压处理**：256 buffer，delta 静默丢弃
- ✅ **分层终止**：SIGTERM → 5s → SIGKILL
