# HotPlex Worker TypeScript Client

> TypeScript/Node.js client SDK for HotPlex Worker Gateway

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

#### sendInput()

Send user input to worker.

```typescript
await client.sendInput(content: string, metadata?: Record<string, any>): Promise<void>
```

**Example**:
```typescript
await client.sendInput("Write a hello world in Go", {
  language: "go",
  test: true,
});
```

#### sendToolResult()

Send tool execution result.

```typescript
await client.sendToolResult(result: {
  toolCallId: string;
  output: string;
  error?: string;
}): Promise<void>
```

**Example**:
```typescript
await client.sendToolResult({
  toolCallId: "call_123",
  output: JSON.stringify({ files: ["main.go"] }),
});
```

#### sendPermissionResponse()

Send permission approval/denial.

```typescript
await client.sendPermissionResponse(response: {
  permissionId: string;
  allowed: boolean;
  reason?: string;
}): Promise<void>
```

**Example**:
```typescript
await client.sendPermissionResponse({
  permissionId: "perm_456",
  allowed: true,
  reason: "User approved",
});
```

#### close()

Close connection and cleanup resources.

```typescript
await client.close(): Promise<void>
```

### Events

All events use EventEmitter3.

#### Message Events

##### `message.start`

Emitted when a new message stream starts.

```typescript
client.on("message.start", (data: MessageStartData) => {
  console.log("Message started:", data.id);
});
```

**MessageStartData**:
```typescript
{
  id: string;
  role: "assistant";
}
```

##### `message_delta`

Streaming content chunks (most common event).

```typescript
client.on("message_delta", (data: MessageDeltaData) => {
  process.stdout.write(data.content);
});
```

**MessageDeltaData**:
```typescript
{
  content: string;
}
```

##### `message.end`

Emitted when message stream ends.

```typescript
client.on("message.end", (data: MessageEndData) => {
  console.log("Message ended:", data.id);
});
```

#### Tool Events

##### `tool_call`

Worker requests tool execution.

```typescript
client.on("tool_call", async (data: ToolCallData) => {
  console.log(`Tool call: ${data.name}`);

  // Execute tool
  const result = await executeTool(data.name, data.input);

  // Send result back
  await client.sendToolResult({
    toolCallId: data.id,
    output: result,
  });
});
```

**ToolCallData**:
```typescript
{
  id: string;
  name: string;
  input: Record<string, any>;
}
```

#### Permission Events

##### `permission_request`

Worker requests user permission.

```typescript
client.on("permission_request", async (data: PermissionRequestData) => {
  const allowed = await askUser(data.tool_name, data.description);

  await client.sendPermissionResponse({
    permissionId: data.id,
    allowed,
    reason: allowed ? "User approved" : "User denied",
  });
});
```

**PermissionRequestData**:
```typescript
{
  id: string;
  tool_name: string;
  description?: string;
}
```

#### Lifecycle Events

##### `state`

Session state changed.

```typescript
client.on("state", (data: StateData) => {
  console.log("State:", data.state);

  if (data.state === "idle") {
    console.log("Worker idle, ready for input");
  }
});
```

**StateData**:
```typescript
{
  state: "created" | "running" | "idle" | "terminated";
}
```

##### `done`

Task completed.

```typescript
client.on("done", (data: DoneData) => {
  console.log("Done! Success:", data.success);

  if (data.stats) {
    console.log("Duration:", data.stats.duration_ms, "ms");
    console.log("Tokens:", data.stats.total_tokens);
    console.log("Cost: $", data.stats.cost_usd);
  }
});
```

**DoneData**:
```typescript
{
  success: boolean;
  stats?: {
    duration_ms: number;
    total_tokens: number;
    cost_usd: number;
  };
}
```

##### `error`

Error occurred.

```typescript
client.on("error", (data: ErrorData) => {
  console.error(`Error [${data.code}]: ${data.message}`);

  if (data.code === "SESSION_TERMINATED") {
    client.close();
  }
});
```

**ErrorData**:
```typescript
{
  code: string;
  message: string;
  details?: Record<string, any>;
}
```

#### Connection Events

##### `connected`

WebSocket connected (before init).

```typescript
client.on("connected", () => {
  console.log("WebSocket connected");
});
```

##### `disconnected`

WebSocket disconnected.

```typescript
client.on("disconnected", () => {
  console.log("WebSocket disconnected");
});
```

##### `reconnecting`

Attempting to reconnect.

```typescript
client.on("reconnecting", (data: { attempt: number; maxAttempts: number }) => {
  console.log(`Reconnecting ${data.attempt}/${data.maxAttempts}...`);
});
```

---

## Advanced Usage

### Session Resumption

Resume an existing session within its retention period.

```typescript
// First session
const client1 = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.CLAUDE_CODE,
  authToken: "your-key",
});

await client1.connect();
const sessionId = client1.sessionId;
await client1.sendInput("Start a long task...");

// Later: resume session
const client2 = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.CLAUDE_CODE,
  authToken: "your-key",
  sessionId, // Resume
});

await client2.connect();
await client2.sendInput("Continue the task...");
```

### Custom Reconnection Strategy

```typescript
const client = new HotPlexClient({
  url: "ws://localhost:8888",
  workerType: WorkerType.CLAUDE_CODE,
  reconnect: true,
  reconnectMaxAttempts: 10, // Try 10 times
});

client.on("reconnecting", ({ attempt, maxAttempts }) => {
  console.log(`Reconnect attempt ${attempt}/${maxAttempts}`);

  if (attempt >= maxAttempts) {
    console.error("Max reconnection attempts reached");
    client.close();
  }
});
```

### Streaming Message Collection

Collect streaming deltas into full message:

```typescript
let fullMessage = "";
let messageId = "";

client.on("message.start", (data) => {
  messageId = data.id;
  fullMessage = "";
});

client.on("message_delta", (data) => {
  fullMessage += data.content;
  process.stdout.write(data.content); // Still print in real-time
});

client.on("message.end", (data) => {
  console.log("\n--- Full Message ---");
  console.log(fullMessage);
  console.log("--------------------");
});
```

### Tool Implementation Pattern

```typescript
import { exec } from "child_process";
import util from "util";

const execAsync = util.promisify(exec);

client.on("tool_call", async (data) => {
  try {
    let output: string;

    switch (data.name) {
      case "bash":
        const { stdout } = await execAsync(data.input.command, {
          cwd: data.input.cwd || process.cwd(),
          timeout: data.input.timeout || 30000,
        });
        output = stdout;
        break;

      case "read_file":
        const content = await fs.readFile(data.input.path, "utf-8");
        output = content;
        break;

      default:
        throw new Error(`Unknown tool: ${data.name}`);
    }

    await client.sendToolResult({
      toolCallId: data.id,
      output,
    });
  } catch (err) {
    await client.sendToolResult({
      toolCallId: data.id,
      output: "",
      error: err.message,
    });
  }
});
```

### Graceful Shutdown

```typescript
const shutdown = async () => {
  console.log("\nShutting down...");

  try {
    await client.close();
    console.log("Client closed");
    process.exit(0);
  } catch (err) {
    console.error("Error during shutdown:", err);
    process.exit(1);
  }
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
```

---

## Error Handling

### Error Events vs Exceptions

The client emits `error` events for protocol-level errors, but throws exceptions for operational errors.

```typescript
// Protocol error (async, non-fatal)
client.on("error", (data) => {
  console.error("Protocol error:", data.code);
});

// Operational error (sync, fatal if uncaught)
try {
  await client.connect();
} catch (err) {
  console.error("Connection failed:", err);
}
```

### Common Error Codes

| Code | Meaning | Action |
|------|---------|--------|
| `SESSION_NOT_FOUND` | Session doesn't exist | Create new session |
| `SESSION_TERMINATED` | Session terminated | Create new session |
| `SESSION_EXPIRED` | Session past retention | Create new session |
| `UNAUTHORIZED` | Invalid auth token | Check token |
| `INVALID_INPUT` | Malformed input | Check message format |
| `WORKER_TIMEOUT` | Worker took too long | Increase timeout or optimize worker |

### Exception Types

```typescript
import {
  HotPlexError,
  ConnectionError,
  SessionError,
  TimeoutError,
} from "@hotplex/client";

try {
  await client.connect();
} catch (err) {
  if (err instanceof ConnectionError) {
    console.error("Connection failed:", err.message);
  } else if (err instanceof SessionError) {
    console.error("Session error:", err.message);
  }
}
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
