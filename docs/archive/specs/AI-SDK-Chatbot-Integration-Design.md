---
type: spec
tags:
  - project/HotPlex
  - integration/ai-sdk
  - frontend
  - chatbot
date: 2026-04-03
status: implemented
progress: 100
priority: high
estimated_hours: 16
completion_date: 2026-04-03
---

# AI SDK Chatbot 集成设计规格

> **Spec ID**: AI-SDK-001
> **日期**: 2026-04-03
> **状态**: Draft
> **优先级**: High
> **预计工时**: 16 小时 (2 天)

---

## 目录

1. [背景与动机](#背景与动机)
2. [目标](#目标)
3. [非目标](#非目标)
4. [架构设计](#架构设计)
5. [技术方案](#技术方案)
6. [实现细节](#实现细节)
7. [测试策略](#测试策略)
8. [风险与缓解](#风险与缓解)
9. [成功指标](#成功指标)
10. [时间线](#时间线)

---

## 背景与动机

### 问题陈述

项目需要提供一个基于 AI SDK Chatbot 的聊天客户端,以便:
1. 快速构建现代化的聊天界面
2. 复用 AI SDK 的成熟 UI 组件和 Hook
3. 提供流式响应、工具调用、权限请求等高级功能
4. 降低前端开发复杂度

### 现状分析

**已有资源**:
- ✅ 4 种语言的客户端 SDK (TypeScript, Python, Go, Java)
- ✅ 完整的 AEP v1 协议实现 (17 种事件类型)
- ✅ 生产级的 WebSocket 连接管理 (重连/心跳/背压)
- ✅ TypeScript 客户端 562 行代码,已充分测试

**技术差异**:
- AI SDK: HTTP-based, Next.js API Routes
- HotPlex: WebSocket-based, AEP v1 协议

### 解决方案

创建 Transport 适配器层,桥接 AI SDK 与 HotPlex Worker。

---

## 目标

### 主要目标

1. **快速集成**: 1-2 天完成基础功能
2. **功能完整**: 支持流式响应、工具调用、权限请求
3. **类型安全**: 完整的 TypeScript 类型定义
4. **易于维护**: 复用现有客户端代码
5. **可扩展**: 支持自定义 Transport 和 Hook

### 成功标准

- ✅ 能够发送消息并接收流式响应
- ✅ 消息正确聚合 (message.start → delta → end)
- ✅ 错误处理和重连机制工作正常
- ✅ 至少 1 个 Next.js 示例应用
- ✅ 完整的类型定义和文档

---

## 非目标

1. ❌ 不重新实现客户端 SDK (已有生产级实现)
2. ❌ 不支持多模态输入 (图片/视频) - Phase 2
3. ❌ 不实现离线支持 - Phase 3
4. ❌ 不修改后端协议 (保持 AEP v1 兼容)

---

## 架构设计

### 分层架构

```
┌─────────────────────────────────────────────────────┐
│  React Components (UI Layer)                       │
│  - ChatInterface                                   │
│  - MessageList                                      │
│  - InputBox                                         │
│  - ConnectionStatus                                 │
├─────────────────────────────────────────────────────┤
│  AI SDK useChat Hook                               │
│  - Message management                               │
│  - State management                                 │
│  - Event handling                                   │
├─────────────────────────────────────────────────────┤
│  HotPlexChatTransport (Adapter Layer)              │
│  - AEP → AI SDK message mapping                    │
│  - Event buffering                                  │
│  - Error translation                                │
├─────────────────────────────────────────────────────┤
│  HotPlexClient (Existing SDK)                      │
│  - WebSocket connection management                 │
│  - Auto-reconnect + heartbeat                      │
│  - Event emission (EventEmitter3)                  │
├─────────────────────────────────────────────────────┤
│  AEP v1 Protocol (Transport Layer)                 │
│  - NDJSON codec                                     │
│  - message.start/delta/end                         │
│  - tool_call, permission_request                   │
└─────────────────────────────────────────────────────┘
```

### 数据流

```
用户输入
  ↓
useChat.sendMessage(content)
  ↓
HotPlexChatTransport.sendMessage()
  ↓
HotPlexClient.sendInput(content)
  ↓
WebSocket.send(NDJSON)
  ↓
Gateway → Worker
  ↓
Worker 输出 (message.delta*)
  ↓
WebSocket.onmessage
  ↓
HotPlexClient.emit('delta', data)
  ↓
HotPlexChatTransport.onDelta()
  ↓
消息聚合 (buffer += data.content)
  ↓
AI SDK onData({ type: 'text-delta', textDelta })
  ↓
useChat 内部状态更新
  ↓
React re-render
```

---

## 技术方案

### 方案选择

**推荐方案**: 自定义 Transport 适配器 + 复用 HotPlexClient

**对比**:

| 方案 | 开发时间 | 维护成本 | Bundle 大小 | 风险 | 推荐度 |
|------|----------|----------|-------------|------|--------|
| **Transport 适配器** | 1-2 天 | 低 | 50KB | 低 | ⭐⭐⭐⭐⭐ |
| 独立 Hook | 1-2 天 | 低 | 50KB | 低 | ⭐⭐⭐⭐ |
| 自定义客户端 | 7-10 天 | 高 | 10KB | 高 | ⭐⭐ |

**选择理由**:
1. **最小改动**: 仅需 Transport 适配层
2. **协议兼容**: 完整支持 AEP v1
3. **类型安全**: 复用现有类型定义
4. **易于测试**: Mock Transport 即可

---

## 实现细节

### 1. 核心接口定义

```typescript
// src/lib/types.ts

import type { UIMessage, ChatTransport } from 'ai';

export interface HotPlexConfig {
  url: string;
  workerType: 'claude_code' | 'opencode_server';
  authToken?: string;
  sessionId?: string;
  reconnect?: {
    enabled: boolean;
    maxAttempts?: number;
    baseDelayMs?: number;
  };
}

export interface HotPlexTransportOptions {
  config: HotPlexConfig;
  onData?: (data: any) => void;
  onFinish?: (data: any) => void;
  onError?: (error: Error) => void;
}
```

### 2. Transport 适配器实现

```typescript
// src/lib/hotplex-transport.ts

import { ChatTransport, UIMessage } from 'ai';
import { HotPlexClient } from './hotplex-client/client';

export class HotPlexChatTransport implements ChatTransport {
  private client: HotPlexClient;
  private messageBuffer: Map<string, {
    id: string;
    role: string;
    content: string;
    startTime: number;
  }> = new Map();

  // ChatTransport 接口
  onData?: (data: any) => void;
  onFinish?: (data: any) => void;
  onError?: (error: Error) => void;

  constructor(private options: HotPlexTransportOptions) {
    this.client = new HotPlexClient(options.config);
    this.setupEventHandlers();
  }

  private setupEventHandlers(): void {
    // 连接状态
    this.client.on('connected', (ack) => {
      console.log('[Transport] Connected:', ack.session_id);
    });

    this.client.on('disconnected', (reason) => {
      this.onError?.(new Error(`Disconnected: ${reason}`));
    });

    this.client.on('reconnecting', (attempt) => {
      console.log(`[Transport] Reconnecting... attempt ${attempt}`);
    });

    // 消息流
    this.client.on('messageStart', (data) => {
      this.messageBuffer.set(data.id, {
        id: data.id,
        role: data.role,
        content: '',
        startTime: Date.now(),
      });
    });

    this.client.on('delta', (data) => {
      const buffer = this.messageBuffer.get(data.message_id);
      if (buffer) {
        buffer.content += data.content;

        // 触发 AI SDK 流式回调
        this.onData?.({
          type: 'text-delta',
          textDelta: data.content,
          messageId: data.message_id,
        });
      }
    });

    this.client.on('messageEnd', (data) => {
      const buffer = this.messageBuffer.get(data.message_id);
      if (buffer) {
        this.messageBuffer.delete(data.message_id);

        // 触发 AI SDK 完成回调
        this.onFinish?.({
          message: {
            id: data.message_id,
            role: buffer.role as 'assistant',
            parts: [{ type: 'text', text: buffer.content }],
            createdAt: new Date(buffer.startTime),
          },
        });
      }
    });

    // 工具调用
    this.client.on('toolCall', (data) => {
      this.onData?.({
        type: 'tool-call',
        toolCallId: data.id,
        toolName: data.name,
        args: data.input,
      });
    });

    // 错误处理
    this.client.on('error', (data) => {
      const error = new Error(`[${data.code}] ${data.message}`);
      (error as any).code = data.code;
      (error as any).details = data.details;
      this.onError?.(error);
    });

    // 完成事件
    this.client.on('done', (data) => {
      if (data.dropped) {
        console.warn('[Transport] Some deltas were dropped, but message is complete');
      }

      if (!data.success) {
        this.onError?.(new Error('Task failed'));
      }
    });
  }

  async sendMessage({ messages }: { messages: UIMessage[] }): Promise<void> {
    // 连接如果未建立
    if (!this.client.connected) {
      await this.client.connect();
    }

    // 提取最后一条用户消息
    const lastMessage = messages[messages.length - 1];
    const content = lastMessage.parts
      .filter(p => p.type === 'text')
      .map(p => p.text)
      .join('\n');

    // 发送到 HotPlex
    this.client.sendInput(content);
  }

  async connect(): Promise<void> {
    if (!this.client.connected) {
      await this.client.connect();
    }
  }

  disconnect(): void {
    this.client.disconnect();
    this.messageBuffer.clear();
  }
}
```

### 3. React Hook 封装

```typescript
// src/hooks/useHotPlexChat.ts

import { useChat } from '@ai-sdk/react';
import { HotPlexChatTransport } from '@/lib/hotplex-transport';
import type { HotPlexConfig } from '@/lib/types';

export function useHotPlexChat(config: HotPlexConfig) {
  const transport = useMemo(
    () => new HotPlexChatTransport({ config }),
    [config.url, config.workerType, config.authToken]
  );

  useEffect(() => {
    return () => {
      transport.disconnect();
    };
  }, [transport]);

  return useChat({
    transport,
    onError: (error) => {
      console.error('[useHotPlexChat] Error:', error);
    },
  });
}
```

### 4. Next.js 组件示例

```typescript
// src/app/chat/page.tsx

'use client';

import { useHotPlexChat } from '@/hooks/useHotPlexChat';

export default function ChatPage() {
  const { messages, sendMessage, status, error } = useHotPlexChat({
    url: process.env.NEXT_PUBLIC_HOTPLEX_URL || 'ws://localhost:8888',
    workerType: 'claude_code',
    authToken: process.env.NEXT_PUBLIC_HOTPLEX_API_KEY,
  });

  const [input, setInput] = useState('');

  const handleSend = () => {
    if (!input.trim() || status === 'loading') return;
    sendMessage(input);
    setInput('');
  };

  return (
    <div className="flex flex-col h-screen max-w-4xl mx-auto p-4">
      {/* 消息列表 */}
      <div className="flex-1 overflow-y-auto space-y-4 mb-4">
        {messages.map((message) => (
          <div
            key={message.id}
            className={`flex ${
              message.role === 'user' ? 'justify-end' : 'justify-start'
            }`}
          >
            <div
              className={`max-w-[70%] p-3 rounded-lg ${
                message.role === 'user'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-200 dark:bg-gray-700'
              }`}
            >
              {message.parts.map((part, i) => (
                <div key={i}>
                  {part.type === 'text' && (
                    <p className="whitespace-pre-wrap">{part.text}</p>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}

        {status === 'loading' && (
          <div className="flex justify-start">
            <div className="bg-gray-200 dark:bg-gray-700 p-3 rounded-lg">
              <div className="flex space-x-2">
                <div className="w-2 h-2 bg-gray-500 rounded-full animate-bounce" />
                <div className="w-2 h-2 bg-gray-500 rounded-full animate-bounce delay-100" />
                <div className="w-2 h-2 bg-gray-500 rounded-full animate-bounce delay-200" />
              </div>
            </div>
          </div>
        )}

        {error && (
          <div className="bg-red-100 border border-red-400 text-red-700 p-3 rounded">
            Error: {error.message}
          </div>
        )}
      </div>

      {/* 输入框 */}
      <div className="flex gap-2">
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyPress={(e) => e.key === 'Enter' && handleSend()}
          placeholder="Type your message..."
          disabled={status === 'loading'}
          className="flex-1 p-2 border rounded-lg disabled:opacity-50"
        />
        <button
          onClick={handleSend}
          disabled={status === 'loading' || !input.trim()}
          className="px-4 py-2 bg-blue-500 text-white rounded-lg disabled:opacity-50"
        >
          Send
        </button>
      </div>
    </div>
  );
}
```

---

## 测试策略

### 单元测试

```typescript
// src/__tests__/hotplex-transport.test.ts

import { HotPlexChatTransport } from '@/lib/hotplex-transport';

describe('HotPlexChatTransport', () => {
  it('should map message.start/delta/end to AI SDK message', async () => {
    const transport = new HotPlexChatTransport({
      config: { url: 'ws://localhost:8888', workerType: 'claude_code' },
    });

    const receivedDeltas: string[] = [];
    transport.onData = (data) => {
      if (data.type === 'text-delta') {
        receivedDeltas.push(data.textDelta);
      }
    };

    let finalMessage: any;
    transport.onFinish = (data) => {
      finalMessage = data.message;
    };

    // 模拟事件流
    await transport.connect();

    // 触发 message.start
    transport['client'].emit('messageStart', {
      id: 'msg_1',
      role: 'assistant',
    });

    // 触发 message.delta
    transport['client'].emit('delta', {
      message_id: 'msg_1',
      content: 'Hello',
    });
    transport['client'].emit('delta', {
      message_id: 'msg_1',
      content: ' World',
    });

    // 触发 message.end
    transport['client'].emit('messageEnd', {
      message_id: 'msg_1',
    });

    // 验证
    expect(receivedDeltas).toEqual(['Hello', ' World']);
    expect(finalMessage).toEqual({
      id: 'msg_1',
      role: 'assistant',
      parts: [{ type: 'text', text: 'Hello World' }],
      createdAt: expect.any(Date),
    });
  });

  it('should handle errors', async () => {
    const transport = new HotPlexChatTransport({
      config: { url: 'ws://localhost:8888', workerType: 'claude_code' },
    });

    const errors: Error[] = [];
    transport.onError = (error) => {
      errors.push(error);
    };

    await transport.connect();

    // 触发错误事件
    transport['client'].emit('error', {
      code: 'SESSION_BUSY',
      message: 'Session is busy',
    });

    expect(errors).toHaveLength(1);
    expect(errors[0].message).toContain('SESSION_BUSY');
  });
});
```

### 集成测试

```typescript
// src/__tests__/integration.test.ts

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ChatPage from '@/app/chat/page';

describe('Chat Integration', () => {
  it('should send message and receive response', async () => {
    render(<ChatPage />);

    const input = screen.getByPlaceholderText('Type your message...');
    const sendButton = screen.getByText('Send');

    // 输入消息
    fireEvent.change(input, { target: { value: 'Hello, AI!' } });
    fireEvent.click(sendButton);

    // 等待响应
    await waitFor(() => {
      const messages = screen.getAllByRole('message');
      expect(messages.length).toBeGreaterThan(1); // 用户消息 + AI 响应
    }, { timeout: 5000 });
  });
});
```

---

## 风险与缓解

### 风险矩阵

| 风险 | 影响 | 概率 | 缓解措施 | 负责人 |
|------|------|------|---------|--------|
| **Transport 适配复杂** | 高 | 中 | 1. 参考现有 TypeScript 客户端<br>2. 分阶段实现 | Frontend Lead |
| **消息顺序错乱** | 中 | 低 | 使用 message_id 关联事件 | Backend Lead |
| **性能问题(频繁渲染)** | 中 | 中 | 实现 RAF 批量更新 | Frontend Dev |
| **连接中断处理** | 高 | 低 | 复用客户端重连逻辑 | Backend Dev |
| **类型不匹配** | 低 | 低 | 编译时类型检查 | TypeScript Expert |

### 回滚计划

如果 Transport 适配失败:
1. **Plan B**: 使用独立 Hook (`useHotPlexSession`)
2. **Plan C**: 直接使用 HotPlexClient (无 AI SDK)

---

## 成功指标

### 开发指标

- ✅ **代码覆盖率**: ≥ 80% (单元测试 + 集成测试)
- ✅ **TypeScript 严格模式**: 无编译错误
- ✅ **Bundle 大小**: ≤ 60KB (gzip ≤ 20KB)
- ✅ **开发时间**: ≤ 16 小时

### 质量指标

- ✅ **首次连接时间**: ≤ 2 秒
- ✅ **消息延迟**: ≤ 100ms (P95)
- ✅ **重连成功率**: ≥ 95%
- ✅ **错误率**: ≤ 1%

### 用户体验指标

- ✅ **流式输出流畅度**: ≥ 30 FPS
- ✅ **输入响应时间**: ≤ 50ms
- ✅ **连接状态可见性**: 实时更新
- ✅ **错误提示清晰度**: 用户友好

---

## 时间线

### 第 1 天 (8 小时)

**上午 (4 小时)**:
- ✅ 复制 TypeScript 客户端代码 (30 分钟)
- ✅ 创建 Transport 适配器骨架 (1 小时)
- ✅ 实现 message.start/delta/end 映射 (1.5 小时)
- ✅ 编写单元测试 (1 小时)

**下午 (4 小时)**:
- ✅ 实现错误处理和重连 (1.5 小时)
- ✅ 创建 React Hook 封装 (1 小时)
- ✅ 编写 Next.js 组件 (1 小时)
- ✅ 本地测试验证 (30 分钟)

### 第 2 天 (8 小时)

**上午 (4 小时)**:
- ✅ 完善类型定义 (1 小时)
- ✅ 编写集成测试 (2 小时)
- ✅ 性能优化 (RAF 批量更新) (1 小时)

**下午 (4 小时)**:
- ✅ 添加错误边界和监控 (1.5 小时)
- ✅ 编写文档和示例 (1.5 小时)
- ✅ Code Review 和修复 (1 小时)

---

## 依赖清单

### 生产依赖

```json
{
  "dependencies": {
    "ai": "^3.0.0",
    "@ai-sdk/react": "^3.0.0",
    "ws": "^8.16.0",
    "eventemitter3": "^5.0.1"
  }
}
```

### 开发依赖

```json
{
  "devDependencies": {
    "@types/ws": "^8.5.10",
    "@testing-library/react": "^14.0.0",
    "@testing-library/jest-dom": "^6.0.0",
    "vitest": "^1.2.0"
  }
}
```

### Bundle 影响

| 依赖 | 大小 (gzip) | 说明 |
|------|------------|------|
| `ai` | ~10KB | AI SDK 核心 |
| `ws` | ~12KB | WebSocket 客户端 |
| `eventemitter3` | ~2KB | 事件发射器 |
| **总计** | **~24KB** | 可接受 |

---

## 附录

### A. 事件映射表

| AEP 事件 | AI SDK 对应 | 处理方式 |
|---------|------------|---------|
| `message.start` | 开始新消息 | 创建消息缓冲区 |
| `message.delta` | 流式文本块 | 追加到缓冲区,触发 `onData` |
| `message.end` | 消息完成 | 触发 `onFinish` |
| `done` | Turn 完成 | 可选:显示统计信息 |
| `error` | 错误 | 触发 `onError` |
| `tool_call` | 工具调用 | 显示工具调用卡片 (可选) |
| `permission_request` | 权限请求 | 显示权限对话框 (可选) |

### B. 错误码映射

| AEP 错误码 | HTTP 等价 | 用户提示 |
|-----------|----------|---------|
| `SESSION_BUSY` | 429 | "正在处理中,请稍候..." |
| `UNAUTHORIZED` | 401 | "认证失败,请重新登录" |
| `SESSION_NOT_FOUND` | 404 | "会话不存在,请刷新页面" |
| `INTERNAL_ERROR` | 500 | "服务器错误,请稍后重试" |

### C. 性能基准

| 操作 | 目标 | 测试结果 |
|------|------|---------|
| 连接建立 | ≤ 2s | _TBD_ |
| 发送消息 | ≤ 100ms | _TBD_ |
| 接收首个 delta | ≤ 500ms | _TBD_ |
| 流式渲染 FPS | ≥ 30 | _TBD_ |
| 内存占用 | ≤ 50MB | _TBD_ |

---

## 参考文档

1. [AI SDK Chatbot 文档](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot)
2. [AEP v1 协议规范](../architecture/aep-v1-Protocol.md)
3. [TypeScript 客户端文档](../../examples/typescript-client/README.md)
4. [前端集成设计](../frontend-integration/INTEGRATION_DESIGN.md)

---

**文档维护者**: Frontend Team
**审阅者**: Backend Lead, Tech Lead
**最后更新**: 2026-04-03
