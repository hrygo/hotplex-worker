package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// scanDirs scans all skill directories and returns deduplicated skills.
// Order: global dirs first, then project dirs (project overrides global by name).
func scanDirs(homeDir, workDir string) []Skill {
	dirs := []struct {
		path   string
		source string
	}{
		{filepath.Join(homeDir, ".claude", "skills"), SourceGlobal},
		{filepath.Join(homeDir, ".agents", "skills"), SourceGlobal},
		{filepath.Join(workDir, ".claude", "skills"), SourceProject},
		{filepath.Join(workDir, ".agents", "skills"), SourceProject},
	}

	var all []Skill
	for _, d := range dirs {
		skills, err := scanDir(d.path, d.source)
		if err != nil {
			continue
		}
		all = append(all, skills...)
	}
	return dedup(all)
}

// scanDir reads all .md files from a single skill directory.
// Skips symlink files to avoid duplicates from linked directories.
func scanDir(dir, source string) ([]Skill, error) {
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	var result []Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		// Skip symlinks — .agents is often a symlink to .claude
		if isSymlink(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Subdirectory: look for SKILL.md or skill.md
			for _, name := range []string{"SKILL.md", "skill.md"} {
				candidate := filepath.Join(fullPath, name)
				if s := parseSkillFile(candidate, source); s != nil {
					result = append(result, *s)
					break
				}
			}
		} else if strings.HasSuffix(entry.Name(), ".md") {
			if s := parseSkillFile(fullPath, source); s != nil {
				result = append(result, *s)
			}
		}
	}
	return result, nil
}

// isSymlink returns true if the path is a symbolic link.
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

// parseSkillFile reads a .md file and extracts name/description from YAML frontmatter.
// Returns nil if the file cannot be read or has no valid frontmatter.
func parseSkillFile(path, source string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	fm := extractFrontmatter(data)
	if fm == nil {
		return nil
	}

	if fm.Name == "" {
		return nil
	}

	desc := strings.TrimSpace(fm.Description)
	// Unfold YAML folded/scalar blocks
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = collapseSpaces(desc)

	return &Skill{
		Name:        fm.Name,
		Description: desc,
		Source:      source,
	}
}

// extractFrontmatter extracts and parses YAML frontmatter from markdown content.
// Frontmatter is delimited by `---` on its own line at the start of the file.
func extractFrontmatter(data []byte) *skillFrontmatter {
	if !bytes.HasPrefix(data, []byte("---")) {
		return nil
	}

	// Find closing ---
	end := bytes.Index(data[3:], []byte("\n---"))
	if end < 0 {
		return nil
	}
	yamlBlock := data[3 : end+3]

	var fm skillFrontmatter
	if err := yaml.Unmarshal(yamlBlock, &fm); err != nil {
		return nil
	}
	return &fm
}

// dedup removes duplicate skills by name. Project skills override global ones.
func dedup(skills []Skill) []Skill {
	seen := make(map[string]int) // name -> index in result
	var result []Skill

	for _, s := range skills {
		if idx, ok := seen[s.Name]; ok {
			// Project overrides global
			if s.Source == SourceProject && result[idx].Source == SourceGlobal {
				result[idx] = s
			}
		} else {
			seen[s.Name] = len(result)
			result = append(result, s)
		}
	}
	return result
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if prev {
				continue
			}
			prev = true
		} else {
			prev = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
