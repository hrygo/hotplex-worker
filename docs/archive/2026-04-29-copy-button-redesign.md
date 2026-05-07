# Design Spec: Integrated Edge Copy Button

**Date**: 2026-04-29
**Topic**: Redesigning the Copy Button for Webchat
**Status**: Approved

## 1. Objective
Redesign the `CopyButton` in the webchat module to achieve "natural integration" with message bubbles and provide "strong interaction" feedback.

## 2. UI/UX Design

### 2.1 Placement & Visibility
- **Current**: Absolute positioning at `top: -14px` (floating outside).
- **New**: Absolute positioning at `top: 8px`, `right: 8px` (inside the bubble/body).
- **Invisibility**: Initial state `opacity: 0`, `transform: translateY(-4px)`.
- **Reveal**: On parent `.group` hover, transition to `opacity: 1`, `transform: translateY(0)`.

### 2.2 Visual Styling (Glassmorphism)
- **Background**: `rgba(255, 255, 255, 0.03)` with `backdrop-filter: blur(12px)`.
- **Border**: `1px solid rgba(255, 255, 255, 0.1)`.
- **Border Hover**: Transition to `var(--border-bright)` or `var(--accent-gold)`.
- **Radius**: `var(--radius-sm)` (8px).
- **Typography**: Mono font, `10px`, uppercase, bold tracking.

### 2.3 Interaction States
- **Normal**: `var(--text-muted)`.
- **Success (Copied)**:
  - Text/Icon color: `var(--accent-emerald)`.
  - Border: `rgba(52, 211, 153, 0.3)`.
  - Background: `rgba(52, 211, 153, 0.05)`.
- **Animations (Framer Motion)**:
  - **Press**: `whileTap={{ scale: 0.95 }}`.
  - **Morph**: Layout transition between icons using `AnimatePresence`.
  - **Shimmer**: A CSS/Motion animation that sweeps a highlight across the button on success.

## 3. Implementation Details

### 3.1 Components
- Modify `CopyButton` in `components/assistant-ui/thread.tsx`.
- Add/Update CSS classes in `app/globals.css`.

### 3.2 Constraints
- Ensure `msg-assistant-body` and `msg-user-bubble` have enough padding-right to avoid text overlap.
- Handle z-index to ensure it's clickable above markdown content.

## 4. Success Criteria
1. The button feels like an organic part of the message bubble.
2. The click feedback is tactile and clearly indicates success.
3. Hover behavior is smooth and non-intrusive.
