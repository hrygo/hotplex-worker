# Slack Adapter

Socket Mode 平台适配器：流式消息、交互管理、状态展示、STT 语音转写。

## 核心文件

| 文件 | 职责 |
|------|------|
| `adapter.go` | Adapter + SlackConn：Socket Mode 事件循环、WriteCtx 事件分发、流式写入 |
| `status.go` | StatusManager：AI 处理状态展示（Assistant API + emoji fallback）、工具状态格式化 |
| `interaction.go` | Permission/Q&A/Elicitation 交互卡片 |
| `validator.go` | 消息验证、去重、截断、格式转换 |
| `streaming.go` | SlackStreamingWriter：150ms flush、20-rune 阈值、append 重试 |
| `format.go` | CommonMark → Slack mrkdwn 转换 |
| `dedup.go` | TTL-based 消息去重 |
| `gate.go` | DM/channel 访问控制 |
| `backoff.go` | 指数退避重连策略 |
| `user_cache.go` | 用户信息缓存 |
| `slash.go` | Slash 命令处理 |
| `image.go` | 图片上传/URL 处理 |

## 状态展示系统

### 双通道架构

- **Assistant API**（付费 workspace）：`SetAssistantThreadsStatus` 原生状态文本
- **Emoji fallback**（免费 workspace）：reaction emoji 替代

启动时异步 probe 检测能力，`StatusManager.SetEmojiOnly` 一次性切换。

### 工具状态格式化

`status.go` 中的 `toolStatusFormatters` 注册表为常用工具提供定制化展示：

```go
// 注册表查找 → 专用 formatter → fallback 通用格式
var toolStatusFormatters = map[string]toolStatusFormatter{
    "TodoWrite": ...,   // 📋 Fixing auth bug / 📋 3 tasks (1 done · 2 pending)
    "Read":     ...,    // 📖 Reading main.go
    "Edit":     ...,    // ✏️ Editing main.go
    "Write":    ...,    // 📝 Writing new.go
    "Bash":     ...,    // ⏳ make test-short
    "Grep":     ...,    // 🔍 pattern in path
    "Glob":     ...,    // 📂 **/*.go
    "Agent":    ...,    // 🤖 description
    "LSP":      ...,    // 🔎 Go to def main.go
    ...
}
```

未注册工具自动 fallback 到 `Name(key=val)` 通用格式，截断到 `statusTextLimit`（80 rune）。

### 持续进化机制

`StatusManager.LogOnceUnregistered` 对每个未注册工具名仅 log 一次（`sync.Map` 去重）：

```
DEBUG slack: unregistered tool in status formatter, consider adding to toolStatusFormatters tool=<name>
```

**新增 formatter 流程：**

1. 在日志中观察高频出现的未注册工具名
2. 在 `toolStatusFormatters` 中添加条目
3. 实现对应的 `formatXxxStatus` 函数
4. 在 `adapter_test.go` 的 `TestExtractToolCallStatus` 中添加测试用例
5. `make test-short && make lint` 验证

### 速率控制

- 状态更新最小间隔：3s（`statusMinInterval`）
- 文本去重：相同文本跳过
- 过期线程状态清理：1h TTL（`threadStateTTL`）
