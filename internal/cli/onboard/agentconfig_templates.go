package onboard

import (
	"embed"
	"strings"
)

//go:embed templates/*.md
var templateFS embed.FS

// DefaultTemplates returns the list of agent config template file names
// and their embedded content, ready for writing to the user's config dir.
func DefaultTemplates() []struct {
	Name    string
	Content string
} {
	names := []string{"SOUL.md", "AGENTS.md", "SKILLS.md", "USER.md", "MEMORY.md"}
	files := make([]struct {
		Name    string
		Content string
	}, 0, len(names))
	for _, n := range names {
		if c := readTemplate(n); c != "" {
			files = append(files, struct {
				Name    string
				Content string
			}{n, c})
		}
	}
	return files
}

// readTemplate reads a named template from the embedded filesystem.
// Returns empty string if the file is not found.
func readTemplate(name string) string {
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n") + "\n"
}
