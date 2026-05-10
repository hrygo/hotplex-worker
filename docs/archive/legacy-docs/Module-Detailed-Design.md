# HotPlex Worker Gateway — 模块详细设计 (Module Detailed Design)

> **设计目标**：本文档详细描述 HotPlex Worker Gateway 核心模块的功能逻辑、组件交互与关键实现策略，作为项目固定资产供后续维护与扩展参考。

---

## 1. 接入层 (Gateway Module)

Gateway 模块负责维护物理连接（WebSocket）并实现 AEP 协议的封包解析。

### 1.1 Hub-Connection 模型
采用典型的 **Hub-Connection 扇入扇出模型**：
- **Hub**：全局唯一的中央交换机，负责连接的注册、注销以及消息的跨 Session 路由。
- **Connection**：每个 WebSocket 连接对应的处理单元，拥有独立的 `ReadPump` 和 `WritePump` 协程，通过带缓冲的 Channel 实现异步非阻塞 I/O。

### 1.2 AEP 事件分发 (Event Dispatching)
- **封包解析**：所有消息经过 AEP v1 Codec，统一封装为 `Envelope` 对象。
- **中间件处理**：在分发前进行认证校验、Panic 恢复以及审计日志记录。
- **会话绑定**：根据 `session_id` 将事件分发至对应的 `managedSession` 实例。

---

## 2. 内核层 (Session & Pool Module)

负责逻辑会话的生命周期管理与全局资源调度。

### 2.1 5-状态机 (State Machine)
Session 遵循严格的状态转换规则：
1.  **CREATED**：元数据已创建，Worker 进程尚未启动。
2.  **RUNNING**：Worker 正在处理输入或执行工具。
3.  **IDLE**：Worker 处于挂起等待状态（Hot-reuse 基础）。
4.  **TERMINATED**：Worker 已退出，状态机保持可恢复（Resume）。
5.  **DELETED**：物理删除，所有内存与进程资源已释放。

### 2.2 确定性 ID 映射
利用 **UUIDv5 (SHA-1)** 算法，将 `(owner_id, worker_type, client_session_id)` 映射为全局唯一的 `session_id`。
- **优势**：实现无状态网关对状态化会话的精确寻址，支持客户端断线重连后的语义恢复。

### 2.3 配额管理 (Pool Management)
- **并发控制**：限制全局与单用户的最大活跃会话数。
- **内存追踪**：基于 Worker 类型估算内存占用（如 512MB/Worker），实现动态资源预警与准入控制。

---

## 3. 执行层 (Worker Module)

负责底层 Agent 运行时的隔离、交互与生命周期控制。

### 3.1 适配器抽象 (BaseWorker)
通过 `BaseWorker` 基类提供标准化的接口：
- **Stdio 适配器**：用于封装 CLI 工具（如 Claude Code），通过管道重定向 NDJSON 流。
- **HTTP/SSE 适配器**：用于封装远程或独立的 Server 服务（如 OpenCode Server）。

### 3.2 进程治理
- **PGID 隔离**：每个 Worker 启动时分配独立的进程组 ID，确保 Terminate 时能清理所有子进程。
- **僵尸检测 (Zombie IO Polling)**：监控 `LastIO` 时间戳，对长时间无有效输出的 `RUNNING` 进程进行强制清退。

---

## 4. 集成层 (Messaging Bridge)

负责对接社交平台，并处理高频交互中的用户体验与一致性问题。

### 4.1 平台适配器 (Platform Adapter)
统一 Slack、飞书等平台的输入输出：
- **输入归一化**：将不同平台的 Mention、RichText、Thread 逻辑统一转换为 `input` 事件。
- **输出格式化**：将 Markdown 转换为平台特定的格式（如 Slack Mrkdwn, 飞书 CardKit）。

### 4.2 交互管理器 (Interaction Manager)
管理复杂的异步交互流程：
- **权限与问答**：处理 Worker 发出的 `permission_request` 或 `elicitation`。
- **自动拒绝机制**：内置超时策略，防止交互请求长期挂起阻塞 Worker。

### 4.3 流控算法
- **消息切片 (Chunking)**：针对 Slack 等平台的单条消息长度限制进行智能分割。
- **增量合并 (Delta Coalescing)**：设置 120ms 的合并窗口，将高频输出合并后更新，兼顾流式体验与平台 API 配额。

---

## 5. 管理与支撑 (Management & Support)

### 5.1 Admin API
提供 RBAC 保护的 REST 接口，用于：
- 会话生命周期干预（强制终止、删除）。
- 实时运行统计与健康状态检查。
- 配置热重载校验与版本回滚。

### 5.2 存储策略
- **SQLite WAL**：使用 SQLite 的预写日志模式，支持高频并发读取。
- **单写序列化**：通过独占 Channel 序列化所有写入操作，确保数据库在高并发下的稳定性。
