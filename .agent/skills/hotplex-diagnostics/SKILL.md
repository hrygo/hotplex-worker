---
name: hotplex-diagnostics
description: HotPlex Gateway 运行时全面诊断。当用户提到"诊断 hotplex"、"检查进程状态"、"分析日志"、"hotplex 有问题"、"排查错误"、"worker 崩溃"、"响应慢"、"session 异常"时调用此 skill。也适用于用户主动请求健康检查、上线前验证、或发现 Gateway/Worker/适配器行为异常的场景。跨平台支持 Linux/macOS/Windows。
---

# HotPlex 运行时诊断

## 核心思路

诊断不是跑检查清单 — 而是从症状出发，逐层缩小范围。每一层发现问题后决定是否深入下一层。

**层次**：进程 → 数据 → 日志 → 适配器 → 源码。进程不在就停，日志干净就跳过源码。

---

## 第一步：进程与端口

进程不在，后面都不用查。

确认以下组件存活：Gateway（8888）、WebChat（3000）、Admin（9999）、Worker 子进程（`claude --session-id` / `--resume`）。

**关键检查**：
- PID 文件（`~/.hotplex/.pids/`）中的 PID 是否对应实际进程 — 不对应就是 stale PID
- 端口是否在预期端口监听
- RSS、CPU%、运行时长。RSS 持续增长暗示泄漏，超过 500MB 或 CPU 持续 >50% 需关注

进程不在 → 🔴，诊断结束。

---

## 第二步：Session 状态一致性

HotPlex 用 SQLite 存 session 状态，内存和磁盘之间可能不一致。查 `~/.hotplex/data/hotplex.db`。

**关键检查**：
- 各状态 session 数量分布（`SELECT state, COUNT(*) FROM sessions GROUP BY state`）
- 对每个 running/idle session：PID 文件存在？进程存活？Worker 进程匹配？
- 对带 `--resume` 的 Worker：Claude session 文件（`~/.claude/projects/*/<uuid>.jsonl`）是否存在？

**不一致分类**：
- **ORPHANED**：DB 在但进程已死 → 等 GC 或手动清理
- **ZOMBIE**：进程在但 DB 状态是 terminated → GC 漏扫或 race
- **STALE_RESUME**：标记 running/idle 但磁盘上 session 文件不存在 → resume 必定失败

---

## 第三步：日志异常分析

日志是诊断的核心，但分析方法很重要。

### 方法：先 grep 全局，再读上下文

1. **全局扫描** — 先 `grep "level=ERROR"` 和 `grep "level=WARN"` 在完整日志中搜索，`tail` 只用来控制输出量，不截断搜索范围
2. **读取上下文** — `grep -n` 拿到行号后，用 Read 工具或 `sed -n` 读取前后 20 行。单条日志没意义，前后因果才重要：往前看 10 行可能发现根因，往后看 5 行可能发现恢复结果
3. **时间线重建** — 对问题 session，`grep <SESSION_ID>` 过滤关键事件（transitioned/crash/resume/error），重建完整生命周期

### 高频问题模式

用这些模式做初始扫描，命中的再读上下文深入分析：

**Worker 生命周期**（高严重度）：
- `session files missing after resume` / `worker crashed shortly after resume` / `No conversation found with session ID` — 根因通常是 zombie GC 清理了文件但 resume 无前置检查

**状态机异常**（中严重度）：
- `terminated.*terminated` — 重复终止，GC 和 crash cleanup 之间的 race
- `from=X to=X` — 幂等保护生效但存在并发问题

**流式传输**（低严重度，通常自愈）：
- `streaming integrity check failed` — `final_flush_ok=false` 才需关注

**前端**（看 webchat.log）：
- `MessageRepository.*same id` — 消息 ID 重复

**正常行为，不需要关注**：`Client disconnect` / `going away` / `proc: stderr`

---

## 第四步：适配器连通性

搜索日志中的 `feishu.*(connected|disconnect|reconnect)` 和 `slack.*(connected|disconnect|reconnect)`。频繁 reconnect 暗示网络或服务端问题。

TCP 连接数持续增长不回落暗示泄漏。

---

## 第五步：源码交叉验证

**只在日志有问题且源码可访问时执行。** 日志干净就跳过。

从日志错误消息提取关键字 → `grep -rn` 在 `internal/` 中定位源码 → 读取上下文理解触发条件 → `git log --oneline -- <file>` 检查是否已有修复。

**快速定位表**：

| 层 | 文件 | 职责 |
|----|------|------|
| Session 管理 | `internal/session/manager.go` | 5 状态机、GC |
| Worker 生命周期 | `internal/gateway/bridge.go` | start/resume/crash/fallback |
| Claude Worker | `internal/worker/claudecode/worker.go` | 进程管理、session 文件 |
| 事件分发 | `internal/gateway/handler.go` | AEP 事件、GC 触发 |
| 飞书适配器 | `internal/messaging/feishu/adapter.go` | WebSocket、消息卡片 |
| Slack 适配器 | `internal/messaging/slack/adapter.go` | Socket Mode |
| WebChat | `webchat/lib/adapters/hotplex-runtime-adapter.ts` | 消息 dedup |

---

## 跨平台适配

所有命令需适配当前平台。检测方法：`uname`（Linux/macOS）或 `OS=Windows_NT`（Windows）。

关键差异：`ps`/`lsof` → Windows 用 `tasklist`/`netstat -ano`；路径 `~/.hotplex/` → Windows 用 `%USERPROFILE%\.hotplex\`；`sqlite3` 跨平台一致但 Windows 需确认在 PATH。

---

## 诊断报告

完成所有步骤后输出报告。原则：

- 正常组件 ✅ 一笔带过，不展开
- 有问题的 ⚠️ 或 🔴，展开原因和建议
- 数字给上下文："RSS 31MB（正常）"比"RSS 31MB"有用
- 建议要可执行："在 bridge.go resumeWithOpts 添加前置检查"比"修复 resume"有用

```markdown
## HotPlex 运行时诊断报告

### 概览
诊断时间、平台、运行时长、健康状态总评（✅/⚠️/🔴）

### 进程 [状态标记]
| 组件 | PID | 运行时长 | RSS | 状态 |

### Session [状态标记]
各状态数量 + ORPHANED/ZOMBIE/STALE_RESUME（如有）

### 日志发现 [状态标记]
按严重度排列：时间、session ID、问题类别、影响评估

### 适配器 [状态标记]
飞书/Slack 连接状态

### 建议
按优先级排列：问题 → 影响 → 修复方向
```

---

## 第六步：问题提交（可选）

报告输出后，如果发现问题，询问用户是否需要提交 GitHub Issue。

### 去重

先 `gh issue list --state open --limit 30` 查看已有 issue。相同关键词的跳过，引用已有编号。

### 合并策略

- **合并**：同一根因链的不同症状、同一模块的多个小问题、优先级相近的发现
- **分开**：不同模块、严重度差异大、修复路径独立

### Issue 格式

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

### 标签

`bug` + 优先级（`P1`/`P2`/`P3`）+ 领域（`area/gateway`/`area/session`/`area/messaging`/`area/webchat`）+ 特征（`race-condition`/`reliability`/`performance`，按需）。

提交后附上 issue 链接。
