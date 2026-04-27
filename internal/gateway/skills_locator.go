package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

// FileSystemSkillsLocator discovers skills from Claude Code skills directories:
//   - ~/.agents/skills/
//   - ~/.claude/skills/ (symlinks skipped)
//   - ./.agents/skills/ (project-level)
//   - ./.claude/skills/ (project-level, symlinks skipped)
//
// Duplicate names are filtered (first wins). Skills are discovered from
// SKILL.md files within each skill directory.
type FileSystemSkillsLocator struct {
	mu   sync.RWMutex
	seen map[string]bool // deduplication
}

// NewFileSystemSkillsLocator creates a new skills locator.
func NewFileSystemSkillsLocator(cfg *config.Config) *FileSystemSkillsLocator {
	return &FileSystemSkillsLocator{
		seen: make(map[string]bool),
	}
}

// List returns all skills discovered from standard skills directories.
func (l *FileSystemSkillsLocator) List(ctx context.Context, homeDir, workDir string) ([]Skill, error) {
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}

	var dirs []string

	// User-level directories
	userAgents := filepath.Join(homeDir, ".agents", "skills")
	userClaude := filepath.Join(homeDir, ".claude", "skills")
	dirs = append(dirs, userAgents, userClaude)

	// Project-level directories
	if workDir != "" {
		projAgents := filepath.Join(workDir, ".agents", "skills")
		projClaude := filepath.Join(workDir, ".claude", "skills")
		dirs = append(dirs, projAgents, projClaude)
		// Also check current working dir (hotplex repo root)
		cwd, _ := os.Getwd()
		if cwd != "" && cwd != workDir {
			dirs = append(dirs, filepath.Join(cwd, ".agents", "skills"))
			dirs = append(dirs, filepath.Join(cwd, ".claude", "skills"))
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Reset seen map for each scan
	l.seen = make(map[string]bool)

	var skills []Skill
	for _, dir := range dirs {
		l.scanDir(dir, &skills)
	}
	return skills, nil
}

// scanDir recursively scans a skills directory, skipping symlinks.
func (l *FileSystemSkillsLocator) scanDir(dir string, skills *[]Skill) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		// Skip symlinks entirely (dedup by name, avoid external links)
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())

		// Try SKILL.md first, then README.md
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			skillPath = filepath.Join(skillDir, "README.md")
		}

		name, desc, ok := l.parseSkillFile(skillPath)
		if !ok {
			continue
		}

		// Deduplicate by name
		if l.seen[name] {
			continue
		}
		l.seen[name] = true

		// Determine source label
		source := "local"
		if strings.Contains(skillDir, "/.agents/") || strings.Contains(skillDir, "\\.agents\\") {
			source = "project"
		}

		*skills = append(*skills, Skill{
			Name:        name,
			Description: desc,
			Source:      source,
		})
	}
}

// parseSkillFile reads a SKILL.md or README.md and extracts name + description.
func (l *FileSystemSkillsLocator) parseSkillFile(path string) (name, description string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	content := string(data)

	// Parse YAML frontmatter
	if !strings.HasPrefix(content, "---") {
		return "", "", false
	}

	frontmatterEnd := strings.Index(content[3:], "---")
	if frontmatterEnd < 0 {
		return "", "", false
	}
	frontmatterEnd += 3 // account for the opening ---

	fm := content[3:frontmatterEnd]
	lines := strings.Split(fm, "\n")

	var fmName, fmDesc string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			fmName = strings.TrimPrefix(line, "name:")
			fmName = strings.Trim(fmName, " \t\"")
		}
		if strings.HasPrefix(line, "description:") {
			fmDesc = strings.TrimPrefix(line, "description:")
			fmDesc = strings.Trim(fmDesc, " \t\"")
		}
	}

	if fmName == "" {
		return "", "", false
	}

	// Truncate description to reasonable length
	if len(fmDesc) > 120 {
		fmDesc = fmDesc[:117] + "..."
	}

	return fmName, fmDesc, true
}
