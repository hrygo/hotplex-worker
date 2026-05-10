---
title: 飞书 (Feishu) 集成教程
description: 一步步将 HotPlex Gateway 接入飞书，实现 AI 对话、语音消息和权限交互
persona: developer
difficulty: beginner
---

# 飞书集成教程

本教程指导你完成 HotPlex 与飞书的集成，获得流式卡片回复、语音转写和权限交互能力。

## 前置条件

- HotPlex 已安装（`hotplex version` 可执行）
- 飞书企业版账号（管理员或有应用创建权限）
- 已配置 Worker（Claude Code 或 OpenCode Server 可用）

---

## 1. 创建飞书应用

登录 [飞书开放平台](https://open.feishu.cn)，进入「开发者后台」。

### 1.1 创建应用

1. 点击「创建企业自建应用」
2. 填写应用名称（如 `HotPlex Bot`）和描述
3. 记录 **App ID**（`cli_` 前缀）和 **App Secret**

### 1.2 启用机器人能力

进入应用 →「添加应用能力」→ 勾选「机器人」。

### 1.3 配置权限

进入「权限管理」，搜索并开通以下权限：

| 权限 | 权限标识 | 用途 |
|------|---------|------|
| 获取与发送单聊、群聊消息 | `im:message` | 收发消息 |
| 以应用身份发消息 | `im:message:send_as_bot` | 机器人发送 |
| 获取与上传图片或文件资源 | `im:resource` | 语音/文件 |
| 获取群组信息 | `im:chat` | 群聊策略 |

> 如需飞书云端 STT，额外开通「语音转文字」权限（`speech:stt`）。

点击「批量开通」→ 发布新版本 → 申请线上发布。

### 1.4 订阅事件

进入「事件订阅」：

1. 选择 **WebSocket 模式**（HotPlex 使用 WS 长连接，无需公网回调地址）
2. 添加事件：`im.message.receive_v1`（接收消息）
3. （推荐）设置 **Encrypt Key** 和 **Verification Token**

**验证**：事件订阅页面显示「已启用」且 `im.message.receive_v1` 状态为正常。

---

## 2. 配置 HotPlex

### 方式 A：手动编辑 .env

```bash
cp configs/env.example .env
```

编辑 `.env`，取消注释并填入飞书配置：

```bash
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_xxxxxxxxxxxx
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=your_app_secret_here
```

### 方式 B：使用 Onboard 向导

```bash
hotplex onboard
```

向导会依次引导你选择平台（Feishu）、输入 App ID/Secret，自动写入 `.env`。

**验证**：

```bash
hotplex doctor
# 输出应包含：messaging.feishu_creds ✓  Feishu credentials present
```

---

## 3. 启动 Gateway

```bash
hotplex gateway start -d
```

- `-d` 表示后台运行（daemon 模式）

**验证**：

```bash
hotplex status
# 输出应显示：feishu ✓  connected
```

查看实时日志确认飞书连接成功：

```bash
hotplex service logs -f
# 期望看到：feishu ws connected  app_id=cli_xxx
```

---

## 4. 功能测试

### 4.1 基础对话

1. 在飞书中搜索你的机器人名称
2. 发送「你好」
3. **期望**：收到流式更新的卡片消息，内容逐步填充

### 4.2 权限交互

发送需要执行命令的请求（如「列出当前目录文件」）：

1. Bot 应发送权限确认卡片
2. 回复「允许」或「拒绝」
3. **期望**：Bot 根据回复继续执行或取消

### 4.3 语音消息

1. 在飞书中按住语音按钮发送一段语音
2. **期望**：Bot 通过 STT 将语音转写为文字，然后正常回复

> 语音转写默认使用本地 STT 引擎。如未安装，参考 `docs/channels/STT-SETUP.md`。

---

## 5. 高级配置

<details>
<summary>DM / 群聊策略</summary>

```bash
# require_mention: 群聊中是否需要 @机器人 才响应（默认 true）
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true

# DM 策略 — allowlist / open / disabled
# open = 接受所有人私聊，allowlist = 仅允许指定用户，disabled = 关闭私聊
HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM=open

# 群聊策略 — 同上
HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM=open

# 指定允许的用户 ID（逗号分隔，allowlist 模式生效）
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=ou_xxx,ou_yyy
```

</details>

<details>
<summary>TTS / STT 配置</summary>

```bash
# STT: feishu（云端）, local（本地 SenseVoice-Small ONNX）, feishu+local（云端优先+本地降级）
HOTPLEX_MESSAGING_STT_PROVIDER=local

# TTS: edge（免费 Edge TTS）, moss（本地 MOSS-TTS-Nano）, edge+moss（Edge 优先+MOSS 降级）
HOTPLEX_MESSAGING_TTS_ENABLED=true
HOTPLEX_MESSAGING_TTS_PROVIDER=edge+moss
HOTPLEX_MESSAGING_TTS_VOICE=zh-CN-XiaoxiaoNeural
HOTPLEX_MESSAGING_TTS_MAX_CHARS=150
```

</details>

<details>
<summary>交互与指示器</summary>

**权限交互**：Bot 发送确认卡片时，用户直接回复文本「允许」或「拒绝」即可，无需点击按钮。

**Typing 指示器**：Bot 收到消息后自动添加 👀 emoji reaction，回复完成后自动移除。

这些行为为内置默认，无需额外配置。

</details>

## 故障排查

| 症状 | 检查项 |
|------|--------|
| `feishu ✗` | 确认 `APP_ID`/`APP_SECRET` 正确，应用已发布 |
| 消息无回复 | `hotplex service logs -f` 查看 Worker 错误 |
| 语音不转写 | 检查 STT provider 配置和本地引擎是否安装 |
| 群聊不响应 | 确认 `REQUIRE_MENTION=true` 时已 @机器人 |

更多细节参考 [配置文档](../../configs/README.md) 和 [STT 安装手册](../channels/STT-SETUP.md)。