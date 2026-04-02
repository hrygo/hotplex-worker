"""
Custom exception hierarchy for HotPlex client.

Exception hierarchy:
    HotPlexError
    ├── ProtocolError (protocol layer)
    │   ├── InvalidMessageError
    │   └── VersionMismatchError
    ├── SessionError (business layer)
    │   ├── SessionNotFoundError
    │   ├── SessionTerminatedError
    │   └── SessionExpiredError
    ├── TransportError (network layer)
    │   ├── ConnectionLostError
    │   ├── ReconnectFailedError
    │   └── HeartbeatTimeoutError
    └── AuthError (authentication layer)
        └── UnauthorizedError
"""


class HotPlexError(Exception):
    """Base exception for all HotPlex client errors."""

    pass


# ============================================================================
# Protocol Errors (Application Layer)
# ============================================================================


class ProtocolError(HotPlexError):
    """AEP protocol error (encoding/decoding/validation failures)."""

    pass


class InvalidMessageError(ProtocolError):
    """Invalid message format or structure."""

    pass


class VersionMismatchError(ProtocolError):
    """Protocol version mismatch between client and gateway."""

    def __init__(self, expected: str, actual: str):
        self.expected = expected
        self.actual = actual
        super().__init__(f"Version mismatch: expected {expected}, got {actual}")


# ============================================================================
# Session Errors (Business Layer)
# ============================================================================


class SessionError(HotPlexError):
    """Session-related errors."""

    pass


class SessionNotFoundError(SessionError):
    """Session does not exist."""

    pass


class SessionTerminatedError(SessionError):
    """Session has been terminated."""

    pass


class SessionExpiredError(SessionError):
    """Session has expired."""

    pass


# ============================================================================
# Transport Errors (Network Layer)
# ============================================================================


class TransportError(HotPlexError):
    """WebSocket transport errors."""

    pass


class ConnectionLostError(TransportError):
    """WebSocket connection lost."""

    pass


class ReconnectFailedError(TransportError):
    """Reconnection failed after maximum attempts."""

    def __init__(self, attempts: int):
        self.attempts = attempts
        super().__init__(f"Reconnect failed after {attempts} attempts")


class HeartbeatTimeoutError(TransportError):
    """Heartbeat timeout (missed too many pongs)."""

    pass


# ============================================================================
# Authentication Errors
# ============================================================================


class AuthError(HotPlexError):
    """Authentication failed."""

    pass


class UnauthorizedError(AuthError):
    """Unauthorized (invalid or expired token)."""

    pass
