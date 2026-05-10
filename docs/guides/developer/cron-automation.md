# Cron 自动化

> 使用 HotPlex Cron 调度器实现 AI Agent 自动化工作流

## 概述

HotPlex 内置 AI-native Cron 调度器，支持从自然语言到自动化执行的完整流程。不同于传统 cron 系统，HotPlex 的定时任务由 AI Agent 执行——你只需用自然语言描述任务，Agent 会自主完成。

核心架构（`internal/cron/`）：

| 组件 | 文件 | 职责 |
|------|------|------|
| Scheduler | `cron.go` | 任务生命周期管理、内存索引、CRUD |
| timerLoop | `timer.go` | tick 引擎、collect → 并发槽 → execute |
| Store | `store.go` | SQLite 持久化 |
| Executor | `executor.go` | Worker 执行适配、Session 构造、环境注入 |
| Delivery | `delivery.go` | 结果投递到飞书/Slack |
| Skill Manual | `skill.go` | `go:embed cron-skill-manual.md` 注入 B 通道 |

## 三种调度模式

### cron 表达式（周期性任务）

标准 5 段 cron 表达式，支持周几映射：

```bash
hotplex cron create \
  --name "weekday-standup" \
  --schedule "cron:0 9 * * 1-5" \
  -m "生成本日 standup 报告" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

| 字段 | 允许值 | 特殊字符 |
|------|--------|---------|
| 分钟 | 0-59 | `*` `,` `-` |
| 小时 | 0-23 | `*` `,` `-` |
| 日 | 1-31 | `*` `,` `-` `?` |
| 月 | 1-12 | `*` `,` `-` |
| 周几 | 0-6（0=周日） | `*` `,` `-` `?` |

### every 固定间隔

按固定间隔执行，支持 `s`/`m`/`h` 单位：

```bash
hotplex cron create \
  --name "health-check" \
  --schedule "every:30m" \
  -m "检查系统健康状态" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

### at 一次性延迟任务

指定精确时间执行一次，支持指数退避重试：

```bash
hotplex cron create \
  --name "deploy-verify" \
  --schedule "at:2026-05-15T09:00:00+08:00" \
  -m "验证生产环境部署是否成功" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

## 常见自动化模式

### 每日健康检查

```bash
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "执行以下检查并报告：
1. 运行 go test ./... 确认所有测试通过
2. 检查 go vet 是否有警告
3. 运行 golangci-lint
4. go vuln check 检查安全漏洞" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

### 部署后验证

```bash
hotplex cron create \
  --name "post-deploy" \
  --schedule "at:2026-05-11T14:30:00+08:00" \
  -m "部署刚完成，请验证：
1. curl /health 返回 200
2. 检查最近 5 分钟的错误日志
3. 验证数据库连接正常" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"
```

### 带生命周期的提醒

```bash
hotplex cron create \
  --name "water-reminder" \
  --schedule "every:30m" \
  -m "提醒：该喝水了！" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --max-runs 8 \
  --expires-at "2026-05-11T18:00:00+08:00"
```

任务在执行 8 次或到达过期时间后自动停止（以先到者为准）。

## 执行上下文

Cron 任务执行时，Gateway 通过 `platformKey`（`map[string]string` 形式）传递任务元数据，而非进程环境变量：

| platformKey 字段 | 说明 |
|------------------|------|
| `cron_job_id` | Cron 任务 ID |
| `bot_id` | 触发任务的 Bot ID（对应 CLI `--bot-id`） |
| `owner_id` | 任务所有者 ID（对应 CLI `--owner-id`） |
| `channel_id` | Slack Channel ID（结果投递目标） |
| `chat_id` | 飞书 Chat ID（结果投递目标） |

这些字段在 `executor.go` 中通过 `platformKey := map[string]string{"cron_job_id": job.ID}` 构造，传递给 `StartSession` 作为平台路由依据。

任务消息自动添加执行上下文前缀：

```
[cron:<job_id> <job_name>] <你的消息内容>
2026-05-10T09:00:00+08:00
```

Agent 能看到任务 ID、名称和执行时间，据此调整行为。

## 结合 Agent 能力

Cron 任务由完整的 AI Agent 执行，可以使用所有配置的 Tool。通过 `--allowed-tools` 限制可用工具：

```bash
# 只读检查任务，限制为只读工具
hotplex cron create \
  --name "safe-check" \
  --schedule "cron:0 9 * * *" \
  -m "检查代码质量" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --allowed-tools "read_file,grep,glob"
```

## 监控 Cron 任务

### 执行历史

```bash
hotplex cron history daily-health        # 查看历史
hotplex cron history daily-health --json # JSON 格式
```

### 任务状态追踪

每个 CronJob 维护运行时状态（`CronJobState`）：

| 字段 | 说明 |
|------|------|
| `last_status` | 最近一次执行结果（success/failed/timeout） |
| `consecutive_errors` | 连续错误次数 |
| `run_count` | 总执行次数 |
| `last_run_at_ms` | 最近执行时间 |
| `next_run_at_ms` | 下次计划执行时间 |

### Silent 模式

后台维护任务不需要投递结果给用户，启用 Silent 模式：

```bash
hotplex cron create \
  --name "cache-cleanup" \
  --schedule "cron:0 3 * * *" \
  -m "清理过期的临时文件和缓存" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --silent
```

Silent 模式下任务正常执行，但不发送消息卡片到飞书/Slack。

## 一次性任务 vs 周期性任务

| 维度 | at 类型 | cron/every 类型 |
|------|---------|----------------|
| 执行次数 | 1 次 | 持续执行 |
| 自动清理 | `--delete-after-run` 可选 | 需手动删除 |
| 生命周期 | `expires_at` 自然过期 | `max_runs` + `expires_at` |
| 重试机制 | 指数退避重试（`max_retries`） | 无自动重试 |
| 适用场景 | 部署验证、延迟通知 | 定期检查、持续监控 |

## 管理命令

```bash
# 列出所有任务
hotplex cron list [--json] [--enabled]

# 查看任务详情
hotplex cron get <id|name>

# 手动触发（需 Gateway 运行中）
hotplex cron trigger <id|name>

# 更新任务
hotplex cron update <id|name> --enabled=false

# 删除任务
hotplex cron delete <id|name>
```

## 执行流程

```
timerLoop tick → collectDue → CAS 并发槽 → executeJob → Worker 执行 → 结果投递
```

1. **collectDue**：收集 `NextRunAtMs <= now` 且未在执行中的任务
2. **并发槽 CAS**：`maxConcurrent`（默认 3）限制同时执行数，`atomic.Int32` 保护
3. **executeJob**：
   - `DeriveCronSessionKey(jobID, epoch)` 生成唯一 Session Key（独立 UUIDv5 命名空间）
   - 构造 Session，注入环境变量
   - 调用 Agent 执行任务
4. **结果投递**：按 `platform` 字段发送到飞书卡片或 Slack 消息

## 自然语言创建

Cron 功能通过 `go:embed cron-skill-manual.md` 注入 Agent 的 B 通道。Agent 自身了解 cron 的完整用法，可通过自然语言识别调度意图：

1. 用户发送"每天早上 9 点检查代码质量"
2. Brain 层 Router 识别为 cron 意图
3. Agent 自动调用 CLI 创建 cron job
4. 结果回传给用户

## Prometheus 监控

| 指标 | 类型 | 说明 |
|------|------|------|
| `cron_fires_total` | Counter | 任务触发次数 |
| `cron_errors_total` | Counter | 执行错误 |
| `cron_duration_seconds` | Histogram | 执行时长 |
