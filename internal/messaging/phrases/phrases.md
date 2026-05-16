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

## Persona
- 🧠 正在回忆上次对话...
- 📋 加载技能库...

## Closings
- 搞定了！有事随时找我～
- ✅ 完成！还需要什么？

## Status
- 正在思考...
- 马上回复～

## Welcome
- Hi，我是 {bot_name}，你的 AI 编程助手！

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

| 分类 | 用途 | 使用位置 | 默认条目数 |
|------|------|---------|-----------|
| `greetings` | 占位卡片欢迎语 | 飞书 placeholder card 第一行 | 8 |
| `tips` | 占位卡片 CLI 提示 | 飞书 placeholder card 第二行 | 17 |
| `persona` | 准备中的人格化状态 | 飞书 tool_activity 区域（placeholder 阶段） | 8 |
| `closings` | 完成时的签名语 | 飞书 tool_activity 区域（turn 完成时） | 8 |
| `status` | 助手状态文本 | Slack assistant status | 4 |
| `welcome` | 首次进入聊天欢迎语 | 飞书 welcome card（支持 `{bot_name}` 占位符） | 2 |
| `welcome_back` | 回访用户欢迎语 | 飞书 welcome card（回访场景） | 2 |

### 各分类默认值

<details>
<summary>greetings（占位卡片欢迎语）</summary>

```
- 来啦～
- 交给我～
- 收到，马上～
- 好嘞！
- 马上来～
- 明白，开始干活！
- 来了来了～
- 收到！
```
</details>

<details>
<summary>tips（占位卡片 CLI 提示）</summary>

```
- 输入 /gc 或 $休眠 可休眠当前会话，下次发消息自动恢复
- 输入 /reset 或 $重置 可重置上下文，从零开始新对话
- 输入 /cd ../other-project 或 $切换目录 ../other-project 切换工作目录
- 输入 ? 或 /help 查看所有可用命令
- 用 $ 前缀可用自然语言触发命令，如 $compact、$上下文、$切换模型
- 输入 /compact 或 $压缩 可压缩历史，释放上下文窗口
- 输入 /commit 或 $提交 可让 AI 快速创建 Git 提交
- 输入 /model sonnet 可切换 AI 模型
- 输入 /context 或 $上下文 可查看上下文窗口使用量
- 输入 /skills 或 $技能 可查看当前已加载的技能列表
- 输入 /mcp 可查看 MCP 服务器连接状态
- 输入 /perm bypassPermissions 可调整权限
- 运行 hotplex onboard 启动交互式配置向导
- 运行 hotplex doctor --fix 可自动检测并修复环境问题
- 运行 hotplex update -y --restart 一键更新并重启 Gateway
- 运行 hotplex dev 可同时启动 Gateway 和 WebChat 开发环境
- 支持 Slack、飞书、WebChat 多平台同时在线
```
</details>

<details>
<summary>persona（准备中状态，显示在 tool_activity 区域）</summary>

```
- 🧠 正在回忆上次对话...
- 📋 加载技能库...
- 🔍 检查工作目录...
- 🎯 分析需求中...
- 🛠️ 准备开发工具...
- 📂 浏览项目结构...
- 💡 思考最佳方案...
- 🚀 引擎预热中...
```
</details>

<details>
<summary>closings（完成签名语，turn 结束时显示）</summary>

```
- 搞定了！有事随时找我～
- ✅ 完成！还需要什么？
- 搞定～
- 🎉 大功告成！
- ☕ 任务完成，随时待命
- ✨ 处理好了，有事吱声
- 😌 收工～
- 🎯 完美收尾！
```
</details>

<details>
<summary>status（Slack 助手状态文本）</summary>

```
- Initializing...
- Thinking...
- Composing response...
- Processing...
```
</details>

<details>
<summary>welcome（首次进入聊天，支持 `{bot_name}` 占位符）</summary>

```
- Hi，我是 {bot_name}，你的 AI 编程助手！
- 欢迎！直接发消息给我，我们可以开始写代码了。
```
</details>

<details>
<summary>welcome_back（回访用户欢迎语）</summary>

```
- 好久不见！有什么我可以帮你的？
- 欢迎回来～随时继续。
```
</details>

## 占位符

`welcome` 分类支持 `{bot_name}` 占位符，在发送时自动替换为 bot 实际名称。

## 生效方式

配置在 **adapter 初始化时加载**，与 agent-config 一致。修改后需要 **重启 gateway** 才能生效。
