/**
 * Server-side utilities for HotPlex AI SDK Transport.
 *
 * This module provides Next.js API route handlers that bridge
 * between AI SDK's HTTP transport and HotPlex's WebSocket protocol.
 */

// Re-export route handler
export { createHotPlexHandler, type HotPlexRouteConfig } from './route-handler';
