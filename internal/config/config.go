package config

import (
	"fmt"
	"os"
	"regexp"
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
		errs = append(errs, "db.path is required (or use default gateway.db)")
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
	Gateway  GatewayConfig   `mapstructure:"gateway"`
	DB       DBConfig        `mapstructure:"db"`
	Worker   WorkerConfig    `mapstructure:"worker"`
	Security SecurityConfig `mapstructure:"security"`
	Session  SessionConfig  `mapstructure:"session"`
	Pool     PoolConfig     `mapstructure:"pool"`
	Admin    AdminConfig    `mapstructure:"admin"`
}

// AdminConfig holds admin API settings.
type AdminConfig struct {
	Enabled            bool     `mapstructure:"enabled"`
	Addr               string   `mapstructure:"addr"`
	Tokens             []string `mapstructure:"tokens"`
	TokenScopes        map[string][]string `mapstructure:"token_scopes"`
	DefaultScopes      []string `mapstructure:"default_scopes"`
	IPWhitelistEnabled bool     `mapstructure:"ip_whitelist_enabled"`
	AllowedCIDRs       []string `mapstructure:"allowed_cidrs"`
	RateLimitEnabled   bool     `mapstructure:"rate_limit_enabled"`
	RequestsPerSec     int      `mapstructure:"requests_per_sec"`
	Burst              int      `mapstructure:"burst"`
}

// GatewayConfig holds WebSocket gateway settings.
type GatewayConfig struct {
	Addr                string        `mapstructure:"addr"`
	ReadBufferSize      int           `mapstructure:"read_buffer_size"`
	WriteBufferSize     int           `mapstructure:"write_buffer_size"`
	PingInterval        time.Duration `mapstructure:"ping_interval"`
	PongTimeout         time.Duration `mapstructure:"pong_timeout"`
	WriteTimeout        time.Duration `mapstructure:"write_timeout"`
	IdleTimeout         time.Duration `mapstructure:"idle_timeout"`
	MaxFrameSize        int64         `mapstructure:"max_frame_size"`
	BroadcastQueueSize  int           `mapstructure:"broadcast_queue_size"`
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
}

// SecurityConfig holds auth and input validation settings.
// Sensitive fields (JWTSecret) must be provided via SecretsProvider after Load.
type SecurityConfig struct {
	APIKeyHeader    string   `mapstructure:"api_key_header"`
	APIKeys         []string `mapstructure:"api_keys"`
	TLSEnabled      bool     `mapstructure:"tls_enabled"`
	TLSCertFile     string   `mapstructure:"tls_cert_file"`
	TLSKeyFile      string   `mapstructure:"tls_key_file"`
	AllowedOrigins  []string `mapstructure:"allowed_origins"`
	JWTSecret       []byte   `mapstructure:"-"` // loaded via SecretsProvider, never from config file
	JWTAudience     string   `mapstructure:"jwt_audience"`
}

// SessionConfig holds session lifecycle settings.
type SessionConfig struct {
	RetentionPeriod   time.Duration `mapstructure:"retention_period"`
	GCScanInterval    time.Duration `mapstructure:"gc_scan_interval"`
	MaxConcurrent     int           `mapstructure:"max_concurrent"`
	EventStoreEnabled bool          `mapstructure:"event_store_enabled"`
}

// PoolConfig holds session pool settings.
type PoolConfig struct {
	MinSize        int `mapstructure:"min_size"`
	MaxSize        int `mapstructure:"max_size"`
	MaxIdlePerUser int `mapstructure:"max_idle_per_user"`
}

// ─── Defaults ────────────────────────────────────────────────────────────────

// Default returns a Config with sensible production defaults.
// All non-sensitive fields have values — the binary runs with zero config.
// Sensitive fields (JWTSecret) are left empty and must be provided separately.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:               ":8080",
			ReadBufferSize:     4096,
			WriteBufferSize:    4096,
			PingInterval:        54 * time.Second,
			PongTimeout:         60 * time.Second,
			WriteTimeout:        10 * time.Second,
			IdleTimeout:         5 * time.Minute,
			MaxFrameSize:        32 * 1024,
			BroadcastQueueSize:  256,
		},
		DB: DBConfig{
			Path:         "gateway.db",
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
		},
		Security: SecurityConfig{
			APIKeyHeader:   "X-API-Key",
			APIKeys:        nil,
			TLSEnabled:     false,
			AllowedOrigins: []string{"*"},
		},
		Session: SessionConfig{
			RetentionPeriod:   7 * 24 * time.Hour,
			GCScanInterval:     1 * time.Minute,
			MaxConcurrent:     1000,
			EventStoreEnabled: true,
		},
		Pool: PoolConfig{
			MinSize:        0,
			MaxSize:        100,
			MaxIdlePerUser: 3,
		},
		Admin: AdminConfig{
			Enabled:            true,
			Addr:               ":9080",
			Tokens:             nil,
			TokenScopes:        nil,
			DefaultScopes:      []string{"session:read", "stats:read", "health:read"},
			IPWhitelistEnabled: false,
			AllowedCIDRs:       []string{"127.0.0.0/8", "10.0.0.0/8"},
			RateLimitEnabled:   true,
			RequestsPerSec:     10,
			Burst:              20,
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

// Load reads configuration from the given file path, then applies defaults
// and secrets.  Configuration strategy: convention over configuration.
//
// Load order (later overrides earlier):
//   1. Sensible defaults (Default())
//   2. Config file (YAML/JSON/TOML) — canonical source for non-sensitive values
//   3. Secrets provider — only sensitive fields (JWTSecret, etc.)
//
// If filePath is empty, only defaults + secrets are used (env-free startup
// is possible by providing secrets via SecretsProvider).
func Load(filePath string, opts LoadOptions) (*Config, error) {
	cfg := Default()

	// If a config file is provided, unmarshal it over defaults.
	// viper merges, so unspecified fields retain defaults.
	if filePath != "" {
		v := viper.New()
		v.SetConfigFile(filePath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read %q: %w", filePath, err)
		}
		if err := v.Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("config: unmarshal %q: %w", filePath, err)
		}
	}

	// Apply secrets via provider.  If no provider given, fall back to env vars
	// (HOTPLEX_JWT_SECRET etc.) for backwards compatibility.
	sp := opts.SecretsProvider
	if sp == nil {
		sp = NewEnvSecretsProvider()
	}

	// JWTSecret — only from secrets provider, never from config file.
	if secret := sp.Get("HOTPLEX_JWT_SECRET"); secret != "" {
		cfg.Security.JWTSecret = []byte(secret)
	}

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

	return cfg, nil
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
