package checkers

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestExtractPortAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		key  string
		want string
	}{
		{"simple value", "gateway.addr: :8888\n", "gateway.addr", ":8888"},
		{"quoted value", `gateway.addr: ":8888"`, "gateway.addr", ":8888"},
		{"single quoted", `gateway.addr: ':9999'`, "gateway.addr", ":9999"},
		{"missing key", "other: val\n", "gateway.addr", ""},
		{"indented", "  gateway.addr: :7777\n", "gateway.addr", ":7777"},
		{"with spaces", "gateway.addr:   :1234  \n", "gateway.addr", ":1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractPortAddr(tt.yaml, tt.key)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestReplaceYAMLValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		yaml  string
		key   string
		value string
		want  string
	}{
		{
			"simple replace",
			"gateway.addr: :8888\n",
			"gateway.addr", ":9999",
			`gateway.addr: ":9999"` + "\n",
		},
		{
			"indented replace",
			"  gateway.addr: :8888\n",
			"gateway.addr", ":7777",
			`  gateway.addr: ":7777"` + "\n",
		},
		{
			"missing key returns unchanged",
			"other: val\n",
			"gateway.addr", ":9999",
			"other: val\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := replaceYAMLValue(tt.yaml, tt.key, tt.value)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFixConfigValues(t *testing.T) {
	setupTestConfigDir(t)

	cfgContent := "gateway:\n  addr: \":99999\"\nadmin:\n  enabled: true\n  addr: \":88888\"\ndb:\n  path: \"\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(cfgContent), 0o600))

	cfg, err := config.Load(configPath, config.LoadOptions{})
	require.NoError(t, err)

	err = fixConfigValues(cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway")
}
