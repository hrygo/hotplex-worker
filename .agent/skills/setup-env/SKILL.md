---
name: setup-env
description: HotPlex 完整安装与配置指引。当用户需要安装、配置、更新 HotPlex 时使用此 skill — 包括首次安装、配置消息平台（Slack/飞书）、更新密钥、调整访问策略、配置 STT、切换 Worker 类型、调优资源限制、启用可观测性。也适用于用户提到"配置 slack"、"配置飞书"、"安装 hotplex"、"设置消息"、"更新 .env"、"添加白名单"、"获取用户 ID"、"配置 OTel"、"配置 worker"、"调整资源"等场景。不适用于与 HotPlex 无关的一般 .env 编辑。
---

# HotPlex 安装与配置指引

从零开始完成 HotPlex 的安装、配置、服务部署和验证。整个流程是幂等的——重复运行时只处理缺失或需要更新的部分。

## 第一步：环境检测

检测当前环境，确定安装方式。

```bash
# 操作系统与架构
uname -sm
# 检查是否已安装
which hotplex 2>/dev/null && hotplex version || echo "NOT_INSTALLED"
# 检查 Go（源码构建需要）
go version 2>/dev/null || echo "NO_GO"
```

根据结果向用户推荐安装方式：
- 已安装 → 跳到第三步（配置），询问是否需要更新版本
- 有 Go 1.26+ → 可选源码构建或二进制安装
- 无 Go → 二进制安装

## 第二步：安装

### 方式 A：一键二进制安装（推荐）

**macOS / Linux：**

```bash
# 最新版，用户级安装（无需 sudo）
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest --prefix ~/.local

# 最新版，系统级安装
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | sudo bash -s -- --latest

# 指定版本
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --release v1.3.0 --prefix ~/.local
```

**Windows（PowerShell 5.1+）：**

```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

安装脚本会自动：检测 OS/架构 → 下载二进制 → 校验 SHA256 → 安装到 PATH → 运行 `hotplex version` 验证。

如果 PATH 未生效，提示用户：
```bash
# 根据当前 shell 执行
export PATH="$HOME/.local/bin:$PATH"  # bash/zsh 加到 ~/.bashrc 或 ~/.zshrc
```

### 方式 B：源码构建

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # 检查工具链 + 构建 + 测试
```

二进制输出：`bin/hotplex-{os}-{arch}`

### 验证安装

```bash
hotplex version           # 应输出版本号
hotplex config validate   # 检查默认配置是否合法
```

## 第三步：评估当前配置

1. 检查 `.env` 是否存在。不存在则 `cp configs/env.example .env`。
2. 读取当前 `.env` 内容。
3. 生成状态表，仅展示缺失或异常项：

| 区域 | 关键字段 |
|------|---------|
| 密钥 | `HOTPLEX_JWT_SECRET`, `HOTPLEX_ADMIN_TOKEN_1` |
| 客户端认证 | `HOTPLEX_SECURITY_API_KEY_1..N` |
| 工作目录 | `SLACK_WORK_DIR`, `FEISHU_WORK_DIR` |
| Slack | `BOT_TOKEN`, `APP_TOKEN` |
| 飞书 | `APP_ID`, `APP_SECRET` |
| 访问策略 | `DM_POLICY`, `GROUP_POLICY`, `ALLOW_FROM`, `ALLOW_DM_FROM`, `ALLOW_GROUP_FROM` |
| 语音转文字 | `STT_PROVIDER`, `STT_LOCAL_MODE` |
| Agent 配置 | `AGENT_CONFIG_ENABLED`, `AGENT_CONFIG_DIR` |
| Worker | `WORKER_CLAUDE_CODE_COMMAND`, `WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD` |
| 可观测性 | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME` |
| 资源限制 | `SESSION_MAX_CONCURRENT`, `POOL_MAX_SIZE`, `POOL_MAX_MEMORY_PER_USER` |

## 第四步：收集消息平台凭据

使用 `AskUserQuestion` 批量收集缺失的凭据（每次最多 4 个问题）。

**Slack**（如果 Token 缺失）：
- `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN`（xoxb-...）
- `HOTPLEX_MESSAGING_SLACK_APP_TOKEN`（xapp-...）

**飞书**（如果凭据缺失）：
- `HOTPLEX_MESSAGING_FEISHU_APP_ID`（cli_xxx）
- `HOTPLEX_MESSAGING_FEISHU_APP_SECRET`

让用户通过"Other"输入粘贴值。绝不猜测或伪造 Token。

## 第五步：验证 Token

收集凭据后，并行调用 API 验证。

**Slack：**
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/auth.test"
```
- `ok: true` → 有效，记录 `user_id` 和 `team`
- `ok: false` → 报告错误，重新询问

**飞书：**
```bash
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<app_id>","app_secret":"<app_secret>"}'
```
- `code: 0` → 有效，记录 `tenant_access_token` 供下一步使用
- `code != 0` → 报告错误，重新询问

Token 无效时不继续后续步骤。

## 第六步：配置工作目录

Worker 进程需要工作目录。优先级：session 级别 > 平台级别 > 全局默认（`~/.hotplex/workspace`）。

询问用户各启用平台的工作目录：

```
Slack 会话的工作目录？（默认: ~/.hotplex/workspace）
飞书会话的工作目录？（默认: ~/.hotplex/workspace）
```

仅在用户指定非默认路径时设置：
```
HOTPLEX_MESSAGING_SLACK_WORK_DIR=/path/to/project
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=/path/to/project
```

用户接受默认值则不设置变量——`worker.default_work_dir` 自动生效。

## 第七步：自动获取用户 ID

用已验证的 Token 自动拉取工作区用户 ID。

**Slack** — 列出真实用户：
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/users.list?limit=50"
```
过滤：跳过 `is_bot: true` 和 `id == "USLACKBOT"`，展示供用户选择。

**飞书** — 列出联系人（需要上一步的 tenant_access_token）：
```bash
curl -s -H "Authorization: Bearer <tenant_access_token>" \
  "https://open.feishu.cn/open-apis/contact/v3/users?page_size=50&user_id_type=open_id"
```
展示供用户选择。

API 调用失败时提供手动查找指引：
- Slack：头像 → 三个点 → "Copy member ID"
- 飞书：管理后台 → 组织架构 → 找到 Open ID

## 第八步：配置访问策略

用 `AskUserQuestion` 让用户选择：

| 选项 | DM 策略 | 群组策略 | ALLOW_FROM |
|------|---------|---------|------------|
| 开放（仅限开发） | `open` | `open` | （空） |
| 白名单（推荐） | `allowlist` | `allowlist` | 上一步获取的用户 ID |
| 仅私聊 | `allowlist` | `disabled` | 上一步获取的用户 ID |

默认推荐：**白名单** + 用户自己的 ID。

精细化控制：
- `ALLOW_DM_FROM` — 可以私聊 Bot 的用户
- `ALLOW_GROUP_FROM` — 可以在群里使用 Bot 的用户
- `ALLOW_FROM` — 通用白名单（以上两者为空时生效）

选择"开放"时警告：工作区所有人都能使用 Bot。只警告一次。

## 第九步：配置语音转文字

两个平台都支持语音转文字：

| 平台 | 推荐方案 | 原因 |
|------|---------|------|
| Slack | `local` | Slack 没有云端 STT API |
| 飞书 | `feishu+local` | 原生云端 API + 本地兜底 |

设置环境变量：
```
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=ephemeral
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral
```

本地模式选项：`ephemeral`（按请求启动进程，默认）或 `persistent`（常驻子进程，预热后延迟更低）。

用户明确不需要 STT 时跳过此步。

## 第十步：配置 Worker 与 Agent

**Worker 类型** — 询问用户要使用的运行时：
- `claude_code`（默认）— Claude Code CLI
- `opencode_server` — OpenCode Server（单例进程，跨 session 共享）
- `pi` — Pi-mono

按平台设置：
```
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
```

`opencode_server` 需设置空闲回收时间（默认 30m）：
```
HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD=30m
```

`claude_code` 可选设置自定义命令路径：
```
HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=claude
```

**Agent 配置** — 默认启用，从 `~/.hotplex/agent-configs/` 加载人格文件：
```
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/
```
仅在用户想禁用或使用自定义目录时设置。

## 第十一步：写入 .env

组装完整的 `.env` 文件。结构：

```
# ── 必需密钥 ──
HOTPLEX_JWT_SECRET=<已生成或现有>
HOTPLEX_ADMIN_TOKEN_1=<已生成或现有>

# ── 客户端认证 ──
# HOTPLEX_SECURITY_API_KEY_1=<generated>

# ── 核心覆盖 ──
HOTPLEX_LOG_LEVEL=debug
HOTPLEX_LOG_FORMAT=text

# ── 资源限制 ──
# HOTPLEX_SESSION_MAX_CONCURRENT=1000
# HOTPLEX_POOL_MAX_SIZE=100
# HOTPLEX_POOL_MAX_MEMORY_PER_USER=8589934592

# ── 可观测性 ──
# OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
# OTEL_SERVICE_NAME=hotplex-gateway

# ── Slack ──
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_SLACK_WORK_DIR=<path>
HOTPLEX_MESSAGING_SLACK_DM_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=<user_id>
# HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=<user_id>
# HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=<user_id>

# ── Slack 语音转文字 ──
# HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
# HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=ephemeral

# ── 飞书 ──
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=<app_id>
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=<secret>
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=<path>
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=<open_id>
# HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM=<open_id>
# HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM=<open_id>

# ── 飞书语音转文字 ──
# HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
# HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral

# ── Agent 配置 ──
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/

# ── Worker 配置 ──
# HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=claude
# HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD=30m
```

密钥生成（缺失时）：
```bash
openssl rand -base64 32 | tr -d '\n'                # JWT 密钥
openssl rand -base64 32 | tr -d '/+=' | head -c 43  # Admin Token / API Key
```

保留现有有效值。仅填充缺失字段或更新用户明确要求修改的字段。

## 第十二步：部署为系统服务

安装完成后，推荐部署为系统服务以实现自动启动和后台运行：

```bash
# 用户级安装（无需 root）
hotplex service install

# 系统级安装（需要 sudo）
sudo hotplex service install --level system

# 启动服务
hotplex service start

# 查看状态
hotplex service status
```

服务管理器：Linux 用 systemd，macOS 用 launchd，Windows 用 SCM。

如果用户更倾向于开发模式，跳过此步，用 `make dev` 前台运行。

## 第十三步：验证

逐步验证安装结果：

```bash
# 1. 二进制可执行
hotplex version

# 2. 配置合法
hotplex config validate

# 3. 网关启动（开发模式）
make dev

# 4. 健康检查
curl http://localhost:9999/admin/health

# 5. 消息平台连通性 — 查看日志确认 WebSocket 连接
hotplex service logs -f
# 或开发模式下查看终端输出
```

展示最终配置摘要：

| 项目 | 值 |
|------|---|
| 版本 | vX.Y.Z |
| Slack Bot | xoxb-...xxxx（已验证） |
| Slack 用户 ID | U0XXXXX |
| Slack 工作目录 | /path/to/project |
| Slack Worker | claude_code |
| 飞书 App | cli_xxx（已验证） |
| 飞书用户 ID | ou_xxx |
| 飞书工作目录 | /path/to/project |
| 飞书 Worker | claude_code |
| 语音转文字 | Slack: local, 飞书: feishu+local |
| 访问策略 | allowlist |
| Agent 配置 | 已启用 (~/.hotplex/agent-configs/) |
| 服务模式 | systemd/launchd/SCM 或 make dev |

## 幂等重入

此 skill 设计为可重复运行：
- 跳过已有有效配置的步骤
- 仅重新处理用户想更新的部分
- Token 修改后总是重新验证
- 绝不重新生成已存在的密钥
