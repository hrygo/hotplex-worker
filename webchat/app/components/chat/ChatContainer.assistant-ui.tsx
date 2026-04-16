'use client';

import { useCallback, useState } from 'react';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon } from '@/components/icons';
import { SessionPanel } from './SessionPanel';

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

export default function ChatContainer() {
  const url = process.env.NEXT_PUBLIC_HOTPLEX_WS_URL || 'ws://localhost:8888/ws';
  const workerType = process.env.NEXT_PUBLIC_HOTPLEX_WORKER_TYPE || 'claude_code';
  const apiKey = process.env.NEXT_PUBLIC_HOTPLEX_API_KEY || 'dev';

  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);

  const handleSessionSelect = useCallback((sessionId: string) => {
    setActiveSessionId(sessionId);
  }, []);

  return (
    <div className="flex flex-col h-screen bg-[var(--bg-base)]">
      {/* Header */}
      <header className="app-header">
        <div className="header-inner">
          <div className="flex items-center justify-between">
            {/* Brand */}
            <div className="flex items-center gap-3">
              <BrandIcon size={36} />
              <div>
                <h1 className="header-title">HotPlex AI</h1>
                <p className="header-subtitle">AEP v1 · gateway</p>
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
