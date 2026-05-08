"""
High-level HotPlex client API.

Provides user-friendly API for session management and event handling.
"""

import asyncio
import logging
from collections import defaultdict
from typing import Any, Callable, Coroutine, TypeVar, Generic

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

T = TypeVar("T")
Callback = Callable[[T], Coroutine[None, None, None]]


class HotPlexClient:
    """
    High-level client for HotPlex Gateway.

    Provides an asynchronous interface for interacting with HotPlex AI workers
    using the AEP v1 protocol.

    Example:
        ```python
        async with HotPlexClient(
            url="ws://localhost:8888",
            worker_type=WorkerType.CLAUDE_CODE,
        ) as client:
            @client.on("message.delta")
            async def handle_delta(data: MessageDeltaData):
                print(data.content, end="", flush=True)

            await client.send_input("Hello, Claude!")
            result = await client.wait_for_done()
            print(f"\nTask completed: {result.success}")
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
            url: WebSocket gateway URL.
            worker_type: Worker type to use (e.g., 'claude_code').
            auth_token: Optional authentication token.
            config: Optional worker configuration overrides.
            session_id: Optional session ID to resume an existing session.
        """
        self._url = url
        self._worker_type = worker_type
        self._auth_token = auth_token
        self._config = config
        self._session_id = session_id

        self._transport = WebSocketTransport()
        self._callbacks: dict[str, list[Callback[Any]]] = defaultdict(list)
        self._event_loop_task: asyncio.Task[None] | None = None
        self._done_future: asyncio.Future[DoneData] | None = None

    @property
    def session_id(self) -> str:
        """Current session ID."""
        return self._transport.session_id

    @property
    def is_connected(self) -> bool:
        """Check if client is connected to the gateway."""
        return self._transport.is_connected

    async def connect(self) -> str:
        """
        Establish connection and complete init handshake.

        Returns:
            The established Session ID.

        Raises:
            TransportError: If connection fails.
            UnauthorizedError: If authentication fails.
        """
        session_id = await self._transport.connect(
            url=self._url,
            worker_type=self._worker_type,
            session_id=self._session_id,
            auth_token=self._auth_token,
            config=self._config,
        )

        # Start event loop if not already running
        if not self._event_loop_task or self._event_loop_task.done():
            self._event_loop_task = asyncio.create_task(self._event_loop())

        return session_id

    async def send_input(
        self,
        content: str,
        metadata: dict[str, Any] | None = None,
    ) -> None:
        """
        Send user input to the worker.

        Args:
            content: User input text.
            metadata: Optional metadata to attach to the input.

        Raises:
            SessionError: If the client is not connected.
            TransportError: If sending the message fails.
        """
        if not self.is_connected:
            raise SessionError("Not connected")

        # Reset done future when new input is sent
        self._done_future = asyncio.get_running_loop().create_future()

        env = create_input_envelope(
            session_id=self.session_id,
            content=content,
            metadata=metadata,
        )
        await self._transport.send(env)

    async def wait_for_done(self, timeout: float | None = None) -> DoneData:
        """
        Wait for the current task to complete (receive 'done' event).

        Args:
            timeout: Optional timeout in seconds.

        Returns:
            The 'done' event data.

        Raises:
            asyncio.TimeoutError: If the timeout is reached.
            SessionError: If no task is in progress or client is disconnected.
        """
        if self._done_future is None:
            raise SessionError("No task in progress")

        if timeout is not None:
            return await asyncio.wait_for(self._done_future, timeout)
        return await self._done_future

    async def send_permission_response(
        self,
        permission_id: str,
        allowed: bool,
        reason: str | None = None,
    ) -> None:
        """
        Send a response to a permission request.

        Args:
            permission_id: The ID from the permission_request event.
            allowed: True to grant, False to deny.
            reason: Optional reason, especially when denying.
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
        Send the result of a tool execution back to the worker.

        Args:
            tool_call_id: The ID from the tool_call event.
            output: The result data (will be JSON serialized).
            error: Optional error message if tool execution failed.
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
        """Request the gateway to terminate the current session."""
        if not self.is_connected:
            return

        env = create_control_envelope(
            session_id=self.session_id,
            action=ControlAction.TERMINATE,
        )
        await self._transport.send(env)

    async def close(self) -> None:
        """Gracefully close the connection and stop the event loop."""
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

    def on(self, event_type: str) -> Callable[[Callback[Any]], Callback[Any]]:
        """
        Decorator to register an event callback.

        Example:
            @client.on("message.delta")
            async def handle_delta(data: MessageDeltaData):
                print(data.content)
        """

        def decorator(callback: Callback[Any]) -> Callback[Any]:
            self._callbacks[event_type].append(callback)
            return callback

        return decorator

    def on_message_start(self, callback: Callback[MessageStartData]) -> None:
        """Register callback for message.start events."""
        self._callbacks["message.start"].append(callback)

    def on_message_delta(self, callback: Callback[MessageDeltaData]) -> None:
        """Register callback for message.delta events."""
        self._callbacks["message.delta"].append(callback)

    def on_message_end(self, callback: Callback[MessageEndData]) -> None:
        """Register callback for message.end events."""
        self._callbacks["message.end"].append(callback)

    def on_message(self, callback: Callback[MessageData]) -> None:
        """Register callback for complete message events."""
        self._callbacks["message"].append(callback)

    def on_tool_call(self, callback: Callback[ToolCallData]) -> None:
        """Register callback for tool_call events."""
        self._callbacks["tool_call"].append(callback)

    def on_permission_request(self, callback: Callback[PermissionRequestData]) -> None:
        """Register callback for permission_request events."""
        self._callbacks["permission_request"].append(callback)

    def on_state_change(self, callback: Callback[StateData]) -> None:
        """Register callback for session state changes."""
        self._callbacks["state"].append(callback)

    def on_done(self, callback: Callback[DoneData]) -> None:
        """Register callback for task completion."""
        self._callbacks["done"].append(callback)

    def on_error(self, callback: Callback[ErrorData]) -> None:
        """Register callback for error events."""
        self._callbacks["error"].append(callback)

    # ========================================================================
    # Context Manager
    # ========================================================================

    async def __aenter__(self) -> "HotPlexClient":
        await self.connect()
        return self

    async def __aexit__(self, *args: Any) -> None:
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
            if self._done_future and not self._done_future.done():
                self._done_future.set_exception(TransportError(f"Connection lost during task: {e}"))

    async def _dispatch_event(self, env: Envelope[Any]) -> None:
        """Dispatch event to registered callbacks."""
        event_type = env.event.type
        data = env.event.data

        # Internal handling for done event
        if event_type == "done" and self._done_future and not self._done_future.done():
            self._done_future.set_result(data)

        callbacks = self._callbacks.get(event_type, [])
        if not callbacks:
            return

        for callback in callbacks:
            try:
                await callback(data)
            except Exception as e:
                logger.error(f"Callback error for {event_type}: {e}")
