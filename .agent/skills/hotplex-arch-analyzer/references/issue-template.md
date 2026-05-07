# Issue 模板与格式参考

## 完整 Issue 模板

Issue 遵循结构化格式，包含 6 个部分：Background → Finding Summary → Findings → Implementation Priority → Out of Scope → Verification。

```bash
gh issue create --title "<type>(<module>): <concise-scope-description>" \
  --label "architecture" --label "<severity-label>" \
  --body "$(cat <<'EOF'
## Background

<1-2 句话：模块的角色、当前规模（文件数、行数）、为什么进行此分析>

**Scope**: <aspect1>, <aspect2> — cycle N (模块分析通过 M)
**Key files**: `<file1.go>`, `<file2.go>`, `<file3.go>`

---

## Finding Summary

| Category | Critical | High | Medium | Low |
|----------|----------|------|--------|-----|
| <aspect1> | <n> | <n> | <n> | <n> |
| <aspect2> | <n> | <n> | <n> | <n> |
| **合计** | **<n>** | **<n>** | **<n>** | **<n>** |

---

## Findings

### <Aspect Name>

#### <descriptive-finding-name-without-numbers>

**Severity**: Critical | **Confidence**: High | **ROI**: High
**Location**: `file.go:123-145`, `file2.go:67-89`

**Problem**: <什么错了，为什么重要 — 量化："影响 N 个调用点"，"持有锁约 ~Xms">

**Current Pattern**:
```go
// file.go:123-145
<5-15 行摘录显示问题代码>
```

**Proposed Fix**:
```go
<提议的代码显示修复方向 — 接口、类型分解或更正的逻辑>
```

**Estimated Impact**: <量化："~N 行减少"，"防止 X 类 bug"，"启用 Y">

**Acceptance Criteria**:
- [ ] <具体的、可验证的更改 — 文件 + 预期行为>
- [ ] <添加/更新测试以验证>
- [ ] <无回归：什么绝不能更改>

---

<重复每个发现，用 --- 分隔>

---

## Implementation Priority

| Finding | Priority | Effort | Risk | Impact |
|---------|----------|--------|------|--------|
| <finding-name-1> | P0 | Small | Low | ~N 行，解锁 X |
| <finding-name-2> | P1 | Medium | Medium | 防止 Y 类 bug |
| <finding-name-3> | P2 | Large | Low | 改进 Z |

**Recommended starting point**: <首先解决哪个发现以及为什么 — 通常是 P0/High-ROI>

---

## Out of Scope

以下区域有意不更改：
- <area1>: <原因 — 例如，"平台 API 差异使抽象适得其反">
- <area2>: <原因 — 例如，"已经通过现有接口很好地抽象">

---

## Verification

- [ ] `make test` 通过，无回归
- [ ] `make lint` 不产生新警告
- [ ] <模块特定的行为验证>
EOF
)"
```

## 标题格式

使用 conventional commit 风格：`<type>(<module>): <scope>`

| 类型 | 何时 |
|------|------|
| `refactor` | DRY/SOLID/耦合发现 |
| `fix` | 错误处理/并发/资源泄漏发现 |
| `perf` | 性能/可扩展性发现 |
| `security` | 安全发现 |
| `chore` | 可观测性/可测试性/代码质量发现 |

## 标签映射

按所有发现的最大严重性：
- Critical → `P1`
- High → `P2`
- Medium/Low → `P3`

如果没有值得 issue 的发现（都低/信息性），跳过 issue 创建并在进度中注明。

## AC 撰写指南

好的 AC 是**具体的、可验证的和可测试的**。

**为什么 AC 很重要**：没有清晰 AC 的 issue 往往导致不完整的实现或需要多次返工。好的 AC 让实施者知道"完成"是什么样子，让审查者可以验证实现。

每个发现的 AC 应该：

1. **陈述更改**：必须修改什么代码/文件
2. **定义成功**：如何验证修复有效（测试用例、行为）
3. **指定约束**：绝不能更改什么（向后兼容、性能预算）

**模式**：对于每个发现，写 2-4 个 AC 项目，涵盖更改、其验证和边界条件。

**示例**：

坏："修复错误处理"
好：
```
- [ ] `bridge.go:HandleMessage` 返回错误而不是记录并返回 nil
- [ ] 错误通过 SessionConn.Send 使用 AEP 错误事件传播到调用者
- [ ] 添加测试：`TestHandleMessage_ErrorPropagation` 验证错误到达客户端
```

坏："改进性能"
好：
```
- [ ] 在 `WritePump` 中用 sync.Pool 替换每个消息的 `bytes.Buffer` 分配
- [ ] 基准 `BenchmarkWritePump_Throughput` 显示 <10ns/op 改进
- [ ] 在 `go test -count=5 -bench=.` pprof 下无分配增加
```

坏："重构适配器以共享代码"
好（受 issue 65 风格启发）：
```
- [ ] 在 `messaging/platform_adapter.go` 中将共享适配器字段提取到 `PlatformAdapter` 基础结构
- [ ] Slack 和 Feishu 适配器嵌入 `*PlatformAdapter` 而不是重复字段
- [ ] `make test` 通过，两个适配器测试套件零回归
- [ ] 添加新平台适配器需要实现 3 个接口 + 1 个 StreamingAPI，而不是 15+ 文件
```
