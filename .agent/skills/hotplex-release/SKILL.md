---
name: hotplex-release
description: HotPlex Worker Gateway 标准化发布流程。版本号确定、自动化变更收集、Changelog 撰写、版本统一、验证、打标签、GitHub Release。**使用此 skill**：发布新版本、创建 GitHub Release、版本号管理、Changelog 生成、变更收集、版本验证。支持自动化版本发布和完整的变更记录。
---

# HotPlex 发布工作流

## 前置条件

- `gh` CLI 已认证并有 repo 访问权限
- 已安装 `make` 和 `go` 1.26+
- 所有测试通过 (`make check`)
- 工作目录干净（无未提交的更改）

## 分支保护

**标签和 GitHub Release 必须只在 `main` 分支上创建。**

1. 在工作流开始时，检查当前分支：
   ```bash
   git branch --show-current
   ```
2. **如果在 `main` 上**：执行完整工作流（步骤 1–8），包括创建 tag 和 release。
3. **如果不在 `main` 上**（feature 分支、release prep 分支等）：仅执行步骤 1–5（版本确定、变更收集、changelog 撰写、版本统一、验证）。然后：
   - 将版本 bump + changelog 作为**准备提交**提交（例如 `chore: prepare release vX.X.X`）。
   - **不要**创建 git tag。
   - **不要**推送 tag 或触发 GitHub Release。
   - 通知用户："Release preparation committed on `<branch>`。Tag and publish after merging to main."
4. **合并到 main 后**：fast-forward 或 checkout main，然后只执行步骤 6（tag）和步骤 7（推送 tag + GitHub Release）。

## 步骤 1：确定下一个版本

从 `cmd/hotplex/main.go:16`（`version` 变量）读取当前版本。

应用 [语义化版本](https://semver.org/)：
- **Patch** (`v1.1.0` → `v1.1.1`)：Bug 修复、安全补丁、无新功能
- **Minor** (`v1.1.0` → `v1.2.0`)：新功能、向后兼容的更改
- **Major** (`v1.1.0` → `v2.0.0`)：破坏性更改

在继续之前与用户确认新版本。

## 步骤 2：收集变更

运行以下命令以收集自上次发布以来的所有变更：

```bash
# 获取最后一个 release tag
LAST_TAG=$(git tag --sort=-version:refname | head -1)

# 收集 conventional commit 摘要（按类型分组）
echo "=== Changes since ${LAST_TAG} ==="
git log --oneline "${LAST_TAG}..HEAD" --no-merges

echo ""
echo "=== By Category ==="
echo "--- feat (Added) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^feat'
echo ""
echo "--- fix (Fixed) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^fix'
echo ""
echo "--- refactor / perf (Changed) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^refactor\|^perf'
echo ""
echo "--- chore / ci / docs (Infrastructure) ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --grep='^chore\|^ci\|^docs\|^build'
echo ""
echo "--- Other ---"
git log --oneline "${LAST_TAG}..HEAD" --no-merges --invert-grep --grep='^feat\|^fix\|^refactor\|^perf\|^chore\|^ci\|^docs\|^build'

# 用于详细审查特定更改
echo ""
echo "=== Full diffstat ==="
git diff --stat "${LAST_TAG}..HEAD"
```

在需要时审查每个 commit 的完整消息以获取上下文：

```bash
git log "${LAST_TAG}..HEAD" --no-merges --format="%h %s%n%b---"
```

### 范围分类映射

按其 scope 将 commit 分组到 changelog 部分：

| Conventional Commit | Changelog Section |
|:---|:---|
| `feat(...)` | **Added** |
| `fix(...)` | **Fixed** |
| `refactor(...)`, `perf(...)` | **Changed** |
| `chore(deps)`, `build(...)` | **Changed**（或 **Dependencies** 如果仅是 dep bump） |
| feat 带 breaking change 后缀或 BREAKING CHANGE footer | **Changed** + callout |
| `ci(...)`, `docs(...)` | 从 changelog 省略，除非面向用户 |

### Scope → 显示组映射

编写 changelog 条目时，按功能区域分组：

| Commit Scope | Display Group |
|:---|:---|
| `gateway`, `session`, `hub`, `conn` | **Gateway Core** |
| `worker`, `claude-code`, `opencode`, `pi` | **Worker** |
| `slack`, `feishu`, `messaging`, `stt` | **Messaging** |
| `webchat`, `ui`, `chat` | **WebChat UI** |
| `config`, `agent-config` | **Configuration** |
| `security`, `jwt`, `ssrf` | **Security** |
| `cli`, `onboard`, `doctor` | **CLI** |
| `client`, `sdk`, `ts`, `python`, `java` | **SDK** |
| `test`, `ci`, `build`, `makefile` | **Infrastructure** |

## 步骤 3：撰写 Changelog

按照 [Keep a Changelog](https://keepachangelog.com/) 格式更新 `CHANGELOG.md`。

### 模板

```markdown
## [X.X.X] - YYYY-MM-DD

### Summary

1-3 句话概括本版本的核心主题和最重要变更。
- 提及版本定位（patch/minor/major）
- 点出 2-3 个最关键的 feature 或 fix
- 说明影响面（哪些模块受益，用户可感知的变化）

### Added

- **Display Group**: One-line description of the change. (#PR or commit SHA for significant changes)

### Changed

- **Display Group**: Description of what changed and why.

### Fixed

- **Display Group**: Description of what was broken and how it was fixed.

### Security

- Description of security-relevant changes (omit section if none).
```

### 撰写规则

1. **Summary 必须有** — 不能只有三段式。Summary 是面向用户的版本叙事，帮助读者在 10 秒内判断是否与自己相关
2. **Summary 写法**: 先说版本定位，再说核心变化，最后说影响面。用自然语言而非条目列表
3. **每个逻辑更改一个条目**，而不是每个 commit — 将相关的 commit 合并为一个条目
4. **现在时态，祈使语气**："Add feature" 而非 "Added feature" 或 "Adds feature"
5. **在每个条目开头加粗显示组**以便扫描
6. **仅包含 PR 号或 commit SHA**用于重要/面向用户的更改
7. **省略**内部重构、CI 更改和仅文档更新，除非面向用户
8. **合并小修复**到一个 "minor fixes" 条目（如果单独不重要）
9. **按影响排序**各部分中的条目（最重要的在前）

### 示例条目

```markdown
## [1.2.0] - 2026-04-30

### Summary

v1.2.0 是一次 minor 版本更新，聚焦于 **可观测性与运维体验**。新增 Session Stats API 和 Conversation Store，
为 WebChat 和管理端提供会话级别的 token/延迟/成本统计。WebChat 经历了全面 UX 重构（暗色主题 + GenUI 工具组件 +
CommandMenu），Gateway Core 获得了连接稳定性修复（CAS race guard、fast reconnect、session ID 一致性）。

### Added

- **Gateway Core**: Session stats API — aggregated turn statistics from done events (`GET /api/sessions/{id}/stats`).
- **Session**: Conversation store — async batch writer for turn-level persistence (user input + assistant response with tools, tokens, cost, duration).
- **WebChat UI**: "Obsidian" dark theme with glassmorphism design system, GenUI tool rendering, and slash command palette.

### Changed

- **Session**: SQLite storage optimization — PRAGMA tuning, cascade delete, events TTL cleanup, automatic VACUUM.
- **Gateway Core**: Fast reconnect for idle sessions — skip terminate+resume cycle when worker is still alive.

### Fixed

- **Gateway Core**: ClaudeCode mapper silently discarded `EventSystem` and `EventSessionState` — payload type mismatch caused all state transitions to be dropped.
- **WebChat UI**: Connection stability — deterministic session IDs across REST/WS paths, browser console warnings eliminated.
```

## 步骤 4：版本统一

更新以下所有位置的版本字符串。对所有位置使用 semver 格式（例如，代码用 `v1.2.0`，包管理器用 `1.2.0`）。

### 4.1 核心 Gateway (Go)

| 文件 | 模式 | 示例 |
|:---|:---|:---|
| `cmd/hotplex/main.go:16` | `version = "v1.x.x"` | `v1.2.0` |
| `Makefile:24` | `LDFLAGS ... -X main.version=v1.x.x` | `v1.2.0` |
| `internal/tracing/tracing.go` | `semconv.ServiceVersion("1.x.x")` | `1.2.0` |

### 4.2 多语言 SDK

| 文件 | 模式 |
|:---|:---|
| `examples/typescript-client/package.json` | `"version": "1.x.x"` |
| `examples/python-client/pyproject.toml` | `version = "1.x.x"` |
| `examples/python-client/hotplex_client/__init__.py` | `__version__ = "1.x.x"` |
| `examples/java-client/pom.xml` | `<version>1.x.x-SNAPSHOT</version>` |

### 4.3 基础设施

| 文件 | 模式 |
|:---|:---|
| `Dockerfile` | `LABEL version="1.x.x"` |

### 验证命令

更新后，验证所有位置都已更改：

```bash
# 将 OLD 替换为先前版本，NEW 替换为目标版本
grep -rn "1\.1\.0" cmd/hotplex/main.go Makefile internal/tracing/tracing.go \
  examples/typescript-client/package.json examples/python-client/pyproject.toml \
  examples/python-client/hotplex_client/__init__.py examples/java-client/pom.xml \
  Dockerfile CHANGELOG.md
```

## 步骤 5：验证

按顺序运行：

```bash
# 1. 代码质量
make quality

# 2. 构建二进制
make build

# 3. 验证版本注入
./bin/hotplex-$(go env GOOS)-$(go env GOARCH) version

# 4. 验证 CHANGELOG 格式
head -50 CHANGELOG.md

# 5. 确认干净 diff（仅版本 + changelog 更改）
git diff --stat
```

## 步骤 6：Git 提交和标签

```bash
# 显式暂存所有版本相关文件
git add \
  cmd/hotplex/main.go \
  Makefile \
  internal/tracing/tracing.go \
  examples/typescript-client/package.json \
  examples/python-client/pyproject.toml \
  examples/python-client/hotplex_client/__init__.py \
  examples/java-client/pom.xml \
  Dockerfile \
  CHANGELOG.md

# 提交
git commit -m "chore: release vX.X.X"

# 带注释的标签
git tag -a vX.X.X -m "Release vX.X.X"
```

## 步骤 7：GitHub Release

CI workflow (`.github/workflows/release.yml`) 在 tag push 时自动触发并：
- 为 `darwin-amd64`, `darwin-arm64`, `linux-amd64`, `linux-arm64` 构建二进制
- 计算 SHA-256 校验和
- 创建带有 `generate_release_notes: true` 的 GitHub Release（自动生成注释 — **必须替换**）

```bash
# 推送提交和标签以触发 release
git push origin main && git push origin vX.X.X

# 监控 workflow
gh run list --workflow=release.yml --limit=1
gh run watch <RUN_ID> --exit-status

# CI 完成后，用完整 CHANGELOG 内容替换自动生成的注释
awk '/^## \[X\.X\.X\]/{found=1} found{print} /^## \[PREV\]/{exit}' CHANGELOG.md | sed '$d' > /tmp/release-notes.md

gh release edit vX.X.X --notes-file /tmp/release-notes.md

# 验证 release
gh release view vX.X.X
```

> [!IMPORTANT]
> **Release Notes 必须使用 CHANGELOG.md 内容**。CI 的 `generate_release_notes: true` 只生成 PR 级别摘要，缺少 Summary/Added/Changed/Fixed 完整结构。每次 release 完成后 **必须** 执行 `gh release edit --notes-file` 用 CHANGELOG.md 对应版本段落覆盖。

## 步骤 8：发布后操作

1. 验证 release notes 显示完整 CHANGELOG 内容和 Summary 部分（不仅仅是 PR 摘要）：`gh release view vX.X.X`
2. 验证附加了所有 5 个 artifact：4 个平台二进制 + `checksums.txt`
3. 验证二进制版本：下载并运行 `./hotplex-* version`
4. 清理临时文件 (`rm -f /tmp/release-notes.md`)

---

> [!IMPORTANT]
> **同步检查**：`cmd/hotplex/main.go` 版本、`Makefile` LDFLAGS 版本和 `CHANGELOG.md` 头部版本必须全部匹配。CI workflow 通过 ldflags 从 git tag 覆盖 `main.version`，但源文件必须对本地构建一致。

> [!NOTE]
> **CI 自动发布**：`.github/workflows/release.yml` workflow 在 tag push 时自动处理二进制构建、校验和和 release 创建。手动创建 release 仅用于 workflow_dispatch 或恢复场景。
