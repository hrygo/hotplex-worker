package feishu

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Card header template color constants (Feishu CardKit v2).
const (
	headerBlue   = "blue"
	headerWathet = "wathet"
	headerGrey   = "grey"
	headerOrange = "orange"
	headerYellow = "yellow"
	headerViolet = "violet"
)

// cardHeader defines a Card JSON 2.0 header component.
type cardHeader struct {
	Title    string    // Required.
	Subtitle string    // Optional.
	Template string    // Optional. Color theme (blue, wathet, grey, etc.).
	Tags     []cardTag // Optional. Up to 3 text_tag_list entries (server truncates excess).
}

// cardTag defines a text_tag_list entry in the card header.
type cardTag struct {
	Text  string
	Color string
}

// toMap converts cardHeader to a map for JSON serialization.
// Zero-value omission: Template empty -> omit; Tags nil/empty -> omit; Subtitle empty -> omit.
// Returns nil if Title is empty.
func (h cardHeader) toMap() map[string]any {
	if h.Title == "" {
		return nil
	}
	m := map[string]any{
		"title": map[string]any{"tag": "plain_text", "content": h.Title},
	}
	if h.Subtitle != "" {
		m["subtitle"] = map[string]any{"tag": "plain_text", "content": h.Subtitle}
	}
	if h.Template != "" {
		m["template"] = h.Template
	}
	if len(h.Tags) > 0 {
		tags := make([]map[string]any, 0, len(h.Tags))
		for _, t := range h.Tags {
			if t.Text == "" {
				continue
			}
			tag := map[string]any{
				"tag":  "text_tag",
				"text": map[string]any{"tag": "plain_text", "content": t.Text},
			}
			if t.Color != "" {
				tag["color"] = t.Color
			}
			tags = append(tags, tag)
		}
		if len(tags) > 0 {
			m["text_tag_list"] = tags
		}
	}
	return m
}

// buildCard constructs a standard CardKit v2 card (non-streaming) with optional header.
func buildCard(header cardHeader, config map[string]any, elements []map[string]any) string {
	card := map[string]any{
		"schema": "2.0",
		"config": config,
		"body":   map[string]any{"elements": elements},
	}
	if hm := header.toMap(); hm != nil {
		card["header"] = hm
	}
	return encodeCard(card)
}

// buildStreamingCard constructs a streaming card with streaming_mode, element_id, summary, and optional header.
func buildStreamingCard(header cardHeader, summary, content string) string {
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
			"summary":        map[string]any{"content": summary},
		},
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":        "markdown",
					"element_id": streamingElementID,
					"content":    content,
				},
			},
		},
	}
	if hm := header.toMap(); hm != nil {
		card["header"] = hm
	}
	return encodeCard(card)
}

// stringPtr returns a pointer to s. Used for SDK builder patterns.
func stringPtr(s string) *string { return &s }

// shortenModel produces a compact model name for tag display.
// "claude-sonnet-4-20250514" -> "claude-4"; "gpt-4o" -> "gpt-4o".
func shortenModel(name string) string {
	if i := strings.Index(name, "-20"); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "-preview"); i > 0 {
		name = name[:i]
	}
	// Strip provider prefix: "anthropic/claude-4" -> "claude-4"
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// shortenDir extracts the last path segment for tag display.
// "/home/user/project" -> "project"; "" -> "".
func shortenDir(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// turnTags builds text_tag_list from turn metadata (max 3 tags, server truncates excess).
// Order: [#N] neutral, [model] turquoise, [dir·branch] green.
func turnTags(turnNum int, model, branch, workDir string) []cardTag {
	var tags []cardTag
	if turnNum > 0 {
		tags = append(tags, cardTag{Text: fmt.Sprintf("#%d", turnNum)})
	}
	if model != "" {
		tags = append(tags, cardTag{Text: shortenModel(model), Color: "turquoise"})
	}
	// Combine workdir and branch into one tag to stay within 3-tag limit.
	dir := shortenDir(workDir)
	if dir != "" && branch != "" {
		if len(branch) > 24 {
			branch = branch[:24]
		}
		tags = append(tags, cardTag{Text: dir + "·" + branch, Color: "green"})
	} else if dir != "" {
		tags = append(tags, cardTag{Text: dir, Color: "green"})
	} else if branch != "" {
		if len(branch) > 24 {
			branch = branch[:24]
		}
		tags = append(tags, cardTag{Text: branch, Color: "indigo"})
	}
	return tags
}
