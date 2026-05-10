---
title: Cron AI-native 定时任务调度器
weight: 5
description: HotPlex AI-native Cron 调度器设计：自然语言到定时任务、timerLoop 引擎、并发槽控制与平台投递
persona: developer
difficulty: advanced
---

# Cron AI-native 定时任务调度器

> HotPlex 的 Cron 不是传统的 crontab 包装器，而是一个 AI-native 的定时任务系统——用户用自然语言描述任务意图，系统自动识别并创建定时任务，Worker 执行后自动投递结果到 Slack/飞书。

## 核心问题

传统的定时任务系统（如 crontab、Airflow）有三个痛点：

1. **创建门槛高**：用户需要理解 cron 表达式语法（`0 9 * * 1-5`），知道如何配置执行环境，手动设置超时和重试。
2. **结果不可见**：任务执行完毕后，结果留在日志文件里，用户需要主动查看。对于 Slack/飞书用户来说，这打破了对话式交互的连贯性。
3. **与 Agent 割裂**：传统 cron 执行的是脚本或命令，而不是 AI Agent。无法利用 Agent 的上下文理解能力来处理复杂任务。

HotPlex Cron 的目标是让定时任务成为 AI Agent 能力的一部分——用户在对话中说"每天早上 9 点提醒我检查系统健康"，Agent 自动理解意图、创建任务、定时执行、将结果发回对话频道。

## 设计决策

### AI-native 创建流程

Cron 任务的创建不通过 CLI 手动输入 cron 表达式，而是通过 Agent 的意图识别：

```
用户："每天早上 9 点检查系统健康状态"
  -> Agent（通过 Skill Manual）识别为 cron 创建意图
  -> Agent 调用 hotplex cron create 命令
  -> Scheduler.CreateJob() 创建任务
  -> timerLoop.arm() 重新计算下次触发时间
```

Skill Manual（`cron-skill-manual.md`）通过 `go:embed` 编译进二进制文件，在 Scheduler 启动时释放到 `~/.hotplex/skills/cron.md`。Agent 在 B 通道的 `<skills>` 中读取这个手册，获得 cron 任务的创建语法和参数说明。

### 3 种调度类型

Cron 支持三种调度语义，覆盖从定时循环到一次性触发的全部场景：

| 类型 | Kind | 触发规则 | 使用场景 |
|------|------|---------|---------|
| Cron 表达式 | `ScheduleCron` | 标准 5 位 cron（分时日月周） | "每个工作日早上 9 点" |
| 固定间隔 | `ScheduleEvery` | 每 N 毫秒 | "每 30 分钟提醒一次" |
| 一次性 | `ScheduleAt` | ISO-8601 时间戳 | "明天下午 3 点部署" |

**Cron 表达式标准化**：`normalize.go` 处理常见的 cron 语法变体：
- `?` 替换为 `*`（Quartz 兼容）
- 周几名称映射（`MON` -> `1`，`SUN` -> `0`）

**最小间隔保护**：`every` 类型强制最小 60 秒间隔（`every_ms < 60000` 被拒绝），防止用户创建过于频繁的任务消耗 Worker 资源。

**时区支持**：Cron 表达式支持 `TZ` 字段，默认使用 `time.Local`。`at` 类型和 `every` 类型隐式使用系统时区。

### 并发槽控制：At-Most-Once 语义

Cron 调度器保证**同一个 Job 不会并发执行**——如果上一次执行还没完成，下一次触发会被跳过。这是通过 `timerLoop.running` 的 CAS（Compare-And-Swap）操作实现的：

```go
func (tl *timerLoop) tryAcquireSlot(max int) bool {
    for {
        cur := tl.running.Load()
        if int(cur) >= max {
            return false  // 全局并发上限
        }
        if tl.running.CompareAndSwap(cur, cur+1) {
            return true   // 成功获取槽位
        }
    }
}
```

**为什么用 CAS 而非 Mutex**：CAS 是无锁的，不会阻塞其他 goroutine。在高并发场景下（多个 Job 同时到期），CAS 避免了 mutex 竞争导致的延迟。默认 `max_concurrent_runs` 为 3，防止同时启动过多 Worker 进程导致资源耗尽。

并发上限是**全局的**——所有 Job 共享同一个并发池。当并发槽已满时，到期的 Job 被跳过（而非排队等待）。对于 `every` 类型的 Job，下一个 tick 周期会自动触发；对于 `cron` 类型，下一个调度时间会触发。

### 生命周期控制

每个 Job 支持可选的生命周期限制，实现"自毁式"定时任务：

| 参数 | 语义 | 效果 |
|------|------|------|
| `MaxRuns` | 最大执行次数 | 达到后自动 disable |
| `ExpiresAt` | 过期时间（RFC3339） | 过期后自动 disable |
| `DeleteAfterRun` | 执行后删除（at 类型） | 一次性任务的自清理 |
| `Enabled` | 启用/禁用开关 | 管理员暂停/恢复 |
| `Silent` | 静默模式 | 跳过结果投递 |
| `MaxRetries` | 最大重试次数（at 类型） | 指数退避重试上限 |

`at` 类型任务执行成功后自动 disable。如果设置了 `DeleteAfterRun`，则执行后直接从 DB 和内存索引中删除。连续调度错误（如无效的 cron 表达式）达到 5 次也会自动 disable。

## 内部机制

### timerLoop：Tick 引擎

`timerLoop` 是 Cron 调度器的核心引擎。它使用 `time.AfterFunc` 而非 `time.Ticker`，每次 tick 后重新计算到下一个到期 Job 的时间间隔：

```
arm(duration)
  -> timer.Stop()（取消旧定时器）
  -> time.AfterFunc(duration, onTick)

onTick()
  -> collectDue(now)（收集到期 Job）
  -> 对每个到期 Job：
       1. NextRun() 计算下次触发时间
       2. UpdateState() 持久化状态（At-Most-Once 保证）
       3. putJob() 更新内存索引
       4. tryAcquireSlot() 获取并发槽
       5. go executeJob()（异步执行）
  -> arm(nextTickDuration)（重新计算下次 tick）
```

**为什么用 AfterFunc 而非 Ticker**：

Ticker 以固定间隔触发，无论是否有到期 Job。AfterFunc 精确计算到下一个到期 Job 的时间间隔：
- 如果最近的 Job 在 5 秒后到期，`arm(5s)`
- 如果最近的 Job 在 1 小时后到期，`arm(1h)`
- 如果没有任何 Job，`arm(60s)`（最大间隔上限 `maxTimerInterval`）

这避免了空闲期间的无效 tick，减少 CPU 唤醒。

**At-Most-Once 保证**：

在执行 Job 之前，先计算并持久化 `next_run_at_ms`。如果 Gateway 在执行过程中崩溃，重启后 `loadFromDB` 会发现 `next_run_at_ms` 已经被推进到未来时间，不会重复执行。

对于 `at` 类型，`NextRun()` 在时间已过后返回零时间（`time.Time{}`），`next_run_at_ms` 被设为大负值，`collectDue` 跳过 `NextRunAtMs <= 0` 的条目，防止同一 tick 内重复执行。

### 内存索引与持久化

Scheduler 维护一个内存 map `jobs map[string]*CronJob` 作为索引，所有 CRUD 操作同步更新内存和 SQLite：

```
CreateJob():
  1. ValidateJob() -- 校验 schedule、payload
  2. NextRun() 计算初始 next_run_at_ms
  3. store.Create() 持久化到 SQLite
  4. s.jobs[id] = job 更新内存索引
  5. tickLoop.arm() 重新计算 tick 间隔
```

所有返回给调用方的 Job 都是 `Clone()` 的深拷贝，包含 map（`PlatformKey`）和 slice（`AllowedTools`）的独立复制，防止外部修改影响内部状态。

`ReloadIndex()` 方法支持外部触发（如 SIGHUP）重新从 DB 加载索引，用于 CLI 修改 Job 后的通知场景。

### 执行器：Executor

`Executor` 将 Cron Job 转化为一个完整的 Session 生命周期：

```
Execute(ctx, job, timeout):
  1. DeriveCronSessionKey(job.ID, now.UnixNano())
     -- 每次 epoch 产生唯一 Session ID
  2. bridge.StartSession() -- 创建并启动 Worker
     -- platform="cron" 触发 MCP 注入抑制
  3. sm.GetWorker() -- 获取 Worker 引用
  4. w.Input(prompt) -- 投递格式化 prompt
  5. waitForCompletion() -- 轮询 Session 状态直到非 RUNNING
```

**Cron 平台的 MCP 抑制**：

Cron Session 使用 `platform="cron"` 标识。Bridge 在构建 Worker 配置时检测到 cron 平台，注入空 MCP 配置 `{"mcpServers":{}}`，同时设置 `StrictMCPConfig=true`。这阻止 Cron Worker 加载任何 MCP 服务器，节省约 600 MB/worker 的内存开销。Cron 任务通常不需要文件系统访问或外部工具，MCP 抑制是合理的资源优化。

**Prompt 格式化**：

投递给 Worker 的 prompt 包含元信息和投递指令：

```
[cron:<job_id> <job_name>] <message>
<timestamp>

## 结果投递（必须执行）
任务执行完成后，必须通过以下命令将结果投递给用户...
```bash
hotplex slack send-message --text "结果内容" --channel <channel_id>
```
```

这种设计将投递能力交给 Agent 本身——Agent 执行完任务后，会看到投递指令并自动调用相应的 CLI 命令发送结果。

### 结果投递：Delivery

Delivery 模块提供两种投递机制：

**Gateway 投递（Delivery 模块）**：

```
Deliver(ctx, job, sessionKey):
  1. extract(ctx, sessionKey) -- 从 EventStore 提取最后的 assistant 回复
  2. deliverFn(ctx, platform, platformKey, response) -- 按 platform 路由
```

`extract` 是一个 `ResponseExtractor` 回调函数，从 Gateway 的 EventStore 中检索指定 Session 的最后一条 assistant 响应。`deliverFn` 是 `PlatformDeliverer` 回调，根据 platform 类型调用对应的 SDK。

**CLI 投递（prompt 内嵌）**：

通过 `HasCLIDelivery` 判断 Job 是否有足够的平台信息（如 `channel_id`）。如果有，投递指令会被嵌入 Cron prompt 中，让 Worker 自己执行。

**投递决策逻辑**：

```
if Silent -> 跳过投递
if HasCLIDelivery -> CLI 投递（Worker 自己发送）
else -> Gateway 投递（Delivery 模块通过 SDK 发送）
if platform="" || platform="cron" -> 不投递
```

**Silent 模式**：当 `job.Silent = true` 时，跳过所有投递逻辑。用于"静默检查"类型的任务——只执行不通知。

### Catch-up 机制：错过任务的补偿

Gateway 重启后，可能有 Job 在停机期间错过了触发时间。`loadFromDB` 实现了 catch-up 逻辑：

```
1. 清理 stale running_at_ms（上次崩溃可能留下的脏状态）
2. 对每个到期 Job：
   a. 计算宽限期（grace period）= 调度间隔 * 50%，上限 2 小时
   b. 如果在宽限期内 -> 立即执行（catch-up）
   c. 如果超出宽限期 -> 重新计算 next_run（recurring）或 disable（one-shot）
3. Catch-up 执行策略：
   a. 前 5 个立即执行
   b. 其余以 5 秒间隔 staggered 执行
```

**为什么是 50% interval**：如果一个每小时执行的任务在 30 分钟的窗口内被错过，补执行是合理的。但如果已经错过 2 小时以上，补执行可能产生过时的结果，不如跳过等下一次调度。

**staggered 执行**：大量 catch-up Job 同时执行会耗尽并发槽。前 5 个立即执行，其余每 5 秒启动一个，平滑资源消耗。

### at 类型的指数退避重试

`at` 类型 Job 失败时触发重试（recurring Job 不重试——下一个调度周期会自然重试）：

```
失败 1 次 -> 等待 30s  -> 重试
失败 2 次 -> 等待 1m   -> 重试
失败 3 次 -> 等待 5m   -> 重试
失败 4 次 -> 等待 15m  -> 重试
失败 5 次 -> 等待 1h   -> 重试（到达 max_retries 后停止）
```

重试仅针对**临时性错误**（timeout、rate limit、5xx），永久性错误（配置错误、认证失败）不会触发重试。

重试通过修改 `NextRunAtMs` 实现——将下次执行时间设为 `now + backoff_delay`。这让 timerLoop 的正常 tick 机制处理重试，无需额外的重试队列。

### YAML 批量导入

`LoadFromYAML` 支持 YAML 文件定义 Job，通过 `name` 字段实现幂等 upsert：

```yaml
# cron 表达式调度
- name: daily-health
  schedule: "cron:0 9 * * 1-5"
  message: "检查系统健康状态"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"

# every 固定间隔调度
- name: remind-water
  schedule: "every:30m"
  schedule_every_ms: 1800000  # 可选：直接指定毫秒数（覆盖 every 解析）
  message: "提醒喝水"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"
  max_runs: 6
  expires_at: "2026-05-11T00:00:00+08:00"

# at 一次性调度
- name: deploy-prod
  schedule: "at:2026-05-15T14:00:00+08:00"
  schedule_at: "2026-05-15T14:00:00+08:00"  # 可选：显式 ISO-8601 时间戳
  message: "执行生产环境部署"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"
  delete_after_run: true
  max_retries: 3
```

YAML 导入在 Gateway 启动时执行，覆盖同名 Job 的定义但保留运行状态（run_count、last_run_at_ms 等）。这使得 Job 定义可以版本化管理（存入 Git 仓库）。

### 关闭与优雅退出

`Shutdown()` 执行三步优雅退出：

```
1. closed.Store(true) -- 阻止新 Job 启动
2. cancelFn() -- 取消 ctx，停止 timerLoop
3. tickLoop.stop() -- 停止定时器
4. wg.Wait() -- 等待所有执行中的 Job 完成
```

`ctx.Done()` 提供超时控制。如果在超时前 `wg.Wait()` 未返回，说明有 Job 仍在执行，记录 warning 日志后退出。

## 权衡与限制

1. **全局并发上限不区分优先级**：所有 Job 共享同一个并发池（默认 3）。一个低优先级的批量任务可能占满槽位，阻塞高优先级的通知任务。当前没有基于 Job 优先级的队列调度。

2. **轮询式完成检测**：`waitForCompletion` 每 2 秒轮询 Session 状态，而非通过事件驱动。这增加了最多 2 秒的检测延迟，但实现简单且不依赖事件订阅机制。

3. **无分布式协调**：整个 Scheduler 运行在单个 Gateway 进程中。如果运行多个 Gateway 实例，需要确保只有一个实例运行 Cron（否则会重复执行）。当前依赖外部机制（如 systemd 单实例）保证。

4. **at 类型无持久重试队列**：`at` 类型的重试通过 `scheduleRetry` 在内存中调度。如果 Gateway 在重试等待期间重启，重试机会丢失。持久重试需要将重试状态写入 DB，当前未实现。

5. **CLI 投递的可靠性**：CLI 投递依赖 Worker 执行 `hotplex slack send-message` 命令。如果 Worker 崩溃或忽略投递指令，结果会丢失。Gateway 投递（Delivery 模块）更可靠，但需要额外的基础设施支持（EventStore + SDK 回调）。

## 参考

- `internal/cron/cron.go` -- Scheduler 核心：内存索引、CRUD、catch-up
- `internal/cron/timer.go` -- timerLoop tick 引擎：collectDue -> CAS -> executeJob
- `internal/cron/types.go` -- CronJob/CronSchedule/CronPayload 数据结构 + Clone()
- `internal/cron/schedule.go` -- 三种调度：cron / every / at + NextRun 计算
- `internal/cron/executor.go` -- Worker 执行适配：Session 创建、prompt 格式化、投递指令
- `internal/cron/delivery.go` -- 结果投递：extract + PlatformDeliverer 回调
- `internal/cron/store.go` -- SQLite 持久化：ErrJobNotFound 哨兵、jobColumns 常量
- `internal/cron/loader.go` -- YAML 批量导入：name 幂等 upsert
- `internal/cron/skill.go` -- go:embed Skill Manual
- `internal/cron/retry.go` -- at 类型指数退避重试
- `internal/cron/normalize.go` -- cron 表达式标准化
