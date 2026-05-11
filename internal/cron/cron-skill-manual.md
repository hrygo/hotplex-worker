# Cron 定时任务操作手册

## 概述

HotPlex Gateway 内置 AI-native 定时任务系统。你当前正在阅读此手册，意味着你需要**为用户创建一个定时任务**。

### 你在整个流程中的位置

```
┌─────────────────────────────────────────────────┐
│ Cron 生命周期                                    │
├─────────────────────────────────────────────────┤
│                                                  │
│  ① 规划与创建 ──→ ② 调度执行 ──→ ③ 任务完成     │
│  ▲ 你在这里        系统自动        系统自动       │
│                                                  │
└─────────────────────────────────────────────────┘
```

**你的职责**：
- 从用户的自然语言中识别调度意图（时间、周期、是否静默）
- 精心组装 Prompt（`-m` 参数）—— **这是最关键的步骤**
- 调用 `hotplex cron create` CLI 创建任务

创建完成后，后续的调度、执行、结果处理均由系统自动完成，无需你参与。

**核心逻辑**：
- **职责分离**：你只负责任务的逻辑指令，系统负责生命周期和结果回传。
- **自动交付**：如果创建时携带了平台信息（默认自动继承），系统会在任务执行后自动追加回传指令块，引导 Worker 完成投递并自动退出。
- **资源回收**：Worker 任务完成后会立即被系统强制回收，不作为常驻进程。

**操作方式**：`hotplex cron` CLI（本机直接操作 SQLite，自动通知 gateway 刷新索引）。Admin API 仅用于远程场景。

## 意图识别

用户消息中出现以下模式时，应创建 cronjob：

| 用户表达                                 | schedule 类型     | CLI 写法           |
| ---------------------------------------- | ----------------- | ------------------ |
| 每 X 分钟/小时/天                        | `every`           | `every:30m`        |
| loop / 循环 / 重复 / 定期                | `every` 或 `cron` | `every:5m`         |
| schedule / 安排 / 定时                   | `at` 或 `cron`    | `cron:0 9 * * 1`   |
| X 点(提醒我) / 每天/每周                 | `cron`            | `cron:0 9 * * *`   |
| X 分钟后 / 过一会儿 / 稍后               | `at`              | `at:ISO timestamp` |
| 提醒我                                   | `at`              | `at:ISO timestamp` |
| 静默 / 悄悄 / 别发 / 不用报告 / 无需通知 | `--silent`        | `--silent`         |

**禁止**：用 `sleep` 循环、系统 crontab、Claude CronCreate、后台脚本等替代方案。

## Prompt 组装指南（关键）

当你使用 `-m` 参数传入将来由新 Worker 执行的 Prompt 时，**必须遵守以下「零上下文」原则**。因为未来的 Agent 是一个全新的实例，**完全不知道你当前的对话历史和上下文**。

### 原则 1：上下文必须自包含

提供目标机器、绝对路径、确切的文件名或 URL。不能用代词。

- ❌ `-m "检查一下刚才那个文件是否更新了"`
- ✅ `-m "请检查 /Users/xxx/project/main.go 文件，对比最新 commit 的修改内容，生成报告"`

### 原则 2：不包含回传逻辑

**不要**在 `-m` 中写“发送结果到 Slack”之类的话。系统会自动在 Prompt 末尾追加包含 `hotplex slack` 或 `lark-cli` 的交付指令块。

### 原则 3：提供充足的操作指令

告诉未来的 Agent 具体要做什么、输出什么格式，如有必要可指定使用的工具。

- ❌ `-m "帮我跟进一下工单"`
- ✅ `-m "请查询 Jira 中状态为 Open 且分配给 user@example.com 的工单，按优先级 P0→P3 排序，输出 markdown 列表，包含标题、优先级、创建时间"`

### 原则 3：换位思考校验

在填入 `-m` 之前，问自己："如果把这段话单独发给一个刚刚被唤醒、没有看过之前任何对话的 AI 助手，它能顺利且正确地完成任务吗？"

## 环境变量

**当前 Worker 进程**已注入以下环境变量，创建 cronjob 时直接使用：

| 环境变量             | 来源（Slack） | 来源（飞书） | 对应 CLI flag | 示例值   |
| -------------------- | ------------- | ------------ | ------------- | -------- |
| `GATEWAY_PLATFORM`   | "slack"       | "feishu"     | —             | slack    |
| `GATEWAY_BOT_ID`     | botID         | botOpenID    | `--bot-id`    | B12345   |
| `GATEWAY_USER_ID`    | userID        | userID       | `--owner-id`  | U12345   |
| `GATEWAY_CHANNEL_ID` | channel_id    | chat_id      | —             | C12345   |
| `GATEWAY_THREAD_ID`  | thread_ts     | message_id   | —             | 1234.56  |
| `GATEWAY_TEAM_ID`    | teamID        | "" (空)      | —             | T12345   |
| `GATEWAY_SESSION_ID` | session ID    | session ID   | —             | uuid-v5  |
| `GATEWAY_WORK_DIR`   | workDir       | workDir      | `--work-dir`  | /tmp/xxx |

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
  [--worker-type <引擎类型: claude_code|opencode_server>] \
  [--allowed-tools <逗号分隔>] \
  [--delete-after-run] \
  [--silent] \
  [--max-retries <次数>] \
  [--max-runs <次数>] \
  [--expires-at <RFC3339>]
```

**必填**：`--name`、`--schedule`、`-m`、`--bot-id`、`--owner-id`

**周期任务额外必填**：`--max-runs`、`--expires-at`（`every`/`cron` 类型必须设置，防止无限执行）

> 若未指定，系统自动填充默认值：`--max-runs 10`、`--expires-at` = 创建时间 +24 小时。

### 生命周期智能推断

用户创建周期任务时，若未明确指定 `--max-runs` 或 `--expires-at`，**不要追问用户**，应按以下规则推断合理值并直接创建，创建后告知用户可随时更新：

**推断规则**（按优先级）：

1. **用户意图明确** → 直接使用用户值（如"每天跑一周"→ `--max-runs 7`、`--expires-at` = 7 天后）
2. **从用途推断** → 根据任务性质设定合理生命周期：

| 用途 | `--max-runs` | `--expires-at` |
|------|-------------|----------------|
| 监控/巡检（长期运行） | 100 | 30 天后 |
| 定期提醒（中期） | 30 | 7 天后 |
| 临时测试/验证 | 5 | 24 小时后 |
| 无法判断 | 10（系统默认） | 24 小时后（系统默认） |

3. **从频率推断** → 高频任务（≤5min）用较小 max-runs，低频任务（≥1h）用较大 max-runs

**回复模板**：

> 已创建周期任务 `xxx`，schedule: `every:30m`，max-runs: 30，expires: 2026-05-18。
> 如需调整执行次数或过期时间，可发送：`更新定时任务 xxx --max-runs 50`

**schedule 格式**（`kind:value` 前缀）：

| 格式          | 说明           | 约束             | 示例                           |
| ------------- | -------------- | ---------------- | ------------------------------ |
| `cron:表达式` | 标准 5 域 cron | `分 时 日 月 周` | `cron:*/5 * * * *`             |
| `every:时长`  | 固定间隔       | 最低 1 分钟      | `every:30m`、`every:2h`        |
| `at:时间戳`   | 一次性         | ISO-8601         | `at:2026-05-12T09:00:00+08:00` |

### 完整示例

```bash
# 一次性提醒（30分钟后）
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:$(date -d '+30 minutes' +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "请检查 /home/deploy/app 目录下最新的部署日志，确认服务是否正常启动。如果有错误，列出关键错误信息和可能的原因。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 工作日每天 9 点（周期任务必须设置 max-runs 和 expires-at）
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "请执行以下系统健康检查：1) 检查 /var/log/syslog 最近 24 小时的 ERROR 日志 2) 检查磁盘使用率 3) 检查关键服务进程状态。输出简洁的健康报告。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --worker-type "claude_code" \
  --max-runs 100 --expires-at "2027-01-01T00:00:00+08:00"

# 固定间隔监控
hotplex cron create \
  --name "monitor" \
  --schedule "every:10m" \
  -m "检查 https://api.example.com/health 端点的响应状态码和延迟。如果状态非 200 或延迟超过 2 秒，标记为异常并给出告警摘要。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --timeout 120 --max-runs 50 --expires-at "$(date -d '+7 days' +%Y-%m-%dT%H:%M:%S+08:00)"

# 有生命周期的周期任务：30 分钟一次，最多 6 次，24 小时后过期
hotplex cron create \
  --name "hydration-remind" \
  --schedule "every:30m" \
  -m "提醒用户喝水，保持健康。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --max-runs 6 --expires-at "$(date -d '+24 hours' +%Y-%m-%dT%H:%M:%S+08:00)"

# 一次性延迟任务，失败自动重试，执行后自动删除
hotplex cron create \
  --name "post-deploy-verify" \
  --schedule "at:$(date -d '+1 hour' +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "请验证 production 环境上 v2.1.0 版本的部署结果：1) 检查 /opt/app/version.txt 中的版本号 2) 运行 curl localhost:8080/health 验证服务可用性 3) 检查最近 10 分钟日志有无 panic。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --delete-after-run --max-retries 3

# 静默自维护任务（不通知用户）
hotplex cron create \
  --name "silent-cleanup" \
  --schedule "every:6h" \
  -m "清理 /tmp/hotplex-sessions/ 下超过 24 小时的临时文件。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --silent
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
hotplex cron update <id|name> [--schedule ...] [-m ...] [--worker-type ...] [--enabled=false] ...

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

用户说"每天早上9点检查服务状态" → `hotplex cron create --schedule "cron:0 9 * * *" --max-runs N --expires-at "RFC3339"`。

### 延迟执行

用户说"2小时后再跑一次测试" → `hotplex cron create --schedule "at:..."`，`at` 设为当前时间 +2h。

### 静默自维护

需要定期清理但不需要看到结果 → 使用 `--silent` 标志。

## 字段速查

| 字段               | CLI flag             | 必填       | 说明                                        |
| ------------------ | -------------------- | ---------- | ------------------------------------------- |
| `name`             | `--name`             | **是**     | 唯一标识                                    |
| `schedule`         | `--schedule`         | **是**     | 调度表达式                                  |
| `payload.message`  | `-m`                 | **是**     | Prompt，最大 4KB                            |
| `owner_id`         | `--owner-id`         | **是**     | 取自 `$GATEWAY_USER_ID`                     |
| `bot_id`           | `--bot-id`           | **是**     | 取自 `$GATEWAY_BOT_ID`                      |
| `description`      | `--description`      | 否         | 任务描述                                    |
| `work_dir`         | `--work-dir`         | 否         | 取自 `$GATEWAY_WORK_DIR`                    |
| `timeout_sec`      | `--timeout`          | 否         | 单次超时（秒），默认 5min                   |
| `allowed_tools`    | `--allowed-tools`    | 否         | 逗号分隔                                    |
| `delete_after_run` | `--delete-after-run` | 否         | 执行后自动删除（one-shot 适用）             |
| `silent`           | `--silent`           | 否         | 静默模式，不通知用户                        |
| `max_retries`      | `--max-retries`      | 否         | 失败最大重试次数，默认 0                    |
| `max_runs`         | `--max-runs`         | 周期必填   | 成功执行 N 次后自动 disable                 |
| `expires_at`       | `--expires-at`       | 周期必填   | 过期时间（RFC3339），到期自动 disable       |
| `worker_type`      | `--worker-type`      | 否         | AI Agent 引擎类型 (claude_code/opencode_server) |

## Admin API（备选）

仅用于远程/非本机场景。CLI 优先。

基础路径：`${HOTPLEX_ADMIN_API_URL}/api/cron/jobs`
认证：`Authorization: Bearer $HOTPLEX_ADMIN_TOKEN`

| 操作 | 方法   | 路径                       | Body          |
| ---- | ------ | -------------------------- | ------------- |
| 创建 | POST   | `/api/cron/jobs`           | JSON (见下方) |
| 列出 | GET    | `/api/cron/jobs`           | —             |
| 详情 | GET    | `/api/cron/jobs/{id}`      | —             |
| 触发 | POST   | `/api/cron/jobs/{id}/run`  | —             |
| 更新 | PATCH  | `/api/cron/jobs/{id}`      | JSON          |
| 删除 | DELETE | `/api/cron/jobs/{id}`      | —             |
| 历史 | GET    | `/api/cron/jobs/{id}/runs` | —             |

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
  "silent": false,
  "max_retries": 0,
  "max_runs": 0,
  "expires_at": ""
}
```

Schedule JSON 格式：

| kind    | 字段                                                        | 示例                       |
| ------- | ----------------------------------------------------------- | -------------------------- |
| `at`    | `{"kind":"at","at":"2026-05-12T09:00:00+08:00"}`            | 一次性                     |
| `every` | `{"kind":"every","every_ms":1800000}`                       | 固定间隔（ms），最低 60000 |
| `cron`  | `{"kind":"cron","expr":"0 9 * * 1-5","tz":"Asia/Shanghai"}` | 5 域表达式                 |

## 错误处理与重试

| 场景                      | 行为                                                           |
| ------------------------- | -------------------------------------------------------------- |
| 创建失败                  | 检查必填字段、schedule 格式、prompt ≤ 4KB                      |
| 执行超时                  | 按 `timeout_sec` 截断（默认 5min），状态标记 `timeout`         |
| 执行失败                  | 自动指数退避重试（1min → 5min → 25min），受 `max_retries` 限制 |
| 达到 max_runs             | 成功执行次数达到上限后自动 disable                             |
| 超过 expires_at           | 到期后自动 disable                                             |
| 连续 5 次调度错误         | Job 自动 disable，需手动重新启用                               |
| 连续 10 次执行失败        | Job 自动 disable，需手动重新启用                               |
| One-shot 执行完成         | 自动 disable；若 `delete_after_run: true` 则自动删除           |
| 网关重启                  | 启动时自动加载未完成 Job，宽限期内的错过任务立即补执行         |
| CLI 修改后 gateway 未刷新 | CLI 自动发送 SIGHUP，若失败会在 stderr 输出警告                |
