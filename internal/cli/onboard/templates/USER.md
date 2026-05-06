---
version: 2
description: "User profile and preferences"
---

# USER.md - 用户画像

## 技术背景

<!-- 填写你的信息，帮助 agent 适配你的专业水平。示例值供参考，替换为你的实际情况 -->

- **主要语言**：Go
- **框架**：Gin, gRPC
- **基础设施**：Docker, Kubernetes

## 工作偏好

- 提交风格：原子提交 + Conventional Commits
- 反馈风格：代码审查格式（指出问题 + 给出建议）
- 不要过度解释基础概念

## 沟通偏好

- 保持简洁——不要总结已完成的工作
- 代码用 file:line 格式引用
- 解释技术决策的 WHY
- 不确定时直接说"需要调查"

## 配置层级

此文件支持 3 级 fallback，高优先级完整替换低优先级：
- 全局级：~/.hotplex/agent-configs/USER.md（本文件）
- 平台级：~/.hotplex/agent-configs/slack/USER.md
- Bot 级：~/.hotplex/agent-configs/slack/U12345/USER.md

使用 `hotplex-setup` skill 进行交互式个性化配置。
