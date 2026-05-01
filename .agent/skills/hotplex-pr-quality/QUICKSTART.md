# HotPlex PR Quality Skill v2.0 - 快速开始

## 一分钟上手

### 使用条件（自动化检测）

这个 skill 会自动检测以下内容，**无需手动配置**：

- ✅ HotPlex Git 仓库
- ✅ `gh` CLI（GitHub CLI）
- ✅ 当前分支有未提交的修改
- ✅ Fork 远程仓库（任何名称）
- ✅ 你的 GitHub 用户名

### 最简单的使用方式

**场景 1：创建新 PR**
```
开发完成了，帮我创建 PR
```

AI 会自动：
1. ✅ 运行 `make test` 和 `make lint`（含跨平台检查）
2. ✅ 分析修改影响的架构组件（channel/worker/平台）
3. ✅ 生成符合 HotPlex 规范的 commit message（架构感知 scope）
4. ✅ 自动检测你的 fork 远程仓库并推送
5. ✅ 创建到 `hrygo/hotplex` 的 PR（标注架构影响）
6. ✅ 监控跨平台 CI 状态并智能处理失败

**场景 2：更新现有 PR**
```
我的 PR 还有 CI 失败，现在修复了，帮我推送
```

AI 会自动：
1. ✅ 检测现有 PR
2. ✅ 运行 `make test` 和 `make lint`
3. ✅ 生成 commit message
4. ✅ 推送代码（GitHub 自动更新 PR 并重新运行 CI）
5. ✅ 监控 CI 状态

### 预期结果

- ✅ 所有核心 CI 检查通过（Test/Build/ Coverage Check，三平台）
- ✅ PR 符合 HotPlex 项目规范（含架构影响标注）
- ⚠️ codecov 可能失败（会智能判断并说明）

## 核心价值

### 1. 自动化流程
- 不需要手动运行测试和 lint
- 不需要记住复杂的 commit message 格式
- 不需要手动分析架构影响
- 不需要手动监控 CI

### 2. 质量保证
- 确保代码符合 HotPlex 标准
- 确保跨平台测试通过
- 确保代码规范正确
- 自动标注架构影响

### 3. 智能处理
- 自动分析 CI 失败原因
- 区分必须修复和可接受的失败
- 提供修复建议或说明原因
- 跨平台失败智能分析

### 4. HotPlex 专用（架构感知）
- 硬编码 HotPlex 配置（`make test`、`make lint`）
- 符合 HotPlex commit 规范（Conventional Commits）
- 基于 HotPlex 项目实战经验的 codecov 决策树
- **架构感知**（channel/worker/平台分析）
- **跨平台监控**（Linux/macOS/Windows）

## HotPlex 架构特性

### 多 Channel 支持

- **Slack Socket Mode**: `internal/messaging/slack/`
- **飞书 WebSocket (P2)**: `internal/messaging/feishu/`
- **WebChat**: `webchat/`

### 多 Worker 支持

- **Claude Code (CC)**: `internal/worker/claudecode/`
- **OpenCode Server (OCS)**: `internal/worker/opencodeserver/`
- **Pi-mono**: `internal/worker/pi/`

### 跨平台兼容

- **Linux**: systemd，主要开发平台
- **macOS**: launchd，SIP 保护
- **Windows**: SCM，无 POSIX 信号

## 常见场景

### 场景 1：快速创建 PR
```
帮我创建 PR
```

### 场景 2：修复 CI 问题
```
CI 失败了，帮我修复
```

### 场景 3：跨平台测试失败
```
Windows CI 失败了，怎么办？
```

### 场景 4：增量推送更新现有 PR
```
我的 PR 还有 CI 失败，现在修复了，帮我推送
```

### 场景 5：分析 codecov
```
codecov 覆盖率不够，怎么办？
```

### 场景 6：完整流程
```
执行完整的 PR 创建和监控流程
```

## 关键特性

### ✅ 核心检查保证
- Test (Linux/macOS/Windows): 三平台测试通过
- Build (Linux/macOS/Windows): 三平台构建成功
- Coverage Check: 覆盖率检查通过
- Gate: Gate 检查通过

### ⚠️ Codecov 灵活处理
- 智能判断是否存在实质性障碍
- 评估 ROI（投入产出比）
- 在 PR 中说明原因
- 避免盲目追求覆盖率

### 📋 规范的文档
- Commit message 遵循 HotPlex 的 Conventional Commits 规范（架构感知 scope）
- PR 描述包含所有必要部分（架构影响标注）
- 关联相关 issues

### 🔧 自动检测 fork
- 支持任何 fork 远程仓库名称（`fork`、`origin`、`upstream` 等）
- 自动推断你的 GitHub 用户名
- 无需手动配置

### 🏗️ 架构感知
- 自动分析影响的 channel（`messaging/slack`、`messaging/feishu`、`webchat`）
- 自动分析影响的 worker（`worker/cc`、`worker/ocs`、`worker/pi`）
- 自动分析影响的平台（Linux/macOS/Windows）
- 在 commit message 和 PR 描述中标注

## 注意事项

### 1. 提交前检查
```bash
# 确保测试通过
make test

# 确保 lint 通过
make lint

# 确认修改的文件
git status
```

### 2. Fork 远程仓库
```bash
# 查看远程仓库
git remote -v

# skill 会自动检测，无需手动配置
```

### 3. GitHub CLI
```bash
# 认证
gh auth login

# 验证
gh auth status
```

### 4. 跨平台开发
```bash
# 使用 filepath.Join() 而非硬编码分隔符
path := filepath.Join("dir", "file")  // ✅
path := "dir/file"                     // ❌

# CI 会自动在 Linux/macOS/Windows 三平台运行
```

## 实战建议

### ✅ 推荐做法
1. 开发完成后立即使用 skill
2. 让 AI 自动处理整个流程
3. 信任 AI 的判断（基于 HotPlex 实战经验）
4. codecov 失败时听取 AI 的分析
5. 注意架构影响标注

### ❌ 避免做法
1. 跳过质量检查
2. 手动创建 PR（容易遗漏步骤）
3. 盲目追求 100% 覆盖率
4. 忽略实质性障碍
5. 忽略跨平台兼容性

## 相关文档

- [SKILL.md](SKILL.md) - 完整的 skill 文档（HotPlex 专用 + 架构感知）
- [CHEATSHEET.md](CHEATSHEET.md) - 快速参考
- [EXAMPLES.md](EXAMPLES.md) - 详细示例
- [README.md](README.md) - 使用说明

## 更新日志

### v2.0.0 (2026-05-01)
基于 HotPlex 项目实战经验创建，包含：
- 完整的 PR 创建流程（HotPlex 专用）
- CI 质量保证（`make test`、`make lint`，跨平台）
- Codecov 智能处理（HotPlex 特定决策树）
- 自动检测 fork 仓库（支持所有贡献者）
- **架构感知能力**（channel/worker/平台分析）
- **跨平台 CI 监控**（Linux/macOS/Windows）
- 错误修复流程
- 实战示例和技巧
