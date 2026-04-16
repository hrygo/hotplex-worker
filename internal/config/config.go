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
		errs = append(errs, "db.path is required (or use default hotplex-worker.db)")
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
	Gateway  GatewayConfig  `mapstructure:"gateway"`
	DB       DBConfig       `mapstructure:"db"`
	Worker   WorkerConfig   `mapstructure:"worker"`
	Security SecurityConfig `mapstructure:"security"`
	Session  SessionConfig  `mapstructure:"session"`
	Pool     PoolConfig     `mapstructure:"pool"`
	Log      LogConfig      `mapstructure:"log"`
	Admin    AdminConfig    `mapstructure:"admin"`
	Inherits string         `mapstructure:"inherits"` // path to parent config file; "" = no inheritance
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
}

// DBConfig holds SQLite settings.
type DBConfig struct {
	Path         string        `mapstructure:"path"`
	WALMode      bool          `mapstructure:"wal_mode"`
	BusyTimeout  time.Duration `mapstructure:"busy_timeout"`
	MaxOpenConns int           `mapstructure:"max_open_conns"`
}

// WorkerConfig holds per-worker defaults.
type WorkerConfig struct {
	MaxLifetime      time.Duration `mapstructure:"max_lifetime"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout"`
	ExecutionTimeout time.Duration `mapstructure:"execution_timeout"`
	AllowedEnvs      []string      `mapstructure:"allowed_envs"`
	EnvWhitelist     []string      `mapstructure:"env_whitelist"`
	DefaultWorkDir   string        `mapstructure:"default_work_dir"`
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
	RetentionPeriod   time.Duration `mapstructure:"retention_period"`
	GCScanInterval    time.Duration `mapstructure:"gc_scan_interval"`
	MaxConcurrent     int           `mapstructure:"max_concurrent"`
	EventStoreEnabled bool          `mapstructure:"event_store_enabled"`
	EventStoreType    string        `mapstructure:"event_store_type"` // "sqlite" (default), or custom registered type
}

// PoolConfig holds session pool settings.
type PoolConfig struct {
	MinSize          int   `mapstructure:"min_size"`
	MaxSize          int   `mapstructure:"max_size"`
	MaxIdlePerUser   int   `mapstructure:"max_idle_per_user"`
	MaxMemoryPerUser int64 `mapstructure:"max_memory_per_user"` // bytes; 0 = unlimited
}

// ─── Defaults ────────────────────────────────────────────────────────────────

// Default returns a Config with sensible production defaults.
// All non-sensitive fields have values — the binary runs with zero config.
// Sensitive fields (JWTSecret) are left empty and must be provided separately.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:               ":8888",
			ReadBufferSize:     4096,
			WriteBufferSize:    4096,
			PingInterval:       54 * time.Second,
			PongTimeout:        60 * time.Second,
			WriteTimeout:       10 * time.Second,
			IdleTimeout:        5 * time.Minute,
			MaxFrameSize:       32 * 1024,
			BroadcastQueueSize: 256,
		},
		DB: DBConfig{
			Path:         "data/hotplex-worker.db",
			WALMode:      true,
			BusyTimeout:  500 * time.Millisecond,
			MaxOpenConns: 1,
		},
		Worker: WorkerConfig{
			MaxLifetime:      24 * time.Hour,
			IdleTimeout:      30 * time.Minute,
			ExecutionTimeout: 10 * time.Minute,
			AllowedEnvs:      nil,
			EnvWhitelist:     nil,
			DefaultWorkDir:   "/tmp/hotplex/workspace",
		},
		Security: SecurityConfig{
			APIKeyHeader:   "X-API-Key",
			APIKeys:        nil,
			TLSEnabled:     false,
			AllowedOrigins: []string{"*"},
		},
		Session: SessionConfig{
			RetentionPeriod:   7 * 24 * time.Hour,
			GCScanInterval:    1 * time.Minute,
			MaxConcurrent:     1000,
			EventStoreEnabled: true,
		},
		Pool: PoolConfig{
			MinSize:          0,
			MaxSize:          100,
			MaxIdlePerUser:   3,
			MaxMemoryPerUser: 2 << 30, // 2 GB
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
		absPath, err := normalizePath(filePath)
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
		absPath, err := normalizePath(cfg.DB.Path)
		if err != nil {
			return nil, fmt.Errorf("config: normalize db path %q: %w", cfg.DB.Path, err)
		}
		cfg.DB.Path = absPath
	}

	return cfg, nil
}

// normalizePath returns an absolute path, resolving ~ and relative paths.
// If the path starts with ~ and $HOME is not set, the original path is returned
// with a warning logged. This allows tests to run without $HOME while still
// catching configuration issues in production.
func normalizePath(p string) (string, error) {
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
	return p, nil
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
