package slackcli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("empty token returns error", func(t *testing.T) {
		_, err := NewClient("")
		require.Error(t, err)
		require.Contains(t, err.Error(), "bot token not configured")
	})

	t.Run("valid token creates client", func(t *testing.T) {
		client, err := NewClient("xoxb-fake-token")
		require.NoError(t, err)
		require.NotNil(t, client)
	})
}

func TestResolveChannel(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		envVal  string
		want    string
		wantErr bool
	}{
		{"flag takes precedence", "D123", "D456", "D123", false},
		{"env fallback", "", "D456", "D456", false},
		{"both empty returns error", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOTPLEX_SLACK_CHANNEL_ID", tt.envVal)
			got, err := ResolveChannel(tt.flag)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "--channel is required")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestResolveThreadTS(t *testing.T) {
	tests := []struct {
		name   string
		flag   string
		envVal string
		want   string
	}{
		{"flag takes precedence", "1234.5678", "9999.0000", "1234.5678"},
		{"env fallback", "", "9999.0000", "9999.0000"},
		{"both empty returns empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOTPLEX_SLACK_THREAD_TS", tt.envVal)
			got := ResolveThreadTS(tt.flag)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	t.Run("does not overwrite existing env vars", func(t *testing.T) {
		t.Setenv("EXISTING_KEY", "original")
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")
		require.NoError(t, os.WriteFile(envPath, []byte("EXISTING_KEY=overwritten\nNEW_KEY=new_value\n"), 0o644))

		loadEnvFile(dir)

		require.Equal(t, "original", os.Getenv("EXISTING_KEY"))
		require.Equal(t, "new_value", os.Getenv("NEW_KEY"))
	})

	t.Run("skips comments and empty lines", func(t *testing.T) {
		dir := t.TempDir()
		envPath := filepath.Join(dir, ".env")
		require.NoError(t, os.WriteFile(envPath, []byte("# comment\n\nKEY=val\n"), 0o644))

		loadEnvFile(dir)
		require.Equal(t, "val", os.Getenv("KEY"))
	})

	t.Run("missing file is no-op", func(t *testing.T) {
		dir := t.TempDir()
		loadEnvFile(dir)
	})
}
