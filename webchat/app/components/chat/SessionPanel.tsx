'use client';

import { useMemo, useState } from 'react';
import { formatRelativeTime, type SessionInfo } from '@/lib/api/sessions';
import { BrandIcon, WORKER_DISPLAY, WorkerIcon } from '@/components/icons';

function getDisplayTitle(session: SessionInfo): string {
  return session.title || session.id.slice(0, 8);
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
  const displayTitle = getDisplayTitle(session);
  const workerName = WORKER_DISPLAY[session.worker_type] ?? session.worker_type;

  const displayPath = session.work_dir || 'No workspace';
  const parts = displayPath === '/' ? [] : displayPath.split('/');
  const lastSegment = parts.length ? (parts[parts.length - 1] || displayPath) : '/';
  const parentPath = parts.length > 1 ? parts.slice(0, -1).join('/') : '';

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={(e) => e.key === 'Enter' && onSelect()}
      className={`group relative mx-2 mb-2 p-4 rounded-[var(--radius-md)] border transition-all duration-300 cursor-pointer overflow-hidden ${
        isActive
          ? 'bg-[var(--bg-active)] border-[var(--border-active)] shadow-[var(--shadow-glow)]'
          : 'bg-[var(--bg-surface)] border-[var(--border-subtle)] hover:bg-[var(--bg-hover)] hover:border-[var(--border-default)]'
      }`}
    >
      <div className="flex flex-col gap-3.5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className={`p-1.5 rounded-[var(--radius-sm)] transition-colors duration-300 ${isActive ? 'bg-[var(--accent-gold)] text-[var(--text-contrast)]' : 'bg-[var(--bg-elevated)] text-[var(--text-secondary)]'}`}>
              <WorkerIcon type={session.worker_type} className="w-3.5 h-3.5" />
            </div>
            <span className={`text-[11px] font-display font-bold uppercase tracking-wider ${isActive ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}`}>
              {workerName}
            </span>
          </div>
          <span className={`text-[10px] font-mono px-2 py-0.5 rounded-[var(--radius-xs)] border transition-colors ${
            isActive ? 'text-[var(--accent-gold)] border-[var(--border-active)] bg-[var(--accent-gold-glow)]' : 'text-[var(--text-faint)] bg-[var(--bg-elevated)] border-[var(--border-subtle)]'
          }`}>
            {displayTitle}
          </span>
        </div>

        {/* Directory */}
        <div className="flex flex-col gap-0.5">
          <div className="flex items-center gap-2 text-[var(--text-primary)] font-semibold text-[13px] truncate">
            <svg className={`w-3.5 h-3.5 ${isActive ? 'text-[var(--accent-gold)]' : 'text-[var(--text-faint)]'} opacity-80`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
            </svg>
            <span className="truncate" title={displayPath}>{lastSegment}</span>
          </div>
          {parentPath && (
            <div className={`text-[11px] truncate pl-5.5 font-mono ${isActive ? 'text-[var(--text-muted)]' : 'text-[var(--text-faint)]'}`}>
              {parentPath}/
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between pt-3 border-t border-[var(--border-subtle)]">
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1.5">
                <div className={`w-1.5 h-1.5 rounded-full ${
                  session.state === 'running' ? 'bg-[var(--accent-emerald)] shadow-[0_0_8px_var(--accent-emerald)] animate-pulse' :
                  session.state === 'idle' ? 'bg-[var(--accent-gold)]' : 
                  session.state === 'terminated' ? 'bg-[var(--text-faint)]' : 'bg-[var(--accent-blue)]'
                }`} />
                <span className={`text-[11px] font-bold capitalize ${isActive ? 'text-[var(--text-primary)]' : 'text-[var(--text-muted)]'}`}>
                  {session.state}
                </span>
              </div>
              <span className="text-[10px] text-[var(--text-faint)] opacity-40">•</span>
              <span className={`text-[11px] font-medium ${isActive ? 'text-[var(--text-secondary)]' : 'text-[var(--text-faint)]'}`}>
                {formatRelativeTime(session.updated_at)}
              </span>
            </div>
            <div className={`flex items-center gap-1.5 text-[10px] font-medium ${isActive ? 'text-[var(--text-muted)]' : 'text-[var(--text-faint)]'}`}>
              <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span>{new Date(session.created_at).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })}</span>
            </div>
          </div>

          <div className="flex items-center">
            {confirmDelete ? (
              <div className="flex items-center gap-2 animate-fade-in">
                <button
                  onClick={(e) => { e.stopPropagation(); onDelete(); }}
                  className="text-[10px] font-bold text-[var(--accent-coral)] hover:underline"
                >
                  Confirm
                </button>
                <button
                  onClick={(e) => { e.stopPropagation(); setConfirmDelete(false); }}
                  className="text-[10px] font-bold text-[var(--text-faint)] hover:text-[var(--text-secondary)]"
                >
                  Cancel
                </button>
              </div>
            ) : (
              <button
                onClick={(e) => { e.stopPropagation(); setConfirmDelete(true); }}
                className="opacity-0 group-hover:opacity-100 p-1.5 rounded-[var(--radius-sm)] text-[var(--text-faint)] hover:text-[var(--accent-coral)] hover:bg-[var(--accent-coral)]/10 transition-all transform hover:scale-110"
                title="Delete session"
              >
                <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                </svg>
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-6 text-center animate-fade-in">
      <div className="w-16 h-16 rounded-2xl glass-dark flex items-center justify-center mb-6">
        <BrandIcon size={48} className="opacity-40" />
      </div>
      <p className="text-sm font-medium mb-1 text-[var(--text-primary)]">No sessions yet</p>
      <p className="text-xs text-[var(--text-muted)] mb-8 max-w-[180px] mx-auto leading-relaxed">
        Start a new conversation to begin your AI coding journey.
      </p>
      <button 
        onClick={onCreate} 
        className="px-6 py-2.5 rounded-full bg-[var(--accent-gold)] text-black text-sm font-bold shadow-[0_8px_20px_rgba(251,191,36,0.15)] hover:scale-105 active:scale-95 transition-all"
      >
        New Session
      </button>
    </div>
  );
}

interface SessionPanelProps {
  sessions: SessionInfo[];
  activeSession: SessionInfo | null;
  isLoading: boolean;
  onSelect: (session: SessionInfo) => void;
  onCreate: () => void;
  onDelete: (id: string) => Promise<void>;
}

export function SessionPanel({ 
  sessions, 
  activeSession, 
  isLoading, 
  onSelect, 
  onCreate, 
  onDelete 
}: SessionPanelProps) {
  const [searchQuery, setSearchQuery] = useState('');

  const filteredSessions = useMemo(() =>
    sessions
      .filter(s => getDisplayTitle(s).toLowerCase().includes(searchQuery.toLowerCase()))
      .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()),
    [sessions, searchQuery]
  );

  return (
    <div className="pc-sidebar flex flex-col h-full bg-[var(--bg-base)] border-r border-[var(--border-subtle)] w-[280px]">
      {/* Sidebar Header */}
      <div className="px-5 py-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-9 h-9 rounded-xl glass-dark flex items-center justify-center">
            <BrandIcon size={28} />
          </div>
          <div>
            <h2 className="text-sm font-display font-bold text-[var(--text-primary)]">HotPlex Sessions</h2>
            <p className="text-[10px] font-mono text-[var(--text-faint)] uppercase tracking-widest">Gateway v1</p>
          </div>
        </div>

        {/* New Session Button */}
        <button
          onClick={() => onCreate()}
          disabled={isLoading}
          className="w-full flex items-center justify-center gap-2 py-2.5 rounded-xl bg-[var(--accent-gold)] text-[var(--bg-base)] hover:bg-[var(--accent-gold-bright)] hover:text-black active:scale-95 transition-all shadow-[0_4px_16px_rgba(251,191,36,0.15)] font-bold text-xs disabled:opacity-50 disabled:cursor-not-allowed group/newchat"
        >
          {isLoading ? (
            <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
          ) : (
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
            </svg>
          )}
          {isLoading ? 'Creating...' : 'New Chat'}
        </button>
      </div>

      {/* Search */}
      <div className="px-5 mb-4">
        <div className="relative">
          <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-[var(--text-faint)]" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            id="session-search"
            name="session-search"
            type="text"
            placeholder="Search history..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full bg-[var(--bg-elevated)] border border-transparent rounded-xl pl-9 pr-4 py-2 text-xs text-[var(--text-primary)] focus:bg-[var(--bg-surface)] focus:border-[var(--border-bright)] transition-all placeholder:text-[var(--text-faint)]"
          />
        </div>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto px-2 pb-6 custom-scrollbar">
        <div className="px-3 mb-2 text-[10px] font-bold text-[var(--text-faint)] uppercase tracking-widest">
          Recent Conversations
        </div>
        
        <div className="space-y-1 session-list-cascade">
          {filteredSessions.map((session) => (
            <SessionRow
              key={session.id}
              session={session}
              isActive={activeSession?.id === session.id}
              onSelect={() => onSelect(session)}
              onDelete={() => onDelete(session.id)}
            />
          ))}
          
          {filteredSessions.length === 0 && !isLoading && (
            <div className="px-3 py-8 text-center">
              <p className="text-[11px] text-[var(--text-faint)]">No results found</p>
            </div>
          )}

          {isLoading && (
            <div className="px-3 py-4 flex justify-center">
              <div className="w-4 h-4 border-2 border-[var(--accent-gold)] border-t-transparent rounded-full animate-spin" />
            </div>
          )}
        </div>
      </div>

      {/* Sidebar Footer */}
      <div className="px-5 py-4 border-t border-[var(--border-subtle)]">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-[var(--bg-elevated)] flex items-center justify-center text-[var(--text-secondary)]">
             <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
             </svg>
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-[11px] font-bold text-[var(--text-primary)] truncate">Developer Portal</p>
            <p className="text-[9px] text-[var(--text-faint)] truncate">Manage Keys & API</p>
          </div>
        </div>
      </div>
    </div>
  );
}

