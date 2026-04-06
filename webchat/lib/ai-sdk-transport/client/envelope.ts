/**
 * Envelope creation and utility helpers for AEP v1 protocol.
 *
 * Design rationale:
 * - All AEP messages are NDJSON (one JSON object per line)
 * - Event IDs use format "evt_<uuid>" (see internal/aep/codec.go)
 * - Session IDs use format "sess_<uuid>"
 * - seq is monotonically increasing per-session (assigned by gateway for server→client)
 */

import {
  AEP_VERSION,
  EVENT_ID_PREFIX,
  SESSION_ID_PREFIX,
  EventKind,
  Priority,
} from './constants';
import type {
  Envelope,
  InputData,
  InitData,
  ControlData,
} from './types';

// ============================================================================
// UUID Generation
// ============================================================================

/**
 * Generate a UUID v4 compatible string.
 * Uses crypto.randomUUID() if available, fallback for Node.js < 19
 */
export function generateUUID(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID();
  }
  // Fallback for older Node.js versions
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

// ============================================================================
// ID Generation
// ============================================================================

/**
 * Generate a new event ID with "evt_" prefix (matches Go codec.go)
 */
export function newEventId(): string {
  return `${EVENT_ID_PREFIX}${generateUUID()}`;
}

/**
 * Generate a new session ID with "sess_" prefix (matches Go codec.go)
 */
export function newSessionId(): string {
  return `${SESSION_ID_PREFIX}${generateUUID()}`;
}

// ============================================================================
// Envelope Creation
// ============================================================================

/**
 * Create a new Envelope with version, ID, seq, session_id, and timestamp set.
 */
export function createEnvelope<T = unknown>(
  id: string,
  sessionId: string,
  seq: number,
  type: string,
  data: T,
  priority?: Priority
): Envelope<T> {
  return {
    version: AEP_VERSION,
    id,
    seq,
    priority,
    session_id: sessionId,
    timestamp: Date.now(),
    event: {
      type,
      data,
    },
  };
}

/**
 * Create an init envelope for session initialization.
 * This is the FIRST message sent after WebSocket connection.
 */
export function createInitEnvelope(
  sessionId: string | undefined,
  workerType: string,
  config?: InitData['config'],
  authToken?: string
): Envelope<InitData> {
  const data: InitData = {
    version: AEP_VERSION,
    worker_type: workerType as InitData['worker_type'],
    config,
  };

  if (authToken) {
    data.auth = { token: authToken };
  }

  data.client_caps = {
    supports_delta: true,
    supports_tool_call: true,
    supported_kinds: [
      EventKind.Message,
      EventKind.MessageDelta,
      EventKind.MessageStart,
      EventKind.MessageEnd,
      EventKind.ToolCall,
      EventKind.ToolResult,
      EventKind.Done,
      EventKind.Error,
      EventKind.State,
      EventKind.Reasoning,
      EventKind.Step,
      EventKind.Control,
      EventKind.Ping,
      EventKind.Pong,
    ],
  };

  return createEnvelope(
    newEventId(),
    sessionId || '',
    1,
    EventKind.Init,
    data,
    'control' as Priority
  );
}

/**
 * Create an input envelope to send user input to the worker.
 */
export function createInputEnvelope(
  sessionId: string,
  content: string,
  metadata?: Record<string, unknown>
): Envelope<InputData> {
  return createEnvelope(
    newEventId(),
    sessionId,
    1,
    EventKind.Input,
    { content, metadata }
  );
}

/**
 * Create a ping envelope for heartbeat.
 */
export function createPingEnvelope(sessionId: string): Envelope<Record<string, never>> {
  return createEnvelope(
    newEventId(),
    sessionId,
    1,
    EventKind.Ping,
    {}
  );
}

/**
 * Create a control envelope (terminate/delete session).
 */
export function createControlEnvelope(
  sessionId: string,
  action: 'terminate' | 'delete'
): Envelope<ControlData> {
  return createEnvelope(
    newEventId(),
    sessionId,
    1,
    EventKind.Control,
    { action }
  );
}

/**
 * Create a permission response envelope.
 */
export function createPermissionResponseEnvelope(
  sessionId: string,
  permissionId: string,
  allowed: boolean,
  reason?: string
): Envelope<{ id: string; allowed: boolean; reason?: string }> {
  return createEnvelope(
    newEventId(),
    sessionId,
    1,
    EventKind.PermissionResponse,
    { id: permissionId, allowed, reason }
  );
}

// ============================================================================
// NDJSON Serialization
// ============================================================================

/**
 * Serialize an envelope to NDJSON string (one JSON per line).
 */
export function serializeEnvelope(env: Envelope<unknown>): string {
  return JSON.stringify(env) + '\n';
}

/**
 * Deserialize an NDJSON line to Envelope.
 */
const LINE_TERMINATORS_REGEX = /[\u2028\u2029]/g;

export function deserializeEnvelope(line: string): Envelope<unknown> {
  const sanitized = line.replace(LINE_TERMINATORS_REGEX, (match) => {
    return match === '\u2028' ? '\\u2028' : '\\u2029';
  });
  return JSON.parse(sanitized) as Envelope<unknown>;
}

// ============================================================================
// Envelope Validation
// ============================================================================

export function isInitAck(env: Envelope<unknown>): boolean {
  return env.event?.type === 'init_ack';
}

export function isError(env: Envelope<unknown>): boolean {
  return env.event?.type === EventKind.Error;
}

export function isState(env: Envelope<unknown>): boolean {
  return env.event?.type === EventKind.State;
}

export function isDone(env: Envelope<unknown>): boolean {
  return env.event?.type === EventKind.Done;
}

export function isDelta(env: Envelope<unknown>): boolean {
  return env.event?.type === EventKind.MessageDelta;
}

export function isControl(env: Envelope<unknown>): boolean {
  return env.event?.type === EventKind.Control;
}
