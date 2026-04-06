'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { BrowserHotPlexClient } from '@/lib/ai-sdk-transport';
import type { MessageDeltaData, ErrorData, DoneData, ReasoningData } from '@/lib/ai-sdk-transport';
import MessageList from './MessageList';
import ChatInput from './ChatInput';
import ThinkingIndicator from './ThinkingIndicator';
import ErrorMessage from './ErrorMessage';
import type { Message } from '@/types/message';

interface UseChatState {
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  status: 'submitted' | 'streaming' | 'awaiting_input' | 'ready';
  setStatus: React.Dispatch<React.SetStateAction<'submitted' | 'streaming' | 'awaiting_input' | 'ready'>>;
  error: Error | null;
  setError: React.Dispatch<React.SetStateAction<Error | null>>;
  stop: () => void;
  sendMessage: (message: { text: string }) => void;
}

export default function ChatContainer() {
  const [input, setInput] = useState('');
  const [messages, setMessages] = useState<Message[]>([]);
  const [status, setStatus] = useState<'submitted' | 'streaming' | 'awaiting_input' | 'ready'>('ready');
  const [error, setError] = useState<Error | null>(null);

  const clientRef = useRef<BrowserHotPlexClient | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isConnectingRef = useRef<boolean>(false);
  const isUnmountedRef = useRef<boolean>(false);

  // Initialize WebSocket client
  useEffect(() => {
    isUnmountedRef.current = false;

    const wsUrl = process.env.NEXT_PUBLIC_HOTPLEX_WS_URL || 'ws://localhost:8888/ws';
    const workerType = process.env.NEXT_PUBLIC_HOTPLEX_WORKER_TYPE || 'claude_code';
    const apiKey = process.env.NEXT_PUBLIC_HOTPLEX_API_KEY || 'dev';

    const client = new BrowserHotPlexClient({
      url: wsUrl,
      workerType: workerType as any,
      apiKey,
      heartbeat: { pingIntervalMs: 10000, pongTimeoutMs: 5000, maxMissedPongs: 2 },
    });

    clientRef.current = client;

    // Handle connection
    client.on('connected', (ack) => {
      console.log('Connected to gateway:', ack);
      isConnectingRef.current = false;
      setError(null);
    });

    // Handle disconnections
    client.on('disconnected', (reason) => {
      console.log('Disconnected:', reason);

      // Don't reconnect if component is unmounted or we're in the middle of connecting
      if (isUnmountedRef.current || isConnectingRef.current) {
        console.log('Skipping reconnect - unmounted or connecting');
        return;
      }

      // Clear any existing reconnect timer
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }

      // Auto reconnect after 3 seconds
      reconnectTimeoutRef.current = setTimeout(() => {
        // Double-check before reconnecting
        if (isUnmountedRef.current || !clientRef.current || isConnectingRef.current) {
          console.log('Skipping reconnect - conditions changed');
          return;
        }

        isConnectingRef.current = true;
        client.connect().catch((err) => {
          console.error('Reconnect failed:', err);
          isConnectingRef.current = false;
        });
      }, 3000);
    });

    // Handle text deltas (streaming response)
    client.on('delta', (data: MessageDeltaData) => {
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (last && last.role === 'assistant') {
          return [...prev.slice(0, -1), { ...last, content: last.content + data.content }];
        }
        return prev;
      });
    });

    // Handle reasoning (thinking)
    client.on('reasoning', (data: ReasoningData) => {
      console.log('Reasoning:', data.content);
    });

    // Handle done
    client.on('done', (data: DoneData) => {
      console.log('Done:', data);
      setStatus('ready');
      setInput('');
    });

    // Handle errors
    client.on('error', (data: ErrorData) => {
      console.error('Error:', data);
      setError(new Error(data.message || 'Unknown error'));
      setStatus('ready');
    });

    // Connect
    isConnectingRef.current = true;
    client.connect().then(() => {
      console.log('Initial connection established');
    }).catch((err) => {
      console.error('Initial connection failed:', err);
      isConnectingRef.current = false;
      setError(new Error('Failed to connect to gateway'));
    });

    return () => {
      console.log('ChatContainer cleanup - marking as unmounted');
      isUnmountedRef.current = true;

      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }

      if (client) {
        client.disconnect();
      }

      clientRef.current = null;
    };
  }, []);

  const stop = useCallback(() => {
    if (clientRef.current) {
      clientRef.current.sendControl('terminate');
    }
    setStatus('ready');
  }, []);

  const sendMessage = useCallback(async (message: string) => {
    if (!message.trim() || !clientRef.current) {
      console.log('sendMessage: early return', { hasMessage: !!message.trim(), hasClient: !!clientRef.current });
      return;
    }

    // Don't send if component is unmounted or client is connecting
    if (isUnmountedRef.current || isConnectingRef.current) {
      console.log('sendMessage: skipping - unmounted or connecting');
      return;
    }

    const trimmedMsg = message.trim();

    // Add user message + empty assistant placeholder (delta handler appends to last=assistant)
    const userMessage: Message = {
      id: `user-${Date.now()}`,
      role: 'user',
      content: trimmedMsg,
      createdAt: new Date(),
    };
    const assistantPlaceholder: Message = {
      id: `assistant-${Date.now()}`,
      role: 'assistant',
      content: '',
    };
    setMessages((prev) => [...prev, userMessage, assistantPlaceholder]);
    setStatus('submitted');
    const client = clientRef.current;

    console.log('sendMessage: about to send', { connected: client.connected, sessionId: client.sessionId, trimmedMsg });

    if (!client.connected) {
      console.log('sendMessage: not connected, connecting...');
      try {
        isConnectingRef.current = true;
        await client.connect();
        console.log('sendMessage: reconnected');
        isConnectingRef.current = false;
      } catch (err) {
        console.error('sendMessage: connect failed', err);
        isConnectingRef.current = false;
        setError(new Error('Failed to connect to gateway'));
        setStatus('ready');
        return;
      }
    }

    try {
      client.sendInput(trimmedMsg);
      console.log('sendMessage: sendInput called successfully');
    } catch (err) {
      console.error('sendMessage: sendInput failed', err);
      setError(new Error('Failed to send message'));
      setStatus('ready');
    }
  }, []);

  const isLoading = status === 'streaming' || status === 'submitted';

  return (
    <div className="flex flex-col h-screen bg-gray-50">
      <header className="bg-white border-b px-4 py-3 flex-shrink-0">
        <div className="max-w-3xl mx-auto">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center">
              <svg className="w-6 h-6 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
              </svg>
            </div>
            <div>
              <h1 className="text-lg font-semibold text-gray-900">HotPlex AI</h1>
              <p className="text-sm text-gray-500">AI SDK • AEP v1 Protocol</p>
            </div>
          </div>
        </div>
      </header>

      {status === 'submitted' && (
        <div className="flex justify-start px-4 py-2">
          <ThinkingIndicator />
        </div>
      )}

      <MessageList messages={messages} status={status} />

      {error && <ErrorMessage error={error} onRetry={() => setError(null)} />}

      <ChatInput
        input={input}
        onInputChange={setInput}
        onSend={sendMessage}
        onStop={stop}
        isLoading={isLoading}
      />
    </div>
  );
}
