package security

import (
	"fmt"
	"os"
)

// BaseEnvWhitelist contains standard system environment variables.
var BaseEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
}

// GoEnvWhitelist contains Go runtime environment variables.
var GoEnvWhitelist = []string{
	"GOPROXY", "GOSUMDB", "GONOSUMDB", "GOPRIVATE",
}

// WorkerEnvWhitelist contains worker-specific environment variables by type.
var WorkerEnvWhitelist = map[string][]string{
	"claude-code": {
		"CLAUDE_API_KEY", "CLAUDE_MODEL", "CLAUDE_BASE_URL",
		"CLAUDE_CODE_MODE", "CLAUDE_DISABLE_AUTO_PERMISSIONS",
	},
}

// HotPlexRequired contains the required HotPlex environment variables.
var HotPlexRequired = []string{
	"HOTPLEX_SESSION_ID",
	"HOTPLEX_WORKER_TYPE",
}

// HotPlexOptional contains optional HotPlex environment variables.
var HotPlexOptional = []string{
	"HOTPLEX_WORK_DIR",
	"HOTPLEX_TRACE_ENABLED",
	"HOTPLEX_LOG_LEVEL",
}

// ProtectedEnvVars lists system variables that cannot be used as secret or HotPlex keys.
// This prevents AddSecret/AddHotPlexVar from overriding system variables and
// hijacking the execution environment.
var ProtectedEnvVars = []string{
	// System basics — overriding changes execution behavior.
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD", "GID", "UID", "SHLVL",
	// Go runtime — overriding breaks Go program behavior.
	"GOROOT", "GOPATH", "GOPROXY", "GOSUMDB",
	// Shells — overriding allows PATH hijacking.
	"BASH", "BASH_VERSION", "ZSH_VERSION", "ZDOTDIR",
}

// IsProtectedEnvVar reports whether a variable name is a protected system variable
// that cannot be overridden by secrets or HotPlex vars.
func IsProtectedEnvVar(key string) bool {
	for _, p := range ProtectedEnvVars {
		if key == p {
			return true
		}
	}
	return false
}

// SafeEnvBuilder constructs a safe environment for worker processes.
// It prevents secrets or HotPlex vars from overriding protected system variables.
type SafeEnvBuilder struct {
	whitelist   []string
	hotplexVars map[string]string
	secrets     map[string]string
	lastErr     error
}

// NewSafeEnvBuilder creates a new SafeEnvBuilder with default whitelists.
func NewSafeEnvBuilder() *SafeEnvBuilder {
	wl := make([]string, len(BaseEnvWhitelist))
	copy(wl, BaseEnvWhitelist)
	wl = append(wl, GoEnvWhitelist...)
	return &SafeEnvBuilder{
		whitelist:   wl,
		hotplexVars: make(map[string]string),
		secrets:     make(map[string]string),
	}
}

// AddWorkerType adds environment variables specific to a worker type.
func (b *SafeEnvBuilder) AddWorkerType(workerType string) *SafeEnvBuilder {
	if extra, ok := WorkerEnvWhitelist[workerType]; ok {
		b.whitelist = append(b.whitelist, extra...)
	}
	return b
}

// AddHotPlexVar adds a HotPlex control variable.
// Returns an error if the key is a protected system variable.
func (b *SafeEnvBuilder) AddHotPlexVar(key, value string) error {
	if IsProtectedEnvVar(key) {
		b.lastErr = fmt.Errorf("security: cannot use %q as HotPlex var: protected system variable", key)
		return b.lastErr
	}
	b.hotplexVars[key] = value
	return nil
}

// AddSecret adds a secret variable (e.g., API key).
// Returns an error if the key is a protected system variable.
func (b *SafeEnvBuilder) AddSecret(key, value string) error {
	if IsProtectedEnvVar(key) {
		b.lastErr = fmt.Errorf("security: cannot use %q as secret key: protected system variable", key)
		return b.lastErr
	}
	b.secrets[key] = value
	return nil
}

// LastError returns the first error encountered during AddHotPlexVar or AddSecret.
func (b *SafeEnvBuilder) LastError() error {
	return b.lastErr
}

// Build constructs the final environment slice for exec.Cmd.Env.
func (b *SafeEnvBuilder) Build() []string {
	// 1. Add whitelisted system variables.
	seen := make(map[string]bool)
	for _, key := range b.whitelist {
		if val := os.Getenv(key); val != "" {
			seen[key] = true
		}
	}

	// 2. Add HotPlex vars (already validated not to override system vars).
	for key := range b.hotplexVars {
		seen[key] = true
	}

	// 3. Add secrets (already validated not to override system vars).
	for key := range b.secrets {
		seen[key] = true
	}

	env := make([]string, 0, len(seen))
	for key := range seen {
		var val string
		if v, ok := b.hotplexVars[key]; ok {
			val = v
		} else if v, ok := b.secrets[key]; ok {
			val = v
		} else {
			val = os.Getenv(key)
		}
		env = append(env, key+"="+val)
	}

	return env
}
