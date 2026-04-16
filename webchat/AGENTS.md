# HotPlex Web Chat

## OVERVIEW
Next.js 15 App Router web chat UI using Vercel AI SDK `useChat` hook + `@hotplex/ai-sdk-transport` for AEP v1 WebSocket communication with HotPlex Gateway.

## STRUCTURE
```
app/
  layout.tsx            # Root layout, global CSS
  page.tsx              # Main page, renders ChatContainer
  api/chat/route.ts     # Server-side: createHotPlexHandler → streaming Response
  components/
    chat/
      ChatContainer.tsx # Main chat component: useChat hook + message list + input
      MessageList.tsx   # Scrollable message list
      MessageBubble.tsx # Single message rendering (user/assistant)
      MessageContent.tsx # Markdown rendering with react-markdown
      CodeBlock.tsx     # Syntax-highlighted code blocks (highlight.js)
      ChatInput.tsx     # Input form with send button
      ThinkingIndicator.tsx # Loading/thinking animation
      ErrorMessage.tsx  # Error display component
    ui/
      CopyButton.tsx    # Clipboard copy button
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Change chat behavior | `app/components/chat/ChatContainer.tsx` | useChat hook config, message handling |
| API route | `app/api/chat/route.ts` | createHotPlexHandler from ai-sdk-transport/server |
| Styling | `app/globals.css` | Tailwind CSS v4 |
| Message rendering | `app/components/chat/MessageContent.tsx` | react-markdown + rehype-highlight + remark-gfm |

## KEY PATTERNS

**Data flow**
```
Browser (useChat) → POST /api/chat → createHotPlexHandler → WS to Gateway → AEP events → AI SDK stream → Response
```

**Dependencies**
- `@hotplex/ai-sdk-transport`: linked via `file:../packages/ai-sdk-transport`
- `ai` + `@ai-sdk/react`: Vercel AI SDK core + React hooks
- `next` 15 + `react` 19 + `tailwindcss` 4

**Environment config**
- `HOTPLEX_WS_URL` — Gateway WebSocket URL (default ws://localhost:8888)
- `HOTPLEX_WORKER_TYPE` — Worker type (default claude_code)
- `HOTPLEX_AUTH_TOKEN` — Optional auth token

## COMMANDS
```bash
pnpm install          # Install deps (resolves ai-sdk-transport from ../packages)
pnpm dev              # Start dev server (default :3000)
pnpm build            # Production build
```
