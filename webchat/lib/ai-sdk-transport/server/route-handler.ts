import { BrowserHotPlexClient } from '../client/browser-client';
import { mapAepToDataStream, mapErrorToDataStream, type DataStreamWriter } from '../transport/chunk-mapper';
import type { ErrorData } from '../client/types';

/**
 * Configuration for HotPlex API route handler
 */
export interface HotPlexRouteConfig {
  /**
   * WebSocket URL of the HotPlex gateway
   */
  url: string;

  /**
   * Type of worker to connect to
   */
  workerType: string;

  /**
   * Authentication token (optional)
   */
  authToken?: string;

  /**
   * API key (optional). If provided, appended as ?api_key=<key> to the WebSocket URL.
   */
  apiKey?: string;
}

/**
 * Create a route handler for HotPlex chat.
 *
 * This is a framework-agnostic handler that can be used with
 * Next.js, Remix, or any other framework that supports Web API Request/Response.
 */
export function createHotPlexHandler(config: HotPlexRouteConfig) {
  return async (request: {
    messages: Array<{
      role: string;
      content?: string | Array<{ type: string; text?: string }>;
      parts?: Array<{ type: string; text?: string }>;
    }>;
  }): Promise<Response> => {
    const messages = request.messages || [];
    const lastMessage = messages[messages.length - 1];

    if (!lastMessage || lastMessage.role !== 'user') {
      return new Response('Last message must be from user', { status: 400 });
    }

    const textParts = lastMessage.parts || lastMessage.content;
    const userContent =
      typeof textParts === 'string'
        ? textParts
        : (textParts || [])
            .filter((part: { type: string; text?: string }) => part.type === 'text')
            .map((part: { type: string; text?: string }) => part.text || '')
            .join('\n') || '';

    if (!userContent) {
      return new Response('No content in user message', { status: 400 });
    }

    // Create WebSocket client
    const client = new BrowserHotPlexClient({
      url: config.url,
      workerType: config.workerType as import('../client/constants').WorkerType,
      apiKey: config.apiKey,
      authToken: config.authToken,
      heartbeat: { pingIntervalMs: 10000, pongTimeoutMs: 5000, maxMissedPongs: 2 },
    });

    // Connection timeout
    const connectionTimeout = new Promise<never>((_, reject) => {
      const t = setTimeout(() => reject(new Error('Gateway connection timeout')), 30000);
    });

    // Create a streaming response
    const stream = new ReadableStream({
      async start(controller) {
        const encoder = new TextEncoder();

        // Create a data stream writer
        const writer: DataStreamWriter = {
          writeData: (data: unknown) => {
            // Format: "0:{data}\n" for data stream parts
            const line = `0:${JSON.stringify(data)}\n`;
            controller.enqueue(encoder.encode(line));
          },
        };

        // Map AEP events to data stream
        mapAepToDataStream(writer, client);

        // Handle errors
        client.on('error', (data: ErrorData) => {
          mapErrorToDataStream(writer, data);
        });

        // Handle done — close the stream
        client.on('done', () => {
          controller.close();
        });

        try {
          // Connect to gateway with timeout
          await Promise.race([client.connect(), connectionTimeout]);
          // Send user input
          client.sendInput(userContent);
        } catch (error) {
          mapErrorToDataStream(writer, {
            code: 'CONNECTION_ERROR',
            message: error instanceof Error ? error.message : 'Connection failed',
          } as unknown as ErrorData);
          controller.close();
        }
      },
      cancel() {
        client.disconnect();
      },
    });

    return new Response(stream, {
      headers: {
        'Content-Type': 'text/plain; charset=utf-8',
        'Transfer-Encoding': 'chunked',
      },
    });
  };
}
