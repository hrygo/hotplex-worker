package phrases

import (
	"strings"
)

// parseMarkdown parses a PHRASES.md file into categorized entries.
// Format:
//
//	## SectionName
//	- item 1
//	- item 2
//
// Lines that are neither headings nor list items are ignored.
func parseMarkdown(content string) map[string][]string {
	result := make(map[string][]string)
	var current string

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			current = strings.ToLower(strings.TrimSpace(trimmed[3:]))
			continue
		}

		if current == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(trimmed[2:])
			if item != "" {
				result[current] = append(result[current], item)
			}
		}
	}

	return result
}
