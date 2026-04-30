/**
 * HotPlex Runtime Adapter
 *
 * Adapts BrowserHotPlexClient (AEP v1 WebSocket) to assistant-ui ExternalStoreAdapter.
 * This is the core integration layer that bridges the two systems.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ExternalStoreAdapter, ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { BrowserHotPlexClient } from '@/lib/ai-sdk-transport';
import type { InitConfig, ContextUsageData } from '@/lib/ai-sdk-transport/client/types';
import { WorkerStdioCommand } from '@/lib/ai-sdk-transport/client/constants';
import { wsUrl, workerType, apiKey, workDir, allowedTools } from '@/lib/config';
import { useMetrics } from '@/lib/hooks/useMetrics';
import { getSessionHistory, type ConversationRecord } from '@/lib/api/sessions';
import { saveMessages, loadMessages, clearMessages, type CacheableMessage } from '@/lib/cache/message-cache';
import { conversationTurnsToMessages } from '@/lib/utils/turn-replay';
import type {
  Envelope,
  MessageDeltaData,
  MessageStartData,
  MessageData,
  DoneData,
  ErrorData,
  ReasoningData,
  ToolCallData,
  ToolResultData,
} from '@/lib/ai-sdk-transport';

// ThreadSuggestion shape — matches @assistant-ui/core ThreadSuggestion
type ThreadSuggestion = { title: string; label: string; prompt: string };

// ============================================================================
// Types
// ============================================================================

export interface UseHotPlexRuntimeConfig {
  /** Initial session ID to resume (calls resume() instead of connect()). */
  sessionId?: string;
  /** Override workDir from URL deep link (spec §5.2). */
  overrideWorkDir?: string;
  /** Called when session metrics update (for dashboard display). */
  onMetricsChange?: (metrics: import('@/lib/hooks/useMetrics').SessionMetrics) => void;
  /** Called when skills list is fetched from the worker. */
  onSkillsChange?: (skills: string[]) => void;
  /** Custom welcome suggestions shown when thread is empty. */
  suggestions?: readonly ThreadSuggestion[];
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

interface ToolCallPart {
  type: 'tool-call';
  toolName: string;
  args: any;
  toolCallId: string;
  result?: any;
  isError?: boolean;
}

interface ToolSummaryPart {
  type: 'tool-summary';
  toolNames: string[];
  count: number;
}

type MessagePart = TextPart | ReasoningPart | ToolCallPart | ToolSummaryPart;

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
function convertToThreadMessage(message: HotPlexMessage): ThreadMessageLike {
  // Filter out ToolSummaryPart — not recognized by assistant-ui's ThreadMessageLike type
  const content = message.parts.filter((p): p is TextPart | ReasoningPart | ToolCallPart => p.type !== 'tool-summary');

  const role = (message.role as string) === 'user' ? 'user' : 'assistant';

  const result: ThreadMessageLike = {
    id: message.id,
    role,
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
// History Conversion Helpers
// ============================================================================

// Convert runtime HotPlexMessage to CacheableMessage for LocalStorage
function toCacheable(msg: HotPlexMessage): CacheableMessage {
  return {
    id: msg.id,
    role: msg.role,
    parts: msg.parts.filter((p): p is TextPart | ReasoningPart | ToolSummaryPart =>
      p.type === 'text' || p.type === 'reasoning' || p.type === 'tool-summary'
    ).map(p => ({
      type: p.type,
      text: 'text' in p ? p.text : undefined,
      toolNames: 'toolNames' in p ? p.toolNames : undefined,
      count: 'count' in p ? p.count : undefined,
    })),
    createdAt: msg.createdAt instanceof Date && !isNaN(msg.createdAt.getTime())
      ? msg.createdAt.toISOString()
      : new Date().toISOString(),
    status: msg.status,
  };
}

// Convert CacheableMessage from LocalStorage back to HotPlexMessage
function fromCacheable(msg: CacheableMessage): HotPlexMessage {
  return {
    id: msg.id,
    role: msg.role as 'user' | 'assistant',
    parts: (msg.parts || []).map(p => {
      if (p.type === 'tool-summary') {
        return { type: 'tool-summary' as const, toolNames: (p as any).toolNames || [], count: (p as any).count || 0 };
      }
      if (p.type === 'reasoning') {
        return { type: 'reasoning' as const, text: p.text || '' };
      }
      return { type: 'text' as const, text: p.text || '' };
    }),
    createdAt: new Date(msg.createdAt),
    status: msg.status,
  };
}

// Convert ConversationRecord[] from API to HotPlexMessage[]
function historyToMessages(records: ConversationRecord[]): HotPlexMessage[] {
  const turns = records.map(r => ({
    id: r.id,
    session_id: r.session_id,
    seq: r.seq,
    role: r.role,
    content: r.content,
    platform: r.platform,
    user_id: r.user_id,
    model: r.model,
    success: r.success == null ? null : !!r.success,
    source: r.source,
    tools: r.tools,
    tool_call_count: r.tool_call_count,
    tokens_in: r.tokens_in,
    tokens_out: r.tokens_out,
    duration_ms: r.duration_ms,
    cost_usd: r.cost_usd,
    metadata: r.metadata,
    created_at: r.created_at,
  }));
  return conversationTurnsToMessages(turns).map(m => ({
    id: m.id,
    role: m.role as 'user' | 'assistant',
    parts: (m.parts || []).map(p => {
      if (p.type === 'tool-summary') {
        return { type: 'tool-summary' as const, toolNames: p.toolNames, count: p.count };
      }
      if (p.type === 'text') {
        return { type: 'text' as const, text: p.text || '' };
      }
      // reasoning not persisted in history (streaming content, restored from cache)
      return null;
    }).filter((p): p is TextPart | ToolSummaryPart => p !== null),
    createdAt: m.createdAt,
    status: 'complete' as const,
  }));
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
  sessionId,
  overrideWorkDir,
  onMetricsChange,
  onSkillsChange,
  suggestions: configSuggestions,
}: UseHotPlexRuntimeConfig = {}): ExternalStoreAdapter<HotPlexMessage> {
  // State
  const [messages, setMessages] = useState<HotPlexMessage[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [historyHasMore, setHistoryHasMore] = useState(true);
  const clientRef = useRef<BrowserHotPlexClient | null>(null);
  const historyLoadingRef = useRef(false);
  const sessionIdRef = useRef(sessionId);
  sessionIdRef.current = sessionId;

  // Welcome suggestions — shown when thread is empty (stable reference)
  // Welcome suggestions — shown when thread is empty (use prop or default)
  const defaultSuggestions: readonly ThreadSuggestion[] = [
    { title: '帮我写一个 React 组件', label: '代码', prompt: '帮我写一个 React 组件' },
    { title: '解释这段代码的逻辑', label: '学习', prompt: '解释这段代码的逻辑' },
    { title: '帮我调试这个错误', label: '调试', prompt: '帮我调试这个错误' },
    { title: '重构这段代码让它更简洁', label: '重构', prompt: '重构这段代码让它更简洁' },
    { title: '解释系统架构设计', label: '架构', prompt: '解释系统架构设计' },
  ];
  const suggestions: readonly ThreadSuggestion[] = configSuggestions ?? defaultSuggestions;

  // Pending reasoning content accumulated before messageStart
  const pendingReasoningRef = useRef<string>('');

  // Stable ref for skills callback — avoids adding to useEffect deps
  const onSkillsChangeRef = useRef(onSkillsChange);
  onSkillsChangeRef.current = onSkillsChange;

  // Track whether skills have been fetched (only after first turn completes)
  const skillsFetchedRef = useRef(false);

  // Cache min seq for cursor-based pagination (avoid O(n) scan on each load)
  const minSeqRef = useRef<number>(0);

  // Metrics tracking (spec §4.5 — Token & latency dashboard)
  const { sessionMetrics, startTurn, recordTurn } = useMetrics();

  // Sync metrics to parent (ChatContainer header dashboard)
  useEffect(() => {
    onMetricsChange?.(sessionMetrics);
  }, [sessionMetrics, onMetricsChange]);

  // Persist messages to LocalStorage on change
  useEffect(() => {
    if (sessionId && messages.length > 0) {
      saveMessages(sessionId, messages.filter(m => m.status !== 'streaming').map(toCacheable));
    }
  }, [sessionId, messages]);

  // Load history on session switch
  useEffect(() => {
    if (!sessionId) return;
    sessionIdRef.current = sessionId;
    setHistoryHasMore(true);

    // L2: Load from LocalStorage first for instant restore
    const cached = loadMessages(sessionId);
    if (cached && cached.length > 0) {
      setMessages(cached.map(fromCacheable));
    }

    // Then fetch authoritative history from server
    getSessionHistory(sessionId, { limit: 50 })
      .then(res => {
        if (res.records.length > 0) {
          const serverMessages = historyToMessages(res.records);
          // Build content signature set for dedup (live user messages have different IDs than server)
          // Extract ALL visible parts (text, reasoning, tool-summary) for accurate dedup
          const extractText = (parts: MessagePart[]) =>
            parts
              .filter(p => p.type === 'text' || p.type === 'reasoning' || p.type === 'tool-summary')
              .map(p => {
                if (p.type === 'text') return (p as TextPart).text || '';
                if (p.type === 'reasoning') return `[THOUGHT]${(p as ReasoningPart).text || ''}`;
                if (p.type === 'tool-summary') return `[TOOL]${((p as ToolSummaryPart).toolNames || []).join(',')}`;
                return '';
              })
              .join('');
          // Merge server messages with live messages (dedup by ID and content signature)
          setMessages(prev => {
            const serverIds = new Set(serverMessages.map(m => m.id));
            const serverSigs = new Set(
              serverMessages.map(m => `${m.role}:${extractText(m.parts)}`)
            );
            const liveOnly = prev.filter(m => {
              if (serverIds.has(m.id)) return false;
              // Also dedup by role+content for user messages (live ID vs server ID)
              const sig = `${m.role}:${extractText(m.parts)}`;
              return !serverSigs.has(sig);
            });
            const merged = [...serverMessages, ...liveOnly];
            // Save merged state to cache after state update
            setTimeout(() => {
              saveMessages(sessionId, merged.filter(m => m.status !== 'streaming').map(toCacheable));
            }, 0);
            return merged;
          });
          // Update minSeq cache for cursor-based pagination
          if (serverMessages.length > 0) {
            const minSeq = Math.min(...serverMessages.map(m => {
              const seq = parseInt(m.id.split('_').pop() || '0', 10);
              return seq > 0 ? seq : Number.MAX_SAFE_INTEGER;
            }));
            minSeqRef.current = minSeq === Number.MAX_SAFE_INTEGER ? 0 : minSeq;
          }
        }
        setHistoryHasMore(res.has_more);
      })
      .catch(err => {
        console.warn('HotPlexRuntimeAdapter: failed to load history', err);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // Initialize WebSocket client
  useEffect(() => {
    if (!sessionId) {
      console.log('HotPlexRuntimeAdapter: No session ID provided, skipping connection.');
      return;
    }

    skillsFetchedRef.current = false;

    const initConfig: InitConfig = {};
    const effectiveWorkDir = overrideWorkDir || workDir;
    if (effectiveWorkDir) initConfig.work_dir = effectiveWorkDir;
    if (allowedTools.length > 0) initConfig.allowed_tools = allowedTools;

    const client = new BrowserHotPlexClient({
      url: wsUrl,
      workerType,
      apiKey,
      initConfig,
      heartbeat: {
        pingIntervalMs: 20000,
        pongTimeoutMs: 10000,
        maxMissedPongs: 3,
      },
    });

    clientRef.current = client;

    // Append delta content to the last text part of the last assistant message
    const appendDelta = (content: string) => {
      setMessages((prev) => {
        const filtered = prev.filter(m => m.id !== 'ghost-assistant');
        const lastMessage = filtered[filtered.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          const parts = [...lastMessage.parts];
          // Append to last text part, or add new one
          if (parts.length > 0 && parts[parts.length - 1].type === 'text') {
            const last = parts[parts.length - 1] as TextPart;
            parts[parts.length - 1] = { type: 'text', text: last.text + content };
          } else {
            parts.push({ type: 'text', text: content });
          }
          return [...filtered.slice(0, -1), { ...lastMessage, parts }];
        }
        // No streaming message — create one (message.start may not have been sent)
        return [
          ...filtered,
          {
            id: `assistant-${Date.now()}`,
            role: 'assistant' as const,
            parts: [{ type: 'text', text: content }],
            createdAt: new Date(),
            status: 'streaming' as const,
          },
        ];
      });
    };

    // Handle reasoning/thinking content (appends to last reasoning part or creates one)
    const handleReasoning = (data: ReasoningData, _env: Envelope) => {
      if (!data) return;
      // Accumulate content in ref (content may arrive before messageStart)
      pendingReasoningRef.current += data.content || '';

      setMessages((prev) => {
        const filtered = prev.filter(m => m.id !== 'ghost-assistant');
        const lastMessage = filtered[filtered.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          const parts = [...lastMessage.parts];
          // Append to last reasoning part, or add new one
          const lastPart = parts[parts.length - 1];
          if (lastPart?.type === 'reasoning') {
            parts[parts.length - 1] = { type: 'reasoning', text: lastPart.text + data.content };
          } else {
            parts.push({ type: 'reasoning', text: pendingReasoningRef.current });
          }
          return [...filtered.slice(0, -1), { ...lastMessage, parts }];
        }
        // No streaming message yet — create one with reasoning
        return [
          ...filtered,
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
      if (!data) return;
      appendDelta(data.content || '');
      setIsRunning(true);
    };

    const handleMessage = (data: MessageData, env: Envelope) => {
      const role: 'user' | 'assistant' = data?.role === 'user' ? 'user' : 'assistant';
      setMessages((prev) => [
        ...prev,
        {
          id: data.id || env.id,
          role,
          parts: [{ type: 'text', text: data?.content || '' }],
          createdAt: new Date(env.timestamp || Date.now()),
          status: 'complete',
        },
      ]);
      setIsRunning(false);
    };

    const handleToolCall = (data: ToolCallData, env: Envelope) => {
      if (!data) return;
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          const parts = [...lastMessage.parts, {
            type: 'tool-call' as const,
            toolName: data.name,
            args: data.input,
            toolCallId: data.id,
          }];
          return [...prev.slice(0, -1), { ...lastMessage, parts }];
        }
        return prev;
      });
    };

    const handleToolResult = (data: ToolResultData, env: Envelope) => {
      if (!data) return;
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          const parts = lastMessage.parts.map((p) =>
            p.type === 'tool-call' && p.toolCallId === data.id
              ? { ...p, result: data.output }
              : p
          );
          return [...prev.slice(0, -1), { ...lastMessage, parts }];
        }
        return prev;
      });
    };

    const handleDone = (data: DoneData, _env: Envelope) => {

      if (data?.stats) {
        recordTurn(data.stats);
      } else {
        recordTurn({});
      }

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

      pendingReasoningRef.current = '';
      setIsRunning(false);

      // Fetch skills after the first turn completes (worker conversation is now active)
      if (!skillsFetchedRef.current) {
        skillsFetchedRef.current = true;
        try {
          client.sendWorkerCommand(WorkerStdioCommand.Skills);
        } catch {
          // Non-critical — skills list stays empty
        }
      }
    };

    const handleError = (data: ErrorData, env: Envelope) => {
      const isBusy = (data?.code as string) === 'SESSION_BUSY';
      const isResumeRetry = (data?.code as string) === 'RESUME_RETRY';
      const isShutdown = (data?.message || '').includes('during shutdown');

      // SESSION_BUSY is a transient state handled internally by auto-retry, so do not show it to the user and don't log as error.
      if (isBusy) {
        return;
      }

      // Shutdown errors are transient — gateway is restarting. Don't pollute the
      // chat with error messages; the client will auto-reconnect.
      if (isShutdown) {
        console.log('HotPlexRuntimeAdapter: gateway shutdown detected, waiting for reconnect');
        return;
      }

      const hasData = data && (data.code || data.message);
      if (hasData) {
        const detailsStr = data.details ? ` Details: ${JSON.stringify(data.details)}` : '';
        const eventStr = env?.id ? ` EventID: ${env.id}` : '';
        if (isResumeRetry) {
          console.warn(`HotPlexRuntimeAdapter: worker recovery triggered. Code: ${data.code}, Message: ${data.message}${detailsStr}${eventStr}`);
        } else {
          console.error(`HotPlexRuntimeAdapter: error received. Code: ${data.code || 'unknown'}, Message: ${data.message || 'none'}${detailsStr}${eventStr}`);
        }
      } else {
        console.warn(`HotPlexRuntimeAdapter: empty error event received (no code/message). EventID: ${env?.id}`);
      }

      // If it's a fatal error, stop the run and complete the streaming message
      if (!isResumeRetry) {
        setIsRunning(false);

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
      }

      let errorMessage = data?.message;
      
      // User-friendly mapping for specific terminal errors
      switch (data?.code as string) {
        case 'TURN_TIMEOUT':
          errorMessage = "Session timeout: The agent took too long to respond (limit: 15m). You may want to break your request into smaller steps.";
          break;
        case 'WORKER_CRASH':
          errorMessage = "The coding agent crashed unexpectedly. Please try again or reset the session.";
          break;
        case 'SESSION_EXPIRED':
          errorMessage = "This session has expired due to inactivity. Please start a new session.";
          break;
        case 'RATE_LIMITED':
          errorMessage = "You've reached the rate limit. Please wait a moment before sending more messages.";
          break;
        case 'UNAUTHORIZED':
          errorMessage = "Authentication failed. Please check your API key or connection settings.";
          break;
        case 'WORKER_OUTPUT_LIMIT':
          errorMessage = "The agent produced too much output and was terminated. Try to narrow down your request.";
          break;
        case 'RESUME_RETRY':
          errorMessage = `🔄 ${data?.message || 'Recovering session after unexpected crash...'}`;
          break;
        default:
          errorMessage = errorMessage || (data?.code ? `Error: ${data.code}` : 'An unexpected error occurred.');
      }

      // Add error message to thread
      setMessages((prev) => [
        ...prev,
        {
          id: `error-${Date.now()}`,
          role: 'assistant',
          parts: [{ type: 'text', text: `⚠️ ${errorMessage}` }],
          createdAt: new Date(),
          status: 'complete',
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
      if (!data) return;
      setMessages((prev) => {
        const filtered = prev.filter(m => m.id !== 'ghost-assistant');
        const existingIdx = filtered.findIndex((m) => m.id === env.id);
        if (existingIdx !== -1) {
          // Message already created (e.g., from reasoning events) — just update status
          const updated = [...filtered];
          updated[existingIdx] = { ...filtered[existingIdx], status: 'streaming' };
          return updated;
        }
        // New message — create with streaming status
        return [
          ...filtered,
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
    client.on('message', handleMessage);
    client.on('done', handleDone);
    client.on('error', handleError);
    client.on('disconnected', handleDisconnected);
    client.on('reasoning', handleReasoning);
    client.on('messageStart', handleMessageStart);
    client.on('toolCall', handleToolCall);
    client.on('toolResult', handleToolResult);

    const handleContextUsage = (data: ContextUsageData) => {
      const names = data?.skills?.names ?? [];
      onSkillsChangeRef.current?.(names);
    };
    client.on('contextUsage', handleContextUsage);

    client.connect(sessionId).catch((err) => {
      console.error('HotPlexRuntimeAdapter: connection failed', err);
    });

    return () => {
      client.off('delta', handleDelta);
      client.off('message', handleMessage);
      client.off('done', handleDone);
      client.off('error', handleError);
      client.off('disconnected', handleDisconnected);
      client.off('reasoning', handleReasoning);
      client.off('messageStart', handleMessageStart);
      client.off('toolCall', handleToolCall);
      client.off('toolResult', handleToolResult);
      client.off('contextUsage', handleContextUsage);
      pendingReasoningRef.current = '';
      client.disconnect();
      clientRef.current = null;
      clearMessages(sessionId);
    };
  }, [sessionId]);

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

    // Handle disconnected state: attempt to reconnect if not already connecting
    if (!client.connected) {
      console.log('HotPlexRuntimeAdapter: client not connected, attempting to reconnect...');
      try {
        if (!client.connecting) {
          // Don't pass sessionId here — the client internally tracks the latest session ID,
          // which may have been updated by a SessionNotFound retry in BrowserHotPlexClient.
          client.connect().catch(err => {
            console.error('HotPlexRuntimeAdapter: auto-connect failed', err);
          });
        }

        // Wait for connection (up to 30s)
        await new Promise<void>((resolve, reject) => {
          let settled = false;
          const settle = (fn: () => void) => {
            if (settled) return;
            settled = true;
            clearTimeout(timeout);
            client.off('connected', onConnected);
            client.off('disconnected', onDisconnected);
            connectionWaitRef.current = null;
            fn();
          };

          const timeout = setTimeout(() => {
            settle(() => reject(new Error('Connection timeout. Please check your network.')));
          }, 30000);

          const onConnected = () => {
            settle(() => resolve());
          };
          const onDisconnected = (reason: string) => {
            settle(() => reject(new Error(`Connection failed: ${reason}`)));
          };

          connectionWaitRef.current = { timeout, onConnected, onDisconnected };
          client.on('connected', onConnected);
          client.on('disconnected', onDisconnected);

          // Check if it connected while we were setting up listeners
          if (client.connected) {
            settle(() => resolve());
          }
        });
      } catch (err) {
        throw new Error(err instanceof Error ? err.message : 'HotPlex client not connected. Please check your network.');
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

    // 1. Add user message to state
    const userMessage: HotPlexMessage = {
      id: `user-${Date.now()}`,
      role: 'user',
      parts: [{ type: 'text', text: textContent }],
      createdAt: new Date(),
      status: 'complete',
    };

    // 2. Add optimistic "Ghost" assistant message to provide immediate feedback
    const ghostMessage: HotPlexMessage = {
      id: 'ghost-assistant',
      role: 'assistant',
      parts: [{ type: 'reasoning', text: 'Initializing HotPlex Agent and analyzing workspace context...' }],
      createdAt: new Date(),
      status: 'streaming',
    };

    setMessages((prev) => [...prev, userMessage, ghostMessage]);
    setIsRunning(true); 
    startTurn(); // Begin timing for metrics

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

  // Handler for loading earlier messages (cursor-based pagination)
  const handleLoadHistory = useCallback(async (): Promise<{ hasMore: boolean }> => {
    const sid = sessionIdRef.current;
    if (!sid || historyLoadingRef.current) return { hasMore: false };
    historyLoadingRef.current = true;

    try {
      // Use cached minSeq for cursor (updated when loading history from server)
      const cursorSeq = minSeqRef.current;
      if (!cursorSeq) return { hasMore: false };

      const res = await getSessionHistory(sid, { beforeSeq: cursorSeq, limit: 50 });
      if (res.records.length > 0) {
        const olderMessages = historyToMessages(res.records);
        setMessages(prev => {
          const existingIds = new Set(prev.map(m => m.id));
          const newOnly = olderMessages.filter(m => !existingIds.has(m.id));
          return [...newOnly, ...prev];
        });
        // Update minSeq cache for next page
        if (olderMessages.length > 0) {
          const minSeq = Math.min(...olderMessages.map(m => {
            const seq = parseInt(m.id.split('_').pop() || '0', 10);
            return seq > 0 ? seq : Number.MAX_SAFE_INTEGER;
          }));
          minSeqRef.current = minSeq === Number.MAX_SAFE_INTEGER ? cursorSeq : minSeq;
        }
      }
      setHistoryHasMore(res.has_more);
      return { hasMore: res.has_more };
    } catch (err) {
      console.warn('HotPlexRuntimeAdapter: failed to load earlier messages', err);
      return { hasMore: false };
    } finally {
      historyLoadingRef.current = false;
    }
  }, []);

  // Memoized thread messages conversion (spec §7.1)
  // Filter out malformed messages and guard against undefined roles to prevent
  // assistant-ui's internal converter from crashing with "Unknown message role".
  const threadMessages = useMemo(
    () => messages
      .filter((m): m is HotPlexMessage => !!m && (m.role === 'user' || m.role === 'assistant'))
      .map((m) => convertToThreadMessage(m)),
    [messages]
  );

  // Stable setMessages callback to prevent adapter churn
  const handleSetMessages = useCallback((msgs: readonly HotPlexMessage[]) => {
    setMessages([...msgs]);
  }, []);

  // Stable capabilities reference
  const capabilities = useMemo(() => ({
    copy: true,
    edit: true,
  }), []);

  // Stable extras reference — only changes when metrics or history state change
  const extras = useMemo(() => ({
    metrics: sessionMetrics,
    hasMore: historyHasMore,
    onLoadHistory: handleLoadHistory,
  }), [sessionMetrics, historyHasMore, handleLoadHistory]);

  // Return ExternalStoreAdapter — memoized to prevent unnecessary setAdapter calls
  return useMemo(() => ({
    // State
    isRunning,
    messages,
    threadMessages,
    suggestions,
    setMessages: handleSetMessages,

    // Message conversion
    convertMessage: convertToThreadMessage,

    // Event handlers
    onNew: handleNew,
    onCancel: handleCancel,

    // Capabilities — Phase 3: branching and editing enabled
    unstable_capabilities: capabilities,

    // Metrics — exposed for session dashboard (spec §4.5)
    extras,
  } as ExternalStoreAdapter<HotPlexMessage>), [
    isRunning, messages, threadMessages, suggestions,
    handleSetMessages, handleNew, handleCancel, capabilities, extras,
  ]);
}
