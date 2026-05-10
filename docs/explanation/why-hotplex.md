---
title: 为什么选择 HotPlex
description: HotPlex 存在的原因 — 它解决了什么问题，为谁而建，以及为什么这样设计
persona: all
difficulty: beginner
---

# 为什么选择 HotPlex

> 这是一篇**解释文档**，回答"Why"而非"How"。如果你在评估 HotPlex 是否适合你的团队，从这里开始。

## 痛点：AI Coding Agent 的现实困境

AI Coding Agent（Claude Code、OpenCode、Cursor 等）正在改变开发者的工作方式。但在实际使用中，团队会撞上几堵墙：

### 被钉在桌前

Agent 运行在本地终端。你发起一个重构任务，去开会 30 分钟回来——发现网络闪断，session 已死，输出全丢。手机上收到一条错误消息，但你无法从 Slack 里直接跟 Agent 说"帮我看看这个报错"。

**Agent 不该是桌面软件的附属品。它应该是随时可达的协作者。**

### 协议碎片化

每个 Agent 都有自己的通信方式：Claude Code 用 stdio NDJSON，OpenCode Server 用 HTTP+SSE，未来还会有更多。为每个 Agent 写一套集成代码，维护成本随 Agent 数量线性增长。

```
Claude Code ── stdio/NDJSON ──┐
OpenCode Server ── HTTP+SSE ──┤── 你的集成代码 × N
Next Agent ── ??? ────────────┘
```

### Session 即抛

Agent 的对话生命周期绑定在终端连接上。连接断开，上下文消失。没有持久化、没有恢复、没有跨设备接续。一次意外的 `Ctrl+C` 就能让 10 分钟的思考链归零。

### 无法自动化

"每天早上 9 点跑一遍测试"、"每小时检查一次 CI 状态"——这些是人类用 cron 做了几十年的事。但 Agent 没有 cron。你无法用自然语言告诉 Agent "每天早上帮我 review PR"。

### 安全裸奔

给 Agent 一个 API key，它就有了完整的系统访问权。没有命令审计、没有权限审批、没有 SSRF 防护。在企业环境中，这意味着合规红线。

### 各自为战

每个开发者跑自己的 Agent 实例，知识不共享、配置不统一、成本无法追踪。团队级 AI 采用停留在"个人玩具"阶段。

---

## 解法：HotPlex 如何逐一应对

### 随时随地的远程访问

通过 Slack、飞书等消息平台直接与 Agent 对话。在地铁上用手机发一条 Slack 消息，Agent 就开始工作。会议中收到报警，在飞书群里直接让 Agent 排查。

| 场景 | 没有 HotPlex | 有 HotPlex |
|------|-------------|-----------|
| 手机上让 Agent 改代码 | 不可能 | Slack/飞书发消息即可 |
| 网络断开后恢复 | 手动重启，上下文丢失 | 自动重连，session 接续 |
| 多人共享一个 Agent | 各自跑各的 | 通过消息群统一交互 |

### 一套协议通吃所有 Agent

HotPlex 定义了 AEP v1（Agent Exchange Protocol），所有 Agent 在 Gateway 层被统一为同一种协议。你的集成代码写一次，换 Agent 只需改一行配置。

```
Claude Code ──┐
OpenCode Server ──┤── HotPlex Gateway (AEP v1) ── 你的代码（只写一次）
Future Agent ────┘
```

### Session 持久化与恢复

基于 UUIDv5 的确定性 session 映射 + SQLite 持久化。连接断了，session 不死；换设备登录，同一个 session ID 自动接续。5 状态机管理完整生命周期，GC 自动清理过期 session。

### AI-native 定时任务

用自然语言告诉 Agent "每个工作日早上 9 点检查测试通过率"，Agent 自主创建 cron job，到点执行，结果自动回传到 Slack/飞书。不是传统 cron 跑 shell 脚本，而是 Agent 理解意图、自主决策、执行并汇报。

### 企业级安全控制

- **JWT 认证** — ES256 签名，session ownership 强校验
- **SSRF 防护** — URL 白名单 + IP 阻断 + DNS 重绑定防御
- **命令审批** — Agent 执行敏感操作前需要用户确认（Permission Hook）
- **输入审计** — Safety Guard 检测威胁指令，防止 prompt injection
- **环境变量白名单** — 敏感变量不会泄露给 Agent

### 团队级 AI 管理

统一的 Agent 配置管理、按 Bot 粒度定制人格和权限、资源配额控制、Prometheus 指标追踪成本。AI 从个人工具升级为团队基础设施。

---

## 架构哲学：为什么这样设计

### 单二进制，零依赖

```bash
cp hotplex /usr/local/bin/
hotplex gateway start
```

30 秒部署完毕。不需要 Docker、不需要 Kubernetes、不需要外部数据库。下载一个二进制，运行，结束。这刻意拒绝了微服务拆分的诱惑——对于一个 Gateway 来说，单进程意味着零运维负担和可预测的延迟。

### SQLite，不是 PostgreSQL

HotPlex 用 SQLite + WAL 模式做持久化。不是因为我们不会用 Postgres，而是因为 Gateway 的数据模型（session 元数据、cron job、事件日志）天然适合嵌入式数据库。零配置、零运维、单文件备份。对于 99% 的部署场景，SQLite 的并发能力完全足够。

### Go，不是 Rust 也不是 Python

- **内存安全** — 没有 GC 的语言在 Gateway 场景下收益有限，Go 的 GC 已经足够好
- **并发模型** — goroutine 天然适配 WebSocket 全双工通信的编程模型
- **跨平台** — 一个代码库覆盖 Linux/macOS/Windows，CI 三平台跑通
- **部署体积** — 静态链接单二进制，无运行时依赖

### Worker 黑盒抽象

Agent 是 Worker，Worker 是黑盒。Gateway 不关心 Worker 内部实现，只通过标准化的接口（启动、停止、读写 stdio）交互。添加新的 Agent 适配器只需实现 `Worker` 接口，不需要修改核心逻辑。

---

## 什么时候用 HotPlex（什么时候不用）

### 适合使用

| 场景 | 原因 |
|------|------|
| 需要远程调用 Agent | 通过 Slack/飞书随时随地交互 |
| 团队共享 AI 能力 | 统一入口、统一配置、统一审计 |
| 自动化 AI 工作流 | AI-native cron 替代手写脚本 |
| 企业合规要求 | JWT/SSRF/命令审批/输入审计全套安全体系 |
| 多 Agent 混合使用 | AEP v1 协议统一，切换零成本 |
| 长时间运行的任务 | Session 持久化，断线不丢上下文 |

### 可以不用

| 场景 | 原因 |
|------|------|
| 只在本地终端用 Claude Code | 直接用就好，不需要 Gateway |
| 不使用 Slack/飞书等消息平台 | 缺少主要交互通道，收益有限 |
| 已有自建 Agent 编排平台 | 除非想替换，否则不要叠加 |
| 纯个人开发者、无需远程访问 | 本地 CLI 体验已经足够好 |

---

## 一句话总结

HotPlex 的存在是为了回答一个问题：**怎么让 AI Coding Agent 从个人桌面工具变成团队级、随时可达、安全可控的基础设施？**

答案：一个单二进制的 Gateway，统一协议、持久化 session、接入消息平台、内建安全控制、支持自动化调度。30 秒部署，零运维负担。
