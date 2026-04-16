/**
 * BrowserHotPlexClient - Browser-native WebSocket client for HotPlex Worker Gateway.
 *
 * Adapted from examples/typescript-client/src/client.ts for browser environments.
 * Uses native WebSocket instead of the 'ws' Node.js package.
 */

import { EventEmitter } from 'eventemitter3';
import {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  ProtocolConstants,
} from './constants';
import type {
  HotPlexClientConfig,
  ReconnectConfig,
  HeartbeatConfig,
  Envelope,
  InitAckData,
  ErrorData,
  StateData,
  MessageDeltaData,
  MessageData,
  MessageStartData,
  MessageEndData,
  ToolCallData,
  ToolResultData,
  DoneData,
  PermissionRequestData,
  ReasoningData,
  StepData,
  PongData,
  ControlData,
} from './types';
import {
  createInitEnvelope,
  createInputEnvelope,
  createPingEnvelope,
  createControlEnvelope,
  createPermissionResponseEnvelope,
  serializeEnvelope,
  deserializeEnvelope,
  newSessionId,
  isInitAck,
} from './envelope';

// ============================================================================
// Event Types
// ============================================================================

export interface BrowserClientEvents {
  connected: (ack: InitAckData) => void;
  disconnected: (reason: string) => void;
  reconnecting: (attempt: number) => void;
  delta: (data: MessageDeltaData, env: Envelope) => void;
  message: (data: MessageData, env: Envelope) => void;
  messageStart: (data: MessageStartData, env: Envelope) => void;
  messageEnd: (data: MessageEndData, env: Envelope) => void;
  toolCall: (data: ToolCallData, env: Envelope) => void;
  toolResult: (data: ToolResultData, env: Envelope) => void;
  done: (data: DoneData, env: Envelope) => void;
  error: (data: ErrorData, env: Envelope) => void;
  state: (data: StateData, env: Envelope) => void;
  reasoning: (data: ReasoningData, env: Envelope) => void;
  step: (data: StepData, env: Envelope) => void;
  permissionRequest: (data: PermissionRequestData, env: Envelope) => void;
  reconnect: (data: ControlData, env: Envelope) => void;
  sessionInvalid: (data: ControlData, env: Envelope) => void;
  throttle: (data: ControlData, env: Envelope) => void;
  pong: (data: PongData, env: Envelope) => void;
}

// ============================================================================
// Constants
// ============================================================================

const DEFAULT_RECONNECT_CONFIG = {
  enabled: true,
  maxAttempts: 10,
  baseDelayMs: ProtocolConstants.ReconnectBaseDelayMs,
  maxDelayMs: ProtocolConstants.ReconnectMaxDelayMs,
};

const DEFAULT_HEARTBEAT_CONFIG = {
  pingIntervalMs: ProtocolConstants.PingPeriodMs,
  pongTimeoutMs: ProtocolConstants.PongWaitMs,
  maxMissedPongs: ProtocolConstants.MaxMissedPongs,
};

// ============================================================================
// BrowserHotPlexClient
// ============================================================================

export class BrowserHotPlexClient extends EventEmitter<BrowserClientEvents> {
  private ws: WebSocket | null = null;
  private config: HotPlexClientConfig;
  private reconnectConfig: Required<ReconnectConfig>;
  private heartbeatConfig: Required<HeartbeatConfig>;

  private _sessionId: string | null = null;
  private _state: SessionState = SessionState.Deleted;
  private _connected: boolean = false;
  private _connecting: boolean = false;
  private _reconnecting: boolean = false;

  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private shouldReconnect = true;

  private pingTimer: ReturnType<typeof setTimeout> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private missedPongs = 0;
  private lastPongTime = 0;

  private pendingInput: { content: string; resolve: () => void; reject: (err: Error) => void } | null = null;
  private inputRetryTimer: ReturnType<typeof setTimeout> | null = null;
  private pendingConnectReject: ((err: Error) => void) | null = null;

  private closed = false;

  constructor(config: HotPlexClientConfig) {
    super();

    this.config = {
      url: config.url,
      workerType: config.workerType,
      apiKey: config.apiKey,
      authToken: config.authToken,
      reconnect: config.reconnect ?? { enabled: true },
      heartbeat: config.heartbeat ?? {},
    };

    const reconnect = this.config.reconnect!;
    this.reconnectConfig = {
      enabled: reconnect.enabled,
      maxAttempts: reconnect.maxAttempts ?? DEFAULT_RECONNECT_CONFIG.maxAttempts,
      baseDelayMs: reconnect.baseDelayMs ?? DEFAULT_RECONNECT_CONFIG.baseDelayMs,
      maxDelayMs: reconnect.maxDelayMs ?? DEFAULT_RECONNECT_CONFIG.maxDelayMs,
    };

    const heartbeat = this.config.heartbeat;
    this.heartbeatConfig = {
      pingIntervalMs: heartbeat?.pingIntervalMs ?? DEFAULT_HEARTBEAT_CONFIG.pingIntervalMs,
      pongTimeoutMs: heartbeat?.pongTimeoutMs ?? DEFAULT_HEARTBEAT_CONFIG.pongTimeoutMs,
      maxMissedPongs: heartbeat?.maxMissedPongs ?? DEFAULT_HEARTBEAT_CONFIG.maxMissedPongs,
    };
  }

  // ============================================================================
  // Public Getters
  // ============================================================================

  get sessionId(): string | null { return this._sessionId; }
  get state(): SessionState { return this._state; }
  get connected(): boolean { return this._connected; }
  /** True while a connection handshake is in progress (awaiting init_ack). */
  get connecting(): boolean { return this._connecting; }
  get reconnecting(): boolean { return this._reconnecting; }

  // ============================================================================
  // Connection Lifecycle
  // ============================================================================

  async connect(sessionId?: string): Promise<InitAckData> {
    this.closed = false;
    this.shouldReconnect = true;
    const id = sessionId || this._sessionId || undefined;
    if (id) {
      this._sessionId = id;
    }
    return this._doConnect(id);
  }

  async resume(existingSessionId: string): Promise<InitAckData> {
    this.closed = false;
    this.shouldReconnect = true;
    this._sessionId = existingSessionId;
    return this._doConnect(existingSessionId);
  }

  private _doConnect(sessionId: string | undefined): Promise<InitAckData> {
    this._connecting = true;
    return new Promise((resolve, reject) => {
      this.pendingConnectReject = reject;
      try {
        const prevWs = this.ws;
        if (prevWs) {
          // Detach handler AND close the socket to avoid server having two active connections
          prevWs.onclose = null;
          prevWs.close();
        }

        let url = this.config.url;
        if (this.config.apiKey) {
          const separator = url.includes('?') ? '&' : '?';
          url = `${url}${separator}api_key=${encodeURIComponent(this.config.apiKey)}`;
        }

        this.ws = new WebSocket(url);

        const initEnv = createInitEnvelope(
          sessionId,
          this.config.workerType,
          undefined,
          this.config.authToken,
        );

        const onOpen = () => {
          if (!this.ws) return;
          this.ws.send(serializeEnvelope(initEnv));
        };

        const onMessage = (event: MessageEvent) => {
          const line = (typeof event.data === 'string' ? event.data : '').trim();
          if (!line) return;

          try {
            const env = deserializeEnvelope(line);
            this._handleMessage(env, resolve, reject);
          } catch (err) {
            this.emit('error', { code: ErrorCode.InvalidMessage, message: String(err) } as ErrorData, {} as Envelope);
          }
        };

        const onError = () => {
          // WebSocket error events don't carry useful info; close event follows
        };

        const activeWs = this.ws;

        this.ws.addEventListener('open', onOpen);
        this.ws.addEventListener('message', onMessage);
        this.ws.addEventListener('error', onError);
        this.ws.addEventListener('close', (event: CloseEvent) => {
          if (activeWs !== this.ws) return;
          this._handleClose(event.code, event.reason || 'Connection closed');
        });
      } catch (err) {
        this._connecting = false;
        this.pendingConnectReject = null;
        reject(err);
      }
    });
  }

  private _handleMessage(env: Envelope, resolve: (ack: InitAckData) => void, _reject: (err: Error) => void): void {
    const { event, session_id } = env;

    if (isInitAck(env)) {
      const ackData = event.data as unknown as InitAckData;

      this._sessionId = session_id;
      this._connected = true;
      this._connecting = false;
      this._reconnecting = false;
      this.reconnectAttempt = 0;
      this.pendingConnectReject = null;

      if (ackData.state) {
        this._state = ackData.state;
      }

      this._startHeartbeat();

      if (this.reconnectTimer) {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
      }

      this.emit('connected', ackData);
      resolve(ackData);
      return;
    }

    this._routeEvent(env);
  }

  private _routeEvent(env: Envelope): void {
    const { event } = env;

    switch (event.type) {
      case EventKind.Error:
        this.emit('error', event.data as ErrorData, env);
        if ((event.data as ErrorData).code === ErrorCode.SessionBusy) {
          this._handleSessionBusy();
        }
        break;

      case EventKind.State:
        this._state = (event.data as StateData).state;
        this.emit('state', event.data as StateData, env);
        break;

      case EventKind.Done:
        this.emit('done', event.data as DoneData, env);
        if (this.pendingInput) {
          this.pendingInput.resolve();
          this.pendingInput = null;
        }
        break;

      case EventKind.MessageDelta:
        this.emit('delta', event.data as MessageDeltaData, env);
        break;

      case EventKind.Message:
        this.emit('message', event.data as MessageData, env);
        break;

      case EventKind.MessageStart:
        this.emit('messageStart', event.data as MessageStartData, env);
        break;

      case EventKind.MessageEnd:
        this.emit('messageEnd', event.data as MessageEndData, env);
        break;

      case EventKind.ToolCall:
        this.emit('toolCall', event.data as ToolCallData, env);
        break;

      case EventKind.ToolResult:
        this.emit('toolResult', event.data as ToolResultData, env);
        break;

      case EventKind.Reasoning:
        this.emit('reasoning', event.data as ReasoningData, env);
        break;

      case EventKind.Step:
        this.emit('step', event.data as StepData, env);
        break;

      case EventKind.PermissionRequest:
        this.emit('permissionRequest', event.data as PermissionRequestData, env);
        break;

      case EventKind.Pong:
        this.missedPongs = 0;
        this.lastPongTime = Date.now();
        this.emit('pong', event.data as PongData, env);
        break;

      case EventKind.Control:
        this._handleControlMessage(event.data as ControlData, env);
        break;
    }
  }

  private _handleControlMessage(data: ControlData, env: Envelope): void {
    switch (data.action) {
      case ControlAction.Reconnect:
        this.emit('reconnect', data, env);
        if (this.reconnectConfig.enabled) {
          this._scheduleReconnect();
        }
        break;

      case ControlAction.SessionInvalid:
        this.emit('sessionInvalid', data, env);
        this.shouldReconnect = false;
        this.disconnect();
        break;

      case ControlAction.Throttle:
        this.emit('throttle', data, env);
        break;

      case ControlAction.Terminate:
        this.emit('reconnect', data, env);
        this.shouldReconnect = false;
        this.disconnect();
        break;

      case ControlAction.Delete:
        this.shouldReconnect = false;
        this.disconnect();
        break;
    }
  }

  // ============================================================================
  // Sending Messages
  // ============================================================================

  private _send(env: Envelope<unknown>): void {
    if (!this._sessionId || !this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('Not connected to gateway');
    }
    this.ws.send(serializeEnvelope(env));
  }

  sendInput(content: string): void {
    const env = createInputEnvelope(this._sessionId!, content);
    this._send(env);
  }

  async sendInputAsync(content: string): Promise<void> {
    if (this.pendingInput) {
      throw new Error('Input already pending');
    }

    return new Promise((resolve, reject) => {
      this.pendingInput = { content, resolve, reject };
      this.sendInput(content);

      setTimeout(() => {
        if (this.pendingInput) {
          this.pendingInput.reject(new Error('Input timeout'));
          this.pendingInput = null;
        }
      }, 300000);
    });
  }

  sendPermissionResponse(permissionId: string, allowed: boolean, reason?: string): void {
    const env = createPermissionResponseEnvelope(this._sessionId!, permissionId, allowed, reason);
    this._send(env);
  }

  sendControl(action: 'terminate' | 'delete'): void {
    const env = createControlEnvelope(this._sessionId!, action);
    this._send(env);
  }

  disconnect(): void {
    this.closed = true;
    this.shouldReconnect = false;

    this._stopHeartbeat();
    this._clearReconnectTimer();
    this._clearInputRetry();

    if (this.pendingConnectReject) {
      this.pendingConnectReject(new Error('Client disconnected'));
      this.pendingConnectReject = null;
    }

    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'Client disconnect');
      }
    }

    this._connected = false;
    this._connecting = false;
    this.emit('disconnected', 'Client initiated disconnect');
  }

  // ============================================================================
  // Heartbeat
  // ============================================================================

  private _startHeartbeat(): void {
    this._stopHeartbeat();
    this.missedPongs = 0;
    this.lastPongTime = Date.now();

    this.pingTimer = setInterval(() => {
      this._sendPing();
    }, this.heartbeatConfig.pingIntervalMs);
  }

  private _stopHeartbeat(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private _sendPing(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN || !this._sessionId) {
      return;
    }

    const env = createPingEnvelope(this._sessionId);
    this.ws.send(serializeEnvelope(env));

    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
    }
    this.pongTimer = setTimeout(() => {
      this.pongTimer = null;
      const timeSinceLastPong = Date.now() - this.lastPongTime;
      if (timeSinceLastPong >= this.heartbeatConfig.pongTimeoutMs) {
        this.missedPongs++;

        if (this.missedPongs >= this.heartbeatConfig.maxMissedPongs) {
          this._handleClose(4000, 'Heartbeat timeout');
        }
      }
    }, this.heartbeatConfig.pongTimeoutMs);
  }

  // ============================================================================
  // Reconnection
  // ============================================================================

  private _scheduleReconnect(): void {
    if (!this.shouldReconnect || this.closed || this.reconnectAttempt >= this.reconnectConfig.maxAttempts) {
      return;
    }

    this._reconnecting = true;
    this.reconnectAttempt++;

    const delay = Math.min(
      this.reconnectConfig.baseDelayMs * Math.pow(2, this.reconnectAttempt - 1),
      this.reconnectConfig.maxDelayMs,
    );

    this.emit('reconnecting', this.reconnectAttempt);

    this.reconnectTimer = setTimeout(async () => {
      if (!this._sessionId) return;

      try {
        await this._doConnect(this._sessionId);
      } catch {
        this._handleClose(4001, 'Reconnect failed');
      }
    }, delay);
  }

  private _clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private _handleClose(_code: number, reason: string): void {
    this._stopHeartbeat();

    const wasConnected = this._connected;
    this._connected = false;

    if (this.ws) {
      this.ws = null;
    }

    if (!wasConnected && !this._reconnecting) {
      // Connection closed before or during handshake — reject pending connect
      if (this._connecting && this.pendingConnectReject) {
        this._connecting = false;
        this.pendingConnectReject(new Error(`WebSocket closed during handshake: ${reason}`));
        this.pendingConnectReject = null;
      }
      return;
    }

    if (this.shouldReconnect && !this.closed && this.reconnectAttempt < this.reconnectConfig.maxAttempts) {
      this._scheduleReconnect();
    } else if (!this.shouldReconnect || this.closed) {
      this.emit('disconnected', reason);
    }
  }

  // ============================================================================
  // SESSION_BUSY Auto-Retry
  // ============================================================================

  private _handleSessionBusy(): void {
    if (!this.pendingInput || this.inputRetryTimer) {
      return;
    }

    this.inputRetryTimer = setTimeout(() => {
      this.inputRetryTimer = null;

      if (this.ws && this.ws.readyState === WebSocket.OPEN && this.pendingInput) {
        this.sendInput(this.pendingInput.content);
      }
    }, ProtocolConstants.SessionBusyRetryDelayMs);
  }

  private _clearInputRetry(): void {
    if (this.inputRetryTimer) {
      clearTimeout(this.inputRetryTimer);
      this.inputRetryTimer = null;
    }
  }
}
