package checkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

// resetConfigPath is a helper that resets the package-level configPath after a test.
func resetConfigPath() { SetConfigPath("") }

// Tests using SetConfigPath cannot use t.Parallel because configPath is a
// package-level mutable variable shared with checkers in other files
// (e.g. filePermsChecker, resolveJWTSecret) that also read it.

func TestSetConfigPath(t *testing.T) {
	SetConfigPath("/some/path/config.yaml")
	require.Equal(t, "/some/path/config.yaml", configPath)
	resetConfigPath()
	require.Equal(t, "", configPath)
}

func TestConfigExists_Missing(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("/nonexistent/config.yaml")

	c := configExistsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.NotNil(t, d.FixFunc)
}

func TestConfigExists_Present(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: val\n"), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configExistsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Equal(t, "config.exists", d.Name)
}

func TestConfigExists_EmptyPath(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("")

	c := configExistsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "not set")
}

func TestConfigExists_FixFunc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	defer resetConfigPath()
	SetConfigPath(path)

	c := configExistsChecker{}
	d := c.Check(context.Background())

	require.NotNil(t, d.FixFunc)
	require.NoError(t, d.FixFunc())

	_, err := os.Stat(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestConfigSyntax_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("invalid: [yaml: {\n"), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configSyntaxChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Equal(t, "config.syntax", d.Name)
	require.Contains(t, d.Message, "syntax")
}

func TestConfigSyntax_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "data/test.db"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configSyntaxChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestConfigSyntax_EmptyPath(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("")

	c := configSyntaxChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}

func TestConfigValues_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `gateway:
  addr: ":99999"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "data/test.db"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configValuesChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Detail, "gateway.addr port out of range")
}

func TestConfigValues_ValidPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Use absolute db path under temp dir so the directory exists.
	dbDir := filepath.Join(dir, "data")
	require.NoError(t, os.MkdirAll(dbDir, 0o755))
	content := "gateway:\n  addr: \":8888\"\nadmin:\n  addr: \":9999\"\n  enabled: true\ndb:\n  path: \"" + filepath.Join(dbDir, "test.db") + "\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configValuesChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestConfigValues_DBPathNonexistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "/nonexistent/dir/test.db"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configValuesChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Detail, "db.path directory does not exist")
}

func TestExtractPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{":8888", 8888},
		{":0", 0},
		{"invalid", 0},
		{"host:9999", 9999},
		{"", 0},
		{":65535", 65535},
		{"127.0.0.1:3000", 3000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := extractPort(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestConfigEnvVars_Missing(t *testing.T) {
	// t.Setenv + package-level configPath — cannot use t.Parallel.
	t.Setenv("JWT_SECRET", "")
	t.Setenv("HOTPLEX_JWT_SECRET", "")
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "")

	c := configEnvVarsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.NotNil(t, d.FixFunc)
	require.Contains(t, d.Detail, "JWT_SECRET")
	require.Contains(t, d.Detail, "ADMIN_TOKEN")
}

func TestConfigEnvVars_Present(t *testing.T) {
	// t.Setenv — cannot use t.Parallel.
	t.Setenv("JWT_SECRET", "my-jwt-secret-value")
	t.Setenv("ADMIN_TOKEN", "my-admin-token-value")

	c := configEnvVarsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestConfigEnvVars_FixFunc(t *testing.T) {
	// t.Setenv + os.Chdir — cannot use t.Parallel.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	t.Setenv("JWT_SECRET", "")
	t.Setenv("HOTPLEX_JWT_SECRET", "")
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "")

	c := configEnvVarsChecker{}
	d := c.Check(context.Background())

	require.NotNil(t, d.FixFunc)
	require.NoError(t, d.FixFunc())

	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	require.Contains(t, string(data), "HOTPLEX_JWT_SECRET=")
	require.Contains(t, string(data), "HOTPLEX_ADMIN_TOKEN_1=")
}

func TestConfigRequired_NoMessaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "data/test.db"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configRequiredChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.Contains(t, d.Detail, "Slack and Feishu")
}

func TestConfigRequired_MissingJWT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	dbDir := filepath.Join(dir, "data")
	require.NoError(t, os.MkdirAll(dbDir, 0o755))
	content := "gateway:\n  addr: \":8888\"\nadmin:\n  addr: \":9999\"\n  enabled: true\ndb:\n  path: \"" + filepath.Join(dbDir, "test.db") + "\"\nmessaging:\n  slack:\n    enabled: true\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configRequiredChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Detail, "security.jwt_secret")
}

func TestConfigRequired_AllPresent(t *testing.T) {
	// t.Setenv — cannot use t.Parallel.
	// JWTSecret has mapstructure:"-" and is loaded via SecretsProvider, not from
	// config file. The checker calls config.Load with empty LoadOptions, so
	// JWTSecret is always empty. This test only verifies that the messaging
	// check passes when Slack is enabled — JWT will still be reported missing.
	t.Setenv("JWT_SECRET", "dGVzdC1zZWNyZXQta2V5LWZvci1qd3Q=")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	dbDir := filepath.Join(dir, "data")
	require.NoError(t, os.MkdirAll(dbDir, 0o755))
	content := "gateway:\n  addr: \":8888\"\nadmin:\n  addr: \":9999\"\n  enabled: true\ndb:\n  path: \"" + filepath.Join(dbDir, "test.db") + "\"\nmessaging:\n  slack:\n    enabled: true\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	defer resetConfigPath()
	SetConfigPath(path)

	c := configRequiredChecker{}
	d := c.Check(context.Background())

	// Messaging is enabled so no messaging warning; JWT is still missing
	// because the checker has no SecretsProvider.
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Detail, "security.jwt_secret")
}

func TestConfigRequired_EmptyPath(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("")

	c := configRequiredChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}
