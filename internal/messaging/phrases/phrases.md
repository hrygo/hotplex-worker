# Phrases 配置手册

可配置的程序化 UI 短语池，用于占位卡片、状态指示、欢迎语等平台反馈。默认值硬编码在 `phrases.Defaults()`，外部配置按分类覆盖默认值。

## 目录结构

```
~/.hotplex/phrases/
├── PHRASES.md                # 全局（所有平台共享，权重 2）
├── feishu/
│   ├── PHRASES.md            # 飞书平台（权重 1）
│   └── ou_xxx/
│       └── PHRASES.md        # 特定 bot（权重 4，最高优先）
└── slack/
    ├── PHRASES.md
    └── U12345/
        └── PHRASES.md
```

## 文件格式

Markdown，`## 分类名` + `- 条目文本`：

```markdown
## Greetings
- 来啦～
- 交给我～

## Persona
- 🧠 正在回忆上次对话...
- 📋 加载技能库...
```

- 分类名不区分大小写
- 条目间空行可选，非标题非列表的行被忽略
- 可自定义分类名，通过 `Random("name")` 访问

## 合并语义

1. **Fallback**：外部配置（任一层级）按分类覆盖代码默认值。未配置的分类仍用默认值
2. **追加**：同一分类的条目从全局→平台→Bot 级联追加到池中，不替换
3. **加权选择**：Bot 级(4) > 全局(2) > 平台级(1) > 代码默认(1)。权重越高被选中概率越大

示例：Bot 配了 `greetings` → greetings 用 Bot 级条目，`tips` 仍用默认值。

## 内置分类

| 分类 | 用途 | 使用位置 |
|------|------|---------|
| `greetings` | 占位卡片欢迎语 | 飞书 placeholder card 第一行 |
| `tips` | 占位卡片 CLI 提示 | 飞书 placeholder card 第二行 |
| `persona` | 准备中的人格化状态 | 飞书 tool_activity（placeholder 阶段，随机取 2 条） |
| `closings` | 完成签名语 | 飞书 tool_activity（turn 结束时） |
| `status` | 助手状态文本 | Slack assistant status |
| `welcome` | 首次进入聊天欢迎语 | 飞书 welcome card（支持 `{bot_name}` 占位符） |
| `welcome_back` | 回访用户欢迎语 | 飞书 welcome card |

## 配置操作

1. 确定目标层级目录（全局/平台/Bot）
2. 创建或编辑对应 `PHRASES.md`
3. 按格式添加 `## 分类` 和 `- 条目`
4. 重启 gateway 生效（adapter 初始化时加载）
