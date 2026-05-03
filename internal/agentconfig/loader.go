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
	Soul   string // SOUL.md   (B channel)
	Agents string // AGENTS.md (B channel)
	Skills string // SKILLS.md (B channel)
	User   string // USER.md   (C channel)
	Memory string // MEMORY.md (C channel)
}

// MaxFileChars is the maximum character limit per file.
const MaxFileChars = 8_000

// MaxTotalChars is the maximum combined character limit across all files.
const MaxTotalChars = 40_000

// Load reads all config files from dir using 3-level per-file fallback:
//
//  1. dir/{platform}/{botID}/{file}    — bot-level (highest priority)
//  2. dir/{platform}/{file}            — platform-level
//  3. dir/{file}                       — global-level
//
// Each file resolves independently. Missing files fall through to the next level.
// Platform can be "slack", "feishu", "webchat", or "" (no platform-level lookup).
// botID is used directly as directory name (e.g., Slack UserID, Feishu OpenID).
// Returns AgentConfigs with frontmatter stripped and size limits enforced.
func Load(dir, platform, botID string) (*AgentConfigs, error) {
	if dir == "" {
		return &AgentConfigs{}, nil
	}

	// Path safety: botID must not contain path traversal components.
	if botID != "" && filepath.Base(botID) != botID {
		return nil, fmt.Errorf("agentconfig: invalid botID %q: path separators not allowed", botID)
	}

	c := &AgentConfigs{}
	var total int

	load := func(baseName string, target *string) error {
		content, err := resolveFile(dir, platform, botID, baseName)
		if err != nil {
			return err
		}
		n := len(content)
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

// resolveFile implements the 3-level per-file fallback.
// Returns the content of the first non-empty file found, or ("", nil) if none exist.
// Non-NotExist I/O errors (e.g., permission denied) are propagated immediately
// rather than falling through — a file that exists but is unreadable indicates
// a real configuration problem that should not be silently masked.
func resolveFile(dir, platform, botID, fileName string) (string, error) {
	// 1. Bot-level: dir/platform/botID/fileName
	if botID != "" && platform != "" {
		content, err := readFile(filepath.Join(dir, platform, botID), fileName)
		if err != nil {
			return "", err
		}
		if content != "" {
			return content, nil
		}
	}
	// 2. Platform-level: dir/platform/fileName
	if platform != "" {
		content, err := readFile(filepath.Join(dir, platform), fileName)
		if err != nil {
			return "", err
		}
		if content != "" {
			return content, nil
		}
	}
	// 3. Global-level: dir/fileName
	return readFile(dir, fileName)
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
