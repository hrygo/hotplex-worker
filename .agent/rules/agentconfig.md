---
paths:
  - "**/agentconfig/**/*.go"
---

# Agent Config 规范

> 加载 personality/context 配置，构建统一 system prompt
> 参考：`internal/agentconfig/loader.go`、`internal/agentconfig/prompt.go`

## B/C 双通道架构

```
~/.hotplex/agent-configs/           配置文件根目录
├── SOUL.md                         B 通道：身份定义（高优先级、无hedging）
├── SOUL.slack.md                   B 通道平台变体（自动追加到 SOUL.md）
├── AGENTS.md                       B 通道：Agent 行为规范
├── SKILLS.md                       B 通道：可用技能列表
├── USER.md                         C 通道：用户个人信息（背景参考）
└── MEMORY.md                       C 通道：记忆/上下文（背景参考）
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

## META-COGNITION.md（Agent 自画像）

`internal/agentconfig/META-COGNITION.md` — 通过 `go:embed` 编译时注入：

```go
//go:embed META-COGNITION.md
var metaCognitionFS embed.FS

func LoadMetaCognition() (string, error) {
    data, err := metaCognitionFS.ReadFile("META-COGNITION.md")
    return string(data), err
}
```

**5 节结构**：identity、system architecture、session lifecycle、agent config architecture、control commands

**注入方式**：作为 C 通道 `<hotplex>` 子节点注入，与 USER.md/MEMORY.md 同级：
```xml
<context>
  <hotplex>  ← META-COGNITION.md 内容
  <user>     ← USER.md 内容
  <memory>   ← MEMORY.md 内容
</context>
```

## 统一 System Prompt 构建

```go
// prompt.go — BuildSystemPrompt
// B 通道 + C 通道合并为单一 prompt，统一注入 CC 和 OCS
<agent-configuration>
  <directives>
    <soul>        ← SOUL.md（+ SOUL.<platform>.md）
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

## 平台变体

配置加载时自动追加平台特定文件（无平台文件时跳过）：

```go
// loader.go — Load
platformVariants := []string{"SOUL." + platform + ".md"}
for _, name := range platformVariants {
    path := filepath.Join(dir, name)
    if data, err := os.ReadFile(path); err == nil {
        content += "\n" + stripFrontmatter(string(data))
    }
}
```

**示例**：Slack 平台 → `SOUL.slack.md` 追加到 `SOUL.md` 后；其他平台同理。

## 大小限制

| 限制 | 值 | 原因 |
|------|-----|------|
| 单文件上限 | 8KB | 防止 prompt 爆炸 |
| 总量上限 | 40KB | Gateway 内存 + Token 成本 |
| 数量限制 | 6 个文件 | SOUL + AGENTS + SKILLS + USER + MEMORY + 变体 |

超限 → `ErrConfigFileTooLarge` / `ErrTotalConfigTooLarge`。

## YAML Frontmatter 剥离

配置文件头部允许 YAML frontmatter（`---...---`），加载时自动剥离：

```go
func stripFrontmatter(content string) string {
    if strings.HasPrefix(content, "---") {
        end := strings.Index(content[2:], "---")
        if end >= 0 {
            return strings.TrimSpace(content[end+4:])
        }
    }
    return content
}
```

**用途**：元数据（版本、作者、标签）放在 frontmatter，正文注入 prompt。
