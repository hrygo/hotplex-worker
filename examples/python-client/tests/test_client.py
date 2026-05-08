import asyncio
import pytest
from unittest.mock import AsyncMock, patch, MagicMock
from hotplex_client import HotPlexClient, WorkerType
from hotplex_client.types import Envelope, Event, DoneData, MessageDeltaData

@pytest.mark.asyncio
async def test_client_callback_registration():
    client = HotPlexClient()
    
    called = False
    @client.on("message.delta")
    async def handle_delta(data: MessageDeltaData):
        nonlocal called
        called = True
        assert data["content"] == "hello"

    # Simulate receiving an event
    env = Envelope(
        event=Event(type="message.delta", data={"content": "hello", "message_id": "1"})
    )
    await client._dispatch_event(env)
    assert called is True

@pytest.mark.asyncio
async def test_client_wait_for_done():
    with patch("hotplex_client.client.WebSocketTransport") as MockTransport:
        mock_instance = MockTransport.return_value
        mock_instance.connect = AsyncMock(return_value="sess_123")
        mock_instance.is_connected = True
        mock_instance.session_id = "sess_123"
        mock_instance.send = AsyncMock()
        
        client = HotPlexClient()
        await client.connect()
        
        # Send input which initializes the done_future
        await client.send_input("test")
        assert client._done_future is not None
        
        # Simulate done event arriving in background
        async def simulate_done():
            await asyncio.sleep(0.1)
            env = Envelope(
                event=Event(type="done", data={"success": True, "stats": {}})
            )
            await client._dispatch_event(env)

        asyncio.create_task(simulate_done())
        
        result = await client.wait_for_done(timeout=1.0)
        assert result["success"] is True

@pytest.mark.asyncio
async def test_client_context_manager():
    with patch("hotplex_client.client.WebSocketTransport") as MockTransport:
        mock_instance = MockTransport.return_value
        mock_instance.connect = AsyncMock(return_value="sess_1")
        mock_instance.close = AsyncMock()
        mock_instance.is_connected = True
        mock_instance.session_id = "sess_1"
        
        async with HotPlexClient() as client:
            assert client.session_id == "sess_1"
            mock_instance.connect.assert_called_once()
            
        mock_instance.close.assert_called_once()
