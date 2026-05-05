/**
 * Message Cache — LocalStorage persistence for recent messages.
 *
 * Caches the last 50 HotPlexMessages per session in localStorage.
 * Used for instant recovery after page refresh before the history API loads.
 *
 * Key format: `hotplex_msgs_${sessionId}`
 * Value: JSON with version, sessionId, messages, updatedAt
 */

const CACHE_VERSION = 1;
const MAX_CACHED_MESSAGES = 50;
const STORAGE_PREFIX = 'hotplex_msgs_';

// Simplified message shape for serialization (matches turn-replay.ts HotPlexMessage)
interface CacheablePart {
  type: string;
  text?: string;
  toolNames?: string[];
  count?: number;
}

export interface CacheableMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: CacheablePart[];
  createdAt: string; // ISO string for serialization
  status?: 'streaming' | 'complete' | 'error';
}

interface CachedData {
  version: number;
  sessionId: string;
  messages: CacheableMessage[];
  updatedAt: number; // Unix timestamp
}

/**
 * Save messages to localStorage for a given session.
 * Only the last MAX_CACHED_MESSAGES are stored.
 */
export function saveMessages(sessionId: string, messages: CacheableMessage[]): void {
  try {
    const toCache = messages.slice(-MAX_CACHED_MESSAGES);
    const data: CachedData = {
      version: CACHE_VERSION,
      sessionId,
      messages: toCache,
      updatedAt: Date.now(),
    };
    localStorage.setItem(STORAGE_PREFIX + sessionId, JSON.stringify(data));
  } catch {
    // localStorage may be full or unavailable — fail silently
  }
}

/**
 * Load cached messages for a given session.
 * Returns null if no cache exists, version mismatch, or data is corrupted.
 */
export function loadMessages(sessionId: string): CacheableMessage[] | null {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + sessionId);
    if (!raw) return null;

    const data: CachedData = JSON.parse(raw);

    // Version check — discard if format changed
    if (data.version !== CACHE_VERSION) return null;
    if (data.sessionId !== sessionId) return null;

    return data.messages;
  } catch {
    return null;
  }
}
