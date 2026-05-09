# 性能优化识别模式

HotPlex Gateway 专用性能分析参考。结合通用 Go 性能反模式和 HotPlex 架构特定的热路径模式。

**读取时机**：分析 aspect 7（Performance）或 aspect 8（Scalability）时。

---

## 目录

- [HotPlex 热路径性能模式](#hotplex-热路径性能模式)
- [通用 Go 性能反模式](#通用-go-性能反模式)
- [检测方法论](#检测方法论)
- [验证手段](#验证手段)

---

## HotPlex 热路径性能模式

### HP-1: 每消息 JSON 编码开销

**路径**: `pkg/aep/codec.go:176` → `internal/gateway/conn.go:536`

**问题**: 每个出站 AEP 事件调用 `json.Marshal`，`Envelope.Data` 类型为 `interface{}`（反射编码），无 buffer 复用。高频 `message.delta` 事件下是主要分配源。

**检测信号**:
- `json.Marshal` 在循环或 per-message 上下文中
- `interface{}` / `any` 类型字段强制反射编码
- 频繁调用 `uuid.NewString()`（crypto/rand 开销）

**优化方向**:
```go
// 当前：每消息完整 marshal
data, _ := json.Marshal(env)

// 优化：同一事件广播到多连接时，marshal 一次（Hub 已实现此优化）
// 进一步：sync.Pool 复用 bytes.Buffer，或考虑 jsoniter/sonic 替代
```

**HotPlex 现状**: Hub `routeMessage` 已实现 lazy encode（一次 marshal，多连接共享字节），这是好的。但 WriteCtx 的 per-connection encode 仍有优化空间。

### HP-2: Event.Clone 深拷贝开销

**路径**: `pkg/events/events.go:103-113`

**问题**: `Clone()` 通过 `json.Marshal` + `json.Unmarshal` 往返实现深拷贝。用于 `sendControlToSession` 等控制消息广播。高频流式场景下产生显著 GC 压力。

**检测信号**:
- `json.Marshal` + `json.Unmarshal` 组合用于深拷贝
- 在循环或广播路径中调用 Clone
- 大型 Event 对象的频繁克隆

**优化方向**: 考虑基于 gob/msgpack 的序列化，或对不需要深拷贝的路径传递 `[]byte` 而非结构体。

### HP-3: Streaming Card 全量重建

**路径**: `internal/messaging/feishu/streaming.go:428-501`

**问题**: 每 150ms flush 重建完整 card 内容（`SanitizeForCard` 正则处理 + 完整 JSON 发送）。随流式输出增长，payload 线性增长，无增量更新。

**检测信号**:
- 固定间隔（如 ticker）触发完整内容重建
- `SanitizeForCard` 或类似正则处理在每次 flush 对增长中内容执行
- `content == c.lastFlushed` 全字符串比较 O(n)

**优化方向**:
```go
// 当前：每次发送完整内容
SanitizeForCard(fullContent)  // 对整个累积内容做正则

// 优化：只处理增量部分，或设置内容截断阈值
if len(delta) > 0 {
    SanitizeForCard(delta)  // 只处理新增部分
}
```

### HP-4: Hub 单线程广播瓶颈

**路径**: `internal/gateway/hub.go:399-440`

**问题**: 所有 session 的消息路由通过单个 `Run` goroutine。高并发（多活跃 session）时序列化所有路由。

**检测信号**:
- 全局单 goroutine 处理所有 session 的消息路由
- `routeMessage` 内含 encode + write 循环阻塞单个 goroutine
- 每次路由 `make([]SessionWriter, 0, len(sessionConns))` 分配

**优化方向**: 分片路由（按 session ID hash 到多个 router goroutine），或 encode 完成后异步 write。

### HP-5: Worker 双重 JSON 解析

**路径**: `internal/worker/claudecode/worker.go:607-627`

**问题**: Claude Code 适配器中，每行输出先 unmarshal 检查 `type == "control_response"`，再由 `parser.ParseLine` 重新 unmarshal。非控制响应事件被解析两次。

**检测信号**:
- 同一 JSON 字符串被 `json.Unmarshal` 两次
- "先检查类型再完整解析" 模式

**优化方向**:
```go
// 当前：两次解析
json.Unmarshal(line, &peek)  // 检查类型
w.parser.ParseLine(line)      // 再次完整解析

// 优化：一次解析，根据类型分流
var raw json.RawMessage
json.Unmarshal(line, &raw)
// 根据 type 字段分流到不同处理器
```

### HP-6: WriteCtx 锁竞争

**路径**: `internal/gateway/conn.go:528-548`

**问题**: `WriteCtx`、`WriteMessage`、`Close` 竞争同一 `sync.Mutex`。高频流式场景下序列化所有写操作。

**检测信号**:
- 单一 `sync.Mutex` 保护写路径（非 RWMutex）
- 高频调用路径上的互斥锁
- 写路径中包含 JSON 编码（CPU 密集 + 锁持有时间叠加）

**优化方向**: 将 JSON 编码移到锁外（encode → lock → write → unlock），或使用 channel-based writer 模式。

### HP-7: Session Manager 缓存未命中路径

**路径**: `internal/session/manager.go:982-1006`

**问题**: `getManagedSession` 缓存未命中时执行 RLock → miss → 释放 → DB read → Lock → insert → 释放。两次锁获取 + DB I/O。

**检测信号**:
- 同一操作中多次获取/释放锁
- 锁路径中包含 DB I/O
- TOCTOU 模式（check-then-act with lock gap）

**HotPlex 现状**: 这是有意的设计权衡 — 避免在读路径持有写锁。缓存命中时只走 RLock 快路径，影响可控。

---

## 通用 Go 性能反模式

### GP-1: 热路径分配

| 反模式 | 检测方法 | 优化 |
|--------|----------|------|
| `fmt.Sprintf` 拼接 | grep 热路径中的 Sprintf | `strings.Builder` 或 `strconv` |
| `make([]T, 0)` 循环内 | 每次迭代分配新 slice | 预分配或 `sync.Pool` |
| `string(b)` / `[]byte(s)` | 频繁 bytes↔string 转换 | 保持统一类型，或使用 `unsafe`（需谨慎） |
| `interface{}` 参数 | 反射开销 | 泛型或类型特化 |
| 闭包捕获循环变量 | 每次迭代分配闭包 | 提取变量到局部 |

### GP-2: 锁竞争模式

| 反模式 | 检测方法 | 优化 |
|--------|----------|------|
| 全局 `sync.Mutex` | 单锁保护大 map | 分片锁 / `sync.Map`（读多写少） |
| `Mutex` vs `RWMutex` | 写少读多用 Mutex | 改用 `RWMutex` |
| 锁内 I/O | 锁持有期间调用 DB/HTTP | 移到锁外 |
| 锁内 JSON 编码 | 锁持有期间 CPU 密集操作 | 编码移到锁前 |

### GP-3: Goroutine 管理

| 反模式 | 检测方法 | 优化 |
|--------|----------|------|
| `go func()` 无边界 | 每次 request/event 启动 goroutine | Worker pool（ants/tunny） |
| `time.After` in select loop | 每次 select 创建 timer 泄漏 | 复用 `time.Timer` + `Reset` |
| goroutine 无退出条件 | 缺少 `ctx.Done()` 检查 | 总是传入 context |
| `sync.WaitGroup` 不平衡 | `Add`/`Done` 不匹配 | defer Done |

### GP-4: 连接/资源复用

| 反模式 | 检测方法 | 优化 |
|--------|----------|------|
| 每次 `http.NewRequest` | 未复用 Transport | 全局 `http.Client` + 连接池 |
| 未关闭 `io.ReadCloser` | defer 缺失或 error 路径遗漏 | `defer Close()` |
| `json.NewDecoder` 每次创建 | 在流式场景中重复创建 | 复用 Decoder |

---

## 检测方法论

### 代码级检测（分析时使用）

按以下优先级扫描性能问题：

1. **分配热点**: 搜索 `make(`, `&T{}`, `json.Marshal`, `fmt.Sprintf` 在热路径中的使用
2. **锁竞争**: 搜索 `sync.Mutex`, `.Lock()`, `.RLock()` 在高频函数中的持有时间
3. **Goroutine 泄漏**: 搜索 `go func()` 或 `go method()` 检查退出条件
4. **重复计算**: 同一函数/循环中多次执行相同计算或 marshal
5. **缓冲区管理**: 搜索 `bytes.Buffer`, `strings.Builder` 的创建位置 — 是否可复用

### HotPlex 热路径清单

分析性能时，优先检查以下代码路径：

| 路径 | 频率 | 文件 |
|------|------|------|
| AEP 编码 | 每消息 | `pkg/aep/codec.go` |
| Hub 路由 | 每消息 | `internal/gateway/hub.go` |
| WS 写入 | 每消息/连接 | `internal/gateway/conn.go` |
| Card flush | ~6.7次/秒 | `internal/messaging/feishu/streaming.go` |
| Worker 行读取 | 每行输出 | `internal/worker/*/worker.go` |
| Event 持久化 | 每事件 | `internal/eventstore/collector.go` |

---

## 验证手段

性能相关的发现应在 AC 中包含可量化的验证方式：

### Benchmark 验证

```go
// 发现涉及分配优化时，要求 benchmark 证明
func BenchmarkWritePump_Encode(b *testing.B) {
    // before/after 对比
}
```

### pprof 验证

```bash
# CPU profile — 证明热点已消除
go tool pprof -http=:8080 cpu.prof

# Memory profile — 证明分配已减少
go tool pprof -http=:8080 mem.prof

# Mutex profile — 证明锁竞争已缓解
go tool pprof -http=:8080 mutex.prof
```

### Race Detector

```bash
# 性能优化不能引入数据竞争
go test -race ./...
```

### 关键指标

| 指标 | 工具 | 关注点 |
|------|------|--------|
| ns/op | `go test -bench` | 操作延迟 |
| allocs/op | `go test -bench -benchmem` | 分配次数 |
| B/op | `go test -bench -benchmem` | 分配大小 |
| goroutine count | `runtime.NumGoroutine()` | 泄漏检测 |
| GC pause | `GODEBUG=gctrace=1` | GC 压力 |
