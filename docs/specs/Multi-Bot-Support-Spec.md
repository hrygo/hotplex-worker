---
type: spec
tags:
  - project/HotPlex
date: 2026-05-13
status: implemented
progress: 100
---

# Multi-Bot Support Design Spec

**状态**：已实现 (Implemented)
**版本**：1.0
**所有者**：黄飞虹
**PR**：#410

---

## 1. 问题

HotPlex 每个 messaging 平台仅支持一个 bot 实例。Slack 和飞书均原生支持 workspace 内多 bot，需要：

1. **多角色**：不同 bot 承担不同职能（技术支持、HR、客服）
2. **负载分担**：多实例分摊消息量
3. **租户隔离**：不同团队/客户使用独立 bot 和配置

底层（session 隔离、agent config、JWT）已通过 BotID 支持多 bot。上层（config、adapter 初始化、bridge 创建）限制为单 bot。

## 2. 方案：Adapter-per-Bot

每个 bot 拥有独立的 Adapter + Bridge + ConnPool 实例，各自维持独立的平台 WebSocket 连接。

### 2.1 设计决策

| 决策 | 理由 |
|------|------|
| 独立连接而非共享 | Slack/飞书要求每 bot 独立凭证，连接共享不可行 |
| 不修改 Adapter 接口 | 最小化变更范围，现有 Adapter 实现无需改动 |
| 自然故障隔离 | 单 bot 连接故障不影响其他 bot |
| 上限 10 bot/平台 | Slack Socket Mode 限制 10 WS 连接/app |

### 2.2 Non-Goals

- 运行时动态增删 bot（未来：hot-reload 或 admin API）
- 跨 bot 消息转发或交接
- Bot 负载均衡（多 adapter 共享同一 bot 身份）
- Per-bot 速率限制或配额管理

## 3. 配置设计

### 3.1 数据结构

```go
type SlackBotConfig struct {
    Name       string     `mapstructure:"name"`
    BotToken   string     `mapstructure:"bot_token"`
    AppToken   string     `mapstructure:"app_token"`
    Soul       string     `mapstructure:"soul,omitempty"`
    WorkerType string     `mapstructure:"worker_type,omitempty"`
    STT        *STTConfig `mapstructure:"stt,omitempty"`
    TTS        *TTSConfig `mapstructure:"tts,omitempty"`
}
```

飞书结构类似，使用 `AppID`/`AppSecret` 替代 `BotToken`/`AppToken`。

### 3.2 YAML 格式

```yaml
messaging:
  slack:
    enabled: true
    # 旧格式（向后兼容）
    bot_token: xoxb-legacy
    app_token: xapp-legacy

    # 新 multi-bot 格式
    bots:
      - name: tech-support
        bot_token: xoxb-aaa
        app_token: xapp-aaa
        soul: tech-support
        worker_type: claude_code
        stt:
          enabled: false
        tts:
          enabled: true
      - name: hr-bot
        bot_token: xoxb-bbb
        app_token: xapp-bbb
        soul: hr-assistant
```

### 3.3 向后兼容

`normalizeSlackBots()` / `normalizeFeishuBots()` 规则：

| 条件 | 行为 |
|------|------|
| `bots[]` 非空 | 使用 `bots[]`，忽略顶层单 bot 凭证 |
| `bots[]` 为空 + 顶层凭证存在 | 自动包装为 `bots: [{name: "default", ...}]` |
| 两者均空 | 该平台不创建 bot |

环境变量 `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN`（单数）映射到 default bot。Multi-bot 必须使用配置文件。

## 4. 初始化流程

### 4.1 messaging_init.go 变更

```
旧流程:
  for platform in RegisteredTypes():
    adapter = builder()
    bridge = new Bridge(adapter)
    adapter.Start()

新流程:
  for platform in RegisteredTypes():
    bots = resolveBots(platform)       // 解析配置 + 向后兼容
    for bot in bots:
      adapter = builder()
      adapter.ConfigureWith(botConfig) // 注入 bot 级配置
      bridge = new Bridge(adapter)
      adapter.Start()
      botRegistry.Register(bot.Name, platform, adapter, bridge)
```

### 4.2 Bridge

每个 bot 拥有独立 `messaging.Bridge` 实例，所有 bridge 共享同一 `SessionStarter`（gateway bridge）。无需修改 Bridge 接口 — `SetAdapter()` 已绑定单 adapter。

## 5. 消息路由

**无需额外路由逻辑。** 平台 API 层面自动过滤：

- **Slack**：每个 Socket Mode 连接（per bot_token）仅接收该 bot 的事件（@mention 和 DM）
- **飞书**：每个 bot 的 WebSocket 连接仅接收发往该 bot 的事件

## 6. Per-Bot Agent Config

现有 3 级 fallback 已支持 per-bot 配置：

```
~/.hotplex/agent-configs/
  SOUL.md              # 全局
  slack/               # 平台级
    SOUL.md
  slack/U12345/        # bot 级（最高优先级）
    SOUL.md
    AGENTS.md
```

`BotConfig.Soul` 字段当前用作 bot 显示名称（日志和状态 API），未来可扩展为 soul 模板选择器。

## 7. Bot 状态 API

### 7.1 端点

```
GET /admin/bots          → 列出所有活跃 bot
GET /admin/bots/{name}   → 单个 bot 详情
```

### 7.2 响应格式

```json
{
  "name": "tech-support",
  "platform": "slack",
  "bot_id": "U12345",
  "status": "running",
  "connected_at": "2026-05-13T20:00:00Z",
  "soul": "tech-support",
  "worker_type": "claude_code"
}
```

`GET /admin/bots` 返回上述对象的数组。`connected_at` 为空时省略。`soul`/`worker_type` 为空时省略。

### 7.3 BotRegistry

`internal/messaging/bot_registry.go`：

- 并发安全：`sync.RWMutex` + `map[string]*BotEntry`（key: `platform/botName`）
- 生命周期：Register（adapter.Start 后）→ 运行时查询 → Unregister（shutdown）
- Admin API handler 通过 `BotLister` 接口解耦

## 8. 校验与错误处理

### 8.1 启动校验

| 场景 | 处理 |
|------|------|
| 同平台 bot `name` 重复 | Error 日志 + 跳过重复 bot |
| 缺少凭证（空 bot_token/app_token） | 跳过该 bot + warning 日志 |
| 所有 bot 启动失败 | Gateway 退出并报错 |
| bot 数量超过平台限制（默认 10） | Warning 日志，超出的 bot 被忽略 |

### 8.2 运行时

- 单 bot 连接故障不影响其他 bot
- Bot 状态跟踪：`starting` → `running` → `stopped` / `error`
- Admin API 实时反映各 bot 状态

### 8.3 优雅关闭

按创建逆序遍历所有已注册 bot：adapter.Stop() → bridge.Shutdown() → botRegistry.Unregister()

现有关闭顺序（signal → cancel ctx → hub → bridge → sessionMgr → HTTP）不受影响。

## 9. 变更范围

| 文件 | 变更 |
|------|------|
| `internal/config/config.go` | `SlackBotConfig`/`FeishuBotConfig` 类型、normalize、propagation、path expansion |
| `internal/config/config_test.go` | normalize 单元测试（向后兼容、优先级、空配置） |
| `configs/config.yaml` | 注释示例 `bots[]` 配置 |
| `cmd/hotplex/messaging_init.go` | 嵌套循环 platforms × bots + fillSlackExtras/fillFeishuExtras |
| `cmd/hotplex/banner.go` | 启动 banner 展示多 bot 信息 |
| `internal/messaging/bot_registry.go` | **新增** — 并发安全 bot 注册表 |
| `internal/messaging/bot_registry_test.go` | Register/Get/Unregister/List/并发测试 |
| `internal/messaging/config.go` | `AdapterConfig.BotName` 字段 |
| `internal/admin/bot_handlers.go` | **新增** — BotLister provider + HTTP handlers |
| `internal/admin/admin.go` | AdminAPI.BotLister 字段 + Deps 注入 |
| `cmd/hotplex/admin_adapters.go` | botListerAdapter 桥接 BotRegistry → admin.BotListerProvider |
| `cmd/hotplex/routes.go` | 注册 `/admin/bots` 和 `/admin/bots/{name}` 端点 |
| `configs/config.yaml` | 注释示例 `bots[]` 配置 |

**无需变更**：
- `internal/session/` — BotID 隔离已就绪
- `internal/agentconfig/` — 3 级 fallback 已就绪
- `internal/security/` — JWT bot_id claim 已就绪
- `internal/gateway/bridge.go` — `StartPlatformSession(botID)` 已参数化
- Adapter 内部（`slack/`、`feishu/`）— 接口不变
