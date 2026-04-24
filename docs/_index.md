---
title: HotPlex v1.0 — Design Documents
type: moc
tags:
  - project/HotPlex
status: active
---

# HotPlex v1.0 — Design Documents

> HotPlex v1.0.0 大版本设计文档集合。从当前 Cli-as-a-Service 重构为 Agent Gateway 平台。

---

## 文档索引

| 文档 | 角色 | 回答的问题 |
|------|------|-----------|
| **[核心资产]** | | |
| [[Architecture-Design]] | **架构设计** (Architecture) | 整体分层、核心概念、安全体系与部署模型 |
| [[Module-Detailed-Design]] | **详细设计** (Detailed Design) | 各模块交互逻辑、状态机、隔离治理与适配策略 |
| [[Protocol-Core-Asset]] | **协议资产** (Protocol) | AEP v1 协议视图、事件分类法与时序逻辑 |
| [[Security-Governance]] | **安全治理** (Security) | 威胁模型、分层防御、隔离策略与审计合规 |
| **[设计规格 (Specs)]** | | |
| [[architecture/Worker-Gateway-Design]] | 系统设计（WHY & WHAT） | 做什么？为什么？架构怎么分？ |
| [[architecture/AEP-v1-Protocol]] | 协议规范（HOW — wire format） | 消息长什么样？事件有哪些？怎么扩展？ |
| [[architecture/AEP-v1-Appendix]] | 可视化补充（DIAGRAM） | 时序怎么走？状态怎么转？竞态怎么防？ |
| [[security/Security-Authentication]] | 安全认证设计 | JWT 认证、Session Ownership、Admin Token |
| [[security/Security-InputValidation]] | 输入验证与命令注入防护 | Shell 元字符过滤、白名单校验 |
| [[management/Admin-API-Design]] | Admin API 设计 | Session 管理、统计查询、健康检查 |
| [[architecture/Message-Persistence]] | 消息持久化设计 | 事件存储、审计日志、会话重放 |
| [[testing/Testing-Strategy]] | 测试策略 | 单元测试、集成测试、e2e 测试 |
| [[management/Observability-Design]] | 可观测性设计 | 日志、Metrics、Tracing |
| [[management/Config-Management]] | 配置管理架构 | 配置分层、环境变量、敏感值管理 |
| [[management/Resource-Management]] | 资源管理与权限控制 | Session Ownership、输出限制、资源配额 |
| [[security/Env-Whitelist-Strategy]] | 环境变量白名单 | 敏感变量过滤、安全注入 |
| [[security/AI-Tool-Policy]] | AI 工具权限控制 | AllowedTools、Bash 命令拦截 |
| [[security/SSRF-Protection]] | SSRF 防护 | URL 验证、IP 阻断、DNS 重绑定防护 |
| **[操作手册 (Manuals)]** | | |
| [[STT-Setup]] | STT 安装手册 | 飞书 STT 云端配置、本地模型安装、ONNX 修补 |
| [[User-Manual]] | 用户手册 | 快速开始、安装构建、CLI 参数与操作指南 |
| [[management/Config-Reference]] | 配置参考手册 | 完整 YAML 字段说明与环境变量映射表 |
| [[Disaster-Recovery]] | 灾难恢复手册 | 故障切换、备份恢复与容灾预案 |
| **[组件规格 (Specs)]** | | |
| [[architecture/WebSocket-Full-Duplex-Flow]] | 全双工流时序 | 客户端/网关/Worker 间的数据流向与状态同步 |
| [[architecture/Agent-Config-Design]] | Agent 配置注入设计 | B/C 通道映射、Worker Context 槽位分析、双 Worker 注入方案 |
| [[specs/Worker-ClaudeCode-Spec]] | Worker 规格 (Claude) | Claude Code CLI 进程生命周期与 I/O 协议 |
| [[specs/Worker-OpenCode-Server-Spec]] | Worker 规格 (OpenCode) | OpenCode Server (HTTP/SSE)适配规格 |
| [[specs/Slack-Adapter-Improvement-Spec]] | 平台适配 (Slack) | 消息分片、增量更新与 Socket Mode 优化 |
| [[specs/Feishu-Adapter-Improvement-Spec]] | 平台适配 (飞书) | CardKit 流式卡片更新与 4 层异常防护 |
| [[Product-Whitepaper]] | 产品白皮书 | 背景、核心价值、业务流程与愿景 |

---

## 文档关系

```
Worker-Gateway-Design（系统设计）
    │
    │  §5 Session 模型 ──────────► Appendix §2.1 状态机图
    │  §5.4 竞态防护 ───────────► Appendix §4 竞态分析
    │  §6 数据模型 ─────────────► SQLite sessions 表
    │  §7 Worker 抽象 ───────────► Appendix §3 真实 Trace
    │  §8 Worker 集成规格 ──────►/Server 详细规格
    │  §9 消息协议 ─────────────► AEP v1（完整协议规范）
    │  §11 WS 生命周期 ─────────► Appendix §1 时序图
    │  §13 持久化职责 ──────────► Worker 自身负责（无 event_log）
    │
    ▼
AEP-v1-Protocol（协议层）
    │
    │  §2 Envelope ─────────────► seq 语义 + ms 精度 timestamp
    │  §3 Event Kind (17种) ───► Appendix §2.2 Event 流状态
    │  §3.10 Error ─────────────► 16 个结构化错误码
    │  §3.11 done.stats ───────► token/cost/model 统计
    │  §3.16 permission ───────► 权限请求/响应交互
    │  §7 Versioning ──────────► 版本协商失败响应
    │  §12 行业参考 ───────────► A2A / MCP / OpenAI / Discord
    │
    ▼
AEP-v1-Appendix（可视化层）
    │  §1.1-1.12 时序图（12个，含启动失败/僵死/多tool/admin kill）
    │  §2 状态机（含完整转换表）
    │  §4 竞态防护分析（3场景）
    │  §5 时序约束汇总（因果顺序 + 时间约束）
    └── 连接 PRD 架构概念 ↔ AEP 协议细节

安全与运维层
    │
    ├─ Security-Authentication ───► JWT + TLS + Session Ownership + jti/ES256
    ├─ Security-InputValidation ──► Shell 元字符过滤 + 白名单 + AI Tool 引用
    ├─ Admin-API-Design ──────────► Session 管理 + 监控端点
    ├─ Message-Persistence ───────► 事件存储 + 审计日志（插件架构）
    ├─ Testing-Strategy ─────────► 单元/集成/e2e 测试
    ├─ Observability-Design ─────► 日志 + Metrics + Tracing
    ├─ Config-Management ─────────► 配置分层 + 环境变量（ExpandEnv 警告）
    ├─ Resource-Management ───────► 权限控制 + 资源限制
    ├─ Env-Whitelist-Strategy ───► 环境变量白名单（ProtectedEnvVars）
    ├─ AI-Tool-Policy ───────────► AllowedTools + Bash 拦截
    └─ SSRF-Protection ─────────► URL 验证 + IP 阻断 + DNS 重绑定
```

---

## 阅读顺序

```
1. Worker-Gateway-Design       → 理解系统全貌和设计约束
2. AEP-v1-Protocol             → 理解线上消息如何编码和流转
3. AEP-v1-Appendix             → 用图和 trace 建立直觉，理解边界场景
```

---

## 当前设计范围

v1.0-design 当前覆盖 **Worker 抽象封装 + WS Gateway + AEP 协议**，后续将补充：

- 安全与隔离增强（沙箱 / cgroup / network namespace）
- 可观测性（trace / metrics 集成 AEP）
- Admin API（REST over HTTP，独立于 AEP WebSocket）
- Cron / Relay（任务调度和中继路由）
- ChatApps 平台适配迁移到 AEP

---

## 行业协议参考

| 协议 | 借鉴点 | 应用方式 |
|------|--------|----------|
| **A2A (Agent-to-Agent)** | Agent Card 能力协商 | Worker Capabilities 在 init 时交换 |
| **MCP (Model Context Protocol)** | initialize → initialized 握手 | AEP 的 init / init_ack 设计 |
| **OpenAI Realtime API** | event_id 引用错误源 | error.event_id 引用触发事件 |
| **Discord Gateway** | seq-based session 管理 | session_id reconnect + 能力协商 |
| **SSE (EventSource)** | 断线重连模式 | session_id resume（Worker 自身持久化） |
| **gRPC Streaming** | HTTP/2 flow control | v1.1 client-side throttling 参考 |

---

## 后续版本规划

```
1-Projects/HotPlex/
├── v1.0-design/     ← 当前（Agent Gateway + AEP 协议）
├── v1.1-design/     ← SSE fallback / 动态配置 / 多客户端 attach / client-side throttling
└── v2.0-design/     ← 多实例 / 分布式 / 调度层 / 多 agent 协作（A2A）
```
