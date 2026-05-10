---
title: 定时任务 (Cron) 教程
description: 用自然语言调度 AI 任务 —— HotPlex AI-native 定时系统完全指南
persona: developer
difficulty: beginner
---

# 定时任务 (Cron) 教程

HotPlex 内置 AI-native 定时任务系统。载荷是自然语言 Prompt，由 Worker 执行（等同一次 AI 对话），结果自动回传到创建者的平台（飞书卡片 / Slack 消息）。

**前置条件**：HotPlex Gateway 已运行（`hotplex gateway start`），已接入 Slack 或飞书。

## 1. 创建你的第一个定时任务

创建一个每 5 分钟执行一次的健康检查任务：

```bash
hotplex cron create \
  --name "quick-health" \
  --schedule "every:5m" \
  -m "检查系统健康状态，汇总异常事件" \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID"
```

> 环境变量 `GATEWAY_BOT_ID` 和 `GATEWAY_USER_ID` 在 Worker 进程中自动注入，直接使用即可。

创建成功后，CLI 返回任务 ID。从此刻起，Worker 每 5 分钟执行一次 Prompt，结果发送到你的 Slack/飞书。

**验证**：`hotplex cron list` 查看任务是否出现，状态为 enabled。

## 2. 三种调度类型

HotPlex 支持三种 schedule 格式，通过 `kind:value` 前缀区分：

### cron — 标准 cron 表达式

5 域格式：`分 时 日 月 周`。适合固定时间点的周期任务。

```bash
# 工作日每天早上 9 点
hotplex cron create \
  --name "weekday-morning" \
  --schedule "cron:0 9 * * 1-5" \
  -m "生成本日工作简报" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 每 15 分钟
hotplex cron create \
  --name "frequent-check" \
  --schedule "cron:*/15 * * * *" \
  -m "检查服务指标是否正常" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 每周一上午 10 点
hotplex cron create \
  --name "weekly-review" \
  --schedule "cron:0 10 * * 1" \
  -m "汇总上周数据并生成周报" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"
```

### every — 固定间隔

从上次执行完成后开始计时，最低 1 分钟。适合监控类任务。

```bash
# 每 30 分钟
--schedule "every:30m"

# 每 2 小时
--schedule "every:2h"

# 每 6 小时
--schedule "every:6h"
```

### at — 一次性定时

指定精确时间戳（ISO-8601），执行一次后自动 disable。适合延迟提醒、定时触发。

```bash
# 指定精确时间
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:2026-05-12T09:00:00+08:00" \
  -m "检查部署状态，如有异常立即报告" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 动态计算（Linux: date -d，macOS: date -v）
--schedule "at:$(date -d '+30 minutes' +%Y-%m-%dT%H:%M:%S+08:00)"
```

## 3. 生命周期管理

### 限制执行次数

`--max-runs` 让任务成功执行 N 次后自动 disable：

```bash
# 30 分钟一次，最多执行 6 次后停止
hotplex cron create \
  --name "hydration-remind" \
  --schedule "every:30m" \
  -m "提醒用户喝水" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --max-runs 6
```

### 设置过期时间

`--expires-at` 在指定时间后自动 disable：

```bash
# 24 小时后自动停止
hotplex cron create \
  --name "temp-monitor" \
  --schedule "every:10m" \
  -m "监控服务状态" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --expires-at "2026-05-11T00:00:00+08:00"
```

两者可组合使用，任一条件满足即停止。

## 4. 一次性延迟任务

`at` 类型配合 `--delete-after-run` 实现真正的即发即弃：

```bash
# 1 小时后执行，完成后自动删除任务
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:$(date -v+1H +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "检查部署状态" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --delete-after-run
```

失败时自动重试（指数退避：1min -> 5min -> 25min）：

```bash
# 失败最多重试 3 次
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:$(date -v+1H +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "检查部署状态" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --delete-after-run --max-retries 3
```

其他选项：`--timeout 120`（超时秒数，默认使用调度器配置（默认 5 分钟），可通过此参数覆盖）、`--silent`（静默，不投递结果）。

## 5. 查看与管理

### 列出所有任务

```bash
# 表格形式
hotplex cron list

# JSON 格式（适合脚本处理）
hotplex cron list --json

# 只看启用的
hotplex cron list --enabled
```

### 查看任务详情

```bash
# 按 ID 或名称查找
hotplex cron get daily-health
hotplex cron get cj_abc123
```

### 更新任务

仅修改指定字段，未指定的保持不变：

```bash
# 修改调度时间
hotplex cron update daily-health --schedule "cron:0 10 * * 1-5"

# 禁用任务（不删除，可随时重新启用）
hotplex cron update daily-health --enabled=false

# 修改 Prompt
hotplex cron update monitor -m "新的检查内容"
```

### 手动触发

无需等待调度，立即执行一次（需要 Gateway 运行中）：

```bash
hotplex cron trigger daily-health
```

### 查看执行历史

每次执行的详细记录：状态、耗时、成本、模型、时间：

```bash
hotplex cron history daily-health

# JSON 格式
hotplex cron history daily-health --json
```

### 删除任务

```bash
hotplex cron delete daily-health
```

## 6. AI-native 用法

这是 HotPlex 的杀手级特性：**在对话中用自然语言创建定时任务**。

在 Slack 或飞书中对 Bot 说：

- "每天早上 9 点检查系统健康状态" -- Bot 自动创建 `cron:0 9 * * *` 任务
- "30 分钟后提醒我检查部署" -- Bot 自动创建 `at:` 一次性任务
- "每隔 2 小时巡检一次服务指标" -- Bot 自动创建 `every:2h` 任务
- "每天提醒我喝水，一共提醒 6 次就行" -- Bot 自动加上 `--max-runs 6`

HotPlex 的 Brain 意图识别会解析自然语言中的时间表达和频率意图，自动选择合适的 schedule 类型并组装 CLI 命令执行。你不需要手动拼命令，直接说就行。

## 参数速查

| 参数 | 必填 | 说明 |
|------|------|------|
| `--name` | 是 | 唯一标识 |
| `--schedule` | 是 | 调度表达式（`cron:` / `every:` / `at:`） |
| `-m` | 是 | Prompt，最大 4KB |
| `--bot-id` | 是 | 取自 `$GATEWAY_BOT_ID` |
| `--owner-id` | 是 | 取自 `$GATEWAY_USER_ID` |
| `--timeout` | 否 | 单次超时秒数，默认使用调度器配置（默认 5 分钟），可通过此参数覆盖 |
| `--max-runs` | 否 | 成功 N 次后自动 disable |
| `--expires-at` | 否 | 过期时间（RFC3339） |
| `--delete-after-run` | 否 | 执行后自动删除 |
| `--max-retries` | 否 | 失败重试次数，默认 0 |
| `--silent` | 否 | 静默模式，不投递结果 |
| `--work-dir` | 否 | 工作目录，取自 `$GATEWAY_WORK_DIR` |

---

**下一步**：了解 [Agent 配置](../reference/configuration.md) 或探索 [Slack 集成](./slack-integration.md)。
