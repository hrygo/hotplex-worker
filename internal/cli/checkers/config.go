package checkers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

var configPath string

// SetConfigPath sets the config file path for all config checkers.
func SetConfigPath(path string) {
	configPath = path
}

// ─── config.exists ────────────────────────────────────────────────────────────

type configExistsChecker struct{}

func (c configExistsChecker) Name() string     { return "config.exists" }
func (c configExistsChecker) Category() string { return "config" }
func (c configExistsChecker) Check(ctx context.Context) cli.Diagnostic {
	if configPath == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Config path not set",
			FixHint:  "Set config path via SetConfigPath() before running checks",
		}
	}

	_, err := os.Stat(configPath)
	if err == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Config file exists",
			Detail:   configPath,
		}
	}

	if os.IsNotExist(err) {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Config file missing",
			Detail:   configPath,
			FixHint:  "Create config file or run onboard",
			FixFunc:  fixConfigExists,
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "Cannot access config file",
		Detail:   err.Error(),
	}
}

func fixConfigExists() error {
	templatePath := filepath.Join("configs", "config.yaml")
	if _, err := os.Stat(templatePath); err == nil {
		data, err := os.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("read template: %w", err)
		}
		return os.WriteFile(configPath, data, 0o600)
	}

	minimal := `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
db:
  path: "~/.hotplex/data/hotplex.db"
worker:
  type: "claude_code"
log:
  level: "info"
`
	return os.WriteFile(configPath, []byte(minimal), 0o600)
}

func init() {
	cli.DefaultRegistry.Register(configExistsChecker{})
}

// ─── config.syntax ────────────────────────────────────────────────────────────

type configSyntaxChecker struct{}

func (c configSyntaxChecker) Name() string     { return "config.syntax" }
func (c configSyntaxChecker) Category() string { return "config" }
func (c configSyntaxChecker) Check(ctx context.Context) cli.Diagnostic {
	if configPath == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Config path not set",
		}
	}

	_, err := config.Load(configPath, config.LoadOptions{})
	if err == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Config syntax valid",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "Config syntax error",
		Detail:   err.Error(),
	}
}

func init() {
	cli.DefaultRegistry.Register(configSyntaxChecker{})
}

// ─── config.required ──────────────────────────────────────────────────────────

type configRequiredChecker struct{}

func (c configRequiredChecker) Name() string     { return "config.required" }
func (c configRequiredChecker) Category() string { return "config" }
func (c configRequiredChecker) Check(ctx context.Context) cli.Diagnostic {
	if configPath == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Config path not set",
		}
	}

	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Cannot load config for validation",
			Detail:   err.Error(),
		}
	}

	var missing []string
	if len(cfg.Security.JWTSecret) == 0 {
		missing = append(missing, "security.jwt_secret")
	}

	hasWorker := cfg.Messaging.Slack.Enabled || cfg.Messaging.Feishu.Enabled
	if !hasWorker {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "No messaging platform enabled",
			Detail:   "Slack and Feishu are both disabled. Enable one to use messaging features.",
			FixHint:  "Run onboard to configure Slack or Feishu",
		}
	}

	if len(missing) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Missing required fields",
			Detail:   strings.Join(missing, ", "),
			FixHint:  "Run onboard to configure required fields",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Required fields present",
	}
}

func init() {
	cli.DefaultRegistry.Register(configRequiredChecker{})
}

// ─── config.values ────────────────────────────────────────────────────────────

type configValuesChecker struct{}

func (c configValuesChecker) Name() string     { return "config.values" }
func (c configValuesChecker) Category() string { return "config" }
func (c configValuesChecker) Check(ctx context.Context) cli.Diagnostic {
	if configPath == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Config path not set",
		}
	}

	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Cannot load config",
			Detail:   err.Error(),
		}
	}

	var issues []string

	gatewayPort := extractPort(cfg.Gateway.Addr)
	if gatewayPort < 1 || gatewayPort > 65535 {
		issues = append(issues, fmt.Sprintf("gateway.addr port out of range: %s", cfg.Gateway.Addr))
	}

	adminPort := extractPort(cfg.Admin.Addr)
	if cfg.Admin.Enabled && (adminPort < 1 || adminPort > 65535) {
		issues = append(issues, fmt.Sprintf("admin.addr port out of range: %s", cfg.Admin.Addr))
	}

	if cfg.DB.Path != "" {
		dir := filepath.Dir(cfg.DB.Path)
		if _, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
			issues = append(issues, fmt.Sprintf("db.path directory does not exist: %s", dir))
		}
	}

	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Invalid config values",
			Detail:   strings.Join(issues, "; "),
			FixHint:  "Reset to defaults",
			FixFunc: func() error {
				return fixConfigValues(cfg)
			},
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Config values valid",
	}
}

func extractPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return p
}

func fixConfigValues(cfg *config.Config) error {
	defaults := config.Default()
	needsFix := false

	if gatewayPort := extractPort(cfg.Gateway.Addr); gatewayPort < 1 || gatewayPort > 65535 {
		cfg.Gateway.Addr = defaults.Gateway.Addr
		needsFix = true
	}
	if cfg.Admin.Enabled {
		if adminPort := extractPort(cfg.Admin.Addr); adminPort < 1 || adminPort > 65535 {
			cfg.Admin.Addr = defaults.Admin.Addr
			needsFix = true
		}
	}
	if cfg.DB.Path != "" {
		dir := filepath.Dir(cfg.DB.Path)
		if _, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
			cfg.DB.Path = defaults.DB.Path
			needsFix = true
		}
	}
	if !needsFix {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	yaml := string(data)
	if cfg.Gateway.Addr != extractPortAddr(yaml, "gateway.addr") {
		yaml = replaceYAMLValue(yaml, "gateway.addr", cfg.Gateway.Addr)
	}
	if cfg.Admin.Addr != extractPortAddr(yaml, "admin.addr") {
		yaml = replaceYAMLValue(yaml, "admin.addr", cfg.Admin.Addr)
	}
	if cfg.DB.Path != defaults.DB.Path {
		yaml = replaceYAMLValue(yaml, "db.path", cfg.DB.Path)
	}

	return os.WriteFile(configPath, []byte(yaml), 0o600)
}

func extractPortAddr(yaml, key string) string {
	lines := strings.Split(yaml, "\n")
	prefix := key + ":"
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			val := strings.TrimSpace(trimmed[len(prefix):])
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

func replaceYAMLValue(yaml, key, value string) string {
	lines := strings.Split(yaml, "\n")
	prefix := key + ":"
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			leading := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = leading + prefix + " " + strconv.Quote(value)
			break
		}
	}
	return strings.Join(lines, "\n")
}

func init() {
	cli.DefaultRegistry.Register(configValuesChecker{})
}

// ─── config.env_vars ──────────────────────────────────────────────────────────

type configEnvVarsChecker struct{}

func (c configEnvVarsChecker) Name() string     { return "config.env_vars" }
func (c configEnvVarsChecker) Category() string { return "config" }
func (c configEnvVarsChecker) Check(ctx context.Context) cli.Diagnostic {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = os.Getenv("HOTPLEX_JWT_SECRET")
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		adminToken = os.Getenv("HOTPLEX_ADMIN_TOKEN_1")
	}

	var missing []string
	if jwtSecret == "" {
		missing = append(missing, "JWT_SECRET (or HOTPLEX_JWT_SECRET)")
	}
	if adminToken == "" {
		missing = append(missing, "ADMIN_TOKEN (or HOTPLEX_ADMIN_TOKEN_1)")
	}

	if len(missing) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Critical environment variables not set",
			Detail:   strings.Join(missing, ", "),
			FixHint:  "Write missing vars to .env file",
			FixFunc:  fixEnvVars,
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Critical environment variables present",
	}
}

func fixEnvVars() error {
	existing := make(map[string]bool)
	envPath := envFilePath()
	if data, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.Index(line, "="); idx > 0 {
				existing[strings.TrimSpace(line[:idx])] = true
			}
		}
	}

	var lines []string

	if !existing["HOTPLEX_JWT_SECRET"] {
		b := make([]byte, 48)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate JWT secret: %w", err)
		}
		lines = append(lines, fmt.Sprintf("HOTPLEX_JWT_SECRET=%s", base64.StdEncoding.EncodeToString(b)))
	}

	if !existing["HOTPLEX_ADMIN_TOKEN_1"] {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate admin token: %w", err)
		}
		lines = append(lines, fmt.Sprintf("HOTPLEX_ADMIN_TOKEN_1=%x", b))
	}

	if len(lines) == 0 {
		return nil
	}

	f, err := os.OpenFile(envPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open .env: %w", err)
	}
	defer func() { _ = f.Close() }()

	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line); err != nil {
			return fmt.Errorf("write .env: %w", err)
		}
	}

	return nil
}

func init() {
	cli.DefaultRegistry.Register(configEnvVarsChecker{})
}
