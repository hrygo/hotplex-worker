# 使用技巧与窍门

> 提升 HotPlex AI Agent 使用效率的实用技巧

## 有效提示技巧

### 明确目标

好的提示词应该清晰表达你的意图和期望的输出格式：

```
# 不够明确
帮我看看这段代码

# 明确目标
请审查 main.go 中的 handleRequest 函数，关注：
1. 错误处理是否完整
2. 是否有并发安全问题
3. 输出修改建议，使用 diff 格式
```

### 分步骤引导

复杂任务拆分为多个步骤，逐步推进：

```
第一步：分析当前架构，列出所有依赖
第二步：设计新的模块划分方案
第三步：逐步重构，每步确认后再继续
```

### 提供上下文

越多的上下文信息，AI 的回答越精准：

```
我正在开发一个 Go 微服务，使用 Gin 框架，需要添加用户认证功能。
项目结构遵循 clean architecture，数据库使用 PostgreSQL。
请帮我设计 JWT 认证中间件。
```

## /compact：在复杂任务前使用

在开始一个需要大量 context 的复杂任务前，先压缩现有对话：

```
# 检查当前用量
/context

# 如果使用率超过 60%，先压缩
/compact

# 然后开始复杂任务
请帮我重构整个认证模块，包括以下 5 个文件...
```

为什么这样做？复杂任务通常涉及多轮 tool 调用（文件读取、搜索、编辑），每轮都消耗 tokens。提前压缩确保有足够的 context 空间。

## /commit：定期保存进度

使用 `/commit` 让 AI 自动创建 Git commit，形成自然的进度检查点：

```
# 完成一个功能后
请帮我提交当前的修改，commit message 遵循 conventional commits 格式

# AI 会自动：
# 1. git status 查看变更
# 2. git diff 查看具体改动
# 3. 生成语义化的 commit message
# 4. 执行 git commit
```

好处：
- **回滚安全**：每步都有 checkpoint，出错可以轻松回退
- **进度可视化**：通过 `git log` 查看开发轨迹
- **代码审查**：commit 粒度适中，方便后续 review

## /rewind：撤销错误

当 AI 做了错误的修改，使用 `/rewind` 回退到之前的状态：

```
# AI 刚做了一个不好的修改
/rewind

# 指定回退方式
请撤销上一次对 config.go 的修改
```

`/rewind` 使用 Git 的版本控制能力安全地回退改动，不会丢失其他文件的修改。

## Session 生命周期管理

### /gc：离开时归档

```
# 完成工作，准备离开
/gc
```

`/gc` 的好处：
- 释放 Worker 进程占用的 ~512MB 内存
- **保留完整对话历史**
- 下次回来时自动 Resume，上下文无缝恢复

### 自动 Resume

`/gc` 后发送新消息，Gateway 自动：
1. 检测到 Session 处于 `TERMINATED` 状态
2. 重新启动 Worker 进程
3. 通过 `--resume` 恢复对话历史
4. 将你的新消息投递到恢复的 Session

整个过程对用户透明——就像对话从未中断。

## Cron 定时任务

### 自动化重复工作

设置定时任务让 AI 按计划自动执行：

```bash
# 每天早上 9 点检查项目健康状态
# 注意：$GATEWAY_BOT_ID 和 $GATEWAY_USER_ID 是 Gateway 注入的环境变量，
# 需在 Gateway 运行时的 shell 中使用，或手动设置为实际值
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查项目健康：运行测试、检查依赖更新、查看未解决 issue" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 每 30 分钟提醒喝水
hotplex cron create \
  --name "water-reminder" \
  --schedule "every:30m" \
  -m "提醒：该喝水了！" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --max-runs 8
```

### 常用 Cron 场景

| 场景 | Schedule | 说明 |
|------|----------|------|
| 每日健康检查 | `cron:0 9 * * 1-5` | 工作日早 9 点 |
| 每周代码审查 | `cron:0 10 * * 1` | 每周一上午 10 点 |
| 部署后验证 | `at:2026-05-11T10:00:00+08:00` | 一次性定时任务 |
| 定期提醒 | `every:30m` | 每 30 分钟 |

## 多语言支持

HotPlex AI Agent 支持中英文混合对话：

```
# 中文提问
请帮我写一个 HTTP 中间件，要求支持 JWT 认证和速率限制

# 英文提问
Refactor the session manager to use a state machine pattern

# 混合使用
分析一下 handler.go 的 performTransition 函数，看看有没有 race condition
```

Agent 会根据你的提问语言自动选择回复语言。代码注释和 commit message 默认使用英文（遵循项目约定）。

## WebChat 快捷键

### 常用操作

| 快捷键 | 功能 |
|--------|------|
| `Enter` | 发送消息 |
| `Shift + Enter` | 换行（不发送） |
| `Ctrl + /` | 显示快捷键帮助 |

### 会话管理

- **新建对话**：点击侧边栏 "+" 按钮
- **切换对话**：在 Session 列表中点击
- **复制代码**：点击代码块右上角的复制按钮

## 高效工作流示例

### 代码审查流

```
1. "请审查 PR #123 的所有变更"
2. AI 阅读 diff，给出审查意见
3. "请把第 2 点的修改建议直接应用"
4. /commit
```

### 调试流

```
1. "运行测试，看看哪些失败了"
2. AI 执行 go test，分析失败原因
3. "修复 session_test.go 中的竞态条件"
4. AI 修复后再次运行测试
5. /commit
```

### 文档编写流

```
1. /compact  # 先清理 context
2. "阅读 internal/cron/ 包的所有文件，生成 API 文档"
3. "用中文重写，面向开发者"
4. /commit
```
