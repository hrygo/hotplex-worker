# Cron 定时任务操作手册

## 概述

HotPlex Gateway 内置 AI-native 定时任务系统。载荷为自然语言 Prompt，由 Worker 执行（等同一次 AI 对话），结果自动回传至创建者的平台（飞书卡片 / Slack 消息）。

Prompt 以 `[SILENT]` 开头则不投递结果（用于自维护任务）。

## 环境变量

以下变量已预配置，直接使用即可：

- `HOTPLEX_ADMIN_API_URL` — Admin API 地址（如 `http://localhost:9999`）
- `HOTPLEX_ADMIN_TOKEN` — 认证 token

所有请求需携带 `Authorization: Bearer $HOTPLEX_ADMIN_TOKEN`。

## 字段速查

### 必填字段

创建 Job 时**缺一不可**：`name`、`schedule`、`payload.message`、`owner_id`、`bot_id`。

### 全部字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | **是** | 唯一标识，不可重复 |
| `description` | string | 否 | 任务描述 |
| `schedule` | object | **是** | 调度配置，见下表 |
| `payload.kind` | string | 否 | 默认 `agent_turn` |
| `payload.message` | string | **是** | 执行 Prompt，最大 4KB |
| `owner_id` | string | **是** | 创建者 ID |
| `bot_id` | string | **是** | 关联 Bot ID |
| `work_dir` | string | 否 | 执行工作目录 |
| `timeout_sec` | int | 否 | 单次超时（秒），0 = 使用默认 5min |
| `delete_after_run` | bool | 否 | 执行后删除（适合 one-shot） |
| `max_retries` | int | 否 | 最大重试次数 |

### Schedule 类型

| kind | 说明 | 字段 | 约束 |
|------|------|------|------|
| `at` | 一次性，ISO-8601 时间戳 | `at` | — |
| `every` | 固定间隔（毫秒） | `every_ms` | **最低 60000**（1 分钟） |
| `cron` | Cron 表达式（5 域） | `expr`, `tz`（可选） | 标准格式：`分 时 日 月 周` |

Cron 示例：`0 9 * * 1-5` = 工作日 9:00，`*/30 * * * *` = 每 30 分钟。

## API 端点

基础路径：`${HOTPLEX_ADMIN_API_URL}/api/cron/jobs`

### 创建 Job

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "daily-report",
    "description": "每日生成运行报告",
    "schedule": {
      "kind": "cron",
      "expr": "0 9 * * 1-5",
      "tz": "Asia/Shanghai"
    },
    "payload": {
      "kind": "agent_turn",
      "message": "生成今日运行报告，汇总 Gateway 状态和异常事件"
    },
    "owner_id": "<user_id>",
    "bot_id": "<bot_id>",
    "work_dir": "/home/user/project",
    "timeout_sec": 300
  }'
```

### One-Shot（一次性定时任务）

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "deploy-reminder",
    "schedule": {
      "kind": "at",
      "at": "2026-05-12T09:00:00+08:00"
    },
    "payload": {
      "kind": "agent_turn",
      "message": "提醒：今天需要部署 v1.9.0，检查清单"
    },
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
    "schedule": {
      "kind": "every",
      "every_ms": 1800000
    },
    "payload": {
      "kind": "agent_turn",
      "message": "检查系统健康状态，有问题立即报告"
    },
    "owner_id": "<user_id>",
    "bot_id": "<bot_id>",
    "timeout_sec": 120
  }'
```

### 列出所有 Jobs

```bash
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

### 获取 Job 详情

```bash
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

### 手动触发

```bash
curl -X POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id}/run \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

### 更新 Job

```bash
curl -X PATCH ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### 删除 Job

```bash
curl -X DELETE ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id} \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

### 查询运行历史

```bash
curl ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs/{job_id}/runs \
  -H "Authorization: Bearer $HOTPLEX_ADMIN_TOKEN"
```

## 常见用例

### 定时提醒

用户说"30分钟后提醒我检查部署" → 创建 `kind=at` 的 one-shot job，`at` 设为当前时间 +30min，`delete_after_run: true`。

### 定期巡检

用户说"每天早上9点检查服务状态" → 创建 `kind=cron` 的 recurring job，`expr` 设为 `0 9 * * *`。

### 延迟执行

用户说"2小时后再跑一次测试" → 创建 `kind=at` 的 job，`at` 设为当前时间 +2h。

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
