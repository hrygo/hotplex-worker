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
        <header className="h-[72px] flex items-center px-8 border-b border-[var(--border-subtle)] bg-[rgba(5,5,7,0.25)] backdrop-blur-3xl flex-shrink-0 z-30">
          <div className="flex items-center gap-6 w-full max-w-[1400px] mx-auto">
            <button
              onClick={() => setSidebarOpen(!sidebarOpen)}
              className="group p-2.5 -ml-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)] rounded-xl transition-all active:scale-90 border border-transparent hover:border-[var(--border-subtle)]"
              title={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
            >
              <svg className={`w-5 h-5 transition-transform duration-500 ${sidebarOpen ? '' : 'rotate-180'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M11 19l-7-7 7-7m8 14l-7-7 7-7" />
              </svg>
            </button>

            <div className="flex items-center gap-4 flex-1 min-w-0">
               <div className="hidden md:block relative group">
                 <div className="absolute inset-0 bg-[var(--accent-gold)] opacity-10 blur-lg rounded-full group-hover:opacity-20 transition-opacity" />
                 <BrandIcon size={32} className="relative z-10" />
               </div>
               <div className="min-w-0">
                  <div className="flex items-center gap-2 mb-0.5">
                    <h1 className="text-[13px] font-display font-bold text-[var(--text-primary)] tracking-tight leading-none">HOTPLEX SESSION</h1>
                    <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] text-[var(--text-faint)] border border-[var(--border-subtle)]">LIVE</span>
                  </div>
                  <div className="flex items-center gap-2.5">
                    <div className="flex items-center gap-1.5">
                      <span className="w-1.5 h-1.5 rounded-full bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)] animate-pulse-subtle" />
                      <span className="text-[10px] font-mono text-[var(--text-muted)] uppercase tracking-wider">{workerType}</span>
                    </div>
                    {(urlDir || workDir) && (
                      <>
                        <span className="text-[var(--text-faint)] text-[10px] opacity-30">/</span>
                        <p className="text-[10px] text-[var(--text-faint)] font-mono truncate max-w-[300px] hover:text-[var(--text-muted)] transition-colors cursor-default" title={urlDir || workDir}>
                          {(() => {
                            const d = urlDir || workDir || '';
                            return d.length > 40 ? `…${d.slice(-38)}` : d;
                          })()}
                        </p>
                      </>
                    )}
                  </div>
               </div>
            </div>

            {/* MetricsBar */}
            <div className="hidden lg:block h-8 w-px bg-[var(--border-subtle)] mx-2" />
            {sessionMetrics && sessionMetrics.turnCount > 0 && (
              <MetricsBar session={sessionMetrics} />
            )}

            <div className="flex items-center gap-3">
                {sessionError && (
                  <div className="flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-[rgba(244,63,94,0.08)] border border-[rgba(244,63,94,0.2)] animate-fade-in">
                    <div className="w-1.5 h-1.5 rounded-full bg-[var(--accent-coral)] shadow-[0_0_8px_var(--accent-coral)]" />
                    <span className="text-[10px] font-bold text-[var(--accent-coral)] tracking-tight">{sessionError.toUpperCase()}</span>
                  </div>
                )}
               <div className="flex items-center gap-2 px-3.5 py-1.5 rounded-xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] shadow-sm">
                  <div className={`w-1.5 h-1.5 rounded-full ${isLoading ? 'bg-[var(--accent-gold)] animate-pulse' : sessionError ? 'bg-[var(--accent-coral)]' : 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]'}`} />
                  <span className="text-[10px] font-mono font-bold text-[var(--text-secondary)] tracking-widest">{isLoading ? 'READYING...' : sessionError ? 'OFFLINE' : 'ONLINE'}</span>
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
            <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] p-8 overflow-hidden">
               <div className="absolute inset-0 bg-mesh opacity-20 pointer-events-none" />
               
               <div className="relative mb-14 group scale-90 sm:scale-100 transition-transform duration-700 ease-out hover:scale-105 transform-gpu" style={{ perspective: '2000px' }}>
                  {/* Cosmic Nebula Glow — Multi-spectral energy field */}
                  <div className="absolute inset-[-150px] bg-[radial-gradient(circle,var(--accent-gold)_0%,var(--accent-violet)_30%,transparent_70%)] opacity-10 blur-3xl animate-pulse-bloom pointer-events-none transform-gpu" />
                  
                  {/* Stardust Field — Micro-Macro stars (GPU Optimized) */}
                  <div className="absolute inset-[-200px] pointer-events-none opacity-20 transform-gpu">
                    {Array.from({ length: 24 }).map((_, i) => (
                      <div 
                        key={`star-${i}`}
                        className="absolute w-0.5 h-0.5 bg-white rounded-full animate-stardust transform-gpu"
                        style={{
                          top: `${(i * 137) % 100}%`,
                          left: `${(i * 151) % 100}%`,
                          animationDelay: `${(i * 0.3) % 5}s`,
                          opacity: ((i * 7) % 5) / 10 + 0.1
                        }}
                      />
                    ))}
                  </div>

                  {/* SVG Quantum Universe — Advanced Physics Algorithm */}
                  <svg className="absolute inset-[-120px] w-[calc(100%+240px)] h-[calc(100%+240px)] pointer-events-none overflow-visible" style={{ transform: 'rotateX(65deg) rotateY(0deg)', animation: 'rotateOrbit 30s linear infinite' }}>
                    <defs>
                      <filter id="quantum-glow-v3">
                        <feGaussianBlur stdDeviation="1.5" result="blur"/>
                        <feColorMatrix in="blur" type="matrix" values="1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 18 -7" result="glow"/>
                        <feComposite in="SourceGraphic" in2="glow" operator="over"/>
                      </filter>
                    </defs>
                    
                    {/* Transient Entanglement — Pulses with heartbeat */}
                    <g className="animate-pulse-bloom" style={{ animationDuration: '3s' }}>
                      <path d="M 50%,50% L 80,40" stroke="url(#line-grad-gold)" strokeWidth="0.5" opacity="0.1" fill="none">
                         <animate attributeName="stroke-dasharray" values="0,100;100,0" dur="3s" repeatCount="indefinite" />
                      </path>
                      <path d="M 50%,50% L 20,60" stroke="url(#line-grad-violet)" strokeWidth="0.5" opacity="0.1" fill="none">
                         <animate attributeName="stroke-dasharray" values="0,100;100,0" dur="3s" repeatCount="indefinite" />
                      </path>
                      <linearGradient id="line-grad-gold" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stopColor="var(--accent-gold)" stopOpacity="0" />
                        <stop offset="50%" stopColor="var(--accent-gold)" stopOpacity="1" />
                        <stop offset="100%" stopColor="var(--accent-gold)" stopOpacity="0" />
                      </linearGradient>
                      <linearGradient id="line-grad-violet" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stopColor="var(--accent-violet)" stopOpacity="0" />
                        <stop offset="50%" stopColor="var(--accent-violet)" stopOpacity="1" />
                        <stop offset="100%" stopColor="var(--accent-violet)" stopOpacity="0" />
                      </linearGradient>
                    </g>

                    {/* Orbit 1 — Velocity Modulated Gold Trail */}
                    <ellipse cx="50%" cy="50%" rx="110" ry="110" fill="none" stroke="var(--accent-gold)" strokeWidth="0.3" strokeDasharray="2 12" opacity="0.15" />
                    {[0, 0.04, 0.08, 0.12].map((d) => (
                      <circle key={`e1-v3-${d}`} r={3.5 - d * 10} fill="var(--accent-gold)" opacity={1 - d * 6} filter="url(#quantum-glow-v3)">
                        <animateMotion 
                          dur="8s" 
                          begin={`${d}s`} 
                          repeatCount="indefinite" 
                          path="M -110,0 a 110,110 0 1,0 220,0 a 110,110 0 1,0 -220,0"
                          calcMode="spline"
                          keyTimes="0;0.25;0.5;0.75;1"
                          keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1"
                        />
                      </circle>
                    ))}

                    {/* Orbit 2 — Velocity Modulated Violet Swarm */}
                    <ellipse cx="50%" cy="50%" rx="135" ry="135" fill="none" stroke="var(--accent-violet)" strokeWidth="0.3" strokeDasharray="1 10" opacity="0.1" />
                    {[0, 0.08, 0.16, 0.24].map((d) => (
                      <circle key={`e2-v3-${d}`} r={3 - d * 8} fill="var(--accent-violet)" opacity={0.8 - d * 2.5} filter="url(#quantum-glow-v3)">
                        <animateMotion 
                          dur="12s" 
                          begin={`${d}s`} 
                          repeatCount="indefinite" 
                          path="M -135,0 a 135,135 0 1,0 270,0 a 135,135 0 1,0 -270,0"
                          calcMode="spline"
                          keyTimes="0;0.5;1"
                          keySplines="0.4 0 0.2 1; 0.4 0 0.2 1"
                        />
                      </circle>
                    ))}
                  </svg>

                  {/* Nucleus Singularity */}
                  <div className="relative z-10 flex items-center justify-center group-hover:scale-110 transition-transform duration-1000 animate-quantum-wobble">
                     <div className="absolute inset-0 bg-gradient-to-tr from-[var(--accent-gold)] to-[var(--accent-violet)] opacity-40 blur-3xl rounded-full scale-150 animate-pulse-bloom transform-gpu" style={{ animationDuration: '3s' }} />
                     <BrandIcon size={100} className="relative z-20 drop-shadow-[0_0_40px_rgba(251,191,36,0.6)]" />
                  </div>
               </div>
               
               <div className="relative z-10 text-center max-w-lg animate-fade-in-up">
                 <h2 className="text-2xl sm:text-3xl font-display font-bold text-[var(--text-primary)] mb-3 tracking-tight">
                   Ready for <span className="text-gradient-gold">Next-Gen</span> Coding?
                 </h2>
                 <div className="flex flex-col sm:flex-row items-center justify-center gap-3 mb-10">
                   <button
                     onClick={handleCreateNew}
                     className="w-full sm:w-auto px-8 py-3.5 rounded-2xl bg-[var(--accent-gold)] text-black text-sm font-bold shadow-2xl hover:scale-105 active:scale-95 transition-all duration-300"
                   >
                     {sessions.length === 0 ? 'Start Your First Project' : 'New Session'}
                   </button>
                   <button className="w-full sm:w-auto px-6 py-3.5 rounded-2xl bg-[var(--bg-elevated)] text-[var(--text-secondary)] text-sm font-bold border border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] transition-all">
                     Documentation
                   </button>
                 </div>

                 <div className="grid grid-cols-2 gap-4 text-left">
                    <div className="p-4 rounded-2xl bg-gradient-to-b from-white/[0.03] to-transparent border border-white/[0.05] hover:border-[var(--border-gold)] transition-all duration-500 group cursor-pointer backdrop-blur-md">
                      <div className="text-[9px] font-mono text-[var(--accent-gold)] mb-1 uppercase tracking-[0.2em] opacity-60 group-hover:opacity-100 transition-opacity">Quick Start</div>
                      <div className="text-[14px] font-bold text-[var(--text-primary)] group-hover:translate-x-1 transition-transform">Analyze Repo</div>
                    </div>
                    <div className="p-4 rounded-2xl bg-gradient-to-b from-white/[0.03] to-transparent border border-white/[0.05] hover:border-[var(--border-emerald)] transition-all duration-500 group cursor-pointer backdrop-blur-md">
                      <div className="text-[9px] font-mono text-[var(--accent-emerald)] mb-1 uppercase tracking-[0.2em] opacity-60 group-hover:opacity-100 transition-opacity">Debug</div>
                      <div className="text-[14px] font-bold text-[var(--text-primary)] group-hover:translate-x-1 transition-transform">Fix Issues</div>
                    </div>
                 </div>
               </div>
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
