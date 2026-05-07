# Issue 审计模式

当步骤 1.5 决定进入审计模式时，执行以下流程替代分析模式（步骤 2-7）。

**为什么审计模式重要**：长期运行的分析器会产生大量 issue，其中：
- 部分是误报（代码模式是设计选择，不是 bug）
- 部分已过时（代码已被修复，issue 未关闭）
- 部分描述偏差（行号偏移、严重性不准确、缺少上下文）
这些问题如果不清理，issue backlog 会变成"噪声场"——团队不再信任 issue 质量，新 issue 被淹没，分析器的价值归零。

## 审计步骤 A：选择审计目标

```bash
# 获取最旧的 open architecture issues（按创建时间排序）
gh issue list --label "architecture" --state open --json number,title,createdAt,body \
  --limit 200 | python3 -c "
import json, sys
issues = json.load(sys.stdin)
issues.sort(key=lambda i: i['createdAt'])
for i in issues[:10]:
    print(f'#{i[\"number\"]}: {i[\"title\"][:70]}  ({i[\"createdAt\"][:10]})')
"
```

**选择规则**（按优先级）：
1. 从未审计过的最旧 issue 优先
2. 同一模块的 issue 放在一起审计（减少上下文切换）
3. 每次审计 3-5 个 issue（与一次分析的 token 开销相当）

**进度跟踪**：在 `progress.json` 的 `audited_issues` 中记录已审计的 issue 号，避免重复审计。

## 审计步骤 B：逐个验证

对每个选中的 issue，执行以下验证：

### B1: 读取 issue 内容

```bash
gh issue view <NUMBER> --json title,body,labels,createdAt
```

从 issue body 提取：
- **Key files**：涉及的源文件
- **Location**：`file.go:line_range`
- **Current Pattern**：描述的问题代码
- **Acceptance Criteria**：预期的修复

### B2: 验证代码现状

读取 issue 中引用的源文件，检查：

1. **行号是否仍然匹配**：`file.go:123-145` 处的代码是否仍是 issue 描述的模式
2. **问题是否已修复**：代码是否已经包含了 Proposed Fix 中的改动
3. **问题是否仍然存在**：Current Pattern 是否仍然准确

**验证产出**：对每个 issue 给出明确判断：

| 判定 | 含义 | 行动 |
|------|------|------|
| **fixed** | 代码已包含修复，issue 可关闭 | 关闭 + 评论说明哪个 commit 修复 |
| **invalid** | 原始分析有误（误报、设计选择、理解偏差） | 关闭 + 评论解释为什么不是问题 |
| **outdated** | 代码已重构，行号和代码片段不再匹配 | 更新行号和代码片段 |
| **valid** | 问题仍然存在，issue 仍然准确 | 评论确认，可选更新 |
| **duplicate** | 与另一个 issue 描述同一问题 | 关闭，指向原始 issue |

## 审计步骤 C：执行处理

根据判定执行对应操作：

### 关闭 issue（fixed / invalid / duplicate）

```bash
# Fixed — 代码已修复
gh issue close <NUMBER> --comment "arch-audit: 此 issue 中描述的问题已在代码中修复。关闭。"

# Invalid — 误报
gh issue close <NUMBER> --comment "arch-audit: 经审查，此发现为误报。<具体原因：设计选择 / 代码模式有其他用途 / 分析理解偏差>。关闭。"

# Duplicate
gh issue close <NUMBER> --comment "arch-audit: 与 issue <ORIGINAL_NUMBER> 描述同一问题，关闭为重复。"
```

### 更新 issue（outdated / valid-but-needs-update）

```bash
# 更新行号引用
gh issue comment <NUMBER> --body "arch-audit: 代码已重构，更新引用位置。

<修正后的行号和代码片段>"

# 降级严重性
gh issue edit <NUMBER> --remove-label "P2" --add-label "P3"
gh issue comment <NUMBER> --body "arch-audit: 严重性从 High 降级为 Medium。<原因>。"
```

### 确认 issue（valid）

```bash
gh issue comment <NUMBER> --body "arch-audit: 已验证此 issue 仍然有效（代码仍匹配描述的模式）。确认。"
```

## 审计步骤 D：更新进度

```python
# 在 progress.json 中记录审计结果
data["audited_issues"].append({
    "number": ISSUE_NUMBER,
    "verdict": "fixed|invalid|outdated|valid|duplicate",
    "action": "closed|updated|confirmed",
    "audited_at": TIMESTAMP
})
data["total_cycles"] += 1
data["recent_activity"].append({
    "cycle": data["total_cycles"],
    "mode": "audit",
    "issues_audited": [ISSUE_NUMBERS],
    "closed": N,
    "updated": M,
    "confirmed": K,
    "timestamp": TIMESTAMP
})
```

## 审计步骤 E：审计报告

```
## Audit Cycle N Complete

**Mode**: Issue Audit
**Issues Audited**: N
**Results**: X closed, Y updated, Z confirmed

| Issue | Module | Verdict | Action |
|-------|--------|---------|--------|
| #265 | gateway | valid | confirmed |
| #258 | worker | outdated | updated line refs |
| #250 | messaging | fixed | closed — fixed in abc123 |

### Stats
- Open issues: <BEFORE> → <AFTER>
- Total audited (all time): N
- **Next**: <audit more / switch to analysis>
```
