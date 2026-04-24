/**
 * Session management hook for HotPlex webchat.
 *
 * Lifecycle:
 * 1. Mount → listSessions → auto-select most recent
 * 2. User selects session → calls onSelect(sessionId)
 * 3. User creates new → POST → calls onSelect(newId)
 * 4. User deletes → optimistically removes from list
 */

'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  listSessions,
  createSession,
  deleteSession,
  type SessionInfo,
} from '@/lib/api/sessions';

export interface UseSessionsOptions {
  /** Called when the active session changes (user selects or creates). */
  onSelect: (sessionId: string) => void;
  /** Initial session to restore (e.g., from URL or localStorage). */
  initialSessionId?: string | null;
}

export interface UseSessionsReturn {
  sessions: SessionInfo[];
  activeSession: SessionInfo | null;
  isLoading: boolean;
  error: string | null;
  isOpen: boolean;
  openPanel: () => void;
  closePanel: () => void;
  selectSession: (session: SessionInfo) => void;
  createNewSession: (workerType?: string) => Promise<void>;
  removeSession: (id: string) => Promise<void>;
  refreshSessions: () => Promise<void>;
}

export function useSessions({
  onSelect,
  initialSessionId,
}: UseSessionsOptions): UseSessionsReturn {
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [activeSession, setActiveSession] = useState<SessionInfo | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isOpen, setIsOpen] = useState(false);

  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;
  const initialRef = useRef(initialSessionId);
  initialRef.current = initialSessionId;

  const refreshSessions = useCallback(async () => {
    try {
      setError(null);
      const { sessions: list } = await listSessions(20, 0);
      setSessions(list.filter(s => s.state !== 'deleted'));

      // If we have an initial session, find it in the list
      const initId = initialRef.current;
      if (initId) {
        const found = list.find(s => s.id === initId);
        if (found) {
          setActiveSession(found);
          onSelectRef.current(found.id);
        }
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load sessions');
    } finally {
      setIsLoading(false);
    }
  }, []);

  // Load sessions on mount
  useEffect(() => {
    refreshSessions();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Auto-select most recent session if no initial and no active
  useEffect(() => {
    if (!isLoading && sessions.length > 0 && !activeSession && !initialRef.current) {
      const mostRecent = sessions.reduce((a, b) =>
        new Date(a.updated_at) > new Date(b.updated_at) ? a : b
      );
      setActiveSession(mostRecent);
      onSelectRef.current(mostRecent.id);
    }
  }, [isLoading, sessions, activeSession]);

  const selectSession = useCallback((session: SessionInfo) => {
    setActiveSession(session);
    onSelectRef.current(session.id);
    setIsOpen(false);
  }, []);

  const createNewSession = useCallback(async (workerType = 'claude_code') => {
    setIsLoading(true);
    try {
      const { session_id } = await createSession(workerType);
      
      // Force a slight delay to ensure the database has indexed the new session
      // (Optional, but helps with consistency in distributed environments)
      await new Promise(r => setTimeout(r, 100));
      
      await refreshSessions();
      onSelectRef.current(session_id);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create session');
    } finally {
      setIsLoading(false);
    }
  }, [refreshSessions]);

  const removeSession = useCallback(async (id: string) => {
    // Optimistic remove
    const prev = sessions;
    setSessions(sessions => sessions.filter(s => s.id !== id));
    if (activeSession?.id === id) {
      setActiveSession(null);
    }
    try {
      await deleteSession(id);
    } catch {
      // Rollback
      setSessions(prev);
      setError('Failed to delete session');
    }
  }, [sessions, activeSession]);

  return {
    sessions,
    activeSession,
    isLoading,
    error,
    isOpen,
    openPanel: () => setIsOpen(true),
    closePanel: () => setIsOpen(false),
    selectSession,
    createNewSession,
    removeSession,
    refreshSessions,
  };
}
