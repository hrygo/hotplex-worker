/**
 * HotPlexClient - TypeScript client SDK for HotPlex Gateway (AEP v1)
 */

import { EventEmitter } from 'eventemitter3';
import { WebSocket } from 'ws';
import {
  EventKind,
  SessionState,
  ErrorCode,
  ControlAction,
  ProtocolConstants,
} from './constants.js';
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
  ToolCallData,
  ToolResultData,
  DoneData,
  PermissionRequestData,
  PongData,
  ControlData,
} from './types.js';
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
  createEnvelope,
  newEventId,
} from './envelope.js';
import {
  ConnectionError,
  SessionError,
  TimeoutError,
  ProtocolError,
} from './errors.js';
import { calculateBackoff } from './backoff.js';

// ============================================================================
// Event Types
// ============================================================================

export interface HotPlexClientEvents {
  connected: (ack: InitAckData) => void;
  disconnected: (reason: string) => void;
  reconnecting: (attempt: number) => void;
  delta: (data: MessageDeltaData, env: Envelope) => void;
  message: (data: MessageData, env: Envelope) => void;
  messageStart: (data: { id: string; role: string; content_type: string }, env: Envelope) => void;
  messageEnd: (data: { message_id: string }, env: Envelope) => void;
  toolCall: (data: ToolCallData, env: Envelope) => void;
  toolResult: (data: ToolResultData, env: Envelope) => void;
  done: (data: DoneData, env: Envelope) => void;
  error: (data: ErrorData, env: Envelope) => void;
  state: (data: StateData, env: Envelope) => void;
  reasoning: (data: { id: string; content: string }, env: Envelope) => void;
  step: (data: { id: string; step_type: string }, env: Envelope) => void;
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
// HotPlexClient Class
// ============================================================================

export class HotPlexClient extends EventEmitter<HotPlexClientEvents> {
  private ws: WebSocket | null = null;
  private config: HotPlexClientConfig;
  private reconnectConfig: Required<ReconnectConfig>;
  private heartbeatConfig: Required<HeartbeatConfig>;
  
  private _sessionId: string | null = null;
  private _state: SessionState = SessionState.Deleted;
  private _connected: boolean = false;
  private _reconnecting: boolean = false;
  
  private reconnectAttempt: number = 0;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private shouldReconnect: boolean = true;
  
  private pingTimer: NodeJS.Timeout | null = null;
  private missedPongs: number = 0;
  private lastPongTime: number = 0;
  
  private pendingInput: { content: string; resolve: () => void; reject: (err: Error) => void } | null = null;
  private inputRetryTimer: NodeJS.Timeout | null = null;
  
  private closed: boolean = false;

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
  get reconnecting(): boolean { return this._reconnecting; }

  // ============================================================================
  // Connection Lifecycle
  // ============================================================================

  async connect(sessionId?: string): Promise<InitAckData> {
    this.shouldReconnect = true;
    this._sessionId = sessionId || newSessionId();
    return this._doConnect(this._sessionId);
  }

  async resume(existingSessionId: string): Promise<InitAckData> {
    this.shouldReconnect = true;
    this._sessionId = existingSessionId;
    return this._doConnect(existingSessionId);
  }

  private async _doConnect(sessionId: string): Promise<InitAckData> {
    this._clearReconnectTimer();
    
    return new Promise((resolve, reject) => {
      let resolved = false;
      
      const timeoutTimer = setTimeout(() => {
        if (resolved) return;
        resolved = true;
        cleanup();
        reject(new TimeoutError('Connection timeout after ' + ProtocolConstants.HandshakeTimeoutMs + 'ms'));
      }, ProtocolConstants.HandshakeTimeoutMs || 30000);

      const cleanup = (): void => {
        clearTimeout(timeoutTimer);
        if (this.ws) {
          this.ws.off('open', onOpen);
          this.ws.off('message', onMessage);
          this.ws.off('error', onError);
          this.ws.off('close', onClose);
        }
      };

      const onOpen = (): void => {
        if (!this.ws) return;
        try {
          const initEnv = createInitEnvelope(
            sessionId,
            this.config.workerType,
            undefined,
            this.config.authToken
          );
          this.ws.send(serializeEnvelope(initEnv));
        } catch (err) {
          onError(err as Error);
        }
      };
      
      const onMessage = (data: string | Buffer | ArrayBuffer | Buffer[]): void => {
        let line: string;
        
        if (Buffer.isBuffer(data)) {
          line = data.toString('utf-8').trim();
        } else if (typeof data === 'string') {
          line = data.trim();
        } else if (data instanceof ArrayBuffer) {
          line = Buffer.from(data).toString('utf-8').trim();
        } else if (Array.isArray(data)) {
          line = Buffer.concat(data).toString('utf-8').trim();
        } else {
          return;
        }
        
        if (!line) return;
        
        try {
          const env = deserializeEnvelope(line);
          if (isInitAck(env)) {
            resolved = true;
            cleanup();
            this._handleInitAck(env, resolve, reject);
          } else {
            this._handleMessage(env, () => {}, () => {});
          }
        } catch (err) {
          this.emit('error', { code: ErrorCode.InvalidMessage, message: String(err) } as ErrorData, {} as Envelope);
        }
      };
      
      const onError = (err: Error): void => {
        if (resolved) return;
        resolved = true;
        cleanup();
        this._handleError(err);
        reject(new ConnectionError(err.message));
      };
      
      const onClose = (code: number, reason: Buffer): void => {
        if (resolved) return;
        resolved = true;
        cleanup();
        this._handleClose(code, reason.toString());
        reject(new ConnectionError(`Connection closed: ${reason.toString()} (${code})`));
      };

      try {
        const headers: Record<string, string> = {};
        if (this.config.apiKey) {
          headers['X-API-Key'] = this.config.apiKey;
        }
        
        if (this.ws) {
          this.ws.removeAllListeners();
          this.ws.close();
        }

        this.ws = new WebSocket(this.config.url, { headers });
        this.ws.on('open', onOpen);
        this.ws.on('message', onMessage);
        this.ws.on('error', onError);
        this.ws.on('close', onClose);
        
      } catch (err) {
        resolved = true;
        clearTimeout(timeoutTimer);
        reject(err);
      }
    });
  }

  private _handleInitAck(env: Envelope, resolve: (ack: InitAckData) => void, reject: (err: Error) => void): void {
    const { event, session_id } = env;
    const ackData = event.data as unknown as InitAckData;
    
    if (ackData.code && ackData.code !== 'OK' as any) {
      reject(new SessionError(ackData.error || 'Session initialization failed', ackData.code));
      return;
    }

    this._sessionId = session_id;
    this._connected = true;
    this._reconnecting = false;
    this.reconnectAttempt = 0;
    
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
  }

  private _handleMessage(env: Envelope, resolve: (ack: InitAckData) => void, _reject: (err: Error) => void): void {
    const { event, session_id } = env;
    
    if (isInitAck(env)) {
      const ackData = event.data as unknown as InitAckData;
      
      this._sessionId = session_id;
      this._connected = true;
      this._reconnecting = false;
      this.reconnectAttempt = 0;
      
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
        this.emit('messageStart', event.data as { id: string; role: string; content_type: string }, env);
        break;
        
      case EventKind.MessageEnd:
        this.emit('messageEnd', event.data as { message_id: string }, env);
        break;
        
      case EventKind.ToolCall:
        this.emit('toolCall', event.data as ToolCallData, env);
        break;
        
      case EventKind.ToolResult:
        this.emit('toolResult', event.data as ToolResultData, env);
        break;
        
      case EventKind.Reasoning:
        this.emit('reasoning', event.data as { id: string; content: string }, env);
        break;
        
      case EventKind.Step:
        this.emit('step', event.data as { id: string; step_type: string }, env);
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

  disconnect(): void {
    this.closed = true;
    this.shouldReconnect = false;
    
    this._stopHeartbeat();
    this._clearReconnectTimer();
    this._clearInputRetry();
    
    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      ws.removeAllListeners();
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'Client disconnect');
      }
    }
    
    this._connected = false;
    this.emit('disconnected', 'Client initiated disconnect');
  }

  close(): void {
    this.disconnect();
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

  sendInput(content: string, metadata?: Record<string, unknown>): void {
    const env = createInputEnvelope(this._sessionId!, content, metadata);
    this._send(env);
  }

  async sendInputAsync(content: string, metadata?: Record<string, unknown>): Promise<void> {
    if (this.pendingInput) {
      throw new Error('Input already pending');
    }
    
    return new Promise((resolve, reject) => {
      const onDone = (data: DoneData) => {
        cleanup();
        if (data.success) {
          resolve();
        } else {
          reject(new ProtocolError('Task failed', 'TASK_FAILED'));
        }
      };

      const onError = (data: ErrorData) => {
        cleanup();
        reject(new SessionError(data.message, data.code));
      };

      const onDisconnected = (reason: string) => {
        cleanup();
        reject(new ConnectionError(`Disconnected while waiting for input: ${reason}`));
      };

      const cleanup = () => {
        this.off('done', onDone);
        this.off('error', onError);
        this.off('disconnected', onDisconnected);
        this.pendingInput = null;
      };

      this.once('done', onDone);
      this.once('error', onError);
      this.once('disconnected', onDisconnected);

      try {
        this.pendingInput = { content, resolve, reject };
        this.sendInput(content, metadata);
      } catch (err) {
        cleanup();
        reject(err);
      }
      
      // Safety timeout: 10 minutes
      setTimeout(() => {
        if (this.pendingInput) {
          cleanup();
          reject(new TimeoutError('Input timeout after 10 minutes'));
        }
      }, 600000);
    });
  }

  sendToolResult(id: string, output: unknown, error?: string): void {
    const env = createEnvelope(
      newEventId(),
      this._sessionId!,
      0,
      EventKind.ToolResult,
      { id, output, error } as ToolResultData
    );
    this._send(env);
  }

  sendPermissionResponse(permissionId: string, allowed: boolean, reason?: string): void {
    const env = createPermissionResponseEnvelope(this._sessionId!, permissionId, allowed, reason);
    this._send(env);
  }

  sendControl(action: ControlAction): void {
    const env = createControlEnvelope(this._sessionId!, action as any);
    this._send(env);
  }

  terminate(): void {
    this.sendControl(ControlAction.Terminate);
  }

  delete(): void {
    this.sendControl(ControlAction.Delete);
  }

  // ============================================================================
  // Heartbeat Management
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
  }

  private _sendPing(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN || !this._sessionId) {
      return;
    }
    
    const env = createPingEnvelope(this._sessionId);
    this.ws.send(serializeEnvelope(env));
    
    setTimeout(() => {
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
  // Reconnection Logic
  // ============================================================================

  private _scheduleReconnect(): void {
    if (!this.shouldReconnect || this.closed || this.reconnectAttempt >= this.reconnectConfig.maxAttempts) {
      return;
    }
    
    const delay = calculateBackoff(this.reconnectAttempt, {
      baseDelayMs: this.reconnectConfig.baseDelayMs,
      maxDelayMs: this.reconnectConfig.maxDelayMs,
      jitter: 0.1,
    });
    
    this.emit('reconnecting', this.reconnectAttempt);
    
    this.reconnectTimer = setTimeout(async () => {
      if (!this._sessionId) return;
      
      try {
        await this._doConnect(this._sessionId);
      } catch (err) {
        // Reconnection failed, _handleClose will be called by WebSocket on 'close' or 'error'
        // or we already handled it in _doConnect's reject.
        // If we want to try again, we should call _scheduleReconnect.
        this._scheduleReconnect();
      }
    }, delay);
  }

  private _clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private _handleError(err: Error): void {
    this.emit('error', { code: ErrorCode.InternalError, message: err.message } as ErrorData, {} as Envelope);
  }

  private _handleClose(_code: number, reason: string): void {
    this._stopHeartbeat();
    
    const wasConnected = this._connected;
    this._connected = false;
    
    if (this.ws) {
      this.ws.removeAllListeners();
      this.ws = null;
    }
    
    if (!wasConnected && !this._reconnecting) {
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
    
    const retryDelay = ProtocolConstants.SessionBusyRetryDelayMs;
    
    this.inputRetryTimer = setTimeout(() => {
      this.inputRetryTimer = null;
      
      if (this.ws && this.ws.readyState === WebSocket.OPEN && this.pendingInput) {
        this.sendInput(this.pendingInput.content);
      }
    }, retryDelay);
  }

  private _clearInputRetry(): void {
    if (this.inputRetryTimer) {
      clearTimeout(this.inputRetryTimer);
      this.inputRetryTimer = null;
    }
  }
}