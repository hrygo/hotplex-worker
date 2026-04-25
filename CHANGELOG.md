# Changelog

All notable changes to the HotPlex Worker Gateway project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-04-25

### Added

- **CLI Self-Service**:
  - **`onboard` Interactive Wizard**: First-time setup and reconfiguration with config detection, keep-or-reconfigure per messaging platform, and JWT/admin token auto-generation.
  - **`doctor` Diagnostics**: Run categorized health checks (environment, config, dependencies, security, runtime, messaging) with optional `--fix` auto-repair.
  - **`security` Audit**: Security-focused checks for JWT strength, admin tokens, TLS, SSRF origins, and file permissions.
  - **`status` Command**: Check gateway process health via PID file and health endpoint.
  - **`config validate`**: Validate YAML syntax and value constraints with optional `--strict` secret check.
  - **Checker Framework**: Plugable `Checker` interface with registry, categories, fix hints, and auto-fix functions.
  - **Terminal Output**: Color-coded printer with status icons, diagnostic report rendering, and JSON output format.
- **Gateway**:
  - **Request Throttling**: In-memory token bucket rate limiter via `internal/gateway/throttle.go`.
  - **Worker Adapters**: Fixed missing blank imports for `opencodeserver` and `pi` adapters — all three worker types now register at startup.
- **Premium WebChat UI/UX (Specs v1.1)**:
  - **SDK-Native Architecture**: Deep integration with `@assistant-ui/react` and Vercel AI SDK, replacing custom state machines with industry-standard runtime providers.
  - **Generative UI (GenUI)**: Interactive high-fidelity rendering for `edit_file` (Monaco-based Diff views), `run_command` (Terminal mockup with stdout/err highlighting), and `ask_permission` (MCP-style interaction cards).
  - **Advanced Session Initialization**: Full support for multi-parameter session creation (`workerType`, `workDir`, `agentConfig`) with persistent recent directories and URL-driven deep linking via `nuqs`.
  - **Premium Design System**: Glassmorphism visual language with `backdrop-blur-2xl`, Tailwind 4 advanced theme tokens, and dark-mode-first aesthetics.
  - **Motion & Feedback**: Smooth layout transitions and micro-interactions powered by `framer-motion` v12.
  - **Workspace Awareness**: Real-time token usage, latency monitoring, and environment indicators (Worker type, Project path) integrated into the NavBar.
  - **Modern Tech Stack**: Upgraded foundation to Next.js 16, React 19, and AI SDK v7/v4 beta tracks for peak performance.
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
  - **Help Command**: `/help` command with bilingual (EN/CN) UI and TableBlock rendering for Slack and Feishu.
  - **Status Indicators**: Worker command status feedback for reset, park, and other control operations.
- **Gateway Core**:
  - **LLM Auto-Retry**: Built-in retry controller with exponential backoff for transient AI provider failures.
  - **Fresh Start Fallback**: Automatic creation of new workers with input re-delivery when session resumption fails.
  - **ACPX Adapter**: Stdio-based worker bridge for enhanced tooling integration.
  - **Per-Conn Async Writer**: Delta coalescing writer per platform connection to reduce redundant writes and improve throughput.
- **Worker**:
  - **Stdio Session Control**: Worker command passthrough via stdio for seamless control command integration.
- **Docs**:
  - New bilingual (EN/CN) README and product whitepaper.
  - Updated AEP v1 protocol specification and handshake diagrams.
  - Restructured docs directory with dedicated Reference Manual.

### Changed

- **Project Renamed**: `hotplex-worker` → `hotplex`; single binary with subcommand architecture (gateway, doctor, security, onboard, config, status, version).
- **Gateway Refactored**: Consolidated `conn.go` hub fields and split throttle into dedicated file for cleaner separation.
- **Webchat UI**: SessionPanel restructured with better visual hierarchy; assistant-ui thread component updated.
- **Config**: `MaxIdlePerUser` default raised from 3 to 5; config tuning for improved session pooling.
- **Security**: Implemented granular 3-layer whitelists and security policies for tool/command execution.
- **Worker Configuration**: Increased default zombie IO timeout from 10m to 30m for long-running batch tasks.
- **Feishu Logic**: Redacted sensitive URL parameters in SDK logs and optimized card streaming state management.
- **Webchat**: Replaced AI SDK with a custom, high-performance WebSocket client.

### Fixed

- **Onboard Verification**: `stepVerify` now loads `.env` into process environment before running checkers, fixing false-positive "Missing required fields" for JWT secret.
- **CI Portability**: Tests now pass on GitHub Actions Linux runners — removed OS-specific hardcoding (`darwin`), fixed work directory validation, and resolved bot ID isolation test inconsistencies.
- **Claude Code Worker**: Resolved `ResetContext` bug that caused "done" event leaks during session resets.
- **Event Ordering**: Restored deterministic event sequencing in `Hub.Run` to eliminate flaky E2E test failures.
- **Media Management**: Implemented automatic cleanup for temporary media files in the Slack adapter.
- **Stability**: Added panic recovery to all gateway handlers and messaging adapters.
- **macOS Compatibility**: Fixed pipe `EAGAIN` handling and adjusted `RLIMIT_AS` (address space) constraints.
- **Slack**: Fixed `clearStatus` infinite recursion that called itself instead of `statusMgr.Clear`.
- **Slack**: Fixed help and other worker commands replying to wrong thread — now always replies in the original thread.
- **Slack**: Deduplicated emoji decoration in context usage display (skills list).
- **Messaging**: Serialized reset commands with message handling to prevent first message loss after reset.
- **Test**: Fixed `TestAnswersToArrays` flaky test caused by non-deterministic map iteration order.

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
