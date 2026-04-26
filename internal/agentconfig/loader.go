// Package agentconfig loads and assembles agent personality, rules, and context
// files from a shared configuration directory, injecting them into worker
// sessions via B-channel (system-level) and C-channel (context-level) paths.
package agentconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentConfigs holds loaded content for all agent config files.
type AgentConfigs struct {
	Soul   string // SOUL.md + SOUL.<platform>.md  (B channel)
	Agents string // AGENTS.md + AGENTS.<platform>.md (B channel)
	Skills string // SKILLS.md + SKILLS.<platform>.md (B channel)
	User   string // USER.md + USER.<platform>.md  (C channel)
	Memory string // MEMORY.md + MEMORY.<platform>.md (C channel)
}

// MaxFileChars is the maximum character limit per file.
const MaxFileChars = 8_000

// MaxTotalChars is the maximum combined character limit across all files.
const MaxTotalChars = 40_000

// Load reads all config files from dir, appending platform-specific variants.
// Returns AgentConfigs with frontmatter stripped and size limits enforced.
// Platform can be "slack", "feishu", or "" (websocket/gateway direct).
// Missing files are silently skipped. A non-existent dir is not an error.
func Load(dir, platform string) (*AgentConfigs, error) {
	if dir == "" {
		return &AgentConfigs{}, nil
	}

	c := &AgentConfigs{}
	var total int

	load := func(baseName string, target *string) {
		content, n := loadFile(dir, baseName, platform)
		if n == 0 {
			return
		}
		if total+n > MaxTotalChars {
			n = MaxTotalChars - total
			if n <= 0 {
				return
			}
			content = content[:n]
		}
		total += n
		*target = content
	}

	load("SOUL.md", &c.Soul)
	load("AGENTS.md", &c.Agents)
	load("SKILLS.md", &c.Skills)
	load("USER.md", &c.User)
	load("MEMORY.md", &c.Memory)

	return c, nil
}

// loadFile reads a base file and appends its platform variant.
// Returns the combined content and total character count.
func loadFile(dir, baseName, platform string) (string, int) {
	content := readFile(dir, baseName)
	if platform != "" {
		ext := filepath.Ext(baseName)
		prefix := strings.TrimSuffix(baseName, ext)
		variantName := prefix + "." + platform + ext
		variant := readFile(dir, variantName)
		if variant != "" {
			if content != "" {
				content += "\n\n" + variant
			} else {
				content = variant
			}
		}
	}
	return content, len(content)
}

// readFile reads a file, strips YAML frontmatter, and enforces per-file size limit.
// Returns empty string if the file does not exist.
func readFile(dir, name string) string {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := stripFrontmatter(string(data))
	if len(s) > MaxFileChars {
		s = s[:MaxFileChars]
	}
	return s
}

// stripFrontmatter removes YAML frontmatter (--- blocks) from markdown content.
func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r") {
		return s
	}
	scanner := bufio.NewScanner(strings.NewReader(s))
	scanner.Scan() // skip opening ---
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" || line == "---\r" {
			rest := ""
			if scanner.Scan() {
				rest = scanner.Text()
			}
			var buf strings.Builder
			buf.WriteString(rest)
			for scanner.Scan() {
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(scanner.Text())
			}
			return buf.String()
		}
	}
	// Malformed frontmatter — return original content as-is.
	return s
}

// EnsureDir creates the config directory and its parents if they don't exist.
func EnsureDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("agentconfig: empty dir")
	}
	return os.MkdirAll(dir, 0o755)
}

// IsEmpty returns true if all config fields are empty.
func (c *AgentConfigs) IsEmpty() bool {
	return c.Soul == "" && c.Agents == "" && c.Skills == "" &&
		c.User == "" && c.Memory == ""
}
