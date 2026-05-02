// Context usage formatting utilities shared by webchat components.

import type { ContextUsageData } from '../ai-sdk-transport/client/types';

export type ContextSeverity = 'comfortable' | 'moderate' | 'high' | 'critical';

export function getContextSeverity(pct: number): ContextSeverity {
  if (pct > 90) return 'critical';
  if (pct > 75) return 'high';
  if (pct >= 50) return 'moderate';
  return 'comfortable';
}

export function getContextIcon(s: ContextSeverity): string {
  const icons: Record<ContextSeverity, string> = {
    comfortable: '🟢',
    moderate: '🟡',
    high: '🟠',
    critical: '🔴',
  };
  return icons[s];
}

export function getContextLabel(s: ContextSeverity): string {
  const labels: Record<ContextSeverity, string> = {
    comfortable: 'Comfortable',
    moderate: 'Moderate',
    high: 'High',
    critical: 'Critical',
  };
  return labels[s];
}

export function formatTokenCount(n: number): string {
  if (n < 1000) return String(n);
  const k = n / 1000;
  return k % 1 === 0 ? `${k}K` : `~${k.toFixed(1)}K`;
}

export function formatTokenDisplay(used: number, max: number): string {
  return `${formatTokenCount(used)} / ${formatTokenCount(max)}`;
}

export function buildProgressBar(pct: number, width = 10): string {
  const clamped = Math.max(0, Math.min(100, pct));
  const filled = Math.round((clamped / 100) * width);
  return `[${'█'.repeat(filled)}${'░'.repeat(width - filled)}]`;
}

export function getContextTip(s: ContextSeverity): string {
  const tips: Record<ContextSeverity, string> = {
    comfortable: '',
    moderate: '',
    high: 'Consider /compact to free up space',
    critical: 'Context nearly full! Use /compact or /reset',
  };
  return tips[s];
}

export function formatContextMessage(data: ContextUsageData): string {
  const severity = getContextSeverity(data.percentage);
  const icon = getContextIcon(severity);
  const label = getContextLabel(severity);
  const bar = buildProgressBar(data.percentage);
  const display = formatTokenDisplay(data.total_tokens, data.max_tokens);
  const tip = getContextTip(severity);

  const lines = [
    `${icon} ${bar} ${display}`,
  ];
  if (data.model) lines.push(`Model: ${data.model}`);
  if (tip) lines.push(tip);

  return lines.join('\n');
}
