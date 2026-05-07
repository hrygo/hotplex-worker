# HotPlex — Design Documents

> HotPlex 设计文档集合。从当前 Cli-as-a-Service 重构为 Agent Gateway 平台。

---

## 文档索引

| 文档 | 角色 | 回答的问题 |
|------|------|-----------|
| **[核心资产]** | | |
| [Architecture-Design](./Architecture-Design.md) | **架构设计** (Architecture) | 整体分层、核心概念、安全体系与部署模型 |
| [Module-Detailed-Design](./Module-Detailed-Design.md) | **详细设计** (Detailed Design) | 各模块交互逻辑、状态机、隔离治理与适配策略 |
| [Protocol-Core-Asset](./Protocol-Core-Asset.md) | **协议资产** (Protocol) | AEP v1 协议视图、事件分类法与时序逻辑 |
| [Security-Governance](./Security-Governance.md) | **安全治理** (Security) | 威胁模型、分层防御、隔离策略与审计合规 |
| **[设计规格 (Specs)]** | | |
| [architecture/Worker-Gateway-Design](./architecture/Worker-Gateway-Design.md) | 系统设计（WHY & WHAT） | 做什么？为什么？架构怎么分？ |
| [architecture/AEP-v1-Protocol](./architecture/AEP-v1-Protocol.md) | 协议规范（HOW — wire format） | 消息长什么样？事件有哪些？怎么扩展？ |
| [architecture/AEP-v1-Appendix](./architecture/AEP-v1-Appendix.md) | 可视化补充（DIAGRAM） | 时序怎么走？状态怎么转？竞态怎么防？ |
| [security/Security-Authentication](./security/Security-Authentication.md) | 安全认证设计 | JWT 认证、Session Ownership、Admin Token |
| [security/Security-InputValidation](./security/Security-InputValidation.md) | 输入验证与命令注入防护 | Shell 元字符过滤、白名单校验 |
| [management/Admin-API-Design](./management/Admin-API-Design.md) | Admin API 设计 | Session 管理、统计查询、健康检查 |
| [architecture/Message-Persistence](./architecture/Message-Persistence.md) | 消息持久化设计 | 事件存储、审计日志、会话重放 |
| [testing/Testing-Strategy](./testing/Testing-Strategy.md) | 测试策略 | 单元测试、集成测试、e2e 测试 |
| [management/Observability-Design](./management/Observability-Design.md) | 可观测性设计 | 日志、Metrics、Tracing |
| [management/Config-Management](./management/Config-Management.md) | 配置管理架构 | 配置分层、环境变量、敏感值管理 |
| [management/Resource-Management](./management/Resource-Management.md) | 资源管理与权限控制 | Session Ownership、输出限制、资源配额 |
| [security/Env-Whitelist-Strategy](./security/Env-Whitelist-Strategy.md) | 环境变量白名单 | 敏感变量过滤、安全注入 |
| [security/AI-Tool-Policy](./security/AI-Tool-Policy.md) | AI 工具权限控制 | AllowedTools、Bash 命令拦截 |
| [security/SSRF-Protection](./security/SSRF-Protection.md) | SSRF 防护 | URL 验证、IP 阻断、DNS 重绑定防护 |
| **[操作手册 (Manuals)]** | | |
| [channels/STT-SETUP](./channels/STT-SETUP.md) | STT 安装手册 | 飞书 STT 云端配置、本地模型安装、ONNX 修补 |
| [User-Manual](./User-Manual.md) | 用户手册 | 快速开始、安装构建、CLI 参数与操作指南 |
| [management/Config-Reference](./management/Config-Reference.md) | 配置参考手册 | 完整 YAML 字段说明与环境变量映射表 |
| [Disaster-Recovery](./Disaster-Recovery.md) | 灾难恢复手册 | 故障切换、备份恢复与容灾预案 |
| [permission-hooks-guide](./permission-hooks-guide.md) | 权限 Hook 配置指南 | PermissionRequest Hook 自定义与 Bash 命令拦截 |
| **[组件规格 (Specs)]** | | |
| [architecture/WebSocket-Full-Duplex-Flow](./architecture/WebSocket-Full-Duplex-Flow.md) | 全双工流时序 | 客户端/网关/Worker 间的数据流向与状态同步 |
| [architecture/Agent-Config-Design](./architecture/Agent-Config-Design.md) | Agent 配置注入设计 | B/C 通道映射、Worker Context 槽位分析、双 Worker 注入方案 |
| [architecture/Platform-Messaging-Extension](./architecture/Platform-Messaging-Extension.md) | 平台消息扩展 | 跨平台消息适配器框架 |
| [architecture/Platform-Messaging-Architecture-Diagrams](./architecture/Platform-Messaging-Architecture-Diagrams.md) | 消息架构图 | 平台消息系统可视化 |
| [architecture/Claude-Code-Context-Analysis](./architecture/Claude-Code-Context-Analysis.md) | Claude Code 上下文分析 | Worker 上下文注入与系统提示词分析 |
| [architecture/OpenCode-Server-Context-Analysis](./architecture/OpenCode-Server-Context-Analysis.md) | OpenCode Server 上下文分析 | OCS Worker 上下文注入与协议适配 |
| [Product-Whitepaper](./Product-Whitepaper.md) | 产品白皮书 | 背景、核心价值、业务流程与愿景 |

> 完整 Specs 索引见 [specs/README.md](./specs/README.md)，已归档文档见 [archive/README.md](./archive/README.md)。

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

## 行业协议参考

| 协议 | 借鉴点 | 应用方式 |
|------|--------|----------|
| **A2A (Agent-to-Agent)** | Agent Card 能力协商 | Worker Capabilities 在 init 时交换 |
| **MCP (Model Context Protocol)** | initialize → initialized 握手 | AEP 的 init / init_ack 设计 |
| **OpenAI Realtime API** | event_id 引用错误源 | error.event_id 引用触发事件 |
| **Discord Gateway** | seq-based session 管理 | session_id reconnect + 能力协商 |
| **SSE (EventSource)** | 断线重连模式 | session_id resume（Worker 自身持久化） |
| **gRPC Streaming** | HTTP/2 flow control | client-side throttling 参考 |
