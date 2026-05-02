# HotPlex 用户手册

HotPlex 是一个 AI 编码助手的统一管理平台。它让你通过 **飞书、Slack、网页聊天** 等渠道与 Claude Code 等 AI 智能体对话，完成代码编写、重构、调试等任务。

---

## 安装

从发布页面下载对应系统的二进制文件，或从源码构建：

```bash
make build
# 产物在 bin/ 目录下
```

支持 macOS (arm64) 和 Linux (amd64/arm64)。

---

## 首次配置

运行交互式配置向导，跟随提示完成：

```bash
hotplex onboard
```

向导会引导你完成以下步骤：

1. **选择消息平台** — 启用 Slack、飞书或两者
2. **输入凭据** — Slack Bot Token / 飞书 App ID 和 App Secret
3. **设置工作目录** — AI 智能体可以访问的项目路径
4. **生成配置文件** — 自动写入 `~/.hotplex/config.yaml`

非交互模式（适合自动化部署）：

```bash
hotplex onboard --non-interactive --enable-slack --enable-feishu
```

配置完成后，运行诊断确认一切就绪：

```bash
hotplex doctor
```

---

## 启动与停止

```bash
# 前台运行（可直接看到日志输出）
hotplex gateway start

# 后台守护进程模式
hotplex gateway start -d

# 查看运行状态
hotplex status

# 查看日志
hotplex gateway logs

# 重启
hotplex gateway restart

# 停止
hotplex gateway stop
```

### 安装为系统服务（推荐生产使用）

HotPlex 可以注册为系统服务，实现开机自启和自动重启：

```bash
# 安装为用户级服务（无需 root）
hotplex service install

# 管理服务
hotplex service start      # 启动
hotplex service stop       # 停止
hotplex service restart    # 重启
hotplex service status     # 查看状态
hotplex service logs -f    # 实时查看日志

# 系统级安装（所有用户共享，需要 sudo）
sudo hotplex service install --level system

# 卸载服务
hotplex service uninstall
```

支持的平台：
- **Linux** — systemd（用户级或系统级 unit）
- **macOS** — launchd（LaunchAgents 或 LaunchDaemons）
- **Windows** — Windows Service Control Manager

启动成功后会显示配置摘要：

```
  Gateway    localhost:8888
  Admin      localhost:9999
  WebChat    http://localhost:8888/
  Sessions   ~/.hotplex/data/hotplex.db
  Events     ~/.hotplex/data/events.db
  Adapters   feishu ✓  slack ✓
```

---

## 使用飞书或 Slack 与 AI 对话

配置完成后，在飞书或 Slack 中直接给 Bot 发消息即可开始对话。

### 发送消息

像和同事聊天一样，直接输入你的需求：

```
帮我重构 hub.go 中的锁逻辑
```

```
看看这个 bug 为什么会发生：panic: concurrent map writes
```

### 控制命令

在聊天中输入以下命令来管理会话：

#### 会话控制

| 命令 | 说明 |
|:-----|:-----|
| `/gc` 或 `/park` | 休眠当前会话（停止 AI 后台进程，保留对话记录） |
| `/reset` 或 `/new` | 重置会话（清除上下文，重新开始） |
| `/cd <目录路径>` | 切换工作目录（如 `/cd ~/projects/myapp`） |

#### 对话管理

| 命令 | 说明 |
|:-----|:-----|
| `/compact` | 压缩对话历史（上下文窗口快满时使用） |
| `/clear` | 清空当前对话 |
| `/rewind` | 撤销上一轮对话（AI 回复不满意时使用） |
| `/commit` | 让 AI 创建 Git 提交 |

#### 查看信息

| 命令 | 说明 |
|:-----|:-----|
| `/context` | 查看上下文窗口使用量 |
| `/skills` | 查看已加载的技能列表 |
| `/skills <关键词>` | 搜索特定技能 |
| `/mcp` | 查看 MCP 服务器状态 |

#### 其他

| 命令 | 说明 |
|:-----|:-----|
| `/model <名称>` | 切换 AI 模型 |
| `/perm <模式>` | 设置权限模式 |
| `/effort <级别>` | 设置推理力度 |
| `?` | 显示帮助信息 |
| `$<中文>` | 自然语言触发（如 `$休眠`、`$切换目录 ~/app`） |

### 权限请求

AI 在执行某些操作（如运行命令、写入文件）时，会向你发送权限请求。你有 **5 分钟** 时间来批准或拒绝，超时将自动拒绝。

在飞书中，权限请求以交互卡片形式展示，点击按钮即可操作。

---

## 使用网页聊天

浏览器访问网关地址即可打开网页聊天界面：

```
http://localhost:8888/
```

网页聊天支持与飞书/Slack 相同的命令。你的会话会在不同渠道间保持独立。

---

## 配置

配置文件位于 `~/.hotplex/config.yaml`，修改后大部分设置会自动热重载，无需重启。

### 常用配置项

```yaml
# 网络监听地址
gateway:
  addr: "localhost:8888"   # 改为 ":8888" 可允许外部访问

# 管理接口
admin:
  addr: "localhost:9999"   # 改为 ":9999" 可允许外部访问
  tokens: ["your-admin-token"]

# Worker 行为
worker:
  idle_timeout: 60m        # 无活动自动休眠时间
  max_lifetime: 24h        # 强制重启周期

# 会话池
pool:
  max_size: 100            # 最大并发会话数
  max_idle_per_user: 5     # 每用户最大空闲会话数

# 安全
security:
  api_keys:
    - "sk-your-secret-key"
```

### 配置文件位置

| 文件 | 用途 |
|:-----|:-----|
| `~/.hotplex/config.yaml` | 主配置文件 |
| `~/.hotplex/.env` | 环境变量（存放密钥等敏感信息） |
| `~/.hotplex/agent-configs/` | AI 人格配置目录 |

### 环境变量

所有配置项都可以通过环境变量覆盖，格式为 `HOTPLEX_<段>_<字段>`：

```bash
export HOTPLEX_GATEWAY_ADDR=":8888"
export HOTPLEX_DB_PATH="/var/lib/hotplex/hotplex.db"
export HOTPLEX_DB_EVENTS_PATH="/var/lib/hotplex/events.db"
```

---

## Agent 人格配置

通过在 `~/.hotplex/agent-configs/` 目录放置 Markdown 文件来定制 AI 的行为：

| 文件 | 用途 |
|:-----|:-----|
| `SOUL.md` | AI 的核心人格和行为准则 |
| `AGENTS.md` | 协作规范和团队约定 |
| `SKILLS.md` | 技能使用指引 |
| `USER.md` | 用户背景和偏好 |
| `MEMORY.md` | AI 的长期记忆 |

还可以为不同平台创建变体，如 `SOUL.slack.md`（仅在 Slack 渠道生效）。

每个文件最大 8KB，总计不超过 40KB。

---

## 健康检查与监控

### 健康状态

```bash
curl http://localhost:9999/admin/health
```

### 会话管理

```bash
# 查看所有会话
curl -H "Authorization: Bearer your-token" http://localhost:9999/admin/sessions

# 查看实时统计
curl -H "Authorization: Bearer your-token" http://localhost:9999/admin/stats

# 终止指定会话
curl -X POST -H "Authorization: Bearer your-token" \
  http://localhost:9999/admin/sessions/<session-id>/terminate
```

### Prometheus 指标

```bash
curl http://localhost:9999/metrics
```

---

## 常用命令速查

```
# 基础命令
hotplex onboard              首次配置向导
hotplex doctor               运行诊断检查
hotplex status               查看运行状态
hotplex config validate      验证配置文件
hotplex security             安全审计
hotplex version              查看版本
hotplex update               自更新到最新版本
hotplex update --check       仅检查更新
hotplex update -y            跳过确认提示

# 网关管理
hotplex gateway start        前台启动网关
hotplex gateway start -d     后台启动网关
hotplex gateway stop         停止网关
hotplex gateway restart      重启网关
hotplex gateway logs         查看日志

# 系统服务（推荐生产使用）
hotplex service install      安装为系统服务
hotplex service start        启动服务
hotplex service stop         停止服务
hotplex service restart      重启服务
hotplex service status       查看服务状态
hotplex service logs -f      实时查看日志
hotplex service uninstall    卸载服务
```

---

## 故障排除

### 网关无法启动

1. 运行诊断：`hotplex doctor`
2. 检查端口占用：`lsof -i :8888`
3. 验证配置文件：`hotplex config validate`

### 飞书/Slack 收不到消息

1. 确认 Adapter 状态：`hotplex status` 查看是否显示 `feishu ✓` 或 `slack ✓`
2. 检查凭据是否正确配置在 `~/.hotplex/.env` 中
3. 查看日志：`hotplex gateway logs`

### AI 不回复

1. 确认 Worker 二进制（`claude` 或 `opencode`）已安装且在 PATH 中
2. 检查 API Key 是否有效
3. 查看会话状态是否为 `idle`（可用 `/gc` 后重试）

### 上下文窗口不足

使用 `/compact` 压缩对话历史，或使用 `/reset` 开始新会话。

---

## 更多文档

- [技术参考手册](./Reference-Manual.md) — 完整的 API 和配置字段说明
- [安全治理规范](./security/Security-Governance.md) — 安全策略和审计
- [架构设计](./architecture/) — 系统架构和协议设计
