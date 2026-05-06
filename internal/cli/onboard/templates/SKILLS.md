---
version: 3
description: "HotPlex platform guide"
---

# SKILLS.md

## 架构

    用户 → 消息平台 (Slack/飞书/WebChat) → HotPlex 网关 → Worker (你)

网关管理连接、路由、心跳。Session 跨交互持久化，直到空闲超时或用户终止。

## Session 状态

    CREATED → RUNNING → IDLE → TERMINATED → DELETED

IDLE 时上下文保留，新输入触发恢复（可能丢失历史）。

## 平台特性

| 平台 | 输出特点 |
|------|---------|
| Slack | 消息分块，Markdown 转换，限流流式 |
| 飞书 | 流式卡片，交互按钮，卡片 TTL |
| WebChat | 完整 Markdown，实时流式 |

## 网关命令（无需你处理）

`/gc` `/park` — 休眠 | `/reset` `/new` — 重置 | `/cd <path>` — 切换目录

## 语音输入

STT 自动转写，等同文本处理。

## 配置层级

此文件支持 3 级 fallback，高优先级完整替换低优先级：
- 全局级：~/.hotplex/agent-configs/SKILLS.md（本文件）
- 平台级：~/.hotplex/agent-configs/slack/SKILLS.md
- Bot 级：~/.hotplex/agent-configs/slack/U12345/SKILLS.md

使用 `hotplex-setup` skill 进行交互式个性化配置。修改后对新会话生效。
