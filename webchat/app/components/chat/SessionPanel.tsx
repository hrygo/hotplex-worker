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
      className={`session-row ${isActive ? 'active' : ''}`}
    >
      {/* Icon */}
      <div
        className="flex-shrink-0 w-8 h-8 rounded-lg flex items-center justify-center session-row-icon"
        data-active={isActive ? 'true' : undefined}
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5}
            d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
        </svg>
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`state-dot state-dot-${session.state}`} />
          <span
            className="text-xs font-mono truncate session-row-id"
            data-active={isActive ? 'true' : undefined}
          >
            {session.id.slice(0, 16)}…
          </span>
          {session.worker_type && (
            <span className="text-[10px] px-1.5 py-0.5 rounded font-mono session-row-worker">
              {WORKER_DISPLAY[session.worker_type] ?? session.worker_type}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5">
          <span
            className="text-[11px] session-row-state"
            data-active={isActive ? 'true' : undefined}
          >
            {stateLabel(session.state)}
          </span>
          <span className="text-[11px] text-[var(--text-faint)]">·</span>
          <span className="text-[11px] text-[var(--text-faint)]">
            {formatRelativeTime(session.updated_at)}
          </span>
          {session.turn_count != null && session.turn_count > 0 && (
            <>
              <span className="text-[11px] text-[var(--text-faint)]">·</span>
              <span className="text-[11px] text-[var(--text-faint)]">{session.turn_count} turns</span>
            </>
          )}
        </div>
      </div>

      {/* Delete */}
      {confirmDelete ? (
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          <button
            onClick={() => { onDelete(); setConfirmDelete(false); }}
            className="session-delete-confirm"
          >
            删除
          </button>
          <button
            onClick={() => setConfirmDelete(false)}
            className="session-delete-cancel"
          >
            取消
          </button>
        </div>
      ) : (
        <button
          onClick={(e) => { e.stopPropagation(); setConfirmDelete(true); }}
          className="session-delete-btn"
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

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 px-6 text-center">
      <BrandIcon size={56} />
      <p className="text-sm mb-1 text-[var(--text-secondary)]">暂无会话</p>
      <p className="text-xs mb-5 text-[var(--text-muted)]">创建一个新会话来开始</p>
      <button onClick={onCreate} className="btn-new-session" style={{ width: 'auto' }}>
        + 新建会话
      </button>
    </div>
  );
}

function SkeletonList() {
  return (
    <div className="space-y-2 p-4">
      {[1, 2, 3].map((i) => (
        <div key={i} className="skeleton-row">
          <div className="skeleton-circle" />
          <div className="flex-1 space-y-2">
            <div className="skeleton-line" style={{ width: '75%' }} />
            <div className="skeleton-line" style={{ width: '50%', opacity: 0.6 }} />
          </div>
        </div>
      ))}
    </div>
  );
}

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
      <button onClick={openPanel} className="session-toggle-btn">
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
        </svg>
        会话
        {sessions.length > 0 && (
          <span className="text-[10px] px-1.5 py-0.5 rounded-full font-mono session-count-badge">
            {sessions.length}
          </span>
        )}
      </button>
    );
  }

  return (
    <>
      <div className="session-panel-backdrop" onClick={closePanel} aria-hidden="true" />

      <div className="session-panel" role="dialog" aria-label="会话列表">
        {/* Header */}
        <div className="session-panel-header">
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded-lg flex items-center justify-center text-[var(--accent-amber)] bg-[var(--amber-light)]">
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 10h16M4 14h16M4 18h16" />
              </svg>
            </div>
            <h2 className="text-sm font-semibold text-[var(--text-primary)]">会话列表</h2>
            {sessions.length > 0 && (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full font-mono session-count-badge">
                {sessions.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => createNewSession()}
              disabled={isLoading}
              className="panel-icon-btn"
              aria-label="新建会话"
              title="新建会话"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 4v16m8-8H4" />
              </svg>
            </button>
            <button
              onClick={closePanel}
              className="panel-icon-btn"
              aria-label="关闭"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        {/* List */}
        <div className="flex-1 overflow-y-auto py-2">
          {isLoading && sessions.length === 0 ? (
            <SkeletonList />
          ) : error ? (
            <div className="px-5 py-8 text-center">
              <p className="text-sm mb-2 text-[var(--accent-coral)]">{error}</p>
              <button
                onClick={() => window.location.reload()}
                className="text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors"
              >
                刷新页面重试
              </button>
            </div>
          ) : sessions.length === 0 ? (
            <EmptyState onCreate={() => createNewSession()} />
          ) : (
            <div className="space-y-1 px-3 session-list-cascade">
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
        <div className="session-panel-footer">
          <button
            onClick={() => createNewSession()}
            disabled={isLoading}
            className="btn-new-session"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 4v16m8-8H4" />
            </svg>
            新建会话
          </button>
        </div>
      </div>
    </>
  );
}
