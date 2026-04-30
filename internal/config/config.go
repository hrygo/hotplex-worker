package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var envVarRe = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// ExpandEnv expands ${VAR} and ${VAR:-default} references in a config value
// using os.Getenv.  This is used to reference secrets (or other values) from
// environment variables within config file values, e.g.:
//
//	db_password: "${DB_PASSWORD:-}"
//
// Unset variables without defaults are left as the literal string.
func ExpandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		key := parts[1]
		val := os.Getenv(key)
		if val == "" && len(parts) >= 3 {
			val = parts[2]
		}
		return val
	})
}

// SecretsProvider abstracts how secrets are retrieved.
type SecretsProvider interface {
	// Get returns the secret value for the given key, or "" if not found.
	Get(key string) string
}

// EnvSecretsProvider retrieves secrets from environment variables.
type EnvSecretsProvider struct{}

func NewEnvSecretsProvider() *EnvSecretsProvider { return &EnvSecretsProvider{} }

func (p *EnvSecretsProvider) Get(key string) string { return os.Getenv(key) }

// ChainedSecretsProvider tries providers in order until a value is found.
type ChainedSecretsProvider struct {
	providers []SecretsProvider
}

func NewChainedSecretsProvider(providers ...SecretsProvider) *ChainedSecretsProvider {
	return &ChainedSecretsProvider{providers: providers}
}

func (p *ChainedSecretsProvider) Get(key string) string {
	for _, pr := range p.providers {
		if val := pr.Get(key); val != "" {
			return val
		}
	}
	return ""
}

// Validate checks that all required configuration fields are set.
// Sensitive fields (JWTSecret) are validated separately via RequireSecrets.
func (c *Config) Validate() []string {
	var errs []string

	if c.Gateway.Addr == "" {
		errs = append(errs, "gateway.addr is required (or use default :8080)")
	}
	if c.DB.Path == "" {
		errs = append(errs, "db.path is required (or use default hotplex.db)")
	}
	if c.Session.RetentionPeriod <= 0 {
		errs = append(errs, "session.retention_period must be positive")
	}
	if c.Pool.MaxSize <= 0 {
		errs = append(errs, "pool.max_size must be positive")
	}
	// Warn (not error) for TLS on non-local address.
	if !c.Security.TLSEnabled &&
		!strings.Contains(c.Gateway.Addr, "localhost") &&
		!strings.Contains(c.Gateway.Addr, "127.0.0.1") &&
		!strings.Contains(c.Gateway.Addr, "[::1]") {
		errs = append(errs, "TLS is disabled on non-local address; enable tls_enabled for production")
	}
	if c.Log.Format != "" && c.Log.Format != "json" && c.Log.Format != "text" {
		errs = append(errs, "log.format must be either 'json' or 'text'")
	}

	return errs
}

// RequireSecrets validates that all required sensitive fields are present.
// Returns an error listing any missing secrets. Call after Load.
func (c *Config) RequireSecrets() error {
	var missing []string
	if len(c.Security.JWTSecret) == 0 {
		missing = append(missing, "security.jwt_secret")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required secrets: %s (set via config file or HOTPLEX_JWT_SECRET env var)", strings.Join(missing, ", "))
	}
	return nil
}

// ─── Config structs ───────────────────────────────────────────────────────────

// Config holds all gateway configuration.
type Config struct {
	Gateway     GatewayConfig   `mapstructure:"gateway"`
	DB          DBConfig        `mapstructure:"db"`
	Worker      WorkerConfig    `mapstructure:"worker"`
	Security    SecurityConfig  `mapstructure:"security"`
	Session     SessionConfig   `mapstructure:"session"`
	Pool        PoolConfig      `mapstructure:"pool"`
	Log         LogConfig       `mapstructure:"log"`
	Admin       AdminConfig     `mapstructure:"admin"`
	WebChat     WebChatConfig   `mapstructure:"webchat"`
	Messaging   MessagingConfig `mapstructure:"messaging"`
	AgentConfig AgentConfig     `mapstructure:"agent_config"`
	Inherits    string          `mapstructure:"inherits"` // path to parent config file; "" = no inheritance
}

// MessagingConfig holds messaging platform adapter settings.
type MessagingConfig struct {
	Slack  SlackConfig  `mapstructure:"slack"`
	Feishu FeishuConfig `mapstructure:"feishu"`
}

// STT constants for provider and mode values.
const (
	STTProviderLocal       = "local"
	STTProviderFeishu      = "feishu"
	STTProviderFeishuLocal = "feishu+local"
	STTModeEphemeral       = "ephemeral"
	STTModePersistent      = "persistent"
)

// STTConfig holds speech-to-text configuration shared across messaging adapters.
type STTConfig struct {
	// Provider: "local" (external command), "feishu" (cloud API),
	// "feishu+local" (cloud primary, local fallback), "" (disabled).
	Provider string `mapstructure:"stt_provider"`
	// LocalCmd is the command template. {file} is replaced with the audio file path.
	LocalCmd string `mapstructure:"stt_local_cmd"`
	// LocalMode: "ephemeral" (per-request process) or "persistent" (long-lived subprocess).
	LocalMode string `mapstructure:"stt_local_mode"`
	// LocalIdleTTL controls auto-shutdown of persistent subprocess. 0 = disabled.
	LocalIdleTTL time.Duration `mapstructure:"stt_local_idle_ttl"`
}

// SlackConfig holds Slack Socket Mode adapter settings.
type SlackConfig struct {
	Enabled             bool     `mapstructure:"enabled"`
	BotToken            string   `mapstructure:"bot_token"`
	AppToken            string   `mapstructure:"app_token"`
	SocketMode          bool     `mapstructure:"socket_mode"`
	WorkerType          string   `mapstructure:"worker_type"`
	WorkDir             string   `mapstructure:"work_dir"`
	AssistantAPIEnabled *bool    `mapstructure:"assistant_api_enabled"`
	DMPolicy            string   `mapstructure:"dm_policy"`
	GroupPolicy         string   `mapstructure:"group_policy"`
	RequireMention      bool     `mapstructure:"require_mention"`
	AllowFrom           []string `mapstructure:"allow_from"`
	AllowDMFrom         []string `mapstructure:"allow_dm_from"`
	AllowGroupFrom      []string `mapstructure:"allow_group_from"`

	ReconnectBaseDelay time.Duration `mapstructure:"reconnect_base_delay"`
	ReconnectMaxDelay  time.Duration `mapstructure:"reconnect_max_delay"`

	STTConfig `mapstructure:",squash"`
}

// FeishuConfig holds Feishu WebSocket adapter settings.
type FeishuConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	AppID      string `mapstructure:"app_id"`
	AppSecret  string `mapstructure:"app_secret"`
	WorkerType string `mapstructure:"worker_type"`
	WorkDir    string `mapstructure:"work_dir"`

	DMPolicy       string   `mapstructure:"dm_policy"`
	GroupPolicy    string   `mapstructure:"group_policy"`
	RequireMention bool     `mapstructure:"require_mention"`
	AllowFrom      []string `mapstructure:"allow_from"`
	AllowDMFrom    []string `mapstructure:"allow_dm_from"`
	AllowGroupFrom []string `mapstructure:"allow_group_from"`

	STTConfig `mapstructure:",squash"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
	Enabled            bool                `mapstructure:"enabled"`
	Addr               string              `mapstructure:"addr"`
	Tokens             []string            `mapstructure:"tokens"`
	TokenScopes        map[string][]string `mapstructure:"token_scopes"`
	DefaultScopes      []string            `mapstructure:"default_scopes"`
	IPWhitelistEnabled bool                `mapstructure:"ip_whitelist_enabled"`
	AllowedCIDRs       []string            `mapstructure:"allowed_cidrs"`
	RateLimitEnabled   bool                `mapstructure:"rate_limit_enabled"`
	RequestsPerSec     int                 `mapstructure:"requests_per_sec"`
	Burst              int                 `mapstructure:"burst"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // "json" or "text"
}

// WebChatConfig holds webchat UI address (informational only, gateway does not manage webchat).
type WebChatConfig struct {
	Addr string `mapstructure:"addr"`
}

// GatewayConfig holds WebSocket gateway settings.
type GatewayConfig struct {
	Addr               string        `mapstructure:"addr"`
	ReadBufferSize     int           `mapstructure:"read_buffer_size"`
	WriteBufferSize    int           `mapstructure:"write_buffer_size"`
	PingInterval       time.Duration `mapstructure:"ping_interval"`
	PongTimeout        time.Duration `mapstructure:"pong_timeout"`
	WriteTimeout       time.Duration `mapstructure:"write_timeout"`
	IdleTimeout        time.Duration `mapstructure:"idle_timeout"`
	MaxFrameSize       int64         `mapstructure:"max_frame_size"`
	BroadcastQueueSize int           `mapstructure:"broadcast_queue_size"`

	// PlatformWriteBuffer is the per-conn channel capacity for async platform writes.
	// 64 slots accommodate ~8 batches at 120ms coalesce window, providing ample headroom
	// even under burst conditions without excessive memory overhead.
	PlatformWriteBuffer int `mapstructure:"platform_write_buffer"`
	// PlatformDropThreshold is the fill level at which droppable events (delta/raw)
	// begin being silently dropped. Set to 87.5% of PlatformWriteBuffer to provide
	// backpressure relief while preserving space for guaranteed events.
	PlatformDropThreshold int `mapstructure:"platform_drop_threshold"`
	// DeltaCoalesceInterval is the time window for batching consecutive delta events.
	// 120ms targets Feishu CardKit's 10 updates/sec per-card rate limit (8.3/sec with margin),
	// while keeping first-token latency well under the 200ms human perception threshold.
	// At 100 tok/s input, this yields ~12x API call reduction.
	DeltaCoalesceInterval time.Duration `mapstructure:"delta_coalesce_interval"`
	// DeltaCoalesceSize is the rune threshold for immediate delta flush, serving as a
	// burst safety valve. 200 runes ≈ 40 tokens triggers early flush only during spikes,
	// while average batches at 100 tok/s / 120ms ≈ 48 runes stay well below this threshold.
	DeltaCoalesceSize int `mapstructure:"delta_coalesce_size"`
}

// DBConfig holds SQLite settings.
type DBConfig struct {
	Path            string        `mapstructure:"path"`
	WALMode         bool          `mapstructure:"wal_mode"`
	BusyTimeout     time.Duration `mapstructure:"busy_timeout"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	VacuumThreshold float64       `mapstructure:"vacuum_threshold"`
}

// WorkerConfig holds per-worker defaults.
type WorkerConfig struct {
	MaxLifetime      time.Duration        `mapstructure:"max_lifetime"`
	IdleTimeout      time.Duration        `mapstructure:"idle_timeout"`
	ExecutionTimeout time.Duration        `mapstructure:"execution_timeout"`
	TurnTimeout      time.Duration        `mapstructure:"turn_timeout"`
	AllowedEnvs      []string             `mapstructure:"allowed_envs"`
	EnvWhitelist     []string             `mapstructure:"env_whitelist"`
	DefaultWorkDir   string               `mapstructure:"default_work_dir"`
	PIDDir           string               `mapstructure:"pid_dir"`
	AutoRetry        AutoRetryConfig      `mapstructure:"auto_retry"`
	OpenCodeServer   OpenCodeServerConfig `mapstructure:"opencode_server"`
	ClaudeCode       ClaudeCodeConfig     `mapstructure:"claude_code"`
}

// ClaudeCodeConfig holds Claude Code worker startup settings.
type ClaudeCodeConfig struct {
	Command string `mapstructure:"command"` // binary + optional subcommand, e.g. "claude" or "ccr code"
}

// OpenCodeServerConfig holds OpenCode Server singleton process settings.
type OpenCodeServerConfig struct {
	IdleDrainPeriod   time.Duration `mapstructure:"idle_drain_period"`
	ReadyTimeout      time.Duration `mapstructure:"ready_timeout"`
	ReadyPollInterval time.Duration `mapstructure:"ready_poll_interval"`
	HTTPTimeout       time.Duration `mapstructure:"http_timeout"`
}

// AutoRetryConfig controls automatic retry behavior when LLM provider returns
// temporary errors (429 rate limit, 529 overload, 400 bad request, etc.).
type AutoRetryConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	MaxRetries int           `mapstructure:"max_retries"`
	BaseDelay  time.Duration `mapstructure:"base_delay"`
	MaxDelay   time.Duration `mapstructure:"max_delay"`
	RetryInput string        `mapstructure:"retry_input"`
	NotifyUser bool          `mapstructure:"notify_user"`
	Patterns   []string      `mapstructure:"patterns"`
}

// Defaults applies sensible defaults to AutoRetryConfig and returns the updated struct.
func (c AutoRetryConfig) Defaults() AutoRetryConfig {
	if c.MaxRetries <= 0 {
		c.MaxRetries = 9
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = 5 * time.Second
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 120 * time.Second
	}
	if c.RetryInput == "" {
		c.RetryInput = "继续"
	}
	return c
}

// SecurityConfig holds auth and input validation settings.
// Sensitive fields (JWTSecret) must be provided via SecretsProvider after Load.
type SecurityConfig struct {
	APIKeyHeader   string   `mapstructure:"api_key_header"`
	APIKeys        []string `mapstructure:"api_keys"`
	TLSEnabled     bool     `mapstructure:"tls_enabled"`
	TLSCertFile    string   `mapstructure:"tls_cert_file"`
	TLSKeyFile     string   `mapstructure:"tls_key_file"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	JWTSecret      []byte   `mapstructure:"-"` // loaded via SecretsProvider, never from config file
	JWTAudience    string   `mapstructure:"jwt_audience"`
}

// SessionConfig holds session lifecycle settings.
type SessionConfig struct {
	RetentionPeriod time.Duration `mapstructure:"retention_period"`
	GCScanInterval  time.Duration `mapstructure:"gc_scan_interval"`
	MaxConcurrent   int           `mapstructure:"max_concurrent"`
}

// PoolConfig holds session pool settings.
type PoolConfig struct {
	MinSize          int   `mapstructure:"min_size"`
	MaxSize          int   `mapstructure:"max_size"`
	MaxIdlePerUser   int   `mapstructure:"max_idle_per_user"`
	MaxMemoryPerUser int64 `mapstructure:"max_memory_per_user"` // bytes; 0 = unlimited
}

// AgentConfig holds agent personality/context configuration settings.
type AgentConfig struct {
	Enabled        bool          `mapstructure:"enabled"`          // enable agent config loading
	ConfigDir      string        `mapstructure:"config_dir"`       // default: ~/.hotplex/agent-configs/
	SkillsCacheTTL time.Duration `mapstructure:"skills_cache_ttl"` // TTL for skills list cache, default 24h
}

// ─── Defaults ────────────────────────────────────────────────────────────────

// Default returns a Config with sensible production defaults.
// All non-sensitive fields have values — the binary runs with zero config.
// Sensitive fields (JWTSecret) are left empty and must be provided separately.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:                  ":8888",
			ReadBufferSize:        4096,
			WriteBufferSize:       4096,
			PingInterval:          54 * time.Second,
			PongTimeout:           60 * time.Second,
			WriteTimeout:          10 * time.Second,
			IdleTimeout:           5 * time.Minute,
			MaxFrameSize:          32 * 1024,
			BroadcastQueueSize:    256,
			PlatformWriteBuffer:   64,
			PlatformDropThreshold: 56,
			DeltaCoalesceInterval: 120 * time.Millisecond,
			DeltaCoalesceSize:     200,
		},
		DB: DBConfig{
			Path:            filepath.Join(HotplexHome(), "data", "hotplex.db"),
			WALMode:         true,
			BusyTimeout:     5 * time.Second,
			MaxOpenConns:    2,
			VacuumThreshold: 0.2,
		},
		Worker: WorkerConfig{
			MaxLifetime:      24 * time.Hour,
			IdleTimeout:      60 * time.Minute,
			ExecutionTimeout: 30 * time.Minute,
			TurnTimeout:      15 * time.Minute,
			AllowedEnvs:      nil,
			EnvWhitelist:     nil,
			DefaultWorkDir:   filepath.Join(HotplexHome(), "workspace"),
			PIDDir:           filepath.Join(HotplexHome(), ".pids"),
			AutoRetry:        AutoRetryConfig{Enabled: true, MaxRetries: 9, BaseDelay: 5 * time.Second, MaxDelay: 120 * time.Second, RetryInput: "继续", NotifyUser: true},
			OpenCodeServer: OpenCodeServerConfig{
				IdleDrainPeriod:   30 * time.Minute,
				ReadyTimeout:      10 * time.Second,
				ReadyPollInterval: 200 * time.Millisecond,
				HTTPTimeout:       30 * time.Second,
			},
			ClaudeCode: ClaudeCodeConfig{
				Command: "claude",
			},
		},
		Security: SecurityConfig{
			APIKeyHeader:   "X-API-Key",
			APIKeys:        nil,
			TLSEnabled:     false,
			AllowedOrigins: []string{"*"},
		},
		Session: SessionConfig{
			RetentionPeriod: 7 * 24 * time.Hour,
			GCScanInterval:  1 * time.Minute,
			MaxConcurrent:   1000,
		},
		Pool: PoolConfig{
			MinSize:          0,
			MaxSize:          100,
			MaxIdlePerUser:   5,
			MaxMemoryPerUser: 3 << 30, // 3 GB
		},
		Admin: AdminConfig{
			Enabled:            true,
			Addr:               ":9999",
			Tokens:             nil,
			TokenScopes:        nil,
			DefaultScopes:      []string{"session:read", "stats:read", "health:read"},
			IPWhitelistEnabled: false,
			AllowedCIDRs:       []string{"127.0.0.0/8", "10.0.0.0/8"},
			RateLimitEnabled:   true,
			RequestsPerSec:     10,
			Burst:              20,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Messaging: MessagingConfig{
			Feishu: FeishuConfig{
				RequireMention: true,
				DMPolicy:       "allowlist",
				GroupPolicy:    "allowlist",
				STTConfig: STTConfig{
					Provider:     "feishu+local",
					LocalCmd:     "python3 " + filepath.Join(HotplexHome(), "scripts", "stt_server.py"),
					LocalMode:    "persistent",
					LocalIdleTTL: time.Hour,
				},
			},
			Slack: SlackConfig{
				RequireMention: true,
				DMPolicy:       "allowlist",
				GroupPolicy:    "allowlist",
				STTConfig: STTConfig{
					Provider:     "local",
					LocalCmd:     "python3 " + filepath.Join(HotplexHome(), "scripts", "stt_server.py"),
					LocalMode:    "persistent",
					LocalIdleTTL: time.Hour,
				},
			},
		},
		AgentConfig: AgentConfig{
			Enabled:        true,
			ConfigDir:      filepath.Join(HotplexHome(), "agent-configs"),
			SkillsCacheTTL: 24 * time.Hour,
		},
	}
}

// ─── Loading ─────────────────────────────────────────────────────────────────

// LoadOptions controls how configuration is loaded.
type LoadOptions struct {
	// SecretsProvider supplies sensitive values (e.g. JWT secret, API keys).
	// If nil, secrets are read from HOTPLEX_* environment variables.
	SecretsProvider SecretsProvider
}

// ErrConfigCycle is returned when a config inheritance chain contains a cycle.
var ErrConfigCycle = errors.New("config: inheritance cycle detected")

// Load reads configuration from the given file path, then applies defaults
// and secrets.  Configuration strategy: convention over configuration.
//
// Load order (later overrides earlier):
//  1. Sensible defaults (Default())
//  2. Parent config file (via inherits field), recursively, with cycle detection
//  3. Config file (YAML/JSON/TOML) — canonical source for non-sensitive values
//  4. Environment variables (HOTPLEX_*)
//  5. Secrets provider — only sensitive fields (JWTSecret, etc.)
//
// If filePath is empty, only defaults + environment + secrets are used.
func Load(filePath string, opts LoadOptions) (*Config, error) {
	cfg, err := loadRecursive(filePath, opts, nil)
	if err != nil {
		return nil, err
	}

	// Environment variable overrides (e.g. HOTPLEX_LOG_FORMAT=text)
	v := viper.New()
	v.SetEnvPrefix("HOTPLEX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Explicitly bind common keys to ensure Unmarshal picks up environment variables.
	// Viper's AutomaticEnv only works during Unmarshal if keys are known.
	_ = v.BindEnv("log.level")
	_ = v.BindEnv("log.format")
	_ = v.BindEnv("db.path")
	_ = v.BindEnv("db.wal_mode")
	_ = v.BindEnv("gateway.addr")
	_ = v.BindEnv("admin.enabled")
	_ = v.BindEnv("admin.addr")
	_ = v.BindEnv("session.max_concurrent")
	_ = v.BindEnv("session.retention_period")
	_ = v.BindEnv("pool.max_size")
	_ = v.BindEnv("pool.max_idle_per_user")
	_ = v.BindEnv("pool.max_memory_per_user")
	_ = v.BindEnv("worker.default_work_dir")
	_ = v.BindEnv("worker.max_lifetime")
	_ = v.BindEnv("worker.idle_timeout")
	_ = v.BindEnv("worker.execution_timeout")
	_ = v.BindEnv("worker.auto_retry.enabled")
	_ = v.BindEnv("worker.auto_retry.max_retries")
	_ = v.BindEnv("security.jwt_audience")
	_ = v.BindEnv("security.api_key_header")

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: environment override: %w", err)
	}

	return cfg, nil
}

// loadRecursive loads a config file and its ancestors, detecting cycles.
// visited tracks file paths already loaded in the current chain; nil on the root call.
func loadRecursive(filePath string, opts LoadOptions, visited []string) (*Config, error) {
	// Start with defaults.
	cfg := Default()

	// Track visited files for cycle detection.
	var ancestors []string
	if visited == nil {
		ancestors = []string{}
	} else {
		ancestors = make([]string, len(visited), len(visited)+1)
		copy(ancestors, visited)
	}

	var parentFile string
	var childViper *viper.Viper

	// If a config file is provided, unmarshal it over defaults.
	if filePath != "" {
		absPath, err := ExpandAndAbs(filePath)
		if err != nil {
			return nil, fmt.Errorf("config: resolve path %q: %w", filePath, err)
		}
		filePath = absPath

		// Check for cycle: if this file is already in the ancestor chain.
		if slices.Contains(ancestors, filePath) {
			return nil, fmt.Errorf("%w: %v → %s", ErrConfigCycle, append(ancestors, filePath), filePath)
		}

		childViper = viper.New()
		childViper.SetConfigFile(filePath)
		if err := childViper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read %q: %w", filePath, err)
		}
		if err := childViper.Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("config: unmarshal %q: %w", filePath, err)
		}

		parentFile = cfg.Inherits
	}

	// Recursively load parent config if inheritance is specified.
	// Resolve parentFile relative to the directory of the current file.
	if parentFile != "" {
		ancestors = append(ancestors, filePath)
		// If parentFile is relative, resolve it relative to the current file's directory.
		if !filepath.IsAbs(parentFile) && filePath != "" {
			parentFile = filepath.Join(filepath.Dir(filePath), parentFile)
		}
		parentCfg, err := loadRecursive(parentFile, opts, ancestors)
		if err != nil {
			return nil, fmt.Errorf("config: inherits %q: %w", parentFile, err)
		}
		// Apply child values over parent using the already-loaded viper instance.
		// This avoids a second disk read and eliminates TOCTOU risk.
		if err := childViper.Unmarshal(parentCfg); err != nil {
			return nil, fmt.Errorf("config: merge %q: %w", filePath, err)
		}
		*cfg = *parentCfg
	}

	// Apply secrets via provider.  If no provider given, fall back to env vars
	// (HOTPLEX_JWT_SECRET etc.) for backwards compatibility.
	sp := opts.SecretsProvider
	if sp == nil {
		sp = NewEnvSecretsProvider()
	}

	// JWTSecret — only from secrets provider, never from config file.
	// The secret is base64-encoded (standard or URL-safe) and decoded before use.
	// This matches the client token generator's key loading behavior.
	if secret := sp.Get("HOTPLEX_JWT_SECRET"); secret != "" {
		cfg.Security.JWTSecret = decodeJWTSecret(secret)
	}

	// Numbered environment variables for slices (e.g. HOTPLEX_ADMIN_TOKEN_1..N)
	// This supports project conventions for secret rotation and .env clarity.
	cfg.Admin.Tokens = aggregateNumberedEnv(cfg.Admin.Tokens, "HOTPLEX_ADMIN_TOKEN_")
	cfg.Security.APIKeys = aggregateNumberedEnv(cfg.Security.APIKeys, "HOTPLEX_SECURITY_API_KEY_")

	// Messaging platform env var overrides.
	applyMessagingEnv(cfg)

	// Post-process: normalize allowed_envs into env_whitelist.
	if len(cfg.Worker.AllowedEnvs) > 0 {
		seen := make(map[string]bool)
		for _, e := range cfg.Worker.EnvWhitelist {
			seen[e] = true
		}
		for _, e := range cfg.Worker.AllowedEnvs {
			seen[e] = true
		}
		cfg.Worker.EnvWhitelist = nil
		for e := range seen {
			cfg.Worker.EnvWhitelist = append(cfg.Worker.EnvWhitelist, e)
		}
	}

	// Normalize DB path.
	if cfg.DB.Path != "" {
		absPath, err := ExpandAndAbs(cfg.DB.Path)
		if err != nil {
			return nil, fmt.Errorf("config: normalize db path %q: %w", cfg.DB.Path, err)
		}
		cfg.DB.Path = absPath
	}

	return cfg, nil
}

// ExpandAndAbs returns an absolute path, resolving ~ and relative paths.
// If the path starts with ~ and $HOME is not set, the original path is returned.
func ExpandAndAbs(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			// In test environments, $HOME may not be set. Return the path as-is
			// rather than failing, but log a warning for visibility.
			// The path will fail later when actually accessed, which is acceptable.
			return p, nil
		}
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		p = abs
	}
	// Resolve symlinks to prevent TOCTOU attacks on SwitchWorkDir.
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p, nil
}

// HotplexHome returns the base directory for all HotPlex state (~/.hotplex).
// It does not create the directory — callers should use ensureDir or rely on
// the components that need the directory to create it on first use.
func HotplexHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return TempBaseDir()
	}
	return filepath.Join(home, ".hotplex")
}

// aggregateNumberedEnv appends values from environment variables like PREFIX_1, PREFIX_2...
// to the existing slice, deduplicating them.  Supports project's secret rotation convention.
func aggregateNumberedEnv(existing []string, prefix string) []string {
	seen := make(map[string]bool)
	for _, v := range existing {
		seen[v] = true
	}

	for i := 1; ; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		val := os.Getenv(key)
		if val == "" {
			break
		}
		if !seen[val] {
			existing = append(existing, val)
			seen[val] = true
		}
	}
	return existing
}

// applyMessagingEnv overrides messaging config from environment variables.
// This is needed because Viper's AutomaticEnv cannot map nested keys
// unless the viper instance has seen them from a config file or SetDefault.
func applyMessagingEnv(cfg *Config) {
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_ENABLED"); v != "" {
		cfg.Messaging.Slack.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN"); v != "" {
		cfg.Messaging.Slack.BotToken = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_APP_TOKEN"); v != "" {
		cfg.Messaging.Slack.AppToken = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_ENABLED"); v != "" {
		cfg.Messaging.Feishu.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_APP_ID"); v != "" {
		cfg.Messaging.Feishu.AppID = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET"); v != "" {
		cfg.Messaging.Feishu.AppSecret = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE"); v != "" {
		cfg.Messaging.Feishu.WorkerType = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_WORK_DIR"); v != "" {
		cfg.Messaging.Feishu.WorkDir = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_WORKER_TYPE"); v != "" {
		cfg.Messaging.Slack.WorkerType = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_WORK_DIR"); v != "" {
		cfg.Messaging.Slack.WorkDir = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_DM_POLICY"); v != "" {
		cfg.Messaging.Slack.DMPolicy = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_GROUP_POLICY"); v != "" {
		cfg.Messaging.Slack.GroupPolicy = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION"); v != "" {
		cfg.Messaging.Slack.RequireMention = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_ALLOW_FROM"); v != "" {
		cfg.Messaging.Slack.AllowFrom = strings.Split(v, ",")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM"); v != "" {
		cfg.Messaging.Slack.AllowDMFrom = strings.Split(v, ",")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM"); v != "" {
		cfg.Messaging.Slack.AllowGroupFrom = strings.Split(v, ",")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_DM_POLICY"); v != "" {
		cfg.Messaging.Feishu.DMPolicy = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY"); v != "" {
		cfg.Messaging.Feishu.GroupPolicy = v
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION"); v != "" {
		cfg.Messaging.Feishu.RequireMention = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM"); v != "" {
		cfg.Messaging.Feishu.AllowFrom = strings.Split(v, ",")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM"); v != "" {
		cfg.Messaging.Feishu.AllowDMFrom = strings.Split(v, ",")
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM"); v != "" {
		cfg.Messaging.Feishu.AllowGroupFrom = strings.Split(v, ",")
	}
}

// MustLoad is like Load but panics on error.
func MustLoad(filePath string, opts LoadOptions) *Config {
	cfg, err := Load(filePath, opts)
	if err != nil {
		panic("config.MustLoad: " + err.Error())
	}
	return cfg
}

// ReadFile loads a config file and returns its raw bytes (for testing).
func ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// decodeJWTSecret decodes a base64-encoded JWT secret.
// It supports both standard base64 and URL-safe base64 (with or without padding).
// This matches the client token generator's key loading behavior.
func decodeJWTSecret(secret string) []byte {
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil && len(decoded) == 32 {
		return decoded
	}
	if decoded, err := base64.URLEncoding.DecodeString(secret); err == nil && len(decoded) == 32 {
		return decoded
	}
	raw := []byte(secret)
	if len(raw) == 32 {
		return raw
	}
	return []byte(secret)
}
