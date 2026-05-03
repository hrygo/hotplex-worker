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
  /** Context window usage percentage */
  contextPct?: number;
  /** Tool call count this turn */
  toolCallCount?: number;
  /** Turn duration in ms */
  turnDurationMs?: number;
  /** Tool name → call count breakdown */
  toolNames?: Record<string, number>;
}

export interface SessionMetrics {
  /** Aggregated totals for the session */
  totalInputTokens: number;
  totalOutputTokens: number;
  totalLatencyMs: number;
  turnCount: number;
  /** Most recent turn metrics for inline display */
  lastTurn?: TurnMetrics;
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

  const recordTurn = useCallback((stats: Record<string, any>): TurnMetrics => {
    const inputTokens = stats.input_tokens ?? stats.inputTokens ?? 0;
    const outputTokens = stats.output_tokens ?? stats.outputTokens ?? 0;
    const latencyMs = Date.now() - turnStartRef.current;

    const session = stats._session as Record<string, any> | undefined;

    const turnMetrics: TurnMetrics = {
      inputTokens: session?.turn_input_tok ?? inputTokens,
      outputTokens: session?.turn_output_tok ?? outputTokens,
      latencyMs: session?.turn_duration_ms ?? latencyMs,
      model: session?.model_name,
      costUsd: session?.turn_cost_usd,
      contextPct: session?.context_pct,
      toolCallCount: session?.tool_call_count,
      turnDurationMs: session?.turn_duration_ms,
      toolNames: session?.tool_names ?? undefined,
    };

    setSessionMetrics((prev) => ({
      totalInputTokens: prev.totalInputTokens + turnMetrics.inputTokens!,
      totalOutputTokens: prev.totalOutputTokens + turnMetrics.outputTokens!,
      totalLatencyMs: prev.totalLatencyMs + turnMetrics.latencyMs!,
      turnCount: prev.turnCount + 1,
      lastTurn: turnMetrics,
    }));

    return turnMetrics;
  }, []);

  return { sessionMetrics, startTurn, recordTurn };
}
