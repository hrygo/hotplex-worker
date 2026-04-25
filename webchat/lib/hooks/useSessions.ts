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
import { workerType as defaultWorkerType } from '@/lib/config';

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
  createNewSession: (workerType?: string, workDir?: string) => Promise<void>;
  removeSession: (id: string) => Promise<void>;
  refreshSessions: () => Promise<void>;
  handleSessionSelect: (id: string) => void;
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

  const isCreating = useRef(false);
  const STORAGE_KEY = 'hotplex_active_session_id';
  const DEFAULT_WORKER_TYPE = defaultWorkerType;

  // Deterministic anchor session ID — ensures the first auto-created session
  // maps to the same server-side key via DeriveSessionKey(userID, workerType, clientSessionID, workDir).
  const MAIN_SESSION_ID = 'main';

  const refreshSessions = useCallback(async () => {
    try {
      setError(null);
      const { sessions: list } = await listSessions(20, 0);
      const filtered = list.filter(s => s.state !== 'deleted');
      setSessions(filtered);

      // 1. Try to restore from props (initialSessionId)
      const initId = initialRef.current;
      if (initId) {
        const found = filtered.find(s => s.id === initId);
        if (found) {
          setActiveSession(found);
          onSelectRef.current(found.id);
          return;
        }
      }

      // 2. Try to restore from localStorage for persistence
      const savedId = localStorage.getItem(STORAGE_KEY);
      if (savedId) {
        const found = filtered.find(s => s.id === savedId);
        if (found) {
          setActiveSession(found);
          onSelectRef.current(found.id);
          return;
        } else {
          // Stale ID found in storage but not in active list -> clear it
          localStorage.removeItem(STORAGE_KEY);
        }
      }

      // 3. Auto-select most recent if existing
      if (filtered.length > 0) {
        const mostRecent = filtered.reduce((a, b) =>
          new Date(a.updated_at) > new Date(b.updated_at) ? a : b
        );
        setActiveSession(mostRecent);
        onSelectRef.current(mostRecent.id);
        localStorage.setItem(STORAGE_KEY, mostRecent.id);
        return;
      }

      // 4. No sessions at all? Auto-create the first one to "map to same session" by default
      if (!initId && !savedId && filtered.length === 0 && !isCreating.current) {
        isCreating.current = true;
        try {
          const { session_id } = await createSession({ workerType: DEFAULT_WORKER_TYPE, sessionId: MAIN_SESSION_ID });
          
          const { sessions: updatedList } = await listSessions(5, 0);
          const newSession = updatedList.find(s => s.id === session_id);
          if (newSession) {
            setSessions([newSession]);
            setActiveSession(newSession);
            onSelectRef.current(newSession.id);
            localStorage.setItem(STORAGE_KEY, newSession.id);
          }
        } finally {
          isCreating.current = false;
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

  const selectSession = useCallback((session: SessionInfo) => {
    setActiveSession(session);
    onSelectRef.current(session.id);
    localStorage.setItem(STORAGE_KEY, session.id);
    setIsOpen(false);
  }, []);

  const createNewSession = useCallback(async (workerType?: string, workDir?: string) => {
    const wt = workerType || DEFAULT_WORKER_TYPE;
    if (isCreating.current) return;
    isCreating.current = true;
    setIsLoading(true);
    try {
      const { session_id } = await createSession({ workerType: wt, workDir });

      const { sessions: list } = await listSessions(20, 0);
      const filtered = list.filter(s => s.state !== 'deleted');
      setSessions(filtered);
      
      const newSession = filtered.find(s => s.id === session_id);
      if (newSession) {
        setActiveSession(newSession);
        onSelectRef.current(session_id);
        localStorage.setItem(STORAGE_KEY, session_id);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create session');
    } finally {
      setIsLoading(false);
      isCreating.current = false;
    }
  }, []);

  const removeSession = useCallback(async (id: string) => {
    // Optimistic remove
    setSessions((prev) => prev.filter((s) => s.id !== id));
    if (activeSession?.id === id) {
      setActiveSession(null);
      localStorage.removeItem(STORAGE_KEY);
    }

    try {
      await deleteSession(id);
    } catch (e) {
      console.error('Failed to delete session', e);
      // Revert optimistic remove on failure
      refreshSessions();
    }
  }, [activeSession, refreshSessions]);

  // Handle manual session selection
  const handleSessionSelect = useCallback((id: string) => {
    const session = sessions.find(s => s.id === id);
    if (session) {
      selectSession(session);
    }
  }, [sessions, selectSession]);

  return {
    sessions,
    activeSession,
    isLoading,
    error,
    isOpen,
    openPanel: () => setIsOpen(true),
    closePanel: () => setIsOpen(false),
    refreshSessions,
    createNewSession,
    removeSession,
    selectSession,
    handleSessionSelect,
  };
}
