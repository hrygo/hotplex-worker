package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSystemdUnit_SystemLevel(t *testing.T) {
	t.Parallel()
	result := BuildSystemdUnit(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/etc/hotplex/config.yaml",
		Level:      LevelSystem,
		Name:       "hotplex",
	}, "/root")

	require.Contains(t, result, "[Unit]")
	require.Contains(t, result, "[Service]")
	require.Contains(t, result, "[Install]")
	require.Contains(t, result, "ExecStart=/usr/local/bin/hotplex gateway start --config /etc/hotplex/config.yaml")
	require.Contains(t, result, "User=hotplex")
	require.Contains(t, result, "WantedBy=multi-user.target")
	require.Contains(t, result, "NoNewPrivileges=true")
	require.Contains(t, result, "ProtectSystem=strict")
}

func TestBuildSystemdUnit_UserLevel(t *testing.T) {
	t.Parallel()
	result := BuildSystemdUnit(InstallOptions{
		BinaryPath: "/home/user/bin/hotplex",
		ConfigPath: "/home/user/.hotplex/config.yaml",
		Level:      LevelUser,
		Name:       "hotplex",
	}, "/home/user")

	require.Contains(t, result, "WorkingDirectory=/home/user")
	require.Contains(t, result, "WantedBy=default.target")
	require.NotContains(t, result, "User=hotplex")
	require.NotContains(t, result, "NoNewPrivileges=true")
}

func TestBuildSystemdUnit_InjectsPath(t *testing.T) {
	t.Parallel()
	result := BuildSystemdUnit(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/etc/hotplex/config.yaml",
		Level:      LevelUser,
		Name:       "hotplex",
	}, "/home/user")

	require.Contains(t, result, "Environment=PATH=")
}

func TestBuildLaunchdPlist_SystemLevel(t *testing.T) {
	t.Parallel()
	result := BuildLaunchdPlist(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/etc/hotplex/config.yaml",
		Level:      LevelSystem,
		Name:       "hotplex",
	}, "/root")

	require.Contains(t, result, `<?xml version="1.0" encoding="UTF-8"?>`)
	require.Contains(t, result, "<string>com.hrygo.hotplex</string>")
	require.Contains(t, result, "<string>/usr/local/bin/hotplex</string>")
	require.Contains(t, result, "<key>RunAtLoad</key>")
	require.Contains(t, result, "<key>EnvironmentVariables</key>")
	require.Contains(t, result, "<key>PATH</key>")
	require.Contains(t, result, "/var/log/hotplex/launchd.stdout.log")
}

func TestBuildLaunchdPlist_UserLevel(t *testing.T) {
	t.Parallel()
	result := BuildLaunchdPlist(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/Users/test/.hotplex/config.yaml",
		Level:      LevelUser,
		Name:       "hotplex",
	}, "/Users/test")

	require.Contains(t, result, "<string>com.hrygo.hotplex.user</string>")
	require.Contains(t, result, "/Users/test/.hotplex/logs/launchd.stdout.log")
}

func TestBuildLaunchdPlist_ContainsGatewayStartArgs(t *testing.T) {
	t.Parallel()
	result := BuildLaunchdPlist(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/etc/hotplex/config.yaml",
		Level:      LevelUser,
		Name:       "hotplex",
	}, "/home")

	require.Contains(t, result, "<string>gateway</string>")
	require.Contains(t, result, "<string>start</string>")
	require.Contains(t, result, "<string>--config</string>")
}

func TestParseLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  Level
		err   bool
	}{
		{"user", LevelUser, false},
		{"system", LevelSystem, false},
		{"", LevelUser, false},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if tt.err {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		}
	}
}

func TestLaunchdLabel(t *testing.T) {
	t.Parallel()
	require.Equal(t, "com.hrygo.hotplex", launchdLabel("hotplex", LevelSystem))
	require.Equal(t, "com.hrygo.hotplex.user", launchdLabel("hotplex", LevelUser))
}

func TestBuildSystemdUnit_ContainsNoTrailingWhitespace(t *testing.T) {
	t.Parallel()
	result := BuildSystemdUnit(InstallOptions{
		BinaryPath: "/usr/local/bin/hotplex",
		ConfigPath: "/etc/hotplex/config.yaml",
		Level:      LevelSystem,
		Name:       "hotplex",
	}, "/root")
	for _, line := range strings.Split(result, "\n") {
		require.False(t, strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t"),
			"trailing whitespace: %q", line)
	}
}
