package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Config holds all gateway configuration.
type Config struct {
	Gateway  GatewayConfig  `mapstructure:"gateway"`
	DB       DBConfig       `mapstructure:"db"`
	Worker   WorkerConfig   `mapstructure:"worker"`
	Security SecurityConfig `mapstructure:"security"`
	Session  SessionConfig  `mapstructure:"session"`
	Pool     PoolConfig     `mapstructure:"pool"`
}

// GatewayConfig holds WS gateway settings.
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
}

// SecurityConfig holds auth and input validation settings.
type SecurityConfig struct {
	APIKeyHeader   string   `mapstructure:"api_key_header"`
	APIKeys        []string `mapstructure:"api_keys"`
	TLSEnabled     bool     `mapstructure:"tls_enabled"`
	TLSCertFile    string   `mapstructure:"tls_cert_file"`
	TLSKeyFile     string   `mapstructure:"tls_key_file"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// SessionConfig holds session lifecycle settings.
type SessionConfig struct {
	RetentionPeriod time.Duration `mapstructure:"retention_period"`
	GCScanInterval  time.Duration `mapstructure:"gc_scan_interval"`
	MaxConcurrent   int           `mapstructure:"max_concurrent"`
}

// PoolConfig holds session pool settings.
type PoolConfig struct {
	MinSize        int `mapstructure:"min_size"`
	MaxSize        int `mapstructure:"max_size"`
	MaxIdlePerUser int `mapstructure:"max_idle_per_user"`
}

// Default returns a Config with sensible production defaults.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:               ":8080",
			ReadBufferSize:     4096,
			WriteBufferSize:    4096,
			PingInterval:       30 * time.Second,
			PongTimeout:        10 * time.Second,
			WriteTimeout:       10 * time.Second,
			IdleTimeout:        5 * time.Minute,
			MaxFrameSize:       32 * 1024,
			BroadcastQueueSize: 256,
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
			AllowedEnvs:      []string{},
			EnvWhitelist:     []string{},
		},
		Security: SecurityConfig{
			APIKeyHeader:   "X-API-Key",
			APIKeys:        []string{},
			TLSEnabled:     false,
			AllowedOrigins: []string{"*"},
		},
		Session: SessionConfig{
			RetentionPeriod: 7 * 24 * time.Hour,
			GCScanInterval:  1 * time.Minute,
			MaxConcurrent:   1000,
		},
		Pool: PoolConfig{
			MinSize:        0,
			MaxSize:        100,
			MaxIdlePerUser: 3,
		},
	}
}

// Load reads configuration from the given file path and environment variables.
// filePath is the path to the config file (YAML/JSON/TOML). If empty, only env vars are used.
func Load(filePath string) (*Config, error) {
	v := viper.New()
	v.SetTypeByDefaultValue(true)

	if filePath != "" {
		v.SetConfigFile(filePath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read file %q: %w", filePath, err)
		}
	}

	// Environment variable overrides: GATEWAY_ADDR, DB_PATH, etc.
	v.SetEnvPrefix("HOTPLEX")
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// Post-process: merge allowed_envs into env_whitelist (union).
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

	return &cfg, nil
}

// MustLoad is like Load but panics on error.
func MustLoad(filePath string) *Config {
	cfg, err := Load(filePath)
	if err != nil {
		panic("config.MustLoad: " + err.Error())
	}
	return cfg
}

// ReadFile loads the named config file and returns its raw bytes.
// Used by tests to verify config file parsing.
func ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}
