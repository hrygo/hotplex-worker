# 故障排除指南

本文档提供常见问题和解决方案。

## 问题 1: 无法实施 issue A，因为它依赖于 issue B

**症状**：
- Issue A 的实施需要 issue B 的更改
- 直接实施 A 会导致编译错误或测试失败

**原因**：
- 依赖关系未在选择阶段识别
- Issues 顺序不正确

**解决方案**：
1. **检查依赖**：在选择阶段仔细阅读 issues，识别依赖
2. **优先实施依赖**：在批量中先实施 B，然后实施 A
3. **或推迟到下一批**：如果依赖复杂，考虑将 A 留到下一批

**预防措施**：
- Phase 3 选择时检查依赖关系
- 在实施计划中标注依赖

## 问题 2: 实施所有 issues 后测试失败

**症状**：
- 单独实施时每个 commit 的测试都通过
- 但所有 issues 实施后集成测试失败
- 难以定位哪个 commit 引入问题

**原因**：
- Issues 之间的交互问题
- 集成测试捕获了单元测试未发现的交互 bug
- 可能是最后一个 issue 暴露了前面的问题

**解决方案**：
1. **运行测试频率**：每次 commit 后运行测试，而不仅仅在最后
2. **二分查找**：使用 git bisect 找到引入问题的 commit
   ```bash
   git bisect start
   git bisect bad HEAD  # 当前版本有问题
   git bisect good <最后一个好的commit>
   # Git 会自动检查中间点
   # 标记好或坏，直到找到问题commit
   git bisect reset
   ```
3. **修复问题commit**：修复引入问题的 commit，不要只是在上面添加更多 commits
4. **考虑删除**：如果某个 issue 的更改导致严重问题，考虑从批量中删除它

**预防措施**：
- 每次提交后运行完整测试套件
- 在选择阶段考虑 issues 的交互

## 问题 3: 批量 PR 的 CI 失败

**症状**：
- GitHub Actions 失败
- 本地测试通过但 CI 失败
- 特定平台的 CI 失败（如 Windows）

**原因**：
- Go 版本不匹配（CI 期望 1.26）
- 依赖未安装或版本错误
- 平台特定问题（路径分隔符、换行符等）
- 超时或资源限制

**解决方案**：
1. **检查 GitHub Actions 日志**：
   ```bash
   gh run view <run-id>
   gh run view <run-id> --log
   ```
2. **确保 Go 版本匹配**：
   ```bash
   go version  # 应该是 1.26+
   ```
3. **检查所有依赖**：
   ```bash
   go mod download
   go mod verify
   ```
4. **本地运行 CI 命令**：
   ```bash
   make test
   make lint
   make build
   ```
5. **如果某个 issue 的更改导致 CI 失败**：
   - 考虑从批量中删除该 issue
   - 或修复 CI 问题后继续

**预防措施**：
- 推送前本地运行完整测试和 lint
- 检查 .github/workflows 中的 CI 要求
- 确保代码符合跨平台标准

## 问题 4: 审查反馈要求拆分 PR

**症状**：
- 审查者说"这个 PR 太大了，拆分一下"
- 审查者觉得难以审查
- 审查者要求每个 issue 单独 PR

**原因**：
- 批量确实太大（>5 issues 或 >3 天工作量）
- 审查者不熟悉批量 PR 模式
- Issues 之间的连贯性不够明显

**解决方案**：
1. **解释批量 PR 的好处**：
   - 减少合并冲突
   - 更快的审查（一次性审查相关更改）
   - 综合测试
   - 清洁的历史
2. **如果审查者坚持**：
   - 按逻辑主题拆分成多个批量
   - 例如："messaging fixes batch 1", "messaging fixes batch 2"
3. **但如果 issues 是连贯的**：
   - 坚持保持批量
   - 在 PR 描述中强调为什么这些 issues 应该在一起
   - 提供清晰的 Changes by Issue 部分

**预防措施**：
- Phase 3 选择时保持批量在 1-5 个 issues
- 确保 issues 有主题连贯性
- 在 PR 描述中清晰解释批量结构

## 问题 5: 批量变得太大（>5 issues 或 >3 天）

**症状**：
- 选择阶段发现候选 issues 超过 5 个
- 估算总工作量超过 3 天
- 担心 PR 会太大难以审查

**原因**：
- 候选 issues 高 ROI 的太多
- 没有在早期限制批量大小
- 试图一次解决太多问题

**解决方案**：
1. **按主题拆分成多个批量**：
   - 例如："messaging fixes batch 1", "messaging fixes batch 2"
   - 或按优先级：P1 issues batch 1, P2 issues batch 2
2. **重新评估 ROI**：
   - 是否所有 issues 都是高 ROI？
   - 有些低 ROI issues 可以推迟吗？
3. **考虑依赖和风险**：
   - 高风险或复杂的 issues 单独成批
   - 简单、安全的 issues 可以合并

**预防措施**：
- Phase 2 评分时考虑工作量限制
- Phase 3 选择时限制在 1-5 个 issues
- 优先考虑高 ROI + 低工作量的 issues

## 问题 6: Git 合并冲突

**症状**：
- 推送分支时提示"your branch is behind"
- Pull main 时出现冲突
- 多个批量 PR 同时开发导致冲突

**原因**：
- Main 分支有新的提交
- 其他批量 PR 先合并
- 长时间开发导致分支过时

**解决方案**：
1. **定期 rebase main**：
   ```bash
   git fetch origin main
   git rebase origin/main
   ```
2. **解决冲突**：
   - 仔细检查每个冲突
   - 保留正确的更改
   - 测试解决后的代码
3. **如果冲突太多**：
   - 考虑重新开始批量
   - 或等待其他批量 PR 合并后再开始

**预防措施**：
- 快速实施（1-3 天内完成）
- 定期 rebase main
- 与团队协调批量开发

## 问题 7: 实施时发现 issue 比预期复杂

**症状**：
- 开始实施后发现需要更多工作
- 预估 2 小时，实际需要 1 天
- 涉及未预料的模块或依赖

**原因**：
- Issue 描述不完整
- 分析阶段未深入理解
- 隐藏的复杂性或技术债

**解决方案**：
1. **评估是否继续**：
   - 如果超出批量工作量预算（>3 天），考虑从批量中删除
   - 如果可以接受，继续实施
2. **更新 issue 描述**：
   - 在 issue 评论中记录发现的复杂性
   - 让未来的开发人员了解
3. **调整批量**：
   - 如果这个 issue 占用太多时间，考虑实施其他 issues 后再回来

**预防措施**：
- Phase 1 分析时深入理解 issue
- 如果不确定，在 issue 中询问更多细节
- 保守估算工作量（乘以 1.5 倍）

## 问题 8: PR 合并后发现遗漏重要更改

**症状**：
- PR 合并后发现某个修复不完整
- 或遗漏了某个相关的改进
- 需要立即跟进 PR

**原因**：
- 实施阶段未完全解决 issue
- 或发现了相关问题但未包含
- 或测试未覆盖某些场景

**解决方案**：
1. **创建新批量 PR**：
   - 不要尝试修改已合并的 PR
   - 创建新的批量 PR 包含遗漏的更改
2. **或创建 hotfix**：
   - 如果是关键问题，单独 hotfix
   - 然后在下一批中包含其他改进
3. **更新原 issue**：
   - 如果原 issue 未完全解决，重新打开
   - 在下一批中完成

**预防措施**：
- 实施前仔细检查 issue 要求
- 实施后验证所有场景都覆盖
- PR 描述中清晰说明解决了什么

## 快速诊断命令

```bash
# 检查分支状态
git status
git log --oneline -10

# 检查测试覆盖率
go test -coverprofile=/tmp/coverage.out ./...
go tool cover -func=/tmp/coverage.out

# 检查 lint
make lint
golangci-lint run

# 检查构建
make build
./hotplex version

# 检查 CI 状态
gh run list --workflow=ci.yml --limit 5
gh run view <run-id>

# Git bisect（查找问题commit）
git bisect start
git bisect bad HEAD
git bisect good <last-good-commit>
# 测试每个版本
git bisect reset
```
