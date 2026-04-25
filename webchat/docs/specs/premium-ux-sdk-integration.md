# 架构与技术规范：HotPlex WebChat 生产级进阶方案

**版本**：1.1  
**状态**：已核准 (Approved Specs)  
**定位**：统一 AI 编码智能体网关 (HotPlex Gateway) 的企业级高保真 Web 客户端。

## 1. 设计哲学与架构原则 (Design Philosophy)

1. **SDK-Native (原生驱动)**：彻底摒弃自定义的复杂状态机。全面拥抱 `@assistant-ui/react` 的 `ExternalStoreAdapter` 与 Vercel AI SDK 的 Content Part 数据模型。
2. **Agent-Agnostic UI (智能体无关渲染)**：WebChat 需作为统一抽象层，无论是 Claude Code, OpenCodeServer 还是 Pi-mono，均提供一致的 "思考-执行-输出" 视觉体验。
3. **Developer-First (开发者优先体验)**：相比普通 Chat，需强化**工作目录感知 (Workspace Awareness)**、**高保真代码展示 (Monaco/Diff)** 和 **终端模拟 (Terminal Output)**。

## 2. 核心集成规范 (Core SDK Integration)

### 2.1 运行时桥接 (Runtime Bridge)
将当前的 `hotplex-runtime-adapter.ts` 升级为严格对齐 `assistant-ui` v0.12+ 的无头适配器：
- **状态托管**：仅负责 AEP v1 协议事件到 `ThreadMessageLike` 的无状态映射，将消息合并与排序完全交由 SDK。
- **并发能力**：开启 `allowEditing` (消息编辑/重试) 和 `allowBranching` (分支对话) 特性，赋予用户穿梭历史上下文的能力。

### 2.2 协议语义映射增强 (AEP v1 -> ContentPart)
| AEP 原始事件    | Assistant-UI 模型        | 渲染策略与高级特性                                               |
| :-------------- | :----------------------- | :--------------------------------------------------------------- |
| `message.delta` | `ContentPart.Text`       | 采用 SDK 内置平滑流式输出，解决首屏渲染抖动。                    |
| `reasoning`     | `ContentPart.Reasoning`  | 映射为专用的思考节点。默认折叠，提供 `duration` 和执行状态展示。 |
| `tool_call`     | `ContentPart.ToolCall`   | 工具执行期进入 `Executing` 脉冲骨架屏状态。                      |
| `tool_result`   | `ContentPart.ToolResult` | 根据工具类型触发 Generative UI (GenUI) 富文本渲染。              |
| `elicitation`   | Custom Tool/Action       | 渲染为交互式表单 (如处理 MCP 协议的鉴权/必填参数补全)。          |
| `error`         | `MessageStatus.Error`    | 提供高亮错误栈详情及快捷 Retry 动作条。                          |

## 3. 生成式 UI 规格 (Generative UI Specs) - AEP 深度支持

基于 AEP v1 协议中对工具调用的流式支持 (`tool_call` 与 `tool_result` 事件)，WebChat 将通过 `@assistant-ui/react` 的 `makeAssistantToolUI` 实现智能体操作的高保真物理可视化。

### 3.1 核心编程工具的渲染映射 (Coding Tools GenUI)

在 AEP 映射中，工具的执行状态通过 `status` 实时流转 (`running` -> `complete`)。针对 HotPlex 常见的高频工具，规范如下：

1. **文件变更类 (`edit_file` / `write_file` / `replace_file_content`)**
   - **AEP 数据提取**：从 `args.CodeContent` 或 `args.ReplacementContent` 获取目标代码。
   - **渲染规范 (GenUI)**：
     - 彻底弃用基础 Markdown 代码块，引入 `<MonacoDiffEditor />` 或 `react-diff-viewer`。
     - **执行中 (`running`)**：展示目标文件路径 `TargetFile` 和 "Patching..." 骨架屏。
     - **已完成 (`complete`)**：展示带行号的语法高亮代码块，并提供一键 "Copy Code" 动作条。

2. **命令执行类 (`run_command` / `bash`)**
   - **AEP 数据提取**：提取 `args.CommandLine` 作为输入，提取 `result.stdout` / `result.stderr` 作为输出。
   - **渲染规范 (GenUI)**：
     - 渲染为高仿真的 Terminal 面板（深色背景、JetBrains Mono 字体）。
     - **执行中 (`running`)**：渲染命令 `$ {args.CommandLine}`，光标闪烁，标识 "Executing..."。
     - **已完成 (`complete`)**：若返回 `stderr` 则高亮为红色警示；若 `stdout` 超过 15 行，则自动折叠输出内容，提供 "Expand Output" 按钮。

3. **搜索与检视类 (`grep_search` / `view_file`)**
   - **渲染规范 (GenUI)**：
     - 拦截原始 JSON 数组，将其渲染为结构化列表。代码搜索结果需高亮匹配项，并提供视觉引导线。

### 3.2 交互式表单与鉴权 (MCP Elicitation GenUI)

AEP 协议允许 Agent 挂起执行并发起反向请求（如危险操作的确认、MCP 鉴权）。
- **拦截确认 (`ask_permission` / `confirm`)**：
  - 阻断常规对话流，在消息区渲染交互式表单（Action Card）。
  - 提供醒目的 **Approve** (绿色) 和 **Reject** (红色) 按钮。
  - 用户交互后，前端通过拦截回调直接发送该事件的 Result 给 Gateway 恢复 Worker 进程。

## 4. 极致 UX 与 UI 落地规格 (Premium UX/UI Specs)

为了确保产品达到极客级的质感，避免落入常规工具的廉价感，开发实施时必须严格遵循以下具体的设计变量与交互规范。

### 4.1 核心视觉与令牌系统 (Color & Theme Tokens)
基于 Tailwind 4 的 CSS Variables 机制定义全局主题：
- **深色模式优先 (Dark Mode Primary)**: 编码网关工具强烈推荐以暗色系为主。
  - **背景色 (Background)**: 避免使用纯黑 (`#000000`)。推荐使用带有极低饱和度的深色底，如 `bg-zinc-950` (`#09090b`)，营造深邃的编码氛围。
  - **动态光效 (Mesh Gradient)**: 在界面背景或 Worker 活跃时触发动态流光效果。利用大范围的低透明度径向渐变，例如：`bg-[radial-gradient(ellipse_at_top,_var(--tw-gradient-stops))] from-indigo-900/15 via-zinc-950/0 to-zinc-950`。
  - **强调色 (Accent)**: 弃用浏览器默认颜色。状态反馈采用现代高亮色彩：成功/完成状态使用荧光绿 (`#00ff9d` 或 `emerald-400`)，错误警示使用朱砂红 (`#ff4d4d`)，焦点高亮使用电光蓝 (`#3b82f6`) 或品牌标志性的渐变紫。

### 4.2 材质与层次 (Glassmorphism & Elevation)
所有的悬浮组件（侧边栏 Sidebar、顶部导航 NavBar、Tool Call 卡片、配置 Modal）必须建立明确的 Z 轴层次：
- **基础材质 (Glass Panel)**: 
  - 运用 `backdrop-blur-xl` 或 `backdrop-blur-2xl`，提供强力的背景模糊，让底层代码或渐变透出轮廓。
  - 叠加半透明底色：如 `bg-white/5` (Dark 环境下)。
  - **关键点**：所有玻璃面板必须附带极细的内描边 (`border border-white/10`)，这是增强边缘光泽感、消除扁平感的绝对核心。
- **阴影系统 (Shadows)**: 弃用普通单调的投影。悬浮的输入框 (Composer) 或弹窗使用带颜色倾向的多层柔和投影，例如 `shadow-[0_8px_32px_rgba(0,0,0,0.5)]`。

### 4.3 专业级排版 (Typography)
实行严格的“多字体栈”策略，提升信息架构的区分度：
- **Headings (标题与重点数据)**: 使用 `font-outfit`。Outfit 具备现代几何质感，极适合展示指标数据、模块标题和 Logo。
- **Body (正文对话)**: 使用 `font-sans`（如 `Inter` 或 `Roboto`），确保大段对话文本的极佳阅读清晰度。
- **Code (代码与终端)**: 强制采用 `JetBrains Mono`、`Geist Mono` 或 `Fira Code` 等专业等宽字体 (`font-mono`)。字号需略小于正文（如 `text-[13px]` 或 `text-sm`），且推荐开启连字功能，增强代码美感。

### 4.4 动效与微交互 (Motion & Micro-interactions)
UI 状态的切换严禁“瞬间突变”，必须依托 `framer-motion` 提供符合物理直觉的过渡：
- **生成过程 (Streaming Feedback)**: 
  - 弃用传统的居中转圈 Spinner。在等待模型首字响应时，采用闪烁的光标或在输入框上方显示一条极细的流动光效进度条。
- **元素登场 (Layout Animation)**:
  - 消息气泡或 Tool 卡片生成时，统一使用上浮淡入动画：`initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }}`，配合 `spring` 缓动函数（如 `stiffness: 300, damping: 24`）。
- **交互反馈 (Hover/Active States)**:
  - 所有可点击元素在 Hover 时必须有亮度的明显抬升（`hover:bg-white/10`）；在点击 (Active) 时必须带有轻微的缩放反馈（`active:scale-95` 或 `active:scale-[0.98]`）。

### 4.5 开发空间感知 (Workspace Awareness)
- **沉浸式环境指示器**: 顶部 NavBar 需充当 IDE 的“状态栏”。它应当显示当前的 `WorkerType` 引擎（搭配其 Logo，如 Claude）以及当前终端挂载的绝对路径 `WorkDir`。路径过长时启用中间截断显示，Hover 时提供完整的 Tooltip。
- **极客指标看板**: 在会话属性面板或界面的角落，低调地展示每一次交互的消耗（如 Token 吞吐量、往返 Latency、执行时长），这些指标由 AEP `done.stats` 提取，赋予开发者“掌控一切”的专业级安全感。

## 5. 参数化与初始化体系 (Initialization & Config)

### 5.1 增强型新建会话 (Advanced New Session)
废弃单一的 "New Chat" 按钮，引入功能完备的配置弹窗或抽屉面板：
- **Worker 引擎**：图形化卡片单选 (Claude Code, OpenCodeServer)。
- **工作区绑定**：强制输入绝对路径，同时提供 "最近使用目录 (Recent WorkDirs)" 快捷入口。
- **高级策略注入**：允许覆盖默认 `Agent Config` 设定（选择特定的系统 Prompt，或开启/禁用危险指令）。

### 5.2 路由与深度链接 (Deep Linking)
- 支持 URL 预置初始化：`hotplex.ai/chat/new?worker=ocs&dir=/path/to/repo` 自动拉起对应环境。
- 参数前置校验：当目标 Worker 依赖 `dir` 但参数缺失时，阻断后台进程创建并呼出配置面板。

## 6. 演进路线图 (Roadmap)

- **Phase 1: 适配器重构与视觉焕新**
  - 完成 `ExternalStoreAdapter` 的重构，彻底剥离 UI 层的事件维护逻辑。
  - 引入 Tailwind 4 设计系统与标准的 Reasoning 折叠组件。
- **Phase 2: 工作区环境化与 GenUI 落地**
  - 实现新建会话的多维参数化面板 (`workDir`, `workerType`)。
  - 针对文件读写与终端执行落地特化 Tool UI。
- **Phase 3: 极客级高级能力**
  - 完全激活消息分支 (Branching) 与编辑重试 (Edit & Retry)。
  - 引入完整的 Token 与耗时指标监控大盘。

## 7. 实施细节与代码范式 (Implementation Details)

### 7.1 Runtime Adapter 消息流转 (ExternalStoreRuntime 最佳实践)
在 `hotplex-runtime-adapter.ts` 中，必须通过 `useMemo` 优化 AEP 数据到 `ThreadMessageLike` 的映射，并提供全量的事件 Hook：
```typescript
const threadMessages = useMemo(() => 
  messages.map((m): ThreadMessageLike => ({
    id: m.id,
    role: m.role,
    // 组装 reasoning, text 和 tool-call
    content: m.parts.map(part => {
      if (part.type === 'text') return { type: 'text', text: part.text };
      if (part.type === 'reasoning') return { type: 'reasoning', text: part.text };
      if (part.type === 'tool-call') return { 
        type: 'tool-call', 
        toolName: part.toolName, 
        args: part.args, 
        toolCallId: part.toolCallId,
        result: part.result
      };
    }),
    status: m.status === 'streaming' ? { type: 'running' } : { type: 'complete', reason: 'stop' }
  })), 
  [messages]
);

const runtime = useExternalStoreRuntime({
  messages: threadMessages,
  onNew: handleNewMessage, // 调用 client.sendInput
  onCancel: () => client.sendControl('terminate'),
  // 未来扩展
  // onEdit: handleEditMessage,
});
```

### 7.2 Tool UI 自定义渲染 (Generative UI)
利用 `@assistant-ui/react` 的 `makeAssistantToolUI` 覆写默认渲染逻辑，以 `run_command` 为例：
```tsx
import { makeAssistantToolUI } from "@assistant-ui/react";

export const RunCommandToolUI = makeAssistantToolUI({
  toolName: "run_command",
  render: ({ args, result, status }) => (
    <div className="terminal-mockup bg-black text-green-400 font-mono p-4 rounded-xl">
       <div className="command-input">$ {args.command}</div>
       {status === "running" && <div className="animate-pulse text-gray-500">Executing...</div>}
       {status === "complete" && <pre className="output text-sm overflow-x-auto">{result?.stdout}</pre>}
    </div>
  ),
});

// 在 Thread 中注册
<ThreadPrimitive.Messages components={{ ToolFallback: CustomToolFallback }} />
// 注：在 @assistant-ui v0.12 后期版本中，也推荐使用底层的 `makeAssistantTool` 搭配全局 Provider 进行更细粒度的控制。
```

### 7.3 工作区化路由参数管理
使用 URL Query 驱动会话初始化，必须引入 `nuqs` (Next.js URL Query State) 库进行状态同步：

**步骤 1：根布局注入 Adapter**
```tsx
// app/layout.tsx
import { NuqsAdapter } from 'nuqs/adapters/next/app'
export default function RootLayout({ children }) {
  return <html><body><NuqsAdapter>{children}</NuqsAdapter></body></html>
}
```

**步骤 2：强类型路由状态绑定**
```typescript
// app/chat/page.tsx
import { useQueryState, parseAsString } from 'nuqs';

// 采用强类型 parser 确保状态安全
const [workerType, setWorkerType] = useQueryState('worker', parseAsString.withDefault('claude_code'));
const [workDir, setWorkDir] = useQueryState('dir');

// 当参数完备时自动连接，缺失时渲染全屏的 <NewSessionConfigModal />
if (!workDir && workerType === 'claude_code') {
  return <NewSessionConfigModal onConfirm={(dir) => setWorkDir(dir)} />;
}
return <ChatUI workerType={workerType} workDir={workDir} />;
```

### 7.4 极致视觉与 Tailwind 4 落地
结合 `Tailwind v4` 提供的高级过滤器与变体，定义 Glassmorphism：
```css
/* globals.css */
@theme {
  --color-glass-bg: rgba(255, 255, 255, 0.6);
  --color-glass-border: rgba(255, 255, 255, 0.2);
}

.glass-panel {
  @apply bg-glass-bg backdrop-blur-2xl border border-glass-border shadow-[0_8px_32px_rgba(0,0,0,0.1)];
}
```

使用 `framer-motion` 为高频列表操作（如 Tool Call 堆叠）增加 Layout 动画：
```tsx
<motion.div layout initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }}>
  {/* Tool UI Card */}
</motion.div>
```

### 7.5 附件与多模态支持 (AttachmentAdapter)
为了支持代码文件与图片的拖拽解析，在初始化 Runtime 时必须配置 `AttachmentAdapter`。这极大地复用了 UI 组件的呈现能力，无需自行编写上传逻辑：
```typescript
import { 
  CompositeAttachmentAdapter, 
  SimpleImageAttachmentAdapter, 
  SimpleTextAttachmentAdapter 
} from "@assistant-ui/react";

// 在 Custom Runtime Hook 中初始化
const attachmentAdapter = useMemo(() => new CompositeAttachmentAdapter([
  new SimpleImageAttachmentAdapter(), // 自动转为 Base64 Data URL
  new SimpleTextAttachmentAdapter(),  // 自动提取纯文本文件内容
]), []);

const runtime = useExternalStoreRuntime({
  // ... 其他配置
  adapters: {
    attachments: attachmentAdapter
  }
});
```
