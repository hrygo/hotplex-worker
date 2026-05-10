/**
 * HotPlex Runtime Adapter
 *
 * Adapts BrowserHotPlexClient (AEP v1 WebSocket) to assistant-ui ExternalStoreAdapter.
 * This is the core integration layer that bridges the two systems.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ExternalStoreAdapter, ThreadMessageLike, AppendMessage } from '@assistant-ui/react';
import { BrowserHotPlexClient } from '@/lib/ai-sdk-transport';
import type { InitConfig, ContextUsageData, PermissionRequestData, QuestionRequestData, ElicitationRequestData } from '@/lib/ai-sdk-transport/client/types';
import { WorkerStdioCommand } from '@/lib/ai-sdk-transport/client/constants';
import { wsUrl, workerType, apiKey, workDir, allowedTools, type ConnectionState } from '@/lib/config';
import { useMetrics } from '@/lib/hooks/useMetrics';
import { getSessionHistory, type ConversationRecord } from '@/lib/api/sessions';
import { conversationTurnsToMessages } from '@/lib/utils/turn-replay';
import { logger } from '@/lib/logger';
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
import type { TextPart, ReasoningPart, ToolCallPart, ToolSummaryPart, ContextUsagePart, TurnSummaryPart, MessagePart } from '@/lib/types/message-parts';
import type { HotPlexMessage } from '@/lib/types/message';

// Re-export for consumers
export type { HotPlexMessage };
export type { TextPart, ReasoningPart, ToolCallPart, ToolSummaryPart, ContextUsagePart, TurnSummaryPart, MessagePart };

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

// Content-signature prefix length for dedup — covers most short/medium responses.
const CONTENT_SIG_PREFIX = 300;

const DEFAULT_SUGGESTIONS: readonly ThreadSuggestion[] = [
  { title: '帮我写一个 React 组件', label: '代码', prompt: '帮我写一个 React 组件' },
  { title: '解释这段代码的逻辑', label: '学习', prompt: '解释这段代码的逻辑' },
  { title: '帮我调试这个错误', label: '调试', prompt: '帮我调试这个错误' },
  { title: '重构这段代码让它更简洁', label: '重构', prompt: '重构这段代码让它更简洁' },
  { title: '解释系统架构设计', label: '架构', prompt: '解释系统架构设计' },
];

// ============================================================================
// Message Converter
// ============================================================================

/**
 * Converts HotPlex message to assistant-ui ThreadMessageLike format.
 * Handles both old format (content: string) and new format (parts: MessagePart[]).
 */
function convertToThreadMessage(message: HotPlexMessage): ThreadMessageLike {
  // Filter out ToolSummaryPart, ContextUsagePart, and TurnSummaryPart — not recognized by assistant-ui's ThreadMessageLike type
  const parts = message.parts ?? [];
  const content = parts.filter((p): p is TextPart | ReasoningPart | ToolCallPart => p.type !== 'tool-summary' && p.type !== 'context-usage' && p.type !== 'turn-summary');

  const role = (message.role as string) === 'user' ? 'user' : 'assistant';

  // Extract context usage data for card rendering
  const contextUsagePart = parts.find((p): p is ContextUsagePart => p.type === 'context-usage');

  // Extract turn summary data for card rendering
  const turnSummaryPart = parts.find((p): p is TurnSummaryPart => p.type === 'turn-summary');

  // Extended ThreadMessageLike for HotPlex-specific metadata
  // (assistant-ui ThreadMessageLike.metadata is typed, but our custom keys need explicit typing)
  const result = {
    id: message.id,
    role,
    content,
    createdAt: message.createdAt,
    attachments: [] as const,
    metadata: {
      ...(contextUsagePart ? { contextUsage: contextUsagePart.data } : {}),
      ...(turnSummaryPart ? { turnSummary: turnSummaryPart.data } : {}),
    } satisfies Record<string, unknown>,
  } as ThreadMessageLike & {
    status?: { type: 'running' } | { type: 'complete'; reason: string };
  };

  // Status is only supported for assistant messages
  if (message.role === 'assistant') {
    result.status = message.status === 'streaming'
      ? { type: 'running' }
      : { type: 'complete', reason: 'stop' };
  }

  return result;
}

// ============================================================================
// History Conversion Helpers
// ============================================================================

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
      // reasoning not persisted in server history (streaming-only content, lost on reload)
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
  const [connectionState, setConnectionState] = useState<ConnectionState>('disconnected');

  // One-time cleanup of orphaned localStorage keys from removed message cache
  useEffect(() => {
    try {
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key?.startsWith('hotplex_msgs_')) keysToRemove.push(key);
      }
      keysToRemove.forEach(k => localStorage.removeItem(k));
    } catch { /* ignore */ }
  }, []);
  const clientRef = useRef<BrowserHotPlexClient | null>(null);
  const historyLoadingRef = useRef(false);
  const sessionIdRef = useRef(sessionId);
  sessionIdRef.current = sessionId;

  // Welcome suggestions — shown when thread is empty (use prop or default list)
  const suggestions: readonly ThreadSuggestion[] = configSuggestions ?? DEFAULT_SUGGESTIONS;

  // Stable ref for skills callback — avoids adding to useEffect deps
  const onSkillsChangeRef = useRef(onSkillsChange);
  onSkillsChangeRef.current = onSkillsChange;

  // Track whether skills have been fetched (only after first turn completes)
  const skillsFetchedRef = useRef(false);

  // Track pending interaction requests for response routing
  const interactionMapRef = useRef<Map<string, { type: 'permission' | 'question' | 'elicitation' }>>(new Map());

  // Cache min seq for cursor-based pagination (avoid O(n) scan on each load)
  const minSeqRef = useRef<number>(0);

  // Metrics tracking (spec §4.5 — Token & latency dashboard)
  const { sessionMetrics, startTurn, recordTurn } = useMetrics();

  // Sync metrics to parent (ChatContainer header dashboard)
  useEffect(() => {
    onMetricsChange?.(sessionMetrics);
  }, [sessionMetrics, onMetricsChange]);

  // Load history on session switch
  useEffect(() => {
    if (!sessionId) return;
    sessionIdRef.current = sessionId;
    setHistoryHasMore(true);

    const controller = new AbortController();

    // Fetch authoritative history from server
    getSessionHistory(sessionId, { limit: 50, signal: controller.signal })
      .then(res => {
        if (res.records.length > 0) {
          const serverMessages = historyToMessages(res.records);
          // Build content signature set for dedup (live user messages have different IDs than server)
          // Extract ALL visible parts (text, reasoning, tool-summary) for accurate dedup
          const extractText = (parts: MessagePart[] | undefined | null) =>
            (parts ?? [])
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
            return [...serverMessages, ...liveOnly];
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
        if (err instanceof DOMException && err.name === 'AbortError') return;
        logger.warn('RuntimeAdapter', 'Failed to load history', { error: String(err) });
        setMessages(prev => [...prev, {
          id: `history-error-${Date.now()}`,
          role: 'assistant' as const,
          parts: [{ type: 'text' as const, text: 'Failed to load conversation history. You can continue chatting, but previous messages may not be visible.' }],
          createdAt: new Date(),
          status: 'complete' as const,
        }]);
      });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  // Initialize WebSocket client
  useEffect(() => {
    // Guard: disconnect any lingering client from a previous effect run
    // (e.g., React Strict Mode double-render or stale closure from async reconnect)
    if (clientRef.current) {
      clientRef.current.disconnect();
      clientRef.current = null;
    }

    if (!sessionId) {
      logger.info('RuntimeAdapter', 'No session ID, skipping connection');
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
      authToken: apiKey, // pass via init envelope auth.token for deferred auth
      initConfig,
      heartbeat: {
        pingIntervalMs: 20000,
        pongTimeoutMs: 10000,
        maxMissedPongs: 3,
      },
    });

    clientRef.current = client;

    // Track the streaming fallback message ID (created by delta/reasoning before messageStart).
    // Used by handleMessage to adopt the fallback instead of creating a duplicate (#331).
    let streamingFallbackId: string | null = null;

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
        // No streaming message — create one (message.start may not have been sent)
        const fallbackId = `assistant-${Date.now()}`;
        streamingFallbackId = fallbackId;
        return [
          ...prev,
          {
            id: fallbackId,
            role: 'assistant' as const,
            parts: [{ type: 'text', text: content }],
            createdAt: new Date(),
            status: 'streaming' as const,
          },
        ];
      });
    };

    // Delta batcher — accumulate streaming deltas and flush once per animation frame
    // to reduce React re-renders from 30-60/s to at most 60fps (1 per frame).
    let pendingDelta = '';
    let deltaRafId = 0;

    const flushDelta = () => {
      const content = pendingDelta;
      pendingDelta = '';
      deltaRafId = 0;
      if (content) {
        appendDelta(content);
      }
    };

    // Handle reasoning/thinking content (appends to last reasoning part or creates one)
    const handleReasoning = (data: ReasoningData, _env: Envelope) => {
      if (!data) return;

      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant' && lastMessage.status === 'streaming') {
          const parts = [...lastMessage.parts];
          const lastPart = parts[parts.length - 1];
          if (lastPart?.type === 'reasoning') {
            parts[parts.length - 1] = { type: 'reasoning', text: lastPart.text + data.content };
          } else {
            parts.push({ type: 'reasoning', text: data.content || '' });
          }
          return [...prev.slice(0, -1), { ...lastMessage, parts }];
        }
        const fallbackId = `assistant-${Date.now()}`;
        streamingFallbackId = fallbackId;
        return [
          ...prev,
          {
            id: fallbackId,
            role: 'assistant' as const,
            parts: [{ type: 'reasoning', text: data.content || '' }],
            createdAt: new Date(),
            status: 'streaming' as const,
          },
        ];
      });
      setIsRunning(true);
    };

    const handleDelta = (data: MessageDeltaData, env: Envelope) => {
      if (!data) return;
      pendingDelta += data.content || '';
      if (!deltaRafId) {
        deltaRafId = requestAnimationFrame(flushDelta);
      }
    };

    const handleMessage = (data: MessageData, env: Envelope) => {
      const role: 'user' | 'assistant' = data?.role === 'user' ? 'user' : 'assistant';
      // Always use env.id for consistency with handleMessageStart
      const msgId = env.id;
      setMessages((prev) => {
        let existingIdx = prev.findIndex((m) => m.id === msgId);

        // Fallback: adopt streaming message with fallback ID instead of creating duplicate (#331).
        // When delta/reasoning arrives before messageStart, a placeholder message is created
        // with `assistant-${Date.now()}` ID. We keep that ID to avoid MessageRepository crash,
        // so handleMessage must find it by the tracked fallback ID.
        if (existingIdx === -1 && role === 'assistant' && streamingFallbackId) {
          existingIdx = prev.findIndex((m) => m.id === streamingFallbackId);
          if (existingIdx !== -1) streamingFallbackId = null;
        }

        if (existingIdx !== -1) {
          // Update existing message (e.g., streaming placeholder → complete)
          const updated = [...prev];
          updated[existingIdx] = {
            ...prev[existingIdx],
            role,
            parts: [{ type: 'text', text: data?.content || '' }],
            status: 'complete',
          };
          return updated;
        }
        return [
          ...prev,
          {
            id: msgId,
            role,
            parts: [{ type: 'text', text: data?.content || '' }],
            createdAt: new Date(env.timestamp || Date.now()),
            status: 'complete',
          },
        ];
      });
      setIsRunning(false);
    };

    const TODO_TOOLS = new Set(['todo', 'todowrite', 'todo_write', 'task_list', 'checklist']);

    const handleToolCall = (data: ToolCallData, env: Envelope) => {
      if (!data) return;
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          const newPart = {
            type: 'tool-call' as const,
            toolName: data.name,
            args: data.input,
            toolCallId: data.id,
          };
          // Replace previous todo tool-call instead of stacking duplicates
          if (TODO_TOOLS.has(data.name?.toLowerCase())) {
            const lastTodoIdx = lastMessage.parts.findLastIndex(
              (p): p is ToolCallPart => p.type === 'tool-call' && TODO_TOOLS.has(p.toolName.toLowerCase())
            );
            if (lastTodoIdx !== -1) {
              const parts = [...lastMessage.parts];
              parts[lastTodoIdx] = newPart;
              return [...prev.slice(0, -1), { ...lastMessage, parts }];
            }
          }
          const parts = [...lastMessage.parts, newPart];
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
      streamingFallbackId = null;

      if (data?.stats) {
        recordTurn(data.stats);
      } else {
        recordTurn({});
      }

      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          const parts = [...lastMessage.parts];
          // Inject turn-summary part from _session data
          if (data?.stats?._session) {
            parts.push({ type: 'turn-summary' as const, data: data.stats._session });
          }
          return [
            ...prev.slice(0, -1),
            { ...lastMessage, status: 'complete' as const, parts },
          ];
        }
        return prev;
      });

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
        logger.info('RuntimeAdapter', 'Gateway shutdown, waiting for reconnect');
        return;
      }

      const hasData = data && (data.code || data.message);
      if (hasData) {
        const detailsStr = data.details ? ` Details: ${JSON.stringify(data.details)}` : '';
        const eventStr = env?.id ? ` EventID: ${env.id}` : '';
        if (isResumeRetry) {
          logger.warn('RuntimeAdapter', 'Worker recovery triggered', { code: data.code, message: data.message, details: data.details, eventId: env?.id });
        } else {
          logger.error('RuntimeAdapter', 'Error received', { code: data.code || 'unknown', message: data.message || 'none', details: data.details, eventId: env?.id });
        }
      } else {
        logger.warn('RuntimeAdapter', 'Empty error event', { eventId: env?.id });
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
      logger.info('RuntimeAdapter', 'Disconnected', { reason });
      setIsRunning(false);
      setConnectionState('disconnected');
    };

    // Handle messageStart: confirm streaming status on existing message or create new.
    // IMPORTANT: never rename an existing message's ID — changing IDs between renders
    // causes assistant-ui MessageRepository orphaned-node crash (#331).
    const handleMessageStart = (data: MessageStartData, env: Envelope) => {
      if (!data) return;
      setMessages((prev) => {
        // Reasoning events may arrive before messageStart, creating env.id early.
        const existingIdx = prev.findIndex((m) => m.id === env.id);
        if (existingIdx !== -1) {
          const updated = [...prev];
          updated[existingIdx] = { ...prev[existingIdx], status: 'streaming' };
          return updated;
        }
        // Delta/reasoning fallback already created a streaming message with a placeholder ID.
        // Keep it as-is — do NOT rename to env.id (causes #331).
        const pendingIdx = prev.findLastIndex((m) =>
          m.role === 'assistant' && m.status === 'streaming'
        );
        if (pendingIdx !== -1) {
          return prev;
        }
        // No prior message for this turn — create with real env.id.
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

      // Inject into last assistant message's parts (same pattern as turn-summary in handleDone)
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          return [
            ...prev.slice(0, -1),
            { ...lastMessage, parts: [...lastMessage.parts, { type: 'context-usage' as const, data }] },
          ];
        }
        return prev;
      });
    };
    client.on('contextUsage', handleContextUsage);

    // Interaction event handlers — inject as tool-call parts for PermissionCard rendering
    const handlePermissionRequest = (data: PermissionRequestData, _env: Envelope) => {
      if (!data) return;
      interactionMapRef.current.set(data.id, { type: 'permission' });
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          return [...prev.slice(0, -1), {
            ...lastMessage,
            parts: [...lastMessage.parts, {
              type: 'tool-call' as const,
              toolName: 'ask_permission',
              args: { description: data.description, tool_name: data.tool_name, args: data.args },
              toolCallId: data.id,
            }],
          }];
        }
        return prev;
      });
    };
    client.on('permissionRequest', handlePermissionRequest);

    const handleQuestionRequest = (data: QuestionRequestData, _env: Envelope) => {
      if (!data) return;
      interactionMapRef.current.set(data.id, { type: 'question' });
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          const questionText = data.questions?.map(q => q.question).join('\n') || '';
          return [...prev.slice(0, -1), {
            ...lastMessage,
            parts: [...lastMessage.parts, {
              type: 'tool-call' as const,
              toolName: 'question_request',
              args: { description: questionText, questions: data.questions },
              toolCallId: data.id,
            }],
          }];
        }
        return prev;
      });
    };
    client.on('questionRequest', handleQuestionRequest);

    const handleElicitationRequest = (data: ElicitationRequestData, _env: Envelope) => {
      if (!data) return;
      interactionMapRef.current.set(data.id, { type: 'elicitation' });
      setMessages((prev) => {
        const lastMessage = prev[prev.length - 1];
        if (lastMessage?.role === 'assistant') {
          return [...prev.slice(0, -1), {
            ...lastMessage,
            parts: [...lastMessage.parts, {
              type: 'tool-call' as const,
              toolName: 'elicitation',
              args: { message: data.message, mcp_server_name: data.mcp_server_name, url: data.url },
              toolCallId: data.id,
            }],
          }];
        }
        return prev;
      });
    };
    client.on('elicitationRequest', handleElicitationRequest);

    setConnectionState('connecting');
    client.connect(sessionId).then(() => {
      setConnectionState('connected');
    }).catch((err) => {
      setConnectionState('disconnected');
      logger.error('RuntimeAdapter', 'Connection failed', { error: String(err) });
    });

    return () => {
      if (deltaRafId) cancelAnimationFrame(deltaRafId);
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
      client.off('permissionRequest', handlePermissionRequest);
      client.off('questionRequest', handleQuestionRequest);
      client.off('elicitationRequest', handleElicitationRequest);
      interactionMapRef.current.clear();
      client.disconnect();
      clientRef.current = null;
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
      logger.info('RuntimeAdapter', 'Client not connected, attempting reconnect');
      try {
        if (!client.connecting) {
          // Don't pass sessionId here — the client internally tracks the latest session ID,
          // which may have been updated by a SessionNotFound retry in BrowserHotPlexClient.
          client.connect().catch(err => {
            logger.error('RuntimeAdapter', 'Auto-connect failed', { error: String(err) });
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

    setMessages((prev) => [...prev, userMessage]);
    setIsRunning(true);
    startTurn(); // Begin timing for metrics

    // Send to HotPlex gateway with error handling
    try {
      client.sendInput(textContent);
    } catch (err) {
      logger.error('RuntimeAdapter', 'sendInput failed', { error: String(err) });
      // Remove user message since send failed
      setMessages((prev) => prev.slice(0, -1));
      throw new Error('Failed to send message. Please check your connection.');
    }
  }, []);

  const [isStopping, setIsStopping] = useState(false);
  const stoppingRef = useRef(false);

  const handleCancel = useCallback(async () => {
    if (stoppingRef.current) return;
    stoppingRef.current = true;
    setIsStopping(true);
    const client = clientRef.current;
    if (client?.connected) {
      client.sendControl('terminate');
    }
    setTimeout(() => {
      setIsRunning(false);
      setIsStopping(false);
      stoppingRef.current = false;
    }, 600);
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
      logger.warn('RuntimeAdapter', 'Failed to load earlier messages', { error: String(err) });
      return { hasMore: false };
    } finally {
      historyLoadingRef.current = false;
    }
  }, []);

  // Interaction response callback — routes to the correct send method
  const handleInteractionRespond = useCallback((toolCallId: string, allowed: boolean) => {
    const client = clientRef.current;
    if (!client) return;
    const entry = interactionMapRef.current.get(toolCallId);
    if (!entry) return;
    interactionMapRef.current.delete(toolCallId);

    switch (entry.type) {
      case 'permission':
        client.sendPermissionResponse(toolCallId, allowed);
        break;
      case 'question':
        client.sendQuestionResponse(toolCallId, { 'default': allowed ? 'yes' : 'no' });
        break;
      case 'elicitation':
        client.sendElicitationResponse(toolCallId, allowed ? 'accept' : 'decline');
        break;
    }
  }, []);

  // Deduped messages for assistant-ui. Two-layer dedup prevents the
  // MessageRepository "same id already exists" error:
  //   Layer 1: exact ID dedup
  //   Layer 2: content-signature dedup for assistant messages (catches live-vs-server
  //            duplicates where the streaming message has a different ID than the history record)
  // Also filters out internal-only parts (context-usage, turn-summary).
  const adapterMessages = useMemo(() => {
    const seenIds = new Set<string>();
    const seenAssistantSigs = new Set<string>();
    return messages
      .filter((m): m is HotPlexMessage => !!m && (m.role === 'user' || m.role === 'assistant'))
      .filter((m) => !m.parts.every(p => p.type === 'context-usage' || p.type === 'turn-summary'))
      .filter((m) => {
        // Layer 1: exact ID dedup
        if (seenIds.has(m.id)) return false;
        seenIds.add(m.id);
        // Layer 2: content-signature dedup for assistant messages
        if (m.role === 'assistant') {
          let sig = '';
          for (const p of m.parts) {
            if (p.type === 'text') {
              sig += (p as TextPart).text;
              if (sig.length >= CONTENT_SIG_PREFIX) break;
            }
          }
          sig = sig.slice(0, CONTENT_SIG_PREFIX);
          if (sig) {
            if (seenAssistantSigs.has(sig)) return false;
            seenAssistantSigs.add(sig);
          }
        }
        return true;
      });
  }, [messages]);

  const threadMessages = useMemo(
    () => adapterMessages.map((m) => convertToThreadMessage(m)),
    [adapterMessages]
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
    onInteractionRespond: handleInteractionRespond,
    isStopping,
  }), [sessionMetrics, historyHasMore, handleLoadHistory, handleInteractionRespond, isStopping]);

  // Return ExternalStoreAdapter — memoized to prevent unnecessary setAdapter calls
  // connectionState is returned separately to avoid invalidating the adapter memo on reconnect.
  return useMemo(() => ({
    // State
    isRunning,
    messages: adapterMessages,
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

    // Connection state — separate from adapter to avoid memo churn
    connectionState,
  } as ExternalStoreAdapter<HotPlexMessage> & { connectionState: ConnectionState }), [
    isRunning, adapterMessages, threadMessages, suggestions,
    handleSetMessages, handleNew, handleCancel, capabilities, extras,
  ]);
}
