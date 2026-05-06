package security

import "strings"

// cliProtectedVars are system variables that .env files must not override.
// Separate from worker blocklists since BuildEnv must pass HOME/PATH/USER
// through to worker processes.
var cliProtectedVars = map[string]bool{
	"HOME":          true,
	"PATH":          true,
	"USER":          true,
	"SHELL":         true,
	"CLAUDECODE":    true,
	"GATEWAY_ADDR":  true,
	"GATEWAY_TOKEN": true,
}

// IsProtected reports whether an environment variable key should not be
// overwritten from .env files. This prevents accidental override of critical
// system and gateway variables.
func IsProtected(key string) bool {
	return cliProtectedVars[strings.ToUpper(key)]
}

// StripNestedAgent removes CLAUDECODE= from the environment to prevent
// nested agent invocation.
func StripNestedAgent(env []string) []string {
	prefix := "CLAUDECODE="
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}
