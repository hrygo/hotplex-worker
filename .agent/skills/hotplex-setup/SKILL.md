---
name: hotplex-setup
description: HotPlex 环境检查、安装、配置、故障排查的完整指引。遇到以下场景时使用此 skill：首次安装 HotPlex、运行 hotplex doctor、依赖缺失（Go/Python/ffmpeg/STT/TTS）、Token 验证（Slack/飞书）、端口冲突、权限问题、服务启动失败、STT/TTS/MOSS 模型配置、Agent 个性化、升级版本、迁移配置。也适用于用户提到 hotplex + 安装/配置/环境/启动/报错/检查/doctor/onboard/服务/模型 等关键词。支持 Linux/macOS/Windows 全平台。即使用户只是问"我的 hotplex 怎么了"、"TTS 不工作"、"MOSS 语音合成失败"或"环境怎么配"，也应使用此 skill 来诊断和修复。
---

# HotPlex 环境检查与安装指引

本 skill 使用 `hotplex doctor` 作为诊断核心，`hotplex onboard` 作为首次安装向导。整个流程幂等——重复运行只处理缺失或需要更新的部分。

## 核心流程：诊断驱动

```
用户请求 → 构建安装二进制 → hotplex doctor --json → 解析报告 → 分支处理
```

不要手动逐项检查依赖——`hotplex doctor` 集成了 25 个 checker（8 个 category），覆盖环境、配置、依赖、安全、运行时、消息平台、STT、TTS、Agent 配置。先让它跑，你再根据报告行动。

### 第零步：构建安装二进制

如果当前目录是 HotPlex 源码仓库（存在 `Makefile` 和 `go.mod`），**必须先编译安装最新二进制**，确保 doctor 使用最新代码。

```bash
make build
cp bin/hotplex-$(go env GOOS)-$(go env GOARCH) ~/.local/bin/hotplex
hotplex version
```

如果不在源码目录（CI 或生产环境），跳过此步骤。

### 第一步：运行诊断

```bash
hotplex doctor --json
```

**JSON 报告结构**：

```json
{
  "version": "vX.Y.Z",
  "timestamp": "RFC3339",
  "summary": { "pass": N, "warn": N, "fail": N },
  "diagnostics": [
    {
      "name": "category.check_name",
      "category": "category",
      "status": "pass|warn|fail",
      "message": "描述",
      "fix_hint": "修复建议"
    }
  ]
}
```

**Exit codes**：0 = 全部通过（含 warning） | 1 = 有失败项 | 3 = 自动修复失败

### 第二步：根据 summary 分支

| summary 状态 | 动作 |
|-------------|------|
| `fail: 0, warn: 0` | 全部就绪，跳到 [验证安装](#验证安装) |
| `fail: 0, warn: N` | 警告项可忽略，展示给用户自行判断 |
| `fail: N` | 按分类处理失败项（见下方） |
| **hotplex 未安装** | 跳到 [安装 HotPlex](#安装-hotplex) |

### 第三步：按分类处理失败项

对 `diagnostics` 中每个 `status: "fail"` 的项，按 category 查找对应处理方式：

#### environment（环境）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `go_version` | Go < 1.26 或未安装 | 源码构建需要 Go 1.26+；二进制安装不需要 |
| `os_arch` | 不支持的 OS/架构 | 仅支持 linux/macOS/windows + amd64/arm64 |
| `build_tools` | golangci-lint/goimports 缺失 | 仅开发时需要，运行时不影响 |

#### config（配置）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `exists` | config.yaml 不存在 | 运行 `hotplex onboard` 生成 |
| `syntax` | YAML 解析错误 | 检查缩进和语法，参考 `configs/config.yaml` |
| `required` | JWT secret 缺失或无平台启用 | 运行 `hotplex onboard` 或手动设置 |
| `values` | 端口无效或数据目录不存在 | 创建目录或修改端口配置 |
| `env_vars` | JWT_SECRET/ADMIN_TOKEN 未设置 | 在 `.env` 中添加 |

#### dependencies（依赖）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `worker_binary` | claude/opencode 不在 PATH | 安装 Claude Code CLI 或设置 `HOTPLEX_WORKER_CLAUDE_CODE_COMMAND` |
| `sqlite_path` | 数据目录不存在或无写权限 | `mkdir -p ~/.hotplex/data && chmod 755 ~/.hotplex` |

#### security（安全）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `jwt_strength` | JWT secret 太短或低熵 | `openssl rand -base64 48` 重新生成 |
| `admin_token` | Token 为空或弱默认值 | 替换为强随机值 |
| `file_permissions` | 配置文件权限过宽 | `chmod 600 ~/.hotplex/.env ~/.hotplex/config.yaml` |
| `env_in_git` | .env 被 git 追踪 | `git rm --cached .env` |

#### runtime（运行时）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `disk_space` | 可用空间 < 100MB | 清理磁盘空间 |
| `port_available` | 8888 或 9999 被占用 | 停止占用进程或修改端口配置 |
| `orphan_pids` | 残留 PID 文件 | `rm ~/.hotplex/.pids/*.pid` |
| `data_dir_writable` | 数据目录不可写 | `chmod 755 ~/.hotplex/data` |

#### messaging（消息平台）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `slack_creds` | Token 格式错误 | Bot Token 以 `xoxb-` 开头，App Token 以 `xapp-` 开头 |
| `feishu_creds` | App ID/Secret 为空 | 检查飞书开放平台应用凭据 |

**在线验证 Token**：

```bash
# Slack
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/auth.test"
# 飞书
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<id>","app_secret":"<secret>"}'
```

#### stt（语音转文字）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `stt.runtime` | python3/funasr-onnx/ffmpeg 缺失 | 详见 `references/stt.md` |

#### tts（文字转语音）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `tts.runtime` | ffmpeg/python3 缺失或 MOSS 模型目录不存在 | Edge TTS 仅需 ffmpeg。MOSS 额外需要 python3 + 模型。详见 `references/tts.md` |

#### agent_config（Agent 配置）

| checker | 失败原因 | 处理 |
|---------|---------|------|
| `suffix_deprecated` | 使用了废弃的平台后缀文件 | 重命名为子目录格式 |
| `directory_structure` | 平台子目录含非标准文件 | 移除非标准文件 |
| `global_files` | 全局级配置影响所有 Bot | 考虑使用平台级/Bot 级配置 |

### 第四步：修复后重新验证

```bash
hotplex doctor --json
```

直到 `summary.fail == 0`。

### 第五步：环境初始化（按需）

如果 doctor 报告 `fail` 或 `warn`，按以下顺序修复。每项修复后运行 `hotplex doctor` 验证。

| 步骤 | 条件 | 依赖 | 安装指引 |
|------|------|------|---------|
| 5.1 ffmpeg | TTS/STT 启用时必需 | — | `references/dependencies.md` 的 ffmpeg 章节 |
| 5.2 Python 3.10+ | STT 或 MOSS-TTS 启用时必需 | — | `references/dependencies.md` 的 Python 章节 |
| 5.3 STT 依赖 | `stt_provider` 含 `local` | ffmpeg + Python | `references/stt.md` |
| 5.4 MOSS-TTS-Nano | `tts_provider` 含 `moss` | ffmpeg + Python | `references/tts.md` 的"MOSS-TTS-Nano 完整安装"章节 |

**注意**：Edge TTS（默认）仅需 ffmpeg，无需本地模型。MOSS-TTS-Nano 约 3GB 磁盘空间。

修复完成后重新运行诊断：

```bash
hotplex doctor --json
```

重复直到 `summary.fail == 0`。

---

## 安装 HotPlex

如果 `hotplex` 命令不存在：

### 方式 A：使用 onboard 向导（推荐首次安装）

```bash
# 1. 安装二进制
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest --prefix ~/.local
# Windows (PowerShell)
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest

# 2. 运行交互式向导
hotplex onboard

# 3. 验证
hotplex doctor
```

`hotplex onboard` 自动处理：Go/OS/磁盘检查、JWT/Token 生成、Slack/飞书配置、Worker 选择、config.yaml/.env 生成、Agent 配置模板、STT/TTS 检查、系统服务安装。

**非交互模式**（CI/自动化）：
```bash
hotplex onboard --non-interactive --enable-slack --slack-allow-from U0XXXXX --slack-dm-policy allowlist
```

### 方式 B：源码构建

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # check-tools + build + test-short
```

---

## Agent 个性化配置

**触发条件**：基础设施已配置，用户想定制 Agent 行为。
**前提**：`~/.hotplex/agent-configs/` 目录存在（`hotplex onboard` 自动创建）。

### 检测流程

1. 读取 `~/.hotplex/agent-configs/` 下的文件
2. 检查 USER.md 是否仍为默认模板（含空字段或 `<!-- -->` 占位符）
3. 全部已个性化 → 展示配置摘要
4. 有未个性化文件 → 启动交互式引导

### Phase A — 用户画像 (USER.md)

询问：
- "你主要使用什么编程语言和框架？"
- "你的角色是什么？（如：后端工程师、全栈开发者）"
- "你偏好简洁回复还是详细解释？"
- "代码审查时希望 Agent 关注哪些方面？"

收集后写入 USER.md 对应字段，替换默认示例值。

### Phase B — Agent 人格微调 (SOUL.md)

展示当前关键特征，询问是否需要调整：
- 沟通语言偏好（默认：用户语言 + 英文术语）
- 输出密度偏好（默认：结论先行，省略开场白）

仅修改用户明确要求的字段。

### Phase C — 3 级 Fallback 策略引导

展示配置层级：全局 → 平台（slack/）→ Bot（slack/U12345/），询问是否需要平台级或 Bot 级定制。

### Phase D — 确认与写入

展示所有变更的 diff，确认后写入。

**规则**：幂等（重复运行只更新明确回答的字段）、最小变更（用 diff 展示 + 精确编辑）、尊重现有配置（不覆盖已个性化内容）。

---

## 部署服务

```bash
hotplex service install          # 用户级（推荐，无需 root）
hotplex service start
hotplex service status

sudo hotplex service install --level system  # 系统级（需要 root）
```

**平台映射**：Linux → systemd | macOS → launchd | Windows → SCM

---

## 验证安装

```bash
hotplex version                  # 二进制可执行
hotplex config validate          # 配置合法
hotplex doctor                   # 完整健康检查
curl http://localhost:9999/admin/health  # Admin API
hotplex service logs -f          # 日志确认连接
```

**配置摘要**（展示给用户确认）：

| 项目 | 值 |
|------|---|
| 版本 | vX.Y.Z |
| 消息平台 | Slack: xoxb-.../飞书: cli_xxx |
| 访问策略 | allowlist |
| Worker | claude_code |
| STT | local / feishu+local |
| TTS | enabled / disabled |
| 服务模式 | systemd/launchd/SCM/dev |

---

## 详细文档

| 文档 | 内容 | 何时查阅 |
|------|------|---------|
| `references/dependencies.md` | Go/Python/ffmpeg 详细安装命令 | doctor 报告依赖缺失 |
| `references/stt.md` | STT 完整配置（本地/云端） | 语音转文字相关 |
| `references/tts.md` | TTS 完整配置（Edge/MOSS/ffmpeg） | 语音回复相关 |
| `references/troubleshooting.md` | 端口/权限/服务/连接问题排查 | 服务启动或连接失败 |
| `references/cross-platform.md` | Linux/macOS/Windows 差异 | 跨平台部署 |

## 何时重新运行此 skill？

- 服务启动失败或无法连接消息平台
- 升级 HotPlex 版本后
- 添加新的消息平台或切换 STT/TTS 配置
- 首次安装后验证所有依赖和模型就绪

## 版本升级

使用 `/hotplex-update` skill 执行升级（跨平台构建、安装、服务重启、回滚）。升级后建议重新运行本 skill 验证环境完整性。
