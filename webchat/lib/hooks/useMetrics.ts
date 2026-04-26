"use client";

import { useCallback, useRef, useState } from "react";

export interface TurnMetrics {
  /** Input tokens consumed */
  inputTokens?: number;
  /** Output tokens generated */
  outputTokens?: number;
  /** Total wall-clock latency in ms */
  latencyMs?: number;
  /** Model name */
  model?: string;
  /** Cost estimate in USD */
  costUsd?: number;
}

export interface SessionMetrics {
  /** Aggregated totals for the session */
  totalInputTokens: number;
  totalOutputTokens: number;
  totalLatencyMs: number;
  turnCount: number;
}

/**
 * Hook to track per-turn and session-aggregate metrics.
 * Metrics are extracted from AEP `done.stats` events.
 */
export function useMetrics() {
  const [sessionMetrics, setSessionMetrics] = useState<SessionMetrics>({
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalLatencyMs: 0,
    turnCount: 0,
  });

  const turnStartRef = useRef<number>(Date.now());

  const startTurn = useCallback(() => {
    turnStartRef.current = Date.now();
  }, []);

  const recordTurn = useCallback((stats: Record<string, any>) => {
    const inputTokens = stats.input_tokens ?? stats.inputTokens ?? 0;
    const outputTokens = stats.output_tokens ?? stats.outputTokens ?? 0;
    const latencyMs = Date.now() - turnStartRef.current;

    setSessionMetrics((prev) => ({
      totalInputTokens: prev.totalInputTokens + inputTokens,
      totalOutputTokens: prev.totalOutputTokens + outputTokens,
      totalLatencyMs: prev.totalLatencyMs + latencyMs,
      turnCount: prev.turnCount + 1,
    }));

    return { inputTokens, outputTokens, latencyMs };
  }, []);

  return { sessionMetrics, startTurn, recordTurn };
}
