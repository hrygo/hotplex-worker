/**
 * Webchat configuration — single source of truth.
 *
 * All HOTPLEX_WEBCHAT_* env vars are defined here.
 * To add a new config:
 *   1. Add the env var to `.env.example` / `.env.local`
 *   2. Add the field here with a sensible default
 *   3. `next.config.mjs` auto-forwards all HOTPLEX_WEBCHAT_* vars to the client
 */

import type { WorkerType } from "@/lib/ai-sdk-transport/client/constants";

// -- Gateway -----------------------------------------------------------

function resolveWsUrl(): string {
  const envUrl = process.env.HOTPLEX_WEBCHAT_WS_URL;
  if (envUrl) return envUrl;
  // Auto-detect when served from the same Go binary (zero-config).
  if (typeof window !== "undefined") {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${proto}//${window.location.host}/ws`;
  }
  return "ws://localhost:8888/ws";
}

export const wsUrl: string = resolveWsUrl();

export const workerType: WorkerType =
  (process.env.HOTPLEX_WEBCHAT_WORKER_TYPE as WorkerType) ?? "claude_code";

export const apiKey: string =
  process.env.HOTPLEX_WEBCHAT_API_KEY ?? "dev";

// -- Per-session init config -------------------------------------------

export const workDir: string =
  process.env.HOTPLEX_WEBCHAT_WORK_DIR ?? "";

export const rawAllowedTools: string =
  process.env.HOTPLEX_WEBCHAT_ALLOWED_TOOLS ?? "";

export const allowedTools: string[] = rawAllowedTools
  ? rawAllowedTools.split(",").map((s) => s.trim()).filter(Boolean)
  : [];

// -- Derived -----------------------------------------------------------

export function httpBase(): string {
  return (
    wsUrl
      .replace(/^ws:\/\//, "http://")
      .replace(/^wss:\/\//, "https://")
      .replace(/\/ws\/?$/, "")
  );
}
