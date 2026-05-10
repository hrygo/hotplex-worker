# Cron 定时任务操作手册

## 概述

HotPlex Gateway 内置 AI-native 定时任务系统。载荷为自然语言 Prompt，由 Worker 执行（等同一次 AI 对话），结果自动回传至创建者的平台（飞书卡片 / Slack 消息）。

Prompt 以 `[SILENT]` 开头则不投递结果（用于自维护任务）。

**操作方式**：`hotplex cron` CLI（本机直接操作 SQLite，自动通知 gateway 刷新索引）。Admin API 仅用于远程场景。

## 意图识别

用户消息中出现以下模式时，应创建 cronjob：

| 用户表达 | schedule 类型 | CLI 写法 |
|----------|--------------|---------|
| 每 X 分钟/小时/天 | `every` | `every:30m` |
| loop / 循环 / 重复 / 定期 | `every` 或 `cron` | `every:5m` |
| schedule / 安排 / 定时 | `at` 或 `cron` | `cron:0 9 * * 1` |
| X 点(提醒我) / 每天/每周 | `cron` | `cron:0 9 * * *` |
| X 分钟后 / 过一会儿 / 稍后 | `at` | `at:ISO timestamp` |
| 提醒我 | `at` | `at:ISO timestamp` |

**禁止**：用 `sleep` 循环、系统 crontab、Claude CronCreate、后台脚本等替代方案。

## 环境变量

Worker 进程已注入以下环境变量，创建 cronjob 时直接使用：

| 环境变量 | 来源（Slack） | 来源（飞书） | 对应 CLI flag | 示例值 |
|----------|--------------|-------------|--------------|--------|
| `GATEWAY_PLATFORM` | "slack" | "feishu" | — | slack |
| `GATEWAY_BOT_ID` | botID | botOpenID | `--bot-id` | B12345 |
| `GATEWAY_USER_ID` | userID | userID | `--owner-id` | U12345 |
| `GATEWAY_CHANNEL_ID` | channel_id | chat_id | — | C12345 |
| `GATEWAY_THREAD_ID` | thread_ts | message_id | — | 1234.56 |
| `GATEWAY_TEAM_ID` | teamID | "" (空) | — | T12345 |
| `GATEWAY_SESSION_ID` | session ID | session ID | — | uuid-v5 |
| `GATEWAY_WORK_DIR` | workDir | workDir | `--work-dir` | /tmp/xxx |

```bash
# 创建 cronjob 的必填字段，直接从环境变量读取
--bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" --work-dir "$GATEWAY_WORK_DIR"
```

## CLI 命令参考

```
hotplex cron <command> [flags]
```

全局 flag：`-c, --config` 配置文件路径（默认 `~/.hotplex/config.yaml`）

### create — 创建任务

```bash
hotplex cron create \
  --name <名称> \
  --schedule <调度表达式> \
  -m <Prompt> \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID" \
  [--description <描述>] \
  [--work-dir "$GATEWAY_WORK_DIR"] \
  [--timeout <秒>] \
  [--allowed-tools <逗号分隔>]
```

**必填**：`--name`、`--schedule`、`-m`、`--bot-id`、`--owner-id`

**schedule 格式**（`kind:value` 前缀）：

| 格式 | 说明 | 约束 | 示例 |
|------|------|------|------|
| `cron:表达式` | 标准 5 域 cron | `分 时 日 月 周` | `cron:*/5 * * * *` |
| `every:时长` | 固定间隔 | 最低 1 分钟 | `every:30m`、`every:2h` |
| `at:时间戳` | 一次性 | ISO-8601 | `at:2026-05-12T09:00:00+08:00` |

```bash
# 一次性提醒（30分钟后）
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:$(date -d '+30 minutes' +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "检查部署状态，如有异常立即报告" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 工作日每天 9 点
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查系统健康状态，汇总异常事件" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 固定间隔
hotplex cron create \
  --name "monitor" \
  --schedule "every:10m" \
  -m "检查服务指标是否正常" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --timeout 120
```

### list — 列出任务

```bash
hotplex cron list [--json] [--enabled]
```

### get — 查看详情

```bash
hotplex cron get <id|name> [--json]
```

按 ID 或名称查找。

### update — 更新任务

仅修改指定字段，未指定的保持不变。

```bash
hotplex cron update <id|name> [--schedule ...] [-m ...] [--enabled=false] ...

# 修改 schedule
hotplex cron update daily-health --schedule "cron:0 10 * * 1-5"
# 禁用
hotplex cron update daily-health --enabled=false
# 修改 Prompt
hotplex cron update monitor -m "新的检查内容"
```

### delete — 删除任务

```bash
hotplex cron delete <id|name>
```

### trigger — 手动触发

需要 gateway 运行中。

```bash
hotplex cron trigger <id|name>
```

### history — 查看执行历史

显示每次执行的 turn 统计：状态、耗时、成本、模型、时间。

```bash
hotplex cron history <id|name> [--json]
```

## 常见用例

### 定时提醒

用户说"30分钟后提醒我检查部署" → `hotplex cron create --schedule "at:..."`，one-shot 执行后自动 disable。

### 定期巡检

用户说"每天早上9点检查服务状态" → `hotplex cron create --schedule "cron:0 9 * * *"`。

### 延迟执行

用户说"2小时后再跑一次测试" → `hotplex cron create --schedule "at:..."`，`at` 设为当前时间 +2h。

### 静默自维护

需要定期清理但不需要看到结果 → Prompt 以 `[SILENT]` 开头，结果不投递。

## 字段速查

| 字段 | CLI flag | 必填 | 说明 |
|------|----------|------|------|
| `name` | `--name` | **是** | 唯一标识 |
| `schedule` | `--schedule` | **是** | 调度表达式 |
| `payload.message` | `-m` | **是** | Prompt，最大 4KB |
| `owner_id` | `--owner-id` | **是** | 取自 `$GATEWAY_USER_ID` |
| `bot_id` | `--bot-id` | **是** | 取自 `$GATEWAY_BOT_ID` |
| `description` | `--description` | 否 | 任务描述 |
| `work_dir` | `--work-dir` | 否 | 取自 `$GATEWAY_WORK_DIR` |
| `timeout_sec` | `--timeout` | 否 | 单次超时（秒），默认 5min |
| `allowed_tools` | `--allowed-tools` | 否 | 逗号分隔 |

## Admin API（备选）

仅用于远程/非本机场景。CLI 优先。

基础路径：`${HOTPLEX_ADMIN_API_URL}/api/cron/jobs`
认证：`Authorization: Bearer $HOTPLEX_ADMIN_TOKEN`

| 操作 | 方法 | 路径 | Body |
|------|------|------|------|
| 创建 | POST | `/api/cron/jobs` | JSON (见下方) |
| 列出 | GET | `/api/cron/jobs` | — |
| 详情 | GET | `/api/cron/jobs/{id}` | — |
| 触发 | POST | `/api/cron/jobs/{id}/run` | — |
| 更新 | PATCH | `/api/cron/jobs/{id}` | JSON |
| 删除 | DELETE | `/api/cron/jobs/{id}` | — |
| 历史 | GET | `/api/cron/jobs/{id}/runs` | — |

创建 JSON 格式：

```json
{
  "name": "job-name",
  "schedule": {"kind": "cron", "expr": "0 9 * * 1-5"},
  "payload": {"kind": "agent_turn", "message": "Prompt 文本"},
  "owner_id": "<user_id>",
  "bot_id": "<bot_id>",
  "work_dir": "/path",
  "timeout_sec": 300,
  "delete_after_run": false,
  "max_retries": 0
}
```

Schedule JSON 格式：

| kind | 字段 | 示例 |
|------|------|------|
| `at` | `{"kind":"at","at":"2026-05-12T09:00:00+08:00"}` | 一次性 |
| `every` | `{"kind":"every","every_ms":1800000}` | 固定间隔（ms），最低 60000 |
| `cron` | `{"kind":"cron","expr":"0 9 * * 1-5","tz":"Asia/Shanghai"}` | 5 域表达式 |

## 错误处理与重试

| 场景 | 行为 |
|------|------|
| 创建失败 | 检查必填字段、schedule 格式、prompt ≤ 4KB |
| 执行超时 | 按 `timeout_sec` 截断（默认 5min），状态标记 `timeout` |
| 执行失败 | 自动指数退避重试（1min → 5min → 25min），受 `max_retries` 限制 |
| 连续 5 次调度错误 | Job 自动 disable，需手动重新启用 |
| One-shot 执行完成 | 自动 disable；若 `delete_after_run: true` 则自动删除 |
| 网关重启 | 启动时自动加载未完成 Job，宽限期内的错过任务立即补执行 |
| CLI 修改后 gateway 未刷新 | CLI 自动发送 SIGHUP，若失败会在 stderr 输出警告 |
