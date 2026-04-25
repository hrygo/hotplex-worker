#!/usr/bin/env python3
"""
HotPlex 快速上手示例

演示最基本的功能：
1. 连接到 gateway
2. 发送用户输入
3. 接收流式响应（message.delta）
4. 处理完成事件
5. 基本错误处理

运行方式：
    cd examples/python-client
    pip install -r requirements.txt
    python examples/quickstart.py
"""

import asyncio
import sys

# 添加父目录到 path 以导入 hotplex_client
sys.path.insert(0, "..")

from hotplex_client import (
    HotPlexClient,
    MessageDeltaData,
    DoneData,
    ErrorData,
    WorkerType,
)


async def main():
    """主函数：演示基本的 HotPlex 交互流程"""

    # 1. 创建客户端（使用 async with 自动管理连接）
    async with HotPlexClient(
        url="ws://localhost:8888",
        worker_type=WorkerType.CLAUDE_CODE,
    ) as client:

        print(f"✓ 已连接！Session: {client.session_id}\n")

        # 2. 注册事件处理器

        @client.on_message_delta
        async def on_delta(data: MessageDeltaData):
            """实时打印 AI 响应（流式）"""
            print(data.content, end="", flush=True)

        @client.on_done
        async def on_done(data: DoneData):
            """任务完成"""
            print(f"\n\n✓ 完成！成功: {data.success}")
            if data.stats:
                duration = data.stats.get("duration_ms", 0)
                tokens = data.stats.get("total_tokens", 0)
                print(f"  耗时: {duration}ms")
                print(f"  Tokens: {tokens}")

        @client.on_error
        async def on_error(data: ErrorData):
            """错误处理"""
            print(f"\n✗ 错误 [{data.code}]: {data.message}")
            if data.details:
                print(f"  详情: {data.details}")

        # 3. 发送输入
        user_input = "Write a hello world in Python"
        print(f"用户: {user_input}")
        print("助手: ", end="", flush=True)

        await client.send_input(user_input)

        # 4. 等待任务完成
        # 在实际应用中应该使用更智能的等待机制（如 Event + 超时）
        await asyncio.sleep(60)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n\n⚠ 用户中断")
    except Exception as e:
        print(f"\n✗ 发生错误: {e}")
        import traceback
        traceback.print_exc()
