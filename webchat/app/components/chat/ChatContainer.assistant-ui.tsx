'use client';

import { useCallback, useState } from 'react';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { Thread } from '@/components/assistant-ui/thread';
import { SessionPanel } from './SessionPanel';

/**
 * ChatInterface — re-mounts when sessionId changes, creating a fresh runtime.
 * The key prop forces React to destroy+recreate the entire component tree,
 * which cleans up the old WebSocket connection and starts a new one.
 */
function ChatInterface({
  url,
  workerType,
  apiKey,
  sessionId,
}: {
  url: string;
  workerType: string;
  apiKey: string;
  sessionId: string | null;
}) {
  const runtime = useExternalStoreRuntime(
    useHotPlexRuntime({ url, workerType, apiKey, sessionId: sessionId ?? undefined })
  );

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread />
    </AssistantRuntimeProvider>
  );
}

/**
 * ChatContainer — main chat interface using assistant-ui + session management.
 *
 * Session lifecycle:
 * 1. Mount → useSessions loads session list
 * 2. Auto-select most recent → ChatInterface connects to that session
 * 3. User switches session → SessionPanel → ChatInterface remounts with new sessionId
 * 4. User creates session → POST → ChatInterface remounts with new sessionId
 */
export default function ChatContainer() {
  const url = process.env.NEXT_PUBLIC_HOTPLEX_WS_URL || 'ws://localhost:8888/ws';
  const workerType = process.env.NEXT_PUBLIC_HOTPLEX_WORKER_TYPE || 'claude_code';
  const apiKey = process.env.NEXT_PUBLIC_HOTPLEX_API_KEY || 'dev';

  // Drives ChatInterface remount on session switch
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);

  const handleSessionSelect = useCallback((sessionId: string) => {
    setActiveSessionId(sessionId);
  }, []);

  return (
    <div
      className="flex flex-col h-screen"
      style={{
        background: 'linear-gradient(180deg, #030712 0%, #0a0f1e 50%, #030712 100%)',
      }}
    >
      {/* Header */}
      <header
        className="flex-shrink-0 border-b"
        style={{
          background: 'rgba(3,7,18,0.8)',
          borderColor: 'rgba(51,65,85,0.3)',
          backdropFilter: 'blur(12px)',
        }}
      >
        <div className="max-w-3xl mx-auto px-4 py-3">
          <div className="flex items-center justify-between">
            {/* Brand */}
            <div className="flex items-center gap-3">
              <div
                className="w-9 h-9 rounded-xl flex items-center justify-center"
                style={{
                  background: 'linear-gradient(135deg, rgba(16,185,129,0.2) 0%, rgba(6,182,212,0.2) 100%)',
                  boxShadow: '0 0 16px rgba(16,185,129,0.2), inset 0 0 8px rgba(16,185,129,0.05)',
                  border: '1px solid rgba(16,185,129,0.3)',
                }}
              >
                <svg className="w-5 h-5 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                    d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
                </svg>
              </div>
              <div>
                <h1 className="text-base font-semibold text-slate-200">HotPlex AI</h1>
                <p className="text-[11px] text-slate-600 font-mono">AEP v1 · gateway</p>
              </div>
            </div>

            {/* Session switcher */}
            <SessionPanel
              onSessionSelect={handleSessionSelect}
              initialSessionId={activeSessionId}
            />
          </div>
        </div>
      </header>

      {/* Thread — key remount reconnects to new session */}
      <div className="flex-1 overflow-hidden">
        <ChatInterface
          key={activeSessionId ?? '__new__'}
          url={url}
          workerType={workerType}
          apiKey={apiKey}
          sessionId={activeSessionId}
        />
      </div>
    </div>
  );
}
