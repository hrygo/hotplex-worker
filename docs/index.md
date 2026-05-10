---
title: HotPlex 文档中心
description: HotPlex 用户文档、教程、指南和参考手册
---

# HotPlex

HotPlex 是一个 AI Coding Agent 统一管理平台。通过飞书、Slack 或 WebChat 远程遥控你的 AI Agent，支持定时任务、多项目管理、企业级安全。

## 快速入口

| 你是谁                                  | 从哪里开始                                                        |
| --------------------------------------- | ----------------------------------------------------------------- |
| 我想快速体验                            | [5 分钟快速开始](getting-started.md)                              |
| 我是普通用户，想用飞书/Slack 和 AI 聊天 | [与 AI 对话](guides/user/chat-with-ai.md)                         |
| 我是开发者，想远程控制 Coding Agent     | [远程 Coding Agent 指南](guides/developer/remote-coding-agent.md) |
| 我是企业管理员，需要部署到生产环境      | [企业部署指南](guides/enterprise/deployment.md)                   |
| 我想为 HotPlex 贡献代码                 | [开发环境搭建](guides/contributor/development-setup.md)           |
| 我在评估 HotPlex 是否适合我             | [为什么选择 HotPlex](explanation/why-hotplex.md)                  |

## 教程

手把手引导，从零开始完成一个具体目标。

| 教程                                          | 目标读者    | 时间   |
| --------------------------------------------- | ----------- | ------ |
| [5 分钟快速开始](getting-started.md)          | 全部        | 5 min  |
| [Slack 集成](tutorials/slack-integration.md)  | 开发者      | 15 min |
| [飞书集成](tutorials/feishu-integration.md)   | 开发者      | 15 min |
| [AI 人格定制](tutorials/agent-personality.md) | 开发者      | 10 min |
| [定时任务](tutorials/cron-scheduled-tasks.md) | 开发者/用户 | 10 min |

## 指南

目标导向，解决特定场景的问题。

### 普通用户

| 指南                                             | 说明                                |
| ------------------------------------------------ | ----------------------------------- |
| [与 AI 对话](guides/user/chat-with-ai.md)        | 飞书/Slack 中使用 AI 助理的完整指南 |
| [命令速查表](guides/user/commands-cheatsheet.md) | 所有聊天命令一览，可打印            |
| [移动端访问](guides/user/mobile-access.md)       | 通过手机使用 HotPlex AI 助理        |
| [使用技巧](guides/user/tips-and-tricks.md)       | 高效使用 AI 助理的进阶技巧          |

### 开发者

| 指南                                                         | 说明                                  |
| ------------------------------------------------------------ | ------------------------------------- |
| [远程 Coding Agent](guides/developer/remote-coding-agent.md) | 远程控制 AI Agent 编程的最佳实践      |
| [Session 管理](guides/developer/session-management.md)       | 5 状态机、/gc vs /reset、Resume 机制  |
| [Context Window 管理](guides/developer/context-window.md)    | /compact、/clear、B/C 通道 token 消耗 |
| [WebChat 设置](guides/developer/webchat-setup.md)            | 嵌入式 Web UI 配置和使用              |
| [多 Agent 协作](guides/developer/multiple-agents.md)         | 多 Worker 类型、多项目管理            |
| [Cron 自动化](guides/developer/cron-automation.md)           | 三种调度模式、常见场景、Silent 模式   |
| [安全模型](guides/developer/security-model.md)               | 7 层安全体系、权限控制、最佳实践      |
| [语音功能](guides/developer/voice-features.md)               | STT 语音转文字 + TTS 语音回复配置     |

### 企业

| 指南                                                  | 说明                                  |
| ----------------------------------------------------- | ------------------------------------- |
| [企业部署](guides/enterprise/deployment.md)           | 生产环境部署、安全加固、资源管理      |
| [安全加固](guides/enterprise/security-hardening.md)   | 7 层安全体系详解                      |
| [可观测性](guides/enterprise/observability.md)        | 日志、Prometheus、OpenTelemetry、告警 |
| [多租户隔离](guides/enterprise/multi-tenant.md)       | Bot 级隔离、JWT 路由、会话配额        |
| [合规与审计](guides/enterprise/compliance.md)         | 配置审计、凭据管理、回滚能力          |
| [灾备恢复](guides/enterprise/disaster-recovery.md)    | RTO/RPO、自动重启、备份策略           |
| [配置管理](guides/enterprise/config-management.md)    | 5 层优先级、热重载、多环境策略        |
| [集成模式](guides/enterprise/integration-patterns.md) | 反向代理、CI/CD、SDK 集成             |
| [资源限制](guides/enterprise/resource-limits.md)      | 全局/用户/Worker 限制、调优建议       |

### 贡献者

| 指南                                                             | 说明                                        |
| ---------------------------------------------------------------- | ------------------------------------------- |
| [开发环境搭建](guides/contributor/development-setup.md)          | Go 1.26+、make quickstart、IDE 配置         |
| [架构概览](guides/contributor/architecture.md)                   | 单进程架构、6 大核心包、数据流路径          |
| [测试指南](guides/contributor/testing-guide.md)                  | testify、表驱动测试、覆盖率目标             |
| [PR 工作流](guides/contributor/pr-workflow.md)                   | 分支命名、Conventional Commits、CI 要求     |
| [扩展 HotPlex](guides/contributor/extending.md)                  | Worker 适配器、消息平台、AEP 事件、CLI 命令 |
| [添加 Worker 适配器](guides/contributor/adding-worker.md)        | BaseWorker 嵌入、接口实现、注册流程         |
| [添加消息适配器](guides/contributor/adding-messaging-adapter.md) | PlatformAdapter 嵌入、5 步初始化、Hub 注册  |

## 参考

权威、完整的技术细节。

| 参考                                               | 说明                                |
| -------------------------------------------------- | ----------------------------------- |
| [CLI 命令参考](reference/cli.md)                   | 全部 38 个 CLI 子命令和参数         |
| [配置参考](reference/configuration.md)             | 全部 14 个配置段的字段级文档        |
| [Admin API 参考](reference/admin-api.md)           | 管理端点、Scope 权限、请求/响应格式 |
| [AEP 协议参考](reference/aep-protocol.md)          | Agent Exchange Protocol v1 完整规范 |
| [事件参考](reference/events.md)                    | 全部 AEP 事件类型和数据结构         |
| [安全策略参考](reference/security-policies.md)     | JWT、SSRF、命令白名单、工具控制     |
| [Metrics 参考](reference/metrics.md)               | Prometheus 指标、scrape 配置        |
| [术语表](reference/glossary.md)                    | HotPlex 核心术语解释                |
| [Go SDK 参考](reference/sdk-go.md)                 | Go 客户端 SDK API 文档              |
| [TypeScript SDK 参考](reference/sdk-typescript.md) | TypeScript SDK API 文档             |
| [Python SDK 参考](reference/sdk-python.md)         | Python SDK API 文档                 |

## 概念解释

理解设计背后的原因。

| 文档                                                     | 说明                                |
| -------------------------------------------------------- | ----------------------------------- |
| [为什么选择 HotPlex](explanation/why-hotplex.md)         | 痛点、解法、架构哲学、适用场景      |
| [Session 生命周期](explanation/session-lifecycle.md)     | 5 状态机、UUIDv5、GC 策略、背压机制 |
| [Agent 配置系统](explanation/agent-config-system.md)     | B/C 双通道、命中即终止、热更新      |
| [Brain LLM 编排](explanation/brain-llm-orchestration.md) | 意图路由、安全审计、上下文压缩      |
| [Cron 调度器设计](explanation/cron-design.md)            | AI-native 调度、并发槽、投递机制    |
| [安全模型](explanation/security-model.md)                | 7 层安全体系设计决策和权衡          |
