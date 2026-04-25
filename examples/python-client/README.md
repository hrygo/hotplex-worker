# HotPlex Python Client

Python 客户端示例模块，演示如何通过 WebSocket 与 HotPlex Gateway 交互。

## 快速开始

### 前置要求

- Python 3.10+
- 运行中的 HotPlex Gateway（默认 `ws://localhost:8888`）

### 安装依赖

```bash
cd examples/python-client
pip install -r requirements.txt
```

### 运行示例

#### 快速上手（5 分钟）

```bash
python examples/quickstart.py
```

演示最基本的连接、发送输入和接收流式响应。

#### 完整功能示例

```bash
python examples/advanced.py
```

演示会话恢复、工具调用、权限请求、错误处理等完整功能。

## 架构设计

### 分层架构

```
┌──────────────────────────────────────┐
│   HotPlexClient (client.py)          │  ← 高层 API（会话管理）
│   - 会话初始化                        │
│   - 事件分发（回调注册）               │
│   - 状态机管理                        │
├──────────────────────────────────────┤
│   WebSocketTransport (transport.py)  │  ← 中层（连接管理）
│   - WebSocket 连接生命周期            │
│   - 消息队列                          │
│   - 基本错误处理                      │
├──────────────────────────────────────┤
│   Protocol (protocol.py)             │  ← 底层（消息编解码）
│   - NDJSON 序列化/反序列化            │
│   - Envelope 构造器                   │
│   - UUID 生成                         │
└──────────────────────────────────────┘
```

### 核心组件

- **`protocol.py`**: AEP v1 消息编解码（纯函数式）
- **`transport.py`**: WebSocket 连接生命周期
- **`client.py`**: 业务逻辑抽象（会话、事件分发）
- **`types.py`**: 类型定义（dataclass）
- **`exceptions.py`**: 异常层次

## API 文档

### HotPlexClient

#### 初始化

```python
from hotplex_client import HotPlexClient, WorkerType

# 方式 1: 上下文管理器（推荐）
async with HotPlexClient(
    url="ws://localhost:8888",
    worker_type=WorkerType.CLAUDE_CODE,
    auth_token="your-token",  # 可选
) as client:
    # 自动完成 init 握手
    print(f"Session: {client.session_id}")

# 方式 2: 手动管理
client = HotPlexClient(...)
await client.connect()
try:
    # 使用客户端
    pass
finally:
    await client.close()
```

#### 发送输入

```python
await client.send_input(
    content="Write a Python hello world",
    metadata={"language": "python"}  # 可选
)
```

#### 事件处理（装饰器风格）

```python
@client.on_message_delta
async def handle_delta(data: MessageDeltaData):
    """流式响应（实时打印）"""
    print(data.content, end="")

@client.on_done
async def handle_done(data: DoneData):
    """任务完成"""
    print(f"Done! Success: {data.success}")

@client.on_error
async def handle_error(data: ErrorData):
    """错误处理"""
    print(f"Error [{data.code}]: {data.message}")

@client.on_state_change
async def handle_state(data: StateData):
    """状态变化"""
    print(f"State: {data.state}")

@client.on_tool_call
async def handle_tool_call(data: ToolCallData):
    """工具调用"""
    result = execute_tool(data.name, data.input)
    await client.send_tool_result(
        tool_call_id=data.id,
        output=result,
    )

@client.on_permission_request
async def handle_permission(data: PermissionRequestData):
    """权限请求"""
    allowed = ask_user(data.tool_name)
    await client.send_permission_response(
        permission_id=data.id,
        allowed=allowed,
    )
```

### 支持的事件类型

| 事件类型 | 方向 | 数据类型 | 说明 |
|---------|------|---------|------|
| `init` | C→S | `InitData` | 初始化握手 |
| `init_ack` | S→C | `InitAckData` | 握手确认 |
| `input` | C→S | `InputData` | 用户输入 |
| `message.start` | S→C | `MessageStartData` | 流式消息开始 |
| `message.delta` | S→C | `MessageDeltaData` | 流式内容块 |
| `message.end` | S→C | `MessageEndData` | 流式消息结束 |
| `message` | S→C | `MessageData` | 完整消息（非流式） |
| `tool_call` | S→C | `ToolCallData` | 工具调用 |
| `tool_result` | C→S | `ToolResultData` | 工具结果 |
| `permission_request` | S→C | `PermissionRequestData` | 权限请求 |
| `permission_response` | C→S | `PermissionResponseData` | 权限响应 |
| `state` | S→C | `StateData` | 状态变化 |
| `done` | S→C | `DoneData` | 任务完成 |
| `error` | S→C | `ErrorData` | 错误 |
| `control` | S→C | `ControlData` | 服务器控制指令 |

完整协议文档：`docs/architecture/AEP-v1-Protocol.md`

## 错误处理

### 异常层次

```python
from hotplex_client.exceptions import (
    HotPlexError,
    SessionError,
    SessionTerminatedError,
    TransportError,
    ReconnectFailedError,
)

try:
    await client.send_input("...")
except SessionTerminatedError:
    # 会话已终止
    logger.warning("Session terminated")
except ReconnectFailedError as e:
    # 重连失败（e.attempts 次尝试后）
    logger.error(f"Failed after {e.attempts} retries")
except HotPlexError as e:
    # 通用 HotPlex 错误
    logger.error(f"HotPlex error: {e}")
```

### 错误码映射

| 错误码 | 异常类型 | 说明 |
|--------|---------|------|
| `SESSION_NOT_FOUND` | `SessionNotFoundError` | 会话不存在 |
| `SESSION_TERMINATED` | `SessionTerminatedError` | 会话已终止 |
| `SESSION_EXPIRED` | `SessionExpiredError` | 会话已过期 |
| `UNAUTHORIZED` | `UnauthorizedError` | 未授权 |

## 高级功能

### 会话恢复

```python
# 首次会话
async with HotPlexClient(...) as client:
    session_id = client.session_id
    await client.send_input("...")

# 恢复会话
async with HotPlexClient(session_id=session_id, ...) as client:
    await client.send_input("Continue...")
```

### 工具调用

```python
@client.on_tool_call
async def handle_tool_call(data: ToolCallData):
    # 执行工具
    result = await execute_tool(data.name, data.input)

    # 返回结果
    await client.send_tool_result(
        tool_call_id=data.id,
        output=result,
        error=None,  # 或错误消息
    )
```

### 权限请求

```python
@client.on_permission_request
async def handle_permission(data: PermissionRequestData):
    # 询问用户或自动批准
    allowed = ask_user(data.tool_name, data.description)

    await client.send_permission_response(
        permission_id=data.id,
        allowed=allowed,
        reason="用户批准" if allowed else "用户拒绝",
    )
```

### 状态监控

```python
@client.on_state_change
async def handle_state(data: StateData):
    print(f"State: {data.state}")

    if data.state == SessionState.IDLE:
        print("Worker is idle, waiting for input")
    elif data.state == SessionState.TERMINATED:
        print("Session terminated")
```

## 生产环境建议

### 1. 超时控制

```python
# 使用 asyncio.wait_for
try:
    await asyncio.wait_for(
        client.send_input("..."),
        timeout=30.0,
    )
except asyncio.TimeoutError:
    logger.error("Request timed out")
```

### 2. 错误重试

```python
import asyncio
from hotplex_client.exceptions import TransportError

async def send_with_retry(client, content: str, max_retries: int = 3):
    """带指数退避的重试"""
    for attempt in range(max_retries):
        try:
            await client.send_input(content)
            return
        except TransportError as e:
            if attempt == max_retries - 1:
                raise
            delay = 2 ** attempt  # 1s, 2s, 4s
            logger.warning(f"Retry {attempt + 1}/{max_retries} after {delay}s")
            await asyncio.sleep(delay)
```

### 3. 结构化日志

```python
import logging

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s"
)
```

### 4. 资源清理

```python
# 使用 async with 确保连接关闭
async with HotPlexClient(...) as client:
    # 使用客户端
    pass
# 自动调用 client.close()
```

## 与 TypeScript 客户端对比

| 特性 | Python | TypeScript |
|------|--------|-----------|
| 异步模式 | `async/await` | `async/await` |
| 类型系统 | dataclass + TypeVar | interface + generic |
| 事件处理 | 装饰器回调 | EventEmitter |
| 连接管理 | `websockets` | `ws` |
| 序列化 | NDJSON | NDJSON |
| 错误处理 | 自定义异常层次 | Error 子类 |

**Python 客户端独特优势：**
- 使用 `match/case` 处理事件类型（Python 3.10+）
- dataclass 自动生成 `__init__` 等方法
- StrEnum 提供字符串枚举（Python 3.11+，向后兼容）
- 异步上下文管理器（`async with`）自动资源清理

TypeScript 客户端：`examples/typescript-client/`

## 项目结构

```
examples/python-client/
├── hotplex_client/          # 可复用客户端库（~800 行）
│   ├── __init__.py          # 导出公共 API
│   ├── protocol.py          # AEP 编解码（~250 行）
│   ├── transport.py         # WebSocket 管理（~150 行）
│   ├── client.py            # 高层 API（~250 行）
│   ├── types.py             # 类型定义（~150 行）
│   └── exceptions.py        # 异常类（~50 行）
│
├── examples/
│   ├── quickstart.py        # 快速上手（~80 行）
│   └── advanced.py          # 完整示例（~300 行）
│
├── requirements.txt         # 依赖：websockets>=12.0
├── pyproject.toml           # Python 3.10+ 配置
└── README.md                # 本文档
```

## 开发

### 安装开发依赖

```bash
pip install -e ".[dev]"
```

### 运行测试

```bash
pytest tests/ -v --cov=hotplex_client
```

### 类型检查

```bash
mypy hotplex_client
```

### 代码格式化

```bash
black hotplex_client examples
```

## 常见问题

### Q: 连接失败怎么办？

**A:** 检查以下项：
1. Gateway 是否运行在指定 URL
2. 防火墙是否阻止 WebSocket 连接
3. 认证 token 是否有效

### Q: 如何调试消息流？

**A:** 启用 DEBUG 日志：
```python
import logging
logging.basicConfig(level=logging.DEBUG)
```

### Q: 如何处理大消息？

**A:** Transport 层已设置 `max_size=32MB`，应该足够大多数场景。

### Q: 可以同时连接多个 session 吗？

**A:** 可以，创建多个 `HotPlexClient` 实例即可。

## 许可证

Apache-2.0

## 相关链接

- **AEP v1 协议规范**：`docs/architecture/AEP-v1-Protocol.md`
- **架构设计文档**：`docs/superpowers/specs/2026-04-02-python-client-design.md`
- **TypeScript 客户端**：`examples/typescript-client/`
- **WebSocket 库文档**：https://websockets.readthedocs.io/
