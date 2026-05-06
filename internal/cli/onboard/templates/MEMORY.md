---
version: 1
description: "Cross-session context memory"
---

# MEMORY.md - Context Memory

<!-- This file is auto-managed by the agent across sessions. -->
<!-- You can also manually add persistent context here. -->

## 配置层级

此文件支持 3 级 fallback，高优先级完整替换低优先级：
- 全局级：~/.hotplex/agent-configs/MEMORY.md（本文件）
- 平台级：~/.hotplex/agent-configs/slack/MEMORY.md
- Bot 级：~/.hotplex/agent-configs/slack/U12345/MEMORY.md

使用 `hotplex-setup` skill 进行交互式个性化配置。

