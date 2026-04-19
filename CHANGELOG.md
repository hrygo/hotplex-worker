# Changelog

All notable changes to the HotPlex Worker Gateway project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **ACPX Worker Adapter**: New `acpx` worker type with stdio-based session management, ACP bridge, and session ID passthrough.
- **Persistent Session Mechanism**: UUIDv5-based session IDs, session reset (`session.reset`), and garbage collection (`session.gc`) commands.
- **WorkerSessionIDHandler**: Client-managed session IDs for/Server — allows the client to provide a session ID during worker init.
- **Workdir Passthrough**: `workdir` parameter support in session creation with security validation (path safety checks).
- **Go Client SDK**: Standalone Go client module (`client/`) with Connect/Resume/SendInput/Close operations and reset/gc commands.
- **WebSocket Full-Duplex Flow Diagram**: Comprehensive documentation of connection lifecycle, race conditions, and boundary scenarios.
- **Proc Manager Tests**: Comprehensive integration tests for process lifecycle (Start, Terminate, Kill, Wait, ReadLine).

### Changed

- **Client SDK**: Renamed `Kind` to `Event` constants; updated `Event.Type` field for clarity.
- ** Adapter**: Extracted `startLocked` to unify Start/Input/Resume/ResetContext flows.
- **Gateway Handlers**: Simplified by eliminating double-fetch, using `IsActive()`, and removing dead code.
- **Base Conn**: Exported `WriteAll` for reuse across adapters; added `runtime.Gosched()` on EAGAIN for macOS pipe compatibility.
- **Webchat**: Renamed from `web-chat` to `webchat`; replaced AI SDK with custom WebSocket client; extracted shared Message type.

### Fixed

- **Gateway**: Reset/GC preconditions and idempotent garbage collection.
- **Gateway**: Session orphan, ping seq, and macOS `RLIMIT_AS` handling (skip on Darwin).
- **WebSocket**: Browser reconnect race condition and strict mode compatibility.
- **macOS**: Pipe EAGAIN handling and mutex deadlock in concurrent scenarios.
- **OpenCode CLI adapter**: Race condition, dead code, and code quality fixes.

## [1.0.0-rc] - 2026-03-31

### Added

- **Core**: Initial release of the HotPlex Worker Gateway.
- **Protocol**: Implementation of AEP v1 (Agent Exchange Protocol).
- **Security**: WAF (Web Application Firewall) and PGID isolation for worker processes.
- **Workers**: Support for `claudecode`, `opencodeserver`, and `pi` worker types.
- **Admin API**: Added endpoints for stats, health checks, session management, and configuration hot-reload.
- **Monitoring**: Integration with Prometheus for metrics and OpenTelemetry for tracing.
- **Governance**: Added `codecov.yml` for enforced coverage checks.
- **Docs**: Comprehensive architecture and testing strategy documents in `docs/`.

### Fixed

- **CI**: Fixed `codecov-action` token configuration and environment access warnings.
- **Session**: Improved session termination logic for cleaner process cleanup.

## [0.1.0] - 2026-03-25

### Added

- Initial internal prototype.
- Base session management and WebSocket gateway.
- Basic SQLite storage for session persistence.
- CI/CD pipeline setup on GitHub Actions.
