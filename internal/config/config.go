package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

var envVarRe = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

// warnedEnvEntries deduplicates env-entry exclusion warnings so the same
// message is only logged once per process, even when Load is called many times.
var warnedEnvEntries sync.Map

// ExpandEnv expands ${VAR} and ${VAR:-default} references in a config value
// using os.Getenv.  This is used to reference secrets (or other values) from
// environment variables within config file values, e.g.:
//
//	db_password: "${DB_PASSWORD:-}"
//
// Unset variables without defaults expand to empty string.
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

// expandEnvEntry expands ${VAR} references in an environment entry.
// Entries that reference unset variables without a default clause are excluded
// (returned as false) so the entry is omitted from the worker environment.
// A variable set to empty string is treated as unset; use ${VAR:-default} to preserve such entries.
func expandEnvEntry(entry string) (string, bool) {
	for _, m := range envVarRe.FindAllStringSubmatch(entry, -1) {
		if strings.Contains(m[0], ":-") {
			continue // has :-default clause, skip exclusion check
		}
		if len(m) >= 2 && os.Getenv(m[1]) == "" {
			return "", false // unset var, no default → exclude
		}
	}
	return ExpandEnv(entry), true
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
		if os.Getenv("HOTPLEX_JWT_SECRET") != "" {
			missing = append(missing, "security.jwt_secret (set but invalid: must decode to >= 32 bytes)")
		} else {
			missing = append(missing, "security.jwt_secret")
		}
	} else if len(c.Security.JWTSecret) < 32 {
		missing = append(missing, "security.jwt_secret (must decode to >= 32 bytes for ES256)")
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
	Skills      SkillsConfig    `mapstructure:"skills"`
	Cron        CronConfig      `mapstructure:"cron"`
	Inherits    string          `mapstructure:"inherits"` // path to parent config file; "" = no inheritance
}

// MessagingConfig holds messaging platform adapter settings.
// Shared defaults (WorkerType, STTConfig, TTSConfig) are set at this level and propagated
// to each platform config via propagateMessagingDefaults().
// DMPolicy and GroupPolicy remain platform-level only (different platforms may have different access policies).
// Priority: platform-level > messaging-level > Default().
type MessagingConfig struct {
	TurnSummaryEnabled bool `mapstructure:"turn_summary_enabled"`

	// Shared defaults — platforms inherit; platform-level overrides take precedence.
	WorkerType string `mapstructure:"worker_type"`
	STTConfig  `mapstructure:",squash"`
	TTSConfig  `mapstructure:",squash"`

	// Platform-specific configs.
	Slack  SlackConfig  `mapstructure:"slack"`
	Feishu FeishuConfig `mapstructure:"feishu"`
}

// STT constants for provider values.
const (
	STTProviderLocal       = "local"
	STTProviderFeishu      = "feishu"
	STTProviderFeishuLocal = "feishu+local"
)

// STTConfig holds speech-to-text configuration shared across messaging adapters.
type STTConfig struct {
	// Provider: "local" (external command), "feishu" (cloud API),
	// "feishu+local" (cloud primary, local fallback), "" (disabled).
	Provider string `mapstructure:"stt_provider"`
	// LocalCmd is the command template. {file} is replaced with the audio file path.
	// If {file} is present → ephemeral mode (fork per request).
	// If absent → persistent mode (long-lived subprocess, stdin/stdout JSON protocol).
	LocalCmd string `mapstructure:"stt_local_cmd"`
	// LocalIdleTTL controls auto-shutdown of persistent subprocess. 0 = disabled.
	LocalIdleTTL time.Duration `mapstructure:"stt_local_idle_ttl"`
}

// IsPersistent returns true when LocalCmd uses persistent mode (no {file} placeholder).
func (c STTConfig) IsPersistent() bool {
	return c.LocalCmd != "" && !strings.Contains(c.LocalCmd, "{file}")
}

// FillFrom propagates zero-value fields from defaults into c.
func (c *STTConfig) FillFrom(defaults STTConfig) {
	if c.Provider == "" {
		c.Provider = defaults.Provider
	}
	if c.LocalCmd == "" {
		c.LocalCmd = defaults.LocalCmd
	}
	if c.LocalIdleTTL == 0 {
		c.LocalIdleTTL = defaults.LocalIdleTTL
	}
}

// TTSConfig holds text-to-speech settings. Squashed into platform configs.
type TTSConfig struct {
	// TTSEnabled controls whether voice replies are sent for voice-triggered turns.
	TTSEnabled bool `mapstructure:"tts_enabled"`
	// Provider: "edge" (Microsoft Edge TTS, free), "edge+moss" (Edge primary + MOSS-TTS-Nano CPU fallback), "" (disabled).
	TTSProvider string `mapstructure:"tts_provider"`
	// Voice name for Edge TTS (e.g. "zh-CN-XiaoxiaoNeural", "zh-CN-YunxiNeural").
	Voice string `mapstructure:"tts_voice"`
	// MaxChars limits the LLM summary length before TTS synthesis.
	MaxChars int `mapstructure:"tts_max_chars"`
	// MossModelDir is the directory containing MOSS-TTS-Nano ONNX model assets (used when provider is "edge+moss").
	MossModelDir string `mapstructure:"tts_moss_model_dir"`
	// MossVoice is the MOSS built-in voice preset name (default "Xiaoyu").
	MossVoice string `mapstructure:"tts_moss_voice"`
	// MossPort is the localhost port for the MOSS sidecar HTTP server (default 18083).
	MossPort int `mapstructure:"tts_moss_port"`
	// MossIdleTimeout controls how long the MOSS sidecar stays resident after its
	// last use before being automatically shut down. Default 30m.
	MossIdleTimeout time.Duration `mapstructure:"tts_moss_idle_timeout"`
	// MossCpuThreads controls ONNX Runtime intra-op threads for the MOSS sidecar (0 = auto-detect = physical cores).
	MossCpuThreads int `mapstructure:"tts_moss_cpu_threads"`
}

// FillFrom propagates zero-value fields from defaults into c.
func (c *TTSConfig) FillFrom(defaults TTSConfig) {
	if !c.TTSEnabled && defaults.TTSEnabled {
		c.TTSEnabled = defaults.TTSEnabled
	}
	if c.TTSProvider == "" {
		c.TTSProvider = defaults.TTSProvider
	}
	if c.Voice == "" {
		c.Voice = defaults.Voice
	}
	if c.MaxChars == 0 {
		c.MaxChars = defaults.MaxChars
	}
	if c.MossModelDir == "" {
		c.MossModelDir = defaults.MossModelDir
	}
	if c.MossVoice == "" {
		c.MossVoice = defaults.MossVoice
	}
	if c.MossPort == 0 {
		c.MossPort = defaults.MossPort
	}
	if c.MossIdleTimeout == 0 {
		c.MossIdleTimeout = defaults.MossIdleTimeout
	}
	if c.MossCpuThreads == 0 {
		c.MossCpuThreads = defaults.MossCpuThreads
	}
}

// MessagingPlatformConfig holds settings shared by all messaging adapters (Slack, Feishu, etc.).
type MessagingPlatformConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	WorkerType     string   `mapstructure:"worker_type"`
	WorkDir        string   `mapstructure:"work_dir"`
	DMPolicy       string   `mapstructure:"dm_policy"`
	GroupPolicy    string   `mapstructure:"group_policy"`
	RequireMention bool     `mapstructure:"require_mention"`
	AllowFrom      []string `mapstructure:"allow_from"`
	AllowDMFrom    []string `mapstructure:"allow_dm_from"`
	AllowGroupFrom []string `mapstructure:"allow_group_from"`

	STTConfig `mapstructure:",squash"`
	TTSConfig `mapstructure:",squash"`
}

// SlackConfig holds Slack Socket Mode adapter settings.
type SlackConfig struct {
	MessagingPlatformConfig `mapstructure:",squash"`

	BotToken            string        `mapstructure:"bot_token"`
	AppToken            string        `mapstructure:"app_token"`
	SocketMode          bool          `mapstructure:"socket_mode"`
	AssistantAPIEnabled *bool         `mapstructure:"assistant_api_enabled"`
	ReconnectBaseDelay  time.Duration `mapstructure:"reconnect_base_delay"`
	ReconnectMaxDelay   time.Duration `mapstructure:"reconnect_max_delay"`
}

// FeishuConfig holds Feishu WebSocket adapter settings.
type FeishuConfig struct {
	MessagingPlatformConfig `mapstructure:",squash"`

	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
}

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

// WebChatConfig holds webchat UI serving settings.
type WebChatConfig struct {
	Addr    string `mapstructure:"addr"`    // informational: banner display
	Enabled bool   `mapstructure:"enabled"` // serve embedded webchat SPA from gateway
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

// DBConfig holds SQLite settings shared by all database connections.
type DBConfig struct {
	Path              string        `mapstructure:"path"`
	EventsPath        string        `mapstructure:"events_path"` // Deprecated: events table now lives in hotplex.db (same as Path)
	WALMode           bool          `mapstructure:"wal_mode"`
	BusyTimeout       time.Duration `mapstructure:"busy_timeout"`
	MaxOpenConns      int           `mapstructure:"max_open_conns"`
	VacuumThreshold   float64       `mapstructure:"vacuum_threshold"`
	CacheSizeKiB      int           `mapstructure:"cache_size_kib"`
	MmapSizeMiB       int           `mapstructure:"mmap_size_mib"`
	WalAutoCheckpoint int           `mapstructure:"wal_autocheckpoint"`
}

// WorkerConfig holds per-worker defaults.
type WorkerConfig struct {
	MaxLifetime      time.Duration        `mapstructure:"max_lifetime"`
	IdleTimeout      time.Duration        `mapstructure:"idle_timeout"`
	ExecutionTimeout time.Duration        `mapstructure:"execution_timeout"`
	TurnTimeout      time.Duration        `mapstructure:"turn_timeout"`
	EnvBlocklist     []string             `mapstructure:"env_blocklist"`
	DefaultWorkDir   string               `mapstructure:"default_work_dir"`
	PIDDir           string               `mapstructure:"pid_dir"`
	AutoRetry        AutoRetryConfig      `mapstructure:"auto_retry"`
	OpenCodeServer   OpenCodeServerConfig `mapstructure:"opencode_server"`
	ClaudeCode       ClaudeCodeConfig     `mapstructure:"claude_code"`
	Environment      []string             `mapstructure:"environment"`
}

// MCPServerConfig defines a single MCP server for worker startup.
type MCPServerConfig struct {
	Command string            `mapstructure:"command" json:"command"`
	Args    []string          `mapstructure:"args" json:"args,omitempty"`
	Env     map[string]string `mapstructure:"env" json:"env,omitempty"`
	URL     string            `mapstructure:"url" json:"url,omitempty"`
}

// Validate checks that the server config specifies exactly one of Command (stdio) or URL (remote).
func (c *MCPServerConfig) Validate() error {
	if c.Command == "" && c.URL == "" {
		return fmt.Errorf("mcp server config: command or url required")
	}
	if c.Command != "" && c.URL != "" {
		return fmt.Errorf("mcp server config: command and url are mutually exclusive")
	}
	return nil
}

// ClaudeCodeConfig holds Claude Code worker startup settings.
type ClaudeCodeConfig struct {
	Command               string                      `mapstructure:"command"`                 // binary + optional subcommand, e.g. "claude" or "ccr code"
	PermissionPrompt      bool                        `mapstructure:"permission_prompt"`       // enable --permission-prompt-tool stdio for interaction chain
	PermissionAutoApprove []string                    `mapstructure:"permission_auto_approve"` // tool names to auto-approve without user interaction
	MCPServers            map[string]*MCPServerConfig `mapstructure:"mcp_servers"`             // user-configured MCP servers; empty = default discovery
}

// OpenCodeServerConfig holds OpenCode Server singleton process settings.
type OpenCodeServerConfig struct {
	Command           string        `mapstructure:"command"` // binary + optional subcommand, e.g. "opencode" or "opencode serve"
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

	// WorkDir security settings
	WorkDirAllowedBasePatterns []string `mapstructure:"work_dir_allowed_base_patterns"` // extra whitelist patterns (supports ~ and ${VAR})
	WorkDirForbiddenDirs       []string `mapstructure:"work_dir_forbidden_dirs"`        // extra blacklist directories
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

type AgentConfig struct {
	Enabled   bool   `mapstructure:"enabled"`    // enable agent config loading
	ConfigDir string `mapstructure:"config_dir"` // default: ~/.hotplex/agent-configs/
}

// SkillsConfig holds skill discovery and caching settings.
type SkillsConfig struct {
	CacheTTL time.Duration `mapstructure:"cache_ttl"` // TTL for skills list cache, default 5m
}

// CronConfig holds AI-native cronjob scheduler settings.
type CronConfig struct {
	Enabled           bool             `mapstructure:"enabled"`
	MaxConcurrentRuns int              `mapstructure:"max_concurrent_runs"` // default 3
	MaxJobs           int              `mapstructure:"max_jobs"`            // default 50
	DefaultTimeoutSec int              `mapstructure:"default_timeout_sec"` // default 300
	TickIntervalSec   int              `mapstructure:"tick_interval_sec"`   // default 60
	YAMLConfigPath    string           `mapstructure:"yaml_config_path"`    // optional external YAML
	Jobs              []map[string]any `mapstructure:"jobs"`                // inline job definitions
}

// ─── Defaults ────────────────────────────────────────────────────────────────

// Default returns a Config with sensible production defaults.
// All non-sensitive fields have values — the binary runs with zero config.
// Sensitive fields (JWTSecret) are left empty and must be provided separately.
func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Addr:                  "localhost:8888",
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
			Path:              filepath.Join(HotplexHome(), "data", "hotplex.db"),
			EventsPath:        "", // Deprecated: unused, events table lives in hotplex.db
			WALMode:           true,
			BusyTimeout:       5 * time.Second,
			MaxOpenConns:      3, // 1 writer + 2 readers for shared session/event store
			VacuumThreshold:   0.2,
			CacheSizeKiB:      8192,
			MmapSizeMiB:       64,
			WalAutoCheckpoint: 2000,
		},
		Worker: WorkerConfig{
			MaxLifetime:      24 * time.Hour,
			IdleTimeout:      60 * time.Minute,
			ExecutionTimeout: 30 * time.Minute,
			TurnTimeout:      0, // disabled by default; execution_timeout catches zombies
			EnvBlocklist:     nil,
			DefaultWorkDir:   filepath.Join(HotplexHome(), "workspace"),
			PIDDir:           filepath.Join(HotplexHome(), ".pids"),
			AutoRetry:        AutoRetryConfig{Enabled: true, MaxRetries: 9, BaseDelay: 5 * time.Second, MaxDelay: 120 * time.Second, RetryInput: "继续", NotifyUser: true},
			OpenCodeServer: OpenCodeServerConfig{
				Command:           "opencode",
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
			Addr:               "localhost:9999",
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
		WebChat: WebChatConfig{
			Addr:    "",
			Enabled: true,
		},
		Messaging: MessagingConfig{
			TurnSummaryEnabled: true,
			// Shared defaults — propagated to platforms via propagateMessagingDefaults().
			WorkerType: "claude_code",
			STTConfig: STTConfig{
				Provider:     "local",
				LocalCmd:     "python3 " + filepath.Join(HotplexHome(), "scripts", "stt_server.py"),
				LocalIdleTTL: time.Hour,
			},
			TTSConfig: TTSConfig{
				TTSEnabled:      true,
				TTSProvider:     "edge+moss",
				Voice:           "zh-CN-XiaoxiaoNeural",
				MaxChars:        150,
				MossModelDir:    filepath.Join(HotplexHome(), "models", "moss-tts-nano"),
				MossVoice:       "Xiaoyu",
				MossPort:        18083,
				MossIdleTimeout: 30 * time.Minute,
				MossCpuThreads:  0,
			},
			Feishu: FeishuConfig{
				MessagingPlatformConfig: defaultMessagingPlatformConfig(),
			},
			Slack: SlackConfig{
				MessagingPlatformConfig: defaultMessagingPlatformConfig(),
			},
		},
		AgentConfig: AgentConfig{
			Enabled:   true,
			ConfigDir: filepath.Join(HotplexHome(), "agent-configs"),
		},
		Skills: SkillsConfig{
			CacheTTL: 5 * time.Minute,
		},
		Cron: CronConfig{
			Enabled:           true,
			MaxConcurrentRuns: 3,
			MaxJobs:           50,
			DefaultTimeoutSec: 300,
			TickIntervalSec:   60,
		},
	}
}

func defaultMessagingPlatformConfig() MessagingPlatformConfig {
	return MessagingPlatformConfig{
		RequireMention: true,
		DMPolicy:       "allowlist",
		GroupPolicy:    "allowlist",
	}
}

// propagateMessagingDefaults copies shared fields from MessagingConfig to each
// platform config. Only zero-value fields on the platform side are filled;
// existing values are never overwritten.
//
// Priority: platform-level (YAML / env) > messaging-level > Default().
func propagateMessagingDefaults(cfg *Config) {
	msg := &cfg.Messaging
	propagatePlatform(&msg.Slack.MessagingPlatformConfig, msg)
	propagatePlatform(&msg.Feishu.MessagingPlatformConfig, msg)
}

// propagatePlatform fills zero-value fields on the platform from the messaging-level shared config.
func propagatePlatform(p *MessagingPlatformConfig, msg *MessagingConfig) {
	if p.WorkerType == "" {
		p.WorkerType = msg.WorkerType
	}
	p.STTConfig.FillFrom(msg.STTConfig)
	p.TTSConfig.FillFrom(msg.TTSConfig)
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
	_ = v.BindEnv("worker.claude_code.command")
	_ = v.BindEnv("worker.opencode_server.command")
	_ = v.BindEnv("worker.opencode_server.idle_drain_period")
	_ = v.BindEnv("worker.opencode_server.ready_timeout")
	_ = v.BindEnv("worker.opencode_server.ready_poll_interval")
	_ = v.BindEnv("worker.opencode_server.http_timeout")
	_ = v.BindEnv("security.jwt_audience")
	_ = v.BindEnv("security.api_key_header")
	_ = v.BindEnv("agent_config.enabled")
	_ = v.BindEnv("agent_config.config_dir")
	_ = v.BindEnv("skills.cache_ttl")
	_ = v.BindEnv("messaging.slack.work_dir")
	_ = v.BindEnv("messaging.slack.stt_provider")
	_ = v.BindEnv("messaging.slack.stt_local_cmd")
	_ = v.BindEnv("messaging.slack.stt_local_idle_ttl")
	_ = v.BindEnv("messaging.feishu.work_dir")
	_ = v.BindEnv("messaging.feishu.stt_provider")
	_ = v.BindEnv("messaging.feishu.stt_local_cmd")
	_ = v.BindEnv("messaging.feishu.stt_local_idle_ttl")
	_ = v.BindEnv("messaging.feishu.tts_enabled")
	_ = v.BindEnv("messaging.feishu.tts_provider")
	_ = v.BindEnv("messaging.feishu.tts_voice")
	_ = v.BindEnv("messaging.feishu.tts_max_chars")
	_ = v.BindEnv("messaging.feishu.tts_moss_model_dir")
	_ = v.BindEnv("messaging.feishu.tts_moss_voice")
	_ = v.BindEnv("messaging.feishu.tts_moss_port")
	_ = v.BindEnv("messaging.feishu.tts_moss_idle_timeout")
	_ = v.BindEnv("messaging.feishu.tts_moss_cpu_threads")
	_ = v.BindEnv("messaging.slack.tts_enabled")
	_ = v.BindEnv("messaging.slack.tts_provider")
	_ = v.BindEnv("messaging.slack.tts_voice")
	_ = v.BindEnv("messaging.slack.tts_max_chars")
	_ = v.BindEnv("messaging.slack.tts_moss_model_dir")
	_ = v.BindEnv("messaging.slack.tts_moss_voice")
	_ = v.BindEnv("messaging.slack.tts_moss_port")
	_ = v.BindEnv("messaging.slack.tts_moss_idle_timeout")
	_ = v.BindEnv("messaging.slack.tts_moss_cpu_threads")

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("config: environment override: %w", err)
	}

	// Normalize path fields AFTER env overrides, because Viper's env binding
	// writes raw values (e.g. "~/.hotplex/...") that bypass ExpandAndAbs.
	cfg.normalizePaths()

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
		if cfg.Security.JWTSecret == nil {
			if _, loaded := warnedEnvEntries.LoadOrStore("jwt_secret_invalid", true); !loaded {
				slog.Warn("config: HOTPLEX_JWT_SECRET set but invalid format (must be 32-byte raw or base64-encoded 32 bytes)", "length", len(secret))
			}
		}
	}

	// Numbered environment variables for slices (e.g. HOTPLEX_ADMIN_TOKEN_1..N)
	// This supports project conventions for secret rotation and .env clarity.
	cfg.Admin.Tokens = aggregateNumberedEnv(cfg.Admin.Tokens, "HOTPLEX_ADMIN_TOKEN_")
	cfg.Security.APIKeys = aggregateNumberedEnv(cfg.Security.APIKeys, "HOTPLEX_SECURITY_API_KEY_")

	// Messaging platform env var overrides.
	applyMessagingEnv(cfg)

	// Propagate shared messaging defaults to per-platform configs.
	// Priority: platform-level (YAML/env) > messaging-level > Default().
	propagateMessagingDefaults(cfg)

	// Expand env entries and drop those referencing unset vars without defaults.
	expanded := make([]string, 0, len(cfg.Worker.Environment))
	for _, e := range cfg.Worker.Environment {
		if entry, ok := expandEnvEntry(e); ok {
			expanded = append(expanded, entry)
		} else if _, loaded := warnedEnvEntries.LoadOrStore(e, true); !loaded {
			slog.Warn("excluding env entry: references unset variable", "entry", e)
		}
	}
	cfg.Worker.Environment = expanded

	cfg.normalizePaths()

	return cfg, nil
}

// normalizePaths expands ~ and resolves relative paths for all config path fields,
// and expands ${VAR} references in command templates.
func (c *Config) normalizePaths() {
	// 1. Expand environment variables in command templates and addresses.
	for _, ef := range []*string{
		&c.Gateway.Addr,
		&c.Admin.Addr,
		&c.Worker.ClaudeCode.Command,
		&c.Worker.OpenCodeServer.Command,
		&c.Messaging.Slack.LocalCmd,
		&c.Messaging.Feishu.LocalCmd,
		&c.Messaging.Feishu.MossModelDir,
		&c.Messaging.Slack.MossModelDir,
	} {
		if *ef != "" {
			*ef = ExpandEnv(*ef)
		}
	}

	// 2. Expand ~ and normalize paths.
	for _, pf := range []*string{
		&c.DB.Path,
		&c.DB.EventsPath,
		&c.Worker.DefaultWorkDir,
		&c.Worker.PIDDir,
		&c.AgentConfig.ConfigDir,
		&c.Messaging.Slack.WorkDir,
		&c.Messaging.Slack.MossModelDir,
		&c.Messaging.Feishu.WorkDir,
		&c.Messaging.Feishu.MossModelDir,
	} {
		if *pf != "" {
			absPath, err := ExpandAndAbs(*pf)
			if err != nil {
				if _, loaded := warnedEnvEntries.LoadOrStore("path:"+*pf, true); !loaded {
					slog.Warn("config: normalize path", "path", *pf, "err", err)
				}
				continue
			}
			*pf = absPath
		}
	}
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

// ResolvePlatformWorkDir returns the workdir for the given platform,
// falling back to Worker.DefaultWorkDir when the platform has no override.
func (c *Config) ResolvePlatformWorkDir(platform string) string {
	switch platform {
	case "slack":
		if c.Messaging.Slack.WorkDir != "" {
			return c.Messaging.Slack.WorkDir
		}
	case "feishu":
		if c.Messaging.Feishu.WorkDir != "" {
			return c.Messaging.Feishu.WorkDir
		}
	}
	return c.Worker.DefaultWorkDir
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
	// Slack
	applyPlatformEnv(&cfg.Messaging.Slack,
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN", "BotToken"},
			{"HOTPLEX_MESSAGING_SLACK_APP_TOKEN", "AppToken"},
			{"HOTPLEX_MESSAGING_SLACK_WORKER_TYPE", "WorkerType"},
			{"HOTPLEX_MESSAGING_SLACK_WORK_DIR", "WorkDir"},
			{"HOTPLEX_MESSAGING_SLACK_DM_POLICY", "DMPolicy"},
			{"HOTPLEX_MESSAGING_SLACK_GROUP_POLICY", "GroupPolicy"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_ENABLED", "Enabled"},
			{"HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION", "RequireMention"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_FROM", "AllowFrom"},
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM", "AllowDMFrom"},
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM", "AllowGroupFrom"},
		},
	)

	// Feishu
	applyPlatformEnv(&cfg.Messaging.Feishu,
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_APP_ID", "AppID"},
			{"HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "AppSecret"},
			{"HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE", "WorkerType"},
			{"HOTPLEX_MESSAGING_FEISHU_WORK_DIR", "WorkDir"},
			{"HOTPLEX_MESSAGING_FEISHU_DM_POLICY", "DMPolicy"},
			{"HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY", "GroupPolicy"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_ENABLED", "Enabled"},
			{"HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION", "RequireMention"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM", "AllowFrom"},
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM", "AllowDMFrom"},
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM", "AllowGroupFrom"},
		},
	)

	// Global messaging env vars.
	if v := os.Getenv("HOTPLEX_MESSAGING_TURN_SUMMARY_ENABLED"); v != "" {
		cfg.Messaging.TurnSummaryEnabled = strings.EqualFold(v, "true")
	}

	// Messaging-level shared defaults (propagated to platforms by propagateMessagingDefaults).
	msgStrs := []envMapping{
		{"HOTPLEX_MESSAGING_WORKER_TYPE", "WorkerType"},
		{"HOTPLEX_MESSAGING_STT_PROVIDER", "Provider"},
		{"HOTPLEX_MESSAGING_STT_LOCAL_CMD", "LocalCmd"},
		{"HOTPLEX_MESSAGING_TTS_PROVIDER", "TTSProvider"},
		{"HOTPLEX_MESSAGING_TTS_VOICE", "Voice"},
		{"HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR", "MossModelDir"},
		{"HOTPLEX_MESSAGING_TTS_MOSS_VOICE", "MossVoice"},
	}
	for _, m := range msgStrs {
		if v := os.Getenv(m.env); v != "" {
			setField(&cfg.Messaging, m.field, v)
		}
	}
	// Int fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MAX_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MaxChars = n
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MossPort = n
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_CPU_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MossCpuThreads = n
		}
	}
	// Duration fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_STT_LOCAL_IDLE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Messaging.LocalIdleTTL = d
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Messaging.MossIdleTimeout = d
		}
	}
	// Bool fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_ENABLED"); v != "" {
		cfg.Messaging.TTSEnabled = strings.EqualFold(v, "true")
	}
}

// envMapping maps an environment variable to a struct field name.
type envMapping struct{ env, field string }

// applyPlatformEnv applies string, bool, and slice env-var mappings to a target struct.
func applyPlatformEnv(target any, strs, bools, slices []envMapping) {
	for _, m := range strs {
		if v := os.Getenv(m.env); v != "" {
			setField(target, m.field, v)
		}
	}
	for _, m := range bools {
		if v := os.Getenv(m.env); v != "" {
			setBoolField(target, m.field, strings.EqualFold(v, "true"))
		}
	}
	for _, m := range slices {
		if v := os.Getenv(m.env); v != "" {
			setSliceField(target, m.field, v)
		}
	}
}

// setField sets a string field on a struct by name using reflection.
func setField(target any, field, value string) {
	v := reflect.ValueOf(target).Elem()
	v.FieldByName(field).SetString(value)
}

// setBoolField sets a bool field on a struct by name using reflection.
func setBoolField(target any, field string, value bool) {
	v := reflect.ValueOf(target).Elem()
	v.FieldByName(field).SetBool(value)
}

// setSliceField sets a []string field on a struct by name using reflection.
func setSliceField(target any, field, value string) {
	v := reflect.ValueOf(target).Elem()
	parts := strings.Split(value, ",")
	slice := make([]string, len(parts))
	for i, p := range parts {
		slice[i] = strings.TrimSpace(p)
	}
	v.FieldByName(field).Set(reflect.ValueOf(slice))
}

// decodeJWTSecret decodes a base64-encoded JWT secret.
// It supports both standard base64 and URL-safe base64 (with or without padding).
// Accepts >= 32 bytes: deriveECDSAP256Key truncates via copy to [32]byte,
// and RequireSecrets validates len >= 32. Canonical length is 32 bytes.
func decodeJWTSecret(secret string) []byte {
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil && len(decoded) >= 32 {
		return decoded
	}
	if decoded, err := base64.URLEncoding.DecodeString(secret); err == nil && len(decoded) >= 32 {
		return decoded
	}
	raw := []byte(secret)
	if len(raw) >= 32 {
		return raw
	}
	return nil
}
