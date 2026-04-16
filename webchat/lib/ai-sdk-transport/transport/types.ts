import type { WorkerType } from '../client/constants';

/**
 * Configuration for HotPlex transport
 */
export interface HotPlexTransportConfig {
  /**
   * WebSocket URL of the HotPlex gateway
   * Example: ws://localhost:8888 or wss://gateway.example.com
   */
  url: string;

  /**
   * Type of worker to connect to
   */
  workerType: WorkerType;

  /**
   * Authentication token (optional)
   */
  authToken?: string;

  /**
   * Session ID to resume (optional)
   */
  sessionId?: string;

  /**
   * Reconnection configuration
   */
  reconnect?: {
    enabled: boolean;
    maxAttempts?: number;
    baseDelayMs?: number;
    maxDelayMs?: number;
  };

  /**
   * Heartbeat configuration
   */
  heartbeat?: {
    pingIntervalMs?: number;
    pongTimeoutMs?: number;
    maxMissedPongs?: number;
  };
}

/**
 * Connection state of the transport
 */
export type ConnectionState = 'connected' | 'disconnected' | 'connecting' | 'error';
