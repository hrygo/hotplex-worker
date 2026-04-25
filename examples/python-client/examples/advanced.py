#!/usr/bin/env python3
"""
HotPlex 高级示例

演示完整功能：
1. 完整错误处理和重连
2. 会话恢复
3. 权限请求处理
4. 工具调用
5. 完整状态机管理
6. 心跳监控
7. 结构化日志
8. 优雅关闭

运行方式：
    cd examples/python-client
    pip install -r requirements.txt
    python examples/advanced.py
"""

import asyncio
import logging
import sys
from typing import Any

# 添加父目录到 path 以导入 hotplex_client
sys.path.insert(0, "..")

from hotplex_client import (
    HotPlexClient,
    MessageDeltaData,
    MessageStartData,
    MessageEndData,
    ToolCallData,
    PermissionRequestData,
    StateData,
    DoneData,
    ErrorData,
    ControlData,
    SessionState,
    WorkerType,
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
        self._current_message_id: str | None = None
        self._message_count = 0

    async def run(self, user_input: str, timeout: float = 300.0) -> bool:
        """
        执行完整会话

        Args:
            user_input: 用户输入
            timeout: 超时时间（秒）

        Returns:
            是否成功完成
        """
        try:
            # 创建客户端（支持会话恢复）
            self.client = HotPlexClient(
                url=self.url,
                worker_type=WorkerType.CLAUDE_CODE,
                session_id=self.session_id,
            )

            async with self.client:
                self.session_id = self.client.session_id
                logger.info(f"会话已连接: {self.session_id}")

                # 注册所有事件处理器
                self._setup_handlers()

                # 发送输入
                logger.info(f"发送输入: {user_input[:50]}...")
                await self.client.send_input(user_input)

                # 等待完成（带超时）
                try:
                    await asyncio.wait_for(
                        self._done_event.wait(),
                        timeout=timeout
                    )
                    return True
                except asyncio.TimeoutError:
                    logger.error(f"会话超时（{timeout}秒）")
                    return False

        except Exception as e:
            logger.exception(f"会话失败: {e}")
            return False

    def _setup_handlers(self):
        """注册所有事件处理器"""

        @self.client.on_message_start
        async def on_message_start(data: MessageStartData):
            """消息开始（流式）"""
            self._current_message_id = data.id
            self._message_count += 1
            logger.debug(f"消息 #{self._message_count} 开始: {data.id}")

        @self.client.on_message_delta
        async def on_delta(data: MessageDeltaData):
            """消息增量（流式内容）"""
            print(data.content, end="", flush=True)

        @self.client.on_message_end
        async def on_message_end(data: MessageEndData):
            """消息结束（流式）"""
            print()  # 换行
            logger.debug(f"消息结束: {data.message_id}")
            self._current_message_id = None

        @self.client.on_tool_call
        async def on_tool_call(data: ToolCallData):
            """工具调用"""
            logger.info(f"工具调用: {data.name} (id: {data.id})")
            logger.debug(f"  输入: {data.input}")

            # 示例：模拟工具执行
            # 在实际应用中，这里应该执行真实的工具逻辑
            result = await self._execute_tool(data.name, data.input)

            # 发送工具结果
            await self.client.send_tool_result(
                tool_call_id=data.id,
                output=result,
            )
            logger.info(f"工具结果已发送: {data.id}")

        @self.client.on_permission_request
        async def on_permission(data: PermissionRequestData):
            """权限请求"""
            logger.warning(f"权限请求: {data.tool_name}")
            logger.info(f"  描述: {data.description}")
            logger.info(f"  参数: {data.args}")

            # 示例：自动批准所有权限请求
            # 在生产环境中应该询问用户
            allowed = await self._ask_permission(data)

            await self.client.send_permission_response(
                permission_id=data.id,
                allowed=allowed,
                reason="用户批准" if allowed else "用户拒绝",
            )
            logger.info(f"权限响应已发送: {allowed}")

        @self.client.on_state_change
        async def on_state(data: StateData):
            """状态变化"""
            logger.info(f"状态变化: {data.state}")
            if data.message:
                logger.info(f"  消息: {data.message}")

            # 检查是否为终止状态
            if data.state in (SessionState.TERMINATED, SessionState.DELETED):
                logger.warning("会话已终止")

        @self.client.on_done
        async def on_done(data: DoneData):
            """任务完成"""
            print(f"\n\n{'✓' if data.success else '✗'} 完成！")
            if data.stats:
                self._print_stats(data.stats)
            if data.dropped:
                logger.warning("部分消息因背压被丢弃")

            self._done_event.set()

        @self.client.on_error
        async def on_error(data: ErrorData):
            """错误处理"""
            logger.error(f"错误 [{data.code}]: {data.message}")
            if data.event_id:
                logger.error(f"  事件 ID: {data.event_id}")
            if data.details:
                logger.error(f"  详情: {data.details}")

            # 某些错误可能触发 done
            if data.code in ("SESSION_TERMINATED", "SESSION_EXPIRED"):
                self._done_event.set()

        @self.client.on_control
        async def on_control(data: ControlData):
            """服务器控制指令"""
            logger.warning(f"控制指令: {data.action}")
            logger.warning(f"  原因: {data.reason}")

            if data.recoverable is not None:
                logger.info(f"  可恢复: {data.recoverable}")

            # 处理不同的控制动作
            if data.action == "reconnect":
                logger.info("服务器要求重连")
                # transport 层会自动处理重连
            elif data.action == "terminate":
                logger.warning("服务器要求终止会话")
                self._done_event.set()

    async def _execute_tool(self, name: str, input: dict[str, Any]) -> Any:
        """
        执行工具（示例实现）

        在实际应用中，这里应该：
        1. 根据 tool name 调用对应的工具函数
        2. 处理工具执行错误
        3. 返回符合工具 schema 的结果
        """
        # 模拟工具执行延迟
        await asyncio.sleep(0.1)

        # 示例：返回模拟结果
        return {
            "status": "simulated",
            "tool": name,
            "input": input,
        }

    async def _ask_permission(self, data: PermissionRequestData) -> bool:
        """
        询问用户权限（示例：自动批准）

        在生产环境中应该：
        1. 显示权限请求详情给用户
        2. 等待用户确认/拒绝
        3. 返回用户决定
        """
        # 示例：自动批准
        logger.info(f"自动批准权限请求: {data.tool_name}")
        return True

    def _print_stats(self, stats: dict[str, Any]):
        """打印执行统计"""
        print("\n执行统计:")
        print(f"  耗时: {stats.get('duration_ms', 0)}ms")
        print(f"  消息数: {self._message_count}")

        if "total_tokens" in stats:
            print(f"  Tokens: {stats['total_tokens']}")
            if "input_tokens" in stats:
                print(f"    输入: {stats['input_tokens']}")
            if "output_tokens" in stats:
                print(f"    输出: {stats['output_tokens']}")

        if "cost_usd" in stats:
            print(f"  成本: ${stats['cost_usd']:.4f}")

        if "model" in stats:
            print(f"  模型: {stats['model']}")


async def demo_basic_conversation():
    """示例 1: 基本对话"""
    print("=" * 60)
    print("示例 1: 基本对话")
    print("=" * 60)

    session = HotPlexWorkerSession(url="ws://localhost:8888")

    success = await session.run(
        user_input="Create a Python function to calculate fibonacci numbers",
        timeout=120.0,
    )

    if success:
        print(f"\n✓ 会话完成")
        print(f"  Session ID: {session.session_id}")
        return session.session_id
    else:
        print("\n✗ 会话失败")
        return None


async def demo_session_resume(session_id: str):
    """示例 2: 恢复会话"""
    print("\n" + "=" * 60)
    print("示例 2: 恢复会话")
    print("=" * 60)

    session = HotPlexWorkerSession(
        url="ws://localhost:8888",
        session_id=session_id,  # 恢复之前的会话
    )

    success = await session.run(
        user_input="Now add error handling to that function",
        timeout=120.0,
    )

    if success:
        print(f"\n✓ 会话已恢复并完成")
    else:
        print("\n✗ 会话恢复失败")


async def main():
    """主入口"""
    try:
        # 示例 1: 基本对话
        session_id = await demo_basic_conversation()

        if session_id:
            # 示例 2: 恢复会话（可选）
            await demo_session_resume(session_id)

        print("\n" + "=" * 60)
        print("所有示例完成！")
        print("=" * 60)

    except KeyboardInterrupt:
        logger.warning("\n用户中断")
    except Exception as e:
        logger.exception(f"示例失败: {e}")


if __name__ == "__main__":
    asyncio.run(main())
