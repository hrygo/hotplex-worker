# PermissionRequest Hooks 配置指南

> 适用于 HotPlex Gateway v1.5+
> 配合 `permission_prompt: true` 使用

## 概述

当 `permission_prompt: true` 启用时，Claude Code 的所有 `ask` 请求（包括 bypass-immune 操作）都会通过交互链路转发给用户。PermissionRequest Hooks 可以在请求到达用户之前自动处理常见操作，减少不必要的打扰。

## 工作原理

```
Claude Code ask → control_request → PermissionRequest Hook 匹配?
                                              ├─ 是: 自动 allow/deny，不转发给用户
                                              └─ 否: 转发到 Slack/飞书交互 UI
```

## 配置位置

**项目级**（推荐，Git 管理）：`.claude/settings.json`

**用户级**：`~/.claude/settings.json`

## 配置示例

### 基础：自动放行低风险操作

```json
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "Read",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(git *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(ls *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      }
    ]
  }
}
```

### 中级：按工具粒度控制

```json
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "Bash(npm *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(go test *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(make *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(rm *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"deny\"}'" }]
      }
    ]
  }
}
```

### 高级：结合 permission_prompt 按环境配置

```yaml
# config.yaml - 生产环境：关闭交互 UI，依赖 hooks
worker:
  claude_code:
    permission_prompt: false

# config.yaml - 开发环境：开启交互 UI + hooks 减少打扰
worker:
  claude_code:
    permission_prompt: true
```

```json
// .claude/settings.json - 开发环境 hooks
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "Read",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Write",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(git *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(go *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      }
    ]
  }
}
```

## Matcher 语法

| 模式 | 匹配 | 示例 |
|------|------|------|
| `ToolName` | 精确匹配工具名 | `Read`, `Write` |
| `ToolName(prefix*)` | 工具名 + 参数前缀匹配 | `Bash(git *)`, `Bash(npm *)` |

## 常用模板

### Go 项目

```json
{
  "hooks": {
    "PermissionRequest": [
      { "matcher": "Read", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Write", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Edit", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(go *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(git *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(make *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] }
    ]
  }
}
```

### 前端项目

```json
{
  "hooks": {
    "PermissionRequest": [
      { "matcher": "Read", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Write", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Edit", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(npm *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(npx *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] },
      { "matcher": "Bash(git *)", "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }] }
    ]
  }
}
```

## 安全注意事项

- **不要自动放行 `Bash(rm *)`、`Bash(curl *)` 等高风险操作**
- 项目级 `.claude/settings.json` 会随 Git 传播，确保团队成员审阅
- 用户级 `~/.claude/settings.json` 仅影响本机
- Hooks 在 Claude Code 进程内执行，不影响 HotPlex Gateway

## 前置条件

- Claude Code 版本需支持 `PermissionRequest` hooks（参见 Claude Code 官方文档）
- HotPlex `permission_prompt: true` 已启用
