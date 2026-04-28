# 飞书 (Lark) 接入 HotPlex 完整教程

> 本教程覆盖从创建飞书应用到完成 HotPlex 接入的全部步骤。

---

## 核心流程

1. 在飞书开放平台创建企业自建应用。
2. 启用 **“机器人”** 能力，配置为 **“长连接 (WebSocket)”** 模式。
3. 批量导入应用 **权限 (Scopes)**。
4. 订阅 **事件 (Events)** 以接收消息。
5. 将凭证配置到 HotPlex 并启动。

---

## 第 1 步：创建飞书应用

1. 登录 [飞书开放平台 - 开发者后台](https://open.feishu.cn/app)。
2. 点击 **“创建企业自建应用”**。
3. 输入应用名称（如 `HotPlex`）和描述，点击 **“确定创建”**。
4. 在左侧菜单点击 **“凭证与基础信息”**，记录下 **App ID** 和 **App Secret**。

---

## 第 2 步：启用机器人与事件订阅

1. 在左侧菜单点击 **“添加应用能力”** -> **“机器人”** -> **“添加”**。
2. 进入左侧菜单 **“事件订阅”** 页面。
3. 配置 **“事件发送方式”**：
   - 选择 **“长连接 (WebSocket)”**。
   - 点击 **“保存”**。
   - *注：WebSocket 模式无需配置请求网址，适合在防火墙后或本地开发环境运行。*

---

## 第 3 步：配置权限 (Scopes)

飞书支持通过 JSON 批量导入权限，这是最快的方式：

1. 在左侧菜单点击 **“权限管理”**。
2. 点击页面右上角的 **“批量导入/导出权限”** 按钮。
3. 选择 **“导入权限”**，在弹出的窗口中粘贴下方 [权限配置 JSON](#权限配置-json-scopes) 中的内容。
4. 点击 **“确定”**。系统会自动勾选所有必要的权限项。
5. **重要**：如果您的权限涉及敏感数据，需点击页面上方的 **“申请开通”**（企业内部应用通常会自动通过）。

---

## 第 4 步：订阅事件 (Events)

为了让机器人能接收到消息，需要订阅特定事件：

1. 返回 **“事件订阅”** 页面。
2. 点击 **“添加事件”**。
3. 搜索并添加以下事件（API V2.0）：
   - `接收消息` (`im.message.receive_v1`) —— **必须**
   - `消息已读` (`im.message.read_v1`)
   - `新增表情回复` (`im.message.reaction.created_v1`)
   - `删除表情回复` (`im.message.reaction.deleted_v1`)
4. 点击 **“确定”** 保存。

---

## 第 5 步：配置 HotPlex

将凭证写入 `.env` 文件（从 `configs/env.example` 复制）：

```env
# 启用飞书适配器
HOTPLEX_MESSAGING_FEISHU_ENABLED=true

# 应用凭证
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_your_app_id
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=your_app_secret
```

更多可选项：

```env
# Worker 类型：claude_code（默认）或 opencode_server
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code

# 访问控制策略：allowlist（默认）或 allow
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=allowlist
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=allowlist
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true

# 用户白名单（飞书 User ID / OpenID，逗号分隔）
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=
```

---

## 第 6 步：发布版本并启动

1. 在飞书左侧菜单点击 **“版本管理与发布”**。
2. 点击 **“创建版本”**，填写版本号和更新日志，点击 **“保存”**。
3. 点击 **“申请发布”**，等待企业管理员审核（或自审通过）。
4. 在本地启动 HotPlex：
   ```bash
   make run
   ```

---

## 可用命令

飞书机器人支持通过以下命令（及 `$` 前缀的自然语言）进行交互：

| 命令 | 说明 |
| --- | --- |
| `/gc` 或 `/park` | 休眠会话（停止 Worker，保留状态） |
| `/reset` 或 `/new` | 重置上下文（全新开始） |
| `/cd <路径>` | 切换工作目录（跨项目操作） |
| `/context` | 查看当前上下文使用量 |
| `/skills` | 查看已加载的技能列表 |

---

## 交互与特性

- **流式卡片 (Streaming Cards)**：HotPlex 使用 Feishu CardKit V1 实现打字机式实时回复。
- **语音转文字 (STT)**：支持直接发送语音消息，系统会自动转录为文本交给 AI 处理。
- **审批确认**：工具运行前会发送交互式卡片，点击按钮即可授权。
- **表情反馈**：系统会通过消息下方的 Emoji（⏳/🔄/✅）实时反馈 AI 的思考与执行状态。

---

## 权限配置 JSON (Scopes)

将以下 JSON 粘贴到飞书 **“权限管理 -> 批量导入”** 中：

```json
{
  "scopes": {
    "tenant": [
      "im:message",
      "im:message.group_msg",
      "im:message.group_msg:readonly",
      "im:message.p2p_msg",
      "im:message.p2p_msg:readonly",
      "im:message:send_as_bot",
      "im:chat",
      "im:chat:readonly",
      "im:message.reaction:readonly",
      "im:resource:download",
      "bot:info",
      "contact:user.employee_id:readonly"
    ]
  }
}
```

> [!TIP]
> 权限说明：
> - `im:message` 系列：用于接收和发送各种消息。
> - `im:resource:download`：用于处理用户发送的图片/文件。
> - `bot:info`：获取机器人自身 ID。
> - `im:message.reaction`：用于通过 Emoji 反馈状态。

技术实现细节参见 [Feishu Adapter 改进规格书](../../specs/Feishu-Adapter-Improvement-Spec.md)。
