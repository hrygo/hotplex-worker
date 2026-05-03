---
name: hotplex-diagnostics
description: HotPlex Gateway 运行时诊断 — 从症状逐层缩小范围到根因。当用户提到 hotplex 进程状态、日志分析、worker 崩溃、响应慢、session 异常、反馈中断、任务卡住、没有输出了、streaming 卡住时使用此 skill。用户主动请求健康检查、上线前验证、或发现 Gateway/Worker/适配器异常时也适用。此 skill 的独特价值在于：理解反馈链路架构，能通过时间线交叉验证检测静默中断（Worker 在跑但用户端无更新），并分类根因是管道阻塞、背压丢弃、适配器故障还是客户端断连。跨平台支持 Linux/macOS/Windows。
---

# HotPlex 运行时诊断

## 诊断哲学

诊断不是跑检查清单 — 而是从症状出发，逐层缩小范围。

核心层次：**进程 → 数据 → 反馈链路 → 日志 → 适配器 → 源码**。

每层都有**跳过条件**：进程不在就停；没有 running session 就跳过反馈检查；日志干净就跳过源码。不要做无用功。

---

## 第一步：进程与端口

为什么先查进程 — 进程不在，后面所有的日志和状态都可能过时。

确认组件存活：Gateway（8888）、WebChat（3000）、Admin（9999）、Worker 子进程（`claude --session-id` / `--resume`）。

检查要点：
- PID 文件（`~/.hotplex/.pids/`）中的 PID 是否对应实际进程 — 不对应说明进程异常退出但系统未清理
- 端口是否在预期地址监听 — 仅绑定 localhost 是安全基线
- RSS/CPU/运行时长 — RSS 持续增长暗示泄漏（>500MB 需关注），CPU 持续 >50% 需排查

**跳过条件**：Gateway 进程不在 → 🔴 诊断结束，报告进程问题即可。

---

## 第二步：Session 状态一致性

为什么查 session — HotPlex 的 session 状态是内存 + SQLite 双写，两处可能不同步，而大多数用户问题（"任务卡住"、"响应慢"）都能从 session 状态找到线索。

查 `~/.hotplex/data/hotplex.db`，关注：
- 各状态 session 数量：`SELECT state, COUNT(*) FROM sessions GROUP BY state`
- 对 running/idle session：进程是否存活？Worker PID 匹配？
- 对带 `--resume` 的 Worker：Claude session 文件（`~/.claude/projects/*/<uuid>.jsonl`）是否存在？

不一致分类（表示系统健康问题的术语）：
- **ORPHANED**：DB 记录在但进程已死 — 等 GC 自动清理或手动终止记录
- **ZOMBIE**：进程在但 DB 状态是 terminated — GC 漏扫，通常是 race condition
- **STALE_RESUME**：标记 running/idle 但磁盘上 session 文件不存在 — resume 时必定失败

**跳过条件**：无 running/idle session → 直接跳到第四步（日志分析），反馈链路检查不适用。

---

## 第三步：反馈连续性

这是用户体验的核心能力 — 任务执行中用户看不到更新，但任务实际上还在跑。这种问题最隐蔽，因为 Worker 日志看起来一切正常。

### 反馈链路

事件从 Worker 到用户经历一条管道，任何环节断裂都导致用户端无反馈：

```
Worker stdout → readOutput/trySend → forwardEvents → Hub.SendToSession → routeMessage → PlatformConn.WriteCtx
```

每一段都有不同的失败模式和检测方法。

### 检测方法：时间线交叉验证

对每个 running session，建立两条时间线并对比：

1. **Worker 产出时间线** — 该 session 在日志中的 Worker 活动事件（bridge: received event、proc: stdout 等）
2. **平台投递时间线** — 同时间段内该 session 的平台写事件（streaming card、cardkit flush、message update 等）

**判定逻辑**：如果时间线 1 有持续条目但时间线 2 空白 → 反馈链路断裂。再用中断指示器（dropped/failed/channel full/TTL exceeded 等）定位断裂发生在哪个阶段。

### FEEDBACK_STALL 分类（用于报告和建议）

- **PIPELINE_STALL**：Worker 有产出但 Hub 无 conn 路由 — 平台未订阅 session，所有事件静默丢弃
- **BACKPRESSURE_DROP**：delta 被背压策略丢弃 — 适配器处理慢或网络差，Hub 丢弃增量事件
- **ADAPTER_FAILURE**：飞书 cardkit/slack streaming 失败 — API 限流、卡片内容超限、TTL 过期
- **CLIENT_DISCONNECT**：客户端心跳超时或连接断开 — 用户端需要重连

### 静默中断（最危险）

有些中断没有任何日志记录，只能通过间接证据推断：

1. **Hub 无连接路由** — Worker 时间线有条目，但完全没有平台写事件。Hub 在 `len(conns) == 0` 时静默丢弃所有事件
2. **平台写缓冲溢出** — platform_writer 的 DropThreshold 被触发，只有 Prometheus 指标 `gateway_platform_dropped_total` 能检测到
3. **飞书 flush 吞错** — streaming.go 的后台 flushLoop 中 `_ = c.Flush()` 静默吞掉所有错误

如果 Prometheus 端点可用，检查 `gateway_deltas_dropped_total` 和 `gateway_platform_dropped_total` — 计数器 >0 说明有静默丢弃。

---

## 第四步：日志分析

日志分析的关键不是找到多少 WARN/ERROR，而是理解每条异常的前因后果。

### 方法

1. **全局扫描** — 在完整日志中搜索 ERROR 和 WARN，注意 `tail` 只控制输出量，不要用 `tail | grep` 截断搜索范围
2. **读上下文** — 用 `grep -n` 拿到行号后读取前后 20 行。单条日志没有诊断价值，往前看可能发现根因，往后看可能发现恢复结果
3. **时间线重建** — 对问题 session，过滤关键事件（transitioned/crash/resume/error/dropped），重建完整生命周期

### 日志模式速查

按反馈链路阶段排列，命中的再读上下文：

**Worker → Bridge**：
- `recv channel full, dropping` — Worker 产出过快导致事件丢失
- `trySend conn nil` — 生命周期 race，Worker 已清理但事件还在产出

**Bridge → Hub**：
- `bridge: handling dropped deltas before done` — Hub 层 delta 丢弃的关键指标，Done 事件会标记 dropped=true
- `bridge: forward event failed` — 转发到 Hub 失败
- `bridge: turn timeout exceeded` — 单轮超时导致 Worker 被终止
- `session files missing after resume` / `No conversation found with session ID` — zombie GC 清理了文件但 resume 未预检

**Hub → Platform**：
- `gateway: platform write enqueue failed` — 平台写缓冲满
- `gateway: write failed` — WS 写失败，连接关闭
- `gateway: max missed pongs` — 客户端心跳超时断连
- `gateway: removed stale conn` — 旧连接被新连接替换

**Platform → 用户**：
- `feishu: cardkit flush failed` / `feishu: cardkit table limit exceeded` — 飞书流式卡片失败
- `feishu: streaming TTL exceeded` — 卡片超时（6 分钟），后续写入被拒
- `feishu: streaming integrity check failed` — 确认内容丢失（>10% 缺失），只在 `final_flush_ok=false` 时需关注
- `feishu: streaming write failed, falling back to static` — 流式失败降级为静态消息
- `feishu: IM patch flush failed` — 降级路径也失败

**状态机异常**：
- `terminated.*terminated` — 重复终止，GC 和 crash cleanup 之间的 race
- `from=X to=X` — 幂等保护生效但存在并发问题

**前端**（看 webchat.log）：
- `MessageRepository.*same id` — 消息 ID 重复

**正常行为，安全忽略**：`Client disconnect` / `going away` / `proc: stderr` — 这些是 WebSocket 正常关闭和 Worker 子进程的常规 stderr 输出。

---

## 第五步：适配器连通性

搜索飞书和 Slack 的连接/断连/重连事件。频繁 reconnect（同一适配器 1 小时内 >3 次）暗示网络或服务端问题。

TCP 连接数持续增长不回落暗示泄漏 — 可以用 `lsof -iTCP` 对比两次快照。

---

## 第六步：源码交叉验证

**只在日志有未解释的问题且源码可访问时执行。** 目的是确认问题是否已在更新版本中修复。

从日志错误消息提取关键字 → 在 `internal/` 中定位源码 → 理解触发条件 → `git log --oneline -- <file>` 检查最近修复。

| 层 | 文件 | 职责 |
|----|------|------|
| Session 管理 | `internal/session/manager.go` | 5 状态机、GC |
| Worker 生命周期 | `internal/gateway/bridge.go` | start/resume/crash/fallback、forwardEvents |
| Claude Worker | `internal/worker/claudecode/worker.go` | readOutput、trySend、last_io |
| 事件分发 | `internal/gateway/hub.go` | SendToSession、routeMessage、背压 |
| 平台写缓冲 | `internal/gateway/platform_writer.go` | delta 合并、DropThreshold |
| 飞书适配器 | `internal/messaging/feishu/adapter.go` | streaming card、WriteCtx |
| 飞书流式 | `internal/messaging/feishu/streaming.go` | CardKit flush、TTL、完整性 |
| Slack 适配器 | `internal/messaging/slack/adapter.go` | Socket Mode、streaming writer |
| WebChat | `webchat/lib/adapters/hotplex-runtime-adapter.ts` | 消息 dedup |

---

## 跨平台适配

所有命令需适配当前平台：`uname`（Linux/macOS）或 `OS=Windows_NT`（Windows）。

关键差异：`ps`/`lsof` → Windows 用 `tasklist`/`netstat -ano`；路径 `~/.hotplex/` → Windows 用 `%USERPROFILE%\.hotplex\`；`sqlite3` 跨平台一致但 Windows 需确认在 PATH。

---

## 诊断报告

完成检查后输出报告，遵循这些原则：

- **正常组件 ✅ 一笔带过** — 不展开正常状态的细节
- **有问题的 ⚠️ 或 🔴 展开说明** — 包括原因、影响、建议
- **数字给上下文** — "RSS 31MB（正常）" 比 "RSS 31MB" 有用
- **建议要可执行** — "在 bridge.go resumeWithOpts 添加前置检查" 比 "修复 resume" 有用
- **只报告实际检查的步骤** — 跳过的步骤不在报告中出现

按实际情况组织报告段落（概览 → 进程 → Session → 反馈连续性 → 日志发现 → 适配器 → 建议），问题按严重度排列。

---

## 第七步：问题提交（可选）

报告输出后，如果发现问题，询问用户是否需要提交 GitHub Issue。

**去重**：先 `gh issue list --state open --limit 30` 查看已有 issue，相同关键词的跳过并引用已有编号。

**合并策略**：同一根因链的不同症状合并为一个 issue；不同模块、严重度差异大的分开提交。

**Issue 格式**：

```markdown
## Summary
一到两句话概括问题和影响。

## 根因链
1. `文件:行号` — 触发条件
2. `文件:行号` — 传导路径
3. 最终症状

## 日志证据
关键日志片段（含时间戳）。

## 修复方案
文件路径、函数名、修改方向。

## Acceptance Criteria
- [ ] 具体验证条件
```

**标签**：`bug` + 优先级（`P1`/`P2`/`P3`）+ 领域（`area/gateway`/`area/session`/`area/messaging`/`area/webchat`）+ 特征（`race-condition`/`reliability`/`performance`，按需）。

提交后附上 issue 链接。
