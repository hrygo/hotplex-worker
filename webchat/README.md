# HotPlex Web Chat

Web chat interface for HotPlex Worker Gateway, built with Next.js App Router and Vercel AI SDK.

The most straightforward way to try HotPlex — just connect and chat with AI coding agents (Claude Code, etc.) through your browser.

## Prerequisites

- A running HotPlex Worker Gateway (`./bin/gateway --dev`)

## Setup

```bash
cd webchat
pnpm install
cp .env.example .env.local
pnpm dev
```

Open [http://localhost:3000](http://localhost:3000)

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HOTPLEX_WS_URL` | Gateway WebSocket URL | `ws://localhost:8888` |
| `HOTPLEX_WORKER_TYPE` | Worker type | `claude_code` |
| `HOTPLEX_AUTH_TOKEN` | Auth token (optional in dev mode) | — |
