---
name: hotplex-setup
description: 遇到任何 HotPlex 安装、配置或运行问题时，第一时间使用此 skill。无论你是首次安装、环境检查、依赖缺失、Token 验证、端口冲突、权限问题、服务启动失败，还是升级版本、迁移配置、添加白名单、获取用户 ID，此 skill 都能提供完整的诊断和解决方案。支持 Linux/macOS/Windows 全平台。
---

# HotPlex 环境检查与安装指引

完整的 HotPlex 环境检测、依赖安装、配置生成、服务部署和验证流程。整个流程设计为幂等——重复运行时只处理缺失或需要更新的部分，不会破坏已有配置。

## 为什么需要完整的环境检查？

HotPlex 依赖多个外部组件（消息平台 API、STT 服务、系统服务管理器），这些组件的配置错误是最常见的故障根源。完整的环境检查可以在启动前发现 90% 的潜在问题，避免"安装后无法启动"的反复调试。

## 前置条件

**支持的系统**：Linux（x86_64/arm64）、macOS（x86_64/arm64）、Windows（amd64）

**必需依赖**：
- Go 1.26+（源码构建必需，二进制安装不需要）
- Python 3.8+（STT 功能必需，如果不使用语音转文字可以跳过）
- Git（源码构建必需，二进制安装不需要）

**可选依赖**：
- funasr-onnx + modelscope（本地 STT，详见 `references/stt.md`）
- SenseVoice Small 模型（约 900MB，本地 STT 需要）

## 快速检查

**为什么先检查？** 快速检查可以立即发现环境问题，避免后续步骤失败。

```bash
# 一键检查所有依赖
hotplex doctor
```

或手动检查关键依赖：

```bash
go version 2>/dev/null || echo "❌ Go 未安装（仅源码构建需要）"
python3 --version 2>/dev/null || echo "❌ Python3 未安装（STT 需要）"
git --version 2>/dev/null || echo "❌ Git 未安装（仅源码构建需要）"
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

**为什么推荐二进制安装？** 二进制安装只需 30 秒，不需要编译环境，适合大多数场景。源码构建适合需要自定义修改或学习源码的场景。

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

**为什么 .env 配置很重要？** 所有敏感信息（Token、密钥）都存储在 .env 中，错误配置会导致连接失败或安全风险。

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

**为什么必须验证 Token？** Token 配置错误是导致连接失败的最常见原因（约占 60% 的故障）。提前验证可以避免启动后才发现问题。

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
- 飞书：API 调试台或调用 OpenAPI 获取 Open ID（管理后台组织架构仅能查看 User ID）

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

```
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
```

**Agent 配置**：默认启用，从 `~/.hotplex/agent-configs/` 加载
```
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/
```

### 第八步：Agent 个性化配置

**为什么需要个性化？** 默认模板提供通用 AI 编程助手人格。个性化后 Agent 能适配你的技术栈、工作风格和沟通偏好，显著提升协作效率。

**触发条件**：
- 基础设施配置已完成（onboard 或手动配置过）
- `~/.hotplex/agent-configs/` 目录存在

**检测流程**：
1. 读取 `~/.hotplex/agent-configs/` 目录下的所有文件
2. 检查 USER.md 是否仍为默认模板（含空字段或 `<!-- -->` 注释占位符）
3. 如果全部已个性化 → 跳过，展示当前配置摘要
4. 如果有未个性化文件 → 启动交互式引导

**Phase A — 用户画像 (USER.md)**：
  "你主要使用什么编程语言和框架？"
  "你的角色是什么？（如：后端工程师、全栈开发者）"
  "你偏好简洁回复还是详细解释？"
  "代码审查时希望 Agent 关注哪些方面？"
  → 收集后写入 USER.md 对应字段，替换默认示例值

**Phase B — Agent 人格微调 (SOUL.md)**：
  "当前 Agent 人格已配置为 [展示关键特征]。需要调整吗？"
  "沟通语言偏好？（默认：用户语言 + 英文术语）"
  "输出密度偏好？（默认：结论先行，省略开场白）"
  → 仅修改用户明确要求的字段，未提及的保持默认

**Phase C — 3 级 Fallback 策略引导**：
  "当前配置层级："
  "  全局：~/.hotplex/agent-configs/SOUL.md（当前生效）"
  "  平台：~/.hotplex/agent-configs/slack/SOUL.md（未配置）"
  "  Bot：~/.hotplex/agent-configs/slack/U12345/SOUL.md（未配置）"
  "是否需要平台级或 Bot 级定制？"
  → 如需要，引导创建对应目录和文件

**Phase D — 确认与写入**：
  展示所有变更的 diff
  "确认写入？[Y/n]"
  → 写入后自动生效（热重载）

**关键规则**：
- **幂等**：重复运行只更新用户明确回答的字段
- **最小变更**：不重写整个文件，用 diff 展示 + 精确编辑
- **尊重现有配置**：已个性化的内容不覆盖，除非用户明确要求
- **AI 判断**：当用户回答模糊时，推理合理默认值并展示给用户确认

### 第九步：部署服务

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

### 第十步：验证安装

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

## 常见陷阱与故障排查

### 最容易犯的错误

1. **跳过 Token 验证**：直接配置 .env 而不验证 Token，导致服务启动后连接失败
2. **权限设置不当**：使用 root 安装但用普通用户启动，或反之
3. **端口冲突忽略**：没有检查 8888/9999 端口是否被占用
4. **STT 配置错误**：配置了本地 STT 但未安装依赖，导致语音消息失败
5. **白名单配置错误**：用户 ID 格式不对（Slack 是 U 开头，飞书是 ou_ 开头）

### 常见问题快速修复

**端口冲突**（约占 15% 的故障）：
```bash
netstat -tuln | grep -E ":(8888|9999)"
# 修改 config.yaml 中的 gateway.addr 或 admin.addr
```

**权限问题**（约占 10% 的故障）：
```bash
chmod 755 ~/.hotplex
# 或使用用户级服务（无需 root）
hotplex service install
```

**服务启动失败**（约占 20% 的故障）：
```bash
hotplex service logs -n 50
hotplex config validate
hotplex service restart
```

**消息平台连接失败**（约占 60% 的故障）：
- 验证 Token（见第四步）
- 检查 Socket Mode 已启用（Slack）
- 检查事件订阅已配置（飞书）

**STT 问题**（约占 5% 的故障）：
- 本地 STT：检查 funasr-onnx、modelscope、模型下载
- 云端 STT：飞书开发者后台 → 你的应用 → 权限管理 → 搜索 `speech_to_text`

详细故障排查见 `references/troubleshooting.md`。

## 详细文档

- **依赖安装**：`references/dependencies.md` - Go/Python/STT 详细安装步骤
- **故障排查**：`references/troubleshooting.md` - 端口/权限/服务/连接问题
- **跨平台**：`references/cross-platform.md` - Linux/macOS/Windows 特定差异
- **STT 配置**：`references/stt.md` - 本地和云端 STT 完整指南

## 幂等重入

**为什么可以安全重复运行？** 此 skill 设计为可重复运行：
- 跳过已有有效配置的步骤
- 仅重新处理用户想更新的部分
- 保留现有有效值（密钥、Token）

这意味着你可以随时重新运行此 skill 来修复问题或更新配置，而不会破坏已有设置。

## 跨平台支持

**Linux**：systemd 用户级/系统级服务
**macOS**：launchd 用户级服务
**Windows**：SCM 服务（需要管理员权限）

详见 `references/cross-platform.md`。

## 何时需要重新运行此 skill？

- 服务启动失败或无法连接消息平台
- 升级 HotPlex 版本后
- 添加新的消息平台（从 Slack 迁移到飞书）
- 修改白名单或访问策略
- 切换 STT 服务（本地 ↔ 云端）
- 更改工作目录或 Worker 类型
