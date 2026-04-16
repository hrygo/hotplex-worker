import type { BrowserHotPlexClient } from '../client/browser-client';
import type { JSONValue } from 'ai';
import type { DataStreamWriter } from './chunk-mapper';
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
 * Create a data stream writer that maps AEP events to AI SDK data stream format.
 *
 * This is used in Next.js API routes to bridge between HotPlex WebSocket
 * and AI SDK's useChat hook.
 */
export function createDataStreamWriter(
  writer: DataStreamWriter,
  client: BrowserHotPlexClient,
): void {
  // Message start
  client.on('messageStart', (data: MessageStartData) => {
    writer.writeData({
      type: 'text-start',
      id: data.id,
    } as unknown as JSONValue);
  });

  // Message delta
  client.on('delta', (data: MessageDeltaData) => {
    writer.writeData({
      type: 'text-delta',
      id: data.message_id,
      delta: data.content,
    } as unknown as JSONValue);
  });

  // Message end
  client.on('messageEnd', (data: MessageEndData) => {
    writer.writeData({
      type: 'text-end',
      id: data.message_id,
    } as unknown as JSONValue);
  });

  // Tool calls
  client.on('toolCall', (data: ToolCallData) => {
    writer.writeData({
      type: 'tool-input-start',
      toolCallId: data.id,
      toolName: data.name,
    } as unknown as JSONValue);
    writer.writeData({
      type: 'tool-input-delta',
      toolCallId: data.id,
      input: data.input,
    } as unknown as JSONValue);
  });

  // Tool results
  client.on('toolResult', (data: ToolResultData) => {
    if (data.error) {
      writer.writeData({
        type: 'tool-result',
        toolCallId: data.id,
        result: { error: data.error },
      } as unknown as JSONValue);
    } else {
      writer.writeData({
        type: 'tool-result',
        toolCallId: data.id,
        result: data.output,
      } as unknown as JSONValue);
    }
  });

  // Reasoning
  client.on('reasoning', (data: ReasoningData) => {
    writer.writeData({
      type: 'reasoning-delta',
      id: data.id,
      delta: data.content,
    } as unknown as JSONValue);
  });

  // Step
  client.on('step', (data: StepData) => {
    writer.writeData({
      type: 'start-step',
      stepType: data.step_type,
      parentId: data.parent_id,
    } as unknown as JSONValue);
  });

  // Done
  client.on('done', (data: DoneData) => {
    writer.writeData({
      type: 'finish',
      reason: data.success ? 'stop' : 'error',
    } as unknown as JSONValue);
  });

  // Error
  client.on('error', (data: ErrorData) => {
    // Don't show SESSION_BUSY errors
    if (data.code === 'SESSION_BUSY') {
      return;
    }
    writer.writeData({
      type: 'error',
      error: {
        code: data.code,
        message: data.message,
      },
    } as unknown as JSONValue);
  });

  // Permission request
  client.on('permissionRequest', (data: PermissionRequestData) => {
    writer.writeData({
      type: 'tool-approval-request',
      approvalId: data.id,
      toolName: data.tool_name,
    } as unknown as JSONValue);
  });
}

/**
 * Create an AEP stream that maps BrowserHotPlexClient events to readable chunks.
 *
 * This is useful for browser-side streaming without using AI SDK's HTTP transport.
 */
export function createAepStream(
  client: BrowserHotPlexClient,
  abortSignal?: AbortSignal,
): ReadableStream<string> {
  let controller: ReadableStreamDefaultController<string>;

  // Abort handling
  const onAbort = () => {
    controller?.close();
  };

  abortSignal?.addEventListener('abort', onAbort, { once: true });

  // Register event handlers
  client.on('messageStart', (data: MessageStartData) => {
    const message = JSON.stringify({ type: 'text-start', id: data.id });
    controller.enqueue(message);
  });

  client.on('delta', (data: MessageDeltaData) => {
    const message = JSON.stringify({
      type: 'text-delta',
      id: data.message_id,
      delta: data.content,
    });
    controller.enqueue(message);
  });

  client.on('messageEnd', (data: MessageEndData) => {
    const message = JSON.stringify({ type: 'text-end', id: data.message_id });
    controller.enqueue(message);
  });

  client.on('toolCall', (data: ToolCallData) => {
    const start = JSON.stringify({
      type: 'tool-input-start',
      toolCallId: data.id,
      toolName: data.name,
    });
    const delta = JSON.stringify({
      type: 'tool-input-delta',
      toolCallId: data.id,
      input: data.input,
    });
    controller.enqueue(start);
    controller.enqueue(delta);
  });

  client.on('toolResult', (data: ToolResultData) => {
    const message = JSON.stringify({
      type: 'tool-result',
      toolCallId: data.id,
      result: data.error || data.output,
    });
    controller.enqueue(message);
  });

  client.on('done', (data: DoneData) => {
    const message = JSON.stringify({
      type: 'finish',
      reason: data.success ? 'stop' : 'error',
    });
    controller.enqueue(message);
    controller.close();
  });

  client.on('error', (data: ErrorData) => {
    if (data.code === 'SESSION_BUSY') {
      return;
    }
    const message = JSON.stringify({
      type: 'error',
      error: { code: data.code, message: data.message },
    });
    controller.enqueue(message);
  });

  return new ReadableStream<string>({
    start(c) {
      controller = c;
    },
    cancel() {
      abortSignal?.removeEventListener('abort', onAbort);
    },
  });
}
