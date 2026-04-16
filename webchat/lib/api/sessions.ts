/**
 * Gateway API client for session management.
 *
 * These endpoints are on the same port as WebSocket (gateway :8888),
 * using api_key query param for auth.
 */

const BASE = process.env.NEXT_PUBLIC_HOTPLEX_WS_URL
  ?.replace(/^ws:\/\//, 'http://')
  .replace(/^wss:\/\//, 'https://')
  .replace(/\/ws\/?$/, '') // strip trailing /ws from WebSocket URL
  ?? 'http://localhost:8888';

function authParams() {
  const key = process.env.NEXT_PUBLIC_HOTPLEX_API_KEY ?? 'dev';
  return `?api_key=${encodeURIComponent(key)}`;
}

export interface SessionInfo {
  id: string;
  user_id: string;
  owner_id?: string;
  bot_id?: string;
  worker_type: string;
  state: SessionState;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  idle_expires_at?: string;
  turn_count?: number;
}

export type SessionState = 'created' | 'running' | 'idle' | 'terminated' | 'deleted';

export interface ListSessionsResponse {
  sessions: SessionInfo[];
  limit: number;
  offset: number;
}

export async function listSessions(limit = 20, offset = 0): Promise<ListSessionsResponse> {
  const res = await fetch(
    `${BASE}/api/sessions?api_key=${encodeURIComponent(process.env.NEXT_PUBLIC_HOTPLEX_API_KEY ?? 'dev')}&limit=${limit}&offset=${offset}`,
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (!res.ok) throw new Error(`listSessions failed: ${res.status}`);
  return res.json();
}

export async function createSession(workerType = 'claude_code'): Promise<{ session_id: string }> {
  const res = await fetch(
    `${BASE}/api/sessions?api_key=${encodeURIComponent(process.env.NEXT_PUBLIC_HOTPLEX_API_KEY ?? 'dev')}&worker_type=${workerType}`,
    { method: 'POST' }
  );
  if (!res.ok) throw new Error(`createSession failed: ${res.status}`);
  return res.json();
}

export async function deleteSession(id: string): Promise<void> {
  const res = await fetch(
    `${BASE}/api/sessions/${id}?api_key=${encodeURIComponent(process.env.NEXT_PUBLIC_HOTPLEX_API_KEY ?? 'dev')}`,
    { method: 'DELETE' }
  );
  if (!res.ok) throw new Error(`deleteSession failed: ${res.status}`);
}

export function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  const diffHour = Math.floor(diffMs / 3600000);
  const diffDay = Math.floor(diffMs / 86400000);

  const timeStr = date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });

  if (diffMin < 1) return `刚刚 ${timeStr}`;
  if (diffMin < 60) return `${diffMin} 分钟前`;
  if (diffHour < 24) return `今天 ${timeStr}`;
  if (diffDay === 1) return `昨天 ${timeStr}`;
  if (diffDay < 7) return `${diffDay} 天前`;
  return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' });
}

export function stateLabel(state: SessionState): string {
  const map: Record<SessionState, string> = {
    created: '待启动',
    running: '进行中',
    idle: '空闲',
    terminated: '已结束',
    deleted: '已删除',
  };
  return map[state] ?? state;
}
