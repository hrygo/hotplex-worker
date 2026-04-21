# Specs 目录索引

> 规范文档集中管理 — 设计规格、验收标准、跟踪矩阵

## 📋 文档索引

### 架构设计规格

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Worker-Gateway-Framework-Design.md](./Worker-Gateway-Framework-Design.md) | HotPlex Worker Gateway 应用框架设计 — 完整基础设施层 | ✅ Implemented | 2026-03-30 | 100% |
| [Gateway-Async-Init-Spec.md](./Gateway-Async-Init-Spec.md) | Gateway 异步初始化 — Session Start 异步化设计 | 📝 Draft | 2026-04-04 | 0% |
| [Worker-ClaudeCode-Spec.md](./Worker-ClaudeCode-Spec.md) | Claude Code Worker 集成规格 | ✅ Implemented | 2026-04-01 | 100% |
| [Worker-OpenCode-Server-Spec.md](./Worker-OpenCode-Server-Spec.md) | OpenCode Server Worker 集成规格 — WebSocket 传输、Session 管理、Resume 支持 | 🔨 Needs Implementation | 2026-04-04 | 0% |
| [Worker-Common-Protocol.md](./Worker-Common-Protocol.md) | Worker 公共协议规范（NDJSON、背压、终止等） | ✅ Implemented | 2026-04-04 | 100% |
| [Persistent-Session-Mechanism.md](./Persistent-Session-Mechanism.md) | 持久会话机制 — UUIDv5 映射、reset/gc、session 状态机 | ✅ Implemented | 2026-04-07 | 100% |
| [Worker-ACPX-Spec.md](./Worker-ACPX-Spec.md) | ACPX Worker 集成规格 — 支持 16+ AI 编程 Agent | 📝 Draft | 2026-04-04 | 0% |
| [Feishu-Adapter-Improvement-Spec.md](./Feishu-Adapter-Improvement-Spec.md) | Feishu Adapter 改进规格 — 流式卡片、访问控制、多消息类型 | 📝 Draft | 2026-04-17 | 0% |
| [Worker-Session-Control-Spec.md](./Worker-Session-Control-Spec.md) | Worker stdio 直达控制 — 10 项已验证命令（compact/clear/context 等） | ✅ Verified | 2026-04-19 | 0% |
| [Worker-User-Interaction-Spec.md](./Worker-User-Interaction-Spec.md) | Worker 用户交互集成 — 权限请求/问题询问/MCP Elicitation 转发与响应 | 📝 Draft | 2026-04-19 | 0% |
| [Session-Stats-Spec.md](./Session-Stats-Spec.md) | Session Stats 展示 — done 事件 token/费用/context 统计 footer | 📝 Draft | 2026-04-19 | 0% |
| [Slack-Adapter-Improvement-Spec.md](./Slack-Adapter-Improvement-Spec.md) | Slack Adapter 改进规格 — 流式消息、状态指示器、多消息类型 | 📝 Draft | 2026-04-18 | 0% |

### 客户端 SDK 设计

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Python-Client-Design.md](./Python-Client-Design.md) | Python 客户端示例模块设计 — 第三方开发者集成指南 | ✅ Implemented | 2026-04-02 | 100% |
| [Go-Client-Example-Design.md](./Go-Client-Example-Design.md) | Go 客户端示例模块设计 — WebSocket + AEP v1 演示 | ✅ Implemented | 2026-04-03 | 100% |

### 前端集成设计

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [AI-SDK-Chatbot-Integration-Design.md](./AI-SDK-Chatbot-Integration-Design.md) | AI SDK Chatbot 集成设计 — Transport 适配器方案 | ✅ Implemented | 2026-04-03 | 100% |

### 验收标准与跟踪

| 文档 | 描述 | 版本 | 状态 |
|------|------|------|------|
| [Acceptance-Criteria.md](./Acceptance-Criteria.md) | 157 条验收标准完整定义 | v1.0 | 草稿 |
| [AC-Tracking-Matrix.md](./AC-Tracking-Matrix.md) | 验收标准跟踪矩阵（Markdown） | v1.0 | 草稿 |
| [AI-SDK-Chatbot-AC.md](./AI-SDK-Chatbot-AC.md) | AI SDK Chatbot 集成验收标准 | - | - |
| [AC-Tracking-Matrix.csv](./AC-Tracking-Matrix.csv) | 验收状态跟踪矩阵（CSV，机器可读） | v1.0 | 草稿 |
| [TRACEABILITY-MATRIX.md](./TRACEABILITY-MATRIX.md) | HotPlex Worker 功能实现与代码溯源矩阵 | v1.0 | 活动 |

### Review 文档

| 文档 | 描述 | 日期 |
|------|------|------|
| [Review-Gateway-Async-Init.md](./Review-Gateway-Async-Init.md) | Gateway Async Init Spec 审查报告 | - |

---

## 📊 状态统计

### 按状态分类

- ✅ **Implemented**: 7 个（已完成实现）
  - Worker-Gateway-Framework-Design
  - Worker-ClaudeCode-Spec
  - Worker-Common-Protocol
  - Persistent-Session-Mechanism
  - Python-Client-Design
  - Go-Client-Example-Design
  - AI-SDK-Chatbot-Integration-Design
- 🔨 **Needs Implementation**: 1 个（规格已完成，待实现）
  - Worker-OpenCode-Server-Spec
- ✅ **Verified**: 1 个（已验证，待实现）
  - Worker-Session-Control-Spec
- 📝 **Draft**: 6 个（设计中）
  - Gateway-Async-Init-Spec
  - Worker-ACPX-Spec
  - AI-SDK-Chatbot-AC
  - Feishu-Adapter-Improvement-Spec
  - Worker-User-Interaction-Spec
  - Session-Stats-Spec
  - Slack-Adapter-Improvement-Spec
- 🔵 **Active**: 2 个（活跃维护）
  - AC-Tracking-Matrix
  - TRACEABILITY-MATRIX
- ✅ **Completed**: 1 个（已完成审查）
  - Review-Gateway-Async-Init
- 📋 **Draft**: 1 个（已完成定义）
  - Acceptance-Criteria

### 按类型分类

- **架构设计**: 11 个
- **平台消息**: 2 个
- **客户端 SDK**: 2 个
- **前端集成**: 1 个
- **验收标准**: 4 个
- **Review**: 1 个

---

## 🔍 快速查询

### 查看 TODO AC

```bash
# 查看所有 P0 AC
grep ",P0,TODO," docs/specs/AC-Tracking-Matrix.csv

# 查看某区域的 P0
grep "AEP v1 协议,P0,TODO" docs/specs/AC-Tracking-Matrix.csv

# 统计进度
grep -c ",P0,PASS," docs/specs/AC-Tracking-Matrix.csv
grep -c ",P0,TODO," docs/specs/AC-Tracking-Matrix.csv
```

### 状态分布

```bash
# TODO / IN_PROGRESS / PASS / FAIL 数量
awk -F',' '{print $5}' docs/specs/AC-Tracking-Matrix.csv | sort | uniq -c
```

---

## 📚 关联规范文档

### 架构设计

- **协议规范**: [`../architecture/AEP-v1-Protocol.md`](../architecture/AEP-v1-Protocol.md), [`../architecture/AEP-v1-Appendix.md`](../architecture/AEP-v1-Appendix.md)
- **架构设计**: [`../architecture/Worker-Gateway-Design.md`](../architecture/Worker-Gateway-Design.md), [`../architecture/Message-Persistence.md`](../architecture/Message-Persistence.md)

### 安全设计

- [`../security/Security-Authentication.md`](../security/Security-Authentication.md)
- [`../security/SSRF-Protection.md`](../security/SSRF-Protection.md)
- [`../security/Env-Whitelist-Strategy.md`](../security/Env-Whitelist-Strategy.md)
- [`../security/AI-Tool-Policy.md`](../security/AI-Tool-Policy.md)
- [`../security/Security-InputValidation.md`](../security/Security-InputValidation.md)

### 管理设计

- [`../management/Admin-API-Design.md`](../management/Admin-API-Design.md)
- [`../management/Config-Management.md`](../management/Config-Management.md)
- [`../management/Observability-Design.md`](../management/Observability-Design.md)
- [`../management/Resource-Management.md`](../management/Resource-Management.md)

### 测试策略

- [`../testing/Testing-Strategy.md`](../testing/Testing-Strategy.md)

---

## 🔗 Spec 关联关系

### 2026-04-19 新增 Specs 依赖图

```
Worker-Session-Control-Spec (✅ Verified)
  └── Worker stdin 直达命令（context 查询等）
  └── 与下方两个 spec 正交，互不影响

Worker-User-Interaction-Spec (📝 Draft, 🔴 High Priority)
  └── 权限请求 / 问题询问 / MCP Elicitation 转发与响应
  └── 阻塞式：Agent 暂停执行等待用户输入
  └── 依赖: Worker-ClaudeCode-Spec, Worker-OpenCode-Server-Spec

Session-Stats-Spec (📝 Draft, 🟡 Medium Priority)
  └── done 事件 token/费用/context 统计 footer
  └── 只读展示：无阻塞，用户体验增强
  └── 与 User-Interaction 正交（改不同文件/路径）
  └── 建议在 User-Interaction 之后实施
```

**实施顺序建议**: Session-Control → User-Interaction → Session-Stats

---

## 📝 Metadata 标准

所有 specs 文档应包含以下 YAML frontmatter：

```yaml
---
type: spec
tags:
  - project/HotPlex
  - category/subcategory  # 如：worker/claude-code, client/python
date: YYYY-MM-DD
status: draft | approved | implemented | deprecated
progress: 0-100
priority: low | medium | high | critical  # 可选
estimated_hours: number  # 可选
completion_date: YYYY-MM-DD  # 可选，完成时填写
---
```

### Status 定义

- 📝 **draft**: 草稿，正在设计中
- ✅ **approved**: 已批准，待实现
- 🔨 **needs-implementation**: 规格已完成，待实现
- 🚀 **implemented**: 已完成实现
- ⚠️ **deprecated**: 已废弃

---

## 🔄 更新日志

| 日期 | 版本 | 更新内容 |
|------|------|----------|
| 2026-04-19 | v1.6 | 新增 Worker-User-Interaction-Spec、Session-Stats-Spec、Worker-Session-Control-Spec、Slack-Adapter-Improvement-Spec；新增 Spec 关联关系章节 |
| 2026-04-18 | v1.5 | 新增 Slack-Adapter-Improvement-Spec |
| 2026-04-07 | v1.4 | 新增 Persistent-Session-Mechanism.md（持久会话机制，100% 已实现）；更新规格索引统计 |
| 2026-04-04 | v1.3 | 修正 Worker-OpenCode-Server-Spec 状态为 needs-implementation；更新统计 |
| 2026-04-04 | v1.1 | 新增客户端 SDK、前端集成设计规格；添加 metadata 索引 |
| 2026-03-31 | v1.0 | 初始版本：157 条 AC，3 个文件（MD 定义 + MD 跟踪 + CSV 跟踪） |
