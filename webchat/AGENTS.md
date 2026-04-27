# HotPlex Web Chat (Hybrid Architecture v5)

## OVERVIEW
Next.js 15 App Router web chat UI. This version implements a **Hybrid Architecture**: combining the visual stability of the original **Industrial Dark Theme** with the advanced **Execution-Path Logic** from the v2 workbench.

## DESIGN PHILOSOPHY
- **Visual Continuity**: Retains the deep charcoal (#050506) and glassmorphism aesthetic preferred for long coding sessions.
- **Smart Collapsing (Functional Regression)**: Automatically compresses completed, non-last tool calls into compact tabs to maintain high information density.
- **Execution Path logic**: Uses a refined tool-routing engine (`getToolCategory`) to render specialized GenUI components.

## STRUCTURE
```
webchat/
  app/
    globals.css         # Original Industrial Dark Theme variables
  components/assistant-ui/
    thread.tsx          # Hybrid Hub: Dark UI + Smart Tool Collapsing Logic
    tools/              # Enhanced Functional Components
      CompactToolTab.tsx # Logic for tool compression
      TerminalTool.tsx   # Advanced CLI output with auto-truncation
      FileDiffTool.tsx   # Robust diff and content parsing
      PermissionCard.tsx # Standardized authorization UX
  lib/
    tool-categories.ts   # Functional tool mapping
```

## KEY FEATURES (回归功能)
- **Smart Execution Path**: Automated state-driven tool collapsing (only the active/last tool stays expanded).
- **Reasoning Duration**: Thinking blocks now include estimated duration and support manual expansion.
- **Safe State Transitions**: Powered by Framer Motion to ensure smooth UI swaps between tool execution and results.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| **UI Styling** | `app/globals.css` | Industrial Dark tokens and bubble styles |
| **Execution Logic** | `components/assistant-ui/thread.tsx` | Message list & Tool collapsing logic |
| **Tool Components** | `components/assistant-ui/tools/` | Re-introduced functional toolsets |

## CONVENTIONS
- **Deprecation Policy**: **Strictly NO use of `useMessage` hook.** Always pass the `message` object as a prop from the loop.
- **Action UI**: Avoid `ActionBarPrimitive`; implement custom action buttons using standard components for better control and styling.
- **Functional Stability**: Maintain the current Dark UI; focus all enhancements on state handling and tool interactivity.
- **Tool Keys**: Always use `toolCallId` for motion keys to prevent jitter during streaming.
