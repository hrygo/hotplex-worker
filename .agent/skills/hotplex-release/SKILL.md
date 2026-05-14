---
name: hotplex-release
description: HotPlex Worker Gateway 标准化发布流程。**立即使用此 skill**：当需要发布新版本、创建 GitHub Release、管理版本号、生成 Changelog、收集变更或验证版本一致性时。自动化版本发布流程，确保完整的变更记录和跨所有组件的版本统一。无论是在 main 分支正式发布，还是在 feature 分支准备发布材料，此 skill 都能指导你完成正确的流程。
---

# HotPlex 发布工作流

## 前置条件

- `gh` CLI 已认证并有 repo 访问权限
- 已安装 `make` 和 `go` 1.26+
- 所有测试通过 (`make check`)
- 工作目录干净（无未提交的更改）

## 分支保护策略

**为什么分支保护很重要**：在非 main 分支创建标签会导致发布混乱，因为：
1. 标签应该指向稳定的、已合并到 main 的代码
2. CI/CD 流程期望在 main 分支上触发 release workflow
3. 避免在 feature 分支上意外发布不完整的版本

**分支判断流程**：

1. 在工作流开始时，检查当前分支：
   ```bash
   git branch --show-current
   ```

2. **如果在 `main` 上**：执行完整工作流（步骤 1–8），包括创建 tag 和 release。

3. **如果不在 `main` 上**（feature 分支、release prep 分支等）：仅执行步骤 1–5（版本确定、变更收集、changelog 撰写、版本统一、验证）。然后：
   - 将版本 bump + changelog 作为**准备提交**提交（例如 `chore: prepare release vX.X.X`）
   - **不要**创建 git tag — 标签只能在 main 上创建
   - **不要**推送 tag 或触发 GitHub Release — 这会在错误的分支上触发 CI
   - 通知用户："Release preparation committed on `<branch>`。Tag and publish after merging to main."

4. **合并到 main 后**：fast-forward 或 checkout main，然后只执行步骤 6（tag）和步骤 7（推送 tag + GitHub Release）

## 步骤 1：确定下一个版本

**为什么需要确定版本**：语义化版本号帮助用户和依赖者理解变更的影响范围。错误的版本号会导致：
- 依赖者错过重要更新（将 major 标记为 minor）
- 或者遇到破坏性更改（将 breaking change 标记为 patch）

从 `cmd/hotplex/main.go:16`（`version` 变量）读取当前版本。

应用 [语义化版本](https://semver.org/)：
- **Patch** (`v1.1.0` → `v1.1.1`)：Bug 修复、安全补丁、无新功能
- **Minor** (`v1.1.0` → `v1.2.0`)：新功能、向后兼容的更改
- **Major** (`v1.1.0` → `v2.0.0`)：破坏性更改

在继续之前与用户确认新版本。

## 步骤 2：收集变更

**为什么收集变更很重要**：完整的变更收集确保：
1. Changelog 包含所有重要更改，不会遗漏任何修复或功能
2. 可以准确评估版本级别（patch/minor/major）
3. 为用户提供清晰的升级路径和影响分析
4. 避免在发布后发现遗漏重要 commit

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

**为什么 Changelog 很重要**：
1. **用户决策支持**：帮助用户快速判断是否需要升级、升级风险和收益
2. **问题追溯**：当出现问题时，可以快速定位引入版本和变更内容
3. **发布透明度**：让团队和社区了解产品演进方向
4. **自动化集成**：CI/CD 使用 changelog 生成 release notes

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

**为什么这些规则很重要**：

1. **Summary 必须有** — Summary 是面向用户的版本叙事，帮助读者在 10 秒内判断是否与自己相关。没有 Summary 的 changelog 只是 commit 列表，不是用户友好的发布说明。

2. **Summary 写法** — 先说版本定位（patch/minor/major），再说核心变化（2-3 个关键点），最后说影响面（哪些模块受益，用户可感知的变化）。用自然语言而非条目列表，让 Summary 成为一个连贯的故事。

3. **每个逻辑更改一个条目** — 将相关的 commit 合并为一个条目，而不是每个 commit 一行。这样避免 changelog 过于冗长，更关注功能而非实现细节。

4. **现在时态，祈使语气** — "Add feature" 而非 "Added feature" 或 "Adds feature"。这与 Keep a Changelog 标准一致，使内容更简洁直接。

5. **在每个条目开头加粗显示组** — 便于快速扫描，让用户能快速找到感兴趣的模块变更。

6. **仅包含 PR 号或 commit SHA** — 只用于重要/面向用户的更改。内部实现细节的引用对用户没有价值，只会增加噪音。

7. **省略内部重构、CI 更改和仅文档更新** — 除非面向用户。这些变更对最终用户没有影响，不应该出现在发布说明中。

8. **合并小修复** — 将多个小的 bugfix 合并到一个 "minor fixes" 条目中，避免 changelog 过于冗长。

9. **按影响排序** — 各部分中的条目按重要性排序（最重要的在前），确保用户首先看到最相关的变更。

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

**为什么版本统一很重要**：
1. **构建一致性**：确保本地构建和 CI 构建使用相同的版本号
2. **可追溯性**：当用户报告问题时，可以从任何组件的版本信息追溯到完整发布
3. **依赖管理**：SDK 用户需要知道他们使用的是哪个 gateway 版本
4. **避免混淆**：版本不一致会导致调试困难和用户混淆

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

### 4.3 项目文档

| 文件 | 模式 | 示例 |
|:---|:---|:---|
| `README.md` | badge `Version-vX.X.X` | `Version-v1.2.0` |
| `README_zh.md` | badge `Version-vX.X.X` | `Version-v1.2.0` |
| `AGENTS.md` | 头部 `**最后更新**: YYYY-MM-DD` | `**最后更新**: 2026-05-10` |
| `AGENTS.md` | 头部 `**版本**: vX.X.X` | `**版本**: v1.2.0` |

> **注意**：`AGENTS.md` 是 `CLAUDE.md` 的符号链接，只需编辑 `AGENTS.md`（这是实际文件），`CLAUDE.md` 会自动同步。修改时请同时更新**版本号**和**最后更新日期**。

### 4.4 基础设施

| 文件 | 模式 |
|:---|:---|
| `Dockerfile` | `LABEL version="1.x.x"` |

### 验证命令

更新后，验证所有位置都已更改：

```bash
# 推荐验证方法：使用 git diff 检查所有被修改文件的变更是否一致且准确
git diff

# 或者使用 grep 确认新版本号已写入所有关键文件（如 1.2.0）
grep -rn "1\.2\.0" cmd/hotplex/main.go Makefile internal/tracing/tracing.go \
  examples/typescript-client/package.json examples/python-client/pyproject.toml \
  examples/python-client/hotplex_client/__init__.py examples/java-client/pom.xml \
  Dockerfile README.md README_zh.md AGENTS.md
```

## 步骤 5：验证

**为什么需要全面验证**：
1. **代码质量**：确保没有引入新的 lint 错误或格式问题
2. **构建成功**：验证所有平台都能成功构建，避免 CI 失败
3. **版本注入**：确认 ldflags 正确注入版本，二进制文件报告正确的版本号
4. **Changelog 格式**：避免发布后发现格式错误或遗漏重要信息
5. **干净 diff**：确保只包含预期的版本和 changelog 更改，避免意外提交

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

**为什么需要显式暂存和带注释的标签**：
1. **显式暂存**：避免意外包含不相关的文件（如临时文件、调试更改）
2. **带注释的标签**：标签消息会在 GitHub Release 中显示，提供更多上下文
3. **版本化提交**：创建清晰的版本历史，方便回滚和追溯

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
  README.md \
  README_zh.md \
  AGENTS.md \
  CHANGELOG.md

# 提交
git commit -m "chore: release vX.X.X"

# 带注释的标签
git tag -a vX.X.X -m "Release vX.X.X"
```

## 步骤 7：GitHub Release

**为什么需要手动更新 Release Notes**：
CI workflow (`.github/workflows/release.yml`) 在 tag push 时自动触发并：
- 为 `darwin-amd64`, `darwin-arm64`, `linux-amd64`, `linux-arm64` 构建二进制
- 计算 SHA-256 校验和
- 创建带有 `generate_release_notes: true` 的 GitHub Release

但是，CI 的 `generate_release_notes: true` 只生成 PR 级别摘要，缺少：
- **Summary** 部分：版本定位和影响面说明
- **完整的结构**：Added/Changed/Fixed 的详细分组
- **用户友好的描述**：技术 commit 消息到用户语言的转换

因此，每次 release 完成后都需要手动用 CHANGELOG.md 内容覆盖自动生成的注释。

```bash
# 推送提交和标签以触发 release
git push origin main && git push origin vX.X.X

# 监控 workflow
gh run list --workflow=release.yml --limit=1
gh run watch <RUN_ID> --exit-status

# CI 完成后，提取 CHANGELOG 内容替换自动生成的注释
# 将 1.2.0 替换为当前发布的纯数字版本号
VERSION="1.2.0"
awk -v ver="[$VERSION]" '
  /^## \[/ && $2 == ver { found=1; next }
  /^## \[/ && found { exit }
  found { print }
' CHANGELOG.md | perl -ne 'BEGIN{$b=1} $b=0 if /\S/; if($b && /^\s*$/){next} print' > /tmp/release-notes.md

# 验证提取结果：首行必须是 ### 开头，且非空
head -1 /tmp/release-notes.md | grep -q "^### " || { echo "ERROR: Release notes extraction failed — first line is not a changelog section"; exit 1; }
wc -l /tmp/release-notes.md

# 追加 Contributors 段（圆形头像，不使用 @mention 避免与 GitHub 原生列表重复）
LAST_TAG=$(git tag --sort=-version:refname | sed -n '2p')
LOGIN_BLACKLIST="HotPlexBot"  # Bot 账号过滤，| 分隔的 grep -E 模式
CONTRIBUTORS=$(git log "${LAST_TAG}..v${VERSION}" --no-merges --format="%H" | while read sha; do
  gh api "repos/{owner}/{repo}/commits/$sha" --jq '.author.login // empty' 2>/dev/null
done | sort -u | grep -vE "^($LOGIN_BLACKLIST)$")
if [ -n "$CONTRIBUTORS" ]; then
  echo -e "\n### Contributors\n" >> /tmp/release-notes.md
  echo "$CONTRIBUTORS" | while read login; do
    # wsrv.nl: 64px source → 32px HiDPI display, circle mask
    echo -n "[![@${login}](https://wsrv.nl/?url=github.com/${login}.png&w=64&h=64&mask=circle&fit=cover&maxage=1w)](https://github.com/${login}) " >> /tmp/release-notes.md
  done
  echo "" >> /tmp/release-notes.md
fi

gh release edit vX.X.X --notes-file /tmp/release-notes.md

# 验证 release
gh release view vX.X.X
```

## 步骤 8：发布后验证

**为什么需要发布后验证**：
1. **确保用户体验完整**：验证 release notes 显示完整内容，用户能获得准确的变更信息
2. **构建产物完整**：确认所有平台的二进制文件都已成功构建和上传
3. **版本一致性**：验证二进制文件的版本号与标签版本一致
4. **清理工作区**：避免临时文件污染后续工作

发布后检查清单：

1. 验证 release notes 显示完整 CHANGELOG 内容和 Summary 部分（不仅仅是 PR 摘要）：`gh release view vX.X.X`
2. 验证附加了所有 5 个 artifact：4 个平台二进制 + `checksums.txt`
3. 验证二进制版本：下载并运行 `./hotplex-* version`
4. 清理临时文件 (`rm -f /tmp/release-notes.md`)

---

## 常见陷阱和故障排除

### 版本不一致

**问题**：`cmd/hotplex/main.go`、`Makefile`、`CHANGELOG.md` 中的版本号不匹配。

**原因**：
- 更新了部分文件但遗漏了其他文件
- 复制粘贴时版本号输入错误
- 多次发布时版本号混淆

**解决方法**：
1. 使用步骤 4 中的验证命令检查所有位置
2. 在提交前运行 `git diff` 确认所有版本相关文件都已更新
3. 在步骤 1 确定版本后，立即在一个地方记录目标版本，避免混淆

### Tag 错误

**问题**：创建了错误的 tag（版本号错误、拼写错误）。

**原因**：
- 命令行输入错误
- 从其他地方复制了错误的版本号
- 在非 main 分支上创建了 tag

**解决方法**：
1. **删除本地 tag**：`git tag -d vX.X.X`
2. **删除远程 tag**：`git push origin :refs/tags/vX.X.X`
3. **重新创建正确的 tag**：`git tag -a vY.Y.Y -m "Release vY.Y.Y"`
4. **推送新 tag**：`git push origin vY.Y.Y`

**预防**：
- 始终使用步骤 1 中确认的版本号
- 在推送前用 `git tag -l` 和 `git tag -n9` 查看 tag 是否正确
- 遵循分支保护策略，只在 main 分支创建 tag

### Changelog 格式问题

**问题**：
- 缺少 Summary 部分
- commit 消息直接复制到 changelog，过于技术化
- 缺少用户友好的描述
- 格式不一致（时态、语气）

**解决方法**：
1. **使用模板**：始终从步骤 3 的模板开始
2. **撰写用户友好的描述**：将 "Fix null pointer in session manager" 改为 "Gateway Core: Fix session crash when worker connection drops unexpectedly"
3. **添加 Summary**：即使版本很小，也要用 1-2 句话说明本版本的目的
4. **格式检查**：使用 `head -50 CHANGELOG.md` 快速预览格式

### Release Notes 未更新

**问题**：GitHub Release 显示的是自动生成的 PR 摘要，而不是完整的 CHANGELOG 内容。

**原因**：
- CI 完成后忘记运行 `gh release edit --notes-file`
- CHANGELOG.md 中的版本号与 tag 不匹配，导致 awk 命令提取失败

**解决方法**：
1. **手动编辑 release**：`gh release edit vX.X.X --notes-file /tmp/release-notes.md`
2. **验证 CHANGELOG 版本**：确认 CHANGELOG.md 中的版本号与 tag 完全一致（包括 `v` 前缀）
3. **验证 release notes**：运行 `gh release view vX.X.X` 确认更新成功

### CI 构建失败

**问题**：tag 推送后，CI workflow 失败。

**常见原因**：
1. **代码质量检查失败**：`make quality` 发现了新的 lint 错误
2. **构建失败**：某个平台的构建出现错误
3. **测试失败**：发布前引入了回归

**解决方法**：
1. **查看 CI 日志**：`gh run view <RUN_ID> --log`
2. **修复问题**：在 main 分支上修复问题
3. **创建新提交**：`git commit -m "fix: CI failure for vX.X.X"`
4. **删除旧 tag 并创建新 tag**：
   ```bash
   git tag -d vX.X.X
   git push origin :refs/tags/vX.X.X
   git tag -a vX.X.X -m "Release vX.X.X"
   git push origin main && git push origin vX.X.X
   ```

**预防**：
- 在步骤 5 中完整运行 `make quality` 和 `make build`
- 确保所有测试通过：`make check`
- 在推送 tag 前，确保工作目录干净

### 发布后发现遗漏重要变更

**问题**：release 发布后，发现遗漏了重要的 commit 或 changelog 条目。

**解决方法**：
1. **评估影响**：如果遗漏的是用户可见的功能或重要修复，需要补发版本
2. **补发 patch 版本**：
   - 递增 patch 版本号（如 `v1.2.0` → `v1.2.1`）
   - 在 CHANGELOG.md 中添加新版本，说明是补充遗漏的变更
   - 重新执行完整发布流程
3. **更新旧 release notes**（可选）：在旧 release 的 notes 中添加注释说明遗漏的内容

**预防**：
- 在步骤 2 中仔细审查所有 commit，使用 `git log --format="%h %s%n%b---"` 查看详细消息
- 在步骤 3 中，按 scope 分组并合并相关 commit，确保没有遗漏
- 在提交前，让团队成员 review CHANGELOG.md

---

## 关键提醒

> [!IMPORTANT]
> **同步检查**：以下位置的版本号必须全部匹配，共 12 处：
> - **Go 核心**：`cmd/hotplex/main.go`、`Makefile`、`internal/tracing/tracing.go`
> - **SDK**：`examples/{typescript,python,java}-client/` 各自的版本文件
> - **文档**：`README.md` badge、`README_zh.md` badge、`AGENTS.md` 头部
> - **基础设施**：`Dockerfile`
> - **Changelog**：`CHANGELOG.md` 头部
>
> CI workflow 通过 ldflags 从 git tag 覆盖 `main.version`，但源文件必须对本地构建一致。

> [!NOTE]
> **CI 自动发布**：`.github/workflows/release.yml` workflow 在 tag push 时自动处理二进制构建、校验和和 release 创建。手动创建 release 仅用于 workflow_dispatch 或恢复场景。
