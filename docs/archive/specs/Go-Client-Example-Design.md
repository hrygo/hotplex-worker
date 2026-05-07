---
type: spec
tags:
  - project/HotPlex
  - client/go
  - example
date: 2026-04-03
status: implemented
progress: 100
completion_date: 2026-04-03
estimated_hours: 12
---

# Go Client Example Module — Design Spec

**Date:** 2026-04-03
**Status:** Approved

## Overview

Create a Go client module under `client/` that demonstrates how to connect to the HotPlex Worker Gateway via WebSocket using the AEP v1 protocol. The module is an independent sub-module (own `go.mod`) that imports the `hotplex` module as a third-party library, mirroring how real Go clients would consume the gateway.

## Goals

- Go idiomatic API: `context.Context`, channels for events, functional options
- Minimal quickstart (~60 lines) demonstrating the core connect → send → receive → done flow
- Complete example demonstrating all event types, reconnect, permissions, session resume
- Token generation script demonstrating ES256 JWT token creation

## Non-Goals

- Production-grade client library (error recovery, backpressure, full reconnection logic)
- Multi-worker type support beyond what's needed for examples
- TLS/mTLS configuration

## Directory Structure

```
client/
├── go.mod                          # Independent module
├── client/
│   ├── client.go                   # Client core: WebSocket, send/recv pumps
│   ├── options.go                  # Functional options
│   ├── events.go                  # Inbound/outbound event types
│   └── token.go                   # ES256 JWT token generation
├── examples/
│   ├── quickstart.go               # ~60 lines: minimal demo
│   └── complete.go                 # ~300 lines: full feature demo
└── scripts/
    └── gen-token/
        └── main.go                 # Standalone token generation tool
```

## API Design

### Client

```go
// Option configures a Client.
type Option func(*Client) error

// URL sets the WebSocket gateway URL.
func URL(rawurl string) Option

// WorkerType sets the worker type (e.g., "claude-code").
func WorkerType(t string) Option

// AuthToken sets the JWT auth token.
func AuthToken(token string) Option

// Reconnect enables auto-reconnect with max attempts.
func Reconnect(maxAttempts int, baseDelay, maxDelay time.Duration) Option

// New creates a new client with the given options.
func New(ctx context.Context, opts ...Option) (*Client, error)

// Events returns a receive-only channel of inbound events.
func (c *Client) Events() <-chan Event

// SendInput sends a user input event.
func (c *Client) SendInput(ctx context.Context, content string) error

// SendPermissionResponse approves or denies a tool permission.
func (c *Client) SendPermissionResponse(ctx context.Context, id string, approved bool, reason string) error

// SendControl sends a control event (terminate, delete).
func (c *Client) SendControl(ctx context.Context, action string) error

// Resume attaches to an existing session.
func (c *Client) Resume(ctx context.Context, sessionID string) error

// Close gracefully closes the client.
func (c *Client) Close() error
```

### Event

```go
// Event is an inbound event from the gateway.
type Event struct {
    Type    string      // "delta", "done", "error", "state", "tool_call", "tool_result", "message", "permission_request", "reconnect", "throttle"
    Session string      // session_id
    Seq     int64       // sequence number
    Data    interface{} // type-specific payload
}

// Type constants.
const (
    TypeDelta             = "message.delta"
    TypeMessage           = "message"
    TypeDone              = "done"
    TypeError             = "error"
    TypeState             = "state"
    TypeToolCall          = "tool_call"
    TypeToolResult        = "tool_result"
    TypePermissionRequest = "permission_request"
    TypeReconnect         = "reconnect"
    TypeThrottle          = "throttle"
    TypePing              = "ping"
    TypePong              = "pong"
)
```

## Example Programs

### quickstart.go (~60 lines)

1. Generate ES256 JWT token using `client.GenerateToken`
2. Create client with URL, worker type, auth token
3. Call `client.Connect()` (new session) or `client.Resume()`
4. Range over `client.Events()` channel:
   - `TypeDelta`: print content to stdout
   - `TypeDone`: print stats, close client, exit
   - `TypeError`: print error, exit with code 1
5. `client.SendInput(ctx, task)`

### complete.go (~300 lines)

Covers all event types, plus:

- Reconnect with `Option.Reconnect`
- Session resume via env var `HOTPLEX_SESSION_ID`
- Permission request auto-approve list
- Tool call monitoring
- Stats display (duration, tokens, cost)
- Graceful SIGINT/SIGTERM shutdown
- Session terminate on shutdown

### scripts/gen-token/main.go

Reads gateway signing key from file or env var, generates ES256 JWT with:
- `iss: hotplex`
- `aud: gateway`
- `sub: example-user`
- `exp: 1 hour from now`
- `scopes: ["read", "write"]`

## Token Generation

Use ECDSA P-256 (ES256) signing. Reference `internal/security/jwt.go` for key derivation pattern:

```go
// DeriveECDSAP256Key derives an ECDSA P-256 private key from a byte seed.
func DeriveECDSAP256Key(seed []byte) (*ecdsa.PrivateKey, error)
```

## Dependencies

- `github.com/golang-jwt/jwt/v5` — JWT parsing and signing
- `github.com/google/uuid` — JTI generation
- Standard library: `net`, `net/http`, `golang.org/x/net/websocket`, `context`, `crypto/ecdsa`

## Key Implementation Notes

- Use `golang.org/x/net/websocket` for WebSocket (stdlib-compatible)
- NDJSON parsing: each WebSocket frame is one JSON line, use `json.Decoder` per frame
- Send pump: goroutine reading from buffered channel, `json.Marshal` → WebSocket write
- Receive pump: `websocket.Message.Receive` → `DecodeLine` → `Events()` channel
- Client closes receive pump via context cancellation
- Backpressure: send channel buffered (capacity 100), block on full send

## Open Questions

None — all resolved during brainstorming.
