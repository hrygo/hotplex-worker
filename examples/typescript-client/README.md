# HotPlex TypeScript Client

> TypeScript/Node.js client SDK for HotPlex Gateway

[![npm version](https://img.shields.io/npm/v/@hotplex/client.svg)](https://www.npmjs.com/package/@hotplex/client)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

---

## Features

- 🚀 **Full AEP v1 Support** - Complete implementation of Agent Exchange Protocol
- 🔄 **Auto-Reconnection** - Exponential backoff with configurable retry limits
- 📡 **Event-Driven API** - Clean EventEmitter-based event handling
- 🎯 **Type-Safe** - Full TypeScript type definitions
- 🔧 **Zero Dependencies** - Minimal deps (only `ws` and `eventemitter3`)
- 🧪 **Well-Tested** - Comprehensive unit and integration tests

---

## Installation

### npm

```bash
npm install @hotplex/client
```

### yarn

```bash
yarn add @hotplex/client
```

### From Source

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex/examples/typescript-client
npm install
npm run build
```

---

## Quick Start

### Minimal Example

```typescript
import { HotPlexClient, WorkerType } from "@hotplex/client";

const client = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.CLAUDE_CODE,
  authToken: process.env.HOTPLEX_API_KEY,
});

// Handle streaming output
client.on("message_delta", (data) => {
  process.stdout.write(data.content);
});

// Handle completion
client.on("done", (data) => {
  console.log(`\n✅ Done! Success: ${data.success}`);
  client.close();
});

// Connect and send
(async () => {
  try {
    await client.connect();
    await client.sendInput("Write a hello world in TypeScript");
  } catch (err) {
    console.error("Error:", err);
    process.exit(1);
  }
})();
```

### Run Example

```bash
# Terminal 1: Start gateway
./hotplex

# Terminal 2: Run example
cd examples/typescript-client
npm install
export HOTPLEX_API_KEY="your-api-key"
npx tsx examples/quickstart.ts
```

---

## API Reference

### Constructor

```typescript
new HotPlexClient(config: ClientConfig)
```

#### ClientConfig

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `url` | `string` | ✅ | - | Gateway WebSocket URL (e.g., `ws://localhost:8888`) |
| `workerType` | `WorkerType` | ✅ | - | Worker type (`CLAUDE_CODE`, `OPENCODE_SERVER`, etc.) |
| `authToken` | `string` | ❌ | - | API key or JWT token |
| `sessionId` | `string` | ❌ | auto | Resume existing session |
| `reconnect` | `boolean` | ❌ | `true` | Enable auto-reconnection |
| `reconnectMaxAttempts` | `number` | ❌ | `5` | Max reconnection attempts |
| `timeout` | `number` | ❌ | `30000` | Connection timeout (ms) |
| `metadata` | `Record<string, any>` | ❌ | `{}` | Session metadata |

### Methods

#### connect()

Establishes WebSocket connection and initializes session.

```typescript
await client.connect(): Promise<InitAckData>
```

**Returns**: `InitAckData`
```typescript
{
  sessionId: string;
  status: "ok";
}
```

#### sendInputAsync()

Send user input and wait for the task to complete (or fail).

```typescript
await client.sendInputAsync(content: string, metadata?: Record<string, any>): Promise<void>
```

**Example**:
```typescript
try {
  await client.sendInputAsync("Write a hello world in Go");
  console.log("Task finished successfully");
} catch (err) {
  if (err instanceof TimeoutError) {
    console.error("Task timed out");
  } else {
    console.error("Task failed:", err.message);
  }
}
```

#### sendToolResult()

Send tool execution result.

```typescript
await client.sendToolResult(id: string, output: unknown, error?: string): Promise<void>
```

**Example**:
```typescript
await client.sendToolResult("call_123", JSON.stringify({ files: ["main.go"] }));
```

#### sendPermissionResponse()

Send permission approval/denial.

```typescript
await client.sendPermissionResponse(permissionId: string, allowed: boolean, reason?: string): Promise<void>
```

**Example**:
```typescript
await client.sendPermissionResponse("perm_456", true, "User approved");
```

#### close()

Close connection and cleanup resources.

```typescript
client.close();
```

### Events

All events use EventEmitter3.

#### Message Events

| Event | Data Type | Description |
|-------|-----------|-------------|
| `message.start` | `MessageStartData` | Emitted when a new message stream starts |
| `message.delta` | `MessageDeltaData` | Streaming content chunks (most common) |
| `message.end` | `MessageEndData` | Emitted when message stream ends |

#### Lifecycle Events

| Event | Data Type | Description |
|-------|-----------|-------------|
| `state` | `StateData` | Session state changed (`running`, `idle`, etc.) |
| `done` | `DoneData` | Task completed with success status and stats |
| `error` | `ErrorData` | Protocol-level error occurred |

#### Connection Events

| Event | Data Type | Description |
|-------|-----------|-------------|
| `connected` | `InitAckData` | WebSocket connected and session initialized |
| `disconnected` | `string` | WebSocket disconnected with reason |
| `reconnecting` | `number` | Attempting to reconnect (current attempt) |

---

## Advanced Usage

### Session Resumption

Resume an existing session within its retention period.

```typescript
const client = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.ClaudeCode,
});

// Connect to existing session
await client.resume("sess_existing_uuid");
```

### Streaming Message Collection

Collect streaming deltas into a full message:

```typescript
let fullMessage = "";

client.on("message.delta", (data) => {
  fullMessage += data.content;
});

client.on("message.end", () => {
  console.log("Full Message:", fullMessage);
});
```

### Tool Implementation

Handle tool calls from the worker:

```typescript
client.on("tool_call", async (data) => {
  console.log(`Tool call: ${data.name}`);
  const result = await myToolRunner(data.name, data.input);
  
  await client.sendToolResult(data.id, result);
});
```

---

## Error Handling

### Custom Error Classes

The client provides several error classes for different failure modes:

```typescript
import {
  HotPlexError,
  ConnectionError,
  SessionError,
  TimeoutError,
  ProtocolError,
} from "@hotplex/client";

try {
  await client.connect();
} catch (err) {
  if (err instanceof ConnectionError) {
    console.error("Network issue:", err.message);
  } else if (err instanceof SessionError) {
    console.error("Gateway rejected session:", err.code);
  }
}
```

### Error Events vs Exceptions

- **Exceptions**: Thrown by `connect()`, `resume()`, and `sendInputAsync()`. These are usually terminal or require immediate action.
- **`error` events**: Emitted for asynchronous protocol errors that don't necessarily break the connection.

```typescript
client.on("error", (data) => {
  console.error(`Protocol error [${data.code}]: ${data.message}`);
});
```

---

## Testing

### Run Tests

```bash
npm test                 # Unit tests
npm run test:coverage    # Coverage report
npm run test:integration # Integration tests (requires gateway)
```

### Test Utilities

```typescript
import { createTestClient, waitForEvent } from "@hotplex/client/testing";

describe("MyClient", () => {
  it("should handle messages", async () => {
    const client = createTestClient();

    await client.connect();
    await client.sendInput("test");

    const done = await waitForEvent(client, "done", 5000);
    expect(done.success).toBe(true);
  });
});
```

---

## Performance

### Memory Management

The client automatically manages memory:
- Clears message buffers after `message.end`
- Limits pending message queue (configurable)
- Cleans up event listeners on close

### Backpressure

When the server is overloaded, it may drop `message_delta` events. The client:
- Continues processing (no exceptions)
- Can detect gaps in `message.end` handler
- Should implement retry logic if needed

### Connection Pooling

For multiple sessions, create separate client instances:

```typescript
const clients = await Promise.all([
  new HotPlexClient(config).connect(),
  new HotPlexClient(config).connect(),
  new HotPlexClient(config).connect(),
]);

// Use clients in parallel
await Promise.all(
  clients.map(c => c.sendInput("Task..."))
);
```

---

## Troubleshooting

### Connection Refused

```
Error: Connection refused ws://localhost:8888
```

**Solution**: Check if gateway is running:
```bash
curl http://localhost:9999/admin/health
```

### No Events Received

**Symptoms**: Connected but no `message_delta` events

**Debug**:
```typescript
client.on("state", (data) => {
  console.log("State:", data.state);
});

client.on("error", (data) => {
  console.error("Error:", data);
});
```

### Authentication Failed

```
Error [UNAUTHORIZED]: Invalid API key
```

**Solution**: Check `authToken` matches gateway config:
```typescript
const client = new HotPlexClient({
  authToken: process.env.HOTPLEX_API_KEY, // Ensure this is set
});
```

### TypeScript Errors

```
Property 'sendInput' does not exist on type 'HotPlexClient'
```

**Solution**: Ensure correct import:
```typescript
import { HotPlexClient } from "@hotplex/client";
// not
// import { Client } from "@hotplex/client";
```

---

## Development

### Build

```bash
npm run build        # Compile TypeScript
npm run build:watch  # Watch mode
```

### Lint

```bash
npm run lint         # Check issues
npm run lint:fix     # Auto-fix
```

### Generate Docs

```bash
npm run docs         # Generate API docs
```

---

## Architecture

```
┌─────────────────────────────────────────┐
│         HotPlexClient                   │
│  - Event registration (on/off/emit)     │
│  - Message builders (sendInput, etc)    │
│  - State management                     │
├─────────────────────────────────────────┤
│         Transport (WebSocket)           │
│  - Connection lifecycle                 │
│  - Auto-reconnect with backoff          │
│  - Message queue                        │
├─────────────────────────────────────────┤
│         Protocol (AEP v1)               │
│  - NDJSON codec                         │
│  - Envelope builder                     │
│  - Event type definitions               │
└─────────────────────────────────────────┘
```

**Source Files**:
- `client.ts`: High-level client API
- `envelope.ts`: AEP message codec
- `types.ts`: TypeScript definitions
- `constants.ts`: Protocol constants

---

## Comparison with Python Client

| Feature | TypeScript | Python |
|---------|-----------|--------|
| Async Model | `async/await` | `async/await` |
| Event System | EventEmitter3 | Decorator callbacks |
| Type System | interface + generic | dataclass + TypeVar |
| Reconnect | Auto (exponential backoff) | Manual |
| Testing | Vitest | pytest |
| Package Size | ~50KB | ~30KB |

---

## Examples

See [`examples/`](examples/) directory:

- [`quickstart.ts`](examples/quickstart.ts): Minimal example
- [`complete.ts`](examples/complete.ts): Full-featured demo

---

## Related

- **Protocol Spec**: `docs/architecture/AEP-v1-Protocol.md`
- **Python Client**: `examples/python-client/`
- **Go Client**: `client/`
- **Java Client**: `examples/java-client/`

---

## License

Apache-2.0

---

## Support

- **Issues**: https://github.com/hrygo/hotplex/issues
- **Docs**: https://hotplex.dev/docs/client/typescript
