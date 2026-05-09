/**
 * Unified HotPlexMessage type — single source of truth.
 *
 * Generic over parts to allow both narrow (history) and wide (live) usage:
 * - History: HotPlexMessage<TextPart | ToolSummaryPart>
 * - Live:    HotPlexMessage<MessagePart> (default)
 */

import type { MessagePart } from './message-parts';

export interface HotPlexMessage<P extends MessagePart = MessagePart> {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: P[];
  createdAt: Date;
  status?: 'streaming' | 'complete' | 'error';
}
