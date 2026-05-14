# Phrases: Procedural Message Pool for Platform UI Feedback

**Date**: 2026-05-14
**Branch**: batch/messaging-dry-issues-376-257
**Related Issues**: #376, #257

---

## Motivation

The feishu adapter hardcodes 28 CLI tips and 8 greetings in `feishu/placeholder.go`. These serve as procedural UI feedback (placeholder cards, status indicators), not agent identity. Slack has no equivalent personalization. Extracting into a shared, configurable module enables:

1. **Platform abstraction** — both feishu and slack consume from the same phrase pools
2. **Configurability** — users and bots can customize greetings, tips, onboarding messages
3. **Per-bot personality** — bot-specific phrases are appended (not replaced), enriching each bot's unique voice
4. **Self-management** — bots can configure their own phrases via a B-channel skill manual

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Package location | `internal/messaging/phrases/` | Procedural UI behavior, not agent identity |
| Config directory | `~/.hotplex/phrases/` | Separate from `agent-configs/` to avoid cognitive confusion |
| Merge semantics | Cascade-append (not first-match-wins) | More entries = richer pool; opposite of agent-config's override model |
| File format | Markdown with `## Section` + `- item` | Simple, human-editable, git-friendly |
| Bot-level behavior | Append to inherited pool | Simplifies configuration; bot only defines what's unique |
| Hot reload | None (loaded at adapter init) | Consistent with agent-config; requires gateway restart |

## PHRASES.md File Format

```markdown
## Greetings
- 来啦～
- 交给我～
- 收到，马上～

## Tips
- 输入 /gc 可休眠当前会话，下次发消息自动恢复
- 输入 /reset 可重置上下文，从零开始新对话

## Onboarding
- 你好！有什么可以帮你？
- 嗨～今天想做什么？

## Custom
- Any user-defined section works
- Access via phrases.Random("custom")
```

**Parsing rules**:
- `## Name` (case-insensitive) defines a category
- `- text` is a list entry; blank lines between entries are optional
- Lines that are neither headings nor list items are ignored (allows free-form comments)
- Unknown `## Section` names are accessible via `Random("section-name")`

## Package Structure

```
internal/messaging/phrases/
├── phrases.go       # Phrases struct, Random(), All(), Categories(), Defaults()
├── loader.go        # Load(dir, platform, botID) cascade-append
├── parse.go         # parseMarkdown() → map[string][]string
├── phrases.md       # go:embed configuration manual
├── skill.go         # Manual() for B-channel injection
└── phrases_test.go
```

### Core Types

```go
package phrases

// Phrases holds categorized message pools used for procedural UI feedback.
// Immutable after creation — no mutex needed.
type Phrases struct {
    entries map[string][]string
}

// Random returns a random entry from the given category.
// Returns "" if category not found or empty.
func (p *Phrases) Random(category string) string

// All returns all entries for a category (for preview/debug).
func (p *Phrases) All(category string) []string

// Categories returns available category names.
func (p *Phrases) Categories() []string
```

### Loading

```go
// Load reads PHRASES.md from all levels with cascade-append:
//
//  1. code defaults (hardcoded via Defaults())
//  2. dir/PHRASES.md (global)
//  3. dir/{platform}/PHRASES.md
//  4. dir/{platform}/{botID}/PHRASES.md
//
// Each level's entries are appended to the pool, never replaced.
// Missing directory or file is not an error — skips gracefully.
func Load(dir, platform, botID string) (*Phrases, error)

// Defaults returns the hardcoded base entries.
func Defaults() *Phrases
```

**Merge algorithm**: `merge(dst, src map[string][]string)` appends `src[key]` values to `dst[key]` for all keys.

**Path safety**: `filepath.Base(botID) == botID` check prevents path traversal (same pattern as agentconfig).

## Cascade-Append Loading

```
Code defaults (Go hardcoded)
  ↓ append
~/.hotplex/phrases/PHRASES.md
  ↓ append
~/.hotplex/phrases/feishu/PHRASES.md
  ↓ append
~/.hotplex/phrases/feishu/ou_xxx/PHRASES.md
```

**Example**: If global defines 8 greetings, feishu/ adds 3, and `feishu/ou_xxx/` adds 2, the bot `ou_xxx` has 13 greetings to draw from.

## Default Values Migration

Current hardcoded arrays in `feishu/placeholder.go` move to `phrases.Defaults()`:

```go
func Defaults() *Phrases {
    return &Phrases{entries: map[string][]string{
        "greetings": {
            "来啦～", "交给我～", "收到，马上～", "好嘞！",
            "马上来～", "明白，开始干活！", "来了来了～", "收到！",
        },
        "tips": {
            // ... existing 28 CLI tips
        },
    }}
}
```

## Rendering Separation

Phrases provides text content only. Platform-specific rendering stays in adapters:

**Feishu** — sticker-wrapped placeholder card:
```go
func buildPlaceholderText(p *phrases.Phrases) string {
    return ":Get: " + p.Random("greetings") +
        "\n:StatusFlashOfInspiration: " + p.Random("tips")
}
```

**Slack** — status indicator text:
```go
a.SetAssistantStatus(ctx, channelID, threadTS, p.Random("greetings"))
```

## Injection Path

```go
// messaging_init.go — per-bot loading during adapter init
phrasesDir := filepath.Join(homeDir, ".hotplex", "phrases")
p, err := phrases.Load(phrasesDir, string(entry.Platform), entry.BotID)
if err != nil {
    log.Warn("phrases load failed, using defaults", "error", err)
    p = phrases.Defaults()
}
cfg.Extras["phrases"] = p
```

Adapter access:
```go
func (a *Adapter) phrases() *phrases.Phrases {
    if p, ok := a.config.Extras["phrases"].(*phrases.Phrases); ok {
        return p
    }
    return phrases.Defaults()
}
```

## Skill Manual: Embed → Release → Reference

Follows the same pattern as `cron/skill.go`: the full manual is embedded in the binary, released to disk on startup, and only a brief reference + path is injected into the B-channel.

**Step 1 — go:embed the manual:**
```go
//go:embed phrases.md
var embeddedManual string

func SkillManual() string { return embeddedManual }
```

**Step 2 — Release to disk on startup:**
```go
// In skill.go, called during adapter init (messaging_init.go)
func releaseSkillManual(log *slog.Logger) {
    dir, _ := os.UserHomeDir()
    skillsDir := filepath.Join(dir, ".hotplex", "skills")
    _ = os.MkdirAll(skillsDir, 0o755)
    path := filepath.Join(skillsDir, "phrases.md")
    if err := os.WriteFile(path, []byte(embeddedManual), 0o644); err != nil {
        log.Warn("phrases: failed to release skill manual", "path", path, "err", err)
    }
}
```

**Step 3 — B-channel reference only (in META-COGNITION.md or similar):**

The B-channel does NOT contain the full manual. It contains a brief description and file path reference, matching the cron pattern:

```
> **Phrases 配置**：识别到 phrases 配置意图后，阅读 `~/.hotplex/skills/phrases.md` 了解目录结构、文件格式和合并规则，然后通过文件操作配置自己的话术库。
```

This enables bots to self-manage their phrase pools by reading the released manual and editing `~/.hotplex/phrases/` files through tool calls.

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/messaging/phrases/phrases.go` | New | Phrases struct, Random(), Defaults() |
| `internal/messaging/phrases/loader.go` | New | Load() with cascade-append |
| `internal/messaging/phrases/parse.go` | New | parseMarkdown() |
| `internal/messaging/phrases/phrases.md` | New | go:embed skill manual |
| `internal/messaging/phrases/skill.go` | New | go:embed + SkillManual() + releaseSkillManual() |
| `internal/messaging/phrases/phrases_test.go` | New | Unit tests |
| `internal/messaging/feishu/placeholder.go` | Modify | Remove hardcoded arrays, use Phrases |
| `internal/messaging/feishu/streaming.go` | Modify | Accept Phrases parameter |
| `internal/messaging/feishu/handler.go` | Modify | Pass Phrases to streaming controller |
| `internal/messaging/slack/adapter.go` | Modify | Use Phrases for status text |
| `internal/messaging/config.go` | Modify | Extras accessor for Phrases |
| `cmd/hotplex/messaging_init.go` | Modify | Load Phrases per bot, call releaseSkillManual() |
| `internal/agentconfig/META-COGNITION.md` | Modify | Add phrases skill reference + file path |
