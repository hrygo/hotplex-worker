import type { EventKind, Priority, SessionState, ErrorCode, ControlAction, WorkerType } from './constants.js';

// ============================================================================
// Envelope and Event Structures (from pkg/events/events.go)
// ============================================================================

export interface Envelope<T = unknown> {
  version: string;
  id: string;
  seq: number;
  priority?: Priority;
  session_id: string;
  timestamp: number;
  event: Event<T>;
}

export interface Event<T = unknown> {
  type: string;
  data: T;
}

// ============================================================================
// Event Data Types (from pkg/events/events.go:109-216)
// ============================================================================

export interface ErrorData {
  code: ErrorCode;
  message: string;
  event_id?: string;
  details?: Record<string, unknown>;
}

export interface StateData {
  state: SessionState;
  message?: string;
}

export interface InputData {
  content: string;
  metadata?: Record<string, unknown>;
}

export interface MessageStartData {
  id: string;
  role: string;
  content_type: string;
  metadata?: Record<string, unknown>;
}

export interface MessageDeltaData {
  message_id: string;
  content: string;
}

export interface MessageEndData {
  message_id: string;
}

export interface ToolCallData {
  id: string;
  name: string;
  input: Record<string, unknown>;
}

export interface ToolResultData {
  id: string;
  output: unknown;
  error?: string;
}

export interface RawData {
  kind: string;
  raw: unknown;
}

export interface DoneData {
  success: boolean;
  stats?: DoneStats;
  dropped?: boolean;
}

export interface DoneStats {
  duration_ms?: number;
  tool_calls?: number;
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  total_tokens?: number;
  cost_usd?: number;
  model?: string;
  context_used_percent?: number;
}

export interface MessageData {
  id: string;
  role: string;
  content: string;
  content_type?: string;
  metadata?: Record<string, unknown>;
}

export interface ReasoningData {
  id: string;
  content: string;
  model?: string;
}

export interface StepData {
  id: string;
  step_type: string;
  name?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  parent_id?: string;
  duration?: number;
}

export interface PermissionRequestData {
  id: string;
  tool_name: string;
  description?: string;
  args?: string[];
}

export interface PermissionResponseData {
  id: string;
  allowed: boolean;
  reason?: string;
}

export interface PongData {
  state: SessionState;
}

// ============================================================================
// Control Data (from pkg/events/events.go:229-237)
// ============================================================================

export interface ControlData {
  action: ControlAction;
  reason?: string;
  delay_ms?: number;
  recoverable?: boolean;
  suggestion?: ControlSuggestion;
  details?: Record<string, unknown>;
}

export interface ControlSuggestion {
  max_message_rate?: number;
  backoff_ms?: number;
  retry_after?: number;
}

// ============================================================================
// Init Handshake Types (from internal/gateway/init.go)
// ============================================================================

export interface InitData {
  version: string;
  worker_type: WorkerType;
  session_id?: string;
  auth?: InitAuth;
  config?: InitConfig;
  client_caps?: ClientCaps;
}

export interface InitAuth {
  token?: string;
}

export interface InitConfig {
  model?: string;
  system_prompt?: string;
  allowed_tools?: string[];
  disallowed_tools?: string[];
  max_turns?: number;
  work_dir?: string;
  metadata?: Record<string, unknown>;
}

export interface ClientCaps {
  supports_delta?: boolean;
  supports_tool_call?: boolean;
  supported_kinds?: string[];
}

export interface InitAckData {
  session_id: string;
  state: SessionState;
  server_caps: ServerCaps;
  error?: string;
  code?: ErrorCode;
}

export interface ServerCaps {
  protocol_version: string;
  worker_type: WorkerType;
  supports_resume: boolean;
  supports_delta: boolean;
  supports_tool_call: boolean;
  supports_ping: boolean;
  max_frame_size: number;
  max_turns?: number;
  modalities?: string[];
  tools?: string[];
}

// ============================================================================
// Client Configuration and State
// ============================================================================

export interface HotPlexClientConfig {
  url: string;
  workerType: WorkerType;
  apiKey?: string;
  authToken?: string;
  reconnect?: ReconnectConfig;
  heartbeat?: HeartbeatConfig;
}

export interface ReconnectConfig {
  enabled: boolean;
  maxAttempts?: number;
  baseDelayMs?: number;
  maxDelayMs?: number;
}

export interface HeartbeatConfig {
  pingIntervalMs?: number;
  pongTimeoutMs?: number;
  maxMissedPongs?: number;
}

export interface ClientState {
  sessionId: string | null;
  state: SessionState;
  connected: boolean;
  reconnecting: boolean;
}

// ============================================================================
// Event Maps for Type Safety
// ============================================================================

export interface ServerEventDataMap {
  [EventKind.Error]: ErrorData;
  [EventKind.State]: StateData;
  [EventKind.Done]: DoneData;
  [EventKind.Message]: MessageData;
  [EventKind.MessageStart]: MessageStartData;
  [EventKind.MessageDelta]: MessageDeltaData;
  [EventKind.MessageEnd]: MessageEndData;
  [EventKind.ToolCall]: ToolCallData;
  [EventKind.ToolResult]: ToolResultData;
  [EventKind.Reasoning]: ReasoningData;
  [EventKind.Step]: StepData;
  [EventKind.Raw]: RawData;
  [EventKind.PermissionRequest]: PermissionRequestData;
  [EventKind.Pong]: PongData;
  [EventKind.Control]: ControlData;
}

export interface ServerEventEnvelopeMap {
  [EventKind.Error]: Envelope<ErrorData>;
  [EventKind.State]: Envelope<StateData>;
  [EventKind.Done]: Envelope<DoneData>;
  [EventKind.Message]: Envelope<MessageData>;
  [EventKind.MessageStart]: Envelope<MessageStartData>;
  [EventKind.MessageDelta]: Envelope<MessageDeltaData>;
  [EventKind.MessageEnd]: Envelope<MessageEndData>;
  [EventKind.ToolCall]: Envelope<ToolCallData>;
  [EventKind.ToolResult]: Envelope<ToolResultData>;
  [EventKind.Reasoning]: Envelope<ReasoningData>;
  [EventKind.Step]: Envelope<StepData>;
  [EventKind.Raw]: Envelope<RawData>;
  [EventKind.PermissionRequest]: Envelope<PermissionRequestData>;
  [EventKind.Pong]: Envelope<PongData>;
  [EventKind.Control]: Envelope<ControlData>;
}

// ============================================================================
// Utility Types
// ============================================================================

export type DeepPartial<T> = {
  [P in keyof T]?: T[P] extends object ? DeepPartial<T[P]> : T[P];
};