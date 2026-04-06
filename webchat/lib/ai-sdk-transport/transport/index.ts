/**
 * AI SDK v4 ChatTransport adapter for HotPlex Worker Gateway.
 *
 * This module provides utilities for converting AEP v1 events
 * to AI SDK's data stream format.
 */

export { createAepStream, createDataStreamWriter } from './stream-controller';
export { mapAepToDataStream, mapErrorToDataStream } from './chunk-mapper';
