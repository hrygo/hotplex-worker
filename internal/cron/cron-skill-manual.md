# Cron 定时任务操作手册

## 概述

HotPlex Gateway 内置 AI-native 定时任务系统。通过 Admin API 创建和管理 cronjob。

## 环境变量

以下变量已预配置，直接使用即可：

- `HOTPLEX_ADMIN_API_URL` — Admin API 地址（如 `http://localhost:9999`）
- `HOTPLEX_ADMIN_TOKEN` — 认证 token

## API 端点

所有请求需携带 `Authorization: Bearer $HOTPLEX_ADMIN_TOKEN`。

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

## Schedule 类型

| kind | 说明 | 字段 |
|------|------|------|
| `at` | 一次性，ISO-8601 时间戳 | `at` |
| `every` | 固定间隔（毫秒） | `every_ms` |
| `cron` | Cron 表达式（5域） | `expr`, `tz`（可选） |

Cron 表达式格式：`分 时 日 月 周`，例如 `0 9 * * 1-5` = 工作日 9:00。

## 常见用例

### 定时提醒

用户说"30分钟后提醒我检查部署" → 创建 `kind=at` 的 one-shot job，`at` 设为当前时间 +30min。

### 定期巡检

用户说"每天早上9点检查服务状态" → 创建 `kind=cron` 的 recurring job，`expr` 设为 `0 9 * * *`。

### 延迟执行

用户说"2小时后再跑一次测试" → 创建 `kind=at` 的 job，`at` 设为当前时间 +2h。

## 错误处理

- Job 创建失败：检查 schedule 格式、prompt 长度（最大 4KB）
- 连续 5 次调度错误：Job 自动 disable
- One-shot job 执行成功后自动 disable
