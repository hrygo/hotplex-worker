package base

import (
	"os"
	"slices"
	"strings"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
)

// hasAnyPrefix reports whether s has any of the given prefixes.
func hasAnyPrefix(s string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

// BuildEnv constructs the environment variables for a CLI worker process.
// It whitelist-filters os.Environ(), adds HOTPLEX_* vars, merges session.Env,
// and strips nested agent configuration.
//
// Whitelist entries ending with "_" are treated as prefix matches (e.g. "OTEL_"
// matches OTEL_EXPORTER, OTEL_SERVICE_NAME, etc.).
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
	env := make([]string, 0, len(os.Environ()))

	// Build whitelist set from provided list, tracking prefix entries.
	whitelistSet := make(map[string]bool)
	prefixKeys := make([]string, 0)
	for _, k := range whitelist {
		if strings.HasSuffix(k, "_") {
			prefixKeys = append(prefixKeys, k)
		} else {
			whitelistSet[k] = true
		}
	}

	// Iterate os.Environ(), keep if in whitelist (exact or prefix) OR in session.Env.
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]

		// Check if key is in whitelist (exact match).
		if whitelistSet[key] {
			env = append(env, e)
			continue
		}

		// Check prefix matches (e.g. OTEL_).
		if hasAnyPrefix(key, prefixKeys) {
			env = append(env, e)
			continue
		}

		// Check if key is in session env.
		if _, ok := session.Env[key]; ok {
			env = append(env, e)
		}
	}

	// Add HOTPLEX session vars.
	env = append(env,
		"HOTPLEX_SESSION_ID="+session.SessionID,
		"HOTPLEX_WORKER_TYPE="+workerTypeLabel,
	)

	// Add session-specific env vars (skip if in whitelist).
	for k, v := range session.Env {
		if k != "" && !whitelistSet[k] {
			env = append(env, k+"="+v)
		}
	}

	// Strip nested agent config (CLAUDECODE=).
	env = security.StripNestedAgent(env)

	return env
}
