---
title: Java Client SDK 参考
weight: 9
description: 基于 AEP v1 协议的 HotPlex Worker Gateway Java 客户端 SDK 完整参考
persona: developer
difficulty: intermediate
---

# Java Client SDK 参考

> `dev.hotplex:hotplex-client` — HotPlex Gateway 的官方 Java 客户端，基于 Spring WebSocket 实现 AEP v1 全双工协议。

> **SDK 位置**：Go SDK 位于仓库根目录的 `client/`（独立 Go module），Python / TypeScript / Java SDK 位于 `examples/` 目录。

## 前置条件

- Java 17+
- Maven 3.8+
- 运行中的 HotPlex Gateway（`ws://localhost:8888`）

## 安装

SDK 位于 `examples/java-client/`，以 Maven 项目形式提供：

```bash
cd examples/java-client
mvn clean install -DskipTests
```

核心依赖：

| 依赖 | 版本 | 用途 |
|------|------|------|
| `spring-boot-starter-websocket` | 3.2.5 | WebSocket 客户端 |
| `jackson-databind` + `jackson-datatype-jsr310` | (managed) | JSON 序列化 |
| `jjwt-api` / `jjwt-impl` / `jjwt-jackson` | 0.12.6 | JWT 生成 |
| `bcprov-jdk18on` | 1.78 | BouncyCastle ECDSA |

## 快速开始

### 一次性任务

```java
import dev.hotplex.client.HotPlexClient;
import dev.hotplex.protocol.*;
import dev.hotplex.security.JwtTokenGenerator;

public class QuickStart {
    public static void main(String[] args) throws Exception {
        String url = System.getenv().getOrDefault("HOTPLEX_GATEWAY_URL", "ws://localhost:8888");
        String signingKey = System.getenv("HOTPLEX_SIGNING_KEY"); // 至少 32 字节

        var tokenGen = new JwtTokenGenerator(signingKey, "hotplex");
        var latch = new java.util.concurrent.CountDownLatch(1);

        try (var client = HotPlexClient.builder()
                .url(url)
                .workerType("claude-code")
                .tokenGenerator(tokenGen)
                .build()) {

            client.<MessageDeltaData>on("messageDelta", delta -> {
                System.out.print(delta.getContent());
            });
            client.<DoneData>on("done", done -> {
                System.out.println("\nDone: " + done.getStats());
                latch.countDown();
            });
            client.<ErrorData>on("error", err -> {
                System.err.println("Error: " + err.getMessage());
                latch.countDown();
            });

            client.connect().get(); // 阻塞等待 init_ack
            client.sendInput("用 Java 写一个 hello world");
            latch.await(5, java.util.concurrent.TimeUnit.MINUTES);
        }
    }
}
```

### 交互式会话

```java
try (var client = HotPlexClient.builder()
        .url(url).workerType("claude-code").tokenGenerator(tokenGen).build()) {

    client.<MessageDeltaData>on("messageDelta", d -> {
        System.out.print(d.getContent());
        System.out.flush();
    });

    var ack = client.connect().get(10, TimeUnit.SECONDS);
    System.out.println("Session: " + ack.getSessionId());

    var scanner = new java.util.Scanner(System.in);
    while (true) {
        System.out.print("> ");
        String input = scanner.nextLine();
        if ("/quit".equals(input)) break;
        if ("/status".equals(input)) {
            System.out.printf("State: %s, Connected: %s%n",
                client.getState(), client.isConnected());
            continue;
        }
        // sendInputAsync 返回 CompletableFuture<DoneData>
        var done = client.sendInputAsync(input).get(5, TimeUnit.MINUTES);
        System.out.println("\nStats: " + done.getStats());
    }
}
```

## 核心 API

### Builder 模式创建客户端

```java
var client = HotPlexClient.builder()
    .url("ws://localhost:8888")              // 必填：Gateway WebSocket 地址
    .workerType("claude-code")               // 必填：Worker 类型
    .apiKey("ak-xxx")                        // 可选：X-API-Key header
    .tokenGenerator(jwtGenerator)            // 可选：JWT ES256 认证
    .config(initConfig)                      // 可选：Session 配置
    .build();
```

Builder 校验：`url` 和 `workerType` 缺失时抛出 `IllegalArgumentException`。

### 连接与会话

```java
// 新建 session
CompletableFuture<InitAckData> connect()

// 恢复已有 session
CompletableFuture<InitAckData> connect(String existingSessionId)
CompletableFuture<InitAckData> resume(String sessionId)

// 运行时状态查询
String getSessionId()            // 当前 session ID
SessionState getState()          // 当前状态枚举
boolean isConnected()            // volatile，init_ack 后为 true
boolean isReconnecting()         // 重连中

// 断开
void disconnect()                // 等效 close()
void close()                     // AutoCloseable，完整清理
```

### 发送方法

| 方法 | 说明 |
|------|------|
| `sendInput(content)` | 发送用户输入（fire-and-forget） |
| `sendInput(content, metadata)` | 带元数据的用户输入 |
| `sendInputAsync(content)` | 发送并等待 `done`，默认 5 分钟超时 |
| `sendInputAsync(content, timeoutMs)` | 自定义超时的异步发送 |
| `sendControl(action)` | 控制指令（`"terminate"` / `"delete"`） |
| `sendPermissionResponse(id, allowed, reason)` | 响应权限请求 |

所有发送方法内部调用 `requireConnected()`，未连接时抛出 `IllegalStateException`。

### 事件监听

```java
// 注册
client.<MessageDeltaData>on("messageDelta", delta -> { ... });

// 移除单个
client.off("messageDelta", handler);

// 清除全部
client.clearListeners();
```

事件存储：`ConcurrentHashMap<String, List<Consumer<?>>>` + `CopyOnWriteArrayList`，线程安全。

### 支持的事件名称

| 事件名 | 数据类型 | 说明 |
|--------|---------|------|
| `connected` | `InitAckData` | 会话建立成功 |
| `disconnected` | `String` | 连接断开（含原因） |
| `reconnecting` | `Integer` | 重连尝试次数 |
| `error` | `ErrorData` | 网关错误 |
| `state` | `StateData` | Session 状态变更 |
| `done` | `DoneData` | 任务完成（Turn 终止符） |
| `messageDelta` | `MessageDeltaData` | 流式内容片段 |
| `message` | `MessageData` | 完整消息 |
| `messageStart` | `Map<?, ?>` | 流式消息开始 |
| `messageEnd` | `Map<?, ?>` | 流式消息结束 |
| `toolCall` | `ToolCallData` | Worker 请求工具执行 |
| `toolResult` | `ToolResultData` | 工具执行结果 |
| `reasoning` | `Map<?, ?>` | Agent 推理过程 |
| `step` | `Map<?, ?>` | 执行步骤 |
| `permissionRequest` | `PermissionRequestData` | 权限请求 |
| `pong` | `Map<?, ?>` | 心跳响应 |
| `reconnect` | `ControlData` | 服务端请求重连 |
| `sessionInvalid` | `ControlData` | 会话失效 |
| `throttle` | `ControlData` | 服务端限流 |
| `terminate` | `ControlData` | 服务端终止会话 |

## 事件类型

### AEP v1 EventKind 枚举

```java
public enum EventKind {
    Error, State, Input, Done,
    Message, MessageStart, MessageDelta, MessageEnd,
    ToolCall, ToolResult, Reasoning, Step,
    Raw, PermissionRequest, PermissionResponse,
    Ping, Pong, Control, InitAck, Init
}
```

每个枚举值对应 AEP v1 wire format 字符串（如 `MessageDelta` → `"message.delta"`），通过 `getValue()` 获取。

### Session 状态枚举

```java
public enum SessionState {
    Created, Running, Idle, Terminated, Deleted
}
```

- `isActive()` — `Created` / `Running` / `Idle` 返回 `true`
- `isTerminal()` — 仅 `Deleted` 返回 `true`

状态流转：`Created → Running ⇄ Idle → Terminated → Deleted`

### 错误码枚举

22 个错误码，覆盖 Worker 故障、Session 生命周期、认证、协议、限流等：

| 枚举值 | Wire Format | 含义 |
|--------|-------------|------|
| `WorkerStartFailed` | `WORKER_START_FAILED` | Worker 启动失败 |
| `WorkerCrash` | `WORKER_CRASH` | Worker 崩溃 |
| `WorkerTimeout` | `WORKER_TIMEOUT` | Worker 执行超时 |
| `WorkerOOM` | `WORKER_OOM` | Worker 内存溢出 |
| `WorkerSIGKILL` | `PROCESS_SIGKILL` | 进程被 SIGKILL |
| `SessionBusy` | `SESSION_BUSY` | Session 正忙 |
| `SessionNotFound` | `SESSION_NOT_FOUND` | Session 不存在 |
| `SessionExpired` | `SESSION_EXPIRED` | Session 已过期 |
| `Unauthorized` | `UNAUTHORIZED` | 认证失败 |
| `AuthRequired` | `AUTH_REQUIRED` | 需要认证 |
| `RateLimited` | `RATE_LIMITED` | 限流 |
| `GatewayOverload` | `GATEWAY_OVERLOAD` | 网关过载 |
| `ExecutionTimeout` | `EXECUTION_TIMEOUT` | 执行超时 |

完整列表参见 `dev.hotplex.protocol.ErrorCode`。

## 连接生命周期

### 心跳

| 参数 | 值 | 说明 |
|------|----|------|
| Ping 间隔 | 15,000ms | `PING_PERIOD_MS` |
| Pong 超时 | 5,000ms | `PONG_WAIT_MS` |
| 最大丢失 | 3 次 | `MAX_MISSED_PONGS` |

连续 3 次未收到 Pong 响应，自动关闭连接并触发重连。

### 自动重连

| 参数 | 值 | 说明 |
|------|----|------|
| 基础延迟 | 1,000ms | `RECONNECT_BASE_DELAY_MS` |
| 最大延迟 | 60,000ms | `RECONNECT_MAX_DELAY_MS` |
| 最大尝试 | 10 次 | `RECONNECT_MAX_ATTEMPTS` |

指数退避策略：`delay = min(1000 × 2^(attempt-1), 60000)`

**触发重连**：连接意外断开（`shouldReconnect && !closed`）、服务端发送 `reconnect` 控制指令。

**禁止重连**：调用 `close()`、服务端发送 `session_invalid` / `terminate` / `delete`。

### Session Busy 重试

收到 `SESSION_BUSY` 错误时，自动延迟 2 秒（`SESSION_BUSY_RETRY_DELAY_MS`）后重发待处理输入。

## Token 生成

### JwtTokenGenerator

ES256 (ECDSA P-256) 签名，从 `secret` 派生 P-256 密钥对：

```java
// 基本用法
var tokenGen = new JwtTokenGenerator(secret, "hotplex");

// 自定义 audience
var tokenGen = new JwtTokenGenerator(secret, "hotplex", "my-gateway");
```

- `secret` — 至少 32 字节的签名字符串
- 内部通过 BouncyCastle HKDF-SHA256 从 secret 派生 P-256 密钥对，与 Go SDK 完全一致

```java
// 生成 token
String token = tokenGen.generateToken(
    "user-1",                              // subject
    List.of("session:create", "session:write"),  // scopes
    3600L                                   // TTL 秒
);

// 带自定义 JTI
String token = tokenGen.generateTokenWithJti(
    "user-1", List.of("worker:use"), 3600L, "my-trace-id"
);

// 带额外 claims
String token = tokenGen.generateTokenWithClaims(
    "user-1", List.of("worker:use"), 3600L,
    Map.of("bot_id", "B12345")
);
```

JWT Claims：`sub`（subject）、`iss`（issuer）、`aud`（audience）、`iat`（签发时间）、`exp`（过期时间）、`scopes`（权限列表）、`jti`（唯一 ID）。

## 已知限制

1. **无自定义异常层次**：连接/发送错误通过 `RuntimeException` 或 `CompletableFuture` 异常传递，无 `HotPlexException` 等自定义类型。
2. **事件数据多态性有限**：`messageStart`、`messageEnd`、`reasoning`、`step`、`pong` 等事件的数据类型为 `Map<?, ?>`，无强类型模型。
3. **无工具结果上报**：不支持 `sendToolResult()`，工具执行结果由 Gateway 和 Worker 内部处理。
4. **Session 配置**：`InitData.InitConfig` 支持设置 model、systemPrompt、allowedTools、maxTurns、workDir，但需在 Builder 阶段通过 `config()` 传入。
