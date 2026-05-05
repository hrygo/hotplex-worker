package messaging

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

// ContextSeverity represents the urgency level of context window usage.
type ContextSeverity string

const (
	SeverityComfortable ContextSeverity = "comfortable"
	SeverityModerate    ContextSeverity = "moderate"
	SeverityHigh        ContextSeverity = "high"
	SeverityCritical    ContextSeverity = "critical"
)

// ContextDisplayInfo holds pre-computed display fields for context usage.
type ContextDisplayInfo struct {
	Severity      ContextSeverity
	Icon          string
	Label         string
	TokenDisplay  string
	ProgressBar   string
	Percentage    int
	TopCategories []events.ContextCategory
	Model         string
	ExtrasLine    string
	ActionTip     string
}

// ExtractContextUsageData extracts ContextUsageData from an envelope.
func ExtractContextUsageData(env *events.Envelope) (events.ContextUsageData, error) {
	d, ok := events.DecodeAs[events.ContextUsageData](env.Event.Data)
	if !ok {
		return d, fmt.Errorf("unexpected context data type: %T", env.Event.Data)
	}
	return d, nil
}

// SeverityLevel maps a usage percentage to a severity level.
func SeverityLevel(percentage int) ContextSeverity {
	switch {
	case percentage > 90:
		return SeverityCritical
	case percentage > 75:
		return SeverityHigh
	case percentage >= 50:
		return SeverityModerate
	default:
		return SeverityComfortable
	}
}

// SeverityIcon returns the colored circle emoji for a severity level.
func SeverityIcon(severity ContextSeverity) string {
	switch severity {
	case SeverityComfortable:
		return "🟢"
	case SeverityModerate:
		return "🟡"
	case SeverityHigh:
		return "🟠"
	case SeverityCritical:
		return "🔴"
	default:
		return "⚪"
	}
}

// SeverityLabel returns a human-readable label for a severity level.
func SeverityLabel(severity ContextSeverity) string {
	switch severity {
	case SeverityComfortable:
		return "Comfortable"
	case SeverityModerate:
		return "Moderate"
	case SeverityHigh:
		return "High"
	case SeverityCritical:
		return "Critical"
	default:
		return ""
	}
}

// FormatTokenCount converts a raw token count to a human-friendly string.
// Examples: 76284 → "~76K", 200000 → "200K", 1500 → "~1.5K", 999 → "999"
func FormatTokenCount(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	k := float64(tokens) / 1000
	if k == float64(int(k)) {
		return fmt.Sprintf("%dK", int(k))
	}
	return fmt.Sprintf("%.1fK", k)
}

// FormatTokenDisplay produces a human-friendly "used / max" string.
func FormatTokenDisplay(used, max int) string {
	return FormatTokenCount(used) + " / " + FormatTokenCount(max)
}

// BuildProgressBar renders a text-based progress bar.
// Example: BuildProgressBar(67, 10) → "[███████░░░] 67%"
func BuildProgressBar(percentage, width int) string {
	if width <= 0 {
		width = 10
	}
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	filled := (percentage * width) / 100
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// FormatTopCategories returns the top N categories sorted by token count descending.
// It also filters out the "Skills" category when skills info is available separately.
func FormatTopCategories(categories []events.ContextCategory, limit int, hasSkillsInfo bool) []events.ContextCategory {
	if len(categories) == 0 {
		return nil
	}
	filtered := make([]events.ContextCategory, 0, len(categories))
	for _, c := range categories {
		if hasSkillsInfo && strings.EqualFold(c.Name, "Skills") {
			continue
		}
		filtered = append(filtered, c)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Tokens > filtered[j].Tokens
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// FormatActionTip returns actionable guidance based on severity.
// Returns empty string for comfortable/moderate levels (no action needed).
func FormatActionTip(severity ContextSeverity) string {
	switch severity {
	case SeverityHigh:
		return "💡 Context usage is high. Consider /compact to free up space."
	case SeverityCritical:
		return "🔴 Context nearly full! Use /compact to compress or /reset to start fresh."
	default:
		return ""
	}
}

// BuildExtrasLine consolidates metadata counts into a single line.
// Example: "8 skills · 3 memory files · 5 MCP tools · 2 agents"
func BuildExtrasLine(d events.ContextUsageData) string {
	var parts []string
	if d.Skills.Total > 0 {
		parts = append(parts, fmt.Sprintf("%d skills", d.Skills.Total))
	}
	if d.MemoryFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d memory files", d.MemoryFiles))
	}
	if d.MCPTools > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP tools", d.MCPTools))
	}
	if d.Agents > 0 {
		parts = append(parts, fmt.Sprintf("%d agents", d.Agents))
	}
	return strings.Join(parts, " · ")
}

// BuildContextDisplay assembles all display fields from ContextUsageData.
func BuildContextDisplay(d events.ContextUsageData) ContextDisplayInfo {
	severity := SeverityLevel(d.Percentage)
	topCats := FormatTopCategories(d.Categories, 3, d.Skills.Total > 0)
	return ContextDisplayInfo{
		Severity:      severity,
		Icon:          SeverityIcon(severity),
		Label:         SeverityLabel(severity),
		TokenDisplay:  FormatTokenDisplay(d.TotalTokens, d.MaxTokens),
		ProgressBar:   BuildProgressBar(d.Percentage, 10),
		Percentage:    d.Percentage,
		TopCategories: topCats,
		Model:         d.Model,
		ExtrasLine:    BuildExtrasLine(d),
		ActionTip:     FormatActionTip(severity),
	}
}

// FormatCanonicalText produces the canonical plain-text representation of context usage.
// This is the baseline output shared across all platforms.
func FormatCanonicalText(d events.ContextUsageData) string {
	info := BuildContextDisplay(d)

	var b strings.Builder
	// Header: icon + progress bar + tokens (single line)
	fmt.Fprintf(&b, "%s %s %s", info.Icon, info.ProgressBar, info.TokenDisplay)
	// Model
	if info.Model != "" {
		fmt.Fprintf(&b, "\nModel: %s", info.Model)
	}
	// Top categories
	if len(info.TopCategories) > 0 {
		catParts := make([]string, len(info.TopCategories))
		for i, c := range info.TopCategories {
			catParts[i] = fmt.Sprintf("%s: %s", c.Name, FormatTokenCount(c.Tokens))
		}
		b.WriteString("\nTop: ")
		b.WriteString(strings.Join(catParts, ", "))
	}
	// Extras line
	if info.ExtrasLine != "" {
		b.WriteString("\n⚡ ")
		b.WriteString(info.ExtrasLine)
	}
	// Action tip
	if info.ActionTip != "" {
		b.WriteString("\n")
		b.WriteString(info.ActionTip)
	}

	return b.String()
}
