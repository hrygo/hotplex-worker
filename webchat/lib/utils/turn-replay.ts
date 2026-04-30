/**
 * Turn Replay Utility
 *
 * Converts backend ConversationRecord[] to frontend HotPlexMessage[].
 * The conversation table stores per-turn summaries (not raw events),
 * so conversion is straightforward: user turns → user messages,
 * assistant turns → assistant messages with text + tool summary.
 */

// -- Types --

/** Matches the backend ConversationRecord JSON shape from GET /api/sessions/{id}/history */
export interface ConversationTurn {
  id: string;
  session_id: string;
  seq: number;
  role: string; // 'user' | 'assistant'
  content: string;
  platform: string;
  user_id: string;
  model: string;
  success: boolean | null;
  source: string;
  tools: Record<string, number> | null; // e.g. {"Read": 2, "Edit": 1}
  tool_call_count: number;
  tokens_in: number;
  tokens_out: number;
  duration_ms: number;
  cost_usd: number;
  metadata: Record<string, unknown> | null;
  created_at: string;
}

// Reuse types from the runtime adapter
interface TextPart {
  type: 'text';
  text: string;
}

interface ToolSummaryPart {
  type: 'tool-summary';
  toolNames: string[];
  count: number;
}

type HistoryMessagePart = TextPart | ToolSummaryPart;

export interface HotPlexMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: HistoryMessagePart[];
  createdAt: Date;
  status?: 'streaming' | 'complete' | 'error';
}

// -- Conversion --

/**
 * Converts an array of ConversationTurn records to HotPlexMessage[].
 * 
 * - User turns become user messages with a single TextPart
 * - Assistant turns become assistant messages with TextPart + optional ToolSummaryPart
 * - Turns are returned in the same order as input (should be ASC by seq)
 */
export function conversationTurnsToMessages(turns: ConversationTurn[]): HotPlexMessage[] {
  return turns.map((turn) => {
    const parts: HistoryMessagePart[] = [];

    // Add text content (always present)
    if (turn.content) {
      parts.push({ type: 'text', text: turn.content });
    }

    // Add tool summary for assistant turns with tools
    if (turn.role === 'assistant' && turn.tools) {
      const toolNames = Object.keys(turn.tools);
      if (toolNames.length > 0) {
        parts.push({
          type: 'tool-summary',
          toolNames,
          count: toolNames.length,
        });
      }
    }

    const createdAt = new Date(turn.created_at);
    return {
      id: turn.id,
      role: turn.role as 'user' | 'assistant',
      parts,
      createdAt: isNaN(createdAt.getTime()) ? new Date() : createdAt,
      status: 'complete' as const,
    };
  });
}