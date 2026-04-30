/**
 * @hotplex/ai-sdk-transport
 *
 * Browser WebSocket client for HotPlex Worker Gateway (AEP v1 over WebSocket).
 */

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
  WorkerStdioCommand,
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
  MessageData,
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
  ContextUsageData,
  ContextSkillInfo,
  ContextCategory,
  WorkerCommandData,
} from './client/types';
