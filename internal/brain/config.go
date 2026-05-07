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
	Enabled            bool          // Enable safety guard
	InputGuardEnabled  bool          // Enable input validation
	OutputGuardEnabled bool          // Enable output sanitization
	Chat2ConfigEnabled bool          // Enable natural language config changes (security risk)
	MaxInputLength     int           // Maximum input length
	ScanDepth          int           // Depth for nested context scanning
	Sensitivity        string        // Detection sensitivity: "low", "medium", "high"
	AdminUsers         []string      // User IDs with admin privileges
	AdminChannels      []string      // Channel IDs with admin privileges
	ResponseTimeout    time.Duration // Timeout for Brain API calls
	RateLimitRPS       float64       // Requests per second per user (0 = disabled)
	RateLimitBurst     int           // Burst capacity per user
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
func buildConfig(apiKey, provider, protocol, model, endpoint string) Config {
	return Config{
		Enabled: apiKey != "",
		Model: ModelConfig{
			Provider: provider,
			Protocol: protocol,
			APIKey:   apiKey,
			Model:    model,
			Endpoint: endpoint,
			TimeoutS: getIntEnv("HOTPLEX_BRAIN_TIMEOUT_S", 30),
		},
		Cache: CacheConfig{
			Enabled: true,
			Size:    getIntEnv("HOTPLEX_BRAIN_CACHE_SIZE", 1000),
		},
		Retry: RetryConfig{
			Enabled:     true,
			MaxAttempts: getIntEnv("HOTPLEX_BRAIN_MAX_RETRIES", 3),
			MinWaitMs:   getIntEnv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", 100),
			MaxWaitMs:   getIntEnv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", 5000),
		},
		Metrics: MetricsConfig{
			Enabled:        getBoolEnv("HOTPLEX_BRAIN_METRICS_ENABLED", true),
			ServiceName:    getEnv("HOTPLEX_BRAIN_METRICS_SERVICE_NAME", "hotplex-brain"),
			ExportInterval: getDurationEnv("HOTPLEX_BRAIN_METRICS_EXPORT_INTERVAL", 10*time.Second),
		},
		Cost: CostConfig{
			Enabled:      getBoolEnv("HOTPLEX_BRAIN_COST_TRACKING_ENABLED", true),
			EnableBudget: getBoolEnv("HOTPLEX_BRAIN_COST_ENABLE_BUDGET", false),
		},
		RateLimit: RateLimitConfig{
			Enabled:      getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_ENABLED", false),
			RPS:          getFloatEnv("HOTPLEX_BRAIN_RATE_LIMIT_RPS", 10.0),
			Burst:        getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_BURST", 20),
			QueueSize:    getIntEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_SIZE", 100),
			QueueTimeout: getDurationEnv("HOTPLEX_BRAIN_RATE_LIMIT_QUEUE_TIMEOUT", 30*time.Second),
			PerModel:     getBoolEnv("HOTPLEX_BRAIN_RATE_LIMIT_PER_MODEL", false),
		},
		Router: RouterConfig{
			Enabled:      getBoolEnv("HOTPLEX_BRAIN_ROUTER_ENABLED", false),
			DefaultStage: getEnv("HOTPLEX_BRAIN_ROUTER_STRATEGY", "cost_priority"),
			Models:       parseRouterModels(getEnv("HOTPLEX_BRAIN_ROUTER_MODELS", "")),
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:     getBoolEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_ENABLED", false),
			MaxFailures: getIntEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_MAX_FAILURES", 5),
			Timeout:     getDurationEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_TIMEOUT", 30*time.Second),
			Interval:    getDurationEnv("HOTPLEX_BRAIN_CIRCUIT_BREAKER_INTERVAL", 60*time.Second),
		},
		IntentRouter: IntentRouterFeatureConfig{
			Enabled:             getBoolEnv("HOTPLEX_BRAIN_INTENT_ROUTER_ENABLED", true),
			ConfidenceThreshold: getFloatEnv("HOTPLEX_BRAIN_INTENT_ROUTER_CONFIDENCE", 0.7),
			CacheSize:           getIntEnv("HOTPLEX_BRAIN_INTENT_ROUTER_CACHE_SIZE", 1000),
		},
		Memory: MemoryCompressionConfig{
			Enabled:          getBoolEnv("HOTPLEX_BRAIN_MEMORY_ENABLED", true),
			TokenThreshold:   getIntEnv("HOTPLEX_BRAIN_MEMORY_TOKEN_THRESHOLD", 8000),
			TargetTokenCount: getIntEnv("HOTPLEX_BRAIN_MEMORY_TARGET_TOKENS", 2000),
			PreserveTurns:    getIntEnv("HOTPLEX_BRAIN_MEMORY_PRESERVE_TURNS", 5),
			MaxSummaryTokens: getIntEnv("HOTPLEX_BRAIN_MEMORY_MAX_SUMMARY_TOKENS", 500),
			CompressionRatio: getFloatEnv("HOTPLEX_BRAIN_MEMORY_COMPRESSION_RATIO", 0.25),
			SessionTTL:       getEnv("HOTPLEX_BRAIN_MEMORY_SESSION_TTL", "24h"),
		},
		Guard: SafetyGuardFeatureConfig{
			Enabled:            getBoolEnv("HOTPLEX_BRAIN_GUARD_ENABLED", true),
			InputGuardEnabled:  getBoolEnv("HOTPLEX_BRAIN_GUARD_INPUT_ENABLED", true),
			OutputGuardEnabled: getBoolEnv("HOTPLEX_BRAIN_GUARD_OUTPUT_ENABLED", true),
			Chat2ConfigEnabled: getBoolEnv("HOTPLEX_BRAIN_CHAT2CONFIG_ENABLED", false),
			MaxInputLength:     getIntEnv("HOTPLEX_BRAIN_GUARD_MAX_INPUT_LENGTH", 100000),
			ScanDepth:          getIntEnv("HOTPLEX_BRAIN_GUARD_SCAN_DEPTH", 3),
			Sensitivity:        getEnv("HOTPLEX_BRAIN_GUARD_SENSITIVITY", "medium"),
			AdminUsers:         parseStringList(getEnv("HOTPLEX_BRAIN_ADMIN_USERS", "")),
			AdminChannels:      parseStringList(getEnv("HOTPLEX_BRAIN_ADMIN_CHANNELS", "")),
			ResponseTimeout:    getDurationEnv("HOTPLEX_BRAIN_GUARD_RESPONSE_TIMEOUT", 10*time.Second),
			RateLimitRPS:       getFloatEnv("HOTPLEX_BRAIN_GUARD_RATE_LIMIT_RPS", 10.0),
			RateLimitBurst:     getIntEnv("HOTPLEX_BRAIN_GUARD_RATE_LIMIT_BURST", 20),
		},
	}
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

// Helper functions for loading config from environment variables

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
		// Try parsing as duration string (e.g., "30s", "1m")
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		// Try parsing as seconds
		if n, err := strconv.Atoi(val); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return fallback
}
