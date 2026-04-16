/**
 * AEP to AI SDK Data Stream mapper.
 *
 * Maps AEP v1 protocol events to AI SDK's data stream format
 * for use with useChat hook.
 */

import type { JSONValue } from 'ai';
import type {
  MessageStartData,
  MessageDeltaData,
  MessageEndData,
  ToolCallData,
  ToolResultData,
  DoneData,
  ReasoningData,
  StepData,
  PermissionRequestData,
  ErrorData,
} from '../client/types';

/**
 * Data stream writer interface
 */
export interface DataStreamWriter {
  writeData(data: unknown): void;
}

/**
 * Map AEP message.start to AI SDK data stream.
 */
export function mapMessageStart(
  writer: DataStreamWriter,
  data: MessageStartData,
): void {
  if (!data || !data.id) {
    return;
  }
  writer.writeData({
    type: 'text-start',
    id: data.id,
  });
}

/**
 * Map AEP message.delta to AI SDK data stream.
 */
export function mapMessageDelta(
  writer: DataStreamWriter,
  data: MessageDeltaData,
): void {
  if (!data || !data.content) {
    return;
  }
  writer.writeData({
    type: 'text-delta',
    id: data.message_id,
    delta: data.content,
  });
}

/**
 * Map AEP message.end to AI SDK data stream.
 */
export function mapMessageEnd(
  writer: DataStreamWriter,
  data: MessageEndData,
): void {
  if (!data || !data.message_id) {
    return;
  }
  writer.writeData({
    type: 'text-end',
    id: data.message_id,
  });
}

/**
 * Map AEP tool_call to AI SDK data stream.
 */
export function mapToolCall(
  writer: DataStreamWriter,
  data: ToolCallData,
): void {
  if (!data || !data.id) {
    return;
  }
  writer.writeData({
    type: 'tool-input-start',
    toolCallId: data.id,
    toolName: data.name,
  });
  writer.writeData({
    type: 'tool-input-delta',
    toolCallId: data.id,
    input: data.input,
  });
}

/**
 * Map AEP tool_result to AI SDK data stream.
 */
export function mapToolResult(
  writer: DataStreamWriter,
  data: ToolResultData,
): void {
  if (!data || !data.id) {
    return;
  }
  writer.writeData({
    type: 'tool-result',
    toolCallId: data.id,
    result: data.error || data.output,
  });
}

/**
 * Map AEP reasoning to AI SDK data stream.
 */
export function mapReasoning(
  writer: DataStreamWriter,
  data: ReasoningData,
): void {
  if (!data) {
    return;
  }
  writer.writeData({
    type: 'reasoning-delta',
    id: data.id,
    delta: data.content,
  });
}

/**
 * Map AEP step to AI SDK data stream.
 */
export function mapStep(
  writer: DataStreamWriter,
  data: StepData,
): void {
  if (!data || !data.id) {
    return;
  }
  writer.writeData({
    type: 'start-step',
    stepType: data.step_type,
    parentId: data.parent_id,
  });
}

/**
 * Map AEP permission_request to AI SDK data stream.
 */
export function mapPermissionRequest(
  writer: DataStreamWriter,
  data: PermissionRequestData,
): void {
  if (!data || !data.id) {
    return;
  }
  writer.writeData({
    type: 'tool-approval-request',
    approvalId: data.id,
    toolName: data.tool_name,
  });
}

/**
 * Map AEP done to AI SDK data stream.
 */
export function mapDone(
  writer: DataStreamWriter,
  data: DoneData,
): void {
  writer.writeData({
    type: 'finish',
    reason: data.success ? 'stop' : 'error',
  });
}

/**
 * Map AEP error to AI SDK data stream.
 */
export function mapErrorToDataStream(
  writer: DataStreamWriter,
  data: ErrorData,
): void {
  // Don't show SESSION_BUSY errors to users
  if (data.code === 'SESSION_BUSY') {
    return;
  }

  const errorText = getErrorMessage(data.code, data.message);
  writer.writeData({
    type: 'error',
    error: {
      code: data.code,
      message: errorText,
    },
  });
}

/**
 * Get user-friendly error message for AEP error codes.
 */
function getErrorMessage(code: string, defaultMessage: string): string {
  const messages: Record<string, string> = {
    UNAUTHORIZED: 'Authentication required. Please check your credentials.',
    WORKER_START_FAILED: 'Failed to start AI worker. Please try again.',
    WORKER_CRASH: 'AI worker crashed unexpectedly. Please retry.',
    WORKER_TIMEOUT: 'AI worker timed out. Please try again.',
    SESSION_NOT_FOUND: 'Session not found. It may have expired.',
    SESSION_EXPIRED: 'Session has expired. Please start a new conversation.',
    SESSION_TERMINATED: 'Session was terminated. Please start a new conversation.',
    INTERNAL_ERROR: 'Internal server error. Please try again later.',
    RATE_LIMITED: 'Too many requests. Please wait a moment and try again.',
    GATEWAY_OVERLOAD: 'Service is busy. Please try again later.',
    EXECUTION_TIMEOUT: 'Operation timed out. Please try again.',
  };

  return messages[code] || defaultMessage;
}

/**
 * Map all AEP events from a BrowserHotPlexClient to AI SDK data stream.
 */
export function mapAepToDataStream(
  writer: DataStreamWriter,
  client: import('../client/browser-client').BrowserHotPlexClient,
): void {
  client.on('messageStart', (data: MessageStartData) => mapMessageStart(writer, data));
  client.on('delta', (data: MessageDeltaData) => mapMessageDelta(writer, data));
  client.on('messageEnd', (data: MessageEndData) => mapMessageEnd(writer, data));
  client.on('toolCall', (data: ToolCallData) => mapToolCall(writer, data));
  client.on('toolResult', (data: ToolResultData) => mapToolResult(writer, data));
  client.on('reasoning', (data: ReasoningData) => mapReasoning(writer, data));
  client.on('step', (data: StepData) => mapStep(writer, data));
  client.on('permissionRequest', (data: PermissionRequestData) => mapPermissionRequest(writer, data));
  client.on('done', (data: DoneData) => mapDone(writer, data));
}
