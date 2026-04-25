'use client';

import { useState } from 'react';
import {
  AssistantRuntimeProvider,
  useExternalStoreRuntime,
} from '@assistant-ui/react';
import { useHotPlexRuntime } from '@/lib/adapters/hotplex-runtime-adapter';
import { useSessions } from '@/lib/hooks/useSessions';
import { Thread } from '@/components/assistant-ui/thread';
import { BrandIcon } from '@/components/icons';
import { SessionPanel } from './SessionPanel';
import { workerType } from '@/lib/config';

function ChatInterface({
  sessionId,
}: {
  sessionId: string | null;
}) {
  const runtime = useExternalStoreRuntime(
    useHotPlexRuntime({ sessionId: sessionId ?? undefined })
  );

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <Thread />
    </AssistantRuntimeProvider>
  );
}

export default function ChatContainer() {
  const [sidebarOpen, setSidebarOpen] = useState(true);

  const {
    activeSession,
    isLoading,
    selectSession,
    createNewSession,
    removeSession,
    sessions,
  } = useSessions({
    onSelect: () => {}, // Handled internally by useSessions
  });

  const activeSessionId = activeSession?.id || null;

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--bg-base)]">
      {/* PC Sidebar */}
      <aside className={`transition-all duration-300 ease-in-out ${sidebarOpen ? 'w-[280px]' : 'w-0'} overflow-hidden flex-shrink-0 relative z-30`}>
        <SessionPanel
          sessions={sessions}
          activeSession={activeSession}
          isLoading={isLoading}
          onSelect={selectSession}
          onCreate={createNewSession}
          onDelete={removeSession}
        />
      </aside>

      {/* Main Content Area */}
      <main className="flex-1 flex flex-col min-w-0 relative">
        {/* ... existing header ... */}
        <header className="h-14 flex items-center px-6 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] flex-shrink-0 z-20">
          <div className="flex items-center gap-4 w-full">
            <button 
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="p-2 -ml-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-elevated)] rounded-lg transition-all"
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
                  <h1 className="text-xs font-bold text-[var(--text-primary)] leading-none mb-0.5">HotPlex Agent</h1>
                  <p className="text-[9px] text-[var(--text-faint)] font-mono uppercase tracking-widest">Active • {workerType}</p>
               </div>
            </div>

            <div className="flex items-center gap-2">
               <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)]">
                  <div className={`w-1.5 h-1.5 rounded-full ${isLoading ? 'bg-amber-400 animate-pulse' : 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]'}`} />
                  <span className="text-[10px] font-bold text-[var(--text-secondary)]">{isLoading ? 'PREPARING...' : 'GATEWAY ONLINE'}</span>
               </div>
            </div>
          </div>
        </header>

        {/* Chat Thread */}
        <div className="flex-1 relative overflow-hidden">
          {(!activeSessionId && isLoading) ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] z-10 animate-fade-in">
              <div className="relative mb-6">
                <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-20 blur-2xl rounded-full animate-pulse" />
                <BrandIcon size={56} className="relative z-10 animate-float" />
              </div>
              <p className="text-sm font-medium text-[var(--text-secondary)] animate-pulse">Starting new session...</p>
            </div>
          ) : !activeSessionId ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] p-8 text-center">
               <div className="w-20 h-20 rounded-3xl bg-white shadow-xl border border-[var(--border-subtle)] flex items-center justify-center mb-8">
                  <BrandIcon size={60} />
               </div>
               <h2 className="text-xl font-display font-bold text-[var(--text-primary)] mb-3">Empower Your Coding</h2>
               <p className="text-sm text-[var(--text-muted)] max-w-sm mb-10 leading-relaxed">
                 Select an existing session from the sidebar or start a new high-fidelity coding conversation.
               </p>
               <button
                 onClick={() => createNewSession()}
                 className="px-8 py-3 rounded-full bg-[var(--text-primary)] text-white text-sm font-bold shadow-2xl hover:scale-105 active:scale-95 transition-all"
               >
                 {sessions.length === 0 ? 'Start Your First Project' : 'New Chat'}
               </button>
            </div>
          ) : (
            <ChatInterface
              key={activeSessionId}
              sessionId={activeSessionId}
            />
          )}
        </div>
      </main>
    </div>
  );
}
