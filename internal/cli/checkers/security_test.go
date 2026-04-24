package checkers

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

// Tests using t.Setenv or modifying configPath cannot use t.Parallel because:
//   - t.Setenv is incompatible with t.Parallel in Go's testing framework
//   - configPath is a package-level mutable variable subject to data races

func TestJWTStrength_Empty(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("HOTPLEX_JWT_SECRET", "")
	defer resetConfigPath()
	SetConfigPath("")

	c := jwtStrengthChecker{}
	d := c.Check(context.Background())

	require.Contains(t, []cli.Status{cli.StatusFail, cli.StatusPass}, d.Status)
	require.Equal(t, "security.jwt_strength", d.Name)
}

func TestJWTStrength_TooShort(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")

	c := jwtStrengthChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "too short")
}

func TestJWTStrength_Strong(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("aBcDeFgHiJkLmNoPqRsTuVwXyZ012345", 2))

	c := jwtStrengthChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestJWTStrength_NoEntropy(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", 48))

	c := jwtStrengthChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "entropy")
}

func TestJWTStrength_FixFunc(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")

	c := jwtStrengthChecker{}
	d := c.Check(context.Background())

	if d.FixFunc != nil {
		require.NotNil(t, d.FixFunc)
	}
}

func TestAdminToken_Empty(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "")
	defer resetConfigPath()
	SetConfigPath("")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "empty")
}

func TestAdminToken_WeakDefault(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"admin", "admin"},
		{"default", "default"},
		{"password", "password"},
		{"changeme", "changeme"},
		{"ADMIN uppercase", "ADMIN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ADMIN_TOKEN", tt.token)

			c := adminTokenChecker{}
			d := c.Check(context.Background())

			require.Equal(t, cli.StatusFail, d.Status)
			require.Contains(t, d.Message, "weak default")
		})
	}
}

func TestAdminToken_Strong(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "a1b2c3d4e5f6a1b2c3d4e5f6")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestAdminToken_FromHotplexEnv(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("HOTPLEX_ADMIN_TOKEN_1", "strong-token-from-env")

	c := adminTokenChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestDecodeBase64Secret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantFn func(t *testing.T, got []byte)
	}{
		{
			name:  "valid standard base64",
			input: base64.StdEncoding.EncodeToString([]byte("hello world")),
			wantFn: func(t *testing.T, got []byte) {
				require.Equal(t, []byte("hello world"), got)
			},
		},
		{
			name:  "valid URL base64",
			input: base64.URLEncoding.EncodeToString([]byte("url safe")),
			wantFn: func(t *testing.T, got []byte) {
				require.Equal(t, []byte("url safe"), got)
			},
		},
		{
			name:  "invalid base64 returns raw string",
			input: "not!base64!!!",
			wantFn: func(t *testing.T, got []byte) {
				require.Equal(t, []byte("not!base64!!!"), got)
			},
		},
		{
			name:  "empty string",
			input: "",
			wantFn: func(t *testing.T, got []byte) {
				require.Equal(t, []byte(""), got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decodeBase64Secret(tt.input)
			tt.wantFn(t, got)
		})
	}
}

func TestFilePerms(t *testing.T) {
	c := filePermsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "security.file_permissions", d.Name)
	require.Equal(t, "security", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusWarn}, d.Status)
}

func TestEnvInGit(t *testing.T) {
	c := envInGitChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "security.env_in_git", d.Name)
	require.Equal(t, "security", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}
