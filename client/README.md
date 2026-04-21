# HotPlex Worker Go Client

> Go client SDK for HotPlex Worker Gateway — AEP v1 WebSocket protocol

[![Go Reference](https://pkg.go.dev/badge/github.com/hotplex/hotplex-go-client.svg)](https://pkg.go.dev/github.com/hotplex/hotplex-go-client)

## Installation

```bash
go get github.com/hotplex/hotplex-go-client
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    client "github.com/hotplex/hotplex-go-client"
)

func main() {
    ctx := context.Background()

    c, err := client.New(ctx,
        client.URL("ws://localhost:8888"),
        client.WorkerType("claude_code"),
        client.AuthToken(os.Getenv("HOTPLEX_TOKEN")),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    ack, err := c.Connect(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Session: %s | State: %s\n", ack.SessionID, ack.State)

    go func() {
        for evt := range c.Events() {
            switch evt.Type {
            case client.EventMessageDelta:
                if d, ok := evt.Data.(map[string]any); ok {
                    fmt.Print(d["content"])
                }
            case client.EventDone:
                if done, ok := evt.AsDoneData(); ok {
                    fmt.Printf("\n--- done (success: %v) ---\n", done.Success)
                }
                os.Exit(0)
            case client.EventError:
                if errData, ok := evt.AsErrorData(); ok {
                    fmt.Fprintf(os.Stderr, "error %s: %s\n", errData.Code, errData.Message)
                }
                os.Exit(1)
            }
        }
    }()

    if err := c.SendInput(ctx, "What is 2+2?"); err != nil {
        log.Fatal(err)
    }
    select {}
}
```

## API

### Options

Functional options pattern, passed to `New`:

```go
client.URL("ws://localhost:8888")           // required
client.WorkerType("claude_code")            // required
client.AuthToken("jwt-token")               // JWT bearer token
client.APIKey("sk-xxx")                     // API key header
client.PingInterval(30 * time.Second)       // heartbeat (default 54s)
client.ClientSessionID("my-session-001")    // client-managed session ID (UUIDv5 mapped)
client.AutoReconnect(true)                  // enable automatic reconnection
client.Logger(slog.Default())               // custom logger
client.Metadata(map[string]any{"p": "v"})   // init handshake metadata
```

### Connection

```go
// New session
ack, err := c.Connect(ctx)                  // returns *InitAckData

// Resume existing session
ack, err := c.Resume(ctx, "sess_xxx")       // returns *InitAckData
```

### Sending

```go
c.SendInput(ctx, "your message", metadata)               // user input + opt. metadata
c.SendPermissionResponse(ctx, "id", true, "approved")    // approve tool
c.SendQuestionResponse(ctx, "id", answers)               // answer question
c.SendElicitationResponse(ctx, "id", "accept", content)  // respond to elicit
c.SendControl(ctx, "terminate")                           // terminate session
c.SendReset(ctx, "user_requested")                       // clear context, restart worker
c.SendGC(ctx, "user_idle")                               // archive session, terminate worker
```

### Events

```go
for evt := range c.Events() {
    // evt.Type    — event type string (see constants below)
    // evt.Seq     — monotonic sequence number
    // evt.Session — session ID
    // evt.Data    — event payload (use helpers below)

    if done, ok := evt.AsDoneData(); ok { /* ... */ }
    if err, ok := evt.AsErrorData(); ok { /* ... */ }
    if tc, ok := evt.AsToolCallData(); ok { /* ... */ }
}
```

### Lifecycle

```go
c.SessionID()     // current session ID
c.State()         // current SessionState
c.Close()         // graceful shutdown
```

## Event Kinds

| Constant | Description |
|----------|-------------|
| `EventMessageStart` | Streaming message begins |
| `EventMessageDelta` | Streaming content chunk |
| `EventMessageEnd` | Streaming message ends |
| `EventToolCall` | Worker requests tool execution |
| `EventToolResult` | Tool execution result |
| `EventPermissionRequest` | Worker asks for permission |
| `EventState` | Session state changed |
| `EventDone` | Session completed |
| `EventError` | Error occurred |
| `EventControl` | Control event |
| `EventPing` | Heartbeat probe |
| `EventPong` | Heartbeat response |
| `EventInitAck` | Connection established |
| `EventRaw` | Passthrough agent data |
| `EventReasoning` | Agent "thinking" tokens |
| `EventStep` | Higher-level task step |
| `EventQuestionRequest` | Worker asks a question |
| `EventElicitationRequest` | MCP elicitation request |

## Data Types

### InitAckData

```go
type InitAckData struct {
    SessionID  string
    State      SessionState
    ServerCaps ServerCaps
    Error      string
}
```

### ServerCaps

```go
type ServerCaps struct {
    ProtocolVersion string
    WorkerType      string
    SupportsResume  bool
    SupportsDelta   bool
    SupportsTool    bool
    SupportsPing    bool
    MaxFrameSize    int
    MaxTurns        int
    Tools           []string
}
```

### Session States

```go
StateCreated    // session initialized
StateRunning    // worker active
StateIdle       // waiting for input
StateTerminated // worker exited
StateDeleted    // GC'd
```

## Token Generation

```go
gen, err := client.NewTokenGenerator(signingKey)
if err != nil { /* ... */ }

// Key formats: PEM file path, 64-char hex, or 44-char base64
token, err := gen.Generate("user-id", []string{"read", "write"}, 1*time.Hour)

// Custom audience (default "gateway")
gen.WithAudience("custom-aud")
```

## Examples

| File | Description |
|------|-------------|
| [`examples/quickstart.go`](examples/quickstart.go) | Minimal connect & chat |
| [`examples/complete.go`](examples/complete.go) | Full features: permissions, stats, resume |
| [`scripts/gen-token/main.go`](scripts/gen-token/main.go) | JWT token generator CLI |

Run an example:

```bash
cd client
HOTPLEX_SIGNING_KEY=<key> go run examples/quickstart.go
```

## Related

- **Protocol Spec**: `docs/architecture/AEP-v1-Protocol.md`
- **Python Client**: `examples/python-client/`
- **TypeScript Client**: `examples/typescript-client/`
- **Java Client**: `examples/java-client/`
