'use client';

import { motion } from 'framer-motion';
import type { ContextUsageData } from '@/lib/ai-sdk-transport/client/types';

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
  return k % 1 === 0 ? `${k}K` : `~${k.toFixed(1)}K`;
}

const severityConfig: Record<Severity, { color: string; bg: string; border: string; label: string; tip: string }> = {
  comfortable: {
    color: 'var(--accent-emerald)',
    bg: 'rgba(52, 211, 153, 0.06)',
    border: 'rgba(52, 211, 153, 0.15)',
    label: 'Comfortable',
    tip: '',
  },
  moderate: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.06)',
    border: 'rgba(251, 191, 36, 0.15)',
    label: 'Moderate',
    tip: '',
  },
  high: {
    color: 'var(--accent-gold)',
    bg: 'rgba(251, 191, 36, 0.08)',
    border: 'rgba(251, 191, 36, 0.25)',
    label: 'High',
    tip: 'Consider /compact to free up space',
  },
  critical: {
    color: 'var(--accent-coral)',
    bg: 'rgba(244, 63, 94, 0.08)',
    border: 'rgba(244, 63, 94, 0.25)',
    label: 'Critical',
    tip: 'Context nearly full — use /compact or /reset',
  },
};

export function ContextUsageCard({ data }: { data: ContextUsageData }) {
  const severity = getSeverity(data.percentage);
  const cfg = severityConfig[severity];
  const pct = Math.max(0, Math.min(100, data.percentage));

  const topCats = (data.categories ?? [])
    .filter(c => !(data.skills && data.skills.total > 0 && c.name.toLowerCase() === 'skills'))
    .sort((a, b) => b.tokens - a.tokens)
    .slice(0, 3);

  const extras: string[] = [];
  if (data.skills && data.skills.total > 0) extras.push(`${data.skills.total} skills`);
  if (data.memory_files) extras.push(`${data.memory_files} memory`);
  if (data.mcp_tools) extras.push(`${data.mcp_tools} MCP`);
  if (data.agents) extras.push(`${data.agents} agents`);

  return (
    <motion.div
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: [0.2, 0, 0, 1] }}
      className="my-2 rounded-xl overflow-hidden"
      style={{ background: cfg.bg, border: `1px solid ${cfg.border}` }}
    >
      {/* Header: severity dot + label */}
      <div className="flex items-center justify-between px-4 pt-3 pb-1">
        <div className="flex items-center gap-2">
          <span
            className="block w-2 h-2 rounded-full"
            style={{ background: cfg.color, boxShadow: `0 0 6px ${cfg.color}` }}
          />
          <span className="text-[11px] font-mono font-bold tracking-wider uppercase" style={{ color: cfg.color }}>
            Context {cfg.label}
          </span>
        </div>
        {data.model && (
          <span className="text-[10px] font-mono text-[var(--text-faint)] tracking-wide">{data.model}</span>
        )}
      </div>

      {/* Progress bar */}
      <div className="px-4 py-2">
        <div className="flex items-center gap-3">
          <div className="flex-1 h-1.5 rounded-full bg-[var(--bg-elevated)] overflow-hidden">
            <motion.div
              className="h-full rounded-full"
              style={{ background: cfg.color }}
              initial={{ width: 0 }}
              animate={{ width: `${pct}%` }}
              transition={{ duration: 0.6, ease: [0.2, 0, 0, 1], delay: 0.1 }}
            />
          </div>
          <span className="text-[11px] font-mono text-[var(--text-secondary)] tabular-nums whitespace-nowrap">
            {formatTokens(data.total_tokens)} / {formatTokens(data.max_tokens)}
          </span>
        </div>
      </div>

      {/* Details row */}
      {(topCats.length > 0 || extras.length > 0) && (
        <div className="px-4 pb-3 flex items-center gap-3 flex-wrap">
          {topCats.map(c => (
            <span key={c.name} className="text-[10px] font-mono text-[var(--text-faint)]">
              {c.name}: <span className="text-[var(--text-secondary)]">{formatTokens(c.tokens)}</span>
            </span>
          ))}
          {extras.length > 0 && (
            <span className="text-[10px] font-mono text-[var(--text-faint)]">
              {extras.join(' · ')}
            </span>
          )}
        </div>
      )}

      {/* Action tip */}
      {cfg.tip && (
        <div className="px-4 pb-3">
          <span className="text-[10px] font-mono" style={{ color: cfg.color }}>
            {cfg.tip}
          </span>
        </div>
      )}
    </motion.div>
  );
}
