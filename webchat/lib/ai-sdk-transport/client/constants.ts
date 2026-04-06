// Protocol version (from pkg/events/events.go)
export const AEP_VERSION = 'aep/v1';

// Event kinds (from pkg/events/events.go:14-34)
export const EventKind = {
  Init: 'init',
  InitAck: 'init_ack',
  Error: 'error',
  State: 'state',
  Input: 'input',
  Done: 'done',
  Message: 'message',
  MessageStart: 'message.start',
  MessageDelta: 'message.delta',
  MessageEnd: 'message.end',
  ToolCall: 'tool_call',
  ToolResult: 'tool_result',
  Reasoning: 'reasoning',
  Step: 'step',
  Raw: 'raw',
  PermissionRequest: 'permission_request',
  PermissionResponse: 'permission_response',
  Ping: 'ping',
  Pong: 'pong',
  Control: 'control',
} as const;

export type EventKind = typeof EventKind[keyof typeof EventKind];

// Handshake types (from internal/gateway/init.go:13-16)
export const HandshakeType = {
  Init: 'init',
  InitAck: 'init_ack',
} as const;

export type HandshakeType = typeof HandshakeType[keyof typeof HandshakeType];

// Priority (from pkg/events/events.go:36-42)
export const Priority = {
  Control: 'control',
  Data: 'data',
} as const;

export type Priority = typeof Priority[keyof typeof Priority];

// Session states (from pkg/events/events.go:240-248)
export const SessionState = {
  Created: 'created',
  Running: 'running',
  Idle: 'idle',
  Terminated: 'terminated',
  Deleted: 'deleted',
} as const;

export type SessionState = typeof SessionState[keyof typeof SessionState];

// Error codes (from pkg/events/events.go:44-70)
export const ErrorCode = {
  WorkerStartFailed: 'WORKER_START_FAILED',
  WorkerCrash: 'WORKER_CRASH',
  WorkerTimeout: 'WORKER_TIMEOUT',
  WorkerOOM: 'WORKER_OOM',
  WorkerSIGKILL: 'PROCESS_SIGKILL',
  InvalidMessage: 'INVALID_MESSAGE',
  SessionNotFound: 'SESSION_NOT_FOUND',
  SessionExpired: 'SESSION_EXPIRED',
  SessionTerminated: 'SESSION_TERMINATED',
  SessionInvalidated: 'SESSION_INVALIDATED',
  SessionBusy: 'SESSION_BUSY',
  Unauthorized: 'UNAUTHORIZED',
  AuthRequired: 'AUTH_REQUIRED',
  InternalError: 'INTERNAL_ERROR',
  ProtocolViolation: 'PROTOCOL_VIOLATION',
  VersionMismatch: 'VERSION_MISMATCH',
  ConfigInvalid: 'CONFIG_INVALID',
  RateLimited: 'RATE_LIMITED',
  GatewayOverload: 'GATEWAY_OVERLOAD',
  ExecutionTimeout: 'EXECUTION_TIMEOUT',
  ReconnectRequired: 'RECONNECT_REQUIRED',
  WorkerOutputLimit: 'WORKER_OUTPUT_LIMIT',
} as const;

export type ErrorCode = typeof ErrorCode[keyof typeof ErrorCode];

// Control actions (from pkg/events/events.go:219-227)
export const ControlAction = {
  Reconnect: 'reconnect',
  SessionInvalid: 'session_invalid',
  Throttle: 'throttle',
  Terminate: 'terminate',
  Delete: 'delete',
} as const;

export type ControlAction = typeof ControlAction[keyof typeof ControlAction];

// Worker types (from internal/worker/worker.go:72-77)
export const WorkerType = {
  ClaudeCode: 'claude_code',
  OpenCodeCLI: 'opencode_cli',
  OpenCodeServer: 'opencode_server',
  PiMono: 'pi-mono',
} as const;

export type WorkerType = typeof WorkerType[keyof typeof WorkerType];

// Protocol constants (from internal/gateway/hub.go and conn.go)
export const ProtocolConstants = {
  DefaultGatewayPort: 8888,
  PingPeriodMs: 54000,           // 54 seconds (from hub.go: pingPeriod)
  PongWaitMs: 60000,            // 60 seconds (from hub.go: pongWait)
  WriteWaitMs: 10000,           // 10 seconds (from hub.go: writeWait)
  InitTimeoutMs: 30000,         // 30 seconds (from conn.go: 30 * time.Second)
  MaxMessageSize: 32 * 1024,    // 32KB (from hub.go: maxMessageSize)
  MaxMissedPongs: 3,             // (from heartbeat.go:29)
  ReconnectBaseDelayMs: 1000,   // 1 second base for exponential backoff
  ReconnectMaxDelayMs: 60000,   // 60 seconds max
  SessionBusyRetryDelayMs: 500,  // 500ms initial delay for SESSION_BUSY retry
} as const;

// Event ID prefix (from internal/aep/codec.go)
export const EVENT_ID_PREFIX = 'evt_';

// Session ID prefix (from internal/aep/codec.go)
export const SESSION_ID_PREFIX = 'sess_';
