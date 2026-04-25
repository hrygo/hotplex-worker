"""
High-level HotPlex client API.

Provides user-friendly API for session management and event handling.
"""

import asyncio
import logging
from typing import Any, Callable, Coroutine

from hotplex_client.exceptions import HotPlexError, SessionError, TransportError
from hotplex_client.protocol import (
    create_control_envelope,
    create_input_envelope,
    create_permission_response_envelope,
    create_tool_result_envelope,
)
from hotplex_client.transport import WebSocketTransport
from hotplex_client.types import (
    ControlAction,
    ControlData,
    DoneData,
    Envelope,
    ErrorData,
    InitAckData,
    InputData,
    MessageData,
    MessageDeltaData,
    MessageEndData,
    MessageStartData,
    PermissionRequestData,
    PermissionResponseData,
    ReasoningData,
    SessionState,
    StateData,
    StepData,
    ToolCallData,
    ToolResultData,
    WorkerType,
)

logger = logging.getLogger(__name__)

# Type aliases for callbacks
Callback = Callable[[Any], Coroutine[None, None, None]]


class HotPlexClient:
    """
    High-level client for HotPlex Gateway.

    Example:
        ```python
        async with HotPlexClient(
            url="ws://localhost:8888",
            worker_type=WorkerType.CLAUDE_CODE,
        ) as client:
            @client.on_message_delta
            async def handle_delta(data: MessageDeltaData):
                print(data.content, end="")

            await client.send_input("Hello, Claude!")
            await asyncio.sleep(60)  # Wait for response
        ```
    """

    def __init__(
        self,
        url: str = "ws://localhost:8888",
        worker_type: WorkerType = WorkerType.CLAUDE_CODE,
        auth_token: str | None = None,
        config: dict[str, Any] | None = None,
        session_id: str | None = None,
    ):
        """
        Initialize HotPlex client.

        Args:
            url: WebSocket gateway URL
            worker_type: Worker type to use
            auth_token: Optional authentication token
            config: Optional worker configuration
            session_id: Optional session ID (for resume)
        """
        self._url = url
        self._worker_type = worker_type
        self._auth_token = auth_token
        self._config = config
        self._session_id = session_id

        self._transport = WebSocketTransport()
        self._callbacks: dict[str, Callback] = {}
        self._event_loop_task: asyncio.Task[None] | None = None
        self._done_event = asyncio.Event()

    @property
    def session_id(self) -> str:
        """Current session ID."""
        return self._transport.session_id

    @property
    def is_connected(self) -> bool:
        """Check if client is connected."""
        return self._transport.is_connected

    async def connect(self) -> str:
        """
        Establish connection and complete init handshake.

        Returns:
            Session ID

        Raises:
            TransportError: If connection fails
            AuthError: If authentication fails
        """
        session_id = await self._transport.connect(
            url=self._url,
            worker_type=self._worker_type,
            session_id=self._session_id,
            auth_token=self._auth_token,
            config=self._config,
        )

        # Start event loop
        self._event_loop_task = asyncio.create_task(self._event_loop())

        return session_id

    async def send_input(
        self,
        content: str,
        metadata: dict[str, Any] | None = None,
    ) -> None:
        """
        Send user input to worker.

        Args:
            content: User input text
            metadata: Optional metadata

        Raises:
            SessionError: If session is not active
            TransportError: If send fails
        """
        if not self.is_connected:
            raise SessionError("Not connected")

        env = create_input_envelope(
            session_id=self.session_id,
            content=content,
            metadata=metadata,
        )
        await self._transport.send(env)

    async def send_permission_response(
        self,
        permission_id: str,
        allowed: bool,
        reason: str | None = None,
    ) -> None:
        """
        Send permission response.

        Args:
            permission_id: Permission request ID
            allowed: Whether permission is granted
            reason: Optional reason for denial
        """
        if not self.is_connected:
            raise SessionError("Not connected")

        env = create_permission_response_envelope(
            session_id=self.session_id,
            permission_id=permission_id,
            allowed=allowed,
            reason=reason,
        )
        await self._transport.send(env)

    async def send_tool_result(
        self,
        tool_call_id: str,
        output: Any,
        error: str | None = None,
    ) -> None:
        """
        Send tool execution result.

        Args:
            tool_call_id: Tool call ID
            output: Tool execution result
            error: Optional error message
        """
        if not self.is_connected:
            raise SessionError("Not connected")

        env = create_tool_result_envelope(
            session_id=self.session_id,
            tool_call_id=tool_call_id,
            output=output,
            error=error,
        )
        await self._transport.send(env)

    async def terminate(self) -> None:
        """Terminate session."""
        if not self.is_connected:
            return

        env = create_control_envelope(
            session_id=self.session_id,
            action=ControlAction.TERMINATE,
        )
        await self._transport.send(env)

    async def close(self) -> None:
        """Close connection."""
        if self._event_loop_task:
            self._event_loop_task.cancel()
            try:
                await self._event_loop_task
            except asyncio.CancelledError:
                pass

        await self._transport.close()

    # ========================================================================
    # Event Callback Registration
    # ========================================================================

    def on_message_start(self, callback: Callable[[MessageStartData], Coroutine[None, None, None]]):
        """Register callback for message.start events."""
        self._callbacks["message.start"] = callback

    def on_message_delta(self, callback: Callable[[MessageDeltaData], Coroutine[None, None, None]]):
        """Register callback for message.delta events."""
        self._callbacks["message.delta"] = callback

    def on_message_end(self, callback: Callable[[MessageEndData], Coroutine[None, None, None]]):
        """Register callback for message.end events."""
        self._callbacks["message.end"] = callback

    def on_message(self, callback: Callable[[MessageData], Coroutine[None, None, None]]):
        """Register callback for message events (non-streaming)."""
        self._callbacks["message"] = callback

    def on_tool_call(self, callback: Callable[[ToolCallData], Coroutine[None, None, None]]):
        """Register callback for tool_call events."""
        self._callbacks["tool_call"] = callback

    def on_permission_request(self, callback: Callable[[PermissionRequestData], Coroutine[None, None, None]]):
        """Register callback for permission_request events."""
        self._callbacks["permission_request"] = callback

    def on_state_change(self, callback: Callable[[StateData], Coroutine[None, None, None]]):
        """Register callback for state events."""
        self._callbacks["state"] = callback

    def on_done(self, callback: Callable[[DoneData], Coroutine[None, None, None]]):
        """Register callback for done events."""
        self._callbacks["done"] = callback

    def on_error(self, callback: Callable[[ErrorData], Coroutine[None, None, None]]):
        """Register callback for error events."""
        self._callbacks["error"] = callback

    def on_control(self, callback: Callable[[ControlData], Coroutine[None, None, None]]):
        """Register callback for control events."""
        self._callbacks["control"] = callback

    # ========================================================================
    # Context Manager
    # ========================================================================

    async def __aenter__(self) -> "HotPlexClient":
        """Auto-connect on context entry."""
        await self.connect()
        return self

    async def __aexit__(self, *args: Any) -> None:
        """Auto-cleanup on context exit."""
        await self.close()

    # ========================================================================
    # Internal Event Loop
    # ========================================================================

    async def _event_loop(self) -> None:
        """Background task: receive and dispatch events to callbacks."""
        try:
            while self.is_connected:
                env = await self._transport.receive()
                await self._dispatch_event(env)
        except asyncio.CancelledError:
            pass
        except Exception as e:
            logger.error(f"Event loop error: {e}")

    async def _dispatch_event(self, env: Envelope[Any]) -> None:
        """Dispatch event to registered callback."""
        event_type = env.event.type
        callback = self._callbacks.get(event_type)

        if not callback:
            logger.debug(f"No callback registered for {event_type}")
            return

        try:
            # Cast event data to appropriate type
            data = env.event.data
            await callback(data)
        except Exception as e:
            logger.error(f"Callback error for {event_type}: {e}")
