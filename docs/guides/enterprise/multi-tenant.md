---
title: Multi-Tenant Isolation Guide
description: Per-bot isolation, access control, and resource quotas for multi-tenant HotPlex deployments.
persona: enterprise
difficulty: advanced
---

# Multi-Tenant Isolation Guide

> 面向企业多团队/多 Bot 场景的 HotPlex 租户隔离方案。涵盖 Agent 配置隔离、JWT 路由、Session 配额和访问控制策略。

---

## 1. 隔离模型概览

HotPlex 采用 **Bot-centric 隔离模型**：每个 Bot（Slack Bot / Feishu App）作为独立租户单元，在配置、路由、资源和访问控制四个层面实现隔离。

| 隔离层 | 机制 | 范围 |
|--------|------|------|
| 配置隔离 | 3-level Agent Config fallback | Bot 级别 |
| 路由隔离 | JWT `bot_id` claim | 请求级别 |
| 工作目录隔离 | Per-bot `work_dir` | 进程级别 |
| 资源隔离 | Per-user Session/Memory 配额 | 用户级别 |
| 访问控制 | DM/Group policy + allowlist | 平台级别 |

---

## 2. Per-Bot Agent 配置隔离

Agent 配置采用 **3-level fallback** 策略，每文件独立解析，命中即终止：

```
agent-configs/
├── SOUL.md              # Level 3: 全局默认人格
├── AGENTS.md            # Level 3: 全局行为规则
├── SKILLS.md            # Level 3: 全局技能列表
├── slack/               # Level 2: Slack 平台覆盖
│   ├── SOUL.md
│   ├── AGENTS.md
│   └── U12345/          # Level 1: Bot 级别覆盖
│       ├── SOUL.md      # 专属于 Slack Bot U12345 的人格
│       ├── USER.md      # 用户上下文（C 通道）
│       └── MEMORY.md    # 用户记忆（C 通道）
└── feishu/              # Level 2: Feishu 平台覆盖
    ├── SOUL.md
    └── cli_xxx/         # Level 1: Feishu App 级别覆盖
        └── SOUL.md
```

**解析顺序**：`bot/<id>/` → `platform/` → `global` — 找到即停止，不会合并上层。

**B/C 双通道**：
- **B 通道** (`<directives>`): META-COGNITION.md + SOUL + AGENTS + SKILLS — 定义 Bot 行为
- **C 通道** (`<context>`): USER + MEMORY — 提供用户上下文

配置热更新仅在 session 初始化或 `/reset` 时加载，运行中的 session 不受影响。

---

## 3. JWT bot_id 路由隔离

JWT Token 包含 `bot_id` claim，网关在请求处理时强制校验：

```
JWT Claims:
  iss: "hotplex"
  sub: "user_id"
  bot_id: "U12345"      ← 路由隔离关键字段
  user_id: "U12345"
  session_id: "..."
  role: "user"
  scopes: ["session:read", "session:write"]
```

**隔离规则**：
- `bot_id` 必须与 Session 所属 Bot **精确匹配**
- 跨 Bot 操作被硬拒绝，返回 `403 Forbidden`
- 每个 Bot 独立的 ES256 密钥对，共享 JWT Secret 但 bot_id 互斥

---

## 4. Per-Bot 工作目录隔离

通过 `work_dir` 配置实现 Bot 级别的文件系统隔离：

```yaml
messaging:
  slack:
    work_dir: /data/workspaces/slack-bot-a
  feishu:
    work_dir: /data/workspaces/feishu-app-1
```

**安全机制**：
- `ExpandAndAbs` 解析路径后，`security.ValidateWorkDir` 执行安全边界检查
- 防止路径穿越（`../`）和访问系统目录
- 支持 `work_dir_allowed_base_patterns` 白名单扩展
- `work_dir_forbidden_dirs` 黑名单阻止敏感目录

---

## 5. Per-User 资源配额

Session Pool 通过 `PoolManager` 实现三层配额控制：

```yaml
pool:
  max_size: 100              # 全局最大活跃 Session
  max_idle_per_user: 5       # 每用户最大空闲 Session
  max_memory_per_user: 3221225472  # 每用户最大内存 3GB
```

**配额执行流程**：

```
Acquire(userID)
  ├─ 全局配额检查: totalCount < max_size
  ├─ 用户配额检查: userCount[userID] < max_idle_per_user
  └─ 内存配额检查: userMemory[userID] + 512MB < max_memory_per_user
```

每个 Worker 按 512MB 估算（匹配 Linux RLIMIT_AS 上限），超出时返回 `MEMORY_EXCEEDED` 错误。

**动态调整**：`UpdateLimits()` 支持运行时修改配额，已有 session 不会被驱逐。

---

## 6. 访问控制策略

### DM/Group 策略

每个平台适配器独立配置 DM（私聊）和 Group（群聊）访问策略：

```yaml
messaging:
  slack:
    dm_policy: "allowlist"       # allowlist / open / closed
    group_policy: "allowlist"
    require_mention: true        # 群聊中必须 @bot
    allow_from: ["U111", "U222"]
    allow_dm_from: ["U111"]
    allow_group_from: ["C333", "C444"]
```

| 策略 | 说明 |
|------|------|
| `allowlist` | 仅 `allow_from` 列表中的用户/群组可触发 |
| `open` | 所有用户均可触发 |
| `closed` | 完全禁止触发 |

### Admin API 访问控制

```yaml
admin:
  enabled: true
  ip_whitelist_enabled: true
  allowed_cidrs: ["10.0.0.0/8", "172.16.0.0/12"]
  rate_limit_enabled: true
  requests_per_sec: 10
  burst: 20
```

Admin Token 支持精细化 scope 控制：`session:read`、`session:write`、`stats:read`、`health:read`。

---

## 7. 多租户最佳实践

1. **一 Bot 一租户**：每个团队/项目使用独立的 Bot Token
2. **最小权限 allowlist**：仅授权必要用户，避免 `open` 策略
3. **独立 work_dir**：不同 Bot 使用不同工作目录，防止文件冲突
4. **配额分层**：根据团队规模设置 `max_idle_per_user` 和 `max_memory_per_user`
5. **Agent 配置覆盖**：在 bot-level 目录放置定制的 SOUL.md，实现人格隔离
6. **监控隔离**：通过 Prometheus `bot_id` label 分维度监控各租户资源使用
