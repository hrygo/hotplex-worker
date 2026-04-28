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
  success: number | null; // 0=false, 1=true, null=unknown
  source: string;
  tools_json: string | null; // JSON array of tool names, e.g. '["Read","Edit","Bash"]'
  tool_call_count: number;
  tokens_in: number;
  tokens_out: number;
  duration_ms: number;
  cost_usd: number;
  metadata_json: string | null;
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
    if (turn.role === 'assistant' && turn.tools_json) {
      try {
        const toolNames: string[] = JSON.parse(turn.tools_json);
        if (Array.isArray(toolNames) && toolNames.length > 0) {
          parts.push({
            type: 'tool-summary',
            toolNames,
            count: toolNames.length,
          });
        }
      } catch {
        // Ignore malformed tools_json
      }
    }

    return {
      id: turn.id,
      role: turn.role as 'user' | 'assistant',
      parts,
      createdAt: new Date(turn.created_at),
      status: 'complete' as const,
    };
  });
}