---
title: HotPlex 文档中心
description: HotPlex 用户文档、教程、指南和参考手册
---

# HotPlex 文档

HotPlex 是一个 AI Coding Agent 统一管理平台。通过飞书、Slack 或 WebChat 远程遥控你的 AI Agent，支持定时任务、多项目管理、企业级安全。

## 快速入口

| 你是谁 | 从哪里开始 |
|--------|-----------|
| 我想快速体验 | [5 分钟快速开始](getting-started.md) |
| 我是普通用户，想用飞书/Slack 和 AI 聊天 | [与 AI 对话](guides/user/chat-with-ai.md) |
| 我是开发者，想远程控制 Coding Agent | [远程 Coding Agent 指南](guides/developer/remote-coding-agent.md) |
| 我是企业管理员，需要部署到生产环境 | [企业部署指南](guides/enterprise/deployment.md) |
| 我在评估 HotPlex 是否适合我 | [为什么选择 HotPlex](explanation/why-hotplex.md) |

## 教程

手把手引导，从零开始完成一个具体目标。

| 教程 | 目标读者 | 时间 |
|------|---------|------|
| [5 分钟快速开始](getting-started.md) | 全部 | 5 min |
| [Slack 集成](tutorials/slack-integration.md) | 开发者 | 15 min |
| [飞书集成](tutorials/feishu-integration.md) | 开发者 | 15 min |
| [AI 人格定制](tutorials/agent-personality.md) | 开发者 | 10 min |
| [定时任务](tutorials/cron-scheduled-tasks.md) | 开发者/用户 | 10 min |

## 指南

目标导向，解决特定场景的问题。

### 普通用户

| 指南 | 说明 |
|------|------|
| [与 AI 对话](guides/user/chat-with-ai.md) | 飞书/Slack 中使用 AI 助理的完整指南 |
| [命令速查表](guides/user/commands-cheatsheet.md) | 所有聊天命令一览，可打印 |

### 开发者

| 指南 | 说明 |
|------|------|
| [远程 Coding Agent](guides/developer/remote-coding-agent.md) | 远程控制 AI Agent 编程的最佳实践 |
| [语音功能](guides/developer/voice-features.md) | STT 语音转文字 + TTS 语音回复配置 |

### 企业

| 指南 | 说明 |
|------|------|
| [企业部署](guides/enterprise/deployment.md) | 生产环境部署、安全加固、资源管理 |
| [安全加固](guides/enterprise/security-hardening.md) | 7 层安全体系：网络、认证、SSRF、命令白名单、环境隔离、工具控制、输出限制 |
| [可观测性](guides/enterprise/observability.md) | 日志、Prometheus 指标、OpenTelemetry 追踪、健康检查、告警 |
| [多租户隔离](guides/enterprise/multi-tenant.md) | Bot 级隔离、JWT 路由、会话配额、访问控制 |
| [合规与审计](guides/enterprise/compliance.md) | 配置审计、凭据管理、Token 生命周期、回滚能力 |
| [灾备恢复](guides/enterprise/disaster-recovery.md) | RTO/RPO 目标、自动重启、备份策略、恢复流程 |
| [配置管理](guides/enterprise/config-management.md) | 5 层优先级、热重载、版本历史、多环境策略 |
| [集成模式](guides/enterprise/integration-patterns.md) | 反向代理、CI/CD、监控、自定义 Worker、SDK 集成 |
| [资源限制](guides/enterprise/resource-limits.md) | 全局/用户/Worker/输出限制、Pool 管理、调优建议 |

## 参考

权威、完整的技术细节。

| 参考 | 说明 |
|------|------|
| [CLI 命令参考](reference/cli.md) | 全部 38 个 CLI 子命令和参数 |
| [配置参考](reference/configuration.md) | 全部 14 个配置段的字段级文档 |
| [Admin API 参考](reference/admin-api.md) | 全部管理端点、Scope 权限、请求/响应格式 |
| [术语表](reference/glossary.md) | 39 个 HotPlex 核心术语解释 |

## 概念解释

理解设计背后的原因。

| 文档 | 说明 |
|------|------|
| [为什么选择 HotPlex](explanation/why-hotplex.md) | 痛点、解法、架构哲学、适用场景分析 |
