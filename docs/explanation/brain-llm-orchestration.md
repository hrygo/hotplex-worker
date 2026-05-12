---
title: Brain/LLM 编排层
weight: 4
description: HotPlex Brain 智能中间件：意图路由、安全审计、上下文压缩、4 层 API Key 发现与装饰器链
persona: developer
difficulty: advanced
---

# Brain/LLM 编排层

> 为什么 HotPlex 在 Worker（Claude Code）之上还需要一个独立的 LLM 编排层，以及这个层如何通过分层接口和中间件链实现"系统 1"智能。

## 核心问题

HotPlex 的核心执行路径是：用户消息 -> Gateway -> Worker（Claude Code / OpenCode Server）。Worker 本身已经是强大的 AI Agent，为什么还需要在它之上再加一个 LLM 编排层？

原因在于**职责分离和成本优化**。不是每条消息都需要启动一个完整的 Worker 进程来处理：

- 用户发送 "你好" -> 不需要启动 Claude Code，Brain 直接回复即可
- 用户发送 "帮我查一下系统状态" -> Brain 可以自行处理，省去 Worker 的冷启动开销
- 用户发送 "帮我重构这个模块" -> 这个确实需要 Worker，Brain 的价值在于**安全检查和意图路由**

Brain 层充当"系统 1"（Daniel Kahneman 的术语）——快速、直觉、低成本的模式识别和分类，让 Worker 专注于"系统 2"——深度推理和代码操作。

## 设计决策

### 4 层接口层级

Brain 的接口设计遵循 Interface Segregation Principle——消费者只依赖它需要的方法：

```
Brain                    <- 基础：Chat + Analyze
  |- StreamingBrain      <- 扩展：ChatStream（token-by-token）
      |- RoutableBrain   <- 扩展：ChatWithModel + AnalyzeWithModel
          |- ObservableBrain  <- 扩展：GetMetrics + GetCostCalculator
```

> **注意**：上图表示概念层级（能力递增），不是代码继承关系。四个接口相互独立，通过 `enhancedBrainWrapper` 同时实现所有接口。

**为什么是层级而非扁平接口？** 不同的消费者需要不同的能力：

| 消费者 | 需要的接口 | 使用的方法 |
|--------|-----------|-----------|
| IntentRouter | Brain | `Analyze` 分类意图 |
| ContextCompressor | Brain | `Chat` 生成摘要 |
| SafetyGuard | Brain | `Analyze` 威胁检测 |
| Admin Dashboard | ObservableBrain | `GetMetrics` + `GetCostCalculator` |
| 流式对话 | StreamingBrain | `ChatStream` |

`enhancedBrainWrapper` 同时实现所有 4 个接口，编译时通过类型断言验证：

```go
var (
    _ Brain           = (*enhancedBrainWrapper)(nil)
    _ StreamingBrain  = (*enhancedBrainWrapper)(nil)
    _ RoutableBrain   = (*enhancedBrainWrapper)(nil)
    _ ObservableBrain = (*enhancedBrainWrapper)(nil)
)
```

### 4 层 API Key 发现

Brain 的配置从环境变量加载，API Key 按以下优先级发现：

```
1. HOTPLEX_BRAIN_API_KEY    -- 专用 Key（最高优先级）
2. Worker 配置文件           -- 扫描 ~/.claude/settings.json 和 ~/.config/opencode/opencode.json
3. 系统环境变量              -- ANTHROPIC_API_KEY -> OPENAI_API_KEY -> SILICONFLOW_API_KEY -> DEEPSEEK_API_KEY
4. 未找到                   -- Brain 禁用，所有功能优雅降级
```

**为什么需要 4 层？** 这让 Brain 在零配置下也能工作。如果用户已经配置了 Claude Code 的 API Key（`ANTHROPIC_API_KEY` 或 `~/.claude/settings.json`），Brain 会自动复用它，不需要额外的配置步骤。只有当需要 Brain 使用不同的模型或 Provider 时，才需要设置 `HOTPLEX_BRAIN_API_KEY`。

`extractFromWorker` 函数扫描 Worker 的配置文件提取凭证：

```go
var extractors = []struct {
    name     string
    extract  func() (*ExtractedConfig, error)
    provider string
    protocol string
    defModel string
}{
    {"claude-code", func() (*ExtractedConfig, error) { return NewClaudeCodeExtractor().Extract() },
        "anthropic", "anthropic", "claude-3-7-sonnet-latest"},
    {"opencode", func() (*ExtractedConfig, error) { return NewOpenCodeExtractor().Extract() },
        "openai", "openai", "gpt-4o"},
}
```

### Decorator Chain 中间件栈

Brain 的 LLM 客户端使用装饰器模式构建中间件链，按顺序叠加功能：

```
BaseClient (OpenAI / Anthropic)
  |- RetryClient (指数退避重试，默认 3 次)
      |- CachedClient (LRU 缓存，默认 1000 条)
          |- CircuitBreakerClient (故障熔断，默认关闭)
```

每个装饰器实现 `LLMClient` 接口，可以独立叠加或移除。

**为什么 Rate Limiting 不在装饰器链中？** Rate limiting 在 `enhancedBrainWrapper.applyRateLimit` 层面处理。如果作为装饰器加入链中，会与可能内置限流的 Provider 客户端产生双重限流。在 wrapper 层面处理可以精确控制限流粒度（per-model 而非 per-client）。

### 13 子配置结构

Brain 的 Config 聚合了 13 个子配置，每个控制一个独立的功能面：

| 子配置 | 功能 | 默认状态 |
|--------|------|---------|
| Model | LLM 后端（provider/model/endpoint） | auto-detect |
| Cache | 响应缓存 | enabled, 1000 条 |
| Retry | 重试策略 | enabled, 3 次, 100-5000ms |
| Metrics | 可观测性 | enabled |
| Cost | 成本追踪 | enabled |
| RateLimit | 请求限流 | disabled |
| Router | 模型路由 | disabled |
| CircuitBreaker | 故障熔断 | disabled |
| IntentRouter | 意图路由 | enabled, 置信度 0.7 |
| Memory | 上下文压缩 | enabled, 阈值 8000 token |
| Guard | 安全审计 | enabled |

大多数功能开箱即用，只有高级功能（模型路由、限流、熔断）需要显式启用。

## 内部机制

### Intent Router：快速路径 vs AI 分析

Intent Router 使用两阶段分类策略来平衡准确性和延迟：

**阶段 1：Fast Path（零 API 调用）**

基于规则的快速检测，处理显而易见的情况：

| 模式 | 分类 | 延迟 |
|------|------|------|
| 短消息（< 3 字符） | `chat` | ~0ms |
| 问候语（"hi"、"hello"） | `chat` | ~0ms |
| 感谢消息 | `chat` | ~0ms |
| 状态查询（"ping"、"status"） | `command` | ~0ms |
| 代码关键词（"function"、"debug"） | 返回 nil -> 进入阶段 2 | ~0ms |

代码关键词检测是一个**反向快速路径**——当消息中包含代码相关词汇时，Fast Path 不做判断，而是将决策交给 Brain AI，因为代码消息的意图可能很复杂（"debug this function" 可能是任务，也可能是闲聊）。

**阶段 2：Brain Analysis（AI 驱动）**

构造 JSON prompt 发送给 Brain，要求返回 `{intent, confidence, reason, response}`。分类结果缓存在 LRU cache 中（默认 1000 条，使用 SHA256 哈希作为 key），相同的消息不会重复调用 Brain API。

**降级策略**：当 Brain 不可用时，所有消息默认路由到 Engine（`IntentTypeTask`），确保功能不受影响——用户不会因为 Brain 故障而无法使用 Agent。

### Safety Guard：输入/输出双层防护

Safety Guard 在用户消息到达 Worker 之前和 Worker 输出返回用户之后各执行一次检查。

**输入检查（CheckInputWithUser）** -- 4 层递进式检查：

```
1. Rate Limit     -> Per-user 令牌桶限流（10 RPS，burst 20）
2. Length Check   -> 超过 MaxInputLength（默认 100K）直接 block
3. Pattern Scan   -> 正则匹配 12 种已知攻击模式
4. Deep Analysis  -> Brain AI 分析微妙威胁（sensitivity > "low" 时启用）
```

正则匹配覆盖的攻击模式包括：
- "ignore previous instructions"（prompt injection）
- "you are now in developer mode"（jailbreak）
- "DAN mode"、"reveal your system prompt"（信息提取）

匹配到任何一条直接 block，不进入 AI 分析。这是性能优化的关键——大部分恶意输入可以通过简单正则拦截，不需要调用 Brain API。

AI 分析（`deepInputAnalysis`）用于检测正则无法捕获的变体攻击，如用自然语言伪装的 prompt injection。Brain 返回 threat_level 和 threat_type，Guard 据此决定 allow 或 block。

**输出检查（CheckOutput）** -- 只做 sanitize，不 block：

检测 7 种敏感数据模式并替换为 `[REDACTED]`：
- API Keys（`api_key=xxx`）
- AWS Access Keys（`AKIA...`）
- Private Keys（`-----BEGIN RSA PRIVATE KEY-----`）
- JWT Tokens（`eyJ...`）
- 内网 IP 地址（`10.x`、`172.16-31.x`、`192.168.x`）
- 数据库连接字符串（`postgres://user:pass@host`）
- 密码（`password=xxx`）

**Per-user Rate Limiter 管理**：

SafetyGuard 维护 `map[string]*rate.Limiter`（userID -> 令牌桶）。这个 map 是无界的，但有后台清理 daemon（10 分钟间隔）驱逐超过 1 小时未活跃的 limiter，防止长期运行的 Gateway 内存泄漏。当 map 尺寸小于 100 条时跳过驱逐，避免不必要的遍历开销。

### Context Compression：Token 预算管理

长对话会导致 context window 溢出。ContextCompressor 通过"保留近期 + 摘要历史"解决：

```
压缩前：Turn1 + Turn2 + ... + Turn10 = 8000+ tokens
压缩后：[Turn1-7 的摘要 ~500 tokens] + Turn8 + Turn9 + Turn10 = ~2000 tokens
```

**触发条件**：Session 的 `TotalTokens` 超过 `TokenThreshold`（默认 8000）。

**压缩算法**：
1. 保留最近 `PreserveTurns`（默认 5）轮对话不变
2. 将更早的对话轮次发送给 Brain，生成摘要（最大 `MaxSummaryTokens`，默认 500）
3. 用摘要替换旧轮次，更新总 token 计数
4. 目标压缩比 `CompressionRatio` 默认 0.25（压缩到 25%）

**后台清理**：每个 Session 的压缩历史有 TTL（默认 24 小时），后台 daemon 每小时扫描一次清除过期 Session 的内存占用。

### Chat2Config：自然语言配置修改（默认关闭）

Chat2Config 允许用户通过自然语言修改 Gateway 配置：

```
"切换模型为 opus"     -> {action: "set", target: "model", value: "opus"}
"当前是什么模型"      -> {action: "get", target: "model"}
"列出可用的模型"      -> {action: "list", target: "models"}
```

**为什么默认关闭？** 这是一个明显的安全风险——允许用户通过聊天修改系统配置。只有显式设置 `HOTPLEX_BRAIN_CHAT2CONFIG_ENABLED=true` 才会启用。即使启用，某些操作（如模型切换）也需要管理员审批。

### 全局单例与并发安全

Brain 使用 `sync.RWMutex` 保护全局实例。IntentRouter 使用 `atomic.Pointer[IntentRouter]`（而非旧的 `sync.Once` + 直接赋值模式，那曾导致数据竞争）。所有组件的 public 方法都是并发安全的——cache 有独立的 mutex，metrics 使用 `atomic.Int64`，不依赖外部锁。

`GlobalIntentRouter()` 使用 `CompareAndSwap` 实现懒初始化，允许 `InitIntentRouter` 在并发调用时安全地覆盖默认实例。

## 权衡与限制

1. **Brain 不可用时的功能降级**：当没有配置 API Key 时，IntentRouter 将所有消息路由到 Engine，SafetyGuard 跳过 AI 分析只做正则匹配。功能不受影响，但安全性和智能性降低。

2. **LRU Cache 的内存占用**：IntentRouter 的缓存默认 1000 条。每条缓存存储完整的 `IntentResult`（含 response 字符串），在高流量场景下可能占用数 MB 内存。当前没有基于内存压力的自动驱逐机制。

3. **ContextCompressor 的 sessions map**：存储所有活跃 Session 的完整对话历史。虽然有 TTL 清理，但在高并发短会话场景下（如大量 cron 触发），map 可能在清理间隔之间快速增长。

4. **SafetyGuard per-user limiter 的无界增长**：虽然有后台清理，但如果大量用户同时活跃，清理间隔之间的 limiter 数量可能很大。清理阈值设为 100 条以下跳过驱逐，在 10K+ 活跃用户的场景下可能需要调整。

5. **模型路由的有限策略**：只支持 `cost_priority` 和 `latency_priority` 两种策略，不支持基于场景的动态策略切换（如"意图分类用便宜模型，安全审计用高准确率模型"）。

## 参考

- `internal/brain/brain.go` -- 4 层接口定义 + 全局单例
- `internal/brain/init.go` -- 初始化编排 + enhancedBrainWrapper
- `internal/brain/config.go` -- 13 子配置 + 4 层 API Key 发现
- `internal/brain/router.go` -- Intent Router + LRU cache
- `internal/brain/guard.go` -- Safety Guard + Chat2Config
- `internal/brain/memory.go` -- ContextCompressor + MemoryManager
- `internal/brain/extractor.go` -- Worker 配置文件凭证提取

---

## 相关实践

- [配置参考 — brain 配置段](../reference/configuration.md) — Brain/IntentRouter/SafetyGuard 的全部可配置参数
