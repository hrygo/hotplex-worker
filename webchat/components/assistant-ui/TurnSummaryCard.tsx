'use client';

import { motion } from 'framer-motion';
import type { TurnSessionStats } from '@/lib/ai-sdk-transport/client/types';

type Severity = 'comfortable' | 'moderate' | 'high' | 'critical';

function getSeverity(pct: number): Severity {
  if (pct > 90) return 'critical';
  if (pct > 75) return 'high';
  if (pct >= 50) return 'moderate';
  return 'comfortable';
}

function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const k = n / 1000;
  return k % 1 === 0 ? `${k}K` : `~${k.toFixed(1)}K}`;
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${Math.round(s)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.round(s % 60);
  return rs > 0 ? `${m}m${rs}s` : `${m}m`;
}

function formatCost(usd: number): string {
  if (usd < 0.01) return '';
  if (usd < 1) return `$${usd.toFixed(2)}`;
  return `$${usd.toFixed(2)}`;
}

const severityConfig: Record<Severity, { color: string; bg: string; border: string }> = {
  comfortable: {
    color: 'var(--accent-emerald)',
    bg: 'rgba(52, 211, 153, 0.06)',
    border: 'rgba(52, 211, 153, 0.15)',
  },
  moderate: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.06)',
    border: 'rgba(251, 191, 36, 0.15)',
  },
  high: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.08)',
    border: 'rgba(251, 191, 36, 0.25)',
  },
  critical: {
    color: 'var(--accent-coral)',
    bg: 'rgba(244, 63, 94, 0.08)',
    border: 'rgba(244, 63, 94, 0.25)',
  },
};

export function TurnSummaryCard({ data }: { data: TurnSessionStats }) {
  const pct = Math.max(0, Math.min(100, data.context_pct ?? 0));
  const severity = getSeverity(pct);
  const cfg = severityConfig[severity];

  const parts: string[] = [];

  if (data.model_name) parts.push(data.model_name);
  if (pct > 0 && data.context_window > 0) {
    parts.push(`${Math.round(pct)}%`);
  }
  if (data.turn_duration_ms > 0) {
    parts.push(formatDuration(data.turn_duration_ms));
  }
  if (data.tool_call_count > 0) {
    parts.push(`🛠 ${data.tool_call_count}`);
  }
  const cost = data.turn_cost_usd ? formatCost(data.turn_cost_usd) : '';
  if (cost) parts.push(cost);

  if (parts.length === 0) return null;

  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: [0.2, 0, 0, 1] }}
      className="my-1.5 rounded-lg px-3 py-1.5 flex items-center gap-2 flex-wrap"
      style={{ background: cfg.bg, border: `1px solid ${cfg.border}` }}
    >
      {pct > 0 && data.context_window > 0 && (
        <div className="flex items-center gap-1.5">
          <span
            className="block w-1.5 h-1.5 rounded-full"
            style={{ background: cfg.color, boxShadow: `0 0 4px ${cfg.color}` }}
          />
          <div className="w-12 h-1 rounded-full bg-[var(--bg-elevated)] overflow-hidden">
            <motion.div
              className="h-full rounded-full"
              style={{ background: cfg.color }}
              initial={{ width: 0 }}
              animate={{ width: `${pct}%` }}
              transition={{ duration: 0.5, ease: [0.2, 0, 0, 1] }}
            />
          </div>
        </div>
      )}
      <span className="text-[10px] font-mono text-[var(--text-secondary)]">
        {parts.join(' · ')}
      </span>
    </motion.div>
  );
}
