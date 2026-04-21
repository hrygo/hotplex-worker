# [FE] Revamp WebChat: Elegant Out-of-the-Box Messaging UI (AEP v1)

## 🎯 Vision Overview
The current Hotplex WebChat is a functional prototype. We aim to transform it into a **versatile, out-of-the-box messaging interface** that serves as an elegant, lightweight alternative to platforms like Slack or Feishu for interacting with AI agents.

The goal is to provide a **ready-to-use, professional chat experience** that is easy to deploy, simple to use, and visually stunning — focusing on communication efficiency and "batteries-included" reliability.

---

## 🏗️ Core Pillars & Feature Requirements

### 1. Visual Excellence: Modern Messaging Aesthetic
We want an interface that feels like a premium social/work tool.
- **Aesthetic**: Minimalist "Glassmorphism" or "Modern Flat" design. Focus on clarity and brand identity.
- **Motion**: Subtle, non-obtrusive micro-animations for message delivery, typing indicators, and status updates.
- **Typography**: Clean, readable sans-serif fonts optimized for long-form reading and mobile responsiveness.

### 2. High-Efficiency Messaging UX
Focus on the core "Chat" experience.
- **💬 Rich Message Rendering**: Markdown support, code block highlighting (with "Copy" and "Wrap" features), and image/media previews.
- **🧵 Thread & History Management**: Clean session switching and clear chat history navigation.
- **📱 Mobile-First Excellence**: A truly polished experience on mobile browsers (PWA-ready).

### 3. AEP v1 Feature Visualization
Display Agent activity without overwhelming the user.
- **📋 Action Logs**: Instead of raw tool calls, show Agent "actions" (e.g., "Searching...", "Writing...") as clean, unobtrusive status pills.
- **🛡️ Secure Interactions**: Elegant permission dialogs for security-sensitive operations.
- **📂 Simple Asset Handling**: A clear way to view and download files or screenshots produced by the Agent.

### 4. Zero-Config Ready
- **⚡ One-Click Deploy**: Easily deployable on Vercel/Netlify with minimal environment setup.
- **⚙️ Basic Settings**: Allow users to toggle models, themes (Dark/Light), and notification sounds.

---

## 🛠️ Technical Requirements

- **Framework**: Next.js 15 (App Router) + React 19.
- **Styling**: Tailwind CSS 4 + Radix UI Primitives (or Shadcn/UI).
- **AI SDK**: Deep integration with Vercel AI SDK 6.x.
- **Editor**: Monaco Editor or CodeMirror 6 for high-performance diffing/viewing.
- **Animations**: Framer Motion.
- **Testing**: Playwright for E2E WebSocket handshake and streaming validation.

---

## 👤 Ideal Candidate Profile (Internal Recommendation)

To execute this vision, we need a **Product-Minded Frontend Engineer**:
- **Design Eye**: Obsessed with making software feel "clean" and "easy."
- **Messaging Experience**: Understands the nuances of building a chat interface (scroll management, optimistic updates).
- **React/Next.js Expert**: Proficient in the modern ecosystem to ensure a lightweight, fast bundle.

---

## 🎨 Layout Inspiration
- **Sidebar**: Zed.dev / Linear.app
- **Message Cards**: Claude.ai / Perplexity.ai
- **Diffs**: Vercel v0 / GitHub PR view

---

> [!IMPORTANT]
> This is a high-autonomy role. You are free to choose the libraries that best suit the "Zero Latency" and "High Fidelity" goals, provided they align with the Next.js 15 core.
