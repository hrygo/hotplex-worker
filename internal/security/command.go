package security

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// AllowedCommands is the whitelist of permitted worker binary names.
// Only these commands may be executed by the gateway.
var AllowedCommands = map[string]bool{
	"claude":   true,
	"opencode": true,
	// Add other allowed worker binaries here.
}

// RegisterCommand adds a binary name to the allowed commands whitelist.
// Validates the name for path separators, dangerous characters, and empty strings.
func RegisterCommand(name string) error {
	if name == "" {
		return fmt.Errorf("security: empty command name")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("security: command name %q must not contain path separators", name)
	}
	if ContainsDangerousChars(name) {
		return fmt.Errorf("security: command name %q contains dangerous characters", name)
	}
	AllowedCommands[name] = true
	return nil
}

// ValidateCommand checks that the command name is in the allowed list.
// Returns nil if valid, or an error describing why it was rejected.
func ValidateCommand(name string) error {
	if name == "" {
		return fmt.Errorf("security: empty command name")
	}
	if !AllowedCommands[name] {
		return fmt.Errorf("security: command %q not in whitelist", name)
	}
	return nil
}

// BuildSafeCommand creates a safe exec.Cmd after validating the command name.
func BuildSafeCommand(name string, args ...string) (*exec.Cmd, error) {
	if err := ValidateCommand(name); err != nil {
		return nil, err
	}
	return exec.Command(name, args...), nil
}

// DangerousChars contains characters that are unsafe in exec arguments
// even when not using a shell. These indicate potential injection attempts.
var DangerousChars = []string{
	";", "|", "&", "`", "$", "\\", "\n", "\r",
	"(", ")", "{", "}", "[", "]", "<", ">",
	"!", "#", "~", "*", "?", " ", "\t",
}

// ContainsDangerousChars reports whether the input contains shell/metacharacters
// that have no valid use in a non-shell exec argument.
func ContainsDangerousChars(input string) bool {
	for _, ch := range DangerousChars {
		if strings.Contains(input, ch) {
			return true
		}
	}
	return false
}

// SanitizeArg removes non-printable and control characters from an argument.
// This is a defense-in-depth measure; os/exec is already safe without it.
func SanitizeArg(input string) string {
	var b strings.Builder
	for _, r := range input {
		if r >= 32 && r < 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ─── Bash Command Policy ───────────────────────────────────────────────────────

// BashPolicyViolation describes a detected dangerous bash command.
type BashPolicyViolation struct {
	Severity string // "P0" (auto-deny) or "P1" (warn/log)
	Reason   string
	Command  string
}

var (
	// DangerousCommandPatterns matches bash commands that are dangerous.
	// P0: auto-deny. P1: log and require confirmation.
	DangerousCommandPatterns = []struct {
		Pattern  *regexp.Regexp
		Severity string
		Reason   string
	}{
		// P0: Destruction
		{regexp.MustCompile(`(?i)^\s*rm\s+-rf\s+/`), "P0", "root recursive delete"},
		{regexp.MustCompile(`(?i)^\s*dd\s+.*of=/`), "P0", "direct write to block device"},
		{regexp.MustCompile(`(?i)^\s*mkfs`), "P0", "filesystem format"},
		{regexp.MustCompile(`(?i)^\s*fdisk`), "P0", "disk partition operation"},
		{regexp.MustCompile(`(?i)^\s*:\(\)\s*\{\s*:\|`), "P0", "fork bomb"},

		// P1: Credential exfiltration
		{regexp.MustCompile(`(?i)(ssh|scp|rsync)\s+.*-i\s+`), "P1", "specifying SSH key file"},
		{regexp.MustCompile(`(?i)cat\s+.*\.ssh/`), "P1", "reading SSH keys"},
		{regexp.MustCompile(`(?i)gh\s+auth\s+token`), "P1", "GitHub token access"},

		// P1: Cloud metadata probing (SSRF)
		{regexp.MustCompile(`(?i)curl.*169\.254\.169\.254`), "P1", "AWS metadata probe"},
		{regexp.MustCompile(`(?i)wget.*169\.254\.169\.254`), "P1", "AWS metadata probe"},
		{regexp.MustCompile(`(?i)curl.*metadata\.google`), "P1", "GCP metadata probe"},

		// P1: Persistence
		{regexp.MustCompile(`(?i)(crontab|cron).*-e`), "P1", "modifying scheduled tasks"},
		{regexp.MustCompile(`(?i)echo.*>>.*authorized_keys`), "P1", "SSH persistence via authorized_keys"},
	}
)

// CheckBashCommand inspects a bash command for dangerous patterns.
// Returns a BashPolicyViolation if found, or nil if the command is safe.
func CheckBashCommand(cmd string) *BashPolicyViolation {
	if cmd == "" {
		return nil
	}
	for _, entry := range DangerousCommandPatterns {
		if entry.Pattern.MatchString(cmd) {
			return &BashPolicyViolation{
				Severity: entry.Severity,
				Reason:   entry.Reason,
				Command:  cmd,
			}
		}
	}
	return nil
}

// IsAutoDeny reports whether the given violation must be denied without confirmation.
func (v *BashPolicyViolation) IsAutoDeny() bool {
	return v != nil && v.Severity == "P0"
}
