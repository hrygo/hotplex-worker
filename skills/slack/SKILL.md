---
name: slack
description: Send messages, upload files, and interact with Slack via hotplex CLI
version: 1.1.0
---

# Slack Integration

Send messages, upload files, and interact with Slack workspaces via `hotplex slack` CLI subcommands.

## When to Use

- User says "send to Slack", "slack send", "发到 Slack"
- Content generated and needs delivery (podcasts, reports, images, videos)
- Need to add bookmarks/links in a channel

## Command Reference

| Scenario | Command |
|----------|---------|
| Send message | `hotplex slack send-message --text "content"` |
| Upload file | `hotplex slack upload-file --file <path> --title "title"` |
| Update message | `hotplex slack update-message --channel <id> --ts <ts> --text "new"` |
| Schedule message | `hotplex slack schedule-message --text "reminder" --at "09:00"` |
| Download file | `hotplex slack download-file --file-id <id> --output ./path` |
| List channels | `hotplex slack list-channels --types im,public_channel` |
| Add bookmark | `hotplex slack bookmark add --channel <id> --title "title" --url <url>` |
| List bookmarks | `hotplex slack bookmark list --channel <id>` |
| Remove bookmark | `hotplex slack bookmark remove --channel <id> --bookmark-id <id>` |
| Add reaction | `hotplex slack react add --channel <id> --ts <ts> --emoji white_check_mark` |
| Remove reaction | `hotplex slack react remove --channel <id> --ts <ts> --emoji white_check_mark` |

## Default Behavior

- Without `--channel`: auto-sends to current conversation (via `HOTPLEX_SLACK_CHANNEL_ID` env var injected by Gateway)
- Without `--title`: uses filename
- Add `--json` for structured JSON output

## Workflow Examples

### Podcast → Slack
```bash
notebooklm download audio ./podcast.mp3 -n <notebook_id>
hotplex slack upload-file --file ./podcast.mp3 --title "Podcast"
```

### Report → Bookmark
```bash
hotplex slack bookmark add --channel C0ABC --title "Report" --url "https://example.com/report"
```

### Task Complete → Visual Feedback
```bash
hotplex slack react add --ts <ts> --emoji white_check_mark
```
