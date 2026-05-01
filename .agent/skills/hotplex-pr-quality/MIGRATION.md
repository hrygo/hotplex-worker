# HotPlex PR Quality Skill v2.0 迁移指南

## 从 v1.0 升级到 v2.0

### 自动升级

如果你已经使用 v1.0，升级到 v2.0 是无缝的：

```bash
# skill 文件会自动更新
# 下次使用时会自动采用新逻辑
```

**不需要手动操作**：
- ❌ 不需要重新配置
- ❌ 不需要修改命令
- ❌ 不需要学习新语法

### 主要变化

#### 变化 1：泛化 fork 仓库检测

**v1.0**（硬编码）：
```bash
git push -u fork <branch>
gh pr create --repo hrygo/hotplex \
  --head aaronwong1989:fix-xxx
```

**v2.0**（自动检测）：
```bash
# skill 自动检测 fork 远程仓库名称（fork/origin/upstream 等）
git push -u <detected-fork-remote> <branch>
gh pr create --repo hrygo/hotplex \
  --head <detected-username>:fix-xxx
```

**影响**：
- ✅ 支持任何 fork 远程仓库名称
- ✅ 支持任何 GitHub 用户名
- ✅ 适用于所有 HotPlex 贡献者

#### 变化 2：保持 HotPlex 专用

**v1.0**：
- HotPlex 项目专用
- `make test` 和 `make lint`
- Upstream: `hrygo/hotplex`

**v2.0**：
- **仍然 HotPlex 项目专用**
- **仍然** `make test` 和 `make lint`
- **仍然** Upstream: `hrygo/hotplex`

**影响**：
- ✅ 保持 HotPlex 专用配置
- ✅ 保持精准度和专业性
- ✅ 仅泛化 fork 用户名和仓库名

#### 变化 3：新增架构感知能力

**v1.0**：
- 无架构分析
- Commit scope 通用
- PR 描述无架构标注

**v2.0**：
- **自动分析影响的 channel**（Slack/飞书/WebChat）
- **自动分析影响的 worker**（CC/OCS/Pi）
- **自动分析影响的平台**（Linux/macOS/Windows）
- **Commit message 使用架构感知 scope**
- **PR 描述标注架构影响**

**影响**：
- ✅ 帮助团队快速理解变更影响范围
- ✅ 提升代码可维护性
- ✅ 符合 HotPlex 分布式系统架构

**示例对比**：

v1.0 commit message：
```
fix: resolve SSE timeout issues
```

v2.0 commit message（架构感知）：
```
fix(worker/ocs): resolve SSE timeout issues

Add separate sseClient without Timeout for SSE connections
and use cancellable context for clean shutdown.

Fixes #85
```

#### 变化 4：新增跨平台 CI 监控

**v1.0**：
- 通用 CI 监控
- 无平台特定分析

**v2.0**：
- **监控 Linux/macOS/Windows 三平台 CI**
- **分析平台特定失败**
- **提供跨平台修复建议**

**影响**：
- ✅ 确保跨平台兼容性
- ✅ 快速定位平台特定问题
- ✅ 符合 HotPlex 跨平台要求

#### 变化 5：描述更明确

**v1.0**（描述）：
```
"PR 质量保证与 CI 达标：在开发完成后创建高质量 PR，
确保所有检查通过。"
```

**v2.0**（描述）：
```
"HotPlex 项目 PR 质量保证与 CI 达标助手。
**请在以下情况使用此 skill**：..."
```

**影响**：
- ✅ 明确定位为 HotPlex 专用
- ✅ 触发率保持高
- ✅ 覆盖所有使用场景

#### 变化 6：解释"为什么"

**v1.0**（强制）：
```
## Commit Message 格式
**必须**包含 Co-Authored-By 声明
```

**v2.0**（解释）：
```
## 为什么需要规范的 commit message？
好的 commit message 帮助团队理解代码变更历史...
```

**影响**：
- ✅ LLM 理解意图后更好地应用
- ✅ 在特殊情况下能灵活处理
- ✅ 提升使用体验

## 兼容性

### 向后兼容

v2.0 **完全向后兼容** v1.0 的使用场景：

- ✅ 所有 v1.0 支持的场景仍然支持
- ✅ HotPlex 项目仍然完美支持
- ✅ `make test` 和 `make lint` 仍然使用
- ✅ Fork 模式仍然支持
- ✅ Commit 规范仍然使用 Conventional Commits

### 新增功能

v2.0 在保持兼容的基础上，新增了：

- ✅ 支持任何 fork 远程仓库名称
- ✅ 支持任何 GitHub 用户名
- ✅ **架构感知能力**（channel/worker/平台分析）
- ✅ **跨平台 CI 监控**
- ✅ **架构影响标注**
- ✅ 更好的错误处理
- ✅ 更清晰的解释

## 升级建议

### 如果你的 fork 配置与 v1.0 相同

**无需任何改变**：
- v2.0 仍然会检测到 `fork` 远程仓库
- v2.0 仍然会使用 `make test` 和 `make lint`
- 所有命令和流程保持不变

**额外好处**：
- 更好的错误处理
- 更清晰的解释
- 更智能的参数推断
- **架构分析**
- **跨平台监控**

### 如果你的 fork 配置不同

**现在可以直接使用**：
- v2.0 会自动检测你的 fork 远程仓库名称
- v2.0 会自动推断你的 GitHub 用户名
- 不需要手动配置

**不需要**：
- ❌ 修改 skill 代码
- ❌ 配置远程仓库
- ❌ 学习新命令

### 如果你关注架构影响

**立即可用**：
- v2.0 会自动分析修改影响的 channel/worker/平台
- 在 commit message 中使用架构感知 scope
- 在 PR 描述中标注架构影响

**示例**：

修改 `internal/worker/opencodeserver/worker.go`：
```
fix(worker/ocs): resolve SSE timeout issues
```

修改 `internal/messaging/slack/adapter.go`：
```
fix(messaging/slack): resolve streaming message loss
```

修改 `internal/service/service_install.go`：
```
fix(service): Windows service installation fails
```

## 实战对比

### 场景 1：标准配置（fork = 'fork'）

**v1.0**：
```
开发完成，帮我创建 PR
→ 运行 make test/lint ✅
→ 推送到 fork ✅
→ 创建 PR ✅
```

**v2.0**：
```
开发完成，帮我创建 PR
→ 运行 make test/lint ✅
→ 自动检测 fork 远程 ✅
→ 分析架构影响 ✅ (新增)
→ 推送并创建 PR ✅
→ 跨平台 CI 监控 ✅ (新增)
→ 更好的错误处理和解释 ✅
```

**结论**：体验相同，但 v2.0 更智能，增加架构分析和跨平台监控。

### 场景 2：不同 fork 仓库名称（fork = 'origin'）

**v1.0**：
```
开发完成，帮我创建 PR
→ 推送到 fork ❌（找不到 'fork' 远程）
→ 需要手动配置或重命名远程仓库
```

**v2.0**：
```
开发完成，帮我创建 PR
→ 自动检测 fork 远程 ✅
→ 推送到 origin ✅
→ 创建 PR ✅
→ 完成！
```

**结论**：v2.0 适用于任何 fork 配置。

### 场景 3：不同用户名

**v1.0**：
```
开发完成，帮我创建 PR
→ 推送 ✅
→ 创建 PR ❌（硬编码用户名 aaronwong1989）
→ 需要手动修改 PR head 参数
```

**v2.0**：
```
开发完成，帮我创建 PR
→ 自动推断用户名 ✅
→ 推送 ✅
→ 创建 PR ✅
→ 完成！
```

**结论**：v2.0 适用于所有贡献者。

### 场景 4：架构相关修改

**v1.0**：
```
开发完成，帮我创建 PR（修改了 worker/ocs）
→ 推送 ✅
→ 创建 PR ✅
→ Commit message: "fix: resolve SSE timeout" (通用)
→ PR 描述: 无架构影响标注
```

**v2.0**：
```
开发完成，帮我创建 PR（修改了 worker/ocs）
→ 分析架构影响 ✅ (worker/ocs)
→ 推送 ✅
→ 创建 PR ✅
→ Commit message: "fix(worker/ocs): resolve SSE timeout" (架构感知)
→ PR 描述: 标注影响 OCS worker ✅
```

**结论**：v2.0 提供架构感知，帮助团队快速理解变更范围。

### 场景 5：跨平台失败

**v1.0**：
```
Windows CI 失败了，怎么办？
→ 查看 CI 日志 ✅
→ 通用错误分析
→ 通用修复建议
```

**v2.0**：
```
Windows CI 失败了，怎么办？
→ 查看跨平台 CI 状态 ✅ (Linux/macOS/Windows)
→ 分析平台特定失败 ✅ (Windows 特定)
→ 提供跨平台修复建议 ✅ (如使用 filepath.Join)
→ 验证其他平台 ✅
```

**结论**：v2.0 提供跨平台智能分析。

## 升级步骤

### 对于现有用户

**无需任何操作**：
- skill 会自动更新
- 下次使用时自动采用 v2.0
- 所有功能保持兼容
- **自动获得架构分析和跨平台监控能力**

### 对于新用户

**直接使用**：
```
开发完成了，帮我创建 PR
```

skill 会自动适配你的 fork 配置并提供架构分析。

## 总结

v2.0 是一次重要升级，核心改进：

1. **泛化 fork 检测**：从硬编码 `fork` → 自动检测任何名称
2. **泛化用户名检测**：从硬编码 `aaronwong1989` → 自动推断任何用户
3. **保持 HotPlex 专用**：`make test`、`make lint`、`hrygo/hotplex` 不变
4. **新增架构感知**：channel/worker/平台分析，commit scope 感知
5. **新增跨平台监控**：Linux/macOS/Windows 三平台 CI 监控
6. **更易用**：更高触发率，更少配置，更清晰的解释

**核心价值保持不变**：
- ✅ HotPlex 项目专用
- ✅ 自动化流程
- ✅ 质量保证
- ✅ 智能 CI 处理
- ✅ 基于实战经验

**新增核心价值**：
- ✅ **架构感知**（分布式系统专业能力）
- ✅ **跨平台监控**（三平台 CI 智能）
- ✅ **影响标注**（commit + PR）

**立即可用**：
```
开发完成了，帮我创建 PR
```
