# Specs 目录索引

> 规范文档集中管理 — 设计规格、验收标准、跟踪矩阵

## 文档索引

### 架构设计规格

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Gateway-Async-Init-Spec.md](./Gateway-Async-Init-Spec.md) | Gateway 异步初始化 — Session Start 异步化设计 | draft | 2026-04-04 | 0% |
| [Worker-ACPX-Spec.md](./Worker-ACPX-Spec.md) | ACPX Worker 集成规格 — 支持 16+ AI 编程 Agent | draft | 2026-04-04 | 0% |
| [Worker-User-Interaction-Spec.md](./Worker-User-Interaction-Spec.md) | Worker 用户交互集成 — 权限请求/问题询问/MCP Elicitation 转发与响应 | implemented | 2026-04-19 | 95% |
| [Feishu-Adapter-Improvement-Spec.md](./Feishu-Adapter-Improvement-Spec.md) | Feishu Adapter 改进规格 — 流式卡片、访问控制、多消息类型 | in-progress | 2026-04-17 | 50% |
| [Consolidate-Events-Store-Spec.md](./Consolidate-Events-Store-Spec.md) | 事件存储合并 — 统一 EventStore 架构 | proposed | - | - |
| [Delta-Optimization-Spec.md](./Delta-Optimization-Spec.md) | Delta 优化 — 增量消息压缩与合并策略 | proposed | - | - |
| [Interaction-Response-Chain-Fix-Spec.md](./Interaction-Response-Chain-Fix-Spec.md) | 交互响应链修复 — 权限/Q&A 响应路由重构 | proposed | - | - |
| [Inbound-Event-Storage-Fix-Spec.md](./Inbound-Event-Storage-Fix-Spec.md) | 入站事件存储修复 — 事件持久化一致性 | proposed | 2026-05-07 | - |
| [Hot-Reload-Spec.md](./Hot-Reload-Spec.md) | 配置热重载修复 — 配置变更即时生效 | draft | 2026-04-22 | 20% |

### Worker 与 Session

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Worker-GAP-Analysis-2026-05-15.md](./Worker-GAP-Analysis-2026-05-15.md) | Worker GAP 分析 — OCS vs CC Worker 能力差距与实施计划 | active | 2026-05-15 | 0% |
| [Session-History-Persistence-Spec.md](./Session-History-Persistence-Spec.md) | Session 历史持久化 — 会话重放与历史查询 | draft | 2026-04-28 | 40% |
| [Per-Bot-Agent-Config-Spec.md](./Per-Bot-Agent-Config-Spec.md) | Per-Bot Agent 配置 — 独立 Bot 人格与上下文 | implemented | 2026-05-03 | 100% |
| [Turn-Summary-Spec.md](./Turn-Summary-Spec.md) | Turn 摘要 — 消息轮次总结与统计 | draft | 2026-05-01 | 70% |
| [Turn-Summary-WorkDir-Fix-Spec.md](./Turn-Summary-WorkDir-Fix-Spec.md) | Turn 摘要 WorkDir 修复 — 缺失工作目录字段 | implemented | 2026-05-04 | 100% |

### 平台适配

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Slack-CLI-Subcommand-Spec.md](./Slack-CLI-Subcommand-Spec.md) | Slack CLI 子命令规格 — 独立 CLI 操作 Slack | draft | 2026-05-03 | - |
| [Slack-Stream-Rotation-Spec.md](./Slack-Stream-Rotation-Spec.md) | Slack 流式旋转 — TTL 超时自动续流 | implemented | 2026-05-05 | 90% |

### 定时任务

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [AI-Native-Cronjob-Spec.md](./AI-Native-Cronjob-Spec.md) | AI 原生定时任务 — Worker 自管理调度系统 | implemented | 2026-05-09 | 100% |
| [Cron-Fast-Path-Spec.md](./Cron-Fast-Path-Spec.md) | Cron Fast Path — 会话内回调机制 | draft | 2026-05-11 | 0% |

### CLI 与 Onboard

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [CLI-Self-Service-Spec.md](./CLI-Self-Service-Spec.md) | CLI 自服务规格 — doctor/onboard/status 子命令 | implemented | 2026-04-22 | 95% |
| [Onboard-UX-Improvement-Spec.md](./Onboard-UX-Improvement-Spec.md) | Onboard UX 改进 — 向导式引导流程 | draft | 2026-05-06 | 10% |
| [Onboard-Go-Embed-AST.md](./Onboard-Go-Embed-AST.md) | Onboard Go:Embed AST 重构 — 模板嵌入优化 | proposed | 2026-04-25 | 0% |

### 前端与平台

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [WebChat-v2-Revamp-Spec.md](./WebChat-v2-Revamp-Spec.md) | WebChat v2 改版 — 产品愿景与技术路线 | proposed | 2026-04-20 | 0% |
| [TTS-Engine-Spec.md](./TTS-Engine-Spec.md) | TTS 引擎规格 — Edge-TTS + Kokoro 语音合成 | draft | 2026-05-07 | 15% |
| [2026-04-29-windows-support.md](./2026-04-29-windows-support.md) | Windows 平台支持 — 跨平台兼容规格 | proposed | 2026-04-29 | 30% |

### 验收标准与跟踪

| 文档 | 描述 | 状态 |
|------|------|------|
| [Acceptance-Criteria.md](./Acceptance-Criteria.md) | 157 条验收标准完整定义 | draft |
| [AC-Tracking-Matrix.md](./AC-Tracking-Matrix.md) | 验收标准跟踪矩阵（Markdown） | active |
| [AC-Tracking-Matrix.csv](./AC-Tracking-Matrix.csv) | 验收状态跟踪矩阵（CSV） | active |
| [TRACEABILITY-MATRIX.md](./TRACEABILITY-MATRIX.md) | 功能实现与代码溯源矩阵 | active |

---

## 状态统计

### 按状态分类

- **implemented**: 4 个 — Per-Bot-Agent-Config, Turn-Summary-WorkDir-Fix, Worker-User-Interaction, Slack-Stream-Rotation
- **draft**: 7 个 — Gateway-Async-Init, Worker-ACPX, Feishu-Adapter, Hot-Reload, Session-History, Turn-Summary, CLI-Self-Service, Onboard-UX, TTS-Engine
- **proposed**: 5 个 — Consolidate-Events-Store, Delta-Optimization, Interaction-Response-Chain-Fix, Inbound-Event-Storage-Fix, Onboard-Go-Embed-AST, WebChat-v2-Revamp, Windows-Support

### 按领域分类

- **架构/Gateway**: 9 个
- **Worker/Session**: 4 个
- **平台适配**: 2 个
- **定时任务**: 2 个
- **CLI/Onboard**: 3 个
- **前端/平台**: 3 个
- **跟踪矩阵**: 4 个

---

## 关联规范文档

### 架构设计

- [`../architecture/AEP-v1-Protocol.md`](../architecture/AEP-v1-Protocol.md), [`../architecture/AEP-v1-Appendix.md`](../architecture/AEP-v1-Appendix.md)
- [`../architecture/Worker-Gateway-Design.md`](../architecture/Worker-Gateway-Design.md), [`../architecture/Message-Persistence.md`](../architecture/Message-Persistence.md)

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

## 已归档

12 个已实现 spec + 4 个过时文件已移至 [`../archive/specs/`](../archive/specs/)。详见 [`../archive/README.md`](../archive/README.md)。
