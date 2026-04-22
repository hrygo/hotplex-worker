# 🚀 HotPlex Worker Gateway 快速上手手册

> **HotPlex Worker Gateway** 是为 AI 编码智能体（AI Coding Agents）设计的统一接入层。它将 **Claude Code**、**OpenCode Server** 等底层 Worker 抽象为统一的 **AEP v1** 协议接口，为开发者提供高性能、高可靠的会话管理能力。

---

## ⚡️ 1 分钟快速启动

无需复杂配置，即可体验 HotPlex 的核心能力。

### 1. 编译二进制文件
```bash
make build
```

### 2. 设置安全密钥 (JWT)
HotPlex 使用 ES256 算法保护会话安全，启动前请设置密钥：
```bash
export HOTPLEX_JWT_SECRET="your-256-bit-secret-key"
```

### 3. 启动网关 (开发模式)
```bash
# -dev 模式会放宽 API Key 校验，适合快速本地调试
./bin/hotplex-worker-darwin-arm64 -dev
```

🎉 **网关现在已在 `localhost:8888` (WebSocket) 和 `localhost:9999` (Admin API) 运行！**

---

## 🏗️ 核心架构概览

HotPlex 扮演着中转枢纽的角色：

| 组件 | 功能描述 | 访问地址 |
| :--- | :--- | :--- |
| **Gateway** | 核心 WebSocket 接口，处理 AEP v1 协议 | `ws://localhost:8888` |
| **Admin API** | 运维管理、会话监控、配置热重载 | `http://localhost:9999` |
| **Metrics** | Prometheus 标准监控指标 | `http://localhost:9999/metrics` |
| **Storage** | SQLite 会话持久化与审计日志 | `hotplex-worker.db` |

---

## 💬 基础交互流程

连接网关并启动一个 AI 编码会话仅需三步：

### 第 1 步：身份认证
连接 WebSocket 后，发送认证消息：
```json
{
  "type": "auth",
  "api_key": "sk-hotplex-default-key"
}
```

### 第 2 步：初始化会话
指定你想要启动的 Worker 类型（如 `claude-code`）：
```json
{
  "type": "session.init",
  "worker_type": "claude-code",
  "user_id": "tester_01",
  "metadata": {
    "work_dir": "/path/to/project"
  }
}
```

### 第 3 步：发送指令
开始与 AI 智能体对话：
```json
{
  "type": "input",
  "text": "请帮我重构 internal/gateway/hub.go 中的锁逻辑"
}
```

---

## ⚙️ 常用配置精要

你可以创建一个 `config.yaml` 来精细化控制网关行为：

```yaml
gateway:
  addr: ":8888"         # 监听端口
  idle_timeout: 5m      # 客户端空闲断开时间

worker:
  idle_timeout: 30m     # Worker 无活动自动休眠时间
  max_lifetime: 24h     # Worker 强制重启周期

security:
  api_keys:             # 允许接入的 API Key 列表
    - "sk-my-secret-key"

admin:
  addr: ":9999"         # 管理端口
  tokens: ["admin-it"] # 管理员访问 Token
```

> [!TIP]
> HotPlex 支持 **配置热重载**。修改配置文件后，网关会自动检测并应用（静态字段除外），无需重启服务。

---

## 🛠️ 运维管理工具

通过 Admin API 实时掌握集群状态：

- **健康检查**: `curl http://localhost:9999/admin/health`
- **实时统计**: `curl -H "Authorization: Bearer admin-it" http://localhost:9999/admin/stats`
- **会话列表**: `curl -H "Authorization: Bearer admin-it" http://localhost:9999/admin/sessions`
- **配置回滚**: `curl -X POST -H "Authorization: Bearer admin-it" http://localhost:9999/admin/config/rollback/1`

---

## 📚 进阶资源

如果您需要了解更深入的技术细节，请参阅以下文档：

- 📖 **[技术参考手册](./Reference-Manual.md)** - 包含完整的 API 列表、字段定义及故障排除指南。
- 🛠 **[AEP v1 协议规范](./architecture/AEP-v1-Protocol.md)** - 深入了解底层的 NDJSON 通讯细节。
- 🛡 **[安全治理规范](./Security-Governance.md)** - 了解 SSRF 防护、命令白名单及环境隔离机制。

---
*HotPlex - 让 AI 编码智能体接入变得简单而强大。*
