"""
WebSocket transport layer for HotPlex client.

Manages WebSocket connection lifecycle, message queuing, and basic error handling.
"""

import asyncio
import logging
from typing import Any, AsyncIterator, Dict, Optional

import websockets
from websockets.client import WebSocketClientProtocol

from hotplex_client.exceptions import (
    ConnectionLostError,
    TransportError,
    UnauthorizedError,
)
from hotplex_client.protocol import (
    create_init_envelope,
    create_ping_envelope,
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

    Handles connection management, NDJSON framing, and background message receiving.
    """

    def __init__(
        self,
        max_queue_size: int = 1000,
        ping_interval: float = 54.0,
        ping_timeout: float = 10.0,
    ) -> None:
        """
        Initialize WebSocket transport.

        Args:
            max_queue_size: Maximum number of messages to buffer.
            ping_interval: Interval between pings in seconds.
            ping_timeout: Time to wait for pong before closing connection.
        """
        self._ws: Optional[WebSocketClientProtocol] = None
        self._session_id: str = ""
        self._message_queue: asyncio.Queue[Envelope[Any]] = asyncio.Queue(maxsize=max_queue_size)
        self._receive_task: Optional[asyncio.Task[None]] = None
        self._ping_interval = ping_interval
        self._ping_timeout = ping_timeout

    @property
    def session_id(self) -> str:
        """Current session ID."""
        return self._session_id

    @property
    def is_connected(self) -> bool:
        """Check if transport is connected."""
        return self._ws is not None and self._ws.open

    async def connect(
        self,
        url: str,
        worker_type: WorkerType,
        session_id: Optional[str] = None,
        auth_token: Optional[str] = None,
        config: Optional[Dict[str, Any]] = None,
        **ws_options: Any,
    ) -> str:
        """
        Establish WebSocket connection and perform init handshake.

        Args:
            url: WebSocket URL (ws:// or wss://).
            worker_type: Type of worker to use.
            session_id: Optional session ID to resume.
            auth_token: Optional authentication token.
            config: Optional worker configuration.
            **ws_options: Additional options for websockets.connect().

        Returns:
            The established Session ID.
        """
        try:
            # Establish WebSocket connection
            # Default options match Go gateway expectations
            connect_options = {
                "ping_interval": self._ping_interval,
                "ping_timeout": self._ping_timeout,
                "max_size": 32 * 1024 * 1024,  # 32MB
            }
            connect_options.update(ws_options)

            self._ws = await websockets.connect(url, **connect_options)

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
            if isinstance(response, bytes):
                response = response.decode("utf-8")
            ack_env = decode_envelope(response)

            if is_error(ack_env):
                error_data = ack_env.event.data
                if error_data.get("code") == "UNAUTHORIZED":
                    raise UnauthorizedError(error_data.get("message", "Unauthorized"))
                raise TransportError(f"Init failed: {error_data.get('message')}")

            if not is_init_ack(ack_env):
                raise TransportError(f"Expected init_ack, got {ack_env.event.type}")

            ack_data = ack_env.event.data
            self._session_id = ack_data.get("session_id", "")

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
        """Send envelope over WebSocket."""
        if not self.is_connected:
            raise ConnectionLostError("WebSocket not connected")

        try:
            message = encode_envelope(envelope)
            await self._ws.send(message)
        except websockets.ConnectionClosed as e:
            raise ConnectionLostError(f"Connection closed: {e}") from e
        except Exception as e:
            raise TransportError(f"Send failed: {e}") from e

    async def receive(self) -> Envelope[Any]:
        """Receive envelope from queue."""
        if not self.is_connected and self._message_queue.empty():
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
                    if isinstance(message, bytes):
                        message = message.decode("utf-8")
                    env = decode_envelope(message)

                    # Handle application-level ping/pong if needed
                    if env.event.type == "ping":
                        # Auto-pong is handled by websockets library for WS-level pings,
                        # but AEP might have its own. For now, we just queue it.
                        pass

                    await self._message_queue.put(env)
                except Exception as e:
                    logger.error(f"Failed to decode message: {e}")
        except websockets.ConnectionClosed as e:
            logger.warning(f"WebSocket closed: {e}")
        except Exception as e:
            logger.error(f"Receive loop error: {e}")
        finally:
            # Signal end of stream by putting None or raising
            pass

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

    async def __aenter__(self) -> "WebSocketTransport":
        return self

    async def __aexit__(self, *args: Any) -> None:
        await self.close()
