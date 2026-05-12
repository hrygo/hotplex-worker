---
title: TypeScript Client SDK 参考
weight: 8
description: 基于 AEP v1 协议的 HotPlex Worker Gateway TypeScript 客户端 SDK 完整参考
persona: developer
difficulty: intermediate
---

# TypeScript Client SDK 参考

> `hotplex-client` — HotPlex Gateway 的官方 TypeScript 客户端，基于 `ws` WebSocket 和 `eventemitter3` 实现 AEP v1 全双工协议。

> **SDK 位置**：Go SDK 位于仓库根目录的 `client/`（独立 Go module），Python / TypeScript / Java SDK 位于 `examples/` 目录。

## 安装

```bash
npm install hotplex-client
# 或从 examples/typescript-client/ 本地引用
```

依赖：`ws`（WebSocket 客户端）、`eventemitter3`（类型安全事件分发）、`jose`（JWT 工具）。

## 快速开始

```ts
import { HotPlexClient, WorkerType } from 'hotplex-client'

const client = new HotPlexClient({
  url: 'ws://localhost:8888/ws',
  workerType: WorkerType.ClaudeCode,
  apiKey: process.env.HOTPLEX_API_KEY,
})

// 流式输出
client.on('delta', (data) => process.stdout.write(data.content))

// 任务完成
client.on('done', (data) => {
  console.log('\n--- done ---', 'success:', data.success)
  client.disconnect()
})

// 错误处理
client.on('error', (data) => {
  console.error('error:', data.code, data.message)
  client.disconnect()
})

// 连接并发送任务
const ack = await client.connect()
console.log('session:', ack.session_id)

await client.sendInputAsync('用 TypeScript 写一个 REST API 示例')
```

## 核心 API

### 创建客户端

```ts
const client = new HotPlexClient({
  url: 'ws://localhost:8888/ws',          // 必填：Gateway WebSocket 地址
  workerType: WorkerType.ClaudeCode,       // 必填：Worker 类型
  apiKey: 'ak-xxx',                        // X-API-Key header（可选）
  authToken: 'eyJ...',                     // JWT Bearer token（可选）
  reconnect: {
    enabled: true,                         // 启用自动重连（默认 true）
    maxAttempts: 10,                       // 最大重连次数（默认 10）
    baseDelayMs: 1000,                     // 退避基数 1s
    maxDelayMs: 60000,                     // 退避上限 60s
  },
  heartbeat: {
    pingIntervalMs: 54000,                 // 心跳间隔（默认 54s）
    pongTimeoutMs: 60000,                  // Pong 超时（默认 60s）
    maxMissedPongs: 3,                     // 最大丢失 Pong 数
  },
})
```

### 连接与会话

```ts
// 新建 session
const ack = await client.connect()
console.log(ack.session_id, ack.state, ack.server_caps)
// ack.session_id  — 服务端分配的 session ID
// ack.state       — 初始状态（created / running）
// ack.server_caps — 服务端能力集

// 恢复已有 session
const ack2 = await client.resume('sess_xxxx')

// 断开连接
client.disconnect()  // 或 client.close()
```

### 只读属性

| 属性 | 类型 | 说明 |
|------|------|------|
| `sessionId` | `string \| null` | 当前 session ID |
| `state` | `SessionState` | 当前 session 状态 |
| `connected` | `boolean` | 是否已连接 |
| `reconnecting` | `boolean` | 是否正在重连 |

### 发送方法

| 方法 | 用途 |
|------|------|
| `sendInput(content, metadata?)` | 发送用户输入（fire-and-forget） |
| `sendInputAsync(content, metadata?)` | 发送并等待 `done`/`error`（Promise，超时 10 分钟） |
| `sendPermissionResponse(id, allowed, reason?)` | 响应权限请求 |
| `sendToolResult(id, output, error?)` | 返回工具执行结果 |
| `sendControl(action)` | 发送控制指令 |
| `terminate()` | 终止 session（等价于 `sendControl(ControlAction.Terminate)`） |
| `delete()` | 删除 session |

### 事件处理

`HotPlexClient` 继承 `EventEmitter<HotPlexClientEvents>`，所有事件回调参数类型安全：

```ts
// 流式内容
client.on('delta', (data: MessageDeltaData, env: Envelope) => {
  process.stdout.write(data.content)
})

// 完整消息
client.on('message', (data: MessageData, env: Envelope) => {
  console.log(data.role, data.content)
})

// 流式生命周期
client.on('messageStart', (data, env) => { /* data.id, data.role, data.content_type */ })
client.on('messageEnd', (data, env) => { /* data.message_id */ })

// 工具调用
client.on('toolCall', (data: ToolCallData, env: Envelope) => {
  console.log('tool:', data.name, data.input)
})
client.on('toolResult', (data: ToolResultData, env: Envelope) => {
  if (data.error) console.error('tool error:', data.error)
})

// Agent 推理
client.on('reasoning', (data, env) => { /* data.content */ })

// 执行步骤
client.on('step', (data, env) => { /* data.id, data.step_type */ })

// 任务完成
client.on('done', (data: DoneData, env: Envelope) => {
  console.log('success:', data.success, 'dropped:', data.dropped)
  if (data.stats) {
    console.log('tokens:', data.stats.total_tokens)
    console.log('cost: $', data.stats.cost_usd)
  }
})

// 错误
client.on('error', (data: ErrorData, env: Envelope) => {
  console.error('error:', data.code, data.message)
})

// 状态变更
client.on('state', (data: StateData, env: Envelope) => {
  console.log('state ->', data.state)
})

// 权限请求
client.on('permissionRequest', (data: PermissionRequestData, env: Envelope) => {
  client.sendPermissionResponse(data.id, true)  // 自动批准
})

// 心跳响应
client.on('pong', (data: PongData, env: Envelope) => { /* data.state */ })
```

### 生命周期事件

```ts
// 连接成功（含重连成功）
client.on('connected', (ack: InitAckData) => {
  console.log('session:', ack.session_id, 'worker:', ack.server_caps.worker_type)
})

// 连接断开
client.on('disconnected', (reason: string) => {
  console.log('disconnected:', reason)
})

// 正在重连
client.on('reconnecting', (attempt: number) => {
  console.log(`reconnecting... attempt ${attempt}`)
})

// 服务端发起重连指令
client.on('reconnect', (data: ControlData, env: Envelope) => {
  console.log('server requested reconnect:', data.reason)
})

// Session 失效
client.on('sessionInvalid', (data: ControlData, env: Envelope) => {
  console.log('session invalid, recoverable:', data.recoverable)
})

// 限流通知
client.on('throttle', (data: ControlData, env: Envelope) => {
  console.log('throttled:', data.suggestion?.backoff_ms, 'ms backoff')
})
```

## 事件类型

### AEP v1 事件类型一览

| EventKind | Data 类型 | 方向 | 说明 |
|-----------|----------|------|------|
| `init` | `InitData` | C→S | 握手初始化 |
| `init_ack` | `InitAckData` | S→C | 握手响应 |
| `error` | `ErrorData` | S→C | 错误通知 |
| `state` | `StateData` | S→C | Session 状态变更 |
| `input` | `InputData` | C→S | 用户输入 |
| `message.start` | `MessageStartData` | S→C | 流式消息开始 |
| `message.delta` | `MessageDeltaData` | S→C | 流式内容片段 |
| `message.end` | `MessageEndData` | S→C | 流式消息结束 |
| `message` | `MessageData` | S→C | 完整消息 |
| `tool_call` | `ToolCallData` | S→C | Worker 调用工具 |
| `tool_result` | `ToolResultData` | S→C | 工具执行结果 |
| `reasoning` | `ReasoningData` | S→C | Agent 推理过程 |
| `step` | `StepData` | S→C | 执行步骤标记 |
| `done` | `DoneData` | S→C | 任务完成 |
| `permission_request` | `PermissionRequestData` | S→C | 请求用户授权 |
| `control` | `ControlData` | S→C | 服务端控制指令 |
| `pong` | `PongData` | S→C | 心跳响应 |

> **注意 1**：上表中部分事件类型（如 `question_request`、`question_response`、`elicitation_request`、`elicitation_response`、`context_usage`、`skills_list`、`mcp_status`、`worker_command`）在 AEP 协议中定义，但 TS SDK `constants.ts` 中未导出对应的 `EventKind` 常量。处理这些事件时需使用字符串形式匹配 `event.type` 字段。

> **注意 2**：`control` 事件的 `reset` 和 `gc` action 在 AEP 协议中定义（Client → Server），但 TS SDK 的 `ControlAction` 常量中未导出这两个值。如需发送 `reset`/`gc` 指令，请直接使用字符串 `client.sendControl('reset')` / `client.sendControl('gc')`。

### Session 状态

```ts
import { SessionState } from 'hotplex-client'

// SessionState.Created    = 'created'
// SessionState.Running    = 'running'
// SessionState.Idle       = 'idle'
// SessionState.Terminated = 'terminated'
// SessionState.Deleted    = 'deleted'
```

## 自动重连

启用 `reconnect.enabled` 后，SDK 自动处理以下断连场景：

- **触发条件**：WebSocket 关闭、服务端 `control.reconnect` 指令
- **退避算法**：指数退避 + 10% 随机抖动（base 1s → max 60s）
- **最大重试**：10 次（可通过 `reconnect.maxAttempts` 配置）
- **停止条件**：客户端主动 `disconnect()`、达到最大重试次数、服务端发送 `session_invalid` / `delete`

```ts
client.on('reconnecting', (attempt) => {
  console.log(`reconnect attempt ${attempt}`)
})
```

## SESSION_BUSY 自动重试

Gateway 返回 `SESSION_BUSY` 错误时，SDK 自动延迟 500ms 重发 `input`，无需手动处理。此行为仅在 `sendInput` 发出的消息中生效。

## 错误处理

### 错误类层级

```
HotPlexError (base)
├── ConnectionError       — WebSocket 连接失败
├── SessionError          — Session 相关错误（含 code）
├── TimeoutError          — 操作超时
└── ProtocolError         — 协议违规（含 code）
```

### 错误码

```ts
import { ErrorCode } from 'hotplex-client'

client.on('error', (data) => {
  switch (data.code) {
    case ErrorCode.SessionBusy:
      // Session 忙，SDK 自动重试
      break
    case ErrorCode.Unauthorized:
      // Token 过期
      break
    case ErrorCode.WorkerCrash:
      // Worker 崩溃
      break
    case ErrorCode.SessionNotFound:
      // Session 不存在
      break
    default:
      console.error(`[${data.code}] ${data.message}`)
  }
})
```

## Envelope 工具函数

SDK 导出一组低级 Envelope 操作函数，供高级用户自定义协议处理：

```ts
import {
  newEventId,           // 生成 evt_xxx ID
  newSessionId,         // 生成 sess_xxx ID
  createEnvelope,       // 通用 Envelope 构造
  createInitEnvelope,   // init 握手 Envelope
  createInputEnvelope,  // 用户输入 Envelope
  createPingEnvelope,   // 心跳 Envelope
  createControlEnvelope, // 控制指令 Envelope
  createPermissionResponseEnvelope, // 权限响应 Envelope
  serializeEnvelope,    // Envelope → NDJSON 字符串
  deserializeEnvelope,  // NDJSON → Envelope
  isInitAck,            // 类型守卫
  isError,
  isState,
  isDone,
  isDelta,
  isControl,
} from 'hotplex-client'
```

## 完整示例

### 权限处理与流式输出

```ts
import { HotPlexClient, WorkerType, ErrorCode } from 'hotplex-client'

const client = new HotPlexClient({
  url: 'ws://localhost:8888/ws',
  workerType: WorkerType.ClaudeCode,
  authToken: process.env.HOTPLEX_TOKEN,
  reconnect: { enabled: true, maxAttempts: 5 },
})

// 安全工具列表，自动批准
const SAFE_TOOLS = ['read_file', 'grep', 'glob', 'bash']

client.on('delta', (data) => process.stdout.write(data.content))

client.on('toolCall', (data) => {
  console.log(`\n[tool call: ${data.name}]`)
})

client.on('permissionRequest', (data) => {
  const approved = SAFE_TOOLS.includes(data.tool_name)
  console.log(`\n  ${approved ? 'Approved' : 'Denied'}: ${data.tool_name}`)
  client.sendPermissionResponse(data.id, approved, approved ? '' : 'Not in allowlist')
})

client.on('done', (data) => {
  console.log('\n--- Task completed ---')
  if (data.stats) {
    console.log(`  Duration: ${data.stats.duration_ms}ms`)
    console.log(`  Tokens: ${data.stats.total_tokens}`)
    console.log(`  Cost: $${data.stats.cost_usd}`)
  }
  client.disconnect()
})

client.on('error', (data) => {
  if (data.code === ErrorCode.SessionBusy) return  // 自动重试
  console.error(`Error: [${data.code}] ${data.message}`)
  client.disconnect()
})

// 连接并发送任务
const ack = await client.connect()
console.log(`Session: ${ack.session_id}\n`)

await client.sendInputAsync('Read go.mod and list all dependencies')
```

### Session 恢复

```ts
// 首次连接，保存 session ID
const ack = await client.connect()
const sessionId = ack.session_id

// ... 使用 session ...

// 断开后恢复
client.disconnect()
// 稍后使用同一 session ID 恢复
const ack2 = await client.resume(sessionId)
console.log('Resumed session:', ack2.session_id)
```
