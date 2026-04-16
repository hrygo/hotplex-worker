/**
 * HotPlex Runtime Adapter
 *
 * Adapts BrowserHotPlexClient (AEP v1 WebSocket) to assistant-ui ExternalStoreAdapter.
 * This is the core integration layer that bridges the two systems.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import type { ExternalStoreAdapter, ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { BrowserHotPlexClient } from '@/lib/ai-sdk-transport';
import type {
  Envelope,
  MessageDeltaData,
  MessageStartData,
  DoneData,
  ErrorData,
  ReasoningData,
} from '@/lib/ai-sdk-transport';

// ThreadSuggestion shape — matches @assistant-ui/core ThreadSuggestion
type ThreadSuggestion = { title: string; label: string; prompt: string };

// ============================================================================
// Types
// ============================================================================

export interface UseHotPlexRuntimeConfig {
  url?: string;
  workerType?: string;
  apiKey?: string;
  /** Initial session ID to resume (calls resume() instead of connect()). */
  sessionId?: string;
}

// Single part of a message
interface TextPart {
  type: 'text';
  text: string;
}

interface ReasoningPart {
  type: 'reasoning';
  text: string;
}

type MessagePart = TextPart | ReasoningPart;

// Internal message format for our store
interface HotPlexMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: MessagePart[];
  createdAt: Date;
  status?: 'streaming' | 'complete' | 'error';
}

// ============================================================================
// Message Converter
// ============================================================================

/**
 * Converts HotPlex message to assistant-ui ThreadMessageLike format.
 * Handles both old format (content: string) and new format (parts: MessagePart[]).
 */
function convertToThreadMessage(message: HotPlexMessage, idx: number): ThreadMessageLike {
  // Support both old (content: string) and new (parts: MessagePart[]) message formats
  const content = 'parts' in message && Array.isArray(message.parts)
    ? message.parts
    : typeof (message as unknown as { content?: unknown }).content === 'string'
      ? [{ type: 'text' as const, text: (message as unknown as { content: string }).content }]
      : [];

  const result: ThreadMessageLike = {
    id: message.id,
    role: message.role as 'user' | 'assistant',
    content,
    createdAt: message.createdAt,
    attachments: [],
    metadata: {},
  };

  // Status is only supported for assistant messages
  if (message.role === 'assistant') {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (result as any).status = message.status === 'streaming'
      ? { type: 'running' }
      : { type: 'complete', reason: 'stop' };
  }

  return result;
}

// ============================================================================
// HotPlex Runtime Adapter Hook
// ============================================================================

/**
 * Hook that creates an assistant-ui ExternalStoreAdapter for HotPlex WebSocket client.
 *
 * This adapter:
 * 1. Manages WebSocket connection lifecycle
 * 2. Converts AEP v1 events to assistant-ui messages
 * 3. Provides onNew handler for sending messages
 *
 * @param config - Configuration options for HotPlex client
 * @returns assistant-ui ExternalStoreAdapter
 */
export function useHotPlexRuntime({
  url = 'ws://localhost:8888/ws',
  workerType = 'claude_code',
  apiKey = 'dev',
  sessionId,
}: UseHotPlexRuntimeConfig = {}): ExternalStoreAdapter<HotPlexMessage> {
  // State
  const [messages, setMessages] = useState<HotPlexMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const clientRef = useRef<BrowserHotPlexClient | null>(null);

  // Welcome suggestions — shown when thread is empty
  const suggestions: readonly ThreadSuggestion[] = [
    { title: '帮我写一个 React 组件', label: '代码', prompt: '帮我写一个 React 组件' },
    { title: '解释这段代码的逻辑', label: '学习', prompt: '解释这段代码的逻辑' },
    { title: '帮我调试这个错误', label: '调试', prompt: '帮我调试这个错误' },
    { title: '重构这段代码让它更简洁', label: '重构', prompt: '重构这段代码让它更简洁' },
    { title: '解释系统架构设计', label: '架构', prompt: '解释系统架构设计' },
  ];

  // Pending reasoning content accumulated before messageStart
  const pendingReasoningRef = useRef<string>('');

  // Initialize WebSocket client
  useEffect(() => {
    const client = new BrowserHotPlexClient({
      url,
      workerType: workerType as any,
      apiKey,
      heartbeat: {
        pingIntervalMs: 10000,
        pongTimeoutMs: 5000,
        maxMissedPongs: 2,
      },
    });

    clientRef.current = client;

    // Append delta content to the last text part of the last assistant message
    const appendDelta = (content: string) => {
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          const parts = [...lastMessage.parts];
          // Append to last text part, or add new one
          if (parts.length > 0 && parts[parts.length - 1].type === 'text') {
            const last = parts[parts.length - 1] as TextPart;
            parts[parts.length - 1] = { type: 'text', text: last.text + content };
          } else {
            parts.push({ type: 'text', text: content });
          }
          return [...prev.slice(0, -1), { ...lastMessage, parts }];
        }
        // No streaming message — this shouldn't happen with valid gateway
        return prev;
      });
    };

    // Handle reasoning/thinking content (appends to last reasoning part or creates one)
    const handleReasoning = (data: ReasoningData, _env: Envelope) => {
      // Accumulate content in ref (content may arrive before messageStart)
      pendingReasoningRef.current += data.content;

      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          const parts = [...lastMessage.parts];
          // Append to last reasoning part, or add new one
          const lastPart = parts[parts.length - 1];
          if (lastPart?.type === 'reasoning') {
            parts[parts.length - 1] = { type: 'reasoning', text: lastPart.text + data.content };
          } else {
            parts.push({ type: 'reasoning', text: pendingReasoningRef.current });
          }
          return [...prev.slice(0, -1), { ...lastMessage, parts }];
        }
        // No streaming message yet — create one with reasoning
        return [
          ...prev,
          {
            id: `assistant-${Date.now()}`,
            role: 'assistant' as const,
            parts: [{ type: 'reasoning', text: pendingReasoningRef.current }],
            createdAt: new Date(),
            status: 'streaming' as const,
          },
        ];
      });
      setIsRunning(true);
    };

    const handleDelta = (data: MessageDeltaData, env: Envelope) => {
      appendDelta(data.content);
      setIsRunning(true);
    };

    const handleDone = (data: DoneData, env: Envelope) => {
      console.log('HotPlexRuntimeAdapter: streaming done', data);

      // Mark last assistant message as complete
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          return [
            ...prev.slice(0, -1),
            { ...lastMessage, status: 'complete' },
          ];
        }
        return prev;
      });

      // Clear pending reasoning for next message
      pendingReasoningRef.current = '';
      setIsRunning(false);
    };

    const handleError = (data: ErrorData, env: Envelope) => {
      console.error('HotPlexRuntimeAdapter: error', data);
      setIsRunning(false);

      // Add error message
      setMessages((prev) => [
        ...prev,
        {
          id: `error-${Date.now()}`,
          role: 'system',
          parts: [{ type: 'text', text: `⚠️ Error: ${data.message}` }],
          createdAt: new Date(),
          status: 'error',
        },
      ]);
    };

    const handleDisconnected = (reason: string) => {
      console.log('HotPlexRuntimeAdapter: disconnected', reason);
      setIsRunning(false);
    };

    // Handle messageStart: if we already created a placeholder message for this env.id
    // (from reasoning events arriving early), keep it. Otherwise create new one.
    const handleMessageStart = (data: MessageStartData, env: Envelope) => {
      setMessages((prev) => {
        const existingIdx = prev.findIndex((m) => m.id === env.id);
        if (existingIdx !== -1) {
          // Message already created (e.g., from reasoning events) — just update status
          const updated = [...prev];
          updated[existingIdx] = { ...prev[existingIdx], status: 'streaming' };
          return updated;
        }
        // New message — create with streaming status
        return [
          ...prev,
          {
            id: env.id,
            role: 'assistant' as const,
            parts: [],
            createdAt: new Date(env.timestamp ?? Date.now()),
            status: 'streaming' as const,
          },
        ];
      });
      setIsRunning(true);
    };

    // Subscribe to events
    client.on('delta', handleDelta);
    client.on('done', handleDone);
    client.on('error', handleError);
    client.on('disconnected', handleDisconnected);
    client.on('reasoning', handleReasoning);
    client.on('messageStart', handleMessageStart);

    // Connect (resume an existing session or create new one)
    client.connect(sessionId).catch((err) => {
      console.error('HotPlexRuntimeAdapter: connection failed', err);
    });

    return () => {
      client.off('delta', handleDelta);
      client.off('done', handleDone);
      client.off('error', handleError);
      client.off('disconnected', handleDisconnected);
      client.off('reasoning', handleReasoning);
      client.off('messageStart', handleMessageStart);
      pendingReasoningRef.current = '';
      client.disconnect();
      clientRef.current = null;
    };
  }, [url, workerType, apiKey, sessionId]);

  // Track pending connection-wait state so useEffect cleanup can tear it down
  const connectionWaitRef = useRef<{
    timeout: ReturnType<typeof setTimeout>;
    onConnected: () => void;
    onDisconnected: (reason: string) => void;
  } | null>(null);

  // Cleanup: tear down any in-flight connection wait if the component unmounts
  useEffect(() => {
    return () => {
      const wait = connectionWaitRef.current;
      if (wait) {
        clearTimeout(wait.timeout);
        clientRef.current?.off('connected', wait.onConnected);
        clientRef.current?.off('disconnected', wait.onDisconnected);
        connectionWaitRef.current = null;
      }
    };
  }, []);

  // Handler for new messages (from assistant-ui Composer)
  // NOTE: reads client.connected directly from ref to avoid stale closure with isConnected state
  const handleNew = useCallback(async (message: AppendMessage) => {
    const client = clientRef.current;
    if (!client) {
      throw new Error('HotPlex client not initialized.');
    }

    // Wait for connection if handshake is still in progress (up to 30s)
    if (!client.connected) {
      if (client.connecting) {
        console.log('HotPlexRuntimeAdapter: waiting for connection...');
        // Register listeners BEFORE checking connected to avoid TOCTOU:
        // connected may fire between the if-check and listener registration.
        await new Promise<void>((resolve, reject) => {
          let settled = false;
          const settle = (fn: () => void) => {
            if (settled) return;
            settled = true;
            clearTimeout(timeout);
            client.off('disconnected', onDisconnected);
            connectionWaitRef.current = null;
            fn();
          };

          const timeout = setTimeout(() => {
            client.off('connected', onConnected);
            settle(() => reject(new Error('Connection timeout. Please check your network.')));
          }, 30000);

          const onConnected = () => {
            settle(() => resolve());
          };
          const onDisconnected = (reason: string) => {
            client.off('connected', onConnected);
            settle(() => reject(new Error(`Connection lost: ${reason}`)));
          };

          connectionWaitRef.current = { timeout, onConnected, onDisconnected };
          client.on('connected', onConnected);
          client.on('disconnected', onDisconnected);

          // If connected flipped true between the outer check and listener registration,
          // the 'connected' event already fired — call onConnected immediately.
          if (client.connected) {
            settle(() => resolve());
          }
        });
      } else {
        throw new Error('HotPlex client not connected. Please wait for connection...');
      }
    }

    // Extract text content from message parts
    const textContent = Array.isArray(message.content)
      ? message.content
          .filter((part): part is { type: 'text'; text: string } => part.type === 'text')
          .map((part) => part.text)
          .join('')
      : '';

    if (!textContent.trim()) {
      return;
    }

    // Add user message to state
    const userMessage: HotPlexMessage = {
      id: `user-${Date.now()}`,
      role: 'user',
      parts: [{ type: 'text', text: textContent }],
      createdAt: new Date(),
      status: 'complete',
    };

    setMessages((prev) => [...prev, userMessage]);

    // Send to HotPlex gateway with error handling
    try {
      client.sendInput(textContent);
    } catch (err) {
      console.error('HotPlexRuntimeAdapter: sendInput failed', err);
      // Remove the user message we just added since it wasn't sent
      setMessages((prev) => prev.slice(0, -1));
      throw new Error('Failed to send message. Please check your connection.');
    }
  }, []);

  // Handler for cancellation
  const handleCancel = useCallback(async () => {
    const client = clientRef.current;
    if (client?.connected) {
      client.sendControl('terminate');
    }
    setIsRunning(false);
  }, []);

  // Return ExternalStoreAdapter
  return {
    // State
    isRunning,
    messages,
    suggestions,
    setMessages: (messages: readonly HotPlexMessage[]) => {
      setMessages([...messages]);
    },

    // Message conversion
    convertMessage: convertToThreadMessage,

    // Event handlers
    onNew: handleNew,
    onCancel: handleCancel,

    // Capabilities
    unstable_capabilities: {
      copy: true,
    },
  } as ExternalStoreAdapter<HotPlexMessage>;
}
