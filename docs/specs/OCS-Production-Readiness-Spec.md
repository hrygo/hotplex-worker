---
type: spec
tags:
  - project/HotPlex
  - worker/ocs
  - architecture/reliability
date: 2026-05-15
status: approved
progress: 0
estimated_hours: 8
---

# OCS Worker 生产级完善规格

## 背景与问题

OpenCode Server (OCS) worker (`internal/worker/opencodeserver/`) 通过 HTTP REST + SSE 与 `opencode serve` 通信。存在以下问题：

### 核心缺陷：SSE 早退出

`Start()` 在第 200 行调用 `startSSE()` 时，还没有发送任何 input。opencode server 发现无待处理任务后 SSE 流立即返回 EOF，`readSSE` goroutine 永久退出。后续 `Input()` 的 HTTP POST 虽然成功，但无 goroutine 消费 SSE 事件 → 用户只看到 emoji reactions，无实际响应。

**时序**：
1. `Start()` → `createSession()` → `startSSE()` — SSE goroutine 启动
2. SSE EOF — server 无任务，流立即结束
3. `Input()` HTTP POST 成功 — server 开始处理
4. Server 产出事件到 SSE 流 — 无人读取
5. `forwardEvents` 阻塞在 `recvCh` — 永远等不到事件

### 测试覆盖缺口

`readSSE()` 是最复杂的 99 行代码（运行在独立 goroutine，处理多种事件类型，有 panic recovery），**测试覆盖率 0%**。

### 缺失能力

1. **LastInput**：Bridge 崩溃恢复需要 `LastInput()` 重投递消息，OCS conn 未实现 `InputRecoverer` 接口
2. **错误分类**：HTTP 连接错误返回裸 `fmt.Errorf`，Bridge 无法区分"server 不可用"和"临时错误"

---

## 修改范围

| 文件 | 改动类型 | 说明 |
|:-----|:---------|:-----|
| `internal/worker/opencodeserver/worker.go` | 修改 | SSE 重连、LastInput、错误分类 |
| `internal/worker/opencodeserver/sse_test.go` | 新建 | readSSE 全覆盖测试 |

**不修改**：`singleton.go`、`commands.go`、`bridge.go`

---

## Step 1: SSE 重连循环（核心修复）

### 当前行为

`readSSE()` 在 `io.EOF` 或任何错误时永久退出（worker.go:601-605）。

### 目标行为

`readSSE()` 在 EOF/网络错误时重连，仅在以下条件停止：
- SSE context 被 cancel（Terminate/Kill）
- HTTP 404（session 已销毁）
- 超过最大重连次数
- `httpConn == nil`（connection 已关闭）

### 设计

```
readSSE(ctx, sessionID)
  for {
    select ctx.Done() → return
    GET /events?session_id={id}
    connect error → backoff, continue
    HTTP 404 → close recvCh, return
    HTTP non-200 → backoff, continue
    HTTP 200:
      for {
        read line
        EOF → break inner, continue outer (reconnect)
        read error → break inner, continue outer
        process data line → send to recvCh
        conn nil → return
      }
  }
```

### 新增常量

```go
const (
    sseBackoffInitial = 500 * time.Millisecond
    sseBackoffMax     = 10 * time.Second
    sseMaxReconnects  = 50
)
```

### 新增方法

```go
func (w *Worker) sseBackoffSleep(ctx context.Context, attempt int)
```

指数退避：`min(initial * 2^attempt, max) ± 20% jitter`。通过 `select { case <-ctx.Done(): case <-time.After(dur): }` 实现可取消等待。

### Entry-conn 捕获

借鉴 ClaudeCode `readOutput` 模式：goroutine 入口捕获当前 `httpConn` 引用。永久失败时关闭捕获的 conn 的 `recvCh`，而非当前 `w.httpConn`（可能已被 reset 替换）。

---

## Step 2: LastInput 捕获（InputRecoverer 接口）

### 目标

使 OCS `conn` 实现 `worker.InputRecoverer` 接口，支持 Bridge 崩溃恢复重投递。

### 改动

1. `conn` 结构体新增 `lastInput string` 字段
2. `conn.Send()` 在 HTTP POST 前从 envelope data 提取 content 并缓存到 `lastInput`
3. 新增 `conn.LastInput() string` 方法
4. 编译时断言：`var _ worker.InputRecoverer = (*conn)(nil)`

---

## Step 3: HTTP 错误分类（WorkerError）

### 目标

`conn.Send()` 的 HTTP 错误分类为 `worker.WorkerError`，使 Bridge 能正确处理"server 不可用"。

### 错误映射

| 条件 | Kind |
|:-----|:-----|
| 连接拒绝、超时、网络中断 | `ErrKindUnavailable` |
| HTTP 502 Bad Gateway | `ErrKindUnavailable` |
| HTTP 503 Service Unavailable | `ErrKindUnavailable` |
| 其他 HTTP 非 200/202 | 保持 `fmt.Errorf` |

### 新增辅助函数

```go
func isServerDownError(err error) bool
```

检查 `context.DeadlineExceeded`、`net.Error` 类型断言。

---

## Step 4: readSSE 全覆盖测试

### 测试文件

`internal/worker/opencodeserver/sse_test.go`（新建）

### 测试矩阵

| # | 测试名 | 覆盖点 | 类型 |
|:--|:-------|:-------|:-----|
| 1 | `TestReadSSE_BasicEventParsing` | data 行 → AEP decode → recvCh | 功能 |
| 2 | `TestReadSSE_BusEventParsing` | permission.asked / question.asked 转换 | 功能 |
| 3 | `TestReadSSE_EmptyLinesIgnored` | 空行跳过 | 功能 |
| 4 | `TestReadSSE_NonDataPrefixIgnored` | event:/id: 行跳过 | 功能 |
| 5 | `TestReadSSE_InvalidJSON_Skipped` | malformed data 跳过并记录 | 功能 |
| 6 | `TestReadSSE_EOF_Reconnects` | 流关闭后重连，事件累积正确 | 重连 |
| 7 | `TestReadSSE_NetworkError_Reconnects` | 连接错误后重试成功 | 重连 |
| 8 | `TestReadSSE_HTTPError_503_Reconnects` | 首次 503，重试 200 | 重连 |
| 9 | `TestReadSSE_HTTPError_404_Stops` | 404 → recvCh 关闭，不重连 | 重连 |
| 10 | `TestReadSSE_ContextCancel_Stops` | sseCancel() 后退出 | 终止 |
| 11 | `TestReadSSE_Backpressure_DropOnFull` | 300 事件 > 256 buffer，无死锁 | 背压 |
| 12 | `TestReadSSE_MaxReconnects_Stops` | 超过最大次数 → recvCh 关闭 | 终止 |
| 13 | `TestReadSSE_MultipleReconnects` | 交替 EOF/成功，事件累积 | 重连 |
| 14 | `TestReadSSE_ConnNil_Stops` | httpConn 置 nil 后退出 | 终止 |
| 15 | `TestConn_LastInput` | Send 后 LastInput 返回正确值 | LastInput |
| 16 | `TestConn_LastInput_UpdatedOnEachSend` | 多次 Send 后返回最新值 | LastInput |
| 17 | `TestConn_LastInput_EmptyOnNoSend` | 初始状态返回空 | LastInput |
| 18 | `TestConn_Send_ServerDown` | 连接错误 → WorkerError(ErrKindUnavailable) | 错误 |
| 19 | `TestConn_Send_503` | HTTP 503 → WorkerError | 错误 |

### 测试基础设施

使用 `httptest.Server` mock OCS server：
- SSE endpoint：`GET /events?session_id={id}` 返回 `text/event-stream`
- Message endpoint：`POST /session/{id}/message` 返回 200
- 可编程行为：按请求次数返回不同状态码，模拟断连

---

## Step 5: 集成验证

1. `make quality` — lint + test 全部通过
2. `make build` — 编译成功
3. `make dev` 启动 + 飞书发消息 — 验证完整响应流程
4. 检查 gateway 日志确认 SSE 重连行为

---

## 实施顺序

```
Step 2 (LastInput) ─── 独立，最小改动，零风险
Step 3 (Error 分类) ── 独立，小改动
Step 1 (SSE 重连) ──── 核心修复，最复杂
Step 4 (测试) ──────── 依赖 Step 1-3
Step 5 (集成验证) ──── 依赖全部
```

## 风险与缓解

| 风险 | 缓解 |
|:-----|:-----|
| SSE 重连导致重复事件 | Bridge 层 AEP seq 分配 + event store SeqKey 去重 |
| SSE goroutine 泄漏 | context cancel + conn.closed 双重退出保障 |
| 404 vs 空闲误判 | 可选 health check (GET /session/{id}) 在重连前确认 |
| singleton 行为被影响 | 所有改动在 worker.go，singleton.go 不变 |
