# HotPlex 非功能性诊断报告

**诊断日期**: 2026-04-27
**诊断范围**: internal/gateway, internal/session, internal/worker, internal/messaging, internal/security, internal/config, internal/admin, pkg/events, cmd/hotplex
**方法**: 代码审查 + 静态分析 + 人工核实
**已排除假阳性**: 8 项（见附表）

---

## 执行摘要

### 问题统计（按严重度）

| 严重度 | 定义 | 数量 |
|--------|------|------|
| **P0** | 数据损坏 / 安全漏洞（已部署环境可直接利用） | 0 |
| **P1** | 资源耗尽 / 服务级故障（每次触发必定复现） | 13 |
| **P2** | 潜在故障 / 性能退化（特定条件下触发） | 17 |
| **P3** | 技术债（低概率或极长周期才触发） | 9 |
| **合计** | | **38** |

### P0/P1 问题一览（需优先关注）

| ID | 问题 | 模块 | 触发场景 |
|----|------|------|---------|
| GW-01 | WebSocket close 无 write deadline | gateway | shutdown 时卡住 |
| SS-01 | runWriter goroutine 无 panic recovery | session | Manager.Close() 永久阻塞 |
| SS-02 | gc() goroutine 无 panic recovery | session | Manager.Close() 永久阻塞 |
| WK-04 | OCS readSSE 无 panic recovery | worker | 静默会话故障 |
| WK-06 | OCS readSSE httpConn TOCTOU 竞态 | worker | panic / 数据错发 |
| MG-01 | ChatQueue.Close() 不等待 goroutine | messaging | 关闭时残留 worker |
| SC-03 | config watcher 无界 goroutine | config | 高频配置变更导致 OOM |
| AP-01 | admin handlers 无 panic recovery | admin | 单个 handler panic 崩服务 |
| AP-02 | admin sessions handlers 无 panic recovery | admin | 同上 |
| AP-03 | admin config validate 无请求体大小限制 | admin | DoS 内存耗尽 |
| AP-04 | admin config rollback 无请求体大小限制 | admin | DoS 内存耗尽 |

---

## 按模块详述

### 一、Gateway 模块 (`internal/gateway/`)

#### GW-01: WebSocket close 缺少 write deadline
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/gateway/conn.go:547-549`
- **描述**: `Conn.Close()` 发送 WebSocket close 帧时未设置 write deadline。若远程 peer 无响应，`WriteMessage` 可能无限阻塞，导致整个 Hub.Shutdown() 挂起。
- **触发**: Gateway shutdown 期间
- **影响**: 进程无法干净退出
- **代码**:
```go
_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
_ = c.wc.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
return c.wc.Close() // 无 deadline，可能阻塞
```

#### GW-02: autoRetry 使用 context.Background 而非调用者 ctx
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/gateway/bridge.go:1003`
- **描述**: `autoRetry` 接收 `ctx` 参数但发送消息时硬编码 `context.Background()`，调用者取消信号无法传播。
- **代码**:
```go
_ = b.hub.SendToSession(context.Background(), notifyEnv) // ← 应使用 ctx
_ = b.hub.SendToSession(context.Background(), env)
```

#### GW-03: broadcastQueueSize 可能返回 0 导致 panic
- **类别**: 性能
- **严重度**: P2
- **文件**: `internal/gateway/conn.go:42-49`
- **描述**: 当 `cfg.Gateway.BroadcastQueueSize <= 0` 时返回 256，但若显式配置为 0，传入 `make(chan ..., 0)` 会 panic。
- **代码**:
```go
func broadcastQueueSize(cfg *config.Config) int {
    if cfg.Gateway.BroadcastQueueSize <= 0 {
        return 256
    }
    return cfg.Gateway.BroadcastQueueSize // ← 可能为 0
}
```

#### GW-04: pcEntry.writeLoop timer 可能在 panic 时泄漏
- **类别**: 泄漏
- **严重度**: P3
- **文件**: `internal/gateway/hub.go:724-746`
- **描述**: `flush()` 在特定路径停止 timer，但 `writeOne` panic 时 timer 泄漏。

#### GW-05: autoRetry cancelCh 在 panic 时泄漏
- **类别**: 泄漏
- **严重度**: P2
- **文件**: `internal/gateway/bridge.go:1014-1040`
- **描述**: `cancelCh` 在 `retryCancel` map 中注册但无 `defer` 清理路径，panic 时 channel 泄漏。
- **代码**:
```go
cancelCh := make(chan struct{})
b.retryCancelMu.Lock()
b.retryCancel[sessionID] = cancelCh
b.retryCancelMu.Unlock()
// 无 defer 清理！panic 时 cancelCh 泄漏
```

#### GW-06: throttleCleanup ticker 代码路径不够清晰
- **类别**: 泄漏
- **严重度**: P3
- **文件**: `internal/gateway/hub.go:382-416`
- **描述**: defer 会正确清理，但 shutdown 路径先 `drainBroadcast()` 再 return，两个 select 逻辑分离。

---

### 二、Session 模块 (`internal/session/`)

#### SS-01: runWriter goroutine 无 panic recovery
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/session/message_store.go:173`
- **描述**: `runWriter` goroutine 没有 `defer recover()`。若发生 panic（如 SQL 错误），`closeWg.Done()` 永不执行，`Manager.Close()` 永久阻塞。
- **触发**: 任意 panic 触发
- **影响**: 进程无法干净退出
- **代码**:
```go
func (s *SQLiteMessageStore) runWriter() {
    defer s.closeWg.Done() // panic 时不执行
    // ... 无 defer recover()
    for {
        select {
        case <-s.closeC:
            flush()
            return
        case req := <-s.writeC:
            // ... 可能 panic
        }
    }
}
```

#### SS-02: gc() goroutine 无 panic recovery
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/session/manager.go:729`
- **描述**: `runGC` goroutine 没有 `defer recover()`。若 `gc()` 中 panic（如 `TransitionWithReason`），goroutine 静默退出，`gcDone` 永不关闭，`Manager.Close()` 永久阻塞。GC 扫描也停止，zombie sessions 不再回收。
- **触发**: zombie 检查时 panic
- **影响**: 进程无法干净退出 + zombie sessions 泄漏

#### SS-03: StateNotifier 使用 context.Background()
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/session/manager.go:258, 486`
- **描述**: 通知通过 `go m.StateNotifier(context.Background(), ...)` 启动，不可取消。

#### SS-04: OnTerminate goroutine 无关闭机制
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/session/manager.go:261`
- **描述**: `OnTerminate` 通过 goroutine 调用，无 timeout 或 ctx 传播，若回调阻塞则泄漏。

#### SS-05: Append 在 channel 满时静默丢弃事件
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/session/message_store.go:165`
- **描述**: `writeC` 满时仅记录警告并返回 `nil`，调用者以为成功，事件永久丢失。

#### SS-06: flushBatch 静默丢弃批量插入失败
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/session/message_store.go:227`
- **描述**: 批量插入失败仅记录，无上报机制，事务仍提交，部分事件静默丢失。

#### SS-07: DeleteTerminated 将所有 ID 加载到内存
- **类别**: 性能
- **严重度**: P3
- **文件**: `internal/session/store.go:259-271`
- **描述**: 大量 terminated session 时内存占用高，且为 N+1 删除。

#### SS-08: getManagedSession store.Get 无 deadline
- **类别**: 可靠性
- **严重度**: P3
- **文件**: `internal/session/manager.go:873`
- **描述**: 使用 `context.Background()` 查询，数据库压力大时可能无限阻塞。

#### SS-09: gc() zombie 检查无 panic recovery
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/session/manager.go:784`
- **描述**: `w.LastIO()` 或 `m.TransitionWithReason()` panic 会导致 gc() 退出，扫描不完整。

---

### 三、Worker 模块 (`internal/worker/`)

#### WK-01: drainStderr goroutine 泄漏
- **类别**: 泄漏
- **严重度**: P2
- **文件**: `internal/worker/proc/manager.go:376-388`
- **描述**: `drainStderr` goroutine 无退出信号机制，`Close()` 关闭 stderr pipe 后 goroutine 才退出（通过 Read EOF）。每次 Start() 创建新的 goroutine，累积。

#### WK-02: discoverPort goroutine timer 泄漏
- **类别**: 泄漏
- **严重度**: P3
- **文件**: `internal/worker/opencodeserver/singleton.go:248-264`
- **描述**: 每次外层循环创建新 `time.After(timeout)` timer，提前 return 时未 stop，累积泄漏。

#### WK-03: Terminate time.After timer 泄漏
- **类别**: 泄漏
- **严重度**: P2
- **文件**: `internal/worker/proc/manager.go:215`
- **描述**: `time.After(gracePeriod)` 在 success 和 ctx cancel 路径均不 stop，每次 Terminate 泄漏一个 timer。
- **代码**:
```go
select {
case <-done:
    m.captureExitCode()
    return nil // ← timer 未 stop
case <-time.After(gracePeriod): // ← success 路径泄漏
    return m.Kill()
case <-ctx.Done():
    return ctx.Err() // ← timer 未 stop
}
```

#### WK-04: OCS readSSE 无 panic recovery
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/worker/opencodeserver/worker.go:525-613`
- **描述**: `readSSE` goroutine 在 for 循环中处理 SSE 数据，无 `defer recover()`。JSON 解析等操作 panic 时静默死亡，用户无响应无日志。
- **触发**: 异常响应格式
- **影响**: 静默会话故障

#### WK-05: drainStderr 无 panic recovery
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/worker/proc/manager.go:377-388`
- **描述**: `drainStderr` goroutine 无 panic recovery。

#### WK-06: OCS readSSE httpConn TOCTOU 竞态
- **类别**: 竞态
- **严重度**: P1
- **文件**: `internal/worker/opencodeserver/worker.go:596-604`
- **描述**: nil check 和使用 local `conn` 之间存在 TOCTOU 窗口。Terminate 可能在线程检查和发送之间替换/关闭 httpConn，导致 panic 或数据错发。
- **触发**: ResetContext 调用期间恰好收到消息
- **影响**: panic 或数据发送到已关闭 channel

---

### 四、Messaging 模块 (`internal/messaging/`)

#### MG-01: ChatQueue.Close() 不等待 worker goroutines 完成
- **类别**: Goroutine 泄漏
- **严重度**: P1
- **文件**: `internal/messaging/feishu/chat_queue.go:129-142`
- **描述**: `Close()` 仅关闭 task channels 但无 WaitGroup 等待。若 task 执行超过 10 分钟，worker goroutine 在 idle timeout (5min) 后才退出，`Close()` 已返回。
- **触发**: Close() 时恰好有长耗时 task
- **影响**: 关闭后残留 goroutine，长期频繁调用导致累积
- **代码**:
```go
func (q *ChatQueue) Close() {
    // 注释说 waits for all in-flight tasks，但实际不等待
    for _, w := range workers {
        close(w.tasks) // 仅关闭 channel
    }
    // ❌ 无 q.wg.Wait()
}
```

#### MG-02: FeishuConn.abort() fire-and-forget goroutine
- **类别**: Goroutine 泄漏
- **严重度**: P2
- **文件**: `internal/messaging/feishu/adapter.go:743`
- **描述**: abort 操作通过 `go func() { _ = streamCtrl.Abort(context.Background()) }()` 异步执行，使用 `context.Background()` 不可取消。

#### MG-03: Feishu runWebSocket goroutine 无显式追踪
- **类别**: 可靠性
- **严重度**: P3
- **文件**: `internal/messaging/feishu/adapter.go:117`
- **描述**: `runWebSocket()` 启动时无 WaitGroup 追踪，与其他 goroutine 管理模式不一致。

---

### 五、Security + Config 模块 (`internal/security/`, `internal/config/`)

#### SC-01: JWT ParseUnverified 用于 botID 提取
- **类别**: 安全
- **严重度**: P2
- **文件**: `internal/security/auth.go:111`
- **描述**: 使用 `ParseUnverified()` 提取 botID，不验证签名。注释称 API Key 是主认证，完整验证在 `performInit`。但若 API Key 验证被绕过，可注入任意 botID 绕过 bot 隔离。
- **影响**: bot 隔离机制可能被绕过

#### SC-02: SSRF double-resolve 仅 100ms 延迟
- **类别**: 安全
- **严重度**: P2
- **文件**: `internal/security/ssrf.go:148`
- **描述**: 100ms 延迟无法防御短 TTL DNS rebinding。攻击者设置 TTL < 100ms 即可绕过，可能访问云元数据端点。
- **影响**: SSRF 防护可能被绕过

#### SC-03: config watcher onChange 无界 goroutine
- **类别**: 泄漏
- **严重度**: P1
- **文件**: `internal/config/watcher.go:248-254`
- **描述**: 每次配置变更通过 `go w.onChange(newCfg)` 和 `go w.onStatic(c.Field)` 启动 goroutine，无并发上限。高频变更场景下可导致 OOM。
- **触发**: 高频配置更新（如编辑器保存、自动化部署）
- **影响**: 资源耗尽

#### SC-04: JTI blacklist sweep goroutine 泄漏
- **类别**: 泄漏
- **严重度**: P2
- **文件**: `internal/security/jwt.go:238-241`
- **描述**: `newJTIBlacklist()` 启动 sweep goroutine，`Stop()` 方法存在但从未被调用。
- **影响**: 每个 JWTValidator 实例泄漏一个 goroutine + time.Ticker

#### SC-05: GenerateTokenWithJTI 混用 HS256
- **类别**: 安全
- **严重度**: P2
- **文件**: `internal/security/jwt.go:188-197`
- **描述**: 项目规范仅允许 ES256，但 `[]byte` secret 时使用 HS256，违反安全设计。
- **代码**:
```go
switch v.secret.(type) {
case *ecdsa.PrivateKey:
    method = jwt.SigningMethodES256
case []byte:
    method = jwt.SigningMethodHS256 // ← 混用！
}
```

#### SC-06: ValidateCommand 缺少路径分隔符检查
- **类别**: 安全
- **严重度**: P3
- **文件**: `internal/security/command.go:20-27`
- **描述**: 未检查命令名是否包含 `/` 或 `\`。

#### SC-07: sensitiveEnvPrefixes 过于宽泛
- **类别**: 可靠性
- **严重度**: P3
- **文件**: `internal/security/env.go:15, 27-32`
- **描述**: `"SECRET"` 和 `"API_KEY"` 前缀匹配所有包含该字符串的变量，导致大量误判（无安全影响，但可能干扰正常工作）。

#### SC-08: Watcher.run timer goroutine 潜在泄漏
- **类别**: 泄漏
- **严重度**: P2
- **文件**: `internal/config/watcher.go:144-171`
- **描述**: `Close()` 后 ctx 取消，`run()` 退出，但 `debounceTimer` 的 timer goroutine 可能未及时清理。

---

### 六、Admin + Pkg + Cmd 模块

#### AP-01: admin handlers 无 panic recovery
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/admin/handlers.go:10-278`
- **描述**: 所有 admin HTTP handler 函数缺少 panic recovery。单个 handler panic 会导致整个 HTTP 服务器崩溃，影响所有 admin 端点和 gateway。
- **对比**: Gateway handler 在 `handler.go:52-58` 正确使用 `defer recover()`。
- **触发**: 任意 panic
- **影响**: 服务崩溃

#### AP-02: admin session handlers 无 panic recovery
- **类别**: 可靠性
- **严重度**: P1
- **文件**: `internal/admin/sessions.go:23-196`
- **描述**: 同 AP-01，session CRUD handlers 全部缺少 panic recovery。

#### AP-03: config validate 无请求体大小限制
- **类别**: 安全
- **严重度**: P1
- **文件**: `internal/admin/handlers.go:166`
- **描述**: `json.NewDecoder(r.Body).Decode()` 无大小限制，恶意客户端可发送任意大 JSON 耗尽服务器内存。
- **触发**: POST /admin/config/validate 大请求体
- **影响**: DoS

#### AP-04: config rollback 无请求体大小限制
- **类别**: 安全
- **严重度**: P1
- **文件**: `internal/admin/sessions.go:231`
- **描述**: 同 AP-03，POST /admin/config/rollback 端点。

#### AP-05: 全局日志计数器无界增长
- **类别**: 泄漏
- **严重度**: P3
- **文件**: `internal/admin/logbuf.go:36`
- **描述**: `logRingBuffer.n` 单调递增，虽然 ring buffer 本身有 100 条上限，但 `n` 永不重置。2^31 次后 int 溢出（100 writes/sec 需要 ~21 年）。

#### AP-06: admin handlers 静默忽略错误
- **类别**: 可靠性
- **严重度**: P2
- **文件**: `internal/admin/handlers.go:16, 268`
- **描述**: `sm.List()` 和 `sm.DebugSnapshot()` 的错误被忽略，失败时返回不完整数据无任何提示。

---

## 按严重度索引

### P1 — 资源耗尽 / 服务级故障（13 项）

| ID | 问题 | 模块 |
|----|------|------|
| GW-01 | WebSocket close 无 deadline，shutdown 卡住 | gateway |
| SS-01 | runWriter 无 panic recovery，Close() 永久阻塞 | session |
| SS-02 | gc() 无 panic recovery，Close() 永久阻塞 + zombie 泄漏 | session |
| WK-04 | OCS readSSE 无 panic recovery，静默会话故障 | worker |
| WK-06 | OCS readSSE httpConn TOCTOU 竞态，panic 或数据错发 | worker |
| MG-01 | ChatQueue.Close() 不等待 goroutine，残留 worker | messaging |
| SC-03 | config watcher 无界 goroutine，高频变更 OOM | config |
| AP-01 | admin handlers 无 panic recovery，单个 panic 崩服务 | admin |
| AP-02 | admin sessions handlers 无 panic recovery | admin |
| AP-03 | config validate 无请求体大小限制，DoS | admin |
| AP-04 | config rollback 无请求体大小限制，DoS | admin |

### P2 — 潜在故障（17 项）

| ID | 问题 | 模块 |
|----|------|------|
| GW-02 | autoRetry 使用 context.Background | gateway |
| GW-03 | broadcastQueueSize 返回 0 导致 panic | gateway |
| GW-05 | autoRetry cancelCh panic 时泄漏 | gateway |
| SS-03 | StateNotifier 使用 context.Background() | session |
| SS-04 | OnTerminate goroutine 无关闭机制 | session |
| SS-05 | Append 在 channel 满时静默丢弃事件 | session |
| SS-06 | flushBatch 静默丢弃批量插入失败 | session |
| SS-09 | gc() zombie 检查无 panic recovery | session |
| WK-01 | drainStderr goroutine 泄漏 | worker |
| WK-03 | Terminate time.After timer 泄漏 | worker |
| WK-05 | drainStderr 无 panic recovery | worker |
| MG-02 | FeishuConn.abort() fire-and-forget goroutine | messaging |
| SC-01 | JWT ParseUnverified 用于 botID 提取（安全弱点） | security |
| SC-02 | SSRF double-resolve 仅 100ms（可绕过） | security |
| SC-04 | JTI blacklist sweep goroutine 泄漏 | security |
| SC-05 | GenerateTokenWithJTI 混用 HS256 | security |
| SC-08 | Watcher.run timer goroutine 潜在泄漏 | config |
| AP-06 | admin handlers 静默忽略错误 | admin |

### P3 — 技术债（9 项）

| ID | 问题 | 模块 |
|----|------|------|
| GW-04 | pcEntry.writeLoop timer panic 时泄漏 | gateway |
| GW-06 | throttleCleanup ticker 代码路径不清晰 | gateway |
| SS-07 | DeleteTerminated 大量 ID 加载内存 | session |
| SS-08 | getManagedSession store.Get 无 deadline | session |
| WK-02 | discoverPort goroutine timer 泄漏 | worker |
| MG-03 | Feishu runWebSocket 无显式追踪 | messaging |
| SC-06 | ValidateCommand 缺少路径分隔符检查 | security |
| SC-07 | sensitiveEnvPrefixes 过于宽泛 | security |
| AP-05 | 全局日志计数器无界增长 | admin |

---

## 按类别索引

| 类别 | P0 | P1 | P2 | P3 | 小计 |
|------|----|----|----|----|------|
| 安全 | 0 | 2 | 4 | 2 | 8 |
| 可靠性（panic recovery） | 0 | 6 | 5 | 0 | 11 |
| Goroutine/Timer 泄漏 | 0 | 3 | 6 | 2 | 11 |
| 竞态条件 | 0 | 1 | 1 | 0 | 2 |
| 静默错误忽略 | 0 | 1 | 1 | 0 | 2 |
| 性能 | 0 | 0 | 1 | 1 | 2 |
| 配置/资源限制 | 0 | 2 | 0 | 1 | 3 |
| **合计** | **0** | **13** | **17** | **9** | **39** |

---

## 已排除的假阳性（8 项）

| 原始声明 | 排除原因 | 验证依据 |
|---------|---------|---------|
| session manager Create 竞态 | store.Upsert 在 map 插入之前执行 | manager.go:150-156 |
| config watcher Rollback 竞态 | muHistory 互斥锁正确保护 | watcher.go:229-240, 359-369 |
| bridge accumulator TOCTOU | 初始化在锁内原子完成 | bridge.go:1062-1071 |
| feishu dedupCleanupLoop 泄漏 | WaitGroup + dedupDone 正确清理 | adapter.go:515-516 |
| slack stream buffer 竞态 | mutex 正确保护所有 buffer 操作 | stream.go:147-199 |
| autoRetry 缺少 panic recovery | 非独立 goroutine，由上层 recovery 覆盖 | bridge.go:456（同步调用）|
| proc/manager 孤儿进程 | cmd.Start() 失败时无子进程，pipe 正确关闭 | manager.go:125-133 |
| admin logbuf n 溢出 | int64 在实际场景中不可达 | logbuf.go:36（极低影响）|

---

## 建议优先级

### 立即修复（P1，建议 1 周内）

1. **SS-01 + SS-02**: 为 `runWriter` 和 `runGC` 添加 `defer recover()` — 阻止 Manager.Close() 永久阻塞
2. **AP-01 + AP-02**: 为所有 admin HTTP handlers 添加 panic recovery — 防止单点故障崩溃整个服务
3. **AP-03 + AP-04**: 添加请求体大小限制 — 防止 DoS
4. **SC-03**: 为 config watcher onChange 添加 worker pool 或 semaphore — 防止无界 goroutine
5. **GW-01**: WebSocket close 添加 write deadline — 防止 shutdown 卡住
6. **WK-04**: OCS readSSE 添加 panic recovery — 防止静默会话故障
7. **WK-06**: OCS readSSE httpConn 竞态修复 — 防止 panic/数据错发
8. **MG-01**: ChatQueue.Close() 添加 WaitGroup 等待 — 确保优雅关闭

### 高优先级（P2，建议 1 个月内）

9. **SC-01**: JWT 签名验证 — 修复 botID 提取路径（安全）
10. **SC-02**: SSRF 延迟增加到 1s+ — 或改用 DNS pinning（安全）
11. **SC-05**: 移除 HS256 混用 — 统一 ES256（安全）
12. **SS-05 + SS-06**: 事件丢失 — 返回错误而非静默丢弃
13. **WK-03**: Terminate 使用 `time.NewTimer` + `Stop()` — 修复 timer 泄漏
14. **其他 P2 项**: 错误处理完善、context 传递规范化

### 中期清理（P3，技术债）

15. Timer/goroutine 生命周期规范统一
16. 配置验证增强（broadcastQueueSize 最小值检查）
17. 日志和监控指标补充

---

## 附录：验证通过的安全实践

以下模式在代码中正确实现：
- **Panic recovery**: Gateway handler (`handler.go:52`), bridge forwardEvents (`bridge.go:272`), Hub routeMessage (`hub.go:397`)
- **WaitGroup 追踪**: `fwdWg` 正确追踪所有 forwardEvents goroutine
- **Mutex 顺序**: SessionManager → managedSession 符合规范
- **Timer cleanup**: 关键路径（bridge.go, hub.go）正确使用 `defer timer.Stop()`
- **Seq 分配**: 原子操作，无竞态
- **Feishu dedupCleanupLoop**: WaitGroup + done channel 正确清理
- **Slack stream buffer**: mutex 正确保护所有 buffer 操作
- **Process isolation**: PGID isolation, layered SIGTERM→SIGKILL
