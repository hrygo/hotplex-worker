# 标签体系与 Issue 关闭流程

Phase 1 标签管理的完整参考。仅 Admin 可执行标签和关闭操作。

## Admin 权限检查

```bash
REPO_OWNER=$(gh repo view --json owner --jq '.owner.login')
CURRENT_USER=$(gh api user --jq '.login')
[ "$REPO_OWNER" = "$CURRENT_USER" ] && echo "ADMIN" || echo "NOT_ADMIN"
```

## 标签体系（6 类 27 个标签）

**类型**（选一个）：`bug` | `enhancement` | `documentation` | `performance` | `refactor` | `security`

**优先级**（ROI 计算后分配）：`P1`（关键 ROI≥50）| `P2`（高 ROI 30-49）| `P3`（中 ROI 15-29）

**领域**（可多选）：`architecture` | `race-condition` | `goroutine` | `resource-leak` | `reliability` | `DoS`

**模块**（可多选）：`area/gateway` | `area/session` | `area/messaging` | `area/worker` | `area/cli` | `area/webchat` | `area/config` | `area/updater`

**状态**：`needs-triage` | `blocked` | `breaking-change`

**关闭原因**：`duplicate` | `wontfix` | `invalid` | `fixed` | `not-reproducible`

```bash
gh issue edit <number> --add-label "bug,race-condition,area/gateway"
gh issue edit <number> --remove-label "needs-triage"
```

## 关闭无效 Issue

| 条件 | 标签 | 评论模板 |
|------|------|---------|
| 已在代码中修复 | `fixed` | `已在 <commit/PR> 中修复，关闭此 issue。` |
| 完全重复 | `duplicate` | `与 #<原issue> 重复，关闭此 issue。` |
| 描述不清且>30天无更新 | `wontfix` | `此 issue 缺少足够信息且长期无更新，关闭。如有新信息请重新打开。` |
| 不在项目范围 | `wontfix` | `此需求不在当前项目范围内，关闭。` |
| 无法复现 | `not-reproducible` | `无法在当前版本复现，关闭。如能提供复现步骤请重新打开。` |
| 已间接解决 | `fixed` | `此问题已通过 <PR/commit> 间接解决，关闭。` |

关闭流程：`gh issue edit <N> --add-label "duplicate"` → `gh issue comment <N> --body "..."` → `gh issue close <N>`

## Issue 质量检查

实施前确认：标题 conventional commit 格式、详细描述、Bug 有复现步骤、功能有验收标准、无重复。质量不足时添加 `needs-triage` + 评论请求澄清。
