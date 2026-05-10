# HotPlex 用户文档重设计

**日期**: 2026-05-10
**状态**: Approved
**目标**: 为 HotPlex 推广期建立业界标杆级用户文档体系

---

## 1. 背景

HotPlex v1.10+ 功能已成熟（Gateway、Cron、Brain、多平台适配），准备大力推广。目标用户四类：

| 人物 | 核心诉求 | 典型入口 |
|------|---------|---------|
| 个人开发者 | 远程遥控 Coding Agent，SDK 集成 | CLI / WebChat / SDK |
| 普通用户 | 通过飞书/Slack 使用 AI 助理 | 飞书 / Slack Bot |
| 企业用户 | DevOps 集成，安全合规，可观测性 | Admin API / Docker / systemd |
| 社区贡献者 | 理解架构，贡献代码 | GitHub |

### 现有文档问题

- Product-Whitepaper.md 像技术规格书，非用户导向
- User-Manual.md 覆盖面薄，缺 Cron/Brain/企业特性
- Reference-Manual.md 英文技术参考，偏开发者
- Brain/LLM 编排层、Cron 调度器、Admin API、安全策略、可观测性 **无用户文档**

---

## 2. 设计原则

### 2.1 Diataxis 框架

四象限分离，每篇文档只服务一个目的：

| 象限 | 读者问题 | 文档类型 |
|------|---------|---------|
| 教程 | "我怎么学？" | step-by-step，可复现 |
| 指南 | "我怎么实现 X？" | 目标导向，问题解决 |
| 参考 | "具体的参数/格式是什么？" | 权威、完整、可检索 |
| 解释 | "为什么这样设计？" | 概念理解、架构权衡 |

### 2.2 多人物路径

用户按角色自选路径，每条路径内部遵循相同的节奏：
Quick Start → Guides → Reference → Explanation

### 2.3 渐进式披露

每篇文档 3 层结构：
1. 概述（1-2 句 + 示意图）
2. 核心用法（步骤 + 代码示例）
3. 高级选项（折叠区域 / "深入阅读" 链接）

### 2.4 BLUF 原则

Bottom Line Up Front — 先给结论和关键信息，再展开细节。

---

## 3. 文档架构

```
docs/
  index.md                          # 首页
  getting-started.md                # 5 分钟快速开始

  tutorials/                        # 教程（学习导向）
    first-session.md
    slack-integration.md
    feishu-integration.md
    cron-scheduled-tasks.md
    agent-personality.md
    custom-worker.md

  guides/                           # 指南（目标导向）
    developer/                      # 个人开发者
      remote-coding-agent.md
      session-management.md
      context-window.md
      voice-features.md
      cron-automation.md
      sdk-integration.md
      webchat-setup.md
      multiple-agents.md
      security-model.md
    user/                           # 普通用户
      chat-with-ai.md
      commands-cheatsheet.md
      voice-input.md
      mobile-access.md
      tips-and-tricks.md
    enterprise/                     # 企业用户
      deployment.md
      security-hardening.md
      admin-api.md
      observability.md
      multi-tenant.md
      compliance.md
      disaster-recovery.md
      config-management.md
      integration-patterns.md
      resource-limits.md
    contributor/                    # 贡献者
      development-setup.md
      architecture.md
      code-conventions.md
      testing-guide.md
      pr-workflow.md
      adding-worker.md
      adding-messaging-adapter.md

  reference/                        # 参考（信息导向）
    cli.md
    configuration.md
    admin-api.md
    aep-protocol.md
    events.md
    security-policies.md
    metrics.md
    sdk-go.md
    sdk-typescript.md
    sdk-python.md
    glossary.md

  explanation/                      # 解释（理解导向）
    why-hotplex.md
    architecture-deep-dive.md
    session-lifecycle.md
    agent-config-system.md
    brain-llm-orchestration.md
    security-model.md
    cron-design.md
```

---

## 4. 优先级

### P0 — 推广阻断项
1. getting-started.md — 5 分钟快速开始
2. tutorials/slack-integration.md
3. tutorials/feishu-integration.md
4. guides/user/chat-with-ai.md
5. guides/user/commands-cheatsheet.md
6. reference/cli.md
7. reference/configuration.md

### P1 — 核心价值展示
8. guides/developer/remote-coding-agent.md
9. tutorials/agent-personality.md
10. tutorials/cron-scheduled-tasks.md
11. guides/enterprise/deployment.md
12. reference/glossary.md
13. explanation/why-hotplex.md

### P2 — 企业级能力
14-23. 企业路径全部 + 安全 + 可观测性

### P3 — 生态建设
24-45. SDK 集成 + 贡献者路径 + 解释类 + AEP 协议

---

## 5. 写作规范

### 语言
- 中文为主，技术术语英文
- 代码示例带注释
- 标题用祈使句（"创建定时任务" 而非 "定时任务的创建"）

### 格式
- Markdown，兼容文档站生成器
- frontmatter: title, description, persona, difficulty
- 代码块标注语言类型
- 表格优先于列表（信息密度高时）

### 每篇结构模板

```markdown
---
title: 标题
description: 一句话描述
persona: developer | user | enterprise | contributor
difficulty: beginner | intermediate | advanced
---

# 标题

> 一句话说明读完这篇能获得什么。

## 概述

背景 + 这篇文档解决什么问题。（2-3 句）

## 前提条件

- 列出开始前需要具备的条件

## 步骤 / 内容

### 1. 第一步

具体操作 + 代码示例。

### 2. 第二步

...

## 验证

如何确认操作成功。

## 下一步

- 相关文档链接

## 参考

- 深入阅读链接
```

---

## 6. 与现有文档的关系

| 现有文档 | 处理 |
|---------|------|
| docs/Product-Whitepaper.md | 保留，重命名为 explanation/architecture-deep-dive.md |
| docs/User-Manual.md | 内容拆分到 tutorials/ + guides/user/ |
| docs/Reference-Manual.md | 内容拆分到 reference/ |
| docs/architecture/ | 保留，用户文档引用 |
| docs/security/ | 保留，guides/enterprise/ 引用 |
| CLAUDE.md | 保留，贡献者文档引用 |

---

## 7. 实施计划

Phase 1 (P0): 7 篇核心文档
Phase 2 (P1): 6 篇价值展示文档
Phase 3 (P2): 10 篇企业级文档
Phase 4 (P3): 22 篇生态文档

总计约 45 篇，逐步交付。
