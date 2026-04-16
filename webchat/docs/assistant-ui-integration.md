# assistant-ui Integration Guide

## Overview

This document describes the integration of `@assistant-ui/react` into the HotPlex webchat application using a clean architecture approach.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Presentation Layer                       │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  assistant-ui Components                              │  │
│  │  • Thread (message list)                              │  │
│  │  • Composer (input)                                   │  │
│  │  • AssistantRuntimeProvider                          │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          ↓ ↑
┌─────────────────────────────────────────────────────────────┐
│                      Adapter Layer                           │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  HotPlexRuntimeAdapter                                │  │
│  │  • Implements ExternalStoreAdapter<T>                 │  │
│  │  • Converts AEP v1 events → assistant-ui messages     │  │
│  │  • Maps assistant-ui operations → HotPlex client      │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          ↓ ↑
┌─────────────────────────────────────────────────────────────┐
│                   Infrastructure Layer                       │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  BrowserHotPlexClient (from @hotplex/ai-sdk-transport)│  │
│  │  • WebSocket connection management                    │  │
│  │  • AEP v1 protocol implementation                    │  │
│  │  • Event emission (delta, done, error)                │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Key Design Decisions

### 1. **ExternalStoreAdapter Pattern**

We use assistant-ui's `ExternalStoreAdapter` pattern instead of direct Runtime implementation because:
- ✅ Simpler integration with existing state management
- ✅ Clear separation between UI and business logic
- ✅ Easier testing and maintenance
- ✅ Protocol-agnostic design

### 2. **Clean Architecture**

The adapter layer is responsible for:
- **Event Translation**: HotPlex events → assistant-ui messages
- **Operation Mapping**: assistant-ui operations → HotPlex client methods
- **State Management**: Single source of truth for messages
- **Error Handling**: Unified error representation

### 3. **Incremental Migration**

Both implementations coexist:
- Old: `ChatContainer.tsx` (custom implementation)
- New: `ChatContainer.assistant-ui.tsx` (assistant-ui)

Switch by changing the import in `app/page.tsx`.

## File Structure

```
webchat/
├── app/
│   ├── page.tsx                              # Current (custom UI)
│   ├── page.assistant-ui.tsx                 # New (assistant-ui)
│   └── components/chat/
│       ├── ChatContainer.tsx                 # Current implementation
│       └── ChatContainer.assistant-ui.tsx    # New implementation
├── lib/
│   └── adapters/
│       └── hotplex-runtime-adapter.ts        # Core adapter
└── package.json
```

## Usage

### Switching to assistant-ui

1. **Update `app/page.tsx`**:

```typescript
// OLD
const ChatUI = dynamic(() => import('./components/chat/ChatContainer'), {
  ssr: false,
  // ...
});

// NEW
const ChatUI = dynamic(() => import('./components/chat/ChatContainer.assistant-ui'), {
  ssr: false,
  // ...
});
```

2. **Start the development server**:

```bash
npm run dev
```

3. **Verify**:
   - Messages appear correctly
   - Streaming works (delta events)
   - Stop/cancel works
   - Error messages display

### Configuration

Environment variables (`.env.local`):

```env
NEXT_PUBLIC_HOTPLEX_WS_URL=ws://localhost:8888/ws
NEXT_PUBLIC_HOTPLEX_WORKER_TYPE=claude_code
NEXT_PUBLIC_HOTPLEX_API_KEY=dev
```

## Implementation Details

### HotPlexRuntimeAdapter

The core adapter (`lib/adapters/hotplex-runtime-adapter.ts`) implements:

#### 1. **Message State Management**

```typescript
const [messages, setMessages] = useState<HotPlexMessage[]>([]);
const [isRunning, setIsRunning] = useState(false);
```

#### 2. **Event Handlers**

| HotPlex Event | assistant-ui Action |
|---------------|---------------------|
| `delta` | Append to last assistant message (streaming) |
| `done` | Mark message as complete |
| `error` | Add system error message |
| `connected` | Log connection status |
| `disconnected` | Reset running state |

#### 3. **Operations**

| assistant-ui Operation | HotPlex Action |
|------------------------|----------------|
| `onNew(message)` | `client.sendInput(textContent)` |
| `onCancel()` | `client.sendControl('terminate')` |

#### 4. **Message Conversion**

```typescript
function convertToThreadMessage(
  message: HotPlexMessage,
  idx: number
): ThreadMessageLike {
  return {
    id: message.id,
    role: message.role,
    content: [{ type: 'text', text: message.content }],
    createdAt: message.createdAt,
    status: message.status === 'streaming'
      ? { type: 'running' }
      : { type: 'complete' },
  };
}
```

## Testing Checklist

- [ ] WebSocket connects successfully
- [ ] User messages send correctly
- [ ] Assistant messages stream (deltas)
- [ ] Message completes (done event)
- [ ] Errors display correctly
- [ ] Cancel/stop works
- [ ] Auto-scroll functions
- [ ] Keyboard shortcuts (Enter, Shift+Enter, Escape)

## Benefits

### Before (Custom Implementation)
- ✅ Full control
- ✅ Lightweight
- ❌ Manual state management
- ❌ Custom UI components
- ❌ Manual accessibility

### After (assistant-ui)
- ✅ Production-tested UI
- ✅ Built-in accessibility
- ✅ Rich component library
- ✅ Community support
- ✅ Future-proof API
- ⚠️ Additional dependency

## Performance

- **Bundle size**: +~50KB (gzipped)
- **Runtime overhead**: Minimal (React state only)
- **WebSocket**: No change (same transport)

## Migration Path

### Phase 1: Parallel Development (Current)
- Both implementations coexist
- Test assistant-ui version
- Gather feedback

### Phase 2: Feature Parity
- Implement missing features
- Custom styling
- Advanced features (tools, attachments)

### Phase 3: Complete Migration
- Deprecate old implementation
- Remove legacy code
- Update documentation

## Troubleshooting

### Common Issues

1. **"Hydration error"**
   - Ensure `ssr: false` in dynamic import
   - Check for browser-only APIs

2. **Messages not streaming**
   - Verify WebSocket connection
   - Check HotPlex gateway logs
   - Inspect `delta` event handlers

3. **"Runtime not found"**
   - Ensure `AssistantRuntimeProvider` wraps components
   - Check `useExternalStoreRuntime` usage

## Resources

- [assistant-ui Documentation](https://github.com/Yonom/assistant-ui)
- [ExternalStoreAdapter API](https://github.com/Yonom/assistant-ui#external-store)
- [HotPlex AEP v1 Protocol](../docs/architecture/WebSocket-Full-Duplex-Flow.md)

## Next Steps

1. ✅ Install assistant-ui
2. ✅ Implement HotPlexRuntimeAdapter
3. ✅ Create new ChatContainer
4. 🔄 Test integration
5. 🔄 Custom styling
6. 🔄 Feature completion (tools, attachments)
7. 🔄 Production deployment
