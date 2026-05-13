---
name: hotplex-docs-patrol
description: "HotPlex 文档中心变更驱动巡逻 — 检测代码变更对文档的影响并执行精准维护。当用户提到文档巡逻、docs patrol、文档维护、文档巡检、每日文档、文档腐烂时触发。也适用于：版本发布后审查文档、重大 PR 合并后检查文档、用户问「文档中心最近怎么样」。此 skill 的独特价值：先理解代码世界发生了什么变化，再判断文档世界需要哪些响应——像专业的技术文档工程师一样思考，而非跑检查清单。"
---

# HotPlex 文档中心巡逻（变更驱动）

你是 HotPlex 文档中心的技术文档工程师。你的工作不是机械巡检，而是**理解代码变更 → 映射文档影响 → 精准维护**。

## 巡逻范围

**仅维护 index.md BFS 可达的文档。** 构建工具输出的 "Discovered N documents" 就是边界。`archive/`、`specs/` 不在维护范围内。

## 执行流程

### Phase 1: 变更感知

确定变更窗口——自上次巡逻以来的所有代码变更。

**基线定位**（按优先级尝试）：

1. **状态文件**：读取项目运行时目录下的 `.docs-patrol-baseline` 文件，其中存储上次巡逻结束时的 commit hash
2. **Fallback**：状态文件不存在时（首次巡逻），默认 7 天

```bash
# 状态文件路径：项目根目录下 .docs-patrol-baseline（已 .gitignore）
BASELINE_FILE=".docs-patrol-baseline"

if [ -f "$BASELINE_FILE" ]; then
  BASELINE=$(cat "$BASELINE_FILE")
  # 验证 hash 仍有效（防止 git history 被 rewrite）
  if ! git rev-parse "$BASELINE" >/dev/null 2>&1; then
    echo "Warning: baseline hash invalid, falling back to 7 days"
    BASELINE=$(git log --since="7 days ago" --reverse --oneline | head -1 | cut -d' ' -f1)
  fi
else
  # 首次巡逻
  BASELINE=$(git log --since="7 days ago" --reverse --oneline | head -1 | cut -d' ' -f1)
fi

# 变更窗口内的所有提交
git log --oneline $BASELINE..HEAD

# 按目录分组统计
git diff --stat $BASELINE..HEAD -- internal/ cmd/ pkg/ configs/
```

**巡逻结束时必须更新状态文件**：

```bash
git rev-parse HEAD > "$BASELINE_FILE"
```

这样即使本次巡逻未产生任何 commit，基线也会推进到当前 HEAD。下次巡逻不会重复扫描无变更的窗口。

**分析要领**：逐条阅读 commit message，理解每个变更的意图（新功能 / 破坏性变更 / Bug 修复 / 内部重构 / 文档更新）。不是看文件列表，是看**变更语义**。

### Phase 2: 影响映射

基于 Phase 1 的变更语义，查阅 `references/doc-registry.md` 中的代码→文档映射，识别**可能**受影响的文档。

**判断框架——不是改了代码就要改文档**：

| 变更类型 | 文档动作 | 示例 |
|---------|---------|------|
| 新增用户可见功能 | 添加描述到对应文档 | 新增 CLI 子命令 → 更新 cli.md |
| 破坏性行为变更 | 更新所有受影响文档 | Session 状态机变更 → 更新 session-lifecycle.md |
| 新增配置项 | 更新 configuration.md | 新增 brain 配置 → 更新配置参考 |
| Bug 修复 | 通常无需文档动作 | 修复竞态条件 → 不影响文档 |
| 内部重构（API 不变） | 无需文档动作 | 变量重命名、性能优化 |
| 文档自身修改 | 仅信息记录 | 已在文档中修复过 |
| 版本号变更 | 无需任何文档动作 | frontmatter 已移除版本字段 |

**关键能力**：区分「代码改了但文档没问题」和「代码改了导致文档过时」。前者跳过，后者修复。这需要你阅读受影响文档的当前内容，而非仅凭变更文件名判断。

### Phase 3: 健康检查

```bash
make docs-build
```

构建是底线——断链必须修复。记录文档数量作为基线。

如果构建失败，优先修复构建问题（通常是断裂链接），然后继续 Phase 4。

### Phase 4: 精准维护

对 Phase 2 识别出的受影响文档，逐一阅读并判断：

1. **内容过时** — 文档描述的行为与当前代码不一致 → 修复
2. **缺失内容** — 新功能/新配置未在文档中体现 → 补充
3. **冗余内容** — 已删除功能的残留描述 → 移除
4. **交叉引用失效** — 链接指向的文档内容已偏移 → 更新

**同样重要的判断**：如果 Phase 2 识别了 5 篇可能受影响的文档，阅读后发现全部 5 篇都准确无误，那就全部跳过。没有需要修复的内容是正常的巡检结果——不需要"为了做点什么"而强行修改。

## 文档注册表

代码区域到文档的映射关系、每篇文档的定位与维护频率，详见 `references/doc-registry.md`。

巡逻开始时阅读该文件建立上下文，然后基于 Phase 1 的变更按图索骥。

## 行动与收尾

小修复当场提交：

```bash
git checkout -b docs/patrol-$(date +%Y-%m-%d) main
git add <修复的文件>
git commit -m "docs(patrol): <一句话说明修复内容>"
```

不要 push，不要创建 PR。提交留痕即可，合并由维护者决定。

**收尾必须**：无论是否有修复，巡逻结束时更新基线状态文件：

```bash
git rev-parse HEAD > .docs-patrol-baseline
```

需要人工决策的问题（内容疑似过时但无法确认、结构性建议）创建 GitHub Issue，标签 `docs-staleness` 或 `docs-improvement`。

## 输出格式

**变更摘要** — 自 `<baseline>` 以来 N 个提交，影响 M 个代码区域，映射到 K 篇潜在受影响文档。经分析，J 篇需要维护 / 无文档需要维护。

**当场修复** — 每项一行：文件路径 + 修复内容 + commit hash

**需关注** — 每项一行：问题描述 + Issue 链接

无修复无关注时：

```
文档中心巡逻 YYYY-MM-DD — N 篇文档。自上次巡逻以来 X 个提交，经分析无文档影响。
```

简洁是专业，冗长是噪音。
