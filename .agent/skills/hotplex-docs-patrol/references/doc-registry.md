# 文档注册表

代码区域到文档的映射关系。巡逻时基于此表按图索骥。

## Gateway Core (`internal/gateway/`)

WebSocket hub、AEP 事件分发、Session-Worker 生命周期编排。

| 文件 | 职责 |
|------|------|
| hub.go | WS 广播中心 |
| conn.go | 单连接 ReadPump/WritePump |
| handler.go | AEP 事件分发 |
| bridge.go | Session ↔ Worker 编排 |
| llm_retry.go | LLM 自动重试 |
| api.go | HTTP session 端点 |

→ `explanation/session-lifecycle.md` — Session 5 状态机（bridge 状态迁移）
→ `guides/enterprise/deployment.md` — 生产部署架构（hub/conn 并发模型）
→ `guides/enterprise/observability.md` — 可观测性（metrics、tracing）
→ `reference/aep-protocol.md` — AEP v1 协议（handler 事件类型）
→ `reference/admin-api.md` — Admin API（api.go 端点）
→ `reference/metrics.md` — Prometheus 指标

**影响规则**：hub/conn 变更 → 部署+可观测性；handler 变更 → AEP 协议；bridge 变更 → session 生命周期；新增端点 → admin API。

---

## Session Management (`internal/session/`)

5 状态机、SQLite 持久化、UUIDv5 确定性 ID、全局+每用户配额。

→ `explanation/session-lifecycle.md` — 状态机设计（必须与代码同步）
→ `guides/developer/session-management.md` — 开发者操作指南
→ `guides/enterprise/resource-limits.md` — 配额和资源管理
→ `guides/enterprise/multi-tenant.md` — 多租户隔离

**影响规则**：状态机变更 → 必须更新 session-lifecycle.md；配额逻辑 → resource-limits.md。

---

## Brain (`internal/brain/`)

LLM 智能中间件：意图路由、安全审计、上下文压缩、装饰器链、4 层 API Key 发现。

→ `explanation/brain-llm-orchestration.md` — Brain 架构和中间件链
→ `explanation/security-model.md` — 安全模型（guard 安全审计）
→ `guides/developer/security-model.md` — 开发者安全配置
→ `reference/security-policies.md` — 安全策略参考
→ `reference/configuration.md` — 配置完整参考（brain 子配置）

**影响规则**：router 变更 → brain 文档；guard 变更 → 安全文档；config 结构变更 → configuration.md。

---

## Cron (`internal/cron/`)

AI-native 定时任务：scheduler、timerLoop、SQLite 持久化、平台投递。

→ `explanation/cron-design.md` — 调度器设计原理
→ `tutorials/cron-scheduled-tasks.md` — 用户教程
→ `guides/developer/cron-automation.md` — 开发者指南
→ `reference/cli.md` — CLI 命令参考（cron 子命令）

**影响规则**：调度逻辑 → cron-design.md；CLI 命令 → cli.md；投递机制 → 教程。

---

## Messaging (`internal/messaging/`)

消息适配器基类、Slack Socket Mode、飞书 WS、STT/TTS、权限交互。

→ `tutorials/slack-integration.md` — Slack 集成
→ `tutorials/feishu-integration.md` — 飞书集成
→ `guides/user/chat-with-ai.md` — AI 对话指南
→ `guides/user/commands-cheatsheet.md` — 命令速查表
→ `guides/user/mobile-access.md` — 移动端访问
→ `guides/developer/voice-features.md` — 语音功能

**影响规则**：平台适配器变更 → 对应集成教程；新增斜杠命令 → 速查表；TTS/STT → voice-features。

---

## Agent Config (`internal/agentconfig/`)

B/C 通道配置加载、BuildSystemPrompt、三级 fallback、XML 安全。

→ `explanation/agent-config-system.md` — 配置系统原理
→ `tutorials/agent-personality.md` — Agent 人格定制
→ `reference/configuration.md` — 配置参考

---

## Worker (`internal/worker/`)

Claude Code stdio 适配器、OpenCode Server HTTP+SSE、进程管理。

→ `guides/developer/remote-coding-agent.md` — 远程开发指南
→ `guides/developer/multiple-agents.md` — 多 Agent 协作
→ `guides/enterprise/resource-limits.md` — 资源限制

---

## CLI Commands (`cmd/hotplex/`)

Cobra 根命令和子命令：gateway、cron、service、update、slack。

→ `reference/cli.md` — CLI 完整参考（必须与代码同步）
→ `getting-started.md` — 快速开始（基本命令示例）

**影响规则**：新增/修改/删除子命令或标志 → 必须更新 cli.md。

---

## Config (`internal/config/`, `configs/`)

Viper 配置加载、热重载、三级继承、审计回滚。

→ `reference/configuration.md` — 配置完整参考
→ `guides/enterprise/config-management.md` — 配置管理指南

---

## Security (`internal/security/`)

JWT 认证、SSRF 防御、路径安全、白名单。

→ `explanation/security-model.md` — 安全模型
→ `guides/developer/security-model.md` — 开发者安全指南
→ `guides/enterprise/security-hardening.md` — 安全加固
→ `reference/security-policies.md` — 安全策略参考

---

## AEP Protocol (`pkg/events/`, `pkg/aep/`)

AEP v1 事件类型和编解码。

→ `reference/aep-protocol.md` — 协议规范
→ `reference/events.md` — 事件类型参考

---

## 文档定位速查

| 分类 | 受众 | 定位 | 维护频率 | 关键特征 |
|------|------|------|---------|---------|
| `getting-started.md` | all | 5 分钟快速上手 | 低 | 稳定，极少变更 |
| `tutorials/*` | developer | 手把手 step-by-step | 中 | 跟随功能更新 |
| `guides/user/*` | user | 用户操作手册 | 中 | 跟随 UI/交互变更 |
| `guides/developer/*` | developer | 开发者深度指南 | 高 | 跟随代码变更 |
| `guides/enterprise/*` | enterprise | 企业运维和安全管理 | 中高 | 跟随安全/部署变更 |
| `guides/contributor/*` | contributor | 贡献者指南 | 低 | 跟随开发流程变更 |
| `reference/*` | developer/enterprise | API/协议/配置权威参考 | 高 | **必须与代码严格同步** |
| `explanation/*` | developer | 原理解析和架构理解 | 中 | 仅重大架构变更时更新 |

**优先级**：reference 文档过时是最严重的——用户直接按文档操作会失败。explanation 文档轻微过时可容忍。
