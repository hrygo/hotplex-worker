---
type: test-plan
tags:
  - project/HotPlex
  - messaging/slack
  - e2e-testing
date: 2026-04-19
status: draft
spec-ref: docs/specs/Slack-Adapter-Improvement-Spec.md
total-acs: 130
total-scenarios: 21
---

# Slack Adapter E2E 用户测试计划

> 版本: v1.0
> 日期: 2026-04-19
> 关联规范: [Slack-Adapter-Improvement-Spec](../specs/Slack-Adapter-Improvement-Spec.md)
> 测试环境: Slack Workspace (付费/免费均可, 部分 AC 需付费)
> 执行方式: 半自动化 (手动 Slack 操作 + 自动化断言脚本)

---

## 1. 概述

### 1.1 目的

基于 [Slack-Adapter-Improvement-Spec](../specs/Slack-Adapter-Improvement-Spec.md) 的 126 条验收标准, 设计覆盖全部 AC 的 E2E 用户测试用例。

### 1.2 测试分层

| 层级 | 说明 | 执行方式 |
|------|------|---------|
| L1 手动验证 | 真实 Slack workspace 中操作, 人工确认结果 | 手动 |
| L2 半自动验证 | Slack 中操作, 自动化脚本检查日志/DB 状态 | 半自动 |
| L3 全自动验证 | Mock Slack server, CI 中运行 | 全自动 |

### 1.3 环境准备

**必需**:
- Slack Workspace (Bot app 已安装)
- Bot Token (`xoxb-...`) + App Token (`xapp-...`)
- Socket Mode 已启用
- `configs/config-dev.yaml` 配置完成

**可选 (部分 AC 需要)**:
- 付费 Slack Workspace (Assistant API 测试)
- 第二个 Bot (Bot 防御测试)
- 配置 `files:read` scope (多媒体测试)

### 1.4 统计

| Phase | 场景数 | AC 覆盖数 | 手动 | 半自动 | 全自动 |
|-------|--------|----------|------|--------|--------|
| Phase 1 消息路由 | 5 | 35 | 8 | 12 | 15 |
| Phase 2 用户体验 | 5 | 40 | 10 | 14 | 16 |
| Phase 3 安全 | 3 | 18 | 4 | 8 | 6 |
| Phase 4 多媒体 | 6 | 33 | 6 | 14 | 13 |
| Phase 5 命令 | 2 | 4 | 2 | 2 | 0 |
| **合计** | **21** | **130** | **30** | **50** | **50** |

### 1.5 自动化程度说明

| 标记 | 含义 | 实现策略 |
|------|------|---------|
| 手动 | 必须人工操作 + 视觉确认 | Slack 中操作, 人工判断 |
| 半自动 | 人工触发 + 自动断言 | Slack 操作后检查日志/DB/API |
| 全自动 | CI 可运行 | `slacktest` mock server + Go test |

---

## 2. Phase 1 — 消息路由修复 (35 ACs)

### Scenario 1.1: DM 基础对话

> 覆盖: 2.1-1~6 (teamID/threadTS), 2.3-1~7 (bot 防御), 2.5-1~7 (rich text 提取)

**前置条件**: Bot 在线, DM 频道已打开

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 2.1-1 | teamID 从 AuthTest 保存 | 1. 启动 worker<br>2. 检查日志 `slack: auth test` | 日志中出现 teamID, 非空 | 半自动 | 检查 adapter.teamID 字段值 |
| 2.1-2 | MakeSlackEnvelope 收到正确 teamID/threadTS | 1. DM 中发送 "hello"<br>2. 检查 session ID 格式 | session ID 为 `slack:{teamID}:{channelID}:{threadTS}:{userID}` 四段格式 | 半自动 | 检查 bridge.MakeSlackEnvelope 调用参数 |
| 2.1-3 | session ID 四段完整 | 1. DM 中发送消息<br>2. 查看 admin API `/admin/sessions` | session ID 格式完整, 非空段 | 半自动 | GET `/admin/sessions` 验证格式 |
| 2.1-4 | threadTS 为空时退化 | 1. DM 中发送消息 (无 thread)<br>2. 检查 session ID | session ID 第三段为空: `slack:{teamID}:{channelID}::{userID}` | 半自动 | 断言 session ID 匹配正则 |
| 2.1-5 | ExtractChannelThread 兼容新格式 | 1. 使用新格式 session ID 恢复会话<br>2. 发送后续消息 | 会话正常恢复, 消息路由正确 | 全自动 | 单元测试: `TestExtractChannelThread` |
| 2.1-6 | AuthTest 失败时 Start 返回 error | 1. 配置无效 bot token<br>2. 启动 worker | 启动失败, 日志含 `slack: auth test` 错误 | 全自动 | Mock `AuthTestContext` 返回 error |
| 2.3-1 | 自身 bot 消息被忽略 | 1. Bot 发送消息后<br>2. 检查日志 | bot 自身消息被 `Debug` 日志标记跳过 | 半自动 | 检查日志无重复处理 |
| 2.3-5 | 人类用户消息正常处理 | 1. 用户 DM 发送 "hello"<br>2. 等待回复 | bot 正常回复 | 手动 | - |
| 2.5-1 | 纯 Text 消息保持现有行为 | 1. DM 发送普通文本 "hello world"<br>2. 等待回复 | bot 收到完整文本 "hello world" | 手动 | - |
| 2.5-6 | Text 和 blocks 均为空 | 1. 发送空消息 (如仅 emoji reaction)<br>2. 检查日志 | 消息被跳过, 不触发 AI 处理 | 半自动 | 检查日志无 HandleTextMessage 调用 |

---

### Scenario 1.2: 群组 @Mention 对话

> 覆盖: 2.4-1~9 (用户提及解析), 2.1-2~4 (threadTS in group)

**前置条件**: Bot 已加入群组频道, `require_mention: true`

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 2.4-1 | `<@U111>` 解析为 `@Alice` | 1. 群组中发送 "@bot 你认识 @Alice 吗"<br>2. 检查 AI 收到的文本 | 文本中 `<@U111>` 被替换为 `@Alice` | 全自动 | Mock `GetUserInfoContext` 返回 Alice |
| 2.4-2 | `<@U111\|Bob>` 使用内嵌名称 | 1. 群组中发送含 `<@U111\|Bob>` 的消息<br>2. 检查解析结果 | 解析为 `@Bob`, 无 API 调用 | 全自动 | 断言无 `GetUserInfoContext` 调用 |
| 2.4-3 | Bot 自身提及被移除 | 1. 群组中发送 "@bot hello"<br>2. 检查 AI 收到的文本 | `<@BOT_ID>` 被移除, 文本为 "hello" | 全自动 | Mock botID, 验证输出文本 |
| 2.4-4 | 多个 mention 全部解析 | 1. 发送 "@bot 问 @Alice 和 @Bob 好"<br>2. 检查结果 | 所有 mention 均解析为 @Name | 全自动 | Mock 多个 user info 返回 |
| 2.4-5 | API 失败保留原始格式 | 1. 模拟 `GetUserInfoContext` 失败<br>2. 发送含 mention 的消息 | 保留原始 `<@U111>` 格式 | 全自动 | Mock API 返回 error |
| 2.4-6 | 缓存命中无 API 调用 | 1. 连续两次发送含同一用户 mention<br>2. 检查 API 调用次数 | 第二次无 API 调用 | 全自动 | Mock 计数器验证 |
| 2.4-7 | 解析后文本为空时跳过 | 1. 群组中仅发送 "@bot" (无其他文本)<br>2. 检查处理结果 | 消息被跳过, 不触发 AI | 半自动 | 检查日志 "skipping empty" |
| 2.4-8 | 无 mention 的文本原样返回 | 1. 发送 "hello world"<br>2. 检查 AI 收到的文本 | 文本完全一致 | 全自动 | 断言输入=输出 |
| 2.4-9 | 混合格式 mention 正确处理 | 1. 发送含 `<@U111>` 和 `<@U222\|Bob>` 的消息 | 两种格式均正确解析 | 全自动 | 正则匹配验证 |

---

### Scenario 1.3: 消息去重

> 覆盖: 2.2-1~6 (去重实现)

**前置条件**: Bot 在线, dedup 已初始化

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 2.2-1 | 相同 ClientMsgID 仅处理一次 | 1. 模拟重推同一消息事件<br>2. 检查处理次数 | 第二次消息被跳过 | 全自动 | Mock 重复事件, 断言 HandleTextMessage 调用 1 次 |
| 2.2-2 | ClientMsgID 空 fallback 到 TimeStamp | 1. 发送 ClientMsgID 为空的消息<br>2. 检查去重键 | 使用 TimeStamp 作为去重键 | 全自动 | 构造空 ClientMsgID 事件 |
| 2.2-3 | 不同消息正常处理 | 1. 连续发送两条不同消息<br>2. 检查处理结果 | 两条均被处理 | 全自动 | 断言 HandleTextMessage 调用 2 次 |
| 2.2-4 | WS 重连后重推旧消息被过滤 | 1. 发送消息 A<br>2. 模拟 WS 重连<br>3. 重推消息 A | 消息 A 被去重过滤 | 半自动 | 需要真实 Socket Mode 断连重连 |
| 2.2-5 | 超过 maxEntries FIFO 淘汰 | 1. 发送 maxEntries+1 条消息<br>2. 重推第一条消息 | 第一条消息已被淘汰, 允许重新处理 | 全自动 | 配置小 maxEntries, 验证淘汰行为 |
| 2.2-6 | Close() 后 dedup goroutine 退出 | 1. 启动 adapter<br>2. 调用 Close()<br>3. 检查 goroutine 泄漏 | 无 goroutine 泄漏 | 全自动 | `runtime.NumGoroutine()` 对比 |

---

### Scenario 1.4: Bot 消息过滤

> 覆盖: 2.3-2~7 (bot 防御增强)

**前置条件**: 群组中有另一个 bot (如 Hubot)

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 2.3-2 | 其他 bot 消息被忽略 | 1. 让另一个 bot 在群组发消息<br>2. 检查日志 | 日志含 "skipping bot message", AI 未触发 | 手动 | 需要真实第二个 bot |
| 2.3-3 | message_changed 被忽略 | 1. 用户编辑已发送的消息<br>2. 检查日志 | 编辑事件被跳过 | 半自动 | 检查日志无重复处理 |
| 2.3-4 | channel_join/leave 被忽略 | 1. 用户加入/离开频道<br>2. 检查日志 | join/leave 事件被跳过 | 半自动 | 检查日志 |
| 2.3-6 | bot 过滤时记录 Debug 日志 | 1. 触发 bot 消息过滤<br>2. 检查日志级别 | 日志级别为 Debug | 半自动 | 日志过滤检查 |
| 2.3-7 | 两 bot 不形成无限循环 | 1. 配置两个 bot 互相监听<br>2. 发送一条消息<br>3. 观察 5 分钟 | 无无限回复循环 | 手动 | 需要真实两 bot 环境 |

---

### Scenario 1.5: 富文本块提取

> 覆盖: 2.5-2~7 (rich text block 提取)

**前置条件**: Bot 在线, 支持各种消息格式

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 2.5-2 | SectionBlock 文本被提取 | 1. 发送含 SectionBlock 的消息<br>2. 检查 AI 收到的文本 | SectionBlock 中的文本被正确提取 | 全自动 | 构造 SectionBlock 事件 |
| 2.5-3 | ContextBlock 文本被提取 | 1. 发送含 ContextBlock 的消息<br>2. 检查提取结果 | ContextBlock 文本被提取 | 全自动 | 构造 ContextBlock 事件 |
| 2.5-4 | RichTextBlock 文本被提取 | 1. 发送 RichTextBlock 消息 (Slack 默认富文本编辑器)<br>2. 检查提取结果 | RichTextBlock 文本被正确拼接 | 全自动 | 构造 RichTextBlock 事件 |
| 2.5-5 | Text 为空但 blocks 有内容 | 1. 发送纯 block 消息 (无 Text 字段)<br>2. 检查处理结果 | 从 blocks 中提取文本, 正常处理 | 全自动 | 构造 Text="" + blocks 事件 |
| 2.5-7 | 未知 block 类型安全跳过 | 1. 发送含未知 block 类型的消息<br>2. 检查处理结果 | 不 panic, 已知 block 正常提取 | 全自动 | 构造含自定义 block 的事件 |

---

## 3. Phase 2 — 用户体验 (40 ACs)

### Scenario 2.1: Markdown 格式化

> 覆盖: 3.1-1~13 (mrkdwn 格式化)

**前置条件**: Bot 在线, 让 AI 回复包含各种 Markdown 元素

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 3.1-1 | `**bold**` → `*bold*` | 1. 让 AI 回复含 **bold** 的文本<br>2. 检查 Slack 中显示效果 | Slack 中文字显示为粗体 | 全自动 | FormatMrkdwn("**bold**") == "*bold*" |
| 3.1-2 | `## H2` → `*H2*` | 1. 让 AI 回复含 ## 标题的文本 | Slack 中标题显示为粗体 | 全自动 | FormatMrkdwn("## Title") == "*Title*" |
| 3.1-3 | `[text](url)` → `<url\|text>` | 1. 让 AI 回复含 Markdown 链接 | Slack 中显示为可点击链接 | 全自动 | FormatMrkdwn("[Go](http://go.dev)") == "<http://go.dev|Go>" |
| 3.1-4 | `~~strike~~` → `~strike~` | 1. 让 AI 回复含删除线的文本 | Slack 中显示删除线 | 全自动 | FormatMrkdwn("~~del~~") == "~del~" |
| 3.1-5 | `- item` → `• item` | 1. 让 AI 回复含列表的文本 | Slack 中列表显示为圆点 | 全自动 | FormatMrkdwn("- item") 包含 "•" |
| 3.1-6 | 代码块保持不变 | 1. 让 AI 回复含 ```code``` 的文本 | 代码块内容不被转换 | 全自动 | 断言代码块内容原样保留 |
| 3.1-7 | 行内代码保持不变 | 1. 让 AI 回复含 `inline` 的文本 | 行内代码不被转换 | 全自动 | 断言行内代码原样保留 |
| 3.1-8 | 粗体与代码混合 | 1. 让 AI 回复含 `code` 和 **bold** 的文本 | 代码不被转换, 粗体正常 | 全自动 | 验证混合文本转换结果 |
| 3.1-9 | 空字符串/纯文本原样返回 | 1. 让 AI 回复纯文本 "hello" | 文本原样显示 | 全自动 | FormatMrkdwn("hello") == "hello" |
| 3.1-10 | 多行 Markdown 正确转换 | 1. 让 AI 回复多段 Markdown | 每行独立转换, 总体正确 | 全自动 | 多行输入断言 |
| 3.1-11 | `*italic*` 不被误转换 | 1. 让 AI 回复含 *italic* 的文本 | 斜体语法不被破坏 | 全自动 | FormatMrkdwn("*italic*") 保持不变 |
| 3.1-12 | `***bold italic***` 正确处理 | 1. 让 AI 回复含 ***bold italic*** 的文本 | 正确转换为 *_bold italic_* | 全自动 | FormatMrkdwn("***bi***") |
| 3.1-13 | 代码块内 **text** 不被转换 | 1. 让 AI 回复代码块内含 **text** 的内容 | 代码块内保持原始 Markdown | 全自动 | 保护机制验证 |

---

### Scenario 2.2: 中止命令

> 覆盖: 3.2-1~9 (abort 检测)

**前置条件**: Bot 正在处理一条消息

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 3.2-1 | "stop" 匹配 | 1. Bot 处理中发送 "stop" | 命令被识别为中止 | 全自动 | IsAbortCommand("stop") == true |
| 3.2-2 | "停止" 匹配 | 1. Bot 处理中发送 "停止" | 命令被识别为中止 | 全自动 | IsAbortCommand("停止") == true |
| 3.2-3 | "Stop." 匹配 (去标点) | 1. 发送 "Stop." | 标点被去除, 匹配成功 | 全自动 | IsAbortCommand("Stop.") == true |
| 3.2-4 | "please stop" 匹配 | 1. 发送 "please stop" | 短语匹配成功 | 全自动 | IsAbortCommand("please stop") == true |
| 3.2-5 | "hello" 不匹配 | 1. 发送 "hello" | 不被识别为中止 | 全自动 | IsAbortCommand("hello") == false |
| 3.2-6 | "stop it" 不匹配 | 1. 发送 "stop it" | 非完整匹配, 不中止 | 全自动 | IsAbortCommand("stop it") == false |
| 3.2-7 | 空字符串不匹配 | 1. 发送空消息 | 不被识别为中止 | 全自动 | IsAbortCommand("") == false |
| 3.2-8 | "STOP" 全大写匹配 | 1. 发送 "STOP" | 大小写不敏感, 匹配成功 | 全自动 | IsAbortCommand("STOP") == true |
| 3.2-9 | "stop," 中文标点匹配 | 1. 发送 "stop，" | 中文标点被去除, 匹配成功 | 全自动 | IsAbortCommand("stop，") == true |

---

### Scenario 2.3: Assistant Status — 原生 API

> 覆盖: 3.3-1~9, 3.3-14~18 (付费 workspace 状态指示)

**前置条件**: 付费 Slack Workspace, Assistant API 可用

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 3.3-1 | 付费 workspace 探测成功 | 1. 启动 worker<br>2. 检查日志 | 日志含 "Assistant API capability confirmed" | 半自动 | 检查日志输出 |
| 3.3-4 | tool_use 触发 "Using {tool_name}..." | 1. 发送触发工具调用的消息<br>2. 观察 thread 底部状态 | 显示 "Using read_file..." 等状态文本 | 手动 | 需要视觉确认 |
| 3.3-5 | done 事件清除状态 | 1. AI 完成回复<br>2. 观察 thread 底部 | 状态文本被清除 | 手动 | 需要视觉确认 |
| 3.3-6 | error 事件清除状态 | 1. 触发 AI 处理错误<br>2. 观察状态 | 状态文本被清除 | 手动 | 需要视觉确认 |
| 3.3-7 | 相同状态不重复调用 API | 1. 连续两个 tool_use 事件<br>2. 检查 API 调用次数 | StatusManager 去重, 仅调用一次 | 全自动 | Mock API 调用计数 |
| 3.3-8 | threadTS 为空时跳过 | 1. DM 中发送消息<br>2. 检查日志 | 无 SetAssistantStatus 调用 | 半自动 | 检查日志无 status 调用 |
| 3.3-9 | DM 场景跳过 assistant status | 1. DM 中发送消息<br>2. 检查状态更新 | 无状态更新 (DM 无 thread 上下文) | 半自动 | 日志检查 |
| 3.3-14 | Status API 失败不阻断消息 | 1. 模拟 API 返回 500<br>2. 发送消息 | 消息仍正常处理, bot 正常回复 | 全自动 | Mock API 返回 error |
| 3.3-15 | 多个 tool_use/tool_result 循环 | 1. 发送需要多步骤工具调用的消息<br>2. 观察状态变化 | 状态正确更新为每次工具调用 | 手动 | 需要视觉确认 |
| 3.3-16 | 原生 API 突然不可用自动降级 | 1. 处理中使 API 返回 not_allowed<br>2. 继续处理 | 自动降级到 emoji, 不再重试 | 全自动 | Mock 先成功后失败 |
| 3.3-17 | Stop() 幂等 | 1. 连续调用 Stop() 两次<br>2. 检查是否 panic | 无 panic, 无重复操作 | 全自动 | 调用两次断言无 error |
| 3.3-18 | loading_messages 可配置 | 1. 配置 loading_messages<br>2. 触发状态更新 | 使用自定义提示文本 | 全自动 | 配置注入测试 |

---

### Scenario 2.4: Emoji Activity Indicators

> 覆盖: 3.3-10~13 (免费 workspace emoji fallback)

**前置条件**: 免费 Slack Workspace 或 `assistant_api_enabled: false`

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 3.3-10 | 收到消息立即添加 :eyes: | 1. 发送消息给 bot<br>2. 立即观察消息 reactions | 消息出现 :eyes: emoji | 手动 | 需要视觉确认 |
| 3.3-11 | 2 分钟后追加 :clock1: | 1. 发送需要长时间处理的消息<br>2. 等待 2 分钟 | 消息追加 :clock1: emoji | 半自动 | Mock timer 加速时间 |
| 3.3-12 | 回复完成后清除所有 reactions | 1. Bot 完成回复<br>2. 观察原始消息 reactions | 所有 activity emoji 被清除 | 手动 | 需要视觉确认 |
| 3.3-13 | assistant_api_enabled: false 跳过探测 | 1. 配置 `assistant_api_enabled: false`<br>2. 启动 worker | 跳过探测, 直接使用 emoji | 全自动 | 配置测试 + 日志检查 |

---

### Scenario 2.5: Typing 指示器多阶段

> 覆盖: 3.3 (typing indicator 阶段配置)

**前置条件**: Emoji fallback 模式激活

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| TYP-1 | 可配置的 typing 阶段 | 1. 在 config 中配置自定义 stages<br>2. 启动 worker | 使用自定义阶段配置 | 全自动 | 配置解析测试 |
| TYP-2 | 默认阶段: 0s :eyes: | 1. 不配置 stages<br>2. 发送消息 | 立即添加 :eyes: | 全自动 | 默认值断言 |
| TYP-3 | 长时间等待阶段递进 | 1. 发送需要 7+ 分钟的任务<br>2. 观察阶段变化 | 依次添加 :eyes: → :clock1: → :hourglass: | 手动 | 需要长时间观察 |
| TYP-4 | 阶段切换时旧 emoji 保留 | 1. 观察阶段递进过程 | 旧 emoji 不被移除, 新 emoji 叠加 | 手动 | 需要视觉确认 |

---

## 4. Phase 3 — 安全 (18 ACs)

### Scenario 3.1: DM 访问控制

> 覆盖: 4.1-1~3, 4.1-10~12 (DM 策略)

**前置条件**: 可修改 config 并重启 worker

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 4.1-1 | dm_policy=open 允许所有 DM | 1. 配置 `dm_policy: open`<br>2. 任意用户 DM bot | 所有 DM 均正常处理 | 全自动 | Gate.Check("im", "U_ANY", true) |
| 4.1-2 | dm_policy=disabled 拒绝所有 DM | 1. 配置 `dm_policy: disabled`<br>2. 任意用户 DM bot | 所有 DM 被拒绝, 日志含 "dm_disabled" | 全自动 | Gate.Check("im", "U_ANY", true) rejected |
| 4.1-3 | dm_policy=allowlist 仅白名单 | 1. 配置 `dm_policy: allowlist` + `allow_from: [U111]`<br>2. U111 DM bot<br>3. U222 DM bot | U111 通过, U222 被拒 | 全自动 | 白名单/非白名单断言 |
| 4.1-10 | DM 中 require_mention 不生效 | 1. 配置 `require_mention: true`<br>2. DM 中不 @bot 发送消息 | DM 消息正常处理 | 全自动 | Gate.Check("im", ...) 忽略 require_mention |
| 4.1-11 | 空配置默认 open | 1. 不配置 gate 参数<br>2. DM bot | 消息正常处理 | 全自动 | 空 Gate 默认值测试 |
| 4.1-12 | gate 被拒仅 Debug 日志 | 1. 配置 restrictive policy<br>2. 被拒用户发送消息<br>3. 检查 Slack | 用户无感知, 仅日志记录 | 全自动 | Mock Slack client, 断言无消息发送 |

---

### Scenario 3.2: 群组访问控制

> 覆盖: 4.1-4~9, 4.1-13~14 (群组策略 + mention 检测)

**前置条件**: Bot 已加入群组, 可配置 gate

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 4.1-4 | group_policy=open 允许所有群消息 | 1. 配置 `group_policy: open`<br>2. 群组中发消息 | 所有群消息正常处理 | 全自动 | Gate.Check("channel", ...) |
| 4.1-5 | group_policy=disabled 拒绝所有群消息 | 1. 配置 `group_policy: disabled`<br>2. 群组中发消息 | 群消息被拒绝 | 全自动 | Gate.Check rejected |
| 4.1-6 | group_policy=allowlist + 非白名单 | 1. 配置 allowlist + allow_from<br>2. 非白名单用户群组发言 | 被拒绝 | 全自动 | 白名单断言 |
| 4.1-7 | require_mention=true + 未 @bot | 1. 配置 `require_mention: true`<br>2. 群组中不 @bot 发消息 | 消息被拒绝, "no_mention" | 全自动 | Gate.Check("channel", uid, false) |
| 4.1-8 | require_mention=true + 已 @bot | 1. 群组中 @bot 发消息 | 消息通过 | 全自动 | Gate.Check("channel", uid, true) |
| 4.1-9 | require_mention=false + 未 @bot | 1. 配置 `require_mention: false`<br>2. 群组中不 @bot 发消息 | 消息通过 | 全自动 | Gate.Check passed |
| 4.1-13 | MPIM 与 group 策略一致 | 1. 在 MPIM (多人群聊) 中发消息 | 使用 group 策略 | 全自动 | Gate.Check("mpim", ...) |
| 4.1-14 | Block Kit 消息中 @mention 检测 | 1. 发送含 Block Kit 元素的 @bot 消息<br>2. 检查 mention 检测 | Block Kit 中的 mention 被正确检测 | 全自动 | 构造 Block Kit mention 事件 |

---

### Scenario 3.3: 消息过期检查

> 覆盖: 4.2-1~4 (消息过期)

**前置条件**: Bot 在线

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 4.2-1 | 超过 30 分钟旧消息被忽略 | 1. 模拟发送时间戳 >30min 的消息事件<br>2. 检查处理结果 | 消息被跳过 | 全自动 | 构造旧时间戳事件 |
| 4.2-2 | 30 分钟内消息正常处理 | 1. 发送当前时间戳消息<br>2. 检查处理结果 | 正常处理 | 全自动 | 构造新时间戳事件 |
| 4.2-3 | 时间戳解析失败静默放行 | 1. 发送无效时间戳格式<br>2. 检查处理结果 | 不阻断, 消息被处理 | 全自动 | 构造非法时间戳 |
| 4.2-4 | 空 TimeStamp 不 panic | 1. 发送 TimeStamp 为空的消息<br>2. 检查处理结果 | 不 panic, 正常处理 | 全自动 | 空 TS 事件 |

---

## 5. Phase 4 — 多媒体 (33 ACs)

### Scenario 4.1: 图片分享

> 覆盖: 5.1-1~10 (入站图片)

**前置条件**: Bot 在线, `files:read` scope 已配置

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.1-1 | file_share 触发 Files 提取 | 1. 在 Slack 中粘贴截图发送给 bot<br>2. 检查日志 | 日志显示 Files 提取, MediaInfo 生成 | 半自动 | 日志检查 |
| 5.1-2 | 图片文件分类为 image | 1. 发送 PNG/JPG/GIF 文件<br>2. 检查分类 | fileCategory 返回 "image" | 全自动 | 单元测试 fileCategory() |
| 5.1-6 | 仅分享文件无文字时生成占位符 | 1. 上传图片不附加文字<br>2. 检查 AI 收到的文本 | 文本为 "[用户分享了一张图片: xxx.png]" | 全自动 | Mock MessageEvent |
| 5.1-7 | bot 自己上传的文件被跳过 | 1. bot 上传文件<br>2. 触发 file_share 事件 | bot 上传的文件被跳过 | 全自动 | botID 匹配断言 |
| 5.1-8 | external/remote 文件被跳过 | 1. 分享一个外部链接文件<br>2. 检查处理 | IsExternal 文件被跳过 | 全自动 | 构造 IsExternal=true 的 File |
| 5.1-10 | 多个文件均被处理 | 1. 同时上传 3 张图片<br>2. 检查 AI 输入 | 3 个文件路径均被拼接 | 全自动 | 构造多 Files 事件 |

---

### Scenario 4.2: 文件下载与存储

> 覆盖: 5.2-1~6 (下载)

**前置条件**: Bot 有 `files:read` scope

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.2-1 | 图片下载到指定目录 | 1. 发送图片给 bot<br>2. 检查 `/tmp/hotplex/media/slack/images/` | 文件存在, 内容完整 | 半自动 | 真实下载 + 文件系统检查 |
| 5.2-2 | MIME 类型正确扩展名 | 1. 发送 JPEG/PNG/GIF<br>2. 检查文件扩展名 | .jpg/.png/.gif 正确 | 全自动 | mimeExt() 单元测试 |
| 5.2-3 | 超过 20MB 文件跳过 | 1. 发送大文件 (>20MB)<br>2. 检查日志 | 文件被跳过, 日志记录 | 全自动 | 构造大文件 Size |
| 5.2-4 | 下载失败不创建空文件 | 1. 模拟下载失败<br>2. 检查文件系统 | 无空文件残留 | 全自动 | Mock GetFile 返回 error |
| 5.2-5 | GetFile 使用 client token 认证 | 1. 检查下载请求的认证头 | 自动附带 bot token | 全自动 | Mock HTTP 检查 Authorization |
| 5.2-6 | 同一文件重复下载覆盖 | 1. 两次分享同一文件<br>2. 检查文件内容 | 第二次覆盖第一次 | 全自动 | 文件哈希对比 |

---

### Scenario 4.3: AI 图片输出

> 覆盖: 5.3-1~7 (Image Block 出站)

**前置条件**: AI 可以生成图片并保存到本地

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.3-1 | 本地图片路径 → Image Block | 1. 让 AI 生成图片保存到 `/tmp/hotplex/media/slack/images/`<br>2. AI 回复含路径<br>3. 观察 Slack 消息 | Slack 中显示 Image Block | 手动 | 需要视觉确认 |
| 5.3-2 | 无图片时纯文本发送 | 1. AI 回复纯文本<br>2. 检查消息类型 | 无 Image Block, 纯文本消息 | 全自动 | extractImages() 返回空 |
| 5.3-3 | 本地图片 <5MB 转 base64 | 1. AI 生成小图片<br>2. 检查发送内容 | 转为 data:image/... URL | 全自动 | localFileToImagePart() 测试 |
| 5.3-4 | 本地图片 >=5MB 跳过 | 1. AI 生成大图片<br>2. 检查发送内容 | 跳过 Image Block, 仅发文本 | 全自动 | 大文件场景 |
| 5.3-5 | 多个图片独立 Image Block | 1. AI 回复含多个图片路径<br>2. 观察 Slack | 每个图片一个独立 Block | 手动 | 需要视觉确认 |
| 5.3-6 | Block 发送失败降级纯文本 | 1. 模拟 PostMessage 失败<br>2. 检查降级行为 | 降级为纯文本发送 | 全自动 | Mock PostMessage 失败 |
| 5.3-7 | Image Block 支持 thread | 1. 在 thread 中触发图片输出<br>2. 检查消息位置 | Image Block 出现在正确 thread | 半自动 | 真实 thread 场景 |

---

### Scenario 4.4: 文件上传

> 覆盖: 5.4-1~4 (File Upload)

**前置条件**: Bot 有 `files:write` scope

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.4-1 | PDF/CSV 上传到 Slack | 1. 让 AI 生成 CSV 报告<br>2. 检查 Slack thread | 文件上传成功, 用户可下载 | 手动 | 需要视觉确认 |
| 5.4-2 | 文件附加到 thread | 1. 在 thread 对话中生成文件<br>2. 检查文件位置 | 文件在 thread 内 | 半自动 | 检查 ThreadTimestamp 参数 |
| 5.4-3 | 上传失败降级文本 | 1. 模拟 UploadFile 失败<br>2. 检查降级行为 | 降级为文本描述 | 全自动 | Mock UploadFile 失败 |
| 5.4-4 | 大文件 >20MB 跳过 | 1. 生成超大文件<br>2. 检查处理 | 跳过上传, 不报错 | 全自动 | 大文件断言 |

---

### Scenario 4.5: Thread 上下文

> 覆盖: 5.5-1~2 (thread 保留)

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.5-1 | 图片消息在 thread 中 | 1. Thread 中发送图片<br>2. 检查 Image Block 位置 | Image Block 在同一 thread | 手动 | 需要视觉确认 |
| 5.5-2 | DM 中无 threadTS 时正常 | 1. DM 中发送图片<br>2. 检查处理 | 正常处理, 不 panic | 半自动 | 检查日志无错误 |

---

### Scenario 4.6: RichText 出站 & OAuth Scope

> 覆盖: 5.6-1~2, 5.7-1~2

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| 5.6-1 | Markdown 表格渲染 | 1. 让 AI 回复含表格的文本<br>2. 检查 Slack 显示 | 表格以 mrkdwn 格式正确显示 | 手动 | 需要视觉确认 |
| 5.6-2 | 代码块高亮 | 1. 让 AI 回复含代码块的文本<br>2. 检查 Slack 显示 | 代码块格式正确 | 手动 | 需要视觉确认 |
| 5.7-1 | files:read scope 正常下载 | 1. 配置 scope 后分享图片<br>2. 检查下载 | 下载成功 | 半自动 | 真实 scope 测试 |
| 5.7-2 | 缺 scope 降级 | 1. 移除 files:read scope<br>2. 分享图片<br>3. 检查日志 | 下载失败, 日志记录, 消息不中断 | 半自动 | 日志检查 |

---

## 6. Phase 5 — Slash Commands (追加)

### Scenario 5.1: /reset 命令

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| SC-1 | /reset 重置会话上下文 | 1. 建立对话上下文<br>2. 输入 `/reset`<br>3. 继续对话 | 上下文被清空, AI 从头开始 | 手动 | 需要真实命令测试 |
| SC-2 | /reset 在 thread 中执行 | 1. 在 thread 中输入 `/reset` | 该 thread 的会话被重置 | 手动 | 需要真实命令测试 |

### Scenario 5.2: /dc 命令

| AC ID | 场景描述 | 测试步骤 | 预期结果 | 自动化 | Mock 策略 |
|-------|---------|---------|---------|--------|----------|
| SC-3 | /dc 终止 worker | 1. AI 处理中输入 `/dc`<br>2. 检查 worker 状态 | Worker 被终止, 会话状态变为 terminated | 手动 | 需要真实命令测试 |
| SC-4 | /dc 后可重新对话 | 1. `/dc` 后发送新消息<br>2. 检查处理 | 新会话被创建, bot 正常回复 | 手动 | 需要真实命令测试 |

---

## 7. 测试执行顺序

### 7.1 推荐执行顺序

| 顺序 | Phase | 场景 | 理由 |
|------|-------|------|------|
| 1 | P1 | 1.1 DM 基础对话 | 基础功能验证 |
| 2 | P1 | 1.2 群组 @Mention | 基础功能验证 |
| 3 | P1 | 1.3 消息去重 | 依赖基础对话 |
| 4 | P1 | 1.4 Bot 消息过滤 | 需要额外 bot |
| 5 | P1 | 1.5 富文本提取 | 边界场景 |
| 6 | P3 | 3.1 DM 访问控制 | 需要配置变更 |
| 7 | P3 | 3.2 群组访问控制 | 需要配置变更 |
| 8 | P3 | 3.3 消息过期 | 需要时间模拟 |
| 9 | P2 | 2.1 Markdown 格式化 | 依赖 AI 回复 |
| 10 | P2 | 2.2 中止命令 | 依赖 AI 处理中 |
| 11 | P2 | 2.3 Assistant Status | 需要付费 workspace |
| 12 | P2 | 2.4 Emoji Indicators | 需要免费 workspace |
| 13 | P2 | 2.5 Typing 多阶段 | 需要长时间任务 |
| 14 | P4 | 4.1 图片分享 | 需要多媒体支持 |
| 15 | P4 | 4.2 文件下载 | 依赖图片分享 |
| 16 | P4 | 4.3 AI 图片输出 | 依赖 AI 生成 |
| 17 | P4 | 4.4 文件上传 | 依赖 AI 生成 |
| 18 | P4 | 4.5~4.6 补充场景 | 依赖前置场景 |
| 19 | P5 | 5.1~5.2 Slash 命令 | 独立功能 |

### 7.2 测试环境矩阵

| 环境 | 用途 | Phase 覆盖 |
|------|------|-----------|
| 免费 Workspace | Emoji fallback 测试 | P1, P2 (emoji), P3 |
| 付费 Workspace | Assistant API 测试 | P1, P2 (native), P3, P4 |
| CI 环境 | 全自动测试 | P1 (15 ACs), P2 (16 ACs), P3 (6 ACs), P4 (13 ACs) |

---

## 8. 自动化测试基础设施

### 8.1 Mock Server 方案

使用 `github.com/slack-go/slack/slacktest` 包或自定义 mock server:

```
┌─────────────┐     ┌──────────────┐     ┌──────────────┐
│  Go Test    │────▶│  Mock Server │────▶│  Slack       │
│  (assert)   │◀────│  (slacktest) │◀────│  Adapter     │
└─────────────┘     └──────────────┘     └──────────────┘
```

### 8.2 半自动验证脚本

提供 `make test-slack-e2e` 命令:
1. 启动 worker 连接真实 Slack
2. 用户在 Slack 中操作
3. 脚本轮询 admin API 验证 session 状态、日志内容

### 8.3 文件结构

```
internal/messaging/slack/
├── e2e_test.go              # 全自动 E2E (mock server)
├── e2e_semi_test.go         # 半自动 E2E (需真实 Slack, build tag: slack_e2e)
└── testdata/                # 测试数据
    ├── message_event.json   # 模拟 Slack 事件
    ├── file_share.json      # 模拟文件分享事件
    └── rich_text.json       # 模拟富文本事件
```

---

## 附录 A. AC → 测试场景映射索引

| Spec AC | 测试场景 | 自动化程度 |
|---------|---------|-----------|
| 2.1-1 | 1.1 DM 基础对话 | 半自动 |
| 2.1-2 | 1.1 DM 基础对话 | 半自动 |
| 2.1-3 | 1.1 DM 基础对话 | 半自动 |
| 2.1-4 | 1.1 DM 基础对话 | 半自动 |
| 2.1-5 | 1.1 DM 基础对话 | 全自动 |
| 2.1-6 | 1.1 DM 基础对话 | 全自动 |
| 2.2-1 | 1.3 消息去重 | 全自动 |
| 2.2-2 | 1.3 消息去重 | 全自动 |
| 2.2-3 | 1.3 消息去重 | 全自动 |
| 2.2-4 | 1.3 消息去重 | 半自动 |
| 2.2-5 | 1.3 消息去重 | 全自动 |
| 2.2-6 | 1.3 消息去重 | 全自动 |
| 2.3-1 | 1.1 DM 基础对话 | 半自动 |
| 2.3-2 | 1.4 Bot 消息过滤 | 手动 |
| 2.3-3 | 1.4 Bot 消息过滤 | 半自动 |
| 2.3-4 | 1.4 Bot 消息过滤 | 半自动 |
| 2.3-5 | 1.1 DM 基础对话 | 手动 |
| 2.3-6 | 1.4 Bot 消息过滤 | 半自动 |
| 2.3-7 | 1.4 Bot 消息过滤 | 手动 |
| 2.4-1 | 1.2 群组 @Mention | 全自动 |
| 2.4-2 | 1.2 群组 @Mention | 全自动 |
| 2.4-3 | 1.2 群组 @Mention | 全自动 |
| 2.4-4 | 1.2 群组 @Mention | 全自动 |
| 2.4-5 | 1.2 群组 @Mention | 全自动 |
| 2.4-6 | 1.2 群组 @Mention | 全自动 |
| 2.4-7 | 1.2 群组 @Mention | 半自动 |
| 2.4-8 | 1.2 群组 @Mention | 全自动 |
| 2.4-9 | 1.2 群组 @Mention | 全自动 |
| 2.5-1 | 1.1 DM 基础对话 | 手动 |
| 2.5-2 | 1.5 富文本提取 | 全自动 |
| 2.5-3 | 1.5 富文本提取 | 全自动 |
| 2.5-4 | 1.5 富文本提取 | 全自动 |
| 2.5-5 | 1.5 富文本提取 | 全自动 |
| 2.5-6 | 1.1 DM 基础对话 | 半自动 |
| 2.5-7 | 1.5 富文本提取 | 全自动 |
| 3.1-1~13 | 2.1 Markdown 格式化 | 全自动 |
| 3.2-1~9 | 2.2 中止命令 | 全自动 |
| 3.3-1~9,14~18 | 2.3 Assistant Status | 手动+全自动混合 |
| 3.3-10~13 | 2.4 Emoji Indicators | 手动+半自动 |
| TYP-1~4 | 2.5 Typing 多阶段 | 手动+全自动 |
| 4.1-1~3,10~12 | 3.1 DM 访问控制 | 全自动 |
| 4.1-4~9,13~14 | 3.2 群组访问控制 | 全自动 |
| 4.2-1~4 | 3.3 消息过期 | 全自动 |
| 5.1-1~10 | 4.1 图片分享 | 半自动+全自动 |
| 5.2-1~6 | 4.2 文件下载 | 半自动+全自动 |
| 5.3-1~7 | 4.3 AI 图片输出 | 手动+全自动 |
| 5.4-1~4 | 4.4 文件上传 | 手动+全自动 |
| 5.5-1~2 | 4.5 Thread 上下文 | 手动+半自动 |
| 5.6-1~2 | 4.6 RichText 出站 | 手动 |
| 5.7-1~2 | 4.6 OAuth Scope | 半自动 |
| SC-1~4 | 5.1~5.2 Slash 命令 | 手动 |

---

## 附录 B. 术语表

| 术语 | 说明 |
|------|------|
| DM | Direct Message, Slack 私信 |
| MPIM | Multi-Party IM, 多人私信 |
| threadTS | Slack thread 时间戳, 标识回复线程 |
| mrkdwn | Slack 的 Markdown 变体语法 |
| Socket Mode | Slack 的 WebSocket 连接模式 |
| Gate | 访问控制门, 控制 DM/群组策略 |
| Dedup | 消息去重, 防止重复处理 |
| Assistant API | Slack 原生 AI 状态 API (付费功能) |
| Block Kit | Slack 富消息 UI 框架 |
| AEP | Agent Envelope Protocol, 网关消息协议 |
