# Phrases 配置手册

## 概述

Phrases 模块提供可配置的程序化 UI 短语池，用于平台适配器的占位卡片、状态指示器等 UI 反馈。

## 目录结构

```
~/.hotplex/phrases/
├── PHRASES.md              # 全局短语（所有平台共享）
├── feishu/
│   ├── PHRASES.md          # 飞书平台短语
│   └── ou_xxx/
│       └── PHRASES.md      # 特定 bot 短语
└── slack/
    ├── PHRASES.md          # Slack 平台短语
    └── U12345/
        └── PHRASES.md      # 特定 bot 短语
```

## 合并规则

采用 **cascade-append（级联追加）** 语义，与 agent-config 的"命中即终止"相反：

1. **代码默认值** — 内置的 greetings/tips/status
2. **全局** — `~/.hotplex/phrases/PHRASES.md`
3. **平台级** — `~/.hotplex/phrases/{platform}/PHRASES.md`
4. **Bot 级** — `~/.hotplex/phrases/{platform}/{botID}/PHRASES.md`

每级的条目追加到池中，**不替换**。条目越多，池越丰富。

**示例**：全局定义 8 条 greetings，飞书平台追加 3 条，特定 bot 追加 2 条 → 该 bot 有 13 条 greetings 可用。

## 文件格式

Markdown 格式，简单、可编辑、git-friendly：

```markdown
## Greetings
- 来啦～
- 交给我～
- 收到，马上～

## Tips
- 输入 /gc 可休眠当前会话，下次发消息自动恢复
- 输入 /reset 可重置上下文，从零开始新对话

## Status
- 正在思考...
- 马上回复～

## Custom
- 自定义分类名称
- 通过 phrases.Random("custom") 访问
```

### 解析规则

- `## Name`（不区分大小写）定义一个分类
- `- text` 是列表条目，条目间空行可选
- 既非标题也非列表的行被忽略（允许自由注释）
- 未知的 `## Section` 名称可通过 `Random("section-name")` 访问

## 内置分类

| 分类 | 用途 | 默认条目数 |
|------|------|-----------|
| `greetings` | 占位卡片欢迎语 | 8 |
| `tips` | 占位卡片 CLI 提示 | 17 |
| `status` | Slack 助手状态文本 | 4 |

## 生效方式

配置在 **adapter 初始化时加载**，与 agent-config 一致。修改后需要 **重启 gateway** 才能生效。
