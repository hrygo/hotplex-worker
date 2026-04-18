package security

import (
	"regexp"
	"strings"
)

// sensitiveEnvPrefixes are key prefixes that indicate sensitive environment variables.
var sensitiveEnvPrefixes = []string{
	"AWS_", "AWS_ACCESS_KEY", "AWS_SECRET", "AWS_SESSION_TOKEN",
	"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_",
	"AZURE_", "AZURE_CLIENT", "AZURE_SECRET", "AZURE_TOKEN",
	"STRIPE_", "STRIPE_SECRET",
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY",
	"API_KEY", "API_SECRET", "SECRET", "PRIVATE_KEY", "TOKEN",
	"PGP_", "GPG_",
	"SLACK_", "SENTRY_", "DATADOG_",
	"NETLIFY_", "VERCEL_", "HEROKU_",
	"cloudflare_", "CLOUDFLARE_",
	"DATABASE_URL", "DB_PASSWORD", "DB_PASS", "POSTGRES_PASSWORD", "MYSQL_PASSWORD",
	"REDIS_PASSWORD", "MONGO_PASSWORD", "MONGODB_URI",
	"JFROG_", "NPM_TOKEN", "NEXUS_",
	"VAULT_", "CONSUL_", "ETCD_",
}

// sensitiveEnvPatterns are regex patterns for specific sensitive key names.
var sensitiveEnvPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret)\d*$`),
	regexp.MustCompile(`(?i)(key|token|cred|credential)\d*$`),
	regexp.MustCompile(`(?i).*_(key|secret|token|password|cred)$`),
	regexp.MustCompile(`(?i)^x[-_]?api[-_]?key$`),
}

// IsSensitive reports whether an environment variable name is considered sensitive
// and should not be passed to worker processes by default.
func IsSensitive(key string) bool {
	upper := strings.ToUpper(key)

	// Check exact prefix matches.
	for _, prefix := range sensitiveEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}

	// Check against regex patterns.
	for _, pat := range sensitiveEnvPatterns {
		if pat.MatchString(key) {
			return true
		}
	}

	return false
}

// FilterSensitive removes sensitive environment variables from the input map,
// returning a filtered copy. Protected variables (CLAUDECODE, GATEWAY_*) are
// always stripped regardless of IsSensitive result.
var protectedVars = map[string]bool{
	"CLAUDECODE":    true,
	"GATEWAY_ADDR":  true,
	"GATEWAY_TOKEN": true,
}

// BuildWorkerEnv constructs a safe environment for worker processes.
// It strips protected variables (CLAUDECODE, etc.) and sensitive variables
// unless explicitly allowed via the whitelist.
func BuildWorkerEnv(input map[string]string, whitelist []string) map[string]string {
	if input == nil {
		input = make(map[string]string)
	}

	whitelistSet := make(map[string]bool)
	for _, k := range whitelist {
		whitelistSet[k] = true
	}

	result := make(map[string]string)
	for k, v := range input {
		upper := strings.ToUpper(k)

		// Always strip protected variables.
		if protectedVars[upper] {
			continue
		}

		// Allow if explicitly whitelisted.
		if whitelistSet[k] || whitelistSet[upper] {
			result[k] = v
			continue
		}

		// Strip if sensitive.
		if IsSensitive(k) {
			result[k] = "[REDACTED]"
			continue
		}

		result[k] = v
	}

	return result
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
