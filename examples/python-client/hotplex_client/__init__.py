"""
HotPlex Python Client

A Python client library for interacting with HotPlex Gateway via AEP v1 protocol.
"""

__version__ = "1.0.0"
__author__ = "HotPlex Team"

# 导出公共 API
from hotplex_client.client import HotPlexClient
from hotplex_client.types import (
    WorkerType,
    SessionState,
    Priority,
    ControlAction,
    Envelope,
    Event,
    InitData,
    InputData,
    MessageData,
    MessageDeltaData,
    MessageStartData,
    MessageEndData,
    ToolCallData,
    ToolResultData,
    PermissionRequestData,
    PermissionResponseData,
    StateData,
    DoneData,
    ErrorData,
    ControlData,
)
from hotplex_client.exceptions import (
    HotPlexError,
    ProtocolError,
    InvalidMessageError,
    VersionMismatchError,
    SessionError,
    SessionNotFoundError,
    SessionTerminatedError,
    SessionExpiredError,
    TransportError,
    ConnectionLostError,
    ReconnectFailedError,
    HeartbeatTimeoutError,
    AuthError,
    UnauthorizedError,
)

__all__ = [
    # Client
    "HotPlexClient",
    # Types
    "WorkerType",
    "SessionState",
    "Priority",
    "ControlAction",
    "Envelope",
    "Event",
    "InitData",
    "InputData",
    "MessageData",
    "MessageDeltaData",
    "MessageStartData",
    "MessageEndData",
    "ToolCallData",
    "ToolResultData",
    "PermissionRequestData",
    "PermissionResponseData",
    "StateData",
    "DoneData",
    "ErrorData",
    "ControlData",
    # Exceptions
    "HotPlexError",
    "ProtocolError",
    "InvalidMessageError",
    "VersionMismatchError",
    "SessionError",
    "SessionNotFoundError",
    "SessionTerminatedError",
    "SessionExpiredError",
    "TransportError",
    "ConnectionLostError",
    "ReconnectFailedError",
    "HeartbeatTimeoutError",
    "AuthError",
    "UnauthorizedError",
]
