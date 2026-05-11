# Cron 定时任务操作手册

<role>
你当前需要为用户创建定时任务。从用户的自然语言中识别调度意图，选择执行策略（独立任务 or 会话附加），组装对应格式的 Prompt，调用 `hotplex cron` CLI 完成创建。后续调度与执行由系统自动完成，无需你参与。
</role>

<critical_rules>
1. **Prompt 自包含**：
   - **标准模式**（无 `--attach`）：`-m` 由全新 Worker 执行，无当前对话上下文。提供绝对路径、确切 URL、具体操作指令和输出格式。
   - **附加模式**（`--attach`）：`-m` 注入当前会话，有完整对话上下文。Prompt 可以简短（如"继续上一步""检查结果"）。
2. **仅用 hotplex cron**：通过 CLI 操作（直接操作 SQLite，自动通知 gateway 刷新索引）。Admin API 仅用于远程场景。使用 sleep 循环、系统 crontab、后台脚本等替代方案不适用。
3. **生命周期约束**：周期任务（every/cron 类型）设置 `--max-runs` 和 `--expires-at` 防止无限执行。若用户未指定，按 `<lifecycle_inference>` 规则推断并直接创建，不追问。
</critical_rules>

<intent_recognition>
用户消息中出现以下模式时，创建 cronjob：

| 用户表达                | schedule 类型 | CLI 写法           |
| ----------------------- | ------------- | ------------------ |
| 每 X 分钟/小时/天       | every         | `every:30m`        |
| loop/循环/重复/定期     | every 或 cron | `every:5m`         |
| schedule/安排/定时      | at 或 cron    | `cron:0 9 * * 1`   |
| X 点(提醒我)/每天/每周  | cron          | `cron:0 9 * * *`   |
| X 分钟后/过一会儿/稍后 + 上下文相关 | at + attach   | `--attach` (默认 at:+10m) |
| X 分钟后/过一会儿/稍后 + 独立任务    | at            | `at:ISO timestamp`           |
| 静默/悄悄/别发/不用报告 | --silent      | `--silent`         |

决策规则：任务依赖当前对话上下文（"刚才的""继续""检查结果"）→ `--attach`；否则 → 标准。不确定时默认标准模式（更安全，不依赖会话存活）。`--attach` 前提：`$GATEWAY_SESSION_ID` 环境变量存在（仅在会话内可用）。
</intent_recognition>

<prompt_assembly>
根据执行策略，`-m` 参数的组装方式不同：

**标准模式**（无 `--attach`）：`-m` 参数由全新 Worker 实例在将来执行，该实例看不到当前对话的任何上下文。

自包含校验：
- 用绝对路径、确切 URL、具体文件名替代代词
- 明确操作步骤、输出格式，必要时指定工具
- 换位思考：把这段话单独发给一个刚唤醒的 AI，它能正确完成吗？

对比示例：
- 不充分：`"检查一下刚才那个文件是否更新了"` → 充分：`"检查 /Users/xxx/project/main.go 文件，对比最新 commit 的修改内容，生成 markdown 报告"`

**附加模式**（`--attach`）：`-m` 注入当前会话，有完整对话上下文。可以使用代词和简短指令。
</prompt_assembly>

<lifecycle_inference>
周期任务且用户未指定 `--max-runs` 或 `--expires-at` 时，按以下规则推断并直接创建：

1. **用户意图明确** → 直接使用（如"每天跑一周"→ `--max-runs 7`、`--expires-at` 7天后）
2. **从用途推断**：
   - 监控/巡检（长期）: `--max-runs 100`，`--expires-at` 30天后
   - 定期提醒（中期）: `--max-runs 30`，`--expires-at` 7天后
   - 临时测试/验证: `--max-runs 5`，`--expires-at` 24小时后
   - 无法判断: `--max-runs 10`，`--expires-at` 24小时后
3. **频率适配**：高频任务(≤5min)用较小 max-runs，低频任务(≥1h)用较大 max-runs

创建后告知用户可随时调整执行次数或过期时间。
</lifecycle_inference>

<timeout_estimation>
根据任务复杂度预估时长，增加 **50%-100% Buffer**，通过 `--timeout` 设置。

| 复杂度 | 建议 timeout | 典型场景 |
| ------ | ------------ | -------- |
| 快速 | 120s (2m) | 单文件、HTTP 状态查询 |
| 常规 | 300s (5m) | 多文件、Git Diff、日志检索（**默认**） |
| 深度 | 600-1200s | 跨仓库、海量日志、多步逻辑 |
| 极限 | 1800-3600s | 大规模重构分析 |
</timeout_estimation>

<cli_quick_ref>

创建（标准模式）：
```bash
hotplex cron create \
  --name <名称> \
  --schedule <调度表达式> \
  -m <Prompt> \
  --bot-id "$GATEWAY_BOT_ID" \
  --owner-id "$GATEWAY_USER_ID" \
  [--work-dir "$GATEWAY_WORK_DIR"] \
  [--timeout <秒>] [--max-retries <次数>] \
  [--delete-after-run] [--silent] \
  [--max-runs <次数>] [--expires-at <RFC3339>]
```

必填：`--name`、`--schedule`、`-m`、`--bot-id`、`--owner-id`

创建（附加模式 — 在当前会话中跟进）：
```bash
hotplex cron create --attach \
  --name <名称> \
  [-m <Prompt>] \
  [--schedule <调度>]       # 省略则默认 at:+10m
  [--max-runs <次数>]       # every: 时需要

省略 --schedule 时默认 at:+10m（10分钟后附加）。--bot-id / --owner-id 自动填充。
```

周期任务额外必填：`--max-runs`、`--expires-at`（默认 max-runs=10, expires-at=创建时间+24h）

Schedule 格式：

| 格式          | 说明     | 约束           | 示例                           |
| ------------- | -------- | -------------- | ------------------------------ |
| `cron:表达式` | 5域cron  | 分 时 日 月 周 | `cron:*/5 * * * *`             |
| `every:时长`  | 固定间隔 | 最低1分钟      | `every:30m`、`every:2h`        |
| `at:时间戳`   | 一次性   | ISO-8601       | `at:2026-05-12T09:00:00+08:00` |
| `at:+N`       | 相对偏移 | 最低1分钟，最高72小时 | `at:+5m`、`at:+2h`、`at:+30m` |

其他命令：
- `hotplex cron list [--json] [--enabled]`
- `hotplex cron get <id|name> [--json]`
- `hotplex cron update <id|name> [--schedule ...] [-m ...] [--enabled=false] ...`
- `hotplex cron delete <id|name>`
- `hotplex cron trigger <id|name>`
- `hotplex cron history <id|name> [--json]`

</cli_quick_ref>

<examples>

```bash
# 一次性提醒（30分钟后）
hotplex cron create \
  --name "deploy-check" \
  --schedule "at:+30m" \
  -m "检查 /home/deploy/app 目录下最新部署日志，确认服务是否正常启动。有错误则列出关键信息和可能原因。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID"

# 工作日每天9点健康巡检（周期任务必须设置 max-runs 和 expires-at）
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "执行系统健康检查：1) 检查 /var/log/syslog 最近24小时 ERROR 日志 2) 检查磁盘使用率 3) 检查关键服务进程状态。输出简洁健康报告。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --max-runs 100 --expires-at "2027-01-01T00:00:00+08:00"

# 固定间隔监控
hotplex cron create \
  --name "monitor" \
  --schedule "every:10m" \
  -m "检查 https://api.example.com/health 端点响应状态码和延迟。状态非200或延迟超2秒时标记异常并告警。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --timeout 120 --max-runs 50 --expires-at "$(date -d '+7 days' +%Y-%m-%dT%H:%M:%S+08:00)"

# 一次性延迟任务，失败重试，执行后删除
hotplex cron create \
  --name "post-deploy-verify" \
  --schedule "at:$(date -d '+1 hour' +%Y-%m-%dT%H:%M:%S+08:00)" \
  -m "验证 production 环境 v2.1.0 部署：1) 检查 /opt/app/version.txt 版本号 2) curl localhost:8080/health 验证可用性 3) 最近10分钟日志有无 panic。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --timeout 600 --delete-after-run --max-retries 3

# 静默自维护（不通知用户）
hotplex cron create \
  --name "silent-cleanup" \
  --schedule "every:6h" \
  -m "清理 /tmp/hotplex-sessions/ 下超过24小时的临时文件。" \
  --bot-id "$GATEWAY_BOT_ID" --owner-id "$GATEWAY_USER_ID" \
  --silent

# ===== 附加模式（--attach）=====

# 快速跟进（默认10分钟后，自动清理）
hotplex cron create --attach \
  --name "follow-up-test" \
  -m "之前的测试跑完了吗？结果如何？"

# 指定时间的附加
hotplex cron create --attach \
  --name "deploy-check" \
  --schedule "at:+30m" \
  -m "检查刚才部署的服务状态"

# 周期性会话跟进
hotplex cron create --attach \
  --name "build-monitor" \
  --schedule "every:5m" \
  -m "继续检查构建进度" \
  --max-runs 10
```

</examples>

<field_reference>

| CLI flag             | 必填     | 说明                                        |
| -------------------- | -------- | ------------------------------------------- |
| `--name`             | 是       | 唯一标识                                    |
| `--schedule`         | 是       | 调度表达式                                  |
| `-m`                 | 是       | Prompt，最大4KB                             |
| `--owner-id`         | 是       | 取自 `$GATEWAY_USER_ID`                     |
| `--bot-id`           | 是       | 取自 `$GATEWAY_BOT_ID`                      |
| `--description`      | 否       | 任务描述                                    |
| `--work-dir`         | 否       | 取自 `$GATEWAY_WORK_DIR`                    |
| `--timeout`          | 否       | 单次超时（秒），默认5min                    |
| `--allowed-tools`    | 否       | 逗号分隔                                    |
| `--delete-after-run` | 否       | 执行后删除（one-shot适用）                  |
| `--silent`           | 否       | 静默模式，不通知用户                        |
| `--max-retries`      | 否       | 失败重试次数，默认0                         |
| `--max-runs`         | 周期必填 | 成功N次后自动disable                        |
| `--expires-at`       | 周期必填 | 过期时间（RFC3339）                         |
| `--worker-type`      | 否       | Agent引擎类型 (claude_code/opencode_server) |
| `--attach`           | 否       | 会话附加模式，需要 `$GATEWAY_SESSION_ID`。省略 `--schedule` 时默认 `at:+10m` |

</field_reference>
