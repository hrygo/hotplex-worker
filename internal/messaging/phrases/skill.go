package phrases

import (
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
)

//go:embed phrases.md
var embeddedManual string

// SkillManual returns the complete phrases configuration manual content.
func SkillManual() string { return embeddedManual }

// ReleaseSkillManual writes the embedded manual to ~/.hotplex/skills/phrases.md.
func ReleaseSkillManual(log *slog.Logger) {
	dir, _ := os.UserHomeDir()
	skillsDir := filepath.Join(dir, ".hotplex", "skills")
	_ = os.MkdirAll(skillsDir, 0o755)
	path := filepath.Join(skillsDir, "phrases.md")
	if err := os.WriteFile(path, []byte(embeddedManual), 0o644); err != nil {
		log.Warn("phrases: failed to release skill manual", "path", path, "err", err)
	}
}
