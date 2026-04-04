# Specs 目录索引

> 规范文档集中管理 — 设计规格、验收标准、跟踪矩阵

## 📋 文档索引

### 架构设计规格

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Worker-Gateway-Framework-Design.md](./Worker-Gateway-Framework-Design.md) | HotPlex Worker Gateway 应用框架设计 — 完整基础设施层 | ✅ Implemented | 2026-03-30 | 100% |
| [Gateway-Async-Init-Spec.md](./Gateway-Async-Init-Spec.md) | Gateway 异步初始化 — Session Start 异步化设计 | 📝 Draft | 2026-04-04 | - |
| [Worker-ClaudeCode-Spec.md](./Worker-ClaudeCode-Spec.md) | Claude Code Worker 集成规格 | - | - | - |
| [Worker-ACPX-Spec.md](./Worker-ACPX-Spec.md) | ACPX Worker 集成规格 — 支持 16+ AI 编程 Agent | - | - | - |

### 客户端 SDK 设计

| 文档 | 描述 | 状态 | 日期 | 预计工时 |
|------|------|------|------|----------|
| [Python-Client-Design.md](./Python-Client-Design.md) | Python 客户端示例模块设计 — 第三方开发者集成指南 | ✅ Approved | 2026-04-02 | 16h |
| [Go-Client-Example-Design.md](./Go-Client-Example-Design.md) | Go 客户端示例模块设计 — WebSocket + AEP v1 演示 | ✅ Approved | 2026-04-03 | 12h |

### 前端集成设计

| 文档 | 描述 | 状态 | 日期 | 优先级 | 预计工时 |
|------|------|------|------|--------|----------|
| [AI-SDK-Chatbot-Integration-Design.md](./AI-SDK-Chatbot-Integration-Design.md) | AI SDK Chatbot 集成设计 — Transport 适配器方案 | 📝 Draft | 2026-04-03 | 🔴 High | 16h |

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

- ✅ **Implemented**: 1 个（已完成实现）
- ✅ **Approved**: 2 个（已批准，待实现）
- 📝 **Draft**: 2 个（草稿，设计中）
- ⚪ **未标记**: 8 个（需要添加 metadata）

### 按类型分类

- **架构设计**: 4 个
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
- 🚀 **implemented**: 已完成实现
- ⚠️ **deprecated**: 已废弃

---

## 🔄 更新日志

| 日期 | 版本 | 更新内容 |
|------|------|----------|
| 2026-04-04 | v1.1 | 新增客户端 SDK、前端集成设计规格；添加 metadata 索引 |
| 2026-03-31 | v1.0 | 初始版本：157 条 AC，3 个文件（MD 定义 + MD 跟踪 + CSV 跟踪） |
