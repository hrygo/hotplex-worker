/**
 * Shared message part types for webchat.
 * Single source of truth — import from here instead of defining locally.
 */

import type { ContextUsageData } from '@/lib/ai-sdk-transport/client/types';
import type { TurnSessionStats } from '@/lib/ai-sdk-transport/client/types';

export interface TextPart {
  type: 'text';
  text: string;
}

export interface ReasoningPart {
  type: 'reasoning';
  text: string;
}

export interface ToolCallPart {
  type: 'tool-call';
  toolName: string;
  args: any;
  toolCallId: string;
  result?: any;
  isError?: boolean;
}

export interface ToolSummaryPart {
  type: 'tool-summary';
  toolNames: string[];
  count: number;
}

export interface ContextUsagePart {
  type: 'context-usage';
  data: ContextUsageData;
}

export interface TurnSummaryPart {
  type: 'turn-summary';
  data: TurnSessionStats;
}

export type MessagePart = TextPart | ReasoningPart | ToolCallPart | ToolSummaryPart | ContextUsagePart | TurnSummaryPart;
