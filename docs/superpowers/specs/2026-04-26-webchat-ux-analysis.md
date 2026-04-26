# WebChat UX 深度分析与优化方案

> **日期**: 2026-04-26 · **状态**: 调研阶段 · **范围**: webchat/ 模块全链路 UX

---

## 一、现状概览

### 技术栈

| 层级 | 技术 | 版本 |
|------|------|------|
| 框架 | Next.js (App Router) | 16.2.4 |
| UI | React + TypeScript | 19.2.5 / 6.0.3 |
| AI 组件 | @assistant-ui/react | 0.12.23 |
| AI SDK | @ai-sdk/react (Vercel) | 4.0.0-beta |
| 动画 | Framer Motion | 12.38.0 |
| 样式 | TailwindCSS | 4.2.2 |
| WebSocket | 自研 BrowserHotPlexClient (AEP v1) | — |
| Markdown | react-markdown + rehype-highlight | — |

### 核心组件

```
app/components/chat/
├── ChatContainer.assistant-ui.tsx   (195行) 主编排：session 管理 + sidebar + deep-link
├── SessionPanel.tsx                 (273行) 会话列表 sidebar：搜索、删除、worker 图标
└── NewSessionModal.tsx                      新建会话弹窗

components/assistant-ui/
├── thread.tsx                       (605行) 消息线程：渲染管线 + welcome + composer
├── MarkdownText.tsx                 (163行) Markdown 渲染 + 代码高亮 + 复制
├── CommandMenu.tsx                          / 命令菜单
├── MetricsBar.tsx                           会话指标
└── BranchSelector.tsx                       分支选择（未完成）

components/assistant-ui/tools/
├── TerminalTool.tsx                          终端输出
├── FileDiffTool.tsx                          文件 diff
├── SearchTool.tsx                            搜索结果
├── PermissionCard.tsx                        权限请求
└── ToolLoadingSkeleton.tsx                  工具加载骨架
```

### 消息流架构

```
用户输入 → Composer → onNew → client.sendInput(text)
                                    ↓ WebSocket AEP
                              Gateway → Worker
                                    ↓ 事件流
                    messageStart → reasoning → delta* → toolCall → toolResult → delta* → done
                                    ↓
                         hotplex-runtime-adapter.ts (672行)
                                    ↓
                         ExternalStoreAdapter → React 状态更新 → UI 渲染
```

---

## 二、逐项 UX 分析

### 2.1 会话创建与列表展示

#### 现状

| 维度 | 实现 | 评估 |
|------|------|------|
| 会话创建 | POST `/api/sessions` → 乐观更新 → 自动选中 | ✅ 基本流畅 |
| 会话列表 | `useSessions` hook → 一次性加载全部 | ⚠️ 无分页 |
| 分组方式 | 平铺列表，按时间倒序 | ❌ 无时间分组（Today/Yesterday/7d） |
| 标题生成 | 用户输入 title，无自动生成 | ❌ 主流产品均自动生成 |
| 删除 | 乐观删除 + 错误回滚 | ✅ 体验良好 |
| 搜索 | SessionPanel 有搜索框 | ✅ 有基础搜索 |
| 持久化 | localStorage 存 active session ID | ✅ 页面刷新可恢复 |

#### 行业基准（ChatGPT / Claude.ai / Cursor / v0）

- **时间分组**: Today / Yesterday / Previous 7 Days / Older — 所有主流产品标配
- **自动标题**: 从第一条用户消息自动生成，无需手动输入
- **归档语义**: assistant-ui 使用 Archive 而非 Delete，降低误操作心理负担
- **新建按钮**: sidebar 顶部常驻 `+ New Thread`，assistant-ui 模式 `<ThreadListPrimitive.New>`
- **会话置顶/收藏**: Claude.ai 支持 pin，ChatGPT 支持 move to folder

#### 优化建议

1. **时间分组**: SessionPanel 增加 Today/Yesterday/Previous 7d/Older 分组头
2. **自动标题**: 会话创建时显示 "New Chat"，收到首次 assistant 回复后用前 50 字符生成标题
3. **NewSessionModal 简化**: 去掉 title 字段，仅保留 worker_type 和 work_dir
4. **会话列表虚拟化**: 超过 50 个会话时启用虚拟滚动

---

### 2.2 消息发送与响应体验

#### 现状 — 状态生命周期

```
用户点击 Send
  → [空白] 无即时反馈
  → client.sendInput()
  → delta 开始到达
  → 流式文本渲染 + 闪烁光标
  → done → 完成
```

#### 核心问题

| 问题 | 严重度 | 说明 |
|------|--------|------|
| **无 "submitted" 状态** | 🔴 高 | 用户发消息后到首个 delta 到达之间无任何视觉反馈，感觉"卡了" |
| **无 Thinking 指示器** | 🔴 高 | 首个 delta 到达前的等待期（可能 5-15s）完全沉默 |
| **无 Stop 按钮** | 🟡 中 | 长时间生成无法中断 |
| **无消息时间戳** | 🟡 中 | 无法判断消息发送时间 |
| **无 partial response 保留** | 🟡 中 | 流中断时已接收内容可能丢失 |

#### 行业标准状态机（Vercel AI SDK v5/v6）

| Status | 含义 | UI 表现 |
|--------|------|---------|
| `submitted` | 消息已发送，等待首个 token | 跳动三点 / "Thinking..." / 呼吸光效 |
| `streaming` | token 持续到达 | 闪烁光标 + 逐字渲染 |
| `ready` | 完整响应已收到 | 隐藏指示器，启用输入 |
| `error` | 请求失败 | 错误消息 + Retry 按钮 |

**HotPlex 当前缺少 `submitted` 状态的处理。** 用户消息发出后，到首个 delta 之间的延迟没有任何 UI 反馈。这是最大的 UX 痛点。

#### 优化建议

1. **submitted 状态指示器**: 发送后立即在 assistant 消息位置显示：
   - 方案 A: 经典三点跳动动画（ChatGPT 风格）
   - 方案 B: 脉冲呼吸 avatar + "Thinking..." 文字（Claude.ai 风格）
   - 方案 C: 骨架消息气泡 + shimmer 动画

2. **Stop 生成按钮**: submitted/streaming 状态下在 composer 区域显示 Stop 按钮

3. **消息时间戳**: 每条消息下方显示相对时间（hover 显示绝对时间）

4. **partial response 保留**: 流中断时保留已接收内容 + 显示 "Continue generating" 按钮

---

### 2.3 连接状态与"活着"感知

#### 现状

| 维度 | 实现 | 评估 |
|------|------|------|
| WebSocket 心跳 | 10s ping / 5s pong 超时 / 2 次丢失 | ✅ 后端健壮 |
| 断线重连 | 指数退避 3s→30s / 最多 10 次 | ✅ 有重连机制 |
| **连接状态 UI** | ❌ 无 | 🔴 用户无法感知连接状态 |
| **离线指示** | ❌ 无 | 🔴 网络断开时用户茫然 |
| **重连进度** | ❌ 无 | 🟡 重连过程不可见 |

#### 行业模式

```
连接状态指示器位置选择:
A) 顶部横幅 — "Connection lost. Reconnecting..." (Slack 风格)
B) 输入框上方 — 状态点 + 文字 (Cursor 风格)
C) 页面角落 — 小圆点 + hover 详情 (轻量级)
```

#### 优化建议

1. **连接状态指示器**: 输入框上方显示连接状态点
   - 🟢 绿色 = 已连接
   - 🟡 黄色闪烁 = 重连中
   - 🔴 红色 = 断开连接
2. **全局离线横幅**: 网络完全断开时顶部显示横幅
3. **重连倒计时**: 重连时显示 "Reconnecting in 3s..."

---

### 2.4 工具调用可视化

#### 现状

已有组件：TerminalTool, FileDiffTool, SearchTool, PermissionCard, ToolLoadingSkeleton

| 维度 | 实现 | 评估 |
|------|------|------|
| 加载状态 | ToolLoadingSkeleton 脉冲动画 | ✅ 有 |
| 结果展示 | 各工具独立组件 | ✅ 完善 |
| 折叠/展开 | 部分支持 | 🟡 可改进 |
| 错误状态 | 基础处理 | 🟡 缺少 retry |
| **工具耗时显示** | ❌ 无 | 🟡 用户不知道工具执行了多久 |

#### 优化建议

1. 工具卡片显示执行耗时（"Read file · 1.2s"）
2. 工具执行出错时显示 retry 按钮
3. 长输出默认折叠，显示 "Show full output"

---

### 2.5 输入体验

#### 现状

| 维度 | 实现 | 评估 |
|------|------|------|
| Enter 发送 / Shift+Enter 换行 | ✅ | ✅ 标准 |
| 自动增高 textarea | ✅ | ✅ 有 |
| 命令菜单 | CommandMenu (`/` 触发) | ✅ 有 |
| **发送时输入框清空** | ⚠️ 需确认是否即时 | 🟡 应立即清空 |
| **Focus 管理** | ⚠️ 响应完成后未自动聚焦 | 🟡 应自动 |
| **空输入防护** | ⚠️ 需确认 | 🟡 防止空消息 |

#### 优化建议

1. 发送后立即清空输入框（乐观更新，不等服务端确认）
2. assistant 回复完成后自动聚焦输入框
3. 禁止发送空消息或纯空格
4. submitted/streaming 状态禁用发送按钮

---

### 2.6 滚动与消息导航

#### 现状

| 维度 | 实现 | 评估 |
|------|------|------|
| 自动滚动 | 基础实现 | 🟡 需确认是否智能 |
| **滚动位置恢复** | ❌ 无 | 🟡 切换会话后丢失位置 |
| **"新消息" 浮动按钮** | ❌ 无 | 🟡 用户上滚时不知道有新内容 |
| **消息搜索** | ❌ 无 | 🟡 无法搜索历史消息 |

#### 优化建议

1. **智能自动滚动**: 仅当用户在底部 150px 内时自动滚动
2. **"Jump to latest" 按钮**: 用户上滚时底部显示浮动按钮
3. **会话切换保持滚动位置**: 按会话 ID 缓存滚动偏移

---

### 2.7 错误处理

#### 现状

已有 error code 映射：TURN_TIMEOUT → "Session timeout", WORKER_CRASH → "Agent crashed", RATE_LIMITED, UNAUTHORIZED

| 维度 | 实现 | 评估 |
|------|------|------|
| 致命错误显示 | 内联在消息中 | ✅ 有 |
| 错误码映射 | 4 种错误码 | ✅ 有 |
| **Retry 按钮** | ❌ 无 | 🔴 用户只能重新输入 |
| **错误分类** | 基础 | 🟡 缺少网络/权限/超时分类 |
| **自动重试** | SESSION_BUSY 静默重试 | ✅ 部分有 |

#### 优化建议

1. 错误消息内联显示 + Retry 按钮（regenerate 语义）
2. 网络错误自动重试 3 次，之后才显示错误
3. 流中断时保留 partial response + "Continue generating" 按钮
4. 错误消息按类型区分样式（网络/权限/超时/服务端）

---

### 2.8 性能

| 问题 | 影响 | 建议 |
|------|------|------|
| 消息列表无虚拟化 | 1000+ 消息卡顿 | 引入 `@tanstack/react-virtual` |
| 全量加载会话 | 列表过多时慢 | 分页或懒加载 |
| highlight.js 同步高亮 | 大代码块阻塞渲染 | 改用 Web Worker 或延迟高亮 |
| streaming 频繁 setState | 每个 delta 触发重渲染 | 批量更新 / React.memo 消息组件 |

---

## 三、优先级矩阵

按 **用户感知 × 实现成本** 排序：

| # | 优化项 | 感知价值 | 实现成本 | 优先级 |
|---|--------|----------|----------|--------|
| 1 | **submitted 状态指示器** | 🔴 极高 | 🟢 低 | P0 |
| 2 | **连接状态指示器** | 🔴 高 | 🟢 低 | P0 |
| 3 | **Stop 生成按钮** | 🟡 高 | 🟢 低 | P0 |
| 4 | **会话时间分组** | 🟡 高 | 🟡 中 | P1 |
| 5 | **错误 Retry 按钮** | 🟡 高 | 🟢 低 | P1 |
| 6 | **发送即时反馈（清空输入框）** | 🟡 高 | 🟢 低 | P1 |
| 7 | **自动标题生成** | 🟡 中 | 🟡 中 | P1 |
| 8 | **消息时间戳** | 🟡 中 | 🟢 低 | P2 |
| 9 | **"Jump to latest" 按钮** | 🟡 中 | 🟡 中 | P2 |
| 10 | **工具调用耗时** | 🟡 中 | 🟢 低 | P2 |
| 11 | **消息列表虚拟化** | 🟡 中 | 🟡 中 | P2 |
| 12 | **Partial response 保留** | 🟡 中 | 🔴 高 | P3 |
| 13 | **消息搜索** | 🟡 中 | 🔴 高 | P3 |

---

## 四、待讨论决策点

以下是需要在设计阶段确认的方向性问题：

1. **submitted 状态指示器风格**: 三点跳动 vs 脉冲呼吸 vs 骨架气泡？
2. **连接状态指示器位置**: 输入框上方 vs 顶部横幅 vs 角落小点？
3. **会话分组粒度**: 仅按时间？还是支持手动分组/标签？
4. **自动标题策略**: 截取前 N 字符？还是调用 LLM 生成？
5. **Stop 按钮位置**: 替换发送按钮？还是独立按钮在消息区域？
6. **是否引入 assistant-ui 的 ThreadListRuntime**: 当前自研 useSessions hook vs 迁移到 assistant-ui 原生线程管理

---

## 五、参考资源

- [Vercel AI SDK - useChat status state machine](https://ai-sdk.dev/docs/ai-sdk-ui/chatbot)
- [Vercel AI SDK - Error Handling](https://ai-sdk.dev/docs/ai-sdk-ui/error-handling)
- [assistant-ui - ThreadListRuntime](https://assistant-ui.com/docs/api-reference/runtimes/ThreadListRuntime)
- [assistant-ui - makeAssistantToolUI](https://assistant-ui.com/docs/guides/ToolUI)
- [Vercel AI SDK v5 Migration - status vs isLoading](https://ai-sdk.dev/docs/migration-guides/migration-guide-5-0)
