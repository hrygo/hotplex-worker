# Changelog

All notable changes to the HotPlex Worker Gateway project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-04-21

### Added

- **Go Client SDK (v1.1.0)**:
  - **Automatic Reconnection**: Intelligent connection recovery with exponential backoff strategy.
  - **Typed Event Helpers**: Fluent API for event processing (`AsDoneData()`, `AsErrorData()`, `AsToolCallData()`, etc.).
  - **AEP v1 Parity**: Full support for `question`, `elicitation`, and `permission` request/response cycles.
  - **Handshake Resilience**: Event buffering during `doConnect` to prevent race conditions between `init_ack` and worker-start events.
  - **Metadata & Priority**: Support for outgoing message priorities and contextual metadata in `SendInput`.
- **Messaging (Slack & Feishu)**:
  - **Speech-to-Text (Feishu)**: Native integration with SenseVoice-Small ONNX engine for high-accuracy, zero-cold-start voice transcription.
  - **Message Chunking (Slack)**: Robust delivery system for long AI responses, bypassing Slack's 4000-character limit.
  - **Control Commands**: Support for natural language triggers (prefixed with `$`) and slash commands (`/reset`, `/park`, `/gc`).
  - **User Interaction Layer**: Standardized Q&A and permission elicitation flows with auto-deny (5-minute timeout).
- **Gateway Core**:
  - **LLM Auto-Retry**: Built-in retry controller with exponential backoff for transient AI provider failures.
  - **Fresh Start Fallback**: Automatic creation of new workers with input re-delivery when session resumption fails.
  - **ACPX Adapter**: Stdio-based worker bridge for enhanced tooling integration.
- **Docs**:
  - New bilingual (EN/CN) README and product whitepaper.
  - Updated AEP v1 protocol specification and handshake diagrams.

### Changed

- **Security**: Implemented granular 3-layer whitelists and security policies for tool/command execution.
- **Worker Configuration**: Increased default zombie IO timeout from 10m to 30m for long-running batch tasks.
- **Feishu Logic**: Redacted sensitive URL parameters in SDK logs and optimized card streaming state management.
- **Webchat**: Replaced AI SDK with a custom, high-performance WebSocket client.

### Fixed

- **Claude Code Worker**: Resolved `ResetContext` bug that caused "done" event leaks during session resets.
- **Event Ordering**: Restored deterministic event sequencing in `Hub.Run` to eliminate flaky E2E test failures.
- **Media Management**: Implemented automatic cleanup for temporary media files in the Slack adapter.
- **Stability**: Added panic recovery to all gateway handlers and messaging adapters.
- **macOS Compatibility**: Fixed pipe `EAGAIN` handling and adjusted `RLIMIT_AS` (address space) constraints.

## [1.0.0] - 2026-04-11

### Added

- **Core**: Initial release of the HotPlex Worker Gateway.
- **Protocol**: Multi-language support for AEP v1 protocol.
- **Security**: WAF and PGID isolation for worker processes.
- **Monitoring**: Integration with Prometheus and OpenTelemetry.
- **Docs**: Architecture and testing strategy documents.

### Fixed

- **CI**: stabilized GitHub Actions environment and coverage reporting.
- **Session**: Optimized termination sequence for clean process cleanup.

## [0.1.0] - 2026-03-25

### Added

- Initial internal prototype.
- Base session management and SQLite persistence.
- CI/CD pipeline setup.
