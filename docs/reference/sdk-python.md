---
title: Python Client SDK 参考
weight: 7
description: 基于 AEP v1 协议的 HotPlex Worker Gateway Python 客户端 SDK 完整参考
---

# Python Client SDK 参考

> `hotplex-client` — HotPlex Gateway 的官方 Python 客户端，基于 `websockets` 实现异步 AEP v1 全双工协议。

> **SDK 位置**：Go SDK 位于仓库根目录的 `client/`（独立 Go module），Python / TypeScript / Java SDK 位于 `examples/` 目录。

## 安装

```bash
pip install hotplex-client
# 或从 examples/python-client/ 本地安装
pip install -e ./examples/python-client
```

依赖：`websockets`（异步 WebSocket 客户端）。

## 快速开始

```python
import asyncio
from hotplex_client import HotPlexClient, WorkerType

async def main():
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
    ) as client:
        print(f"Connected | Session: {client.session_id}")

        # 注册流式输出回调
        @client.on("message.delta")
        async def on_delta(data):
            print(data["content"], end="", flush=True)

        # 发送任务并等待完成
        await client.send_input("用 Python 写一个快速排序算法")
        result = await client.wait_for_done()
        print(f"\nDone: success={result['success']}")

asyncio.run(main())
```

## 核心 API

### 创建客户端

```python
from hotplex_client import HotPlexClient, WorkerType

client = HotPlexClient(
    url="ws://localhost:8888",                    # 必填：Gateway WebSocket 地址
    worker_type=WorkerType.CLAUDE_CODE,           # 必填：Worker 类型
    auth_token="eyJ...",                          # JWT 认证（可选）
    session_id="sess_xxxx",                       # 恢复已有 session（可选）
    config={"model": "sonnet"},                   # Worker 配置覆盖（可选）
)
```

支持 `async with` 上下文管理器：`__aenter__` 自动调用 `connect()`，`__aexit__` 自动调用 `close()`。

### 连接与会话

```python
# 方式一：上下文管理器（推荐）
async with HotPlexClient(url="ws://localhost:8888", worker_type=WorkerType.CLAUDE_CODE) as client:
    print(client.session_id)  # 服务端分配的 session ID
    ...

# 方式二：手动管理
client = HotPlexClient(url="ws://localhost:8888", worker_type=WorkerType.CLAUDE_CODE)
session_id = await client.connect()
try:
    ...
finally:
    await client.close()

# 恢复已有 session
client = HotPlexClient(url="...", worker_type=WorkerType.CLAUDE_CODE, session_id="sess_xxxx")

# 查询运行时状态
client.session_id    # 当前 session ID
client.is_connected  # 是否已连接
```

### 发送方法

| 方法 | 返回值 | 说明 |
|------|--------|------|
| `send_input(content, metadata=None)` | `None` | 发送用户输入 |
| `wait_for_done(timeout=None)` | `DoneData` | 等待当前任务完成，可设超时秒数 |
| `send_permission_response(permission_id, allowed, reason=None)` | `None` | 响应权限请求 |
| `send_tool_result(tool_call_id, output, error=None)` | `None` | 返回工具执行结果 |
| `terminate()` | `None` | 终止当前 session |
| `close()` | `None` | 关闭连接并停止事件循环 |

> **注意**：Python SDK 当前不支持 `send_question_response`（`question_request` 问答响应）和 `send_elicitation_response`（`elicitation_request` MCP 输入响应）方法。收到这两种交互请求时，需要通过底层 `transport.send()` 手动构造 Envelope 发送响应。如需完整的交互支持，请使用 TypeScript SDK。

### 事件回调注册

SDK 提供两种回调注册方式：**装饰器**和**直接方法注册**。所有回调必须是 `async` 函数。

#### 装饰器模式（推荐）

```python
@client.on("message.delta")
async def on_delta(data):
    print(data["content"], end="", flush=True)

@client.on("done")
async def on_done(data):
    print(f"完成: success={data['success']}")

@client.on("error")
async def on_error(data):
    print(f"错误: {data['code']} - {data['message']}")

@client.on("state")
async def on_state(data):
    print(f"状态变更: {data['state']}")

@client.on("permission_request")
async def on_permission(data):
    await client.send_permission_response(data["id"], allowed=True)

@client.on("tool_call")
async def on_tool_call(data):
    print(f"工具调用: {data['name']}")

@client.on("reasoning")
async def on_reasoning(data):
    # Agent 思考过程
    pass

@client.on("step")
async def on_step(data):
    # 执行步骤
    pass
```

#### 方法注册

```python
async def handle_delta(data):
    print(data["content"], end="")

client.on_message_delta(handle_delta)
client.on_message_start(lambda d: print(f"消息开始: {d['id']}"))
client.on_message_end(lambda d: print(f"\n消息结束: {d['message_id']}"))
client.on_message(lambda d: print(f"完整消息: {d['content']}"))
client.on_tool_call(lambda d: print(f"工具: {d['name']}"))
client.on_permission_request(lambda d: ...)
client.on_state_change(lambda d: print(f"状态: {d['state']}"))
client.on_done(lambda d: print(f"完成: {d['success']}"))
client.on_error(lambda d: print(f"错误: {d['message']}"))
```

## 事件类型

### AEP v1 事件类型一览

| event type | Data 类型 | 方向 | 说明 |
|-----------|----------|------|------|
| `init` | `InitData` | C→S | 握手初始化 |
| `init_ack` | `InitAckData` | S→C | 握手响应 |
| `error` | `ErrorData` | S→C | 错误通知 |
| `state` | `StateData` | S→C | Session 状态变更 |
| `input` | `InputData` | C→S | 用户输入 |
| `message.start` | `MessageStartData` | S→C | 流式消息开始 |
| `message.delta` | `MessageDeltaData` | S→C | 流式内容片段 |
| `message.end` | `MessageEndData` | S→C | 流式消息结束 |
| `message` | `MessageData` | S→C | 完整消息（非流式） |
| `tool_call` | `ToolCallData` | S→C | Worker 调用工具 |
| `tool_result` | `ToolResultData` | S→C | 工具执行结果 |
| `reasoning` | `ReasoningData` | S→C | Agent 推理过程 |
| `step` | `StepData` | S→C | 执行步骤标记 |
| `done` | `DoneData` | S→C | 任务完成（Turn 终止符） |
| `permission_request` | `PermissionRequestData` | S→C | 请求用户授权 |
| `permission_response` | `PermissionResponseData` | C→S | 用户授权/拒绝 |
| `control` | `ControlData` | 双向 | 控制指令（Client 可发送 terminate/delete 等，Server 可发送 reconnect/throttle 等） |

### Session 状态

```python
from hotplex_client import SessionState

# SessionState.CREATED    = 'created'
# SessionState.RUNNING    = 'running'
# SessionState.IDLE       = 'idle'
# SessionState.TERMINATED = 'terminated'
# SessionState.DELETED    = 'deleted'
```

状态通过 `StrEnum` 定义，可直接比较字符串值。

## 三层架构

```
hotplex_client/
├── protocol.py    # 协议层：Envelope 编解码、NDJSON 序列化、Envelope 构造函数
├── transport.py   # 传输层：WebSocket 连接管理、消息收发队列、后台接收循环
├── client.py      # 业务层：高阶 API、事件回调分发、async context manager
├── types.py       # 数据类型：所有 dataclass / StrEnum 定义
└── exceptions.py  # 异常层级：Protocol → Session → Transport → Auth
```

### 传输层 (WebSocketTransport)

```python
from hotplex_client.transport import WebSocketTransport

transport = WebSocketTransport(
    max_queue_size=1000,   # 消息缓冲队列大小
    ping_interval=54.0,    # 心跳间隔（秒）
    ping_timeout=10.0,     # Pong 超时（秒）
)

# 连接
session_id = await transport.connect(
    url="ws://localhost:8888",
    worker_type=WorkerType.CLAUDE_CODE,
    session_id=None,        # 恢复已有 session
    auth_token=None,        # JWT 认证
    config=None,            # Worker 配置
)

# 收发
await transport.send(envelope)
envelope = await transport.receive()

# 关闭
await transport.close()
```

### 协议层函数

```python
from hotplex_client.protocol import (
    generate_event_id,       # 生成 evt_xxx ID
    generate_session_id,     # 生成 sess_xxx ID
    encode_envelope,         # Envelope → NDJSON 字符串
    decode_envelope,         # NDJSON → Envelope
    create_envelope,         # 通用 Envelope 构造
    create_init_envelope,    # init 握手 Envelope
    create_input_envelope,   # 用户输入 Envelope
    create_ping_envelope,    # 心跳 Envelope
    create_control_envelope, # 控制指令 Envelope
    create_permission_response_envelope,  # 权限响应 Envelope
    create_tool_result_envelope,          # 工具结果 Envelope
    is_init_ack,             # 类型守卫
    is_error,
    is_state,
    is_done,
    is_delta,
    is_control,
)
```

## 错误处理

### 异常层级

```
HotPlexError (base)
├── ProtocolError              — AEP 协议错误（编码/解码/验证）
│   ├── InvalidMessageError    — 消息格式无效
│   └── VersionMismatchError   — 协议版本不匹配
├── SessionError               — Session 相关错误
│   ├── SessionNotFoundError   — Session 不存在
│   ├── SessionTerminatedError — Session 已终止
│   └── SessionExpiredError    — Session 已过期
├── TransportError             — 网络传输错误
│   ├── ConnectionLostError    — 连接断开
│   ├── ReconnectFailedError   — 重连失败（含重试次数）
│   └── HeartbeatTimeoutError  — 心跳超时
└── AuthError                  — 认证错误
    └── UnauthorizedError      — 未授权（Token 无效或过期）
```

### 错误处理模式

```python
from hotplex_client import HotPlexClient, WorkerType
from hotplex_client.exceptions import (
    UnauthorizedError,
    SessionError,
    TransportError,
    ConnectionLostError,
)

try:
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
        auth_token="your-jwt-token",
    ) as client:
        await client.send_input("hello")
        result = await client.wait_for_done(timeout=120)
except UnauthorizedError:
    print("Token 无效或过期，请重新认证")
except ConnectionLostError:
    print("连接断开")
except TransportError as e:
    print(f"传输错误: {e}")
except SessionError as e:
    print(f"Session 错误: {e}")
except asyncio.TimeoutError:
    print("任务超时")
```

## 完整示例

### 权限处理与多轮对话

```python
import asyncio
import os
from hotplex_client import HotPlexClient, WorkerType

SAFE_TOOLS = {"Read", "Glob", "Grep"}

async def main():
    url = os.getenv("HOTPLEX_URL", "ws://localhost:8888")

    async with HotPlexClient(
        url=url,
        worker_type=WorkerType.CLAUDE_CODE,
        auth_token=os.getenv("HOTPLEX_TOKEN"),
    ) as client:
        print(f"Session: {client.session_id}")

        full_content = ""

        @client.on("message.delta")
        async def on_delta(data):
            nonlocal full_content
            full_content += data["content"]
            print(data["content"], end="", flush=True)

        @client.on("tool_call")
        async def on_tool(data):
            print(f"\n[tool: {data['name']}]")

        @client.on("permission_request")
        async def on_permission(data):
            approved = data["tool_name"] in SAFE_TOOLS
            print(f"  {'Approved' if approved else 'Denied'}: {data['tool_name']}")
            await client.send_permission_response(
                permission_id=data["id"],
                allowed=approved,
                reason="" if approved else "Not in safe tools list",
            )

        @client.on("error")
        async def on_error(data):
            print(f"\n[error] {data['code']}: {data['message']}")

        # 发送任务
        await client.send_input("Read go.mod and list all dependencies")
        result = await client.wait_for_done(timeout=120)
        print(f"\nDone: success={result['success']}")
        if result.get("stats"):
            print(f"Tokens: {result['stats'].get('total_tokens')}")
            print(f"Cost: ${result['stats'].get('cost_usd', 0):.4f}")

asyncio.run(main())
```

### Session 恢复

```python
import asyncio
from hotplex_client import HotPlexClient, WorkerType

async def main():
    # 首次连接
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
    ) as client:
        session_id = client.session_id
        print(f"Session: {session_id}")

        @client.on("message.delta")
        async def on_delta(data):
            print(data["content"], end="", flush=True)

        await client.send_input("记住这个数字: 42")
        await client.wait_for_done()

    # 恢复同一 session
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
        session_id=session_id,
    ) as client:
        print(f"Resumed: {client.session_id}")

        @client.on("message.delta")
        async def on_delta(data):
            print(data["content"], end="", flush=True)

        await client.send_input("我刚才让你记住的数字是什么？")
        await client.wait_for_done()

asyncio.run(main())
```

### 自定义 Worker 配置

```python
from hotplex_client import HotPlexClient, WorkerType

async with HotPlexClient(
    url="ws://localhost:8888",
    worker_type=WorkerType.CLAUDE_CODE,
    config={
        "model": "claude-sonnet-4-6",
        "system_prompt": "You are a helpful coding assistant.",
        "allowed_tools": ["read_file", "write_file", "bash"],
        "max_turns": 20,
    },
) as client:
    ...
```
