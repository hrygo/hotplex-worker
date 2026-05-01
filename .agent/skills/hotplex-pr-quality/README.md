# HotPlex PR Quality Assurance Skill v2.0

## 概述

这个 skill 是 **HotPlex 项目专用**的 PR 质量助手，用于在开发完成后创建高质量的 Pull Request，并确保所有 CI 检查通过。

**HotPlex 架构特性**：
- **多 Channel 支持**: Slack Socket Mode、飞书 WebSocket (P2)、WebChat
- **多 Worker 支持**: Claude Code (CC)、OpenCode Server (OCS)、Pi-mono
- **跨平台兼容**: Linux、macOS、Windows 三平台 CI 必须通过

这个 skill 从 HotPlex 项目实战经验中提炼，深度理解 HotPlex 的架构特性，提供专业级的 PR 质量保证。

## 核心定位

### ✅ HotPlex 项目专用（架构感知）

这个 skill 硬编码了 HotPlex 项目的特定配置：
- 测试命令：`make test`（含跨平台测试）
- Lint 命令：`make lint`（含跨平台检查）
- Upstream 仓库：`hrygo/hotplex`
- Commit 规范：Conventional Commits（架构感知 scope）
- Codecov 决策树：HotPlex 项目特定

**架构感知能力**：
- 自动分析影响的 channel（Slack/飞书/WebChat）
- 自动分析影响的 worker（CC/OCS/Pi）
- 自动分析影响的平台（Linux/macOS/Windows）
- 在 commit message 和 PR 描述中标注架构影响

### ✅ 支持所有贡献者

虽然项目专用，但 skill 支持所有 HotPlex 贡献者：
- 自动检测 fork 远程仓库名称（`fork`、`origin`、`upstream` 等）
- 自动推断你的 GitHub 用户名
- 自动推断 PR 参数

## 核心改进（v2.0）

### 1. ✅ 泛化 fork 仓库检测

**问题**：v1.0 假设 fork 远程仓库名称为 `fork`
**解决**：自动检测任何名称的 fork 远程仓库

**为什么重要？**
每个贡献者的配置可能不同（`fork`、`origin`、`upstream` 等），自动检测确保 skill 适用于所有贡献者。

### 2. ✅ 泛化 fork 用户名检测

**问题**：v1.0 假设用户名为 `aaronwong1989`
**解决**：从 fork URL 自动推断任何用户名

**为什么重要？**
让 skill 适用于所有贡献者，而不需要手动配置。

### 3. ✅ 增强 HotPlex 架构感知

**新增**：
- 自动分析影响的 channel（`messaging/slack`、`messaging/feishu`、`webchat`）
- 自动分析影响的 worker（`worker/cc`、`worker/ocs`、`worker/pi`）
- 自动分析影响的平台（Linux/macOS/Windows）
- 在 commit message 中使用架构感知的 scope
- 在 PR 描述中标注架构影响

**为什么重要？**
HotPlex 是分布式系统，修改可能影响多个架构组件。标注架构影响帮助团队快速理解变更范围。

### 4. ✅ 保持 HotPlex 专用配置

**不变**：
- 测试命令：`make test`
- Lint 命令：`make lint`
- Upstream 仓库：`hrygo/hotplex`
- Commit 规范：Conventional Commits
- Codecov 决策树：HotPlex 特定

**为什么保持？**
HotPlex 项目有特定的配置和规范，硬编码这些确保精准匹配，提供专业级支持。

## 使用方式

### 最简单的方式

```
开发完成了，帮我创建 PR
```

AI 会自动：
1. 运行 `make test` 和 `make lint`（含跨平台检查）
2. 分析修改影响的架构组件（channel/worker/平台）
3. 生成符合 HotPlex 规范的 commit message（架构感知 scope）
4. 检测你的 fork 远程仓库并推送
5. 创建到 `hrygo/hotplex` 的 PR（标注架构影响）
6. 监控 CI 并智能处理失败（跨平台测试）

### 其他常见场景

```
提交代码
创建 PR
PR review
CI 失败了
跨平台测试失败
codecov 覆盖率不够
测试失败
lint 错误
分支管理
合并代码
```

## 适用范围

### ✅ 项目范围

**HotPlex 项目专用**：
- 仓库：`hrygo/hotplex`
- 测试：`make test`（跨平台）
- Lint：`make lint`（跨平台检查）
- Commit 规范：Conventional Commits（架构感知）
- 架构：多 channel、多 worker、跨平台

### ✅ 贡献者范围

**所有 HotPlex 贡献者**：
- 任何 GitHub 用户名
- 任何 fork 远程仓库名称（`fork`、`origin`、`upstream` 等）
- 任何分支名称

## 核心特性

### 1. 自动检测 fork 配置

**Fork 远程仓库**（自动检测）：
```bash
# 标准配置
origin    https://github.com/<your-username>/hotplex.git
upstream  https://github.com/hrygo/hotplex.git

# 其他配置
fork      https://github.com/<your-username>/hotplex.git
origin    https://github.com/hrygo/hotplex.git
```

skill 会自动检测并适配。

### 2. 架构感知的 commit message

- 自动根据修改位置推断 scope
  - Gateway 核心：`gateway`、`session`、`config`、`security`
  - Channel：`messaging/slack`、`messaging/feishu`、`webchat`
  - Worker：`worker/cc`、`worker/ocs`、`worker/pi`
  - 平台：`cli`、`service`、`build`
- 生成中文描述（技术术语英文）
- 包含 Co-Authored-By 声明
- 关联相关 issues

**示例**：
```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85
Fixes #79

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

### 3. 智能 PR 参数推断

- `--repo`: 固定为 `hrygo/hotplex`
- `--head`: 从你的 fork 用户名和分支推断
- `--title`: 使用 commit subject
- `--body`: 使用生成的描述（含架构影响标注）

### 4. 智能 codecov 处理

- 基于 HotPlex 项目实战经验的决策树
- 自动分析失败原因
- 判断是否存在实质性障碍
- 评估 ROI（投入产出比）
- 在 PR 中说明或修复

**实质性障碍**（可接受的失败原因）：
- 需要真实外部服务（HTTP server、数据库）
- Mock 测试成本过高
- 集成测试不稳定
- 跨平台测试难以在 CI 进行
- ROI 低（投入 > 1 小时，提升 < 5%）

### 5. 跨平台 CI 监控

- 监控 Linux/macOS/Windows 三平台 CI
- 分析平台特定失败
- 提供跨平台修复建议

## HotPlex 架构概览

### 多 Channel 支持

HotPlex 支持多种消息 channel：

| Channel | 目录 | 特性 |
|---------|------|------|
| **Slack Socket Mode** | `internal/messaging/slack/` | 流式消息、打字提示、交互按钮 |
| **飞书 WebSocket (P2)** | `internal/messaging/feishu/` | 流式卡片、STT 语音转文字、交互卡片 |
| **WebChat** | `webchat/` | HTTP/SSE、session 粘性、localStorage 持久化 |

### 多 Worker 支持

HotPlex 支持多种 AI worker 运行时：

| Worker | 目录 | 特性 |
|--------|------|------|
| **Claude Code (CC)** | `internal/worker/claudecode/` | Claude Code CLI、Agent 配置注入 |
| **OpenCode Server (OCS)** | `internal/worker/opencodeserver/` | 单例进程管理器、SSE 长连接、session 池化 |
| **Pi-mono** | `internal/worker/pi/` | Pi-mono CLI |

### 跨平台兼容

HotPlex 支持三大平台：

| 平台 | CI | 系统服务 | 特殊注意 |
|------|-----|----------|----------|
| **Linux** | GitHub Actions | systemd | 主要开发平台 |
| **macOS** | GitHub Actions | launchd | SIP 保护 `/System` |
| **Windows** | GitHub Actions | SCM | 无 POSIX 信号 |

**跨平台注意事项**：
- 路径分隔符：使用 `filepath.Join()`，禁止硬编码 `/` 或 `\`
- 进程管理：POSIX 用 `syscall.Setpgid`，Windows 用 `Job Object`
- 系统服务：systemd (Linux) / launchd (macOS) / SCM (Windows)
- 文件权限：使用 `os.FileMode` 常量，避免平台特定权限

## 与 v1.0 的对比

| 特性 | v1.0 | v2.0 |
|------|------|------|
| Fork 仓库名称 | 硬编码 `fork` | 自动检测 |
| Fork 用户名 | 硬编码 `aaronwong1989` | 自动推断 |
| 测试命令 | `make test` | `make test`（不变） |
| Lint 命令 | `make lint` | `make lint`（不变） |
| Upstream 仓库 | `hrygo/hotplex` | `hrygo/hotplex`（不变） |
| 项目定位 | HotPlex | HotPlex（不变） |
| Commit 规范 | Conventional Commits | Conventional Commits（不变） |
| **架构感知** | ❌ 无 | ✅ 新增 |
| **跨平台分析** | ❌ 无 | ✅ 新增 |
| **Channel/Worker 标注** | ❌ 无 | ✅ 新增 |

## 实战价值

### 时间节省

- **v1.0**: 节省 66% 时间（15-20 分钟 → 5 分钟）
- **v2.0**: 进一步节省，减少配置时间，增加架构分析

### 质量提升

- ✅ 零遗漏：自动化检查所有步骤（含跨平台）
- ✅ 零错误：遵循 HotPlex 规范（含架构标注）
- ✅ 零配置：自动检测 fork 仓库
- ✅ 架构感知：自动标注影响的 channel/worker/平台

### 适用范围

- **v1.0**: HotPlex 项目，fork 仓库名称为 `fork`，用户名为 `aaronwong1989`
- **v2.0**: HotPlex 项目，所有贡献者，任何 fork 配置，架构感知

## 文档结构

```
~/.agents/skills/pr-quality/
├── SKILL.md          (v2.0) - 完整的 skill 规范（HotPlex 专用 + 架构感知）
├── README.md         (v2.0) - 使用说明（本文件）
├── CHEATSHEET.md     - 快速参考卡片
├── EXAMPLES.md       - 详细使用示例
└── QUICKSTART.md     - 一分钟上手指南
```

## 下一步

### 立即使用

```
开发完成了，帮我创建 PR
```

### 学习更多

1. **快速开始**（1 分钟）
   ```bash
   cat ~/.agents/skills/pr-quality/QUICKSTART.md
   ```

2. **查看示例**（15 分钟）
   ```bash
   cat ~/.agents/skills/pr-quality/EXAMPLES.md
   ```

3. **深入了解**（30 分钟）
   ```bash
   cat ~/.agents/skills/pr-quality/SKILL.md
   ```

## 更新日志

### v2.0.0 (2026-05-01) - 重大更新

**核心改进**：
- ✅ 泛化 fork 仓库名称检测（支持任何名称）
- ✅ 泛化 fork 用户名检测（支持任何用户）
- ✅ **新增架构感知能力**（channel/worker/平台分析）
- ✅ **新增跨平台 CI 监控**
- ✅ **新增架构影响标注**（commit message + PR 描述）
- ✅ 保持 HotPlex 专用配置（`make test`、`make lint`、`hrygo/hotplex`）
- ✅ 明确定位为 HotPlex 专家级 skill

**破坏性变更**：
- 不再硬编码 fork 仓库名称为 `fork`
- 不再硬编码用户名为 `aaronwong1989`
- 明确定位为 HotPlex 专用（不再声称通用）

### v1.0.0 (2026-05-01)

- 初始版本，基于 HotPlex 项目实战经验
- 硬编码 fork 仓库名称为 `fork`
- 硬编码用户名为 `aaronwong1989`

## 贡献

如果你使用这个 skill 并有改进建议，欢迎反馈！

**改进方向**：
- 优化 codecov 决策树
- 添加更多 commit scope 推断规则
- 改进跨平台错误处理和修复建议
- 增强架构影响分析

## 许可证

MIT License - 自由使用和修改
