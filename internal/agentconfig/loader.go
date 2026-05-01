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
// Missing files are silently skipped. Non-NotExist errors (permission denied, I/O errors) are returned.
func Load(dir, platform string) (*AgentConfigs, error) {
	if dir == "" {
		return &AgentConfigs{}, nil
	}

	c := &AgentConfigs{}
	var total int

	load := func(baseName string, target *string) error {
		content, n, err := loadFileWithErrorCount(dir, baseName, platform)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			return nil // missing file is expected
		}
		if n == 0 {
			return nil
		}
		if total+n > MaxTotalChars {
			n = MaxTotalChars - total
			if n <= 0 {
				return nil
			}
			content = content[:n]
		}
		total += n
		*target = content
		return nil
	}

	if err := load("SOUL.md", &c.Soul); err != nil {
		return nil, err
	}
	if err := load("AGENTS.md", &c.Agents); err != nil {
		return nil, err
	}
	if err := load("SKILLS.md", &c.Skills); err != nil {
		return nil, err
	}
	if err := load("USER.md", &c.User); err != nil {
		return nil, err
	}
	if err := load("MEMORY.md", &c.Memory); err != nil {
		return nil, err
	}

	return c, nil
}

// loadFileWithErrorCount is a helper that wraps loadFile and returns character count.
func loadFileWithErrorCount(dir, baseName, platform string) (string, int, error) {
	content, err := loadFile(dir, baseName, platform)
	if err != nil {
		return "", 0, err
	}
	return content, len(content), nil
}

// loadFile reads a base file and appends its platform variant.
// Returns the combined content and total character count.
// Propagates non-NotExist errors from readFile (permission denied, I/O errors).
func loadFile(dir, baseName, platform string) (string, error) {
	content, err := readFile(dir, baseName)
	if err != nil {
		return "", err
	}
	if platform != "" {
		ext := filepath.Ext(baseName)
		prefix := strings.TrimSuffix(baseName, ext)
		variantName := prefix + "." + platform + ext
		variant, err := readFile(dir, variantName)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if variant != "" {
			if content != "" {
				content += "\n\n" + variant
			} else {
				content = variant
			}
		}
	}
	return content, nil
}

// readFile reads a file, strips YAML frontmatter, and enforces per-file size limit.
// Returns ("", nil) if the file does not exist (expected), ("", error) for other errors.
func readFile(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("agentconfig: read %s: %w", name, err)
	}
	s := stripFrontmatter(string(data))
	if len(s) > MaxFileChars {
		s = s[:MaxFileChars]
	}
	return s, nil
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
