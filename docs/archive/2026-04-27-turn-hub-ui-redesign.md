# Spec: HotPlex Turn-Hub UI Redesign

**Date**: 2026-04-27
**Status**: Approved
**Topic**: High-Premium Turn-Based UI for AI Coding Sessions

## 1. Overview
Redesign the HotPlex Webchat interface to move from a linear message list to a structured "Turn-Hub" architecture. This redesign focuses on increasing layout stability, improving information hierarchy, and infusing an "Organic Neural Flow" visual style.

## 2. Goals
- **Structural Clarity**: Group user intent and assistant actions into a single visual unit (Turn).
- **Reduced Cognitive Load**: Automatically compact historical tool outputs to focus on the current task.
- **Premium Aesthetics**: Implement high-fidelity organic animations and glassmorphism.
- **Layout Stability**: Eliminate content jumps during streaming through better lifecycle management.

## 3. Architecture: The Turn-Hub
Each conversation turn is encapsulated in a `TurnContainer`.

### 3.1 TurnContainer Structure
- **TurnRoot**: The master wrapper with a subtle gradient background and soft outer glow.
- **Life-Line**: A vertical gradient line on the left that connects the entire turn.
- **UserSection**: Displays the user's message and a system-derived "Intent Label".
- **ToolChain**: A vertical list of tool calls.
- **ConclusionSection**: The final assistant text response.

## 4. Components & Lifecycles

### 4.1 Smart Tool Compaction
Tools transition through three visual states:
1.  **Executing (Active)**: Full visibility, skeleton screens, pulsing status indicators.
2.  **Resolved (Active)**: Full visibility of the result (e.g., terminal output, file diff).
3.  **Compacted (History)**: Once the next tool or text part starts, the previous tool shrinks into a 32px height `CompactTab` showing a summary (e.g., `✓ Bash: make build`).

### 4.2 Organic Flow Animations
- **The Pulse**: The Life-Line pulses with light as content is generated.
- **Quantum Transition**: The "Quantum Thinking" orb dissolves and "flows" into the Life-Line when text generation begins.
- **Expansion**: Compacted tools expand with a spring-based animation when clicked.

## 5. Technical Implementation Details
- **CSS System**: Use CSS Variables for all theme colors (defined in `index.css`).
- **State Management**: Leverage `@assistant-ui/react` part statuses to trigger transitions.
- **Animation Library**: `framer-motion` for all layout transitions and pulse effects.

## 6. Implementation Plan (High-Level)
1.  Update `index.css` with new design tokens (gradients, blurs, neural-colors).
2.  Create `TurnContainer` wrapper component in `assistant-ui/`.
3.  Implement `CompactToolTab` for collapsed tool states.
4.  Refactor `thread.tsx` to group parts into `TurnContainers`.
5.  Apply "Organic Flow" animations to the Life-Line and placeholders.
