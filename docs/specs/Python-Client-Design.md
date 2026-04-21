---
type: spec
tags:
  - project/HotPlex
  - client/python
  - sdk
date: 2026-04-02
status: implemented
progress: 100
completion_date: 2026-04-02
estimated_hours: 16
---

# Python Client 示例模块设计文档

**日期：** 2026-04-02
**状态：** 已批准
**作者：** Claude Sonnet 4.6

## 概述

为 HotPlex Worker Gateway 创建 Python 客户端示例模块，帮助第三方开发者快速集成 HotPlex Worker 到他们的 Python 应用中。

### 目标

- **主要用户：** 第三方开发者（需要集成 HotPlex Worker）
- **使用场景：** 学习 AEP v1 协议、快速原型开发、生产环境集成参考
- **成功标准：**
  - 开发者能在 5 分钟内运行 quickstart.py
  - 代码清晰易懂，有完整的类型提示
  - 架构分层合理，易于定制和扩展

---

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
│   - 自动重连（指数退避）               │
│   - 心跳管理（ping/pong）             │
│   - 消息队列                          │
├──────────────────────────────────────┤
│   Protocol (protocol.py)             │  ← 底层（消息编解码）
│   - NDJSON 序列化/反序列化            │
│   - Envelope 构造器                   │
│   - UUID 生成                         │
└──────────────────────────────────────┘
```

### 设计原则

1. **关注点分离**：每层专注单一职责
2. **异步优先**：全 async/await，无同步包装
3. **类型安全**：Python 3.10+ 现代类型提示，泛型保留事件数据类型
4. **事件驱动**：通过回调函数处理服务器事件，避免轮询
5. **错误透明**：自定义异常层次，清晰区分协议错误/网络错误/业务错误

---

## 核心组件设计

### 1. protocol.py - AEP 消息编解码（~200 行）

**职责：**
- NDJSON 序列化/反序列化
- Envelope 构造器（init, input, ping, control 等）
- UUID 生成（符合 AEP 规范）

**核心函数：**
```python
# 编解码
def encode_envelope(env: Envelope) -> str:
    """序列化为 NDJSON（单行 JSON + \n）"""

def decode_envelope(line: str) -> Envelope:
    """从 NDJSON 反序列化，自动检测事件类型"""

# UUID 生成
def generate_event_id() -> str:
    """evt_<uuid>"""

def generate_session_id() -> str:
    """sess_<uuid>"""

# 消息构造器
def create_init_envelope(
    worker_type: WorkerType,
    session_id: str | None = None,
    auth_token: str | None = None,
    config: InitConfig | None = None,
) -> Envelope[InitData]:
    """创建 init 握手消息"""

def create_input_envelope(
    session_id: str,
    content: str,
    metadata: dict[str, Any] | None = None,
) -> Envelope[InputData]:
    """创建用户输入消息"""

def create_ping_envelope(session_id: str) -> Envelope[None]:
    """创建心跳 ping"""

def create_control_envelope(
    session_id: str,
    action: ControlAction,
) -> Envelope[ControlData]:
    """创建控制消息（terminate/delete）"""
```

**设计要点：**
- 使用 `@dataclass` 定义所有数据类型，自动支持序列化
- `decode_envelope()` 使用 `match/case` 根据事件类型动态反序列化
- NDJSON 格式：每行一个 JSON，以 `\n` 结尾
- 完全类型安全：`Envelope[T]` 泛型保留事件数据类型

---

### 2. transport.py - WebSocket 连接管理（~250 行）

**职责：**
- WebSocket 连接生命周期（连接、断连、重连）
- 心跳管理（ping/pong）
- 消息队列（异步缓冲）
- 连接状态管理

**核心类：**
```python
class WebSocketTransport:
    """底层 WebSocket 连接管理"""

    async def connect(
        self,
        url: str,
        auth_token: str | None = None,
    ) -> None:
        """建立 WebSocket 连接"""

    async def send(self, envelope: Envelope[Any]) -> None:
        """发送 Envelope（自动编码为 NDJSON）"""

    async def receive(self) -> Envelope[Any]:
        """接收 Envelope（阻塞直到收到消息）"""

    async def close(self) -> None:
        """优雅关闭连接"""

    async def __aenter__(self):
        """支持 async with 上下文管理"""

    async def __aexit__(self, *args):
        """自动清理资源"""
```

**连接状态机：**
```python
class ConnectionState(StrEnum):
    DISCONNECTED = "disconnected"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    RECONNECTING = "reconnecting"
    CLOSED = "closed"
```

**自动重连机制：**
- 指数退避：1s → 2s → 4s → 8s → ... → 60s（最大）
- 触发条件：连接断开、收到 `control.reconnect`
- 最多重试 5 次，之后抛出 `ReconnectFailedError`

**心跳管理：**
- 每 54 秒发送 ping
- 60 秒未收到 pong 则判定连接失效
- 最多允许错过 3 次 pong

**消息队列：**
- 使用 `asyncio.Queue` 缓冲接收的消息
- 后台任务持续读取 WebSocket 并放入队列
- `receive()` 从队列获取，不阻塞 I/O

---

### 3. client.py - 高层客户端 API（~300 行）

**职责：**
- 会话管理（初始化握手、状态机）
- 事件分发（回调注册）
- 用户友好的 API

**核心类：**
```python
class HotPlexClient:
    """高层客户端 API"""

    def __init__(
        self,
        url: str = "ws://localhost:8888",
        worker_type: WorkerType = WorkerType.CLAUDE_CODE,
        auth_token: str | None = None,
        config: InitConfig | None = None,
    ):
        """初始化客户端配置"""

    async def connect(self) -> str:
        """建立连接并完成 init 握手，返回 session_id"""

    async def send_input(
        self,
        content: str,
        metadata: dict[str, Any] | None = None,
    ) -> None:
        """发送用户输入到 worker"""

    async def terminate(self) -> None:
        """终止会话"""

    async def close(self) -> None:
        """关闭连接"""

    # 事件回调注册（装饰器风格）
    def on_message_delta(self, callback: Callable[[MessageDeltaData], None]):
        """注册 message.delta 事件回调"""

    def on_error(self, callback: Callable[[ErrorData], None]):
        """注册 error 事件回调"""

    def on_state_change(self, callback: Callable[[StateData], None]):
        """注册 state 事件回调"""

    def on_done(self, callback: Callable[[DoneData], None]):
        """注册 done 事件回调"""

    # 上下文管理
    async def __aenter__(self) -> "HotPlexClient":
        """自动连接"""

    async def __aexit__(self, *args):
        """自动清理"""
```

**事件处理流程：**
```python
# 后台任务：持续接收消息并分发到回调
async def _event_loop(self):
    async for envelope in self._transport:
        match envelope.event.type:
            case "message.delta":
                await self._callbacks["message_delta"](envelope.event.data)
            case "error":
                await self._callbacks["error"](envelope.event.data)
            case "state":
                await self._callbacks["state"](envelope.event.data)
            case "done":
                await self._callbacks["done"](envelope.event.data)
            # ... 其他事件类型
```

**设计要点：**
- **回调异步**：所有回调都是 `async` 函数，支持异步操作
- **事件循环自动管理**：连接后自动启动后台任务接收消息
- **类型安全**：回调参数类型由事件类型决定
- **会话状态透明**：用户无需关心状态机，由客户端管理

---

### 4. types.py - 数据模型（~50 行）

**核心类型：**
```python
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any, Generic, TypeVar

# 枚举类型
class WorkerType(StrEnum):
    CLAUDE_CODE = "claude_code"
    OPENCODE_SERVER = "opencode_server"
    PI_MONO = "pi-mono"

class SessionState(StrEnum):
    CREATED = "created"
    RUNNING = "running"
    IDLE = "idle"
    TERMINATED = "terminated"
    DELETED = "deleted"

class Priority(StrEnum):
    CONTROL = "control"
    DATA = "data"

# 核心协议类型
T = TypeVar("T")

@dataclass
class Event(Generic[T]):
    """事件容器"""
    type: str
    data: T

@dataclass
class Envelope(Generic[T]):
    """AEP v1 消息信封"""
    version: str = "aep/v1"
    id: str = ""
    seq: int = 0
    priority: Priority | None = None
    session_id: str = ""
    timestamp: int = 0
    event: Event[T] = field(default_factory=lambda: Event(type="", data=None))

# 事件数据类型
@dataclass
class InitData:
    version: str
    worker_type: WorkerType
    session_id: str | None = None
    auth: dict[str, Any] | None = None
    config: dict[str, Any] | None = None

@dataclass
class InputData:
    content: str
    metadata: dict[str, Any] | None = None

@dataclass
class MessageDeltaData:
    message_id: str
    content: str

@dataclass
class StateData:
    state: SessionState
    message: str | None = None

@dataclass
class DoneData:
    success: bool
    stats: dict[str, Any] | None = None
    dropped: bool | None = None

@dataclass
class ErrorData:
    code: str
    message: str
    event_id: str | None = None
    details: dict[str, Any] | None = None

@dataclass
class ControlData:
    action: ControlAction
    reason: str | None = None
    delay_ms: int | None = None
    recoverable: bool | None = None
    suggestion: dict[str, Any] | None = None
```

**设计要点：**
- 使用 `@dataclass` 自动生成 `__init__`、`__repr__` 等方法
- `StrEnum` (Python 3.10+) 提供字符串枚举，兼容 JSON 序列化
- 泛型 `Envelope[T]` 保留事件数据类型信息
- 字段名使用 `snake_case`（Python 惯例），序列化时自动转为 `snake_case`（与 Go 一致）

---

### 5. exceptions.py - 异常层次（~30 行）

**异常层次：**
```python
class HotPlexError(Exception):
    """所有 HotPlex 客户端错误的基类"""
    pass

# ============================================================================
# 协议错误（应用层）
# ============================================================================

class ProtocolError(HotPlexError):
    """AEP 协议错误（编解码、验证失败）"""
    pass

class InvalidMessageError(ProtocolError):
    """无效的消息格式"""
    pass

class VersionMismatchError(ProtocolError):
    """协议版本不匹配"""
    def __init__(self, expected: str, actual: str):
        self.expected = expected
        self.actual = actual
        super().__init__(f"Version mismatch: expected {expected}, got {actual}")

# ============================================================================
# 会话错误（业务层）
# ============================================================================

class SessionError(HotPlexError):
    """会话相关错误"""
    pass

class SessionNotFoundError(SessionError):
    """会话不存在"""
    pass

class SessionTerminatedError(SessionError):
    """会话已终止"""
    pass

class SessionExpiredError(SessionError):
    """会话已过期"""
    pass

# ============================================================================
# 网络错误（传输层）
# ============================================================================

class TransportError(HotPlexError):
    """WebSocket 传输错误"""
    pass

class ConnectionLostError(TransportError):
    """连接丢失"""
    pass

class ReconnectFailedError(TransportError):
    """重连失败"""
    def __init__(self, attempts: int):
        self.attempts = attempts
        super().__init__(f"Reconnect failed after {attempts} attempts")

class HeartbeatTimeoutError(TransportError):
    """心跳超时"""
    pass

# ============================================================================
# 认证错误
# ============================================================================

class AuthError(HotPlexError):
    """认证失败"""
    pass

class UnauthorizedError(AuthError):
    """未授权（token 无效或过期）"""
    pass
```

**异常层次图：**
```
HotPlexError
├── ProtocolError (协议层)
│   ├── InvalidMessageError
│   └── VersionMismatchError
├── SessionError (业务层)
│   ├── SessionNotFoundError
│   ├── SessionTerminatedError
│   └── SessionExpiredError
├── TransportError (网络层)
│   ├── ConnectionLostError
│   ├── ReconnectFailedError
│   └── HeartbeatTimeoutError
└── AuthError (认证层)
    └── UnauthorizedError
```

---

## 示例代码设计

### 1. quickstart.py - 快速上手（~150 行）

**目标：** 5 分钟内理解核心流程

**功能范围：**
- 连接到 gateway
- 发送用户输入
- 接收流式响应（message.delta）
- 处理完成事件
- 基本错误处理

**代码结构：**
```python
"""
HotPlex Worker 快速上手示例

演示最基本的功能：
1. 连接到 gateway
2. 发送用户输入
3. 接收流式响应（message.delta）
4. 处理完成事件
"""

import asyncio
from hotplex_client import HotPlexClient

async def main():
    # 1. 创建客户端（自动连接）
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type="claude_code",
    ) as client:

        print(f"✓ Connected! Session: {client.session_id}")

        # 2. 注册事件处理器
        @client.on_message_delta
        async def on_delta(data):
            """实时打印 AI 响应"""
            print(data.content, end="", flush=True)

        @client.on_done
        async def on_done(data):
            """任务完成"""
            print(f"\n\n✓ Done! Success: {data.success}")
            if data.stats:
                print(f"  Duration: {data.stats.duration_ms}ms")
                print(f"  Tokens: {data.stats.total_tokens}")

        @client.on_error
        async def on_error(data):
            """错误处理"""
            print(f"\n✗ Error [{data.code}]: {data.message}")

        # 3. 发送输入
        user_input = "Write a hello world in Python"
        print(f"User: {user_input}")
        print("Assistant: ", end="")

        await client.send_input(user_input)

        # 4. 等待任务完成
        await asyncio.sleep(60)  # 简单超时

if __name__ == "__main__":
    asyncio.run(main())
```

**特点：**
- 单文件，容易理解
- 最小化错误处理
- 专注核心流程（连接 → 发送 → 接收 → 完成）
- 没有重连、心跳等复杂逻辑（由客户端库自动处理）

---

### 2. advanced.py - 完整功能（~400 行）

**目标：** 展示所有高级功能和最佳实践

**功能范围：**
- 完整错误处理和重连
- 会话恢复
- 权限请求处理（permission_request/response）
- 工具调用（tool_call/tool_result）
- 完整的状态机处理
- 心跳监控
- 结构化日志
- 优雅关闭

**代码结构：**
```python
"""
HotPlex Worker 高级示例

演示完整功能：
1. 完整错误处理和重连
2. 会话恢复
3. 权限请求处理
4. 工具调用
5. 完整状态机管理
6. 心跳监控
7. 结构化日志
8. 优雅关闭
"""

import asyncio
import logging
from typing import Any
from hotplex_client import (
    HotPlexClient,
    MessageDeltaData,
    ToolCallData,
    PermissionRequestData,
    StateData,
    DoneData,
    ErrorData,
    ControlData,
)

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s"
)
logger = logging.getLogger(__name__)

class HotPlexWorkerSession:
    """封装完整会话管理逻辑"""

    def __init__(self, url: str, session_id: str | None = None):
        self.url = url
        self.session_id = session_id
        self.client: HotPlexClient | None = None
        self._done_event = asyncio.Event()

    async def run(self, user_input: str) -> bool:
        """执行完整会话，返回是否成功"""
        try:
            async with HotPlexClient(
                url=self.url,
                session_id=self.session_id,  # 支持恢复
            ) as client:
                self.client = client
                self.session_id = client.session_id

                # 注册所有事件处理器
                self._setup_handlers()

                # 发送输入
                logger.info(f"Sending input: {user_input[:50]}...")
                await client.send_input(user_input)

                # 等待完成（带超时）
                await asyncio.wait_for(
                    self._done_event.wait(),
                    timeout=300.0  # 5 分钟超时
                )

                return True

        except asyncio.TimeoutError:
            logger.error("Session timed out after 5 minutes")
            return False
        except Exception as e:
            logger.exception(f"Session failed: {e}")
            return False

    def _setup_handlers(self):
        """注册所有事件处理器"""

        @self.client.on_message_delta
        async def on_delta(data: MessageDeltaData):
            print(data.content, end="", flush=True)

        @self.client.on_tool_call
        async def on_tool_call(data: ToolCallData):
            logger.info(f"Tool call: {data.name}")
            # 实际应用中应该执行工具并返回结果

        @self.client.on_permission_request
        async def on_permission(data: PermissionRequestData):
            logger.info(f"Permission request: {data.tool_name}")
            # 示例：自动批准所有权限请求（生产环境应该询问用户）
            await self.client.send_permission_response(
                permission_id=data.id,
                allowed=True,
            )

        @self.client.on_state_change
        async def on_state(data: StateData):
            logger.info(f"State changed: {data.state}")
            if data.message:
                logger.info(f"  Message: {data.message}")

        @self.client.on_done
        async def on_done(data: DoneData):
            print(f"\n\n✓ Done!")
            if data.stats:
                self._print_stats(data.stats)
            self._done_event.set()

        @self.client.on_error
        async def on_error(data: ErrorData):
            logger.error(f"Error [{data.code}]: {data.message}")
            if data.details:
                logger.error(f"  Details: {data.details}")

        @self.client.on_control
        async def on_control(data: ControlData):
            logger.warning(f"Control: {data.action} - {data.reason}")
            # 处理服务器控制指令（如 reconnect, terminate）

    def _print_stats(self, stats: dict[str, Any]):
        """打印执行统计"""
        print(f"\nStats:")
        print(f"  Duration: {stats.get('duration_ms', 0)}ms")
        print(f"  Tokens: {stats.get('total_tokens', 0)}")
        if 'cost_usd' in stats:
            print(f"  Cost: ${stats['cost_usd']:.4f}")

async def main():
    """主入口"""
    session = HotPlexWorkerSession(url="ws://localhost:8888")

    # 示例 1: 基本对话
    success = await session.run("Create a Python function to calculate fibonacci")

    if success:
        print(f"\n✓ Session completed successfully")
        print(f"  Session ID: {session.session_id}")

        # 示例 2: 恢复会话
        print("\n--- Resuming session ---")
        session2 = HotPlexWorkerSession(
            url="ws://localhost:8888",
            session_id=session.session_id,
        )
        await session2.run("Now add error handling to that function")
    else:
        print("\n✗ Session failed")

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logger.info("\nInterrupted by user")
```

**高级功能：**
- **会话恢复**：通过 `session_id` 恢复之前的会话
- **工具调用**：演示 `tool_call` 事件处理
- **权限请求**：自动批准（可定制）
- **完整状态机**：监听所有状态变化
- **结构化日志**：使用 logging 模块
- **超时控制**：5 分钟超时保护
- **优雅关闭**：Ctrl+C 优雅退出

---

## 文件结构

```
examples/python-client/
├── hotplex_client/          # 可复用客户端库（~800 行）
│   ├── __init__.py          # 导出公共 API
│   ├── protocol.py          # AEP 编解码（~200 行）
│   ├── transport.py         # WebSocket 管理（~250 行）
│   ├── client.py            # 高层 API（~300 行）
│   ├── types.py             # 类型定义（~50 行）
│   └── exceptions.py        # 异常类（~30 行）
│
├── examples/
│   ├── quickstart.py        # 快速上手（~150 行）
│   └── advanced.py          # 完整示例（~400 行）
│
├── tests/                   # 测试（可选）
│   ├── test_protocol.py
│   ├── test_transport.py
│   └── test_client.py
│
├── requirements.txt         # websockets>=12.0
├── pyproject.toml           # Python 3.10+
└── README.md                # 使用指南 + API 文档
```

---

## 技术栈

- **Python 版本：** 3.10+（现代类型提示、match/case、StrEnum）
- **WebSocket 库：** websockets 12.0+（纯异步实现）
- **类型系统：** dataclass + TypeVar + Generic
- **异步框架：** asyncio（标准库）
- **序列化：** NDJSON（NDJSON = newline-delimited JSON）
- **日志：** logging 模块（标准库）

---

## 依赖管理

### requirements.txt
```
websockets>=12.0
```

### pyproject.toml
```toml
[project]
name = "hotplex-client"
version = "1.0.0"
description = "Python client example for HotPlex Worker Gateway"
requires-python = ">=3.10"
dependencies = [
    "websockets>=12.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0",
    "pytest-asyncio>=0.21",
    "pytest-cov>=4.0",
    "mypy>=1.0",
    "black>=23.0",
]

[build-system]
requires = ["setuptools>=68.0"]
build-backend = "setuptools.build_meta"
```

---

## 测试策略（可选）

### 测试范围

- **test_protocol.py**：
  - NDJSON 序列化/反序列化
  - 所有事件类型的编解码
  - UUID 生成格式验证

- **test_transport.py**：
  - 连接/断连流程（使用 mock WebSocket）
  - 重连逻辑（指数退避）
  - 心跳超时处理
  - 消息队列缓冲

- **test_client.py**：
  - 完整会话流程
  - 事件分发机制
  - 错误处理
  - 会话恢复

### 运行测试
```bash
pytest tests/ -v --cov=hotplex_client
```

**注意：** 测试为可选，初期可先实现核心功能，后续补充测试。

---

## README.md 结构

```markdown
# HotPlex Worker Python Client

## 快速开始
- 安装依赖
- 运行示例（quickstart, advanced）

## 架构设计
- 分层架构图
- 核心组件说明

## API 文档
- HotPlexClient 初始化
- 发送输入
- 事件处理（装饰器回调）

## 协议参考
- AEP v1 事件类型表格
- 完整协议文档链接

## 错误处理
- 异常层次
- 错误码映射表

## 高级功能
- 会话恢复
- 工具调用
- 权限请求

## 生产环境建议
- 超时控制
- 错误重试
- 日志记录
- 监控指标
- 资源清理

## 与 TypeScript 客户端对比

## 许可证
```

---

## 关键决策记录

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 目标用户 | 第三方开发者 | 提供易用的集成体验，而非内部测试工具 |
| 功能范围 | 渐进式示例 | quickstart（5分钟）+ advanced（完整功能） |
| 异步模式 | 纯异步 | 符合现代 Python 最佳实践，非阻塞 |
| 代码组织 | 模块化结构 | 清晰职责划分，易于维护和扩展 |
| 架构方案 | 分层客户端库 | protocol/transport/client 三层抽象 |
| WebSocket 库 | websockets | 专注 WebSocket，API 现代，性能优秀 |
| Python 版本 | 3.10+ | 现代类型提示、match/case、StrEnum |
| 发布方式 | 本地包（无 PyPI） | 避免长期维护负担，用户可定制 |

---

## 实现优先级

### Phase 1: 核心功能（MVP）
1. `types.py` - 数据模型定义
2. `exceptions.py` - 异常类
3. `protocol.py` - 消息编解码
4. `transport.py` - WebSocket 连接管理（无重连）
5. `client.py` - 基本客户端 API
6. `quickstart.py` - 快速上手示例

### Phase 2: 完整功能
1. `transport.py` - 添加自动重连、心跳
2. `client.py` - 添加工具调用、权限请求处理
3. `advanced.py` - 完整功能示例
4. `README.md` - 完整文档

### Phase 3: 质量保证（可选）
1. 单元测试
2. 类型检查（mypy）
3. 代码格式化（black）
4. 集成测试（真实 gateway）

---

## 风险和缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| websockets 库 API 变更 | 中 | 锁定版本 >=12.0，关注 changelog |
| AEP 协议更新 | 高 | 版本协商机制，向后兼容 |
| 用户环境 Python < 3.10 | 低 | pyproject.toml 明确要求 >=3.10 |
| 缺少真实环境测试 | 高 | 提供 advanced.py，包含完整错误处理 |
| 依赖库安全漏洞 | 中 | 定期更新依赖，使用 pip-audit |

---

## 未来扩展

1. **同步 API 包装**：提供同步版本的 HotPlexClient（基于 asyncio.run）
2. **Admin API 客户端**：集成 HTTP 调用（使用 httpx）
3. **更多 worker 类型**：支持/Server、Pi-mono
4. **监控集成**：OpenTelemetry tracing/metrics
5. **连接池**：多 session 并发管理
6. **PyPI 发布**：如果社区需求强烈，可发布到 PyPI

---

## 参考文档

- **AEP v1 协议规范**：`docs/architecture/AEP-v1-Protocol.md`
- **TypeScript 客户端**：`examples/typescript-client/`
- **WebSocket 库文档**：https://websockets.readthedocs.io/
- **Python asyncio 文档**：https://docs.python.org/3/library/asyncio.html

---

## 附录：与 TypeScript 客户端对比

| 特性 | Python | TypeScript |
|------|--------|-----------|
| 异步模式 | `async/await` | `async/await` |
| 类型系统 | dataclass + TypeVar | interface + generic |
| 事件处理 | 装饰器回调 | EventEmitter |
| 连接管理 | `websockets` | `ws` |
| 序列化 | NDJSON | NDJSON |
| 错误处理 | 自定义异常层次 | Error 子类 |
| 心跳 | 内置（transport 层） | 内置 |
| 重连 | 内置（transport 层） | 内置 |

**Python 客户端独特优势：**
- 使用 `match/case` 处理事件类型（Python 3.10+）
- dataclass 自动生成 `__init__` 等方法
- StrEnum 提供字符串枚举（Python 3.11+，向后兼容）
- 异步上下文管理器（`async with`）自动资源清理

**TypeScript 客户端独特优势：**
- EventEmitter 模式更灵活（多监听器）
- 更成熟的生态（npm 包管理）
- 前端/Node.js 通用

---

**文档结束**
