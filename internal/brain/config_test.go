package brain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAndValidate_Defaults(t *testing.T) {
	cfg, errs := LoadAndValidate()
	require.Empty(t, errs, "defaults should produce no validation errors")

	assert.Equal(t, 30, cfg.Model.TimeoutS)
	assert.Equal(t, true, cfg.Cache.Enabled)
	assert.Equal(t, 1000, cfg.Cache.Size)
	assert.Equal(t, true, cfg.Retry.Enabled)
	assert.Equal(t, 3, cfg.Retry.MaxAttempts)
	assert.Equal(t, 100, cfg.Retry.MinWaitMs)
	assert.Equal(t, 5000, cfg.Retry.MaxWaitMs)
	assert.Equal(t, true, cfg.Metrics.Enabled)
	assert.Equal(t, "hotplex-brain", cfg.Metrics.ServiceName)
	assert.Equal(t, 10*time.Second, cfg.Metrics.ExportInterval)
	assert.Equal(t, true, cfg.Cost.Enabled)
	assert.Equal(t, false, cfg.Cost.EnableBudget)
	assert.Equal(t, false, cfg.RateLimit.Enabled)
	assert.Equal(t, 10.0, cfg.RateLimit.RPS)
	assert.Equal(t, 20, cfg.RateLimit.Burst)
	assert.Equal(t, 100, cfg.RateLimit.QueueSize)
	assert.Equal(t, 30*time.Second, cfg.RateLimit.QueueTimeout)
	assert.Equal(t, false, cfg.RateLimit.PerModel)
	assert.Equal(t, false, cfg.Router.Enabled)
	assert.Equal(t, "cost_priority", cfg.Router.DefaultStage)
	assert.Nil(t, cfg.Router.Models)
	assert.Equal(t, false, cfg.CircuitBreaker.Enabled)
	assert.Equal(t, 5, cfg.CircuitBreaker.MaxFailures)
	assert.Equal(t, 30*time.Second, cfg.CircuitBreaker.Timeout)
	assert.Equal(t, 60*time.Second, cfg.CircuitBreaker.Interval)
	assert.Equal(t, true, cfg.IntentRouter.Enabled)
	assert.Equal(t, 0.7, cfg.IntentRouter.ConfidenceThreshold)
	assert.Equal(t, 1000, cfg.IntentRouter.CacheSize)
	assert.Equal(t, true, cfg.Memory.Enabled)
	assert.Equal(t, 8000, cfg.Memory.TokenThreshold)
	assert.Equal(t, 2000, cfg.Memory.TargetTokenCount)
	assert.Equal(t, 5, cfg.Memory.PreserveTurns)
	assert.Equal(t, 500, cfg.Memory.MaxSummaryTokens)
	assert.Equal(t, 0.25, cfg.Memory.CompressionRatio)
	assert.Equal(t, "24h", cfg.Memory.SessionTTL)
	assert.Equal(t, true, cfg.Guard.Enabled)
	assert.Equal(t, true, cfg.Guard.InputGuardEnabled)
	assert.Equal(t, true, cfg.Guard.OutputGuardEnabled)
	assert.Equal(t, false, cfg.Guard.Chat2ConfigEnabled)
	assert.Equal(t, 100000, cfg.Guard.MaxInputLength)
	assert.Equal(t, 3, cfg.Guard.ScanDepth)
	assert.Equal(t, "medium", cfg.Guard.Sensitivity)
	assert.Nil(t, cfg.Guard.AdminUsers)
	assert.Nil(t, cfg.Guard.AdminChannels)
	assert.Equal(t, 10*time.Second, cfg.Guard.ResponseTimeout)
	assert.Equal(t, 10.0, cfg.Guard.RateLimitRPS)
	assert.Equal(t, 20, cfg.Guard.RateLimitBurst)
	assert.Equal(t, false, cfg.Guard.FailClosedOnBrainError)
}

func TestLoadAndValidate_EnvOverrides(t *testing.T) {

	envSets := map[string]string{
		"HOTPLEX_BRAIN_TIMEOUT_S":                        "60",
		"HOTPLEX_BRAIN_CACHE_SIZE":                       "500",
		"HOTPLEX_BRAIN_MAX_RETRIES":                      "5",
		"HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS":                "200",
		"HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS":                "10000",
		"HOTPLEX_BRAIN_METRICS_ENABLED":                  "false",
		"HOTPLEX_BRAIN_METRICS_SERVICE_NAME":             "test-svc",
		"HOTPLEX_BRAIN_METRICS_EXPORT_INTERVAL":          "30s",
		"HOTPLEX_BRAIN_COST_TRACKING_ENABLED":            "false",
		"HOTPLEX_BRAIN_COST_ENABLE_BUDGET":               "true",
		"HOTPLEX_BRAIN_RATE_LIMIT_ENABLED":               "true",
		"HOTPLEX_BRAIN_RATE_LIMIT_RPS":                   "50.5",
		"HOTPLEX_BRAIN_RATE_LIMIT_BURST":                 "100",
		"HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE":            "200",
		"HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT":         "1m",
		"HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL":             "true",
		"HOTPLEX_BRAIN_ROUTER_ENABLED":                   "true",
		"HOTPLEX_BRAIN_ROUTER_STRATEGY":                  "latency_priority",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED":          "true",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES":     "10",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT":          "45s",
		"HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL":         "2m",
		"HOTPLEX_BRAIN_INTENT_ROUTER_ENABLED":            "false",
		"HOTPLEX_BRAIN_INTENT_ROUTER_CONFIDENCE":         "0.9",
		"HOTPLEX_BRAIN_INTENT_ROUTER_CACHE_SIZE":         "500",
		"HOTPLEX_BRAIN_MEMORY_ENABLED":                   "false",
		"HOTPLEX_BRAIN_MEMORY_TOKEN_THRESHOLD":           "16000",
		"HOTPLEX_BRAIN_MEMORY_TARGET_TOKENS":             "4000",
		"HOTPLEX_BRAIN_MEMORY_PRESERVE_TURNS":            "10",
		"HOTPLEX_BRAIN_MEMORY_MAX_SUMMARY_TOKENS":        "1000",
		"HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO":         "0.5",
		"HOTPLEX_BRAIN_MEMORY_SESSION_TTL":               "48h",
		"HOTPLEX_BRAIN_GUARD_ENABLED":                    "false",
		"HOTPLEX_BRAIN_GUARD_INPUT_ENABLED":              "false",
		"HOTPLEX_BRAIN_GUARD_OUTPUT_ENABLED":             "false",
		"HOTPLEX_BRAIN_CHAT2CONFIG_ENABLED":              "true",
		"HOTPLEX_BRAIN_GUARD_MAX_INPUT_LENGTH":           "50000",
		"HOTPLEX_BRAIN_GUARD_SCAN_DEPTH":                 "5",
		"HOTPLEX_BRAIN_GUARD_SENSITIVITY":                "high",
		"HOTPLEX_BRAIN_ADMIN_USERS":                      "u1,u2",
		"HOTPLEX_BRAIN_ADMIN_CHANNELS":                   "c1,c2",
		"HOTPLEX_BRAIN_GUARD_RESPONSE_TIMEOUT":           "20s",
		"HOTPLEX_BRAIN_GUARD_RATE_LIMIT_RPS":             "5.0",
		"HOTPLEX_BRAIN_GUARD_RATE_LIMIT_BURST":           "10",
		"HOTPLEX_BRAIN_GUARD_FAIL_CLOSED_ON_BRAIN_ERROR": "true",
	}
	for k, v := range envSets {
		t.Setenv(k, v)
	}

	cfg, errs := LoadAndValidate()
	require.Empty(t, errs)

	assert.Equal(t, 60, cfg.Model.TimeoutS)
	assert.Equal(t, 500, cfg.Cache.Size)
	assert.Equal(t, 5, cfg.Retry.MaxAttempts)
	assert.Equal(t, 200, cfg.Retry.MinWaitMs)
	assert.Equal(t, 10000, cfg.Retry.MaxWaitMs)
	assert.False(t, cfg.Metrics.Enabled)
	assert.Equal(t, "test-svc", cfg.Metrics.ServiceName)
	assert.Equal(t, 30*time.Second, cfg.Metrics.ExportInterval)
	assert.False(t, cfg.Cost.Enabled)
	assert.True(t, cfg.Cost.EnableBudget)
	assert.True(t, cfg.RateLimit.Enabled)
	assert.Equal(t, 50.5, cfg.RateLimit.RPS)
	assert.Equal(t, 100, cfg.RateLimit.Burst)
	assert.Equal(t, 200, cfg.RateLimit.QueueSize)
	assert.Equal(t, time.Minute, cfg.RateLimit.QueueTimeout)
	assert.True(t, cfg.RateLimit.PerModel)
	assert.True(t, cfg.Router.Enabled)
	assert.Equal(t, "latency_priority", cfg.Router.DefaultStage)
	assert.True(t, cfg.CircuitBreaker.Enabled)
	assert.Equal(t, 10, cfg.CircuitBreaker.MaxFailures)
	assert.Equal(t, 45*time.Second, cfg.CircuitBreaker.Timeout)
	assert.Equal(t, 2*time.Minute, cfg.CircuitBreaker.Interval)
	assert.False(t, cfg.IntentRouter.Enabled)
	assert.Equal(t, 0.9, cfg.IntentRouter.ConfidenceThreshold)
	assert.Equal(t, 500, cfg.IntentRouter.CacheSize)
	assert.False(t, cfg.Memory.Enabled)
	assert.Equal(t, 16000, cfg.Memory.TokenThreshold)
	assert.Equal(t, 4000, cfg.Memory.TargetTokenCount)
	assert.Equal(t, 10, cfg.Memory.PreserveTurns)
	assert.Equal(t, 1000, cfg.Memory.MaxSummaryTokens)
	assert.Equal(t, 0.5, cfg.Memory.CompressionRatio)
	assert.Equal(t, "48h", cfg.Memory.SessionTTL)
	assert.False(t, cfg.Guard.Enabled)
	assert.False(t, cfg.Guard.InputGuardEnabled)
	assert.False(t, cfg.Guard.OutputGuardEnabled)
	assert.True(t, cfg.Guard.Chat2ConfigEnabled)
	assert.Equal(t, 50000, cfg.Guard.MaxInputLength)
	assert.Equal(t, 5, cfg.Guard.ScanDepth)
	assert.Equal(t, "high", cfg.Guard.Sensitivity)
	assert.Equal(t, []string{"u1", "u2"}, cfg.Guard.AdminUsers)
	assert.Equal(t, []string{"c1", "c2"}, cfg.Guard.AdminChannels)
	assert.Equal(t, 20*time.Second, cfg.Guard.ResponseTimeout)
	assert.Equal(t, 5.0, cfg.Guard.RateLimitRPS)
	assert.Equal(t, 10, cfg.Guard.RateLimitBurst)
	assert.True(t, cfg.Guard.FailClosedOnBrainError)
}

func TestLoadAndValidate_InvalidValuesFallBack(t *testing.T) {

	t.Setenv("HOTPLEX_BRAIN_TIMEOUT_S", "-1")
	t.Setenv("HOTPLEX_BRAIN_CACHE_SIZE", "abc")
	t.Setenv("HOTPLEX_BRAIN_INTENT_ROUTER_CONFIDENCE", "5.0")
	t.Setenv("HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO", "1.5")
	t.Setenv("HOTPLEX_BRAIN_GUARD_SENSITIVITY", "invalid")
	t.Setenv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES", "0")

	cfg, errs := LoadAndValidate()

	require.Len(t, errs, 6, "should report one error per invalid env var")

	assert.Equal(t, 30, cfg.Model.TimeoutS, "negative timeout should fall back to default 30")
	assert.Equal(t, 1000, cfg.Cache.Size, "non-integer should fall back to default 1000")
	assert.Equal(t, 0.7, cfg.IntentRouter.ConfidenceThreshold, "out-of-range confidence should fall back")
	assert.Equal(t, 0.25, cfg.Memory.CompressionRatio, "out-of-range ratio should fall back")
	assert.Equal(t, "medium", cfg.Guard.Sensitivity, "invalid sensitivity should fall back")
	assert.Equal(t, 5, cfg.CircuitBreaker.MaxFailures, "zero max failures should fall back")
}

func TestLoadAndValidate_DurationParsing(t *testing.T) {

	t.Setenv("HOTPLEX_BRAIN_GUARD_RESPONSE_TIMEOUT", "15")
	cfg, errs := LoadAndValidate()
	require.Empty(t, errs)
	assert.Equal(t, 15*time.Second, cfg.Guard.ResponseTimeout, "bare seconds should parse as seconds")
}

func TestConfigRegistry_Coverage(t *testing.T) {

	// Verify every spec has a non-empty name and env key.
	for _, spec := range configRegistry {
		assert.NotEmpty(t, spec.Name, "spec should have a name")
		assert.NotEmpty(t, spec.EnvKey, "spec %s should have an env key", spec.Name)
		if spec.Validate != nil {
			assert.NoError(t, spec.Validate(spec.Default),
				"spec %s: default %q should pass validation", spec.Name, spec.Default)
		}
	}
}

func TestValidationHelpers(t *testing.T) {

	assert.NoError(t, positiveInt("1"))
	assert.Error(t, positiveInt("0"))
	assert.Error(t, positiveInt("-1"))
	assert.Error(t, positiveInt("abc"))

	assert.NoError(t, nonNegativeInt("0"))
	assert.NoError(t, nonNegativeInt("5"))
	assert.Error(t, nonNegativeInt("-1"))

	assert.NoError(t, nonNegativeFloat("0"))
	assert.NoError(t, nonNegativeFloat("3.14"))
	assert.Error(t, nonNegativeFloat("-0.1"))

	assert.NoError(t, positiveDuration("1s"))
	assert.NoError(t, positiveDuration("1"))
	assert.Error(t, positiveDuration("0"))
	assert.Error(t, positiveDuration("0s"))

	assert.NoError(t, confidenceRange("0"))
	assert.NoError(t, confidenceRange("0.5"))
	assert.NoError(t, confidenceRange("1"))
	assert.Error(t, confidenceRange("-0.1"))
	assert.Error(t, confidenceRange("1.1"))

	assert.NoError(t, compressionRatioRange("0.1"))
	assert.NoError(t, compressionRatioRange("0.9"))
	assert.Error(t, compressionRatioRange("0"))
	assert.Error(t, compressionRatioRange("1"))

	assert.NoError(t, sensitivityLevel("low"))
	assert.NoError(t, sensitivityLevel("medium"))
	assert.NoError(t, sensitivityLevel("high"))
	assert.Error(t, sensitivityLevel("extreme"))
}
