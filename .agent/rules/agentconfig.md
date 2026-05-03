---
paths:
  - "**/agentconfig/**/*.go"
---

# Agent Config 规范

> 加载 personality/context 配置，构建统一 system prompt
> 参考：`internal/agentconfig/loader.go`、`internal/agentconfig/prompt.go`

## 三级目录 Fallback 架构

每个配置文件独立解析，按以下优先级查找：

```
1. dir/{platform}/{botID}/{file}    — Bot 级（最高优先级）
2. dir/{platform}/{file}            — 平台级
3. dir/{file}                       — 全局级（兜底）
```

```
~/.hotplex/agent-configs/
├── SOUL.md                         ← 全局默认
├── AGENTS.md
├── SKILLS.md
├── USER.md
├── MEMORY.md
├── slack/                          ← Slack 平台级
│   ├── SOUL.md
│   └── U12345/                     ← Slack Bot 级（UserID from auth.test）
│       └── SOUL.md
├── feishu/                         ← 飞书平台级
│   └── ou_abc123/                  ← 飞书 Bot 级（OpenID from Bot API）
│       └── SOUL.md
└── webchat/                        ← WebChat 平台级
    └── my-bot/                     ← WebChat Bot 级（JWT bot_id）
        └── SOUL.md
```

### B 通道（`<directives>`）— 高优先级指令
- **SOUL.md**：Agent 身份定义、核心原则、沟通风格
- **AGENTS.md**：详细行为规范、响应模式
- **SKILLS.md**：可用技能清单、调用方式
- 注入位置：`<directives>` XML 组内，无 hedging 信号词

### C 通道（`<context>`）— 背景参考
- **USER.md**：用户角色、偏好、背景（降低幻觉）
- **MEMORY.md**：长期记忆、会话历史摘要
- 注入位置：`<context>` XML 组内，作为参考信息

## 加载逻辑

```go
// loader.go — Load(dir, platform, botID)
// resolveFile: 按优先级查找文件，找到第一个非空文件即返回
// 空 frontmatter-only 文件视为"未找到"，继续 fallthrough
// botID 安全校验：filepath.Base(botID) == botID（防路径穿越）
```

**旧后缀格式已废弃**：`SOUL.slack.md` 不再加载。启动时扫描并输出 deprecation warning。

## META-COGNITION.md（Agent 自画像）

`internal/agentconfig/META-COGNITION.md` — 通过 `go:embed` 编译时注入，5 节结构：identity、system architecture、session lifecycle、agent config architecture、control commands

**注入方式**：作为 C 通道 `<hotplex>` 子节点注入，与 USER.md/MEMORY.md 同级：
```xml
<context>
  <hotplex>  ← META-COGNITION.md 内容
  <user>     ← USER.md 内容
  <memory>   ← MEMORY.md 内容
</context>
```

## 统一 System Prompt 构建

```xml
<agent-configuration>
  <directives>
    <soul>        ← SOUL.md
    <agents>      ← AGENTS.md
    <skills>      ← SKILLS.md
  </directives>
  <context>
    <hotplex>     ← META-COGNITION.md（go:embed）
    <user>        ← USER.md
    <memory>      ← MEMORY.md
  </context>
</agent-configuration>
```

**CC 注入**：`--append-system-prompt` flag
**OCS 注入**：`system` 字段（HTTP 请求体）

## BotID 传播路径

```
Adapter.Start() → adapter.botID / adapter.botOpenID
  → GetBotID() → Bridge.Handle() → StartPlatformSession(..., botID)
    → gateway.Bridge → injectAgentConfig(info, platform, botID)
      → agentconfig.Load(dir, platform, botID) → resolveFile per file
```

## 大小限制

| 限制 | 值 | 原因 |
|------|-----|------|
| 单文件上限 | 8KB | 防止 prompt 爆炸 |
| 总量上限 | 40KB | Gateway 内存 + Token 成本 |

超限 → `ErrConfigFileTooLarge` / `ErrTotalConfigTooLarge`。

## YAML Frontmatter 剥离

配置文件头部允许 YAML frontmatter（`---...---`），`loader.go stripFrontmatter()` 自动剥离。元数据放 frontmatter，正文注入 prompt。
