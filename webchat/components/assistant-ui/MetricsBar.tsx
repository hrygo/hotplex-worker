"use client";

import { motion } from "framer-motion";
import type { TurnMetrics, SessionMetrics } from "@/lib/hooks/useMetrics";
import { formatTokens, formatLatency } from "@/lib/utils";

interface MetricsBarProps {
  turn?: TurnMetrics;
  session?: SessionMetrics;
  compact?: boolean;
}

export function MetricsBar({ turn, session, compact = false }: MetricsBarProps) {
  if (compact && turn) {
    return (
      <div className="flex items-center gap-3 text-[9px] font-mono text-[var(--text-faint)] opacity-0 group-hover:opacity-60 transition-opacity">
        {turn.inputTokens !== undefined && (
          <span>in:{formatTokens(turn.inputTokens)}</span>
        )}
        {turn.outputTokens !== undefined && (
          <span>out:{formatTokens(turn.outputTokens)}</span>
        )}
        {turn.latencyMs !== undefined && (
          <span>{formatLatency(turn.latencyMs)}</span>
        )}
      </div>
    );
  }

  const metrics = session ?? turn;
  if (!metrics) return null;

  const items: Array<{ label: string; value: string; color: string }> = [];

  if ("totalInputTokens" in metrics) {
    const s = metrics as SessionMetrics;
    items.push({ label: "IN", value: formatTokens(s.totalInputTokens), color: "text-[var(--accent-blue)]" });
    items.push({ label: "OUT", value: formatTokens(s.totalOutputTokens), color: "text-[var(--accent-emerald)]" });
    items.push({ label: "TURNS", value: String(s.turnCount), color: "text-[var(--text-faint)]" });
    items.push({ label: "TIME", value: formatLatency(s.totalLatencyMs), color: "text-[var(--text-faint)]" });
  }

  if (items.length === 0) return null;

  return (
    <motion.div
      className="flex items-center gap-3 px-3 py-1.5 rounded-full bg-[var(--bg-glass)] backdrop-blur-xl border border-[var(--border-subtle)]"
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3 }}
    >
      {items.map((item) => (
        <span key={item.label} className="flex items-center gap-1 text-[9px] font-mono">
          <span className="text-[var(--text-faint)] uppercase">{item.label}</span>
          <span className={`font-bold ${item.color}`}>{item.value}</span>
        </span>
      ))}
    </motion.div>
  );
}
