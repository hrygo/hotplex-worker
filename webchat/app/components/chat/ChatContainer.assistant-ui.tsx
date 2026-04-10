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
    <div className="flex flex-col h-screen" style={{ background: "var(--bg-base)" }}>
      {/* Header */}
      <header className="app-header">
        <div style={{ maxWidth: "46rem", margin: "0 auto", padding: "0.75rem 1.5rem" }}>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <BrandIcon size={36} />
              <div>
                <h1
                  style={{
                    fontSize: "0.9375rem",
                    fontWeight: 600,
                    color: "var(--text-primary)",
                    fontFamily: "var(--font-display)",
                    lineHeight: 1.3,
                  }}
                >
                  HotPlex AI
                </h1>
                <p
                  style={{
                    fontSize: "0.6875rem",
                    color: "var(--text-faint)",
                    fontFamily: "var(--font-mono)",
                    letterSpacing: "0.04em",
                    lineHeight: 1.3,
                  }}
                >
                  AEP v1 · gateway
                </p>
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

      {/* Thread */}
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
