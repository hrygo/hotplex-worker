package checkers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type pathPerm struct {
	path string
	perm os.FileMode
}

// ─── security.jwt_strength ────────────────────────────────────────────────────

type jwtStrengthChecker struct{}

func (c jwtStrengthChecker) Name() string     { return "security.jwt_strength" }
func (c jwtStrengthChecker) Category() string { return "security" }
func (c jwtStrengthChecker) Check(ctx context.Context) cli.Diagnostic {
	secret := resolveJWTSecret()

	if len(secret) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "JWT secret is empty",
			FixHint:  "Generate a strong JWT secret",
			FixFunc:  fixJWTStrength,
		}
	}

	if len(secret) < 32 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  fmt.Sprintf("JWT secret too short (%d bytes, need >= 32)", len(secret)),
			FixHint:  "Generate a strong JWT secret (>= 32 bytes)",
			FixFunc:  fixJWTStrength,
		}
	}

	allSame := true
	for i := 1; i < len(secret); i++ {
		if secret[i] != secret[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "JWT secret has no entropy (all same character)",
			FixHint:  "Generate a strong JWT secret",
			FixFunc:  fixJWTStrength,
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "JWT secret strong enough",
	}
}

func resolveJWTSecret() []byte {
	if val := os.Getenv("JWT_SECRET"); val != "" {
		return []byte(val)
	}
	if val := os.Getenv("HOTPLEX_JWT_SECRET"); val != "" {
		return decodeBase64Secret(val)
	}
	if configPath != "" {
		cfg, err := config.Load(configPath, config.LoadOptions{})
		if err == nil && len(cfg.Security.JWTSecret) > 0 {
			return cfg.Security.JWTSecret
		}
	}
	return nil
}

func decodeBase64Secret(s string) []byte {
	if d, err := base64.StdEncoding.DecodeString(s); err == nil {
		return d
	}
	if d, err := base64.URLEncoding.DecodeString(s); err == nil {
		return d
	}
	return []byte(s)
}

func fixJWTStrength() error {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generate secret: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	if err := writeEnvVar("HOTPLEX_JWT_SECRET", encoded); err != nil {
		return err
	}
	return unsetEnvVar("JWT_SECRET")
}

func init() {
	cli.DefaultRegistry.Register(jwtStrengthChecker{})
}

// ─── security.admin_token ─────────────────────────────────────────────────────

type adminTokenChecker struct{}

func (c adminTokenChecker) Name() string     { return "security.admin_token" }
func (c adminTokenChecker) Category() string { return "security" }
func (c adminTokenChecker) Check(ctx context.Context) cli.Diagnostic {
	token := resolveAdminToken()

	if token == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Admin token is empty",
			FixHint:  "Generate a secure admin token",
			FixFunc:  fixAdminToken,
		}
	}

	lower := strings.ToLower(token)
	if lower == "admin" || lower == "default" || lower == "password" || lower == "changeme" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Admin token uses a weak default value",
			FixHint:  "Generate a secure admin token",
			FixFunc:  fixAdminToken,
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Admin token present and not weak",
	}
}

func resolveAdminToken() string {
	if val := os.Getenv("ADMIN_TOKEN"); val != "" {
		return val
	}
	if val := os.Getenv("HOTPLEX_ADMIN_TOKEN_1"); val != "" {
		return val
	}
	if configPath != "" {
		cfg, err := config.Load(configPath, config.LoadOptions{})
		if err == nil && len(cfg.Admin.Tokens) > 0 {
			return cfg.Admin.Tokens[0]
		}
	}
	return ""
}

func fixAdminToken() error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := fmt.Sprintf("%x", b)

	if err := unsetEnvVar("ADMIN_TOKEN"); err != nil {
		return err
	}
	return writeEnvVar("HOTPLEX_ADMIN_TOKEN_1", token)
}

func init() {
	cli.DefaultRegistry.Register(adminTokenChecker{})
}

// ─── security.file_permissions ────────────────────────────────────────────────

type filePermsChecker struct{}

func (c filePermsChecker) Name() string     { return "security.file_permissions" }
func (c filePermsChecker) Category() string { return "security" }
func (c filePermsChecker) Check(ctx context.Context) cli.Diagnostic {
	var issues []string
	var toFix []pathPerm

	checkPerm := func(path string, target os.FileMode) {
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			issues = append(issues, fmt.Sprintf("%s is world-accessible (%o)", path, mode))
			toFix = append(toFix, pathPerm{path, target})
		}
	}

	if configPath != "" {
		checkPerm(filepath.Dir(configPath), 0o700)
		checkPerm(configPath, 0o600)

		cfg, err := config.Load(configPath, config.LoadOptions{})
		if err == nil && cfg.DB.Path != "" {
			checkPerm(filepath.Dir(cfg.DB.Path), 0o700)
		}

		checkPerm(envFilePath(), 0o600)
	} else {
		checkPerm(".env", 0o600)
	}

	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Insecure permissions: " + strings.Join(issues, "; "),
			FixHint:  "Restrict permissions (dirs: 0700, files: 0600)",
			FixFunc: func() error {
				for _, p := range toFix {
					if err := os.Chmod(p.path, p.perm); err != nil {
						return err
					}
				}
				return nil
			},
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "File permissions secure",
	}
}

func init() {
	cli.DefaultRegistry.Register(filePermsChecker{})
}

// ─── security.env_in_git ──────────────────────────────────────────────────────

type envInGitChecker struct{}

func (c envInGitChecker) Name() string     { return "security.env_in_git" }
func (c envInGitChecker) Category() string { return "security" }
func (c envInGitChecker) Check(ctx context.Context) cli.Diagnostic {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--error-unmatch", ".env")
	cmd.Stderr = nil
	err := cmd.Run()

	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  ".env is not tracked by git",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  ".env is tracked by git (secrets may be exposed)",
		FixHint:  "Add .env to .gitignore",
		FixFunc:  fixEnvInGit,
	}
}

func fixEnvInGit() error {
	gitignorePath := ".gitignore"

	data, err := os.ReadFile(gitignorePath)
	if err == nil && strings.Contains(string(data), ".env") {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == ".env" || trimmed == "*.env" {
				return nil
			}
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer func() { _ = f.Close() }()

	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	_, err = f.WriteString(".env\n")
	return err
}

func init() {
	cli.DefaultRegistry.Register(envInGitChecker{})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func envFilePath() string {
	if configPath != "" {
		return filepath.Join(filepath.Dir(configPath), ".env")
	}
	return ".env"
}

func writeEnvVar(key, value string) error {
	envPath := envFilePath()
	f, err := os.OpenFile(envPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open .env: %w", err)
	}
	defer func() { _ = f.Close() }()

	line := fmt.Sprintf("%s=%s\n", key, value)
	_, err = f.WriteString(line)
	return err
}

func unsetEnvVar(key string) error {
	envPath := envFilePath()
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .env: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	prefix := key + "="
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			continue
		}
		cleaned = append(cleaned, line)
	}

	result := strings.Join(cleaned, "\n")
	result = strings.TrimRight(result, "\n") + "\n"
	return os.WriteFile(envPath, []byte(result), 0o600)
}
