import asyncio
import os
from hotplex_client import HotPlexClient, WorkerType

async def main():
    print("🚀 HotPlex Gateway - Quick Start\n")
    url = os.getenv("HOTPLEX_URL", "ws://localhost:8888")
    
    async with HotPlexClient(url=url, worker_type=WorkerType.CLAUDE_CODE) as client:
        print(f"Connected | Session: {client.session_id}\n")

        # Register delta handler for streaming output
        @client.on("message.delta")
        async def on_delta(data):
            print(data.content, end="", flush=True)

        # Send input and wait for result
        task = "Briefly introduce yourself."
        print(f"> {task}")
        
        await client.send_input(task)
        done_data = await client.wait_for_done()
        
        print(f"\n\n✅ Task completed: success={done_data.success}")
        if done_data.stats:
            print(f"   Duration: {done_data.stats.duration_ms}ms")
            print(f"   Tokens:   {done_data.stats.total_tokens}")

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
