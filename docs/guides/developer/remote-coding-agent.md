---
title: "Remote Coding Agent"
description: "通过 Slack / 飞书 / WebChat 远程控制 Claude Code，随时随地写代码"
persona: developer
difficulty: intermediate
---

# Remote Coding Agent

离开工位也能写代码。在手机上打开 Slack 或飞书，给 Bot 发消息，Claude Code 就在你的项目目录里干活——读代码、改文件、跑测试、提交 Git，结果实时流式回传。

---

## 核心流程

```
Slack/飞书消息 → HotPlex Gateway → Claude Code Worker → 执行 → 结果流式回传
```

1. 在 Slack/飞书给 Bot 发消息："帮我修复 auth 模块的空指针问题"
2. HotPlex 启动 Claude Code Worker（在你配置的 `work_dir` 下）
3. Agent 自主分析、定位、修改、测试，每一步实时流式回传
4. 敏感操作（Bash/文件写入）暂停等你批准
5. 完成 → IDLE，随时等下一条指令

**会话持久性**：同一聊天线程 = 同一会话。UUIDv5 确定性映射（`ownerID + workDir + channelID` → 固定 session ID），天然共享上下文。

---

## 会话管理实战

### `/gc` — 离开时休眠

离开前发 `/gc`，Worker 进程终止但上下文完整保留。回来后直接发消息，自动 `--resume` 恢复，对话历史不丢。

> `/gc` 只杀进程不丢上下文；`/reset` 连历史一起清空。

### `/reset` — 重新开始

话题切换或对话混乱时：`/reset`。同一 session ID，全新进程，不带历史。

### `/compact` — 压缩上下文

长对话后 Agent 变迟钝：`$压缩`。自动总结历史、释放 token 空间。

### `/context` — token 用量

`$上下文` — 返回 context window 使用情况。

### `/commit` — 检查点

`$提交` — 让 Agent 创建 Git commit，自动生成 message。

### `/rewind` — 撤销上一轮

`$回退` — 撤销上一轮的文件修改，回到安全状态。

---

## 自动重试

LLM 错误（429/529/网络抖动）自动重试，最多 **9 次**指数退避。覆盖 `429`、`529`、`500-503`、`network timeout`。不需要手动发"继续"。

---

## 权限模型

Claude Code 自主执行大部分操作，但敏感操作需你批准：

**自主**：读文件、搜索代码、对话。**需确认**（5 分钟超时）：Bash 命令、文件写入。回复 `允许`/`allow`/`yes` 批准，`拒绝`/`deny`/`no` 拒绝。

### 修改权限模式

```
/perm bypassPermissions
```

跳过所有权限确认（适合自动化场景，风险自担）。

---

## 模型与性能调节

### 切换模型

```
/model claude-sonnet-4-6    # 快速（默认）
/model claude-opus-4-6      # 深度推理
/model claude-haiku-3-5     # 轻量快速
```

### 调整推理力度

```
/effort high    # 深度思考，适合复杂架构决策
/effort medium  # 平衡（默认）
/effort low     # 快速响应，适合简单查询
```

### 查看已加载 Skills

```
$技能
```

查看当前 Agent 加载了哪些技能（如 Git 操作、代码审查等）。

---

## 多项目切换

用 `/cd` 在不同项目间切换，无需重新配置：

```
/cd ~/projects/frontend-app     # 切换到前端项目
/cd ~/projects/api-server       # 切换到后端项目
/cd                             # 不带参数查看当前目录
```

**底层机制**：`/cd` 会派生新的 session key（workDir 是 UUIDv5 输入之一），所以切换目录 = 新会话。Agent 在对应目录下以全新上下文启动。

**回到之前的项目**：再次 `/cd ~/projects/frontend-app`，HotPlex 发现已有该 session 的 TERMINATED 记录（resume decision flag），自动 `--resume` 恢复之前的对话上下文。

---

## 最佳实践

| 场景 | 做法 | 原因 |
|------|------|------|
| 离开工位 | `/gc` | 释放 Worker 进程资源，保留对话上下文 |
| 话题切换 | `/reset` | 避免旧上下文干扰新任务 |
| 长对话变慢 | `$压缩` | 释放 context window，恢复响应质量 |
| 完成小阶段 | `$提交` | Git checkpoint，防止 /rewind 丢失太多 |
| 改坏了 | `$回退` | 快速回退到上一轮的安全状态 |
| 复杂任务前 | `$上下文` | 确认 token 充裕再开始 |
| 定时巡检 | Cron 任务 | [参考 Cron 教程](../user/cron-scheduling.md) |

### 典型工作流示例

```
你：看看 auth 模块为什么报空指针
Agent：[分析代码] → [定位问题] → [请求权限：修改 auth.go]
你：允许
Agent：[修改文件] → [请求权限：运行 go test]
你：允许
Agent：✅ 测试通过
你：$提交
Agent：[创建 commit: fix(auth): nil check before token validation]
你：/gc                          ← 暂时离开
    ... 2 小时后 ...
你：帮我加上单元测试覆盖那个分支   ← 自动 resume，上下文还在
```

---

## 自然语言触发

所有斜杠命令都支持 `$` 前缀 + 中文，不用记英文命令：

```
$休眠    $挂起    $重置    $压缩    $清空    $回退
$提交    $上下文  $技能    $切换模型  $权限模式
```

完整命令列表输入 `?` 或 `/help` 随时查看。
