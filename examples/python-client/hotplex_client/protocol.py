"""
AEP v1 protocol encoder/decoder.

This module provides functions for encoding/decoding AEP v1 messages
(NDJSON format: one JSON object per line terminated with \\n).
"""

import json
import re
import time
import uuid
from typing import Any, TypeVar

from hotplex_client.types import (
    AEP_VERSION,
    EVENT_ID_PREFIX,
    SESSION_ID_PREFIX,
    ControlAction,
    ControlData,
    Envelope,
    Event,
    InitData,
    InputData,
    PermissionResponseData,
    Priority,
    ToolResultData,
    WorkerType,
)
from hotplex_client.exceptions import InvalidMessageError, ProtocolError

T = TypeVar("T")

# ============================================================================
# UUID Generation
# ============================================================================


def generate_uuid() -> str:
    """Generate a UUID v4 string."""
    return str(uuid.uuid4())


def generate_event_id() -> str:
    """Generate event ID with prefix (evt_<uuid>)."""
    return f"{EVENT_ID_PREFIX}{generate_uuid()}"


def generate_session_id() -> str:
    """Generate session ID with prefix (sess_<uuid>)."""
    return f"{SESSION_ID_PREFIX}{generate_uuid()}"


# ============================================================================
# NDJSON Serialization
# ============================================================================


def encode_envelope(env: Envelope[T]) -> str:
    """
    Serialize envelope to NDJSON string (single JSON line + \\n).

    Args:
        env: Envelope to serialize

    Returns:
        NDJSON string with trailing newline

    Raises:
        ProtocolError: If serialization fails
    """
    try:
        # Use to_dict() method for proper serialization
        data = env.to_dict()
        # JSON dump + newline = NDJSON
        return json.dumps(data, separators=(",", ":")) + "\n"
    except (TypeError, ValueError) as e:
        raise ProtocolError(f"Failed to encode envelope: {e}") from e


def decode_envelope(line: str) -> Envelope[Any]:
    """
    Deserialize NDJSON line to Envelope.

    Args:
        line: NDJSON line (may contain trailing \\n)

    Returns:
        Decoded Envelope with appropriate event data type

    Raises:
        InvalidMessageError: If JSON parsing fails
        ProtocolError: If message structure is invalid
    """
    # Strip newline and whitespace
    line = line.strip()
    if not line:
        raise InvalidMessageError("Empty message")

    # Sanitize line/paragraph separators (match Go implementation)
    line = line.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")

    try:
        data = json.loads(line)
    except json.JSONDecodeError as e:
        raise InvalidMessageError(f"Invalid JSON: {e}") from e

    # Validate required fields
    if not isinstance(data, dict):
        raise InvalidMessageError("Message must be a JSON object")

    if "event" not in data:
        raise InvalidMessageError("Missing 'event' field")

    # Extract event type
    event_data = data.get("event")
    if not isinstance(event_data, dict) or "type" not in event_data:
        raise InvalidMessageError("Invalid 'event' structure")

    event_type = event_data["type"]
    event_payload = event_data.get("data")

    # Build Envelope (keep event.data as raw dict for flexibility)
    return Envelope(
        version=data.get("version", AEP_VERSION),
        id=data.get("id", ""),
        seq=data.get("seq", 0),
        priority=Priority(data["priority"]) if "priority" in data else None,
        session_id=data.get("session_id", ""),
        timestamp=data.get("timestamp", 0),
        event=Event(type=event_type, data=event_payload),
    )


# ============================================================================
# Envelope Constructors
# ============================================================================


def create_envelope(
    id: str,
    session_id: str,
    seq: int,
    event_type: str,
    event_data: T,
    priority: Priority | None = None,
) -> Envelope[T]:
    """
    Create a new envelope with timestamp.

    Args:
        id: Event ID
        session_id: Session ID
        seq: Sequence number (0 for client→server)
        event_type: Event type string
        event_data: Event payload
        priority: Optional priority level

    Returns:
        New Envelope instance
    """
    return Envelope(
        version=AEP_VERSION,
        id=id,
        seq=seq,
        priority=priority,
        session_id=session_id,
        timestamp=int(time.time() * 1000),
        event=Event(type=event_type, data=event_data),
    )


def create_init_envelope(
    worker_type: WorkerType,
    session_id: str | None = None,
    auth_token: str | None = None,
    config: dict[str, Any] | None = None,
) -> Envelope[InitData]:
    """
    Create init handshake envelope (first message after WebSocket connection).

    Args:
        worker_type: Type of worker to use
        session_id: Optional session ID (for resume)
        auth_token: Optional authentication token
        config: Optional worker configuration

    Returns:
        Init event envelope
    """
    # Build init data
    init_data: dict[str, Any] = {
        "version": AEP_VERSION,
        "worker_type": worker_type,
        "client_caps": {
            "supports_delta": True,
            "supports_streaming": True,
            "supports_tools": True,
            "supports_permissions": True,
        },
    }

    if session_id:
        init_data["session_id"] = session_id

    if auth_token:
        init_data["auth"] = {"token": auth_token}

    if config:
        init_data["config"] = config

    return create_envelope(
        id=generate_event_id(),
        session_id=session_id or "",
        seq=0,
        event_type="init",
        event_data=init_data,
        priority=Priority.CONTROL,
    )


def create_input_envelope(
    session_id: str,
    content: str,
    metadata: dict[str, Any] | None = None,
) -> Envelope[InputData]:
    """
    Create user input envelope.

    Args:
        session_id: Session ID
        content: User input text
        metadata: Optional metadata

    Returns:
        Input event envelope
    """
    input_data: dict[str, Any] = {"content": content}
    if metadata:
        input_data["metadata"] = metadata

    return create_envelope(
        id=generate_event_id(),
        session_id=session_id,
        seq=0,
        event_type="input",
        event_data=input_data,
        priority=Priority.DATA,
    )


def create_ping_envelope(session_id: str) -> Envelope[dict[str, Any]]:
    """
    Create ping envelope for heartbeat.

    Args:
        session_id: Session ID

    Returns:
        Ping event envelope
    """
    return create_envelope(
        id=generate_event_id(),
        session_id=session_id,
        seq=0,
        event_type="ping",
        event_data={},
        priority=Priority.CONTROL,
    )


def create_control_envelope(
    session_id: str,
    action: ControlAction,
) -> Envelope[ControlData]:
    """
    Create control envelope (terminate/delete session).

    Args:
        session_id: Session ID
        action: Control action

    Returns:
        Control event envelope
    """
    return create_envelope(
        id=generate_event_id(),
        session_id=session_id,
        seq=0,
        event_type="control",
        event_data={"action": action},
        priority=Priority.CONTROL,
    )


def create_permission_response_envelope(
    session_id: str,
    permission_id: str,
    allowed: bool,
    reason: str | None = None,
) -> Envelope[PermissionResponseData]:
    """
    Create permission response envelope.

    Args:
        session_id: Session ID
        permission_id: Permission request ID
        allowed: Whether permission is granted
        reason: Optional reason for denial

    Returns:
        Permission response event envelope
    """
    data: dict[str, Any] = {
        "id": permission_id,
        "allowed": allowed,
    }
    if reason:
        data["reason"] = reason

    return create_envelope(
        id=generate_event_id(),
        session_id=session_id,
        seq=0,
        event_type="permission_response",
        event_data=data,
        priority=Priority.CONTROL,
    )


def create_tool_result_envelope(
    session_id: str,
    tool_call_id: str,
    output: Any,
    error: str | None = None,
) -> Envelope[ToolResultData]:
    """
    Create tool result envelope.

    Args:
        session_id: Session ID
        tool_call_id: Tool call ID
        output: Tool execution result
        error: Optional error message

    Returns:
        Tool result event envelope
    """
    data: dict[str, Any] = {
        "id": tool_call_id,
        "output": output,
    }
    if error:
        data["error"] = error

    return create_envelope(
        id=generate_event_id(),
        session_id=session_id,
        seq=0,
        event_type="tool_result",
        event_data=data,
        priority=Priority.DATA,
    )


# ============================================================================
# Envelope Type Guards
# ============================================================================


def is_init_ack(env: Envelope[Any]) -> bool:
    """Check if envelope is init_ack event."""
    return env.event.type == "init_ack"


def is_error(env: Envelope[Any]) -> bool:
    """Check if envelope is error event."""
    return env.event.type == "error"


def is_state(env: Envelope[Any]) -> bool:
    """Check if envelope is state event."""
    return env.event.type == "state"


def is_done(env: Envelope[Any]) -> bool:
    """Check if envelope is done event."""
    return env.event.type == "done"


def is_delta(env: Envelope[Any]) -> bool:
    """Check if envelope is message.delta event."""
    return env.event.type == "message.delta"


def is_control(env: Envelope[Any]) -> bool:
    """Check if envelope is control event."""
    return env.event.type == "control"
