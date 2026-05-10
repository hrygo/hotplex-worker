---
title: "命令速查表"
weight: 2
description: "HotPlex 聊天命令快速参考卡片，涵盖飞书 / Slack / WebChat 全平台"
persona: user
difficulty: beginner
---

# 命令速查表

> 适用平台：飞书、Slack、WebChat | 输入 `?` 随时查看帮助

---

## 1. 会话控制（改变会话状态）

| 命令 | 别名 | 说明 |
|:-----|:-----|:-----|
| `/gc` | `/park` | 休眠会话 — 停止 Worker 进程，会话保留，可后续恢复 |
| `/reset` | `/new` | 重置上下文 — 同一会话 ID，全新 Worker 进程重新开始 |
| `/cd <目录>` | — | 切换工作目录（创建新会话） |

---

## 2. Worker 命令（不改变会话状态）

### 信息查询

| 命令 | 说明 |
|:-----|:-----|
| `/context` | 查看上下文窗口使用量 |
| `/skills [关键词]` | 列出 / 搜索已加载的 Skills |
| `/mcp` | 查看 MCP 服务器连接状态 |

### 配置调整

| 命令 | 参数 | 说明 |
|:-----|:-----|:-----|
| `/model` | `<名称>` | 切换 AI 模型 |
| `/perm` | `<模式>` | 设置权限模式 |
| `/effort` | `<级别>` | 设置推理力度 |

### 对话操作（透传至 Worker）

| 命令 | 说明 |
|:-----|:-----|
| `/compact` | 压缩对话历史 |
| `/clear` | 清空当前对话 |
| `/rewind` | 撤销上一轮对话 |
| `/commit` | 创建 Git 提交 |

---

## 3. 自然语言触发（`$` 前缀）

> 用 `$` 前缀代替斜杠命令，支持中英文

| 触发词 | 等效命令 | 触发词 | 等效命令 |
|:-------|:---------|:-------|:---------|
| `$gc` | `/gc` | `$休眠` | `/gc` |
| `$挂起` | `/gc` | — | — |
| `$reset` | `/reset` | `$重置` | `/reset` |
| `$context` | `/context` | `$上下文` | `/context` |
| `$skills` | `/skills` | `$技能` | `/skills` |
| `$mcp` | `/mcp` | — | — |
| `$model` | `/model` | `$切换模型` | `/model` |
| `$perm` | `/perm` | `$权限模式` | `/perm` |
| `$effort` | `/effort` | — | — |
| `$compact` | `/compact` | `$压缩` | `/compact` |
| `$clear` | `/clear` | `$清空` | `/clear` |
| `$rewind` | `/rewind` | `$回退` | `/rewind` |
| `$commit` | `/commit` | `$提交` | `/commit` |
| `$cd` | `/cd` | `$切换目录` | `/cd` |

---

## 4. 交互响应（回复 AI 请求）

当 AI 发起权限确认、提问或输入请求时，直接回复以下内容即可。

### 权限请求（Permission）

| 操作 | 飞书 / Slack 响应词 |
|:-----|:-----|
| **允许** | `允许` `allow` `yes` `是` `同意` `ok` `y` `好` `好的` `确认` `approve` |
| **拒绝** | `拒绝` `deny` `no` `否` `取消` `cancel` `n` `不` `不要` `reject` |

> Slack 额外支持 `allow <requestID>` / `deny <requestID>` 格式指定具体请求

### 提问请求（Question）

直接回复文本内容即可，回复内容将作为答案传回 Worker。

### 输入请求（Elicitation）

| 操作 | 响应词 |
|:-----|:-------|
| **接受** | `accept`（或任意非拒绝文本） |
| **拒绝** | `decline` `拒绝` `cancel` `取消` |
