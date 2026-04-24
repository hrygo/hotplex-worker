# Go Client SDK Examples

Progressive examples for [hotplex/client](../), from quickstart to production.

## Quick Start

```bash
export HOTPLEX_API_KEY="test-api-key"
export HOTPLEX_GATEWAY_URL="ws://localhost:8888/ws"

go run ./01_quickstart
```

## Examples

| # | Example | Complexity | What You Learn |
|---|---------|-----------|----------------|
| [01](01_quickstart/) | **Quickstart** | ★☆☆ | Connect, send one message, print response, exit |
| [02](02_streaming_output/) | **Streaming Output** | ★☆☆ | Assemble delta stream into complete response |
| [03](03_multi_turn_chat/) | **Multi-Turn Chat** | ★★☆ | Interactive CLI with stdin loop |
| [04](04_session_resume/) | **Session Resume** | ★★☆ | Persist and resume sessions across connections |
| [05](05_permission_handling/) | **Permission Handling** | ★★☆ | Auto-approve/deny tool permissions by policy |
| [06](06_error_handling/) | **Error Handling** | ★★☆ | Retry, timeout, error classification patterns |
| [07](07_multi_worker/) | **Multi-Worker** | ★★★ | Test all worker types sequentially |
| [08](08_token_generator/) | **Token Generator** | ★★☆ | Generate JWT tokens (standalone tool) |
| [09](09_production/) | **Production** | ★★★ | Full integration: JWT + resume + signals + stats |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HOTPLEX_GATEWAY_URL` | `ws://localhost:8888/ws` | Gateway WebSocket URL |
| `HOTPLEX_API_KEY` | — | API Key for authentication |
| `HOTPLEX_SIGNING_KEY` | — | ES256 signing key (PEM/hex/base64) for JWT auth |
| `HOTPLEX_WORKER_TYPE` | `claude_code` | Worker type (claude_code, opencode_server, acpx) |
| `HOTPLEX_SESSION_ID` | — | Existing session ID (for resume) |
| `HOTPLEX_TASK` | *(varies)* | Task prompt to send |
| `HOTPLEX_AUTO_APPROVE` | — | Set to `1` to auto-approve all permissions |

## Authentication

Examples 01–07 use **API Key** auth (simplest). Examples 08–09 support **JWT** auth.

```bash
# API Key auth (simple).
HOTPLEX_API_KEY="your-key" go run ./01_quickstart

# JWT auth (production).
HOTPLEX_SIGNING_KEY="your-signing-key" go run ./09_production
```

## Run Commands

```bash
go run ./01_quickstart
go run ./02_streaming_output
go run ./03_multi_turn_chat                       # interactive, reads stdin
go run ./04_session_resume
HOTPLEX_AUTO_APPROVE=1 go run ./05_permission_handling
go run ./06_error_handling
go run ./07_multi_worker
HOTPLEX_SIGNING_KEY="key" go run ./08_token_generator -v
HOTPLEX_SIGNING_KEY="key" go run ./09_production
```

## Structure

```
client/examples/
├── 01_quickstart/            # ★ Minimal example
├── 02_streaming_output/      # ★ Stream assembly
├── 03_multi_turn_chat/       # ★★ Interactive CLI
├── 04_session_resume/        # ★★ Session persistence
├── 05_permission_handling/   # ★★ Permission policy
├── 06_error_handling/        # ★★ Error patterns
├── 07_multi_worker/          # ★★★ Multi-worker testing
├── 08_token_generator/       # ★★ JWT token tool
├── 09_production/            # ★★★ Production reference
└── README.md
```
