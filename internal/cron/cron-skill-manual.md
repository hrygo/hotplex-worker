# Cron 定时任务操作手册

## 概述

HotPlex Gateway 内置 AI-native 定时任务系统。载荷为自然语言 Prompt，由 Worker 执行（等同一次 AI 对话），结果自动回传至创建者的平台（飞书卡片 / Slack 消息）。

Prompt 以 `[SILENT]` 开头则不投递结果（用于自维护任务）。

## 意图识别

用户消息中出现以下模式时，应创建 cronjob：

| 用户表达 | schedule 类型 | 示例 |
|----------|--------------|------|
| 每 X 分钟/小时/天 | `every` | "每30分钟检查" → every:30m |
| loop / 循环 / 重复 / 定期 | `every` 或 `cron` | "loop 5m 发消息" → every:5m |
| schedule / 安排 / 定时 | `at` 或 `cron` | "schedule 周一9点" → cron:0 9 * * 1 |
| X 点(提醒我) / 每天/每周 | `cron` | "每天早上9点" → `cron:0 9 * * *` |
| X 分钟后 / 过一会儿 / 稍后 / 延迟 | `at` | "30分钟后提醒" → at:ISO timestamp |
| 提醒我 | `at` | "明天提醒我发周报" → at timestamp |

**禁止**：用 `sleep` 循环、系统 crontab、Claude CronCreate、后台脚本等替代方案。

## 环境发现

### Worker 进程环境变量

Worker 可直接从进程环境变量获取当前会话上下文，**无需查询 Admin API**：

| 环境变量 | 来源（Slack） | 来源（飞书） | 示例值 |
|----------|--------------|-------------|--------|
| `GATEWAY_PLATFORM` | "slack" | "feishu" | slack |
| `GATEWAY_BOT_ID` | botID | botOpenID | B12345 |
| `GATEWAY_USER_ID` | userID | userID | U12345 |
| `GATEWAY_CHANNEL_ID` | channel_id | chat_id | C12345 |
| `GATEWAY_THREAD_ID` | thread_ts | message_id | 1234.56 |
| `GATEWAY_TEAM_ID` | teamID | "" (空) | T12345 |
| `GATEWAY_SESSION_ID` | session ID | session ID | uuid-v5 |
| `GATEWAY_WORK_DIR` | workDir | workDir | /tmp/xxx |

创建 cronjob 需要的 `--owner-id`、`--bot-id` 等字段直接从环境变量读取：

```bash
# Worker 内获取创建 cronjob 所需的上下文
echo $GATEWAY_USER_ID    # → owner-id
echo $GATEWAY_BOT_ID     # → bot-id
echo $GATEWAY_WORK_DIR   # → work-dir
echo $GATEWAY_PLATFORM   # → platform
```

### Admin API 凭据

```bash
# 环境变量已注入（cron enabled 时自动设置）
echo $HOTPLEX_ADMIN_API_URL   # e.g. http://localhost:9999
echo $HOTPLEX_ADMIN_TOKEN     # admin token

# 验证连通性
curl -sf -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" "$HOTPLEX_ADMIN_API_URL/admin/health"
```

## CLI 命令

`hotplex cron` 子命令直接操作本地 SQLite 数据库，无需 gateway 运行即可执行 CRUD。修改类操作会自动通知 gateway 刷新内存索引。

```bash
hotplex cron <command> [flags]
```

全局 flag：

- `-c, --config` — 配置文件路径（默认 `~/.hotplex/config.yaml`）

### 列出任务

```bash
hotplex cron list [--json] [--enabled]
```

### 查看详情

```bash
hotplex cron get <id|name> [--json]
```

支持按 ID 或名称查找。

### 创建任务

```bash
hotplex cron create \
  --name <名称> \
  --schedule <调度表达式> \
  -m <Prompt 消息> \
  --bot-id <Bot ID> \
  --owner-id <用户 ID> \
  [--description <描述>] \
  [--work-dir <工作目录>] \
  [--timeout <超时秒数>] \
  [--allowed-tools <逗号分隔工具列表>]
```

**schedule 格式**（`kind:value` 前缀）：

| 格式 | 说明 | 示例 |
|------|------|------|
| `cron:表达式` | 标准 5 域 cron | `cron:*/5 * * * *` |
| `every:时长` | 固定间隔（最低 1m） | `every:30m`、`every:2h` |
| `at:时间戳` | 一次性 ISO-8601 | `at:2026-05-12T09:00:00+08:00` |

**创建示例**：

```bash
# 从 Worker 环境变量创建一次性提醒
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:$(date -d '+30 minutes' +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "检查部署状态，如有异常立即报告" \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID" \
  --work-dir "$GATEWAY_WORK_DIR"

# 定期巡检
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查系统健康状态，汇总异常事件" \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID"

# 固定间隔
hotplex cron create \
  --name "monitor" \
  --schedule "every:10m" \
  -m "检查服务指标是否正常" \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID" \
  --timeout 120
```

### 更新任务

```bash
hotplex cron update <id|name> [--schedule ...] [-m ...] [--enabled=false] ...
```

仅修改指定字段，未指定的保持不变。

```bash
# 修改 schedule
hotplex cron update daily-health --schedule "cron:0 10 * * 1-5"

# 禁用任务
hotplex cron update daily-health --enabled=false

# 修改 Prompt
hotplex cron update monitor -m "新的检查内容"
```

### 删除任务

```bash
hotplex cron delete <id|name>
```

### 手动触发

```bash
hotplex cron trigger <id|name>
```

需要 gateway 运行中。通过 Admin API 触发实际执行。

### 查看执行历史

```bash
hotplex cron history <id|name> [--json]
```

显示任务每次执行的 turn 统计：状态、耗时、成本、模型、时间。

## 字段速查

### 必填字段

创建 Job 时**缺一不可**：`--name`、`--schedule`、`-m`（message）、`--bot-id`、`--owner-id`。

### 全部字段

| 字段 | CLI flag | 必填 | 说明 |
|------|----------|------|------|
| `name` | `--name` | **是** | 唯一标识 |
| `description` | `--description` | 否 | 任务描述 |
| `schedule` | `--schedule` | **是** | 调度表达式，见上方格式表 |
| `payload.message` | `-m, --message` | **是** | 执行 Prompt，最大 4KB |
| `owner_id` | `--owner-id` | **是** | 创建者 ID（取自 `$GATEWAY_USER_ID`） |
| `bot_id` | `--bot-id` | **是** | Bot ID（取自 `$GATEWAY_BOT_ID`） |
| `work_dir` | `--work-dir` | 否 | 执行工作目录 |
| `timeout_sec` | `--timeout` | 否 | 单次超时（秒），0 = 默认 5min |
| `allowed_tools` | `--allowed-tools` | 否 | 逗号分隔的允许工具列表 |

### Schedule 类型

| kind | CLI 前缀 | 说明 | 约束 |
|------|---------|------|------|
| `at` | `at:` | 一次性 ISO-8601 | 执行后自动 disable |
| `every` | `every:` | 固定间隔 | **最低 1 分钟** |
| `cron` | `cron:` | 标准 5 域表达式 | `分 时 日 月 周` |

Cron 示例：`cron:0 9 * * 1-5` = 工作日 9:00，`cron:*/30 * * * *` = 每 30 分钟。

## API 端点（远程管理）

基础路径：`${HOTPLEX_ADMIN_API_URL}/api/cron/jobs`

适用于远程/非本机场景（CLI 仅限本机直接操作 SQLite）。

### 创建 Job

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "daily-report",
    "schedule": {"kind": "cron", "expr": "0 9 * * 1-5", "tz": "Asia/Shanghai"},
    "payload": {"kind": "agent_turn", "message": "生成今日运行报告"},
    "owner_id": "<user_id>",
    "bot_id": "<bot_id>",
    "work_dir": "/home/user/project",
    "timeout_sec": 300
  }'
```

### One-Shot

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "deploy-reminder",
    "schedule": {"kind": "at", "at": "2026-05-12T09:00:00+08:00"},
    "payload": {"kind": "agent_turn", "message": "提醒：部署检查"},
    "owner_id": "<user_id>",
    "bot_id": "<bot_id>",
    "delete_after_run": true
  }'
```

### 固定间隔

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "health-check",
    "schedule": {"kind": "every", "every_ms": 1800000},
    "payload": {"kind": "agent_turn", "message": "检查系统健康状态"},
    "owner_id": "<user_id>",
    "bot_id": "<bot_id>",
    "timeout_sec": 120
  }'
```

### 列出 / 获取 / 触发 / 更新 / 删除 / 历史

```bash
# 列出
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"

# 详情
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"

# 触发
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id}/run \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"

# 更新
curl -X PATCH ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'

# 删除
curl -X DELETE ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"

# 历史
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id}/runs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

## 常见用例

### 定时提醒

用户说"30分钟后提醒我检查部署" → `hotplex cron create --schedule "at:..." --owner-id $GATEWAY_USER_ID`。One-shot 执行后自动 disable。

### 定期巡检

用户说"每天早上9点检查服务状态" → `hotplex cron create --schedule "cron:0 9 * * *"`。

### 延迟执行

用户说"2小时后再跑一次测试" → `hotplex cron create --schedule "at:..."`，`at` 设为当前时间 +2h。

### 静默自维护

需要定期清理但不需要看到结果 → Prompt 以 `[SILENT]` 开头，结果不投递。

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
