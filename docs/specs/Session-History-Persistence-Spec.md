---
type: spec
tags:
  - project/HotPlex
date: 2026-04-28
status: draft
progress: 40
---
# 会话持久化与按需分页加载 (Session History Persistence)

> **Issue**: #51
> **Status**: Draft
> **Date**: 2026-04-28

## 背景

当前 Webchat 页面在刷新后会丢失所有消息记录，尽管会话在后端是持久化的（`events` 表存储了所有 AEP 事件）。需要实现一套分级存储与按需加载机制，使页面刷新后能即时恢复最近消息，并支持加载更早的历史记录。

## 现有数据模型

### Events 表（已有）

```sql
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_events_session_seq ON events(session_id, seq);
```

- 每个 AEP 事件（delta, message, tool_call, reasoning, done 等）都以 JSON 形式存储
- `(session_id, seq)` 有唯一索引，天然支持按 seq 范围查询
- 现有 `MessageStore.Query(ctx, sessionID, fromSeq)` 返回 `fromSeq` 之后的所有事件

### 前端消息模型（已有）

```typescript
interface HotPlexMessage {
  id: string;
  role: 'user' | 'assistant' | 'system';
  parts: MessagePart[];   // TextPart | ReasoningPart | ToolCallPart
  createdAt: Date;
  status?: 'streaming' | 'complete' | 'error';
}
```

- 前端通过事件处理器（delta → message → done 流水线）实时将 AEP 事件转为 `HotPlexMessage`
- 消息状态存储在 React `useState` 中，无持久化

## 方案设计

**LocalStorage 即时恢复 + 服务端事件分页重放**

### 架构图

```
┌──────────────────────────────────────────────┐
│                   Frontend                    │
│                                               │
│  L1: React State (内存)                       │
│    ↑ setMessages()                            │
│    │                                          │
│  L2: LocalStorage (持久化最近 50 条)           │
│    ↑ saveMessages() / loadMessages()          │
│    │                                          │
│  History API: GET /api/sessions/{id}/history  │
│    ↑ fetchHistory(beforeSeq, limit)           │
│    │                                          │
│  Event Replay: replayEvents() → HotPlexMessage│
└──────────────────────────────────────────────┘
                      │
                      ▼
┌──────────────────────────────────────────────┐
│                   Backend                     │
│                                               │
│  MessageStore.QueryBefore(sessionID, seq, n)  │
│    → SQL: events WHERE seq < ? ORDER BY seq   │
│                                               │
│  GatewayAPI.GetHistory()                      │
│    → Auth + Ownership check                   │
│    → Return { events[], has_more, oldest_seq }│
└──────────────────────────────────────────────┘
```

### 设计决策

**为什么后端返回原始事件而非重构消息？**

1. 后端 `events` 表已有完整数据，无需额外处理
2. 前端已有事件到消息的转换知识（实时处理中每天在做）
3. 避免后端重复实现消息重建逻辑
4. API 通用性更强（未来可用于事件回放、调试等场景）

**为什么不用 `conversation` 表？**

`conversation` 表已有结构化消息数据，但：
- 目前未被 `MessageStore` 接口暴露，也未在生产中写入
- 缺少 reasoning/tool-call 详细信息（仅有 `tools_json` 摘要）
- 需要额外写入链路才能保证数据一致性
- 后续可作为优化方向，当前以 `events` 表为 source of truth

---

## 后端变更 (Go)

### 1. 新增 SQL 查询

**新建** `internal/session/sql/queries/message_store.query_events_before.sql`

```sql
-- query_events_before returns events before a given seq in descending order for pagination.
SELECT id, session_id, seq, event_type, payload_json
 FROM events WHERE session_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?;
```

### 2. 扩展 MessageStore 接口

**修改** `internal/session/message_store.go`

```go
// MessageStore interface 新增方法：
QueryBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*events.Envelope, error)
```

`SQLiteMessageStore` 实现：
- 加载新 SQL query（复用现有 `queries` map 机制）
- 返回 `[]*events.Envelope`（与 `Query` 返回格式一致）

### 3. GatewayAPI 历史接口

**修改** `internal/gateway/api.go`

```go
type GatewayAPI struct {
    auth     *security.Authenticator
    sm       SessionManager
    bridge   SessionStarter
    cfgStore *config.ConfigStore
    msgStore MessageStore  // 新增
}
```

**新增 handler**: `GetHistory(w http.ResponseWriter, r *http.Request)`

处理流程：
1. Auth 认证 → 获取 `userID`
2. 解析路径参数 `{id}` → `sessionID`
3. Ownership 校验 → `msgStore.GetOwner(ctx, sessionID)` 验证 `userID == ownerID`
4. 解析 query params：
   - `before_seq` → int64，默认 `math.MaxInt64`（获取最新事件）
   - `limit` → int，默认 50，上限 200
5. 调用 `msgStore.QueryBefore(ctx, sessionID, beforeSeq, limit+1)`（多取一条判断 has_more）
6. 返回 JSON：
   ```json
   {
     "events": [...],
     "has_more": true,
     "oldest_seq": 42
   }
   ```

### 4. 路由注册

**修改** `cmd/hotplex/routes.go`

```go
// 传入 MsgStore
gatewayAPI := gateway.NewGatewayAPI(auth, sm, bridge, deps.ConfigStore, deps.MsgStore)

// 注册路由
mux.HandleFunc("GET /api/sessions/{id}/history", withCORS(gatewayAPI.GetHistory))
mux.HandleFunc("OPTIONS /api/sessions/{id}/history", withCORS(func(w, r){}))
```

### 5. GatewayAPI 依赖接口

**修改** `internal/gateway/api.go` — 新增接口类型

```go
// MessageStore defines the history query interface needed by GatewayAPI.
type MessageStore interface {
    QueryBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*events.Envelope, error)
    GetOwner(ctx context.Context, sessionID string) (string, error)
}
```

> 注意：GatewayAPI 自己定义接口（依赖倒置），不需要导入 `session` 包。

---

## 前端变更 (React/TS)

### 5. 消息缓存工具

**新建** `webchat/lib/cache/message-cache.ts`

```typescript
const CACHE_VERSION = 1;
const MAX_CACHED_MESSAGES = 50;
const STORAGE_PREFIX = 'hotplex_msgs_';

interface CachedMessages {
  version: number;
  sessionId: string;
  messages: HotPlexMessage[];
  updatedAt: number;
}

function saveMessages(sessionId: string, messages: HotPlexMessage[]): void
function loadMessages(sessionId: string): HotPlexMessage[] | null
function clearMessages(sessionId: string): void
```

策略：
- 缓存最近 50 条消息（取 messages 数组末尾 50 条）
- 写入时带版本号，读取时校验版本
- `save` 由 adapter 中 200ms debounced 调用
- `done` 事件后立即写入（确保完整 turn 持久化）

### 6. 集成缓存到 Runtime Adapter

**修改** `webchat/lib/adapters/hotplex-runtime-adapter.ts`

变更点：
- **初始化**：`useState<HotPlexMessage[]>` 初始值从 `loadMessages(sessionId)` 获取
- **消息变更**：`useEffect` 监听 `messages` 变化，debounced 写入 localStorage
- **done 事件**：立即写入 localStorage
- **session 切换**：清理旧缓存，`loadMessages(newSessionId)` 加载新缓存

### 7. 事件重放工具

**新建** `webchat/lib/utils/event-replay.ts`

```typescript
function replayEvents(events: Envelope[]): HotPlexMessage[]
```

纯函数，将事件序列转为结构化消息：
- `input` 事件 → user message（`role: 'user'`）
- `message` 事件 → assistant message（`role: 'assistant'`，完整文本）
- `tool_call` 事件 → ToolCallPart
- `tool_result` 事件 → 更新对应 ToolCallPart 的 result
- `reasoning` 事件 → ReasoningPart
- 跳过 `message.delta`/`message.start`/`message.end`/`state`/`ping`/`pong`/`done`

按 seq 排序后，通过 `message.id` 分组聚合同一消息的事件。

### 8. 历史 API 客户端

**修改** `webchat/lib/api/sessions.ts`

```typescript
interface HistoryResponse {
  events: Envelope[];
  has_more: boolean;
  oldest_seq: number;
}

async function getSessionHistory(
  sessionId: string,
  beforeSeq?: number,
  limit?: number
): Promise<HistoryResponse>
```

### 9. "加载历史" UI

**修改** `webchat/components/assistant-ui/thread.tsx`

在 `<ThreadPrimitive.Messages>` 渲染区域顶部添加触发器：
- 有更多历史时显示 "Load earlier messages" 按钮
- Loading 状态显示 spinner
- 无更多历史时隐藏

通过 adapter 暴露的 `onLoadHistory` 回调触发。

### 10. 适配器集成历史加载

**修改** `webchat/lib/adapters/hotplex-runtime-adapter.ts`

新增方法：
```typescript
async function loadHistory(): Promise<{ hasMore: boolean }>
```

流程：
1. 获取当前最早消息的 `seq` 作为 `beforeSeq`
2. 调用 `getSessionHistory(sessionId, beforeSeq)`
3. `replayEvents(events)` → `HotPlexMessage[]`
4. 按 `id` 幂等合并到 `messages` 头部（去重）
5. 更新 `hasMore` 状态

---

## 验收标准映射

| # | 标准 | 实现方式 |
|---|------|---------|
| 1 | 刷新页面后，最近的对话记录能够瞬间恢复 | Phase 2: LocalStorage 缓存恢复 |
| 2 | 点击"加载历史"能成功拉取并渲染更早的消息 | Phase 3: History API + Event Replay |
| 3 | 历史消息中的工具调用、思考过程等 UI 状态还原准确 | Event Replay 处理 tool_call/reasoning 事件 |
| 4 | 跨 Session 切换时缓存能够正确隔离 | LocalStorage key 包含 sessionId |

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/session/sql/queries/message_store.query_events_before.sql` | 新建 | 逆序分页 SQL |
| `internal/session/message_store.go` | 修改 | 接口新增 `QueryBefore` + 实现 |
| `internal/gateway/api.go` | 修改 | 新增 `msgStore` 字段 + `GetHistory` handler |
| `cmd/hotplex/routes.go` | 修改 | 路由注册 + MsgStore 注入 |
| `webchat/lib/cache/message-cache.ts` | 新建 | LocalStorage 缓存工具 |
| `webchat/lib/utils/event-replay.ts` | 新建 | 事件重放纯函数 |
| `webchat/lib/api/sessions.ts` | 修改 | 新增 `getSessionHistory` |
| `webchat/lib/adapters/hotplex-runtime-adapter.ts` | 修改 | 缓存读写 + 历史加载集成 |
| `webchat/components/assistant-ui/thread.tsx` | 修改 | "Load earlier" UI 触发器 |

## 复用清单

| 已有资源 | 用途 |
|----------|------|
| `queries` map (`internal/session/queries.go`) | 自动加载新 SQL 文件 |
| `MessageStore.GetOwner` | 权限校验 |
| `GatewayAPI` handler 模式 | Auth + respondJSON + PathValue |
| `withCORS` wrapper | 路由层复用 |
| 前端 `Envelope` 类型 | 历史事件反序列化 |
| 前端 `HotPlexMessage` 类型 | 缓存和历史加载共用 |

## 未来优化方向

1. **conversation 表写入**：在 bridge 层写入结构化消息到 `conversation` 表，历史 API 可直接返回结构化消息，省去前端 replay 开销
2. **增量同步**：断线重连后仅拉取缺失 seq 范围的事件，而非全量
3. **虚拟滚动**：大量历史消息时使用虚拟列表渲染，避免 DOM 过多
4. **消息搜索**：基于 `conversation` 表实现全文搜索
