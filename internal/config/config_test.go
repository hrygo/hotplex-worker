package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	cfg := Default()
	require.NotNil(t, cfg)
	require.Equal(t, ":8080", cfg.Gateway.Addr)
	require.True(t, cfg.DB.WALMode)
	require.Equal(t, 100, cfg.Pool.MaxSize)
	require.Equal(t, 3, cfg.Pool.MaxIdlePerUser)
	require.Equal(t, 7*24*time.Hour, cfg.Session.RetentionPeriod)
	require.Equal(t, 1*time.Minute, cfg.Session.GCScanInterval)
	require.False(t, cfg.Security.TLSEnabled)
	require.True(t, cfg.Admin.Enabled)
	require.Equal(t, ":9080", cfg.Admin.Addr)
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    Config
		errCnt int
	}{
		{
			name:   "valid defaults",
			cfg:    *Default(),
			errCnt: 1, // TLS warning for non-local address :8080
		},
		{
			name: "missing gateway addr",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = ""
				return c
			}(),
			errCnt: 2, // missing addr + TLS warning
		},
		{
			name: "missing db path",
			cfg: func() Config {
				c := *Default()
				c.DB.Path = ""
				return c
			}(),
			errCnt: 2, // missing path + TLS warning
		},
		{
			name: "non-positive retention period",
			cfg: func() Config {
				c := *Default()
				c.Session.RetentionPeriod = 0
				return c
			}(),
			errCnt: 2, // invalid retention + TLS warning
		},
		{
			name: "non-positive pool max size",
			cfg: func() Config {
				c := *Default()
				c.Pool.MaxSize = 0
				return c
			}(),
			errCnt: 2, // invalid pool + TLS warning
		},
		{
			name: "multiple errors",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = ""
				c.DB.Path = ""
				return c
			}(),
			errCnt: 3, // missing addr + missing path + TLS warning
		},
		{
			name: "localhost TLS bypass",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = "127.0.0.1:8080"
				return c
			}(),
			errCnt: 0, // localhost bypasses TLS warning
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := tt.cfg.Validate()
			require.Len(t, errs, tt.errCnt)
		})
	}
}

func TestExpandEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		setup  func()
		verify func(string)
	}{
		{
			name:  "no variables",
			input: "hello world",
			setup: func() {},
			verify: func(got string) {
				require.Equal(t, "hello world", got)
			},
		},
		{
			name:  "simple variable",
			input: "path=${TEST_MY_HOME}",
			setup: func() { os.Setenv("TEST_MY_HOME", "/home/user") },
			verify: func(got string) {
				require.Equal(t, "path=/home/user", got)
			},
		},
		{
			name:  "variable with default",
			input: "path=${UNSET_VAR:-/default/path}",
			setup:  func() {},
			verify: func(got string) {
				require.Equal(t, "path=/default/path", got)
			},
		},
		{
			name:  "variable with non-empty default",
			input: "token=${MY_TOKEN:-fallback}",
			setup:  func() {},
			verify: func(got string) {
				require.Equal(t, "token=fallback", got)
			},
		},
		{
			name:  "multiple variables",
			input: "${HOME}/${USER}/${PWD}",
			setup:  func() {},
			verify: func(got string) {
				// All three are typically set in a shell environment, so expect expansion
				require.NotContains(t, got, "${HOME}")
				require.NotContains(t, got, "${USER}")
				require.NotContains(t, got, "${PWD}")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			os.Unsetenv("HOME")
			os.Unsetenv("USER")
			os.Unsetenv("PWD")
			os.Unsetenv("UNSET_VAR")
			os.Unsetenv("MY_TOKEN")
			os.Unsetenv("TEST_MY_HOME")
			tt.setup()
			got := ExpandEnv(tt.input)
			tt.verify(got)
		})
	}
}

func TestEnvSecretsProvider(t *testing.T) {
	t.Parallel()

	os.Setenv("TEST_SECRET", "secret123")
	defer os.Unsetenv("TEST_SECRET")

	p := NewEnvSecretsProvider()
	require.Equal(t, "secret123", p.Get("TEST_SECRET"))
	require.Empty(t, p.Get("NONEXISTENT"))
}

func TestChainedSecretsProvider(t *testing.T) {
	t.Parallel()

	p := NewChainedSecretsProvider(
		&staticProvider{data: map[string]string{"key1": "from-first"}},
		&staticProvider{data: map[string]string{"key1": "from-second", "key2": "from-second"}},
	)

	require.Equal(t, "from-first", p.Get("key1"))  // first provider wins
	require.Equal(t, "from-second", p.Get("key2")) // only in second
	require.Empty(t, p.Get("key3"))                 // neither has it
}

type staticProvider struct {
	data map[string]string
}

func (p *staticProvider) Get(key string) string {
	return p.data[key]
}

func TestChainedSecretsProvider_Empty(t *testing.T) {
	t.Parallel()

	p := NewChainedSecretsProvider()
	require.Empty(t, p.Get("anything"))
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/config.yaml", LoadOptions{})
	require.Error(t, err)
}

func TestMustLoad_Panic(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		MustLoad("/nonexistent/config.yaml", LoadOptions{})
	})
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	// Create a temp file
	tmp, err := os.CreateTemp("", "test_config_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	content := []byte("gateway:\n  addr: :9090\n")
	_, err = tmp.Write(content)
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	data, err := ReadFile(tmp.Name())
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestReadFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadFile("/nonexistent/file.yaml")
	require.Error(t, err)
}
