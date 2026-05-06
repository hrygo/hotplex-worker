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

// workerSecretPrefix is the .env prefix for secrets that should be passed to
// worker subprocesses. Only vars with this prefix are stripped and injected.
// All other HOTPLEX_* vars are gateway-internal and never reach workers.
const workerSecretPrefix = "HOTPLEX_WORKER_"

// setOrAppend sets key=value in env, replacing existing entry or appending.
func setOrAppend(env []string, entry string) []string {
	key, _, _ := strings.Cut(entry, "=")
	for i, existing := range env {
		if strings.HasPrefix(existing, key+"=") {
			env[i] = entry
			return env
		}
	}
	return append(env, entry)
}

// BuildEnv constructs the environment variables for a CLI worker process.
//
// Priority (low → high):
//  1. os.Environ() — filtered through blocklist
//  2. HOTPLEX_WORKER_ prefix-stripped injections from .env
//  3. session.Env — per-session overrides
//  4. ConfigEnv — highest priority config overrides
//
// HOTPLEX_WORKER_ prefix stripping:
//
//	Only vars prefixed with HOTPLEX_WORKER_ are stripped and passed to workers.
//	Example: HOTPLEX_WORKER_GITHUB_TOKEN=xxx → GITHUB_TOKEN=xxx in worker env.
//	All other HOTPLEX_* vars (JWT_SECRET, ADMIN_TOKEN, etc.) are gateway-internal
//	and blocked from reaching workers.
//	When a stripped var exists, the system-level version is dynamically blocked
//	to prevent the gateway's own secrets from leaking to workers.
//
// Blocklist entries ending with "_" are treated as prefix matches.
func BuildEnv(session worker.SessionInfo, blocklist []string, workerTypeLabel string) []string {
	env := make([]string, 0, len(os.Environ()))

	// Build blocklist set, tracking prefix entries.
	blockSet := make(map[string]bool)
	prefixKeys := make([]string, 0)
	for _, k := range blocklist {
		if strings.HasSuffix(k, "_") {
			prefixKeys = append(prefixKeys, k)
		} else {
			blockSet[k] = true
		}
	}

	// Merge config-driven blocklist (worker.env_blocklist).
	for _, k := range session.ConfigBlocklist {
		if strings.HasSuffix(k, "_") {
			prefixKeys = append(prefixKeys, k)
		} else {
			blockSet[k] = true
		}
	}

	// Phase 1: Scan HOTPLEX_WORKER_* vars for prefix stripping.
	// HOTPLEX_WORKER_GITHUB_TOKEN → GITHUB_TOKEN.
	// Only HOTPLEX_WORKER_ prefix is stripped; other HOTPLEX_* vars are untouched.
	stripMap := make(map[string]string)
	for _, e := range os.Environ() {
		key, val, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, workerSecretPrefix) {
			continue
		}
		strippedKey := strings.TrimPrefix(key, workerSecretPrefix)
		if strippedKey == "" {
			continue
		}
		stripMap[strippedKey] = val
	}

	// Phase 2: Filter os.Environ() through blocklist + dynamic blocks.
	for _, e := range os.Environ() {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if blockSet[key] || hasAnyPrefix(key, prefixKeys) {
			continue
		}
		// HOTPLEX_WORKER_* vars are handled by stripping, not passed through directly.
		if strings.HasPrefix(key, workerSecretPrefix) {
			continue
		}
		// Dynamic block: system-level version of a stripped var is blocked
		// (e.g., system GITHUB_TOKEN blocked when HOTPLEX_WORKER_GITHUB_TOKEN exists).
		if _, blocked := stripMap[key]; blocked {
			continue
		}

		env = append(env, e)
	}

	// Phase 3: Add HOTPLEX session vars.
	env = append(env,
		"HOTPLEX_SESSION_ID="+session.SessionID,
		"HOTPLEX_WORKER_TYPE="+workerTypeLabel,
	)

	// Phase 4: Inject stripped HOTPLEX_WORKER_* vars (override system env).
	for k, v := range stripMap {
		env = setOrAppend(env, k+"="+v)
	}

	// Phase 5: Add session-specific env vars (override stripped vars).
	for k, v := range session.Env {
		if k == "" {
			continue
		}
		env = setOrAppend(env, k+"="+v)
	}

	// Phase 6: Strip nested agent config (CLAUDECODE=).
	env = security.StripNestedAgent(env)

	// Phase 7: Apply config-driven env vars (worker.environment). Highest priority.
	for _, e := range session.ConfigEnv {
		if e == "" || !strings.Contains(e, "=") {
			continue
		}
		key, _, _ := strings.Cut(e, "=")
		replaced := false
		for i, existing := range env {
			if strings.HasPrefix(existing, key+"=") {
				env[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, e)
		}
	}

	return env
}
