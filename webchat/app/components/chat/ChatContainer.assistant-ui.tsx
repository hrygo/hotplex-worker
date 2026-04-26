'use client';

import { useState, useEffect, useCallback } from 'react';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useQueryState, parseAsString } from 'nuqs';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { useSessions } from '@/lib/hooks/useSessions';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon } from '@/components/icons';
import { SessionPanel } from './SessionPanel';
import { NewSessionModal } from './NewSessionModal';
import { MetricsBar } from '@/components/assistant-ui/MetricsBar';
import { workerType, workDir } from '@/lib/config';
import type { SessionMetrics } from '@/lib/hooks/useMetrics';

function ChatInterface({
  sessionId,
  overrideWorkDir,
  onMetricsChange,
}: {
  sessionId: string | null;
  overrideWorkDir?: string;
  onMetricsChange?: (metrics: SessionMetrics) => void;
}) {
  const [skills, setSkills] = useState<string[]>([]);
  const adapter = useHotPlexRuntime({
    sessionId: sessionId ?? undefined,
    overrideWorkDir,
    onMetricsChange,
    onSkillsChange: setSkills,
  });
  const runtime = useExternalStoreRuntime(adapter);

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread skills={skills} />
    </AssistantRuntimeProvider>
  );
}

export default function ChatContainer() {
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [showNewModal, setShowNewModal] = useState(false);
  const [sessionMetrics, setSessionMetrics] = useState<SessionMetrics | null>(null);

  // nuqs deep link params
  const [urlWorker] = useQueryState('worker', parseAsString);
  const [urlDir] = useQueryState('dir', parseAsString);

  const {
    activeSession,
    isLoading,
    error: sessionError,
    selectSession,
    createNewSession,
    removeSession,
    sessions,
  } = useSessions({
    onSelect: () => {},
  });

  const activeSessionId = activeSession?.id || null;

  // Handle NewSessionModal confirm
  const handleModalConfirm = useCallback(async (title: string, wt: string, dir: string) => {
    setShowNewModal(false);
    await createNewSession(title, wt, dir || undefined);
  }, [createNewSession]);

  // Handle "New Chat" button — show modal for session config
  const handleCreateNew = useCallback(async () => {
    setShowNewModal(true);
  }, []);

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      {/* PC Sidebar */}
      <aside className={`transition-all duration-300 ease-in-out ${sidebarOpen ? 'w-[280px]' : 'w-0'} overflow-hidden flex-shrink-0 relative z-30`}>
        <SessionPanel
          sessions={sessions}
          activeSession={activeSession}
          isLoading={isLoading}
          onSelect={selectSession}
          onCreate={handleCreateNew}
          onDelete={removeSession}
        />
      </aside>

      {/* Main Content Area */}
      <main className="flex-1 flex flex-col min-w-0 relative">
        {/* Header — Workspace Awareness Bar */}
        <header className="h-14 flex items-center px-6 border-b border-[var(--border-subtle)] bg-[rgba(15,15,18,0.6)] backdrop-blur-xl flex-shrink-0 z-20">
          <div className="flex items-center gap-4 w-full">
            <button
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="p-2 -ml-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] rounded-lg transition-all active:scale-95"
              title={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>

            <div className="flex items-center gap-3 flex-1">
               <div className="md:hidden">
                 <BrandIcon size={28} />
               </div>
               <div>
                  <h1 className="text-xs font-display font-bold text-[var(--text-primary)] leading-none mb-0.5">HotPlex Agent</h1>
                  <p className="text-[9px] text-[var(--text-faint)] font-mono uppercase tracking-widest flex items-center gap-1.5">
                    <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] shadow-[0_0_6px_var(--accent-emerald)]" />
                    Active · {workerType}
                  </p>
                  {(urlDir || workDir) && (
                    <p className="text-[9px] text-[var(--text-faint)] font-mono mt-0.5 truncate max-w-[200px]" title={urlDir || workDir}>
                      {(() => {
                        const d = urlDir || workDir || '';
                        return d.length > 30 ? `…${d.slice(-28)}` : d;
                      })()}
                    </p>
                  )}
               </div>
            </div>

            {/* MetricsBar */}
            {sessionMetrics && sessionMetrics.turnCount > 0 && (
              <MetricsBar session={sessionMetrics} />
            )}

            <div className="flex items-center gap-2">
                {sessionError && (
                  <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[rgba(244,63,94,0.1)] border border-[rgba(244,63,94,0.2)]">
                    <div className="w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)]" />
                    <span className="text-[10px] font-bold text-[var(--accent-coral)]">{sessionError}</span>
                  </div>
                )}
               <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[var(--bg-glass)] backdrop-blur-xl border border-[var(--border-default)]">
                  <div className={`w-1.5 h-1.5 rounded-full ${isLoading ? 'bg-[var(--accent-gold)] animate-pulse' : sessionError ? 'bg-[var(--accent-coral)]' : 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]'}`} />
                  <span className="text-[10px] font-bold text-[var(--text-secondary)]">{isLoading ? 'PREPARING...' : sessionError ? 'ERROR' : 'GATEWAY ONLINE'}</span>
               </div>
            </div>
          </div>
        </header>

        {/* Chat Thread */}
        <div className="flex-1 relative overflow-hidden">
          {(!activeSessionId && isLoading) ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] z-10 animate-fade-in">
              <div className="relative mb-6">
                <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-15 blur-2xl rounded-full animate-pulse" />
                <BrandIcon size={56} className="relative z-10 animate-float" />
              </div>
              <p className="text-sm font-medium text-[var(--text-muted)] animate-pulse">Starting new session...</p>
            </div>
          ) : !activeSessionId ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] p-8 text-center">
               <div className="w-20 h-20 rounded-3xl glass-dark flex items-center justify-center mb-8">
                  <BrandIcon size={60} />
               </div>
               <h2 className="text-xl font-display font-bold text-[var(--text-primary)] mb-3">Empower Your Coding</h2>
               <p className="text-sm text-[var(--text-muted)] max-w-sm mb-10 leading-relaxed">
                 Select an existing session from the sidebar or start a new high-fidelity coding conversation.
               </p>
               <button
                 onClick={handleCreateNew}
                 className="px-8 py-3 rounded-full bg-[var(--accent-gold)] text-black text-sm font-bold shadow-[0_8px_32px_rgba(251,191,36,0.15)] hover:scale-105 active:scale-95 transition-all"
               >
                 {sessions.length === 0 ? 'Start Your First Project' : 'New Chat'}
               </button>
            </div>
          ) : (
            <ChatInterface
              key={activeSessionId}
              sessionId={activeSessionId}
              overrideWorkDir={urlDir ?? undefined}
              onMetricsChange={setSessionMetrics}
            />
          )}
        </div>
      </main>

      {/* New Session Modal */}
      {showNewModal && (
        <NewSessionModal
          onConfirm={handleModalConfirm}
          onCancel={() => setShowNewModal(false)}
          existingTitles={sessions.filter(s => s.title).map(s => s.title!)}
        />
      )}
    </div>
  );
}
