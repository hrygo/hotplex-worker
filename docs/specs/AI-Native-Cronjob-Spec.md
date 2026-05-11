# AI-Native Cronjob Technical Specification

**Date**: 2026-05-09
**Version**: 1.0
**Status**: Draft

---

## 1. Overview

HotPlex Gateway 内置 AI-native 定时任务调度系统。Job 的 payload 是自然语言 prompt，由 worker（Claude Code / OpenCode Server）全权执行；worker 自身可通过注入的 cron tool 创建和管理 cronjob。

### Core Properties

| Property | Description |
|----------|-------------|
| Job = Prompt | Payload 是自然语言文本，worker 全权解读和执行 |
| Worker 自管理 | Worker 通过 cron tool CRUD 操作，元认知通过 agentconfig 通道注入 |
| Isolated Session | 每个 job fire 创建独立 session + worker，执行完自动进入 GC |
| Result Delivery | 执行结果自动路由到创建来源平台（Slack/飞书/webchat） |
| 持久化 | SQLite 表（复用现有 DB 连接）+ YAML 配置文件导入 |

### Constraints

- Cron session 占用 pool quota（受 `max_concurrent_runs` + `pool.max_size` 双重限制）
- `kind=at` one-shot job 不支持 sub-minute 精度（最小 1 分钟）
- Worker 必须支持 `--session-id` 模式
- Job prompt 最大 4KB

### Non-Goals

- `system_event` payload（注入已有 session）→ 已由 `attached_session` 实现，见 [Cron-Fast-Path-Spec.md](Cron-Fast-Path-Spec.md)
- Job chaining (`context_from`)
- Wake gate / pre-check script
- Per-job model/provider override
- Distributed scheduling（单进程足够）

---

## 2. Data Model

### 2.1 CronSchedule

```go
type Schedule string

const (
    ScheduleAt    Schedule = "at"    // one-shot: ISO-8601 timestamp
    ScheduleEvery Schedule = "every" // recurring: fixed interval in ms
    ScheduleCron  Schedule = "cron"  // recurring: cron expression
)

type CronSchedule struct {
    Kind    Schedule `json:"kind"`
    At      string   `json:"at,omitempty"`       // kind=at: "2026-05-10T09:00:00+08:00"
    EveryMs int64    `json:"every_ms,omitempty"`  // kind=every: 1800000 (30min)
    Expr    string   `json:"expr,omitempty"`      // kind=cron: "0 9 * * 1-5"
    TZ      string   `json:"tz,omitempty"`        // timezone, default Local
}
```

### 2.2 CronPayload

```go
type Payload string

const (
    PayloadAgentTurn       Payload = "agent_turn"        // isolated session → will rename to isolated_session
    PayloadSystemEvent     Payload = "system_event"      // reserved
    PayloadAttachedSession Payload = "attached_session"  // inject existing session — see Fast Path Spec
)

type CronPayload struct {
    Kind            Payload  `json:"kind"`
    Message         string   `json:"message"`                 // the prompt
    TargetSessionID string   `json:"target_session_id,omitempty"` // attached_session: target session
    AllowedTools    []string `json:"allowed_tools,omitempty"`  // tool restriction
}
```

### 2.3 CronJobState

```go
type JobStatus string

const (
    StatusSuccess JobStatus = "success"
    StatusFailed  JobStatus = "failed"
    StatusTimeout JobStatus = "timeout"
)

type CronJobState struct {
    NextRunAtMs     int64     `json:"next_run_at_ms"`
    LastRunAtMs     int64     `json:"last_run_at_ms"`
    RunningAtMs     int64     `json:"running_at_ms"`
    LastStatus      JobStatus `json:"last_status,omitempty"`
    ConsecutiveErrs int       `json:"consecutive_errors"`
    LastRunID       string    `json:"last_run_id,omitempty"`  // session ID of last run
}
```

### 2.4 CronJob

```go
type CronJob struct {
    ID             string            `json:"id"`
    Name           string            `json:"name"`
    Description    string            `json:"description,omitempty"`
    Enabled        bool              `json:"enabled"`
    Schedule       CronSchedule      `json:"schedule"`
    Payload        CronPayload       `json:"payload"`
    WorkDir        string            `json:"work_dir,omitempty"`
    BotID          string            `json:"bot_id,omitempty"`
    OwnerID        string            `json:"owner_id,omitempty"`
    Platform       string            `json:"platform,omitempty"`         // slack/feishu/webchat/cron
    PlatformKey    map[string]string `json:"platform_key,omitempty"`     // delivery routing
    TimeoutSec     int               `json:"timeout_sec,omitempty"`
    DeleteAfterRun bool              `json:"delete_after_run,omitempty"`
    MaxRetries     int               `json:"max_retries,omitempty"`      // kind=at one-shots
    State          CronJobState      `json:"state"`
    CreatedAtMs    int64             `json:"created_at_ms"`
    UpdatedAtMs    int64             `json:"updated_at_ms"`
}
```

### 2.5 Delivery Routing

Delivery target 在创建时从当前 session 的 platform context 自动推导：

| 创建来源 | Delivery 路径 |
|---------|-------------|
| Slack session | 原 channel/thread |
| 飞书 session | 原 chat_id |
| WebChat | 不投递（用户可在 WebChat 查看历史） |
| YAML 配置 | 按 `platform` + `platform_key` 配置 |

Worker 响应以 `[SILENT]` 开头时抑制投递。

---

## 3. SQLite Schema

复用现有 session store 的 `*sql.DB` 连接，新增 migration `003_cron_jobs_table.sql`：

```sql
-- +goose Up

CREATE TABLE IF NOT EXISTS cron_jobs (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    schedule_data    TEXT NOT NULL,          -- JSON: {at, every_ms, expr, tz}
    payload_kind     TEXT NOT NULL DEFAULT 'isolated_session' CHECK(payload_kind IN ('isolated_session', 'system_event', 'attached_session')),
    payload_data     TEXT NOT NULL,          -- JSON: {kind, message, allowed_tools}
    work_dir         TEXT NOT NULL DEFAULT '',
    bot_id           TEXT NOT NULL DEFAULT '',
    owner_id         TEXT NOT NULL DEFAULT '',
    platform         TEXT NOT NULL DEFAULT '',
    platform_key     TEXT NOT NULL DEFAULT '{}',  -- JSON
    timeout_sec      INTEGER NOT NULL DEFAULT 0,
    delete_after_run INTEGER NOT NULL DEFAULT 0 CHECK(delete_after_run IN (0, 1)),
    max_retries      INTEGER NOT NULL DEFAULT 0,
    state            TEXT NOT NULL DEFAULT '{}',   -- JSON: runtime state
    created_at       INTEGER NOT NULL,             -- Unix ms
    updated_at       INTEGER NOT NULL              -- Unix ms
);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_owner  ON cron_jobs(owner_id);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));

-- +goose Down
DROP TABLE IF EXISTS cron_jobs;
```

---

## 4. Package Architecture

### 4.1 Directory Layout

```
internal/cron/
├── cron.go       # Scheduler: Start/Shutdown, public API
├── store.go      # SQLite CRUD
├── timer.go      # Timer loop: armTimer → onTick → collectDue → execute → re-arm
├── executor.go   # Job 执行 (isolated_session): Bridge.StartSession + Input + waitForCompletion
├── callback.go   # Attached session (attached_session): find/resume + inject — see Fast Path Spec
├── schedule.go   # cron 表达式求值 + next-run 计算
├── loader.go     # YAML → SQLite 导入
├── delivery.go   # 结果投递: route to platform adapter
├── types.go      # 数据模型
├── normalize.go  # 输入校验 + prompt 安全扫描
└── cron_test.go
```

### 4.2 Scheduler

```go
type Scheduler struct {
    log      *slog.Logger
    store    Store
    executor *Executor
    deliver  *Delivery

    mu        sync.Mutex
    jobs      map[string]*CronJob // in-memory index
    timer     *time.Timer

    maxConcurrent int
    running       atomic.Int32
    closed        atomic.Bool

    ctx    context.Context
    cancel context.CancelFunc
}
```

**生命周期**：

```
New(log, db, bridge, hub, cfgStore, adapterResolver)
  │
Start(ctx)
  ├─ loadFromDB()
  ├─ loadFromYAML(cfg.Cron.Jobs)
  └─ armTimer()
       │
       ├─ setTimeout(min(nextRunAt) - now, max 60s)
       │
onTick()
  ├─ collectDueJobs(now)
  ├─ advanceNextRun(due)              // at-most-once
  ├─ execute(due) with concurrency cap
  ├─ persist state
  └─ re-arm timer
       │
Shutdown(ctx)
  ├─ cancel timer
  └─ wait running jobs drain (timeout)
```

### 4.3 Executor

```go
func (e *Executor) Execute(ctx context.Context, job *CronJob) error {
    // 1. Derive deterministic session key (UUIDv5, platform="cron")
    sessionKey := session.DerivePlatformSessionKey(
        job.OwnerID, "claude_code",
        session.PlatformContext{
            Platform: "cron",
            BotID:    job.BotID,
            UserID:   job.OwnerID,
            WorkDir:  job.WorkDir,
            ChatID:   job.ID,
        },
    )

    // 2. Create session + start worker
    platformKey := map[string]string{"cron_job_id": job.ID}
    if err := e.bridge.StartSession(ctx, sessionKey, job.OwnerID, job.BotID,
        worker.TypeClaudeCode, job.Payload.AllowedTools, job.WorkDir,
        "cron", platformKey, job.Name,
    ); err != nil {
        return fmt.Errorf("start cron session: %w", err)
    }

    // 3. Build prompt with metadata wrapper
    prompt := fmt.Sprintf("[cron:%s %s] %s\n%s", job.ID, job.Name,
        job.Payload.Message, time.Now().Format(time.RFC3339))

    // 4. Deliver prompt to worker
    w := e.sm.GetWorker(sessionKey)
    if w == nil {
        return fmt.Errorf("worker not found after start")
    }
    if err := w.Input(ctx, prompt, nil); err != nil {
        return fmt.Errorf("input prompt: %w", err)
    }

    // 5. Wait for done/error event with timeout
    return e.waitForCompletion(ctx, sessionKey, timeout)
}
```

调用 `gateway.Bridge.StartSession`（非 `messaging.Bridge`），直接走完整 CreateWithBot → AttachWorker → injectAgentConfig → Start → Transition 链路。

### 4.4 Session Key Strategy

同一 job 的多次 fire 共享同一 session key（复用 conversation history）。Session 在 GC idle_timeout 后自动清理。

### 4.5 Delivery

```go
type Delivery struct {
    log     *slog.Logger
    hub     *gateway.Hub
    resolve func(platform string) messaging.PlatformAdapterInterface
}

func (d *Delivery) Deliver(ctx context.Context, job *CronJob, response string) error {
    if strings.HasPrefix(strings.TrimSpace(response), "[SILENT]") {
        return nil
    }
    if job.Platform == "" || job.Platform == "cron" {
        return nil
    }
    adapter := d.resolve(job.Platform)
    if adapter == nil {
        return fmt.Errorf("no adapter for platform %s", job.Platform)
    }
    return adapter.SendCronResult(ctx, job.PlatformKey, response)
}
```

Response 由 Executor 从 cron session 的最终 `message` / `done` 事件中提取。

---

## 5. Error Handling

### 5.1 Exponential Backoff

```go
var backoffDurations = []time.Duration{
    30 * time.Second,
    1 * time.Minute,
    5 * time.Minute,
    15 * time.Minute,
    1 * time.Hour,
}
```

连续失败时按序取值，达到上限后固定 1h。

### 5.2 Auto-Disable

连续 5 次调度计算错误（非执行错误）→ 自动 disable job，写日志告警。

### 5.3 One-Shot Retry

`kind=at` 的 one-shot job：

| 结果 | 处理 |
|------|------|
| 成功 | disable |
| 临时错误（timeout/5xx/rate-limit） | 重试最多 `max_retries` 次（默认 3） |
| 永久错误 | disable |

### 5.4 Startup Catch-Up

Gateway 启动时：

1. 清理 stale `running_at_ms`（上次 crash 遗留）
2. 识别 missed jobs（`next_run_at_ms < now`）
3. 计算 grace period（schedule 间隔的 50%，上限 2h），超期跳过，未超期立即执行
4. 最多立即执行 5 个 missed jobs，剩余 stagger 5s 间隔

### 5.5 At-Most-Once

`onTick` 先推进 `next_run_at_ms` 再执行，确保 crash 后不重复。

---

## 6. Worker Meta-Cognition

Worker 是外部进程（Claude Code CLI / OpenCode Server），tool call 在进程内执行，Gateway 无法拦截。因此采用**渐进式知识注入**：元认知注入少量意识信息，完整管理手册按需从磁盘读取。

### 6.1 两层架构

| 层级 | 内容 | 大小 | 注入时机 |
|------|------|------|---------|
| 意识层 | Cron 可用性声明 + 手册路径 | ~200 chars | 始终注入（META-COGNITION.md go:embed） |
| 知识层 | Admin API 端点、请求格式、curl 示例 | ~3-4 KB | Worker 按需 `cat` 读取 |

### 6.2 意识层：更新 META-COGNITION.md

在 `internal/agentconfig/META-COGNITION.md` 末尾追加 Cron 能力声明。该文件通过 `go:embed` 始终注入 B 通道 `<hotplex>` 区域，无需额外注入逻辑。

新增内容：

```markdown
## 6. Cron 定时任务

你可以通过 Admin API 创建和管理定时任务（cronjob）。用于定时提醒、定期巡检、延迟后续操作。
当你需要操作 cronjob 时，先读取操作手册：

    cat ~/.hotplex/skills/cron.md

然后按手册指引使用 curl 调用 Admin API。环境变量 `HOTPLEX_ADMIN_API_URL` 和 `HOTPLEX_ADMIN_TOKEN` 已预配置。
```

### 6.3 知识层落盘（go:embed → 文件系统）

```go
// internal/cron/skill.go

//go:embed cron-skill-manual.md
var embeddedManual string

// SkillManual 返回完整管理手册内容。
func SkillManual() string { return embeddedManual }
```

Gateway 启动时，Scheduler.Start() 将手册释放到磁盘：

```go
func (s *Scheduler) Start(ctx context.Context) error {
    // ... load jobs, arm timer ...
    // Release skill manual to disk
    if err := s.releaseSkillManual(); err != nil {
        s.log.Warn("cron: failed to release skill manual", "err", err)
    }
    return nil
}

func (s *Scheduler) releaseSkillManual() error {
    dir := filepath.Join(os.Getenv("HOME"), ".hotplex", "skills")
    _ = os.MkdirAll(dir, 0o755)
    return os.WriteFile(filepath.Join(dir, "cron.md"), []byte(SkillManual()), 0o644)
}
```

`cron-skill-manual.md` 包含：Admin API 端点列表、请求/响应 JSON 格式、schedule 三种类型说明、curl 完整示例、错误处理指引。

### 6.4 环境变量注入

Gateway 通过现有 `workerEnv` 机制注入 Admin API 凭证：

```go
// cron 启用时追加到 workerEnv
workerEnv = append(workerEnv,
    "HOTPLEX_ADMIN_API_URL=http://localhost:9999",
    "HOTPLEX_ADMIN_TOKEN="+adminToken,
)
```

### 6.5 Worker 侧执行流程

```
用户说 "30分钟后提醒我检查部署"
  → Worker 从意识层知道 cronjob 可用
  → cat ~/.hotplex/skills/cron.md 读取完整手册
  → curl POST ${HOTPLEX_ADMIN_API_URL}/api/cron/jobs 创建 cronjob
  → 返回确认给用户
```

### 6.6 目录布局更新

```
internal/cron/
├── cron.go
├── store.go
├── timer.go
├── executor.go
├── callback.go                # AttachedSessionRouter + AttachedSessionHandler — see Fast Path Spec
├── schedule.go
├── loader.go
├── delivery.go
├── skill.go                # go:embed manual + releaseSkillManual()
├── cron-skill-manual.md    # ~4 KB 完整管理手册
├── types.go
├── normalize.go
└── cron_test.go
```

### 6.7 Security

Job 创建和执行前双重 prompt injection 扫描：

```go
var threatPatterns = []string{
    "ignore previous instructions",
    "system prompt override",
    "you are now",
    // OWASP LLM Top 10 patterns
}

func ValidateJobPrompt(prompt string) error {
    lower := strings.ToLower(prompt)
    for _, pat := range threatPatterns {
        if strings.Contains(lower, pat) {
            return fmt.Errorf("potential prompt injection detected")
        }
    }
    return nil
}
```

---

## 7. Gateway Integration

### 7.1 GatewayDeps

```go
type GatewayDeps struct {
    // ... existing fields ...
    CronScheduler *cron.Scheduler
}
```

### 7.2 Startup

在 Bridge 创建之后、messaging adapter 启动之前初始化：

```
step 11: cronScheduler = cron.New(log, db, bridge, hub, cfgStore, adapterResolver)
step 12: cronScheduler.Start(ctx)
```

### 7.3 Shutdown

在 ConfigWatcher 之后、Adapters 之前关闭：

```
step 2.5: CronScheduler.Shutdown(ctx)
```

### 7.4 Config

```yaml
cron:
  enabled: true
  max_concurrent_runs: 3
  max_jobs: 50
  default_timeout_sec: 300
  tick_interval_sec: 60
  yaml_config_path: ""   # optional: path to external jobs YAML
  jobs: []                # inline job definitions
```

Hot-reload 支持：`cron.enabled`、`cron.max_concurrent_runs`、`cron.max_jobs`。

---

## 8. YAML Config Import

```yaml
jobs:
  - name: "daily-health-check"
    description: "检查 Gateway 运行状态，生成健康报告"
    schedule: "0 9 * * 1-5"
    prompt: "执行 hotplex doctor，汇总所有检查项状态，如果有异常则列出具体问题"
    work_dir: "/home/user/project"
    timeout_sec: 120

  - name: "deploy-reminder"
    schedule: "at:2026-05-12T09:00:00+08:00"
    prompt: "提醒：今天需要部署 v1.9.0，检查清单"
    delete_after_run: true
```

以 `name` 为幂等 key：

| 场景 | 行为 |
|------|------|
| YAML 有、SQLite 无 | insert |
| YAML 有、SQLite 有 | update 定义，不覆盖 runtime state |
| YAML 无、SQLite 有 | 不删除（运行时创建的 job 不受 YAML 影响） |

---

## 9. Admin API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/cron/jobs` | List all jobs |
| GET | `/api/cron/jobs/:id` | Get job detail |
| POST | `/api/cron/jobs` | Create job |
| PATCH | `/api/cron/jobs/:id` | Update job |
| DELETE | `/api/cron/jobs/:id` | Delete job |
| POST | `/api/cron/jobs/:id/run` | Trigger manual run |
| GET | `/api/cron/jobs/:id/runs` | Get run history |

---

## 10. Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/robfig/cron/v3` | Cron expression parsing |
| 现有 `*sql.DB` | SQLite persistence（shared with session/event store） |
| 现有 `gateway.Bridge` | Session + worker lifecycle |
| 现有 `gateway.Hub` | Event routing + eventstore auto-persist |
| 现有 `session.Manager` | Session state machine + GC + pool quota |
| 现有 `agentconfig` | Meta-cognition injection |

---

## 11. Implementation Phases

### Phase 1 — Core

- `internal/cron/` 包骨架：types.go、store.go、schedule.go、timer.go、executor.go、cron.go
- SQLite migration `003_cron_jobs_table.sql`
- Scheduler timer loop + at-most-once semantics
- Executor：`Bridge.StartSession` + `worker.Input` + `waitForCompletion`
- Exponential backoff + auto-disable
- GatewayDeps 集成（startup/shutdown）
- YAML config loader
- Config struct + hot-reload

### Phase 2 — AI-Native

- META-COGNITION.md 追加 Cron 意识层（~200 chars）
- Skill 文件：cron-skill-manual.md（知识层 go:embed → 释放到 `~/.hotplex/skills/cron.md`）
- WorkerEnv 注入 Admin API 凭证（`HOTPLEX_ADMIN_API_URL` + `HOTPLEX_ADMIN_TOKEN`）
- Delivery system（route to platform adapter）
- `[SILENT]` suppression
- Prompt injection scanning
- Admin API endpoints

### Phase 3 — Resilience

- Startup catch-up with grace period
- One-shot retry logic
- Prometheus metrics（`cron_fires_total`、`cron_errors_total`、`cron_duration_seconds`）
- Run history query（join eventstore by session_id）
- WebChat cron management UI（stretch）

### Phase 4 — Session Callback (Fast Path)

> 详见 [Cron-Fast-Path-Spec.md](Cron-Fast-Path-Spec.md)

- SQLite migration: expand `payload_kind` CHECK constraint
- `internal/cron/callback.go`: `AttachedSessionRouter` interface + `AttachedSessionHandler`
- `internal/cron/types.go`: rename `PayloadAgentTurn` → `PayloadIsolatedSession`, add `PayloadAttachedSession` + `TargetSessionID`
- `internal/cron/normalize.go`: attached_session validation rules
- `internal/cron/timer.go`: `executeAttached` dispatch in `executeJob`
- Bridge adapter in `cmd/hotplex/`: `cronAttachedRouter`
- Session GC hook: `OnTerminate` → `CleanupForSession`
- CLI: `--callback` flag + `at:+N` relative time
- Skill manual: `<callback_mode>` section
- Metrics: `hotplex_cron_callback_total`
- Tests: unit + integration
