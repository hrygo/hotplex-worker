# HotPlex WebChat — AGENTS.md

## Tech Stack

- **Framework**: Next.js 16 App Router (Turbopack)
- **Language**: TypeScript (strict mode, target ES2017)
- **UI**: React 19 + Framer Motion + Tailwind CSS v4
- **State**: assistant-ui ExternalStoreAdapter pattern (`@assistant-ui/react` + `@assistant-ui/store`)
- **Transport**: WebSocket via AEP v1 (`BrowserHotPlexClient`)
- **Fonts**: Self-hosted via `next/font/local` + `@fontsource`
- **Testing**: Playwright E2E only (no unit tests)

## Architecture

```
BrowserHotPlexClient (WebSocket/AEP)
  ↕ events: delta, messageStart, done, error, reasoning, toolCall, toolResult,
            permissionRequest, questionRequest, elicitationRequest, contextUsage
  ↓
useHotPlexRuntime (hotplex-runtime-adapter.ts)
  → ExternalStoreAdapter<HotPlexMessage>
  → Messages stored in React useState, persisted to localStorage (debounced 1s)
  ↓
Thread (thread.tsx)
  → AssistantMessage / UserMessage via MessagePrimitive.Parts
  → Tool routing via getToolCategory() → specialized GenUI components
```

## Directory Map

```
app/
  layout.tsx              RootLayout — fonts, NuqsAdapter, dark mode
  page.tsx                Root page — ChatContainer shell
  error.tsx               Error Boundary — prevents white-screen crash
  components/chat/        ChatContainer, SessionPanel, NewSessionModal

components/assistant-ui/
  thread.tsx              Core thread — message rendering, tool routing, composer
  CommandMenu.tsx         Slash command autocomplete
  MarkdownText.tsx        Markdown rendering (rehype-highlight, remark-gfm)
  ContextUsageCard.tsx    Token/turn context dashboard
  TurnSummaryCard.tsx     Turn cost summary card
  tools/                  Specialized tool UI components
    TerminalTool.tsx      CLI output with auto-truncation
    FileDiffTool.tsx      Diff and content viewer
    SearchTool.tsx        Search results display
    PermissionCard.tsx    Approve/Reject permission requests
    CompactToolTab.tsx    Collapsed tool summary tab
    AgentTool.tsx         Sub-agent delegation display
    TodoTool.tsx          Todo list rendering
    ListTool.tsx          Directory listing

lib/
  adapters/
    hotplex-runtime-adapter.ts  Core hook: WS client → ExternalStoreAdapter
  ai-sdk-transport/
    client/browser-client.ts    BrowserHotPlexClient — AEP v1 WebSocket client
    client/types.ts             Event data types (InitConfig, PermissionRequestData, etc.)
    client/constants.ts         WorkerType, WorkerStdioCommand enums
    client/envelope.ts          AEP envelope codec
  api/sessions.ts         REST API client (list/create/delete/history)
  hooks/useSessions.ts     Session lifecycle hook (CRUD, auto-create, active session persistence)
  hooks/useMetrics.ts      Token/latency tracking
  config.ts               Env config (wsUrl, apiKey, workerType, workDir)
  tool-categories.ts      Tool name → category router
  types/message-parts.ts  Shared MessagePart union type
  utils/turn-replay.ts    ConversationRecord → HotPlexMessage converter
```

## Key Patterns

### Message Flow

1. User sends text → `handleNew()` → `client.sendInput()` → optimistic ghost message
2. Server streams back: `messageStart` → `delta` (RAF batched 60fps) → `toolCall`/`toolResult` → `done`
3. Each event updates `messages` state → `adapterMessages` useMemo (dedup) → `threadMessages` useMemo (convert)
4. Authoritative history fetched from server on session switch (<10ms localhost)

### Tool Routing

`thread.tsx` iterates `MessagePrimitive.Parts` → `getToolCategory(toolName)` → switch to specialized component. Adding a new tool category: add to `tool-categories.ts` set + add case in `thread.tsx` switch.

### Session Lifecycle

Mount → `listSessions` → auto-select most recent (or restore from localStorage session ID / URL) → no sessions → auto-create "main".

## Conventions

- **No `useMessage` hook** — always pass `message` as prop from the render loop
- **No `ActionBarPrimitive`** — custom action buttons for full style control
- **Tool keys**: use `toolCallId` for motion keys to prevent jitter during streaming
- **`as any` casts**: minimize — type gaps in assistant-ui interop are tracked in issue #310
- **Imports**: use `@/` path aliases (configured in tsconfig.json)
- **CSS**: Tailwind utility classes via CSS variables (`var(--bg-base)`, `var(--accent-gold)`, etc.)
- **Animation**: Framer Motion only, no CSS animation keyframes except in globals.css
- **Font loading**: `next/font/local` only — no Google Fonts CDN calls

## Common Tasks

| Task | Where | How |
|------|-------|-----|
| Add tool UI | `tools/` + `thread.tsx` switch | New component + category in `tool-categories.ts` |
| Change theme | `app/globals.css` | CSS variable tokens |
| Add AEP event | `browser-client.ts` + adapter | Subscribe in useEffect, update `messages` state |
| Session API change | `api/sessions.ts` | Add/update fetch call |

## Testing

```bash
pnpm build          # Type check + production build (must pass before commit)
pnpm dev            # Dev server at localhost:3000
pnpm exec playwright test   # E2E tests (requires gateway running)
```

No unit test framework — all testing is Playwright E2E or manual verification via `pnpm dev`.
