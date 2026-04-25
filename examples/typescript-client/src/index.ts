/**
 * HotPlex Gateway - TypeScript Client SDK
 * 
 * AEP v1 WebSocket client for connecting to HotPlex Gateway.
 */

// Main client
export { HotPlexClient } from './client.js';
export type { HotPlexClientEvents } from './client.js';

// Constants and types
export * from './constants.js';
export * from './types.js';

// Envelope utilities
export {
  newEventId,
  newSessionId,
  generateUUID,
  createEnvelope,
  createInitEnvelope,
  createInputEnvelope,
  createPingEnvelope,
  createControlEnvelope,
  createPermissionResponseEnvelope,
  serializeEnvelope,
  deserializeEnvelope,
  isInitAck,
  isError,
  isState,
  isDone,
  isDelta,
  isControl,
} from './envelope.js';