// Package brain provides intelligent orchestration capabilities for HotPlex.
// This file (config.go) defines configuration structures loaded from environment variables.
//
// # Configuration Hierarchy
//
//	Config (root)
//	├── Model (LLM backend settings)
//	├── Cache (response caching)
//	├── Retry (retry policy)
//	├── Metrics (observability)
//	├── Cost (cost tracking)
//	├── RateLimit (throttling)
//	├── Router (model routing)
//	├── CircuitBreaker (fault tolerance)
//	├── Failover (provider failover)
//	├── Budget (budget limits)
//	├── Priority (request prioritization)
//	├── IntentRouter (message classification)
//	├── Memory (context compression)
//	└── Guard (safety guardrails)
//
// # Environment Variables
//
// All config is loaded from environment variables with prefix HOTPLEX_BRAIN_.
// See LoadConfigFromEnv() for the full list of variables.
package brain

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// === Model Configuration ===

// ModelConfig configures the LLM backend for Brain operations.
type ModelConfig struct {
	Provider string // LLM provider identifier (e.g., "openai", "anthropic", "siliconflow")
	Protocol string // Protocol to use: "openai" or "anthropic"
	APIKey   string // API Key for the provider
	Model    string // Model name: "gpt-4o", "claude-3-7-sonnet", etc.
	Endpoint string // Custom API endpoint (optional)
	TimeoutS int    // Request timeout in seconds
}

// === Cache Configuration ===

// CacheConfig configures response caching for repeated queries.
type CacheConfig struct {
	Enabled bool // Enable response caching
	Size    int  // Maximum cache entries
}

// === Retry Configuration ===

// RetryConfig configures retry behavior for transient failures.
type RetryConfig struct {
	Enabled     bool // Enable retry mechanism
	MaxAttempts int  // Maximum retry attempts
	MinWaitMs   int  // Minimum wait between retries (milliseconds)
	MaxWaitMs   int  // Maximum wait between retries (milliseconds)
}

// === Metrics Configuration ===

// MetricsConfig configures observability and metrics export.
type MetricsConfig struct {
	Enabled        bool          // Enable metrics collection
	ServiceName    string        // Service name for metrics identification
	Endpoint       string        // Metrics export endpoint (e.g., OTLP collector)
	ExportInterval time.Duration // Interval for periodic metric export
}

// === Cost Configuration ===

// CostConfig configures cost tracking for LLM API calls.
type CostConfig struct {
	Enabled      bool // Enable cost tracking
	EnableBudget bool // Enable budget enforcement
}

// === Rate Limit Configuration ===

// RateLimitConfig configures request rate limiting.
type RateLimitConfig struct {
	Enabled      bool          // Enable rate limiting
	RPS          float64       // Requests per second limit
	Burst        int           // Burst capacity (token bucket)
	QueueSize    int           // Queue size for waiting requests
	QueueTimeout time.Duration // Max wait time in queue
	PerModel     bool          // Apply limit per-model instead of global
}

// === Router Configuration ===

// RouterConfig configures intelligent model routing.
type RouterConfig struct {
	Enabled      bool              // Enable model routing
	DefaultStage string            // Default routing strategy: "cost_priority", "latency_priority"
	Models       []llm.ModelConfig // Available models with cost/latency info
}

// === Circuit Breaker Configuration ===

// CircuitBreakerConfig configures circuit breaker for fault tolerance.
type CircuitBreakerConfig struct {
	Enabled     bool          // Enable circuit breaker
	MaxFailures int           // Failures before opening circuit
	Timeout     time.Duration // Time before attempting to close circuit
	Interval    time.Duration // Interval for resetting failure count
}

// === Intent Router Configuration ===

// IntentRouterFeatureConfig configures intent routing features.
type IntentRouterFeatureConfig struct {
	Enabled             bool    `json:"enabled"`              // Enable intent routing
	ConfidenceThreshold float64 `json:"confidence_threshold"` // Minimum confidence for classification
	CacheSize           int     `json:"cache_size"`           // Cache size for classification results
}

// === Memory Compression Configuration ===

// MemoryCompressionConfig configures context compression.
type MemoryCompressionConfig struct {
	Enabled          bool    // Enable context compression
	TokenThreshold   int     // Trigger compression at this token count
	TargetTokenCount int     // Target tokens after compression
	PreserveTurns    int     // Recent turns to preserve during compression
	MaxSummaryTokens int     // Maximum tokens for summary
	CompressionRatio float64 // Target compression ratio (0.0-1.0)
	SessionTTL       string  // Session time-to-live (e.g., "24h")
}

// === Safety Guard Configuration ===

// SafetyGuardFeatureConfig configures safety guardrails.
type SafetyGuardFeatureConfig struct {
	Enabled                bool          // Enable safety guard
	InputGuardEnabled      bool          // Enable input validation
	OutputGuardEnabled     bool          // Enable output sanitization
	Chat2ConfigEnabled     bool          // Enable natural language config changes (security risk)
	MaxInputLength         int           // Maximum input length
	ScanDepth              int           // Depth for nested context scanning
	Sensitivity            string        // Detection sensitivity: "low", "medium", "high"
	AdminUsers             []string      // User IDs with admin privileges
	AdminChannels          []string      // Channel IDs with admin privileges
	ResponseTimeout        time.Duration // Timeout for Brain API calls
	RateLimitRPS           float64       // Requests per second per user (0 = disabled)
	RateLimitBurst         int           // Burst capacity per user
	FailClosedOnBrainError bool          // Block input when deep analysis fails (e.g. LLM down)
}

// === Main Config ===

// Config holds the configuration for the Global Brain.
// It aggregates all sub-configurations for the Brain system.
//
// # Auto-Enable Logic
//
// Config.Enabled is automatically set based on APIKey presence:
//   - HOTPLEX_BRAIN_API_KEY present → Enabled = true
//   - HOTPLEX_BRAIN_API_KEY absent → Enabled = false
//
// This allows graceful degradation when Brain is not configured.
type Config struct {
	// Enabled is automatically determined based on APIKey presence.
	Enabled bool
	// Model is the model configuration.
	Model ModelConfig
	// Cache is the cache configuration.
	Cache CacheConfig
	// Retry is the retry configuration.
	Retry RetryConfig
	// Metrics is the metrics configuration.
	Metrics MetricsConfig
	// Cost is the cost configuration.
	Cost CostConfig
	// RateLimit is the rate limit configuration.
	RateLimit RateLimitConfig
	// Router is the router configuration.
	Router RouterConfig
	// CircuitBreaker is the circuit breaker configuration.
	CircuitBreaker CircuitBreakerConfig
	// IntentRouter is the intent router feature configuration.
	IntentRouter IntentRouterFeatureConfig
	// Memory is the memory compression feature configuration.
	Memory MemoryCompressionConfig
	// Guard is the safety guard feature configuration.
	Guard SafetyGuardFeatureConfig
}

// LoadConfigFromEnv loads the brain configuration from environment variables.
//
// Resolution order (first non-empty API key wins):
//
//  1. HOTPLEX_BRAIN_API_KEY  — Brain's own dedicated key
//  2. Worker config files    — scan ~/.claude/settings.json then ~/.config/opencode/opencode.json
//  3. System env vars        — scan ANTHROPIC_API_KEY → OPENAI_API_KEY → SILICONFLOW_API_KEY → DEEPSEEK_API_KEY
//  4. Disabled               — no key found, Brain degrades gracefully
func LoadConfigFromEnv() Config {
	// ── 1. Brain's own configuration (HOTPLEX_BRAIN_*) ──
	if apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY"); apiKey != "" {
		provider := getEnv("HOTPLEX_BRAIN_PROVIDER", "openai")
		protocol := getEnv("HOTPLEX_BRAIN_PROTOCOL", protocolForProvider(provider))
		model := getEnv("HOTPLEX_BRAIN_MODEL", defaultModelForProtocol(protocol))
		return buildConfig(apiKey, provider, protocol, model, os.Getenv("HOTPLEX_BRAIN_ENDPOINT"))
	}

	// ── 2. Worker config discovery ──
	if getBoolEnv("HOTPLEX_BRAIN_WORKER_EXTRACT", true) {
		if cfg := extractFromWorker(); cfg != nil {
			return *cfg
		}
	}

	// ── 3. System env vars scan ──
	for _, src := range systemKeySources {
		if apiKey := os.Getenv(src.envKey); apiKey != "" {
			return buildConfig(apiKey, src.provider, src.protocol, src.defaultModel, os.Getenv(src.baseURLEnv))
		}
	}

	// ── 4. No key found — disabled ──
	return buildConfig("", "openai", "openai", "gpt-4o", "")
}

// systemKeySources defines the scan order for system env vars (step 3).
var systemKeySources = []struct {
	envKey, provider, protocol, defaultModel, baseURLEnv string
}{
	{"ANTHROPIC_API_KEY", "anthropic", "anthropic", "claude-3-7-sonnet-latest", "ANTHROPIC_BASE_URL"},
	{"OPENAI_API_KEY", "openai", "openai", "gpt-4o", "OPENAI_BASE_URL"},
	{"SILICONFLOW_API_KEY", "openai", "openai", "deepseek-ai/DeepSeek-V3", "SILICONFLOW_BASE_URL"},
	{"DEEPSEEK_API_KEY", "openai", "openai", "deepseek-chat", "DEEPSEEK_BASE_URL"},
}

// extractors defines the ordered scan list for worker config discovery.
var extractors = []struct {
	name     string
	extract  func() (*ExtractedConfig, error)
	provider string
	protocol string
	defModel string
}{
	{"claude-code", func() (*ExtractedConfig, error) { return NewClaudeCodeExtractor().Extract() },
		"anthropic", "anthropic", "claude-3-7-sonnet-latest"},
	{"opencode", func() (*ExtractedConfig, error) { return NewOpenCodeExtractor().Extract() },
		"openai", "openai", "gpt-4o"},
}

// extractFromWorker scans worker config files in order; first hit wins.
func extractFromWorker() *Config {
	for _, ext := range extractors {
		extracted, err := ext.extract()
		if err != nil || extracted == nil || extracted.APIKey == "" {
			continue
		}

		endpoint := os.Getenv("HOTPLEX_BRAIN_ENDPOINT")
		if endpoint == "" {
			endpoint = extracted.Endpoint
		}
		model := os.Getenv("HOTPLEX_BRAIN_MODEL")
		if model == "" {
			model = extracted.Model
		}
		if model == "" {
			model = ext.defModel
		}

		provider := ext.provider
		protocol := ext.protocol
		// OpenCode uses "provider/model" format — extract provider from prefix.
		if strings.Contains(model, "/") {
			parts := strings.SplitN(model, "/", 2)
			provider = parts[0]
			protocol = provider
		}

		cfg := buildConfig(extracted.APIKey, provider, protocol, model, endpoint)
		return &cfg
	}
	return nil
}

func protocolForProvider(provider string) string {
	if provider == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

func defaultModelForProtocol(protocol string) string {
	if protocol == "anthropic" {
		return "claude-3-7-sonnet-latest"
	}
	return "gpt-4o"
}

// buildConfig constructs a Config from resolved values.
// Sub-config fields are populated via the configRegistry.
func buildConfig(apiKey, provider, protocol, model, endpoint string) Config {
	cfg, _ := LoadAndValidate()
	cfg.Enabled = apiKey != ""
	cfg.Model.Provider = provider
	cfg.Model.Protocol = protocol
	cfg.Model.APIKey = apiKey
	cfg.Model.Model = model
	cfg.Model.Endpoint = endpoint
	return cfg
}

func parseRouterModels(s string) []llm.ModelConfig {
	if s == "" {
		return nil
	}

	var models []llm.ModelConfig
	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fields := strings.Split(part, ":")
		if len(fields) < 5 {
			continue
		}

		costInput, _ := strconv.ParseFloat(fields[2], 64)
		costOutput, _ := strconv.ParseFloat(fields[3], 64)
		latency, _ := strconv.ParseInt(fields[4], 10, 64)

		models = append(models, llm.ModelConfig{
			Name:            fields[0],
			Provider:        fields[1],
			CostPer1KInput:  costInput,
			CostPer1KOutput: costOutput,
			AvgLatencyMs:    latency,
			Enabled:         true,
		})
	}

	return models
}

func parseStringList(s string) []string {
	if s == "" {
		return nil
	}

	var result []string
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// === ConfigSpec Registry ===

// ConfigSpec defines a single environment-driven configuration entry.
// The registry centralizes all env var lookups, defaults, and validation
// so that configuration can be inspected, validated, and tested uniformly.
type ConfigSpec struct {
	Name     string             // config field name, e.g. "cache_size"
	EnvKey   string             // e.g. "HOTPLEX_BRAIN_CACHE_SIZE"
	Default  string             // string representation of default
	Validate func(string) error // optional validator; nil = accept any value
}

// configRegistry enumerates every environment variable used by buildConfig.
// Order matches the struct field order in Config for readability.
var configRegistry = []ConfigSpec{
	// Model
	{Name: "model_timeout_s", EnvKey: "HOTPLEX_BRAIN_TIMEOUT_S", Default: "30",
		Validate: positiveInt},
	// Cache
	{Name: "cache_size", EnvKey: "HOTPLEX_BRAIN_CACHE_SIZE", Default: "1000",
		Validate: positiveInt},
	// Retry
	{Name: "retry_max_attempts", EnvKey: "HOTPLEX_BRAIN_MAX_RETRIES", Default: "3",
		Validate: positiveInt},
	{Name: "retry_min_wait_ms", EnvKey: "HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", Default: "100",
		Validate: nonNegativeInt},
	{Name: "retry_max_wait_ms", EnvKey: "HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", Default: "5000",
		Validate: positiveInt},
	// Metrics
	{Name: "metrics_enabled", EnvKey: "HOTPLEX_BRAIN_METRICS_ENABLED", Default: "true"},
	{Name: "metrics_service_name", EnvKey: "HOTPLEX_BRAIN_METRICS_SERVICE_NAME", Default: "hotplex-brain"},
	{Name: "metrics_export_interval", EnvKey: "HOTPLEX_BRAIN_METRICS_EXPORT_INTERVAL", Default: "10s",
		Validate: positiveDuration},
	// Cost
	{Name: "cost_tracking_enabled", EnvKey: "HOTPLEX_BRAIN_COST_TRACKING_ENABLED", Default: "true"},
	{Name: "cost_enable_budget", EnvKey: "HOTPLEX_BRAIN_COST_ENABLE_BUDGET", Default: "false"},
	// RateLimit
	{Name: "rate_limit_enabled", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_ENABLED", Default: "false"},
	{Name: "rate_limit_rps", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_RPS", Default: "10",
		Validate: nonNegativeFloat},
	{Name: "rate_limit_burst", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_BURST", Default: "20",
		Validate: nonNegativeInt},
	{Name: "rate_limit_queue_size", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE", Default: "100",
		Validate: nonNegativeInt},
	{Name: "rate_limit_queue_timeout", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT", Default: "30s",
		Validate: positiveDuration},
	{Name: "rate_limit_per_model", EnvKey: "HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL", Default: "false"},
	// Router
	{Name: "router_enabled", EnvKey: "HOTPLEX_BRAIN_ROUTER_ENABLED", Default: "false"},
	{Name: "router_strategy", EnvKey: "HOTPLEX_BRAIN_ROUTER_STRATEGY", Default: "cost_priority"},
	{Name: "router_models", EnvKey: "HOTPLEX_BRAIN_ROUTER_MODELS", Default: ""},
	// CircuitBreaker
	{Name: "circuit_breaker_enabled", EnvKey: "HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED", Default: "false"},
	{Name: "circuit_breaker_max_failures", EnvKey: "HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES", Default: "5",
		Validate: positiveInt},
	{Name: "circuit_breaker_timeout", EnvKey: "HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT", Default: "30s",
		Validate: positiveDuration},
	{Name: "circuit_breaker_interval", EnvKey: "HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL", Default: "60s",
		Validate: positiveDuration},
	// IntentRouter
	{Name: "intent_router_enabled", EnvKey: "HOTPLEX_BRAIN_INTENT_ROUTER_ENABLED", Default: "true"},
	{Name: "intent_router_confidence", EnvKey: "HOTPLEX_BRAIN_INTENT_ROUTER_CONFIDENCE", Default: "0.7",
		Validate: confidenceRange},
	{Name: "intent_router_cache_size", EnvKey: "HOTPLEX_BRAIN_INTENT_ROUTER_CACHE_SIZE", Default: "1000",
		Validate: positiveInt},
	// Memory
	{Name: "memory_enabled", EnvKey: "HOTPLEX_BRAIN_MEMORY_ENABLED", Default: "true"},
	{Name: "memory_token_threshold", EnvKey: "HOTPLEX_BRAIN_MEMORY_TOKEN_THRESHOLD", Default: "8000",
		Validate: positiveInt},
	{Name: "memory_target_tokens", EnvKey: "HOTPLEX_BRAIN_MEMORY_TARGET_TOKENS", Default: "2000",
		Validate: positiveInt},
	{Name: "memory_preserve_turns", EnvKey: "HOTPLEX_BRAIN_MEMORY_PRESERVE_TURNS", Default: "5",
		Validate: nonNegativeInt},
	{Name: "memory_max_summary_tokens", EnvKey: "HOTPLEX_BRAIN_MEMORY_MAX_SUMMARY_TOKENS", Default: "500",
		Validate: positiveInt},
	{Name: "memory_compression_ratio", EnvKey: "HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO", Default: "0.25",
		Validate: compressionRatioRange},
	{Name: "memory_session_ttl", EnvKey: "HOTPLEX_BRAIN_MEMORY_SESSION_TTL", Default: "24h"},
	// Guard
	{Name: "guard_enabled", EnvKey: "HOTPLEX_BRAIN_GUARD_ENABLED", Default: "true"},
	{Name: "guard_input_enabled", EnvKey: "HOTPLEX_BRAIN_GUARD_INPUT_ENABLED", Default: "true"},
	{Name: "guard_output_enabled", EnvKey: "HOTPLEX_BRAIN_GUARD_OUTPUT_ENABLED", Default: "true"},
	{Name: "chat2config_enabled", EnvKey: "HOTPLEX_BRAIN_CHAT2CONFIG_ENABLED", Default: "false"},
	{Name: "guard_max_input_length", EnvKey: "HOTPLEX_BRAIN_GUARD_MAX_INPUT_LENGTH", Default: "100000",
		Validate: nonNegativeInt},
	{Name: "guard_scan_depth", EnvKey: "HOTPLEX_BRAIN_GUARD_SCAN_DEPTH", Default: "3",
		Validate: nonNegativeInt},
	{Name: "guard_sensitivity", EnvKey: "HOTPLEX_BRAIN_GUARD_SENSITIVITY", Default: "medium",
		Validate: sensitivityLevel},
	{Name: "guard_response_timeout", EnvKey: "HOTPLEX_BRAIN_GUARD_RESPONSE_TIMEOUT", Default: "10s",
		Validate: positiveDuration},
	{Name: "guard_rate_limit_rps", EnvKey: "HOTPLEX_BRAIN_GUARD_RATE_LIMIT_RPS", Default: "10",
		Validate: nonNegativeFloat},
	{Name: "guard_rate_limit_burst", EnvKey: "HOTPLEX_BRAIN_GUARD_RATE_LIMIT_BURST", Default: "20",
		Validate: nonNegativeInt},
	{Name: "guard_fail_closed_on_brain_error", EnvKey: "HOTPLEX_BRAIN_GUARD_FAIL_CLOSED_ON_BRAIN_ERROR", Default: "false"},
}

// --- Validation helpers ---

func positiveInt(val string) error {
	n, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("invalid integer %q", val)
	}
	if n <= 0 {
		return fmt.Errorf("must be positive, got %d", n)
	}
	return nil
}

func nonNegativeInt(val string) error {
	n, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("invalid integer %q", val)
	}
	if n < 0 {
		return fmt.Errorf("must be non-negative, got %d", n)
	}
	return nil
}

func nonNegativeFloat(val string) error {
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("invalid float %q", val)
	}
	if n < 0 {
		return fmt.Errorf("must be non-negative, got %f", n)
	}
	return nil
}

func positiveDuration(val string) error {
	d, err := parseDuration(val)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", val, err)
	}
	if d <= 0 {
		return fmt.Errorf("must be positive, got %s", d)
	}
	return nil
}

func confidenceRange(val string) error {
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("invalid float %q", val)
	}
	if n < 0 || n > 1 {
		return fmt.Errorf("must be between 0 and 1, got %f", n)
	}
	return nil
}

func compressionRatioRange(val string) error {
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fmt.Errorf("invalid float %q", val)
	}
	if n <= 0 || n >= 1 {
		return fmt.Errorf("must be between 0 and 1 exclusive, got %f", n)
	}
	return nil
}

func sensitivityLevel(val string) error {
	switch val {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("must be low, medium, or high, got %q", val)
	}
}

// --- Parse helpers ---

// parseDuration parses a duration string, accepting both Go duration syntax
// (e.g., "30s", "1m") and bare seconds (e.g., "30").
func parseDuration(val string) (time.Duration, error) {
	if d, err := time.ParseDuration(val); err == nil {
		return d, nil
	}
	if n, err := strconv.Atoi(val); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration %q", val)
}

// ResolveEnv looks up the env var for a spec, falling back to Default.
// If validation fails it returns the default value.
func (s ConfigSpec) ResolveEnv() string {
	val := os.Getenv(s.EnvKey)
	if val == "" {
		return s.Default
	}
	if s.Validate != nil {
		if err := s.Validate(val); err != nil {
			return s.Default
		}
	}
	return val
}

// LoadAndValidate loads brain config from environment and returns the config
// along with any validation errors. Errors are non-fatal — the config uses
// defaults for invalid values and reports all issues.
func LoadAndValidate() (Config, []error) {
	// Resolve all env vars through the registry, collecting validation errors.
	values := make(map[string]string, len(configRegistry))
	var errs []error
	for _, spec := range configRegistry {
		raw := os.Getenv(spec.EnvKey)
		if raw == "" {
			values[spec.Name] = spec.Default
			continue
		}
		if spec.Validate != nil {
			if err := spec.Validate(raw); err != nil {
				errs = append(errs, fmt.Errorf("%s (%s): %w", spec.Name, spec.EnvKey, err))
				values[spec.Name] = spec.Default // fall back to default on validation error
				continue
			}
		}
		values[spec.Name] = raw
	}

	cfg := Config{
		Cache: CacheConfig{Enabled: true},
		Retry: RetryConfig{Enabled: true},
	}

	cfg.Model.TimeoutS = getInt(values["model_timeout_s"])
	cfg.Cache.Size = getInt(values["cache_size"])
	cfg.Retry.MaxAttempts = getInt(values["retry_max_attempts"])
	cfg.Retry.MinWaitMs = getInt(values["retry_min_wait_ms"])
	cfg.Retry.MaxWaitMs = getInt(values["retry_max_wait_ms"])

	cfg.Metrics.Enabled = getBool(values["metrics_enabled"])
	cfg.Metrics.ServiceName = values["metrics_service_name"]
	cfg.Metrics.ExportInterval = getDuration(values["metrics_export_interval"])

	cfg.Cost.Enabled = getBool(values["cost_tracking_enabled"])
	cfg.Cost.EnableBudget = getBool(values["cost_enable_budget"])

	cfg.RateLimit.Enabled = getBool(values["rate_limit_enabled"])
	cfg.RateLimit.RPS = getFloat(values["rate_limit_rps"])
	cfg.RateLimit.Burst = getInt(values["rate_limit_burst"])
	cfg.RateLimit.QueueSize = getInt(values["rate_limit_queue_size"])
	cfg.RateLimit.QueueTimeout = getDuration(values["rate_limit_queue_timeout"])
	cfg.RateLimit.PerModel = getBool(values["rate_limit_per_model"])

	cfg.Router.Enabled = getBool(values["router_enabled"])
	cfg.Router.DefaultStage = values["router_strategy"]
	cfg.Router.Models = parseRouterModels(values["router_models"])

	cfg.CircuitBreaker.Enabled = getBool(values["circuit_breaker_enabled"])
	cfg.CircuitBreaker.MaxFailures = getInt(values["circuit_breaker_max_failures"])
	cfg.CircuitBreaker.Timeout = getDuration(values["circuit_breaker_timeout"])
	cfg.CircuitBreaker.Interval = getDuration(values["circuit_breaker_interval"])

	cfg.IntentRouter.Enabled = getBool(values["intent_router_enabled"])
	cfg.IntentRouter.ConfidenceThreshold = getFloat(values["intent_router_confidence"])
	cfg.IntentRouter.CacheSize = getInt(values["intent_router_cache_size"])

	cfg.Memory.Enabled = getBool(values["memory_enabled"])
	cfg.Memory.TokenThreshold = getInt(values["memory_token_threshold"])
	cfg.Memory.TargetTokenCount = getInt(values["memory_target_tokens"])
	cfg.Memory.PreserveTurns = getInt(values["memory_preserve_turns"])
	cfg.Memory.MaxSummaryTokens = getInt(values["memory_max_summary_tokens"])
	cfg.Memory.CompressionRatio = getFloat(values["memory_compression_ratio"])
	cfg.Memory.SessionTTL = values["memory_session_ttl"]

	cfg.Guard.Enabled = getBool(values["guard_enabled"])
	cfg.Guard.InputGuardEnabled = getBool(values["guard_input_enabled"])
	cfg.Guard.OutputGuardEnabled = getBool(values["guard_output_enabled"])
	cfg.Guard.Chat2ConfigEnabled = getBool(values["chat2config_enabled"])
	cfg.Guard.MaxInputLength = getInt(values["guard_max_input_length"])
	cfg.Guard.ScanDepth = getInt(values["guard_scan_depth"])
	cfg.Guard.Sensitivity = values["guard_sensitivity"]
	cfg.Guard.AdminUsers = parseStringList(os.Getenv("HOTPLEX_BRAIN_ADMIN_USERS"))
	cfg.Guard.AdminChannels = parseStringList(os.Getenv("HOTPLEX_BRAIN_ADMIN_CHANNELS"))
	cfg.Guard.ResponseTimeout = getDuration(values["guard_response_timeout"])
	cfg.Guard.RateLimitRPS = getFloat(values["guard_rate_limit_rps"])
	cfg.Guard.RateLimitBurst = getInt(values["guard_rate_limit_burst"])
	cfg.Guard.FailClosedOnBrainError = getBool(values["guard_fail_closed_on_brain_error"])

	return cfg, errs
}

// --- Value parse helpers (string → typed) ---

func getBool(val string) bool {
	b, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return b
}

func getInt(val string) int {
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

func getFloat(val string) float64 {
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0
	}
	return n
}

func getDuration(val string) time.Duration {
	d, err := parseDuration(val)
	if err != nil {
		return 0
	}
	return d
}

// --- Legacy env helpers (kept for backward compat with LoadConfigFromEnv) ---

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if val := os.Getenv(key); val != "" {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}

func getFloatEnv(key string, fallback float64) float64 {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := parseDuration(val); err == nil {
			return d
		}
	}
	return fallback
}
