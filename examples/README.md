# HotPlex Client SDKs

> 多语言客户端 SDK，通过 WebSocket (AEP v1) 与 HotPlex Gateway 交互

---

## 概览

本目录提供 4 种语言的官方客户端实现，遵循统一的架构模式：

```
┌─────────────────────────────────────────────────────┐
│  Application Code                                   │
├─────────────────────────────────────────────────────┤
│  High-Level Client API                              │  ← 业务层
│  - 会话管理                                          │
│  - 事件回调注册                                       │
│  - 自动重连                                          │
├─────────────────────────────────────────────────────┤
│  Transport Layer                                    │  ← 传输层
│  - WebSocket 连接生命周期                            │
│  - 消息队列/背压                                     │
│  - 心跳管理                                          │
├─────────────────────────────────────────────────────┤
│  Protocol Layer (AEP v1)                            │  ← 协议层
│  - NDJSON 编解码                                     │
│  - Envelope 构造                                     │
│  - 事件类型序列化                                    │
└─────────────────────────────────────────────────────┘
```

## 快速开始

### 1. 启动 Gateway

```bash
# 开发模式（零配置）
./hotplex

# 或使用配置文件
./hotplex -config configs/config-dev.yaml
```

Gateway 默认监听：
- **WebSocket**: `ws://localhost:8888`
- **Admin API**: `http://localhost:9999`

### 2. 选择客户端语言

| 语言 | 目录 | 状态 | 文档 |
|------|------|------|------|
| Python 3.10+ | [`python-client/`](python-client/) | ✅ 生产可用 | [README.md](python-client/README.md) |
| TypeScript/Node.js | [`typescript-client/`](typescript-client/) | ✅ 生产可用 | [README.md](typescript-client/README.md) |
| Go 1.26+ | [`../client/`](../client/) | ✅ 生产可用 | [README.md](../client/README.md) |
| Java 17+ | [`java-client/`](java-client/) | ✅ 生产可用 | [README.md](java-client/README.md) |

---

## Python 客户端

### 安装

```bash
cd examples/python-client
pip install -r requirements.txt
```

### 快速示例

```python
import asyncio
from hotplex_client import HotPlexClient, WorkerType

async def main():
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
        auth_token="your-api-key",
    ) as client:
        # 注册事件处理器
        @client.on_message_delta
        async def on_delta(data):
            print(data.content, end="", flush=True)

        @client.on_done
        async def on_done(data):
            print(f"\n✅ Done! Success: {data.success}")

        # 发送输入
        await client.send_input("Write a hello world in Python")

        # 等待完成
        await asyncio.sleep(10)

asyncio.run(main())
```

**完整文档**: [`python-client/README.md`](python-client/README.md)

---

## TypeScript 客户端

### 安装

```bash
cd examples/typescript-client
npm install
```

### 快速示例

```typescript
import { HotPlexClient, WorkerType } from "@hotplex/client";

const client = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.CLAUDE_CODE,
  authToken: "your-api-key",
});

// 事件监听
client.on("message_delta", (data) => {
  process.stdout.write(data.content);
});

client.on("done", (data) => {
  console.log(`\n✅ Done! Success: ${data.success}`);
});

// 连接并发送
await client.connect();
await client.sendInput({
  content: "Write a hello world in TypeScript",
});

// 清理
process.on("SIGINT", async () => {
  await client.close();
  process.exit(0);
});
```

**完整文档**: [`typescript-client/README.md`](typescript-client/README.md)

---

## Go 客户端

### 安装

```bash
cd client
go mod download
```

### 快速示例

```go
package main

import (
    "context"
    "fmt"
    "time"

    client "github.com/hrygo/hotplex/client"
)

func main() {
    ctx := context.Background()

    c, err := client.New(ctx,
        client.URL("ws://localhost:8888/ws"),
        client.WorkerType("claude_code"),
        client.APIKey("your-api-key"),
    )
    if err != nil {
        panic(err)
    }
    defer c.Close()

    // 事件处理
    for evt := range c.Events() {
        if data, ok := evt.AsMessageDeltaData(); ok {
            fmt.Print(data.Content)
        }
        if data, ok := evt.AsDoneData(); ok {
            fmt.Printf("\n✅ Done! Success: %v\n", data.Success)
        }
    }

    // 发送输入
    if err := c.SendInput(ctx, "Write a hello world in Go"); err != nil {
        panic(err)
    }

    // 等待完成
    time.Sleep(10 * time.Second)
}
```

**完整文档**: [`client/README.md`](../client/README.md)

---

## Java 客户端

### 安装 (Maven)

```xml
<dependency>
    <groupId>dev.hotplex</groupId>
    <artifactId>hotplex-client</artifactId>
    <version>1.0.0-SNAPSHOT</version>
</dependency>
```

### 快速示例

```java
import dev.hotplex.client.*;
import dev.hotplex.protocol.*;

public class Main {
    public static void main(String[] args) throws Exception {
        HotPlexClient client = HotPlexClient.builder()
            .url("ws://localhost:8888")
            .workerType(WorkerType.CLAUDE_CODE)
            .authToken("your-api-key")
            .build();

        // 事件监听器
        client.onMessageDelta(data -> {
            System.out.print(data.getContent());
        });

        client.onDone(data -> {
            System.out.printf("\n✅ Done! Success: %b%n", data.isSuccess());
        });

        // 连接
        client.connect();

        // 发送输入
        client.sendInput(InputData.builder()
            .content("Write a hello world in Java")
            .build());

        // 等待完成
        Thread.sleep(10000);

        // 清理
        client.close();
    }
}
```

**完整文档**: [`java-client/README.md`](java-client/README.md)

---

## 核心概念

### AEP v1 协议

所有客户端遵循 **AEP (Agent Exchange Protocol) v1** 协议：

#### 消息流向

```
Client                           Gateway                    Worker
  │                                │                          │
  ├── init (session_id, worker_type) ──────────────────────→│
  │←── init_ack ───────────────────┤                          │
  │                                │                          │
  ├── input (content) ─────────────┼────────────────────────→│
  │                                │                          │
  │←── message.start ──────────────┼──────────────────────────┤
  │←── message.delta ──────────────┼─── (streaming) ──────────┤
  │←── message.delta ──────────────┼──────────────────────────┤
  │←── message.end ────────────────┼──────────────────────────┤
  │                                │                          │
  │←── done (success, stats) ──────┼──────────────────────────┤
```

#### 消息格式 (NDJSON)

```json
{"id":"msg_001","v":1,"seq":1,"session_id":"sess_abc123","event":{"type":"input","data":{"content":"..."}}}
{"id":"msg_002","v":1,"seq":1,"session_id":"sess_abc123","event":{"type":"message.delta","data":{"delta":"Hello"}}}
```

每行一个 JSON 对象（NDJSON - Newline Delimited JSON）

### 事件类型

#### 客户端 → Gateway

| 事件 | 用途 | 必需字段 |
|------|------|---------|
| `init` | 初始化会话 | `worker_type`, `session_id?` |
| `input` | 用户输入 | `content` |
| `tool_result` | 工具执行结果 | `tool_call_id`, `output` |
| `permission_response` | 权限批准/拒绝 | `permission_id`, `allowed` |

#### Gateway → 客户端

| 事件 | 用途 | 关键字段 |
|------|------|---------|
| `init_ack` | 握手确认 | `session_id` |
| `message.start` | 流式消息开始 | `id`, `role` |
| `message.delta` | 流式内容块 | `delta` |
| `message.end` | 流式消息结束 | `id` |
| `message` | 完整消息 | `content` |
| `tool_call` | 工具调用请求 | `id`, `name`, `input` |
| `permission_request` | 权限请求 | `id`, `tool_name` |
| `state` | 状态变化 | `state` |
| `done` | 任务完成 | `success`, `stats` |
| `error` | 错误 | `code`, `message` |

### Worker 类型

| 类型 | 协议 | 描述 |
|------|------|------|
| `claude-code` | stdio/NDJSON | Claude Code CLI |
| `opencode-server` | HTTP/SSE | OpenCode Server |
| `pi-mono` | stdio | Pi-mono 协议（stub） |

### 会话生命周期

```
created → running → idle ↔ running → terminated → deleted
                      └──────────────→ terminated
```

- **created**: 会话初始化，等待 worker 启动
- **running**: Worker 活跃，处理输入
- **idle**: 等待输入（默认 30m 超时）
- **terminated**: Worker 退出
- **deleted**: GC 清理（默认 7 天后）

---

## 通用 API 模式

### 连接管理

**Python**:
```python
async with HotPlexClient(...) as client:
    # 自动连接和关闭
    pass
```

**TypeScript**:
```typescript
const client = new HotPlexClient(...);
await client.connect();
try {
    // 使用客户端
} finally {
    await client.close();
}
```

**Go**:
```go
c := client.New(cfg)
defer c.Close()
if err := c.Connect(ctx); err != nil { /* ... */ }
```

**Java**:
```java
HotPlexClient client = HotPlexClient.builder().build();
try {
    client.connect();
    // 使用客户端
} finally {
    client.close();
}
```

### 事件注册

**Python (装饰器)**:
```python
@client.on_message_delta
async def handle_delta(data: MessageDeltaData):
    print(data.content)
```

**TypeScript (EventEmitter)**:
```typescript
client.on("message_delta", (data) => {
    console.log(data.content);
});
```

**Go (回调函数)**:
```go
c.OnMessageDelta(func(data *client.MessageDeltaData) {
    fmt.Print(data.Content)
})
```

**Java (监听器)**:
```java
client.onMessageDelta(data -> {
    System.out.print(data.getContent());
});
```

### 发送输入

**Python**:
```python
await client.send_input(
    content="Write a hello world",
    metadata={"language": "python"}
)
```

**TypeScript**:
```typescript
await client.sendInput({
    content: "Write a hello world",
    metadata: { language: "typescript" }
});
```

**Go**:
```go
c.SendInput(ctx, &client.InputData{
    Content: "Write a hello world",
    Metadata: map[string]any{"language": "go"},
})
```

**Java**:
```java
client.sendInput(InputData.builder()
    .content("Write a hello world")
    .metadata(Map.of("language", "java"))
    .build());
```

### 工具调用响应

**Python**:
```python
@client.on_tool_call
async def handle_tool(data: ToolCallData):
    result = await execute_tool(data.name, data.input)
    await client.send_tool_result(
        tool_call_id=data.id,
        output=result,
    )
```

**TypeScript**:
```typescript
client.on("tool_call", async (data) => {
    const result = await executeTool(data.name, data.input);
    await client.sendToolResult({
        toolCallId: data.id,
        output: result,
    });
});
```

**Go**:
```go
c.OnToolCall(func(data *client.ToolCallData) {
    result := executeTool(data.Name, data.Input)
    c.SendToolResult(ctx, &client.ToolResultData{
        ToolCallID: data.ID,
        Output:     result,
    })
})
```

**Java**:
```java
client.onToolCall(data -> {
    String result = executeTool(data.getName(), data.getInput());
    client.sendToolResult(ToolResultData.builder()
        .toolCallId(data.getId())
        .output(result)
        .build());
});
```

---

## 认证

### API Key 认证

在 `init` 事件中传递：

```python
# Python
client = HotPlexClient(
    url="ws://localhost:8888",
    auth_token="sk-hotplex-xxx",
)
```

### JWT 认证

在 WebSocket 连接时传递 Bearer Token：

```typescript
// TypeScript
const client = new HotPlexClient({
    url: "ws://localhost:8888",
    authToken: "Bearer eyJhbGciOiJFUzI1NiIs...",
});
```

### Dev 模式

启动 Gateway 时使用 `-dev` 标志，接受任意 API Key：

```bash
./hotplex -dev
```

---

## 错误处理

### 错误事件

```python
@client.on_error
async def handle_error(data: ErrorData):
    print(f"Error [{data.code}]: {data.message}")
    if data.details:
        print(f"Details: {data.details}")
```

### 异常捕获

**Python**:
```python
from hotplex_client.exceptions import SessionError, TransportError

try:
    await client.send_input("...")
except SessionError as e:
    logger.error(f"Session error: {e}")
except TransportError as e:
    logger.error(f"Connection error: {e}")
```

**TypeScript**:
```typescript
try {
    await client.sendInput({ content: "..." });
} catch (error) {
    if (error instanceof SessionError) {
        console.error("Session error:", error.message);
    }
}
```

### 常见错误码

| 错误码 | 含义 | 处理建议 |
|--------|------|---------|
| `SESSION_NOT_FOUND` | 会话不存在 | 重新创建会话 |
| `SESSION_TERMINATED` | 会话已终止 | 创建新会话 |
| `SESSION_EXPIRED` | 会话过期 | 恢复会话或重建 |
| `UNAUTHORIZED` | 认证失败 | 检查 API Key/JWT |
| `INVALID_INPUT` | 输入无效 | 检查消息格式 |
| `WORKER_TIMEOUT` | Worker 超时 | 增加 timeout 或优化 Worker |

---

## 高级功能

### 会话恢复

```python
# 首次会话
async with HotPlexClient(...) as client:
    session_id = client.session_id
    await client.send_input("Start task...")

# 恢复会话（需要在 retention_period 内）
async with HotPlexClient(session_id=session_id, ...) as client:
    await client.send_input("Continue task...")
```

### 流式响应处理

```typescript
let fullContent = "";

client.on("message.start", () => {
    fullContent = "";
    console.log("Stream started");
});

client.on("message_delta", (data) => {
    fullContent += data.content;
    process.stdout.write(data.content);
});

client.on("message.end", () => {
    console.log("\nStream ended");
    console.log("Full content:", fullContent);
});
```

### 权限请求

```java
client.onPermissionRequest(data -> {
    boolean allowed = askUser(data.getToolName(), data.getDescription());
    client.sendPermissionResponse(PermissionResponseData.builder()
        .permissionId(data.getId())
        .allowed(allowed)
        .reason(allowed ? "User approved" : "User denied")
        .build());
});
```

### 状态监控

```go
c.OnState(func(data *client.StateData) {
    log.Printf("State changed: %s", data.State)

    switch data.State {
    case client.StateIdle:
        log.Println("Worker idle, ready for input")
    case client.StateTerminated:
        log.Println("Session terminated")
    }
})
```

---

## 生产环境最佳实践

### 1. 超时控制

**Python**:
```python
import asyncio

try:
    await asyncio.wait_for(
        client.send_input("..."),
        timeout=30.0,
    )
except asyncio.TimeoutError:
    logger.error("Request timed out")
```

**TypeScript**:
```typescript
const timeoutPromise = new Promise((_, reject) =>
    setTimeout(() => reject(new Error("Timeout")), 30000)
);

await Promise.race([
    client.sendInput({ content: "..." }),
    timeoutPromise,
]);
```

### 2. 错误重试

**Go**:
```go
func sendWithRetry(ctx context.Context, c *client.Client, content string, maxRetries int) error {
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        if err := c.SendInput(ctx, &client.InputData{Content: content}); err != nil {
            lastErr = err
            time.Sleep(time.Second * time.Duration(math.Pow(2, float64(i))))
            continue
        }
        return nil
    }
    return lastErr
}
```

### 3. 结构化日志

**Java**:
```java
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

private static final Logger log = LoggerFactory.getLogger(Main.class);

client.onMessageDelta(data -> {
    log.debug("Received delta: {} bytes", data.getContent().length());
});
```

### 4. 优雅关闭

**TypeScript**:
```typescript
const shutdown = async () => {
    console.log("\nShutting down...");
    await client.close();
    process.exit(0);
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
```

---

## 性能调优

### WebSocket 缓冲

**Python**:
```python
# 调整内部队列大小
client = HotPlexClient(
    url="...",
    queue_size=1000,  # 默认 256
)
```

### 批量处理

**TypeScript**:
```typescript
const batch: string[] = [];

client.on("message_delta", (data) => {
    batch.push(data.content);

    // 每 100ms 批量处理一次
    if (batch.length >= 10) {
        processBatch(batch.splice(0));
    }
});
```

---

## 测试

### 单元测试

```bash
# Python
cd python-client
pytest tests/ -v --cov=hotplex_client

# TypeScript
cd typescript-client
npm test

# Go
cd client
go test ./... -cover

# Java
cd java-client
mvn test
```

### 集成测试

```bash
# 启动测试 Gateway
./hotplex -dev &

# 运行集成测试
cd python-client
pytest tests/integration/ -v
```

---

## 故障排查

### 连接失败

**症状**: 无法连接到 Gateway

**检查清单**:
1. Gateway 是否运行: `curl http://localhost:9999/admin/health`
2. WebSocket URL 是否正确: `ws://localhost:8888`（不是 `http://`）
3. 防火墙是否阻止连接
4. 认证 token 是否有效

### 没有收到事件

**症状**: 连接成功但无事件

**可能原因**:
1. Worker 类型不支持（检查 `worker_type`）
2. Worker 进程启动失败（检查 Gateway 日志）
3. 事件处理器未注册（检查回调函数）

### 消息丢失

**症状**: 部分消息未接收到

**原因**: Gateway 在高负载时会丢弃 `message.delta` 事件（背压机制）

**解决方案**:
- 增加 `broadcast_queue_size`（默认 256）
- 使用 `message.end` 确认消息完整性
- 实现客户端重试逻辑

### 内存泄漏

**症状**: 客户端内存持续增长

**检查**:
1. 确保调用 `client.close()` 关闭连接
2. 清理事件监听器（TypeScript: `client.removeAllListeners()`）
3. 检查是否有未完成的 Promise/Future

---

## 示例项目

各语言目录提供完整示例：

| 示例 | Python | TypeScript | Go | Java |
|------|--------|-----------|----|----|
| 快速上手 | [`quickstart.py`](python-client/examples/quickstart.py) | [`quickstart.ts`](typescript-client/examples/quickstart.ts) | [`main.go`](../client/examples/quickstart.go) | [`Main.java`](java-client/src/main/java/dev/hotplex/example/Main.java) |
| 完整功能 | [`advanced.py`](python-client/examples/advanced.py) | [`complete.ts`](typescript-client/examples/complete.ts) | - | - |

---

## 协议参考

- **AEP v1 规范**: `docs/architecture/AEP-v1-Protocol.md`
- **架构设计**: `docs/architecture/Worker-Gateway-Design.md`
- **安全认证**: `docs/security/Security-Authentication.md`

---

## 贡献

欢迎贡献新语言客户端！

### 设计原则

1. **三层架构**: Protocol → Transport → Client
2. **事件驱动**: 使用回调/监听器模式
3. **异步优先**: 所有 I/O 操作异步
4. **类型安全**: 强类型消息定义
5. **资源管理**: 支持 RAII/try-with-resources

### 代码规范

- Python: PEP 8 + Black
- TypeScript: ESLint + Prettier
- Go: gofmt + golangci-lint
- Java: Google Java Style

---

## 许可证

Apache-2.0

---

## 支持

- **GitHub Issues**: https://github.com/hrygo/hotplex/issues
- **文档**: `docs/`
- **示例**: `examples/`
