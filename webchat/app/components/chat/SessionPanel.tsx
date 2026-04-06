'use client';

import { useState } from 'react';
import { formatRelativeTime, stateLabel, type SessionInfo } from '@/lib/api/sessions';
import { useSessions } from '@/lib/hooks/useSessions';

interface SessionPanelProps {
  /** Called when a session is selected — triggers reconnection in the runtime. */
  onSessionSelect: (sessionId: string) => void;
  initialSessionId?: string | null;
}

// State indicator dot with glow
function StateDot({ state }: { state: SessionInfo['state'] }) {
  const colors: Record<SessionInfo['state'], string> = {
    created: 'bg-cyan-400',
    running: 'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.8)]',
    idle: 'bg-amber-400',
    terminated: 'bg-slate-500',
    deleted: 'bg-slate-700',
  };
  return (
    <span
      className={`inline-block w-2 h-2 rounded-full ${colors[state] ?? 'bg-slate-500'}`}
      aria-label={state}
    />
  );
}

// Single session row
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
      className={`
        group relative flex items-center gap-3 px-4 py-3 cursor-pointer
        transition-all duration-150 rounded-xl border
        ${isActive
          ? 'bg-emerald-950/60 border-emerald-500/40 shadow-[0_0_12px_rgba(16,185,129,0.15)]'
          : 'bg-slate-900/50 border-transparent hover:bg-slate-800/70 hover:border-slate-700/50'
        }
      `}
    >
      {/* Active indicator */}
      {isActive && (
        <div className="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-8 bg-emerald-400 rounded-r shadow-[0_0_8px_rgba(52,211,153,0.6)]" />
      )}

      {/* Icon */}
      <div className={`flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center transition-colors ${
        isActive ? 'bg-emerald-500/20' : 'bg-slate-800 group-hover:bg-slate-700'
      }`}>
        <svg className={`w-4 h-4 ${isActive ? 'text-emerald-400' : 'text-slate-400'}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
            d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
        </svg>
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <StateDot state={session.state} />
          <span className={`text-xs font-mono truncate ${isActive ? 'text-emerald-400' : 'text-slate-400'}`}>
            {session.id.slice(0, 16)}…
          </span>
          {session.worker_type && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-500 font-mono">
              {session.worker_type.replace('claude_code', 'claude')}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span className={`text-[11px] ${isActive ? 'text-emerald-300/70' : 'text-slate-500'}`}>
            {stateLabel(session.state)}
          </span>
          <span className="text-slate-600">·</span>
          <span className="text-[11px] text-slate-600">
            {formatRelativeTime(session.updated_at)}
          </span>
          {session.turn_count != null && session.turn_count > 0 && (
            <>
              <span className="text-slate-600">·</span>
              <span className="text-[11px] text-slate-600">{session.turn_count} turns</span>
            </>
          )}
        </div>
      </div>

      {/* Delete button */}
      {confirmDelete ? (
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          <button
            onClick={() => { onDelete(); setConfirmDelete(false); }}
            className="px-2 py-1 text-[11px] rounded bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors"
          >
            删除
          </button>
          <button
            onClick={() => setConfirmDelete(false)}
            className="px-2 py-1 text-[11px] rounded bg-slate-700 text-slate-400 hover:bg-slate-600 transition-colors"
          >
            取消
          </button>
        </div>
      ) : (
        <button
          onClick={(e) => { e.stopPropagation(); setConfirmDelete(true); }}
          className="opacity-0 group-hover:opacity-100 p-1.5 rounded-lg hover:bg-red-500/10 text-slate-600 hover:text-red-400 transition-all"
          aria-label="删除会话"
        >
          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
              d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
          </svg>
        </button>
      )}
    </div>
  );
}

// Empty state
function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 px-6 text-center">
      <div className="w-14 h-14 rounded-2xl bg-slate-800/80 flex items-center justify-center mb-4 shadow-[0_0_20px_rgba(6,182,212,0.1)]">
        <svg className="w-7 h-7 text-cyan-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
            d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z" />
        </svg>
      </div>
      <p className="text-slate-400 text-sm mb-1">暂无会话</p>
      <p className="text-slate-600 text-xs mb-5">创建一个新会话来开始</p>
      <button
        onClick={onCreate}
        className="px-5 py-2 rounded-xl text-sm font-medium bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 border border-emerald-500/20 transition-all hover:shadow-[0_0_16px_rgba(52,211,153,0.2)]"
      >
        + 新建会话
      </button>
    </div>
  );
}

// Loading skeleton
function LoadingSkeleton() {
  return (
    <div className="space-y-2 p-4">
      {[1, 2, 3].map((i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3">
          <div className="w-8 h-8 rounded-lg bg-slate-800 animate-pulse" />
          <div className="flex-1 space-y-2">
            <div className="h-3 bg-slate-800 rounded w-3/4 animate-pulse" />
            <div className="h-2 bg-slate-800/60 rounded w-1/2 animate-pulse" />
          </div>
        </div>
      ))}
    </div>
  );
}

// Main SessionPanel component
export function SessionPanel({ onSessionSelect, initialSessionId }: SessionPanelProps) {
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
        className="group flex items-center gap-2 px-3 py-1.5 rounded-xl text-sm text-slate-400 hover:text-slate-200 hover:bg-slate-800/70 border border-transparent hover:border-slate-700/50 transition-all"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
            d="M4 6h16M4 10h16M4 14h16M4 18h16" />
        </svg>
        会话
        {sessions.length > 0 && (
          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-slate-800 text-slate-500 font-mono">
            {sessions.length}
          </span>
        )}
      </button>
    );
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm"
        onClick={closePanel}
        aria-hidden="true"
      />

      {/* Panel */}
      <div
        className="fixed right-0 top-0 h-full z-50 w-80 flex flex-col"
        style={{
          background: 'linear-gradient(180deg, rgba(3,7,18,0.97) 0%, rgba(8,15,35,0.97) 100%)',
          borderLeft: '1px solid rgba(100,116,139,0.15)',
          boxShadow: '-20px 0 60px rgba(0,0,0,0.6), -1px 0 0 rgba(16,185,129,0.08)',
        }}
        role="dialog"
        aria-label="会话列表"
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-slate-800/60">
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded-lg bg-emerald-500/10 flex items-center justify-center">
              <svg className="w-3.5 h-3.5 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2}
                  d="M4 6h16M4 10h16M4 14h16M4 18h16" />
              </svg>
            </div>
            <h2 className="text-sm font-semibold text-slate-200">会话列表</h2>
            {sessions.length > 0 && (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-slate-800 text-slate-500 font-mono">
                {sessions.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => createNewSession()}
              disabled={isLoading}
              className="p-1.5 rounded-lg hover:bg-emerald-500/10 text-slate-500 hover:text-emerald-400 transition-colors disabled:opacity-50"
              aria-label="新建会话"
              title="新建会话"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M12 4v16m8-8H4" />
              </svg>
            </button>
            <button
              onClick={closePanel}
              className="p-1.5 rounded-lg hover:bg-slate-800 text-slate-500 hover:text-slate-300 transition-colors"
              aria-label="关闭"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                  d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        {/* Session list */}
        <div className="flex-1 overflow-y-auto py-2">
          {isLoading && sessions.length === 0 ? (
            <LoadingSkeleton />
          ) : error ? (
            <div className="px-5 py-8 text-center">
              <p className="text-red-400/80 text-sm mb-2">{error}</p>
              <button
                onClick={() => window.location.reload()}
                className="text-xs text-slate-500 hover:text-slate-300 transition-colors"
              >
                刷新页面重试
              </button>
            </div>
          ) : sessions.length === 0 ? (
            <EmptyState onCreate={() => createNewSession()} />
          ) : (
            <div className="space-y-1 px-3">
              {sessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  isActive={activeSession?.id === session.id}
                  onSelect={() => selectSession(session)}
                  onDelete={() => removeSession(session.id)}
                />
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="px-5 py-3 border-t border-slate-800/60">
          <button
            onClick={() => createNewSession()}
            disabled={isLoading}
            className="w-full flex items-center justify-center gap-2 px-4 py-2.5 rounded-xl text-sm font-medium
              bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20 border border-emerald-500/20
              transition-all hover:shadow-[0_0_16px_rgba(52,211,153,0.15)]
              disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
                d="M12 4v16m8-8H4" />
            </svg>
            新建会话
          </button>
        </div>
      </div>
    </>
  );
}
