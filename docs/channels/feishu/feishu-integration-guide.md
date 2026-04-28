# Feishu (Lark) Integration Guide for HotPlex

> This guide covers all steps from creating a Feishu app to completing the HotPlex integration.

---

## Core Process

1. Create an Internal App on the Feishu Open Platform.
2. Enable **"Bot"** capability and configure it to **"WebSocket"** mode.
3. Batch import app **Permissions (Scopes)**.
4. Subscribe to **Events** to receive messages.
5. Configure credentials in HotPlex and start.

---

## Step 1: Create Feishu App

1. Log in to [Feishu Open Platform - Developer Console](https://open.feishu.cn/app).
2. Click **"Create Internal App"**.
3. Enter the app name (e.g., `HotPlex`) and description, then click **"Confirm"**.
4. Click **"Credentials & Basic Info"** in the left menu and record the **App ID** and **App Secret**.

---

## Step 2: Enable Bot & Event Subscription

1. Click **"Add Capabilities"** -> **"Bot"** -> **"Add"**.
2. Go to the **"Event Subscriptions"** page in the left menu.
3. Configure **"Event Delivery Method"**:
   - Select **"WebSocket"**.
   - Click **"Save"**.
   - *Note: WebSocket mode doesn't require a public URL, making it ideal for running behind firewalls or in local development.*

---

## Step 3: Configure Permissions (Scopes)

Feishu supports batch importing permissions via JSON, which is the fastest method:

1. Click **"Permission Management"** in the left menu.
2. Click the **"Batch Import/Export Permissions"** button at the top right.
3. Select **"Import Permissions"** and paste the content from the [Permission JSON (Scopes)](#permission-json-scopes) section below.
4. Click **"Confirm"**. The system will automatically select all necessary permissions.
5. **Important**: If your permissions involve sensitive data, click **"Apply for Opening"** at the top (internal apps are usually approved automatically).

---

## Step 4: Subscribe to Events

To receive messages, the bot needs to subscribe to specific events:

1. Return to the **"Event Subscriptions"** page.
2. Click **"Add Events"**.
3. Search for and add the following events (API V2.0):
   - `Receive Messages` (`im.message.receive_v1`) —— **Required**
   - `Message Read` (`im.message.read_v1`)
   - `Message Reaction Created` (`im.message.reaction.created_v1`)
   - `Message Reaction Deleted` (`im.message.reaction.deleted_v1`)
4. Click **"Confirm"** to save.

---

## Step 5: Configure HotPlex

Write the credentials into the `.env` file (copied from `configs/env.example`):

```env
# Enable Feishu adapter
HOTPLEX_MESSAGING_FEISHU_ENABLED=true

# App Credentials
HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_your_app_id
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=your_app_secret
```

Additional options:

```env
# Worker type: claude_code (default) or opencode_server
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code

# Access policy: allowlist (default) or allow
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=allowlist
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=allowlist
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true

# User Allowlist (Feishu User ID / OpenID, comma-separated)
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=
```

---

## Step 6: Create Version and Publish

1. Click **"Version Management & Release"** in the left menu.
2. Click **"Create a Version"**, fill in the version number and update logs, then click **"Save"**.
3. Click **"Apply for Release"** and wait for administrator approval (or self-approve).
4. Start HotPlex locally:
   ```bash
   make run
   ```

---

## Available Commands

The Feishu bot supports the following commands (and natural language triggers with the `$` prefix):

| Command | Description |
| --- | --- |
| `/gc` or `/park` | Hibernate session (stop worker, keep state) |
| `/reset` or `/new` | Reset context (fresh start) |
| `/cd <path>` | Switch working directory |
| `/context` | View current context window usage |
| `/skills` | View list of loaded skills |

---

## Interactions & Features

- **Streaming Cards**: HotPlex uses Feishu CardKit V1 for real-time, typewriter-style responses.
- **STT (Speech-to-Text)**: Send voice messages directly; the system automatically transcribes them for AI processing.
- **Approval Confirmation**: Interactive cards are sent before running tools; click buttons to authorize.
- **Emoji Feedback**: Real-time status feedback (⏳/🔄/✅) is provided via emojis below messages.

---

## Permission JSON (Scopes)

Paste the following JSON into **"Permission Management -> Batch Import"**:

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
> Scope details:
> - `im:message` series: For receiving and sending various messages.
> - `im:resource:download`: For handling images/files sent by users.
> - `bot:info`: To retrieve the bot's own ID.
> - `im:message.reaction`: To provide status feedback via emojis.
