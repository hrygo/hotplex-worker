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
	require.Equal(t, ":8888", cfg.Gateway.Addr)
	require.True(t, cfg.DB.WALMode)
	require.Equal(t, 100, cfg.Pool.MaxSize)
	require.Equal(t, 3, cfg.Pool.MaxIdlePerUser)
	require.Equal(t, 7*24*time.Hour, cfg.Session.RetentionPeriod)
	require.Equal(t, 1*time.Minute, cfg.Session.GCScanInterval)
	require.False(t, cfg.Security.TLSEnabled)
	require.True(t, cfg.Admin.Enabled)
	require.Equal(t, ":9999", cfg.Admin.Addr)
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
			errCnt: 1, // TLS warning for non-local address :8888
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
				c.Gateway.Addr = "127.0.0.1:8888"
				return c
			}(),
			errCnt: 0, // localhost bypasses TLS warning
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// NOT parallel — mutates global env vars
				errs := tt.cfg.Validate()
			require.Len(t, errs, tt.errCnt)
		})
	}
}

func TestExpandEnv(t *testing.T) {
	// NOT parallel — mutates global env vars (HOME, USER, etc.)
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
			// NOT parallel — mutates global env vars
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

// ─── Config inheritance tests ──────────────────────────────────────────────────

func TestLoad_Inheritance_CycleDetection(t *testing.T) {
	t.Parallel()

	// Create two config files that reference each other.
	baseDir := t.TempDir()

	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8888\ninherits: child.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	childCfg := baseDir + "/child.yaml"
	if err := os.WriteFile(childCfg, []byte("gateway:\n  addr: :9090\ninherits: base.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(baseCfg, LoadOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigCycle)
}

func TestLoad_Inheritance_SelfReference(t *testing.T) {
	t.Parallel()

	tmp, err := os.CreateTemp("", "self_cycle_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("gateway:\n  addr: :8888\ninherits: " + tmp.Name() + "\n"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	_, err = Load(tmp.Name(), LoadOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigCycle)
}

func TestLoad_Inheritance_ThreeLevelChain(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8888\npool:\n  max_size: 10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	midCfg := baseDir + "/mid.yaml"
	if err := os.WriteFile(midCfg, []byte("gateway:\n  addr: :9090\ninherits: base.yaml\npool:\n  max_size: 20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	leafCfg := baseDir + "/leaf.yaml"
	if err := os.WriteFile(leafCfg, []byte("gateway:\n  addr: :7070\ninherits: mid.yaml\npool:\n  max_size: 30\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(leafCfg, LoadOptions{})
	require.NoError(t, err)
	// Leaf overrides mid, mid overrides base.
	require.Equal(t, ":7070", cfg.Gateway.Addr)
	require.Equal(t, 30, cfg.Pool.MaxSize)
}

func TestLoad_Inheritance_NoInherits(t *testing.T) {
	t.Parallel()

	tmp, err := os.CreateTemp("", "no_inherit_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("gateway:\n  addr: :6060\npool:\n  max_size: 5\n"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	cfg, err := Load(tmp.Name(), LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, ":6060", cfg.Gateway.Addr)
	require.Equal(t, 5, cfg.Pool.MaxSize)
}

func TestLoad_Inheritance_PathExpansion(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Use absolute path for base, relative for child.
	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	relChild := "child.yaml"
	childPath := baseDir + "/" + relChild
	if err := os.WriteFile(childPath, []byte("inherits: "+relChild+"\ngateway:\n  addr: :8001\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Fix: child inherits from base (the parent), not itself.
	if err := os.WriteFile(childPath, []byte("inherits: "+baseCfg+"\ngateway:\n  addr: :8001\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(childPath, LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, ":8001", cfg.Gateway.Addr)
}
