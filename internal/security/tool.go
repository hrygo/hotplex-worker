package security

import (
	"fmt"
)

// AllowedTools contains the set of permitted Claude Code tools.
var AllowedTools = map[string]bool{
	"Read":         true,
	"Edit":         true,
	"Write":        true,
	"Bash":         true,
	"Grep":         true,
	"Glob":         true,
	"Agent":        true,
	"WebFetch":     true,
	"NotebookEdit": true,
	"TodoWrite":    true,
}

// ProductionAllowedTools is the set of tools enabled in production (no Bash/WebFetch).
var ProductionAllowedTools = map[string]bool{
	"Read": true,
	"Grep": true,
	"Glob": true,
}

// ValidateTools checks that every tool in the list is allowed.
// Returns nil if all tools are valid, or an error listing the first invalid tool.
func ValidateTools(tools []string) error {
	for _, tool := range tools {
		if !AllowedTools[tool] {
			return fmt.Errorf("security: tool %q not in allowed list", tool)
		}
	}
	return nil
}

// BuildAllowedToolsArgs builds the --allowed-tools CLI arguments for claude.
func BuildAllowedToolsArgs(tools []string) []string {
	var args []string
	for _, tool := range tools {
		args = append(args, "--allowed-tools", tool)
	}
	return args
}

// IsToolAllowed returns true if the tool is in the allowed set.
func IsToolAllowed(tool string) bool {
	return AllowedTools[tool]
}
