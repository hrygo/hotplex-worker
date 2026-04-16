# @hotplex/ai-sdk-transport

## OVERVIEW
TypeScript package bridging Vercel AI SDK `useChat` hook to HotPlex Gateway's AEP v1 WebSocket protocol. Three entry points: main barrel, server, client.

## STRUCTURE
```
src/
  index.ts              # Barrel: re-exports from transport/ + shared types
  server/
    index.ts            # Server barrel
    route-handler.ts    # createHotPlexHandler: framework-agnostic HTTP → WS bridge
  client/
    index.ts            # Client barrel
    browser-client.ts   # BrowserHotPlexClient: WebSocket lifecycle, AEP event parsing
    types.ts            # Client-specific types
    envelope.ts         # AEP envelope encode/decode for browser
  transport/
    chunk-mapper.ts     # AEP events → AI SDK data stream chunks
    stream-controller.ts # ReadableStream management, backpressure
  __tests__/            # Vitest tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new AEP → AI SDK mapping | `transport/chunk-mapper.ts` | Map AEP event kinds to AI SDK stream parts |
| Change server-side handler | `server/route-handler.ts` | createHotPlexHandler: accepts messages, returns streaming Response |
| Browser WebSocket client | `client/browser-client.ts` | Connect, send/receive AEP events, auto-reconnect |
| Envelope codec | `client/envelope.ts` | NDJSON encode/decode for browser WebSocket |
| Shared types | `client/types.ts` | Client config, event data types |

## KEY PATTERNS

**Three exports (package.json exports map)**
- `@hotplex/ai-sdk-transport` → main barrel (transport utils)
- `@hotplex/ai-sdk-transport/server` → createHotPlexHandler
- `@hotplex/ai-sdk-transport/client` → BrowserHotPlexClient

**Event mapping (chunk-mapper.ts)**
- message.start → text-start
- message.delta → text-delta
- message.end → text-end
- tool_call → tool-input-start + tool-input-delta
- done → finish
- error → error

**Server route pattern**
- createHotPlexHandler returns async function accepting `{ messages: UIMessage[] }`
- Internally: creates BrowserHotPlexClient → pipes through chunk-mapper → returns streaming Response

**Peer dependencies**
- ai (>=4.0.0) required
- react + @ai-sdk/react optional (for client-side hooks)

## COMMANDS
```bash
pnpm build          # tsc
pnpm test           # vitest run
pnpm test:watch     # vitest
```
