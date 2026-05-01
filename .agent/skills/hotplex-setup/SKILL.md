---
name: hotplex-setup
description: HotPlex 完整环境检查、依赖验证与安装引导。执行系统级环境检测（OS/架构/Go/Python/端口/权限）、引导式安装（二进制/源码）、配置生成与验证（消息平台/STT/Worker/资源限制）、服务部署与验证。**使用此 skill**：首次安装 HotPlex、环境检查、依赖缺失、配置消息平台、端口冲突、权限问题、服务启动失败、升级版本、迁移配置、添加白名单、获取用户 ID。支持 Linux/macOS/Windows 跨平台安装。
---

# HotPlex 环境检查与安装指引

从零开始完成 HotPlex 的环境检测、依赖安装、配置生成、服务部署和验证。整个流程是幂等的——重复运行时只处理缺失或需要更新的部分。

## 前置条件

**支持的系统**：Linux（x86_64/arm64）、macOS（x86_64/arm64）、Windows（amd64）

**必需依赖**：
- Go 1.26+（源码构建必需）
- Python 3.8+（STT 功能必需）
- Git（源码构建必需）

**可选依赖**：
- funasr-onnx + modelscope（本地 STT，详见 `references/stt.md`）
- SenseVoice Small 模型（约 900MB）

## 快速检查

```bash
# 快速验证环境
hotplex doctor
```

或手动检查关键依赖：

```bash
go version 2>/dev/null || echo "❌ Go 未安装"
python3 --version 2>/dev/null || echo "❌ Python3 未安装"
git --version 2>/dev/null || echo "❌ Git 未安装"
which hotplex 2>/dev/null && hotplex version || echo "❌ HotPlex 未安装"
```

## 安装流程

### 第一步：检测当前环境

运行环境检测并生成状态报告：

| 检查项 | 说明 |
|--------|------|
| 操作系统 | Linux/macOS/Windows + 架构 |
| Go 1.26+ | 源码构建必需 |
| Python 3.8+ | STT 功能必需 |
| STT 依赖 | funasr-onnx, modelscope, SenseVoice 模型 |
| 端口 8888/9999 | Gateway 和 Admin API |
| 写入权限 | ~/.hotplex 目录 |
| 系统服务 | systemd/launchd/SCM |

根据检测结果推荐安装方式：
- **已安装** → 跳到配置验证（第四步）
- **有 Go 1.26+** → 可选源码构建或二进制安装
- **无 Go** → 二进制安装（推荐）
- **依赖缺失** → 引导安装（详见 `references/dependencies.md`）

### 第二步：安装 HotPlex

**方式 A：一键二进制安装（推荐）**

macOS / Linux：
```bash
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest --prefix ~/.local
```

Windows (PowerShell)：
```powershell
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest
```

**方式 B：源码构建**

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # 检查工具链 + 构建 + 测试
```

**验证安装**：

```bash
hotplex version           # 应输出版本号
hotplex config validate   # 检查默认配置
```

### 第三步：配置 .env

1. **创建 .env**：`cp configs/env.example .env`
2. **生成密钥**：缺失时自动生成 JWT_SECRET 和 ADMIN_TOKEN_1
3. **配置消息平台**：收集 Slack/飞书 Token（见第四步）
4. **配置访问策略**：选择开放/白名单/仅私聊
5. **配置 STT**：选择本地或云端 STT（详见 `references/stt.md`）

**关键配置字段**：

```
# 必需密钥
HOTPLEX_JWT_SECRET=<生成或现有>
HOTPLEX_ADMIN_TOKEN_1=<生成或现有>

# Slack（如果使用）
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-...

# 飞书（如果使用）
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_xxx
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=<secret>

# 访问策略（推荐白名单）
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=<user_id>
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=<open_id>

# STT（可选）
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
```

### 第四步：验证 Token

**Slack**：
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/auth.test"
# 应返回 {"ok":true,...}
```

**飞书**：
```bash
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<app_id>","app_secret":"<app_secret>"}'
# 应返回 {"code":0,"tenant_access_token":"..."}
```

Token 无效时重新询问，不继续后续步骤。

### 第五步：获取用户 ID

自动拉取工作区用户 ID 供白名单配置：

**Slack**：列出真实用户（跳过 bot）
**飞书**：列出联系人

API 调用失败时提供手动查找指引：
- Slack：头像 → 三个点 → "Copy member ID"
- 飞书：管理后台 → 组织架构 → 找到 Open ID

### 第六步：配置工作目录（可选）

Worker 进程需要工作目录。默认：`~/.hotplex/workspace`

仅在用户指定非默认路径时设置：
```
HOTPLEX_MESSAGING_SLACK_WORK_DIR=/path/to/project
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=/path/to/project
```

### 第七步：配置 Worker 与 Agent

**Worker 类型**：
- `claude_code`（默认）— Claude Code CLI
- `opencode_server` — OpenCode Server（单例进程）
- `pi` — Pi-mono

```
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
```

**Agent 配置**：默认启用，从 `~/.hotplex/agent-configs/` 加载
```
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/
```

### 第八步：部署服务

**用户级服务（推荐，无需 root）**：
```bash
hotplex service install
hotplex service start
hotplex service status
```

**系统级服务（需要 root）**：
```bash
sudo hotplex service install --level system
sudo hotplex service start
```

**开发模式（跳过服务）**：
```bash
make dev
```

### 第九步：验证安装

```bash
# 1. 二进制可执行
hotplex version

# 2. 配置合法
hotplex config validate

# 3. 健康检查
curl http://localhost:9999/admin/health

# 4. 查看日志确认 WebSocket 连接
hotplex service logs -f
# 或开发模式下查看终端输出
```

**配置摘要**：

| 项目 | 值 |
|------|---|
| 版本 | vX.Y.Z |
| Slack Bot | xoxb-...（已验证） |
| Slack 用户 ID | U0XXXXX |
| 飞书 App | cli_xxx（已验证） |
| 飞书用户 ID | ou_xxx |
| STT | Slack: local, 飞书: feishu+local |
| 访问策略 | allowlist |
| Worker | claude_code |
| 服务模式 | systemd/launchd/SCM 或 make dev |

## 故障排查

### 常见问题

**端口冲突**：
```bash
netstat -tuln | grep -E ":(8888|9999)"
# 修改 config.yaml 中的 gateway.addr 或 admin.addr
```

**权限问题**：
```bash
chmod 755 ~/.hotplex
# 或使用用户级服务（无需 root）
hotplex service install
```

**服务启动失败**：
```bash
hotplex service logs -n 50
hotplex config validate
hotplex service restart
```

**消息平台连接失败**：
- 验证 Token（见第四步）
- 检查 Socket Mode 已启用（Slack）
- 检查事件订阅已配置（飞书）

**STT 问题**：
- 本地 STT：检查 funasr-onnx、modelscope、模型下载
- 云端 STT：申请飞书权限（https://open.feishu.cn/app/cli_a954eab23678dbb5/auth?q=speech_to_text:speech）

详细故障排查见 `references/troubleshooting.md`。

## 详细文档

- **依赖安装**：`references/dependencies.md` - Go/Python/STT 详细安装步骤
- **故障排查**：`references/troubleshooting.md` - 端口/权限/服务/连接问题
- **跨平台**：`references/cross-platform.md` - Linux/macOS/Windows 特定差异
- **STT 配置**：`references/stt.md` - 本地和云端 STT 完整指南

## 幂等重入

此 skill 设计为可重复运行：
- 跳过已有有效配置的步骤
- 仅重新处理用户想更新的部分
- 保留现有有效值（密钥、Token）

## 跨平台支持

**Linux**：systemd 用户级/系统级服务
**macOS**：launchd 用户级服务
**Windows**：SCM 服务（需要管理员权限）

详见 `references/cross-platform.md`。
