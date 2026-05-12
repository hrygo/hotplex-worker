---
type: spec
tags:
  - project/HotPlex
date: 2026-05-12
status: draft
progress: 0
---

# Gateway 自重启能力方案 (AEP-006)

**状态**：草案 (Draft)
**所有者**：Antigravity
**更新日期**：2026-05-12

> **目标**：解决 Worker 进程无法成功重启 Gateway 的问题，实现从 Gateway 内部（Worker/Cron）发起的安全自重启。

---

## 1. 问题分析

### 1.1 核心矛盾

Worker 是 Gateway 的子进程。Gateway 停止 = Worker 死亡。`hotplex gateway restart` 的 stop + start 顺序执行中，start 阶段永远不会到达。

### 1.2 死亡链时序

```
进程树初始状态:
  Gateway (PID=100, PGID=100)           ← daemon 模式, Setpgid=true
    └─ Worker (PID=200, PGID=200)       ← proc.Manager.Start() Setpgid=true
         └─ bash (PID=299, PGID=200)    ← 继承 Worker PGID
              └─ hotplex gateway restart (PID=300, PGID=200) ← 继承 Worker PGID

时序:
  T=0    restart CLI → stopGateway() → SIGTERM to Gateway (PGID 100)
  T=0+   Gateway 收到 SIGTERM → shutdownGateway() 开始
  T=δ    Gateway: Hub→Skills→Brain→Config→Cron→Messaging→STT/TTS (步骤 1-7)
  T=δ    restart CLI 在 waitForProcessExit() 中等待 Gateway 退出...
  T=δ'   Gateway 第 8 步: TerminateAllWorkers() → kill(-200, SIGTERM)
         ↓ SIGTERM 命中 PGID=200 的所有进程:
         ↓   Worker (PID 200)   — DEAD
         ↓   bash   (PID 299)   — DEAD
         ↓   restart CLI (PID 300) — DEAD  ← start 阶段永远不执行
  T=30s  Gateway 完全退出
         新 Gateway 永远不会启动
```

### 1.3 根因定位

**文件**: `internal/worker/proc/manager.go:194-196`

```go
if pgid > 0 {
    _ = GracefulTerminate(pgid)  // syscall.Kill(-pgid, SIGTERM)
}
```

`TerminateAllWorkers()` 对每个 Worker 调用 `GracefulTerminate(workerPGID)`，发送 SIGTERM 到整个进程组。restart CLI 作为 Worker 的孙子进程，继承 Worker 的 PGID，被连带杀死。

### 1.4 次要问题：竞态窗口

即使 restart CLI 不被 SIGTERM 杀死，还存在第二个问题：

**文件**: `cmd/hotplex/gateway_cmd.go:111-115`

```go
if inst.Source == sourcePID {
    waitForProcessExit(inst.PID, 5*time.Second)  // 只等 5 秒
}
```

`waitForProcessExit` 只等 5 秒，但 `shutdownGateway` 有 30 秒超时。随后的 `startDaemon()` 调用 `ensureNotRunning()` 会发现旧 Gateway 仍在 shutdown 中，返回错误。

### 1.5 影响范围

| 触发来源 | 当前结果 | 期望结果 |
|----------|---------|---------|
| Worker (Claude Code) 执行 `hotplex gateway restart` | Gateway 停止，不启动 | Gateway 重启成功 |
| Cron Job 触发 Gateway 重启 | 同上 | Gateway 重启成功 |
| 终端执行 `hotplex gateway restart` | 正常（CLI 不在 Worker PGID 中） | 保持不变 |
| `systemctl restart hotplex` | 正常（systemd 原子操作） | 保持不变 |

---

## 2. 设计方案：Detached Restart Helper

### 2.1 核心原理

在杀死 Gateway 之前，fork 一个**独立进程组**的 restart-helper 进程。该进程在 Gateway/Worker 死亡后存活，负责等待旧 Gateway 退出、启动新 Gateway。

### 2.2 进程组隔离验证

Helper 用 `Setpgid: true` + `Process.Release()` 启动，创建独立 PGID：

```
重启前:
  Gateway (PID 100, PGID 100)
    └─ Worker (PID 200, PGID 200)
         └─ restart CLI (PID 300, PGID 200)
              └─ restart-helper (PID 301, PGID 301) ← Setpgid=true + Release

CLI 退出后:
  Gateway (PID 100, PGID 100)
    └─ Worker (PID 200, PGID 200)
  restart-helper (PID 301, PGID 301) ← 被 init 收养，存活

Gateway shutdown kill(-200):
  PID 200 (Worker)    ← DEAD
  PID 301 (helper)    ← PGID=301 ≠ 200，存活！

Helper 启动新 Gateway:
  startDaemon() → New Gateway (PID 400, PGID 400)
  Helper 退出
```

### 2.3 重启策略

| Gateway 运行方式 | 检测方式 | Helper 行为 |
|---|---|---|
| PID 管理 | `gateway.pid` 文件存在 | SIGTERM → 等退出(30s) → startDaemon |
| systemd 服务 | `systemctl status` | `systemctl --user restart hotplex` (原子) |
| launchd 服务 | `launchctl list` | `launchctl stop` (KeepAlive 重启) |
| Windows SCM | SCM 查询 | Stop + Start (SCM 管理) |

---

## 3. 实现规格

### 3.1 新增文件

#### `cmd/hotplex/gateway_restart_helper.go` (Unix)

```go
//go:build !windows

package main

// restartHelperSysProcAttr returns platform-specific process attributes
// that ensure the restart helper runs in its own process group.
func restartHelperSysProcAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{Setpgid: true}
}
```

包含平台无关的核心逻辑：

- `forkRestartHelper()` — fork 独立 PGID 的 helper 进程
- `runRestartHelper()` — helper 主函数：等待旧 Gateway 退出、启动新 Gateway

#### `cmd/hotplex/gateway_restart_helper_windows.go`

```go
//go:build windows

func restartHelperSysProcAttr() *syscall.SysProcAttr {
    return &syscall.SysProcAttr{
        CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
    }
}
```

### 3.2 修改 `cmd/hotplex/gateway_cmd.go`

#### 3.2.1 添加 `--detached` 标志

`newGatewayRestartCmd()` 新增：

```go
var detached bool
cmd.Flags().BoolVar(&detached, "detached", false,
    "spawn detached restart helper (safe when called from Worker)")
```

当 `--detached` 时，执行 `forkRestartHelper()` 替代 inline stop+start。

当非 `--detached` 时，保持现有逻辑（终端使用场景不受影响）。

#### 3.2.2 注册隐藏的 `_restart-helper` 命令

`newGatewayCmd()` 中添加：

```go
cmd.AddCommand(newRestartHelperCmd())

func newRestartHelperCmd() *cobra.Command {
    return &cobra.Command{
        Use:    "_restart-helper",
        Hidden: true,
        RunE:   runRestartHelper,
    }
}
```

### 3.3 修改 `cmd/hotplex/pid.go`

添加冷却标记函数，防重启循环：

```go
type restartMarker struct {
    HelperPID int       `json:"helper_pid"`
    CreatedAt time.Time `json:"created_at"`
}

func restartMarkerPath() string  // ~/.hotplex/.pids/gateway.restart
func writeRestartMarker() error
func readRestartMarker() (*restartMarker, error)
func removeRestartMarker()
```

---

## 4. 核心函数设计

### 4.1 `forkRestartHelper()`

```
输入: gatewayInstance, configPath, devMode, daemon
输出: error

步骤:
  1. 冷却期检查 — 读标记文件，若 helper 存活则拒绝
  2. 写入冷却标记文件
  3. 构造 helper 参数:
     hotplex gateway _restart-helper \
       --old-pid <PID> \
       --source <pid|service> \
       --config <path> \
       [--dev] [--daemon] [--level <level>]
  4. exec.Command(self, args...)
     - SysProcAttr: restartHelperSysProcAttr() (Setpgid=true)
     - Stdout/Stderr → ~/.hotplex/logs/gateway-restart.log
     - Stdin: nil
  5. cmd.Start()
  6. cmd.Process.Release() — 脱离父进程
  7. 验证 helper 存活 (500ms)
  8. 打印 "gateway: restart helper spawned (PID %d)"
```

### 4.2 `runRestartHelper()`

```
输入: cobra 命令参数 (--old-pid, --source, --config, --level, --dev, --daemon)
输出: error

步骤:
  1. 解析参数
  2. 写入冷却标记 (PID + timestamp)
  3. defer removeRestartMarker()
  4. 根据 source 分支:

     Service 模式:
       service.NewManager().Restart("hotplex", level)
       → systemd: 原子 restart
       → launchd: stop + start (KeepAlive 保底)
       → SCM: stop + start

     PID 模式:
       a. proc.GracefulTerminate(oldPID) — SIGTERM
       b. waitForProcessExit(oldPID, 30s) — 等待退出
       c. 若仍存活: proc.ForceKill(oldPID)
       d. removeGatewayState() — 清理旧 PID 文件
       e. startDaemon(configPath, devMode) — 启动新 Gateway
       f. 验证新 Gateway 存活 (1s)

  5. 记录成功日志
```

### 4.3 冷却期逻辑

```
冷却检查:
  1. 读标记文件
  2. 标记不存在 → 允许重启
  3. 标记存在，helper 进程存活 → 拒绝: "restart in progress (PID %d)"
  4. 标记存在，helper 已死且 < 60s → 拒绝: "cooldown active (try again in %s)"
  5. 标记存在，helper 已死且 ≥ 60s → 清理过期标记，允许重启
```

---

## 5. 错误处理

| 失败场景 | 处理 |
|---|---|
| Helper fork 失败 | CLI 返回错误，不触发重启 |
| 旧 Gateway 拒绝退出 | Helper 30s 后升级为 ForceKill |
| 新 Gateway 启动失败 | Helper 记录错误到日志，退出码 1 |
| Helper 中途死亡 | 标记文件残留，下次尝试检测到过期标记后清理 |
| Service restart 失败 | Helper 返回错误，旧实例不受影响 |
| 冷却期冲突 | CLI 返回错误并显示剩余等待时间 |

---

## 6. 使用方式

### Worker (Claude Code) 触发

Worker 通过 bash 工具执行：

```bash
hotplex gateway restart --detached
```

CLI 在 ~100ms 内返回，输出 "restart helper spawned"。随后 Gateway shutdown 杀死 Worker。Helper 存活并完成重启。

### 终端使用（不变）

```bash
hotplex gateway restart          # 保持现有 inline 行为
hotplex gateway restart -d       # daemon 模式，保持现有行为
hotplex gateway restart --detached  # 也可以从终端使用 detached 模式
```

### Cron Job 触发

Cron executor 构造的 prompt 中可以包含 `hotplex gateway restart --detached` 命令。

---

## 7. 文件变更清单

| 文件 | 操作 | 说明 |
|---|---|---|
| `cmd/hotplex/gateway_cmd.go` | 修改 | 添加 `--detached` 标志，注册 `_restart-helper` |
| `cmd/hotplex/gateway_restart_helper.go` | **新建** | Unix: `restartHelperSysProcAttr()` + `forkRestartHelper()` + `runRestartHelper()` |
| `cmd/hotplex/gateway_restart_helper_windows.go` | **新建** | Windows: `restartHelperSysProcAttr()` |
| `cmd/hotplex/pid.go` | 修改 | 添加冷却标记读写函数 |

### 复用的现有函数

| 函数 | 文件 | 用途 |
|---|---|---|
| `findRunningGateway()` | `pid.go:92` | 发现 Gateway 实例 |
| `waitForProcessExit()` | `pid.go:128` | 轮询等待进程退出 |
| `startDaemon()` | `gateway_cmd.go:147` | 启动 daemon Gateway |
| `daemonSysProcAttr()` | `daemon_unix.go` | 平台 SysProcAttr 参考 |
| `proc.GracefulTerminate()` | `proc/signal_unix.go` | SIGTERM 到进程组 |
| `proc.ForceKill()` | `proc/signal_unix.go` | SIGKILL 到进程组 |
| `proc.IsProcessAlive()` | `proc/signal_unix.go` | 进程存活检查 |
| `service.NewManager().Restart()` | `service/manager_*.go` | 服务重启 |

---

## 8. 测试策略

### 8.1 手动测试

```bash
# 1. 启动 Gateway
make dev

# 2. 从终端测试 --detached
./hotplex gateway restart --detached
# 期望: "gateway: restart helper spawned (PID ...)"
# 验证: 旧 Gateway 停止，新 Gateway 自动启动

# 3. 测试冷却期
./hotplex gateway restart --detached
# 期望: "gateway: restart cooldown active (try again in Ns)"

# 4. 测试终端模式不受影响
./hotplex gateway restart
# 期望: 现有 inline stop+start 行为不变
```

### 8.2 从 Worker 测试

```bash
# 1. 启动 Gateway + 连接 Worker
# 2. 通过消息平台发送 "重启 Gateway"
# 3. Worker 执行 hotplex gateway restart --detached
# 4. 验证: Worker 输出 "restart helper spawned"，然后连接断开
# 5. 验证: 新 Gateway 在 30s 内启动完成
# 6. 验证: 新消息可以正常到达 Worker
```

---

## 9. 后续增强 (Phase 2)

- **Admin API 端点**: `POST /admin/gateway/restart` 提供编程式重启接口
- **重启通知**: Helper 完成重启后通过消息适配器发送通知
- **健康检查集成**: Helper 验证新 Gateway 不仅是进程存活，还验证 HTTP ready 端点
- **指标埋点**: Prometheus counter 记录重启次数和结果
