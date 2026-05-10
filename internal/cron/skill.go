package cron

import _ "embed"

//go:embed cron-skill-manual.md
var embeddedManual string

// SkillManual returns the complete cron management manual content.
func SkillManual() string { return embeddedManual }
