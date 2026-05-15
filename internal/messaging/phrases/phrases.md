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

### Fallback 语义

外部配置（全局/平台/Bot 级）**完全覆盖**同分类的代码默认值。仅当一个分类没有任何外部配置时，才 fallback 到代码默认值。

**示例**：Bot 级只配了 `greetings` → greetings 使用 bot 级条目，`tips` 和 `status` 仍使用代码默认值。

### 加权选择

各层级条目在随机选择时拥有不同的权重，层级越高被选中概率越大：

| 层级 | 权重 | 路径 |
|------|------|------|
| Bot 级 | 4 | `~/.hotplex/phrases/{platform}/{botID}/PHRASES.md` |
| 全局 | 2 | `~/.hotplex/phrases/PHRASES.md` |
| 平台级 | 1 | `~/.hotplex/phrases/{platform}/PHRASES.md` |
| 代码 fallback | 1（均匀） | 内置默认值，仅在该分类无外部配置时使用 |

**概率示例**：若全局配 2 条 greetings（weight=2），平台配 1 条（weight=1），Bot 配 1 条（weight=4），总权重 = 2×2 + 1×1 + 1×4 = 9。Bot 级条目被选中概率 = 4/9 ≈ 44%。

### 级联追加

同一层级内的多个条目追加到池中，不替换。条目越多，池越丰富。

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
