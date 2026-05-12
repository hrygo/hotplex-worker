# WebChat 设置与使用

> HotPlex 内置 Web Chat UI 的安装、配置和开发指南

## 概述

WebChat 是 HotPlex Gateway 内置的 Web 聊天界面，使用 Next.js 16 + React 19 构建，通过 `go:embed` 嵌入到 Gateway 二进制中。无需额外部署，启动 Gateway 即可使用。

## 快速开始

### 生产模式

Gateway 启动后直接访问：

```
http://localhost:8888/
```

无需单独启动 WebChat 服务，静态文件已嵌入 Gateway 二进制。

### 开发模式

开发模式下支持热更新，前后端分离运行：

```bash
# 方式一：使用 Make 命令（推荐）
make dev              # 同时启动 Gateway + WebChat

# 方式二：手动启动
# 终端 1：启动 Gateway
make run              # 构建 + 运行 Gateway（端口 8888）

# 终端 2：启动 WebChat 开发服务器
cd webchat
pnpm dev              # 启动 Next.js dev server（端口 3000）
```

开发模式下访问 `http://localhost:3000`，Next.js dev server 提供热更新。

### 构建与嵌入

```bash
cd webchat
pnpm build            # 构建 Next.js 静态导出（输出到 out/）
                      # go:embed 会将 out/ 目录嵌入到 Gateway 二进制
```

构建产物位于 `webchat/out/`，Gateway 启动时通过 `go:embed` 读取这些文件并直接提供服务。

## 功能特性

### 聊天功能

- **流式输出**：实时显示 AI 响应，使用 AEP v1 的 `message.start` / `message.delta` / `message.end` 三段式协议
- **Markdown 渲染**：支持 GFM（GitHub Flavored Markdown），使用 `react-markdown` + `remark-gfm`
- **代码高亮**：使用 `highlight.js` + `rehype-highlight`，支持多种编程语言
- **暗色模式**：支持亮/暗主题切换

### Session 管理

WebChat 提供 Session 面板（`SessionPanel` 组件）：

- **新建 Session**：点击 "+" 按钮创建新对话
- **切换 Session**：在侧边栏 Session 列表中点击切换
- **删除 Session**：删除不需要的对话

Session 信息包括：
- Session 标题（`SessionInfo.Title`）
- 创建时间
- 当前状态

### 交互功能

- **权限请求**：AI 请求执行敏感操作时弹出审批对话框
- **问题交互**：AI 提出选择题时显示选项卡片
- **Tool 调用展示**：实时显示 AI 正在使用的工具

## 技术栈

| 依赖 | 版本 | 用途 |
|------|------|------|
| Next.js | ^16.2.4 | React 框架 |
| React | ^19.2.5 | UI 库 |
| Tailwind CSS | ^4.2.2 | 样式框架 |
| @assistant-ui/react | ^0.14.0 | AI 聊天 UI 组件库 |
| ai (Vercel AI SDK) | 7.0.0-beta | AI SDK 集成 |
| react-markdown | ^10.1.0 | Markdown 渲染 |
| highlight.js | ^11.11.1 | 代码语法高亮 |
| framer-motion | ^12.38.0 | 动画库 |
| nuqs | ^2.8.9 | URL 状态管理 |

## 项目结构

```
webchat/
├── app/
│   ├── page.tsx                         # 主页面
│   ├── layout.tsx                       # 根布局
│   ├── error.tsx                        # 错误边界
│   ├── global-error.tsx                 # 全局错误边界
│   └── components/
│       ├── chat/
│       │   ├── ChatContainer.assistant-ui.tsx  # 聊天容器
│       │   ├── SessionPanel.tsx                 # Session 管理
│       │   └── NewSessionModal.tsx              # 新建 Session
│       └── ui/
│           └── CopyButton.tsx                   # 复制按钮
├── components/
│   ├── assistant-ui/                    # assistant-ui 自定义组件
│   └── icons.tsx                        # 图标组件
├── lib/
│   ├── adapters/                        # AEP 协议适配器
│   ├── ai-sdk-transport/                # Vercel AI SDK 传输层
│   ├── api/                             # Gateway API 客户端
│   ├── hooks/                           # React Hooks
│   ├── types/                           # TypeScript 类型定义
│   ├── config.ts                        # 配置
│   ├── highlight.ts                     # 高亮配置
│   ├── logger.ts                        # 日志
│   ├── utils.ts                         # 工具函数
│   └── tool-categories.ts               # Tool 分类
├── e2e/                                 # Playwright E2E 测试
├── package.json
├── tsconfig.json
├── next.config.mjs
└── playwright.config.ts
```

## 开发命令

```bash
cd webchat

# 安装依赖
pnpm install

# 开发服务器（热更新）
pnpm dev

# 生产构建
pnpm build

# Lint 检查
pnpm lint

# E2E 测试
pnpm test:e2e
```

## 与 Gateway 的集成

### 通信协议

WebChat 使用 AEP v1 协议通过 WebSocket 与 Gateway 通信：

1. 建立 WebSocket 连接（`ws://localhost:8888/ws`）
2. 发送 `init` 握手（包含 `worker_type`、`client_caps`）
3. 接收 `init_ack` 确认
4. 通过 `input` / `message.delta` / `done` 进行双向通信

### go:embed 嵌入

Gateway 使用 Go 的 `go:embed` 机制将 WebChat 静态文件嵌入二进制：

```go
//go:embed all:out
var StaticFS embed.FS
```

构建流程：`pnpm build` → `webchat/out/` → `go:embed` → Gateway 二进制

### API Key 认证

WebChat 通过以下方式之一进行认证：
- HTTP Header `X-API-Key`
- Query Parameter `api_key`（浏览器 WebSocket 的 CORS 兼容方案）

开发模式下（未配置 API Key）自动使用 `anonymous` 用户身份。

---

## 延伸阅读

- [配置参考 — webchat 配置段](../../reference/configuration.md) — WebChat 相关的完整配置参数
