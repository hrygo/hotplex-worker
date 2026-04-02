"""
WebSocket transport layer for HotPlex client.

Manages WebSocket connection lifecycle, message queuing, and basic error handling.
"""

import asyncio
import logging
from typing import Any, AsyncIterator

import websockets
from websockets.client import WebSocketClientProtocol

from hotplex_client.exceptions import (
    ConnectionLostError,
    TransportError,
    UnauthorizedError,
)
from hotplex_client.protocol import (
    create_init_envelope,
    decode_envelope,
    encode_envelope,
    is_init_ack,
    is_error,
)
from hotplex_client.types import (
    Envelope,
    InitAckData,
    WorkerType,
)

logger = logging.getLogger(__name__)


class WebSocketTransport:
    """
    WebSocket transport for AEP v1 protocol.

    Responsibilities:
    - WebSocket connection lifecycle
    - NDJSON message framing
    - Message queue for async receive
    - Basic error handling
    """

    def __init__(self) -> None:
        self._ws: WebSocketClientProtocol | None = None
        self._session_id: str = ""
        self._message_queue: asyncio.Queue[Envelope[Any]] = asyncio.Queue()
        self._receive_task: asyncio.Task[None] | None = None
        self._connected = False

    @property
    def session_id(self) -> str:
        """Current session ID."""
        return self._session_id

    @property
    def is_connected(self) -> bool:
        """Check if transport is connected."""
        return self._connected and self._ws is not None and self._ws.open

    async def connect(
        self,
        url: str,
        worker_type: WorkerType,
        session_id: str | None = None,
        auth_token: str | None = None,
        config: dict[str, Any] | None = None,
    ) -> str:
        """
        Establish WebSocket connection and perform init handshake.

        Args:
            url: WebSocket URL (ws:// or wss://)
            worker_type: Type of worker to use
            session_id: Optional session ID (for resume)
            auth_token: Optional authentication token
            config: Optional worker configuration

        Returns:
            Session ID from init_ack

        Raises:
            TransportError: If connection fails
            UnauthorizedError: If authentication fails
        """
        try:
            # Establish WebSocket connection
            self._ws = await websockets.connect(
                url,
                ping_interval=54,  # 54 seconds (match Go pingPeriod)
                ping_timeout=60,  # 60 seconds (match Go pongWait)
                max_size=32 * 1024 * 1024,  # 32MB
            )

            # Send init handshake
            init_env = create_init_envelope(
                worker_type=worker_type,
                session_id=session_id,
                auth_token=auth_token,
                config=config,
            )
            await self._ws.send(encode_envelope(init_env))

            # Wait for init_ack
            response = await self._ws.recv()
            ack_env = decode_envelope(str(response))

            if is_error(ack_env):
                error_data = ack_env.event.data
                if error_data.get("code") == "UNAUTHORIZED":
                    raise UnauthorizedError(error_data.get("message", "Unauthorized"))
                raise TransportError(f"Init failed: {error_data.get('message')}")

            if not is_init_ack(ack_env):
                raise TransportError(f"Expected init_ack, got {ack_env.event.type}")

            ack_data = ack_env.event.data
            self._session_id = ack_data.get("session_id", "")
            self._connected = True

            # Start background message receiver
            self._receive_task = asyncio.create_task(self._receive_loop())

            logger.info(f"Connected to {url}, session: {self._session_id}")
            return self._session_id

        except Exception as e:
            await self._cleanup()
            if isinstance(e, (UnauthorizedError, TransportError)):
                raise
            raise TransportError(f"Connection failed: {e}") from e

    async def send(self, envelope: Envelope[Any]) -> None:
        """
        Send envelope over WebSocket.

        Args:
            envelope: Envelope to send

        Raises:
            ConnectionLostError: If connection is closed
            TransportError: If send fails
        """
        if not self.is_connected:
            raise ConnectionLostError("WebSocket not connected")

        try:
            message = encode_envelope(envelope)
            await self._ws.send(message)
        except websockets.ConnectionClosed as e:
            self._connected = False
            raise ConnectionLostError(f"Connection closed: {e}") from e
        except Exception as e:
            raise TransportError(f"Send failed: {e}") from e

    async def receive(self) -> Envelope[Any]:
        """
        Receive envelope from queue (non-blocking receive from WebSocket).

        Returns:
            Next envelope from server

        Raises:
            ConnectionLostError: If connection is closed
        """
        if not self.is_connected:
            raise ConnectionLostError("WebSocket not connected")

        try:
            return await self._message_queue.get()
        except asyncio.CancelledError:
            raise ConnectionLostError("Receive cancelled")

    async def close(self) -> None:
        """Close WebSocket connection gracefully."""
        await self._cleanup()

    async def _receive_loop(self) -> None:
        """Background task: continuously receive messages from WebSocket."""
        try:
            async for message in self._ws:
                try:
                    env = decode_envelope(str(message))
                    await self._message_queue.put(env)
                except Exception as e:
                    logger.error(f"Failed to decode message: {e}")
        except websockets.ConnectionClosed as e:
            logger.warning(f"WebSocket closed: {e}")
            self._connected = False
        except Exception as e:
            logger.error(f"Receive loop error: {e}")
            self._connected = False

    async def _cleanup(self) -> None:
        """Clean up resources."""
        if self._receive_task:
            self._receive_task.cancel()
            try:
                await self._receive_task
            except asyncio.CancelledError:
                pass
            self._receive_task = None

        if self._ws:
            try:
                await self._ws.close()
            except Exception:
                pass
            self._ws = None

        self._connected = False

    async def __aenter__(self) -> "WebSocketTransport":
        """Support async with context manager."""
        return self

    async def __aexit__(self, *args: Any) -> None:
        """Automatically cleanup on context exit."""
        await self.close()
