---
paths:
  - "**/agentconfig/**/*.go"
---

# Agent Config

`Load(dir, platform, botID)` 按三级 fallback 加载配置：`{botID}/` → `{platform}/` → 全局，每文件独立，命中即终止。

**B/C 双通道**：冲突时 directives 无条件覆盖 context。

```xml
<agent-configuration>
  <directives>
    <hotplex>   META-COGNITION.md (go:embed, 首位, 始终存在)
    <persona>   SOUL.md
    <rules>     AGENTS.md
    <skills>    SKILLS.md
  </directives>
  <context>
    <user>      USER.md
    <memory>    MEMORY.md
  </context>
</agent-configuration>
```

- 注入：CC → `--append-system-prompt` | OCS → `system` 字段
- BotID：`Adapter.botID → Bridge → injectAgentConfig → Load`
- 限制：单文件 8KB / 总量 40KB | YAML frontmatter 自动剥离
- 安全：`filepath.Base(botID) == botID` 防路径穿越
