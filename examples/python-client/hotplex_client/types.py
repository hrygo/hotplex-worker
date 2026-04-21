"""
AEP v1 protocol type definitions.

This module defines all data types used in the AEP v1 protocol,
matching the structures defined in pkg/events/events.go.
"""

from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any, Generic, TypeVar

# ============================================================================
# Protocol Constants
# ============================================================================

AEP_VERSION = "aep/v1"
EVENT_ID_PREFIX = "evt_"
SESSION_ID_PREFIX = "sess_"

# ============================================================================
# Enums
# ============================================================================


class WorkerType(StrEnum):
    """Worker type identifiers."""

    CLAUDE_CODE = "claude_code"
    OPENCODE_SERVER = "opencode_server"
    PI_MONO = "pi-mono"


class SessionState(StrEnum):
    """Session lifecycle states."""

    CREATED = "created"
    RUNNING = "running"
    IDLE = "idle"
    TERMINATED = "terminated"
    DELETED = "deleted"


class Priority(StrEnum):
    """Message priority levels."""

    CONTROL = "control"  # Bypasses backpressure
    DATA = "data"  # Subject to backpressure


class ControlAction(StrEnum):
    """Server-originated control actions."""

    RECONNECT = "reconnect"
    SESSION_INVALID = "session_invalid"
    THROTTLE = "throttle"
    TERMINATE = "terminate"
    DELETE = "delete"


class ErrorCode(StrEnum):
    """AEP v1 standardized error codes."""

    WORKER_START_FAILED = "WORKER_START_FAILED"
    WORKER_CRASH = "WORKER_CRASH"
    WORKER_TIMEOUT = "WORKER_TIMEOUT"
    WORKER_OOM = "WORKER_OOM"
    PROCESS_SIGKILL = "PROCESS_SIGKILL"
    INVALID_MESSAGE = "INVALID_MESSAGE"
    SESSION_NOT_FOUND = "SESSION_NOT_FOUND"
    SESSION_EXPIRED = "SESSION_EXPIRED"
    SESSION_TERMINATED = "SESSION_TERMINATED"
    SESSION_INVALIDATED = "SESSION_INVALIDATED"
    SESSION_BUSY = "SESSION_BUSY"
    UNAUTHORIZED = "UNAUTHORIZED"
    AUTH_REQUIRED = "AUTH_REQUIRED"
    INTERNAL_ERROR = "INTERNAL_ERROR"
    PROTOCOL_VIOLATION = "PROTOCOL_VIOLATION"
    VERSION_MISMATCH = "VERSION_MISMATCH"
    CONFIG_INVALID = "CONFIG_INVALID"
    RATE_LIMITED = "RATE_LIMITED"
    GATEWAY_OVERLOAD = "GATEWAY_OVERLOAD"
    EXECUTION_TIMEOUT = "EXECUTION_TIMEOUT"
    RECONNECT_REQUIRED = "RECONNECT_REQUIRED"
    WORKER_OUTPUT_LIMIT = "WORKER_OUTPUT_LIMIT"


# ============================================================================
# Event Data Types
# ============================================================================


@dataclass
class InitData:
    """init event payload (client → gateway handshake)."""

    version: str
    worker_type: WorkerType
    session_id: str | None = None
    auth: dict[str, Any] | None = None
    config: dict[str, Any] | None = None
    client_caps: dict[str, Any] | None = None


@dataclass
class InitAckData:
    """init_ack event payload (gateway → client handshake response)."""

    session_id: str
    state: SessionState
    server_caps: dict[str, Any]
    error: str | None = None
    code: str | None = None


@dataclass
class InputData:
    """input event payload (user input to worker)."""

    content: str
    metadata: dict[str, Any] | None = None


@dataclass
class MessageStartData:
    """message.start event payload (streaming message start)."""

    id: str
    role: str
    content_type: str
    metadata: dict[str, Any] | None = None


@dataclass
class MessageDeltaData:
    """message.delta event payload (streaming content chunk)."""

    message_id: str
    content: str


@dataclass
class MessageEndData:
    """message.end event payload (streaming message end)."""

    message_id: str


@dataclass
class MessageData:
    """message event payload (complete message, non-streaming)."""

    id: str
    role: str
    content: str
    content_type: str | None = None
    metadata: dict[str, Any] | None = None


@dataclass
class ToolCallData:
    """tool_call event payload (worker invoking a tool)."""

    id: str
    name: str
    input: dict[str, Any]


@dataclass
class ToolResultData:
    """tool_result event payload (tool execution result)."""

    id: str
    output: Any
    error: str | None = None


@dataclass
class PermissionRequestData:
    """permission_request event payload (asking user for permission)."""

    id: str
    tool_name: str
    description: str | None = None
    args: list[str] | None = None


@dataclass
class PermissionResponseData:
    """permission_response event payload (user grants/denies permission)."""

    id: str
    allowed: bool
    reason: str | None = None


@dataclass
class ReasoningData:
    """reasoning event payload (agent thinking/reasoning)."""

    id: str
    content: str
    model: str | None = None


@dataclass
class StepData:
    """step event payload (execution step marker)."""

    id: str
    step_type: str
    name: str | None = None
    input: dict[str, Any] | None = None
    output: dict[str, Any] | None = None
    parent_id: str | None = None
    duration: int | None = None  # milliseconds


@dataclass
class RawData:
    """raw event payload (passthrough for agent-specific messages)."""

    kind: str
    raw: Any


@dataclass
class DoneStats:
    """Execution statistics from done event."""

    duration_ms: int | None = None
    tool_calls: int | None = None
    input_tokens: int | None = None
    output_tokens: int | None = None
    cache_read_tokens: int | None = None
    cache_write_tokens: int | None = None
    total_tokens: int | None = None
    cost_usd: float | None = None
    model: str | None = None
    context_used_percent: float | None = None


@dataclass
class DoneData:
    """done event payload (task completion)."""

    success: bool
    stats: DoneStats | None = None
    dropped: bool | None = None


@dataclass
class ErrorData:
    """error event payload (error notification)."""

    code: str
    message: str
    event_id: str | None = None
    details: dict[str, Any] | None = None


@dataclass
class StateData:
    """state event payload (session state change)."""

    state: SessionState
    message: str | None = None


@dataclass
class PongData:
    """pong event payload (heartbeat response)."""

    state: SessionState


@dataclass
class ControlData:
    """control event payload (server-originated control instruction)."""

    action: ControlAction
    reason: str | None = None
    delay_ms: int | None = None
    recoverable: bool | None = None
    suggestion: dict[str, Any] | None = None
    details: dict[str, Any] | None = None


# ============================================================================
# Core Protocol Types
# ============================================================================

T = TypeVar("T")


@dataclass
class Event(Generic[T]):
    """Event container with type and data."""

    type: str
    data: T


@dataclass
class Envelope(Generic[T]):
    """AEP v1 message envelope."""

    version: str = AEP_VERSION
    id: str = ""
    seq: int = 0
    priority: Priority | None = None
    session_id: str = ""
    timestamp: int = 0
    event: Event[T] = field(default_factory=lambda: Event(type="", data=None))  # type: ignore

    def to_dict(self) -> dict[str, Any]:
        """Convert envelope to dictionary for JSON serialization."""
        result: dict[str, Any] = {
            "version": self.version,
            "id": self.id,
            "seq": self.seq,
            "session_id": self.session_id,
            "timestamp": self.timestamp,
            "event": {
                "type": self.event.type,
                "data": self.event.data,
            },
        }
        if self.priority is not None:
            result["priority"] = self.priority
        return result
