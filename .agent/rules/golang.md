---
paths:
  - "**/*.go"
---

# Go 编码规范

> 合并自原 `go126.md`（Go 1.26 语言特性）+ `golang-style.md`（Uber Go Style Guide）
> hotplex-worker 使用 Go 1.26，遵循 Uber Go Style Guide + 项目补充规则

---

## 格式化
- 行宽软限制 99 字符
- **三个 import group**：标准库 → 第三方库 → 项目内部包，用空行分隔
- goimports 必须带 `-local github.com/hotplex/hotplex-worker`
- 相似声明分组：`const`、`var`、类型各自集中
- 避免不必要的 import alias
- 减少嵌套：**优先 early return**
- `gofmt -s` 格式化
- 八进制字面量用 `0o755` 而非 `0755`

## 变量与类型
- 短声明用 `:=`，零值初始化用 `var`
- struct 初始化**使用 field name**
- **省略零值字段**，除非有特殊含义
- 空 map 用 `make(map[K]V)`，有初始数据用字面量
- slice 容量已知时 `make([]T, 0, cap)` 预分配
- 枚举从 **1 开始**（避免零值歧义）
- **瞬间用 `time.Time`**，**时长用 `time.Duration`**
- 原子操作用 `go.uber.org/atomic`

## 接口
- **接口值传递**，不要传指针
- 编译时验证实现：`var _ Worker = (*ClaudeCodeWorker)(nil)`
- receiver：value receiver 可接收值/指针，pointer receiver 只能接收指针

## 错误处理
- 静态错误：`errors.New("session not found")`
- 动态错误：`fmt.Errorf("session %s: %w", id, err)`（保留错误链）
- 错误变量前缀 `Err`，自定义类型后缀 `Error`（如 `SessionNotFoundError`）
- 每个错误**只处理一次**：不同时 log 又 return
- 避免堆叠 "failed to"
- `printf` 格式化字符串用 `const`
- **错误比较用 `errors.Is`**，不用 `==`（支持 wrapped errors）
- **类型断言用 `errors.As`**，不用 `err.(*Type)`（支持 wrapped errors）
- **Best-effort 清理操作**用 `_ =` 显式忽略：`_ = resp.Body.Close()`
- **defer 中的 Close** 用 `defer func() { _ = rows.Close() }()`

## 进程与并发
- **边界处复制 slice/map**，防止外部意外修改
- `sync.Mutex` / `sync.RWMutex` 零值即可，**禁止指针传递**，**禁止 embedding**，**显式命名** `mu`
- `exec.CommandContext` 传递 ctx，goroutine 监听 `ctx.Done()`
- nil slice 检查用 `len(s) == 0`

## 其他
- 尽可能**避免 `init()`**，保持行为确定性
- 使用 `testify/require` 而非 `t.Fatal`
- Functional Options 用于配置类 API：
  ```go
  type Option func(*Config)
  func WithTimeout(d time.Duration) Option
  ```

---

## Go 1.26 语言特性

## 默认生效（无需改代码）
- **Green Tea GC**：GC overhead ↓ 10-40%，长驻进程直接受益
- **Swiss Table Maps**：`make(map[K]V)` 使用新哈希表
- **Container-Aware GOMAXPROCS**：自动适配容器 CPU 限制
- **`io.ReadAll`**：2x faster，50% less allocation（Worker 输出流解析受益）
- **`fmt.Errorf`**：分配减少，等价于 `errors.New`

## 推荐使用
- **`log/slog`**：标准库结构化日志，统一使用
- **Generic Interfaces**：Worker 接口类型参数化（如 `Worker[T Event]`）
- **`weak.Value`**：session metadata LRU 缓存
- **`slices.Clone`**：slice 边界复制的标准方式
- **`unique.Make`**：字符串驻留
- **`new(expr)` 增强**：支持表达式初始化值，Worker 配置简化
- **自引用泛型约束**：更灵活的泛型设计

## Goroutine 泄漏检测（高优先级）
- 实验性 profile：`runtime/pprof` 的 `goroutineleak` 类型
- 启用：`GOEXPERIMENT=goroutineleakprofile`
- 预期 **Go 1.27 默认启用**
- **必须确保所有 goroutine 有 shutdown 路径**（ctx cancel / channel close / WaitGroup）

## 构建优化
```bash
go build -pgo=auto ./cmd/gateway  # Profile-Guided Optimization
```

## 现代化工具
```bash
go fix ./...  # 自动现代化代码（go fix 完全重写，数十个 fixer）
```

## Flight Recorder（生产诊断）
```bash
go tool trace trace.out
```
