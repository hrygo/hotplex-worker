'use client';

import { useState } from 'react';
import { formatRelativeTime, stateLabel, type SessionInfo } from '@/lib/api/sessions';
import { useSessions } from '@/lib/hooks/useSessions';
import { BrandIcon, WORKER_DISPLAY } from '@/components/icons';

interface SessionPanelProps {
  onSessionSelect: (sessionId: string) => void;
  initialSessionId?: string | null;
}

function SessionRow({
  session,
  isActive,
  onSelect,
  onDelete,
}: {
  session: SessionInfo;
  isActive: boolean;
  onSelect: () => void;
  onDelete: () => void;
}) {
  const [confirmDelete, setConfirmDelete] = useState(false);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => e.key === 'Enter' && onSelect()}
      className={`relative group px-4 py-3 rounded-xl transition-all duration-200 cursor-pointer border ${
        isActive 
          ? 'bg-[var(--amber-light)] border-[var(--amber-border)] shadow-[0_0_20px_rgba(217,119,6,0.05)]' 
          : 'bg-transparent border-transparent hover:bg-[var(--bg-elevated)] hover:border-[var(--border-default)]'
      }`}
    >
      <div className="flex items-center gap-3">
        {/* Status indicator */}
        <div className="relative">
          <div className={`w-2 h-2 rounded-full ${
            session.state === 'running' ? 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)]' :
            session.state === 'idle' ? 'bg-[var(--accent-gold)]' : 'bg-[var(--text-faint)]'
          }`} />
          {session.state === 'running' && (
            <div className="absolute inset-0 w-2 h-2 rounded-full bg-[var(--accent-emerald)] animate-ping opacity-40" />
          )}
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between gap-2">
            <span className={`text-xs font-mono font-medium truncate ${isActive ? 'text-[var(--text-primary)]' : 'text-[var(--text-secondary)]'}`}>
              {session.id.slice(0, 12)}...
            </span>
            {session.worker_type && (
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-[var(--bg-elevated)] border border-[var(--border-subtle)] text-[var(--text-muted)] font-mono uppercase tracking-wider scale-90 origin-right">
                {WORKER_DISPLAY[session.worker_type] ?? session.worker_type}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 mt-1">
            <span className="text-[10px] text-[var(--text-muted)] font-medium uppercase tracking-tight">
              {stateLabel(session.state)}
            </span>
            <span className="text-[10px] text-[var(--text-faint)]">•</span>
            <span className="text-[10px] text-[var(--text-faint)]">
              {formatRelativeTime(session.updated_at)}
            </span>
          </div>
        </div>

        {/* Actions */}
        <div className="flex items-center opacity-0 group-hover:opacity-100 transition-opacity ml-2">
          {confirmDelete ? (
            <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
              <button
                onClick={() => { onDelete(); setConfirmDelete(false); }}
                className="p-1 text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.1)] rounded transition-colors"
                title="Confirm delete"
              >
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                </svg>
              </button>
              <button
                onClick={() => setConfirmDelete(false)}
                className="p-1 text-[var(--text-muted)] hover:bg-[var(--bg-elevated)] rounded transition-colors"
                title="Cancel"
              >
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          ) : (
            <button
              onClick={(e) => { e.stopPropagation(); setConfirmDelete(true); }}
              className="p-1.5 text-[var(--text-faint)] hover:text-[var(--accent-coral)] hover:bg-[rgba(244,63,94,0.05)] rounded-lg transition-all"
              aria-label="Delete session"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-6 text-center animate-fade-in">
      <div className="w-16 h-16 rounded-2xl bg-[var(--bg-elevated)] border border-[var(--border-subtle)] flex items-center justify-center mb-6 shadow-xl">
        <BrandIcon size={32} className="opacity-40" />
      </div>
      <p className="text-sm font-medium mb-1 text-[var(--text-primary)]">No sessions yet</p>
      <p className="text-xs text-[var(--text-muted)] mb-8 max-w-[180px] mx-auto leading-relaxed">
        Start a new conversation to begin your AI coding journey.
      </p>
      <button 
        onClick={onCreate} 
        className="px-6 py-2.5 rounded-full bg-[var(--accent-gold)] text-white text-sm font-bold shadow-[0_8px_20px_rgba(217,119,6,0.2)] hover:scale-105 active:scale-95 transition-all"
      >
        New Session
      </button>
    </div>
  );
}

export function SessionPanel({ onSessionSelect, initialSessionId }: SessionPanelProps) {
  const [searchQuery, setSearchQuery] = useState('');
  const {
    sessions,
    activeSession,
    isLoading,
    error,
    isOpen,
    openPanel,
    closePanel,
    selectSession,
    createNewSession,
    removeSession,
  } = useSessions({
    onSelect: onSessionSelect,
    initialSessionId,
  });

  if (!isOpen) {
    return (
      <button 
        onClick={openPanel} 
        className="flex items-center gap-2 px-4 py-2 rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:border-[var(--accent-gold)] transition-all group"
      >
        <svg className="w-4 h-4 text-[var(--text-muted)] group-hover:text-[var(--accent-gold)] transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
        </svg>
        <span className="text-sm font-bold tracking-tight">SESSIONS</span>
        {sessions.length > 0 && (
          <span className="flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-[var(--accent-gold)] text-white text-[10px] font-black shadow-[0_2px_8px_rgba(217,119,6,0.3)]">
            {sessions.length}
          </span>
        )}
      </button>
    );
  }

  return (
    <>
      <div 
        className="fixed inset-0 z-[190] bg-black/60 backdrop-blur-sm animate-fade-in" 
        onClick={closePanel} 
        aria-hidden="true" 
      />

      <div className="side-panel" role="dialog" aria-label="Sessions">
        {/* Header */}
        <div className="px-6 py-6 border-b border-[var(--border-subtle)] bg-[var(--bg-base)]">
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-lg font-display font-bold tracking-tight text-gradient-gold">Conversations</h2>
            <button 
              onClick={closePanel}
              className="p-2 text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-elevated)] rounded-xl transition-all"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
          
          {/* Search */}
          <div className="relative mb-2">
            <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input 
              type="text"
              placeholder="Search history..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full bg-[var(--bg-elevated)] border border-[var(--border-subtle)] rounded-xl pl-10 pr-4 py-2.5 text-xs text-[var(--text-primary)] focus:outline-none focus:border-[var(--accent-gold)] focus:ring-1 focus:ring-[var(--accent-gold)] transition-all placeholder:text-[var(--text-faint)]"
            />
          </div>

          <p className="text-[10px] text-[var(--text-faint)] font-mono uppercase tracking-widest pl-1 mt-4">
            {sessions.filter(s => s.id.toLowerCase().includes(searchQuery.toLowerCase())).length} SESSIONS
          </p>
        </div>

        {/* List */}
        <div className="flex-1 overflow-y-auto py-4 custom-scrollbar">
          {error ? (
            <div className="px-8 py-12 text-center">
              <div className="w-12 h-12 rounded-full bg-[rgba(244,63,94,0.1)] flex items-center justify-center mx-auto mb-4">
                <svg className="w-6 h-6 text-[var(--accent-coral)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                </svg>
              </div>
              <p className="text-sm font-medium mb-2 text-[var(--accent-coral)]">Sync Failed</p>
              <p className="text-xs text-[var(--text-muted)] mb-6 leading-relaxed">{error}</p>
              <button 
                onClick={() => window.location.reload()}
                className="text-xs font-bold text-[var(--text-primary)] hover:text-[var(--accent-gold)] transition-colors underline underline-offset-4"
              >
                Reload Window
              </button>
            </div>
          ) : sessions.length === 0 && !isLoading ? (
            <EmptyState onCreate={() => createNewSession()} />
          ) : (
            <div className="px-3 space-y-1">
              {sessions
                .filter(s => s.id.toLowerCase().includes(searchQuery.toLowerCase()))
                .map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  isActive={activeSession?.id === session.id}
                  onSelect={() => { selectSession(session); closePanel(); }}
                  onDelete={() => removeSession(session.id)}
                />
              ))}
              {isLoading && (
                <div className="px-4 py-8 flex justify-center">
                  <div className="w-5 h-5 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="p-6 border-t border-[var(--border-subtle)] bg-[var(--bg-base)]">
          <button
            onClick={() => createNewSession()}
            disabled={isLoading}
            className="w-full py-3.5 rounded-2xl bg-[var(--bg-elevated)] border border-[var(--border-default)] text-[var(--text-primary)] font-bold text-sm flex items-center justify-center gap-3 hover:border-[var(--accent-gold)] hover:bg-[var(--amber-light)] active:scale-95 transition-all group shadow-xl"
          >
            <div className="w-6 h-6 rounded-lg bg-[var(--accent-gold)] flex items-center justify-center text-white group-hover:scale-110 transition-transform">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M12 4v16m8-8H4" />
              </svg>
            </div>
            New Conversation
          </button>
        </div>
      </div>
    </>
  );
}

