# @hotplex/ai-sdk-transport

AI SDK ChatTransport adapter for HotPlex Worker Gateway (AEP v1 over WebSocket).

## Overview

This package provides a bridge between Vercel AI SDK's `useChat` hook and HotPlex Worker Gateway's WebSocket-based AEP v1 protocol. It enables you to build chat interfaces using HotPlex's AI coding agents (Claude Code, OpenCode CLI) with the familiar AI SDK patterns.

## Architecture

```
┌─────────────────┐     HTTP      ┌─────────────────┐    WebSocket    ┌─────────────────┐
│   React App     │◄─────────────►│  Next.js API    │◄──────────────►│  HotPlex        │
│  useChat Hook   │              │    Route        │                 │  Gateway        │
└─────────────────┘              └─────────────────┘                 └─────────────────┘
                                       │
                                       ├── BrowserHotPlexClient (WebSocket → AEP)
                                       └── mapAepToDataStream (AEP → AI SDK)
```

## Installation

```bash
npm install @hotplex/ai-sdk-transport
# or
pnpm add @hotplex/ai-sdk-transport
```

## Peer Dependencies

```bash
npm install ai @ai-sdk/react react
```

## Quick Start

### 1. Create API Route

```typescript
// app/api/chat/route.ts
import { createHotPlexHandler } from '@hotplex/ai-sdk-transport/server';

const handleChat = createHotPlexHandler({
  url: process.env.HOTPLEX_WS_URL!,
  workerType: 'claude_code',
  authToken: process.env.HOTPLEX_AUTH_TOKEN,
});

export async function POST(req: Request) {
  const body = await req.json();
  return handleChat(body);
}
```

### 2. Create Chat Interface

```tsx
// app/chat/page.tsx
'use client';

import { useChat } from '@ai-sdk/react';

export default function Chat() {
  const { messages, isLoading, input, handleInputChange, handleSubmit } = useChat({
    api: '/api/chat',
  });

  return (
    <div className="flex flex-col h-screen">
      <div className="flex-1 overflow-y-auto p-4">
        {messages.map((message) => (
          <div key={message.id} className="mb-4">
            <div className="font-bold">{message.role}</div>
            <div>{message.content}</div>
          </div>
        ))}
      </div>

      <form onSubmit={handleSubmit} className="p-4 border-t">
        <input
          value={input}
          onChange={handleInputChange}
          placeholder="Ask me anything..."
          className="w-full p-2 border rounded"
          disabled={isLoading}
        />
        <button type="submit" disabled={isLoading}>
          {isLoading ? 'Thinking...' : 'Send'}
        </button>
      </form>
    </div>
  );
}
```

## API Reference

### Server Module (`@hotplex/ai-sdk-transport/server`)

#### `createHotPlexHandler(config)`

Creates a framework-agnostic route handler for HotPlex chat.

**Parameters:**

```typescript
interface HotPlexRouteConfig {
  url: string;           // WebSocket URL of HotPlex gateway
  workerType: string;    // 'claude_code' | 'opencode_cli' | 'opencode_server'
  authToken?: string;    // Optional authentication token
}
```

**Returns:** A handler function that accepts `{ messages: UIMessage[] }` and returns a streaming `Response`.

### Client Module (`@hotplex/ai-sdk-transport/client`)

#### `BrowserHotPlexClient`

Browser-native WebSocket client for HotPlex Gateway.

```typescript
import { BrowserHotPlexClient } from '@hotplex/ai-sdk-transport/client';

const client = new BrowserHotPlexClient({
  url: 'ws://localhost:8888',
  workerType: 'claude_code',
  authToken: 'optional-token',
});

client.on('messageStart', (data) => { /* ... */ });
client.on('delta', (data) => { /* ... */ });
client.on('done', (data) => { /* ... */ });

await client.connect();
client.sendInput('Hello, AI!');
```

### Transport Utilities (`@hotplex/ai-sdk-transport`)

#### `createAepStream(client, abortSignal?)`

Creates a `ReadableStream<string>` that emits AI SDK data stream events from a BrowserHotPlexClient.

#### `mapAepToDataStream(writer, client)`

Maps AEP events from a BrowserHotPlexClient to a data stream writer.

## Event Mapping

AEP events are mapped to AI SDK data stream format:

| AEP Event | AI SDK Event | Description |
|-----------|--------------|-------------|
| `message.start` | `text-start` | Text block started |
| `message.delta` | `text-delta` | Text content received |
| `message.end` | `text-end` | Text block completed |
| `tool_call` | `tool-input-start` + `tool-input-delta` | Tool call |
| `tool_result` | `tool-result` | Tool execution result |
| `reasoning` | `reasoning-delta` | Reasoning content |
| `done` | `finish` | Stream completed |
| `error` | `error` | Error occurred |

## Environment Variables

```bash
HOTPLEX_WS_URL=ws://localhost:8888
HOTPLEX_AUTH_TOKEN=your-token-here
```

## Examples

See the top-level `webchat/` directory for a complete Next.js App Router example.

## License

Apache-2.0
