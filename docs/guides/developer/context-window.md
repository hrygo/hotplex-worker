---
title: Context Window 管理
weight: 13
description: 理解和控制 AI Agent 的 token 预算，上下文压缩策略与长对话优化
---

# Context Window 管理

> 理解和控制 AI Agent 的 token 预算，优化长对话体验

## 概述

HotPlex Gateway 通过 AEP v1 协议的 `context_usage` 事件实时报告 Worker 的 context window 使用情况。合理管理 context window 是保持 AI Agent 高效运行的关键——超出窗口限制会导致早期对话被截断，丢失重要上下文。

## /context 命令

通过 `/context` 斜杠命令（或 `worker_command` 事件类型 `context_usage`）查询当前 token 用量。

**返回数据**（`ContextUsageData` 结构体）：

| 字段 | 类型 | 含义 |
|------|------|------|
| `total_tokens` | int | 当前已使用的总 token 数 |
| `max_tokens` | int | 模型的 context window 上限 |
| `percentage` | int | 使用百分比（0-100） |
| `model` | string | 当前使用的模型名称 |
| `categories` | []ContextCategory | 按类别细分的 token 用量 |
| `memory_files` | int | MEMORY.md 等记忆文件数量 |
| `mcp_tools` | int | 已加载的 MCP 工具数量 |
| `agents` | int | Agent 配置文件数量 |
| `skills` | ContextSkillInfo | Skills 相关的 token 消耗 |

> **来源说明**：`memory_files`、`mcp_tools`、`agents`、`skills` 等字段来自 Worker（如 Claude Code）通过 `context_usage` 事件上报的数据，由 Worker 端计算和填充，非 Gateway 定义。Gateway 仅透传这些信息。

**使用示例**：

```
/context
→ total_tokens: 62000, max_tokens: 200000, percentage: 31%
  categories:
    - system_prompt: 4500 tokens
    - conversation: 48000 tokens
    - tool_results: 8000 tokens
    - memory: 1500 tokens
```

## /compact 命令

`/compact` 触发 Worker 对早期对话轮次进行摘要压缩，释放 context window 空间。

**适用时机**：
- 使用率达到 ~80% 时
- 开始一个复杂的新任务前（确保有足够空间）
- 长时间对话中定期执行

**工作原理**：
1. Worker 将较早的对话轮次总结为简短摘要
2. 保留最近轮次的完整内容
3. 总 token 数显著下降
4. 对话连续性保持不变

**最佳实践**：

```
# 在长对话中，每 10-15 轮执行一次
/compact

# 在开始复杂任务前
/context     # 先检查当前用量
/compact     # 压缩，为新任务腾出空间
```

## /clear 与 /reset 的区别

| 命令 | 效果 | Worker 进程 | 对话历史 | 适用场景 |
|------|------|------------|---------|---------|
| `/clear` | 清空对话历史 | 复用（in-place） | 完全清除 | 重新开始话题，但保持配置 |
| `/reset` | 清空上下文 + 可能重启 Worker | 可能重启 | 完全清除 | 彻底重置，回到初始状态 |

**`/clear`**：发送 passthrough 命令 `StdioClear`，Worker 执行 in-place 清空。Session 保持 `RUNNING` 状态。

**`/reset`**：发送 AEP `control.reset`，Gateway 清空 `SessionInfo.Context`，Worker 自行决定 in-place 或 terminate+start。Session 状态短暂切回 `RUNNING`。

## Auto-Compaction

Brain 层在 `total_tokens` 超过阈值时自动触发 compaction（默认阈值可在配置中调整）。自动 compaction 与手动 `/compact` 使用相同的底层机制，但触发时机由系统控制。

当 `context_used_percent` 接近上限时，`done` 事件的 `stats` 中会包含 `context_used_percent` 字段，开发者应关注此指标以预判何时需要手动压缩。

## Token 预算意识

### B/C 通道的 token 消耗

Agent 配置通过 B/C 双通道注入，每个通道都消耗 context window：

**B 通道（`<directives>`）**—— 控制性指令：
- `META-COGNITION.md`（go:embed，始终存在且排首位）
- `SOUL.md`（人格定义）
- `AGENTS.md`（行为规则）
- `SKILLS.md`（技能列表）

**C 通道（`<context>`）**—— 上下文信息：
- `USER.md`（用户信息）
- `MEMORY.md`（跨会话记忆）

三级 fallback 机制（全局 → 平台 → Bot）意味着配置文件越多，token 消耗越大。`/context` 返回的 `categories` 中可以查看各类配置的具体 token 占用。

### Token 预算规划

假设模型 context window 为 200K tokens：

| 组成部分 | 典型用量 | 说明 |
|---------|---------|------|
| System Prompt（B 通道） | 2K-8K | 固定开销 |
| Memory Files（C 通道） | 1K-5K | 随记忆积累增长 |
| MCP Tools 声明 | 1K-3K | 每个 MCP server 增加约 500 tokens |
| Skills 列表 | 0.5K-2K | 取决于启用的 skill 数量 |
| 对话历史 | 动态增长 | 每轮约 1K-5K tokens |
| Tool Results | 动态增长 | 文件读取输出是大头 |

**建议**：保持 30% 以上余量用于复杂任务。当使用率超过 70% 时考虑执行 `/compact`。

## 长对话最佳实践

1. **定期检查**：每 10 轮执行 `/context` 了解用量趋势
2. **主动压缩**：使用率 ~80% 时执行 `/compact`
3. **分段工作**：复杂任务拆分为多个 Session，通过 `/gc` 归档后 Resume
4. **控制输出**：让 AI 给出简洁回答而非冗长解释，减少 token 消耗
5. **及时归档**：完成一个主题后 `/gc`，开始新主题时通过新 Session 保持 context 清洁
6. **监控 tool output**：文件读取是 token 消耗大户，指导 AI 只读必要内容

---

## 延伸阅读

- [Session 生命周期](../../explanation/session-lifecycle.md) — Session 状态机与上下文回收机制的设计原理
