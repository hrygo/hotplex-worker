/**
 * @hotplex/ai-sdk-transport
 *
 * AI SDK ChatTransport adapter for HotPlex Worker Gateway (AEP v1 over WebSocket).
 */

// Transport utilities
export { createAepStream, createDataStreamWriter } from './transport/stream-controller';
export { mapAepToDataStream, mapErrorToDataStream } from './transport/chunk-mapper';
export type { DataStreamWriter } from './transport/chunk-mapper';

// Client
export { BrowserHotPlexClient } from './client/browser-client';
export type { BrowserClientEvents } from './client/browser-client';

// Constants
export {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  WorkerType,
  AEP_VERSION,
} from './client/constants';

// Types
export type {
  Envelope,
  Event,
  ErrorData,
  StateData,
  InputData,
  MessageStartData,
  MessageDeltaData,
  MessageEndData,
  ToolCallData,
  ToolResultData,
  ReasoningData,
  StepData,
  PermissionRequestData,
  DoneData,
  HotPlexClientConfig,
  ReconnectConfig,
  HeartbeatConfig,
  ClientState,
} from './client/types';
