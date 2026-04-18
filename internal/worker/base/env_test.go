package base

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

func TestBuildEnv_BasicWhitelist(t *testing.T) {
	// Set up test environment.
	os.Setenv("HOME", "/home/test")
	os.Setenv("PATH", "/usr/bin")
	os.Setenv("SECRET_KEY", "should-be-filtered")
	defer os.Unsetenv("HOME")
	defer os.Unsetenv("PATH")
	defer os.Unsetenv("SECRET_KEY")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env:        nil,
	}

	whitelist := []string{"HOME", "PATH"}
	env := BuildEnv(session, whitelist, "test-worker")

	// Check that whitelisted vars are present.
	foundHOME := false
	foundPATH := false
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			foundHOME = true
			require.Equal(t, "HOME=/home/test", e)
		}
		if strings.HasPrefix(e, "PATH=") {
			foundPATH = true
			require.Equal(t, "PATH=/usr/bin", e)
		}
	}
	require.True(t, foundHOME, "HOME should be in env")
	require.True(t, foundPATH, "PATH should be in env")

	// Check HOTPLEX vars.
	foundSessionID := false
	foundWorkerType := false
	for _, e := range env {
		if strings.HasPrefix(e, "HOTPLEX_SESSION_ID=") {
			foundSessionID = true
			require.Equal(t, "HOTPLEX_SESSION_ID=test-session", e)
		}
		if strings.HasPrefix(e, "HOTPLEX_WORKER_TYPE=") {
			foundWorkerType = true
			require.Equal(t, "HOTPLEX_WORKER_TYPE=test-worker", e)
		}
	}
	require.True(t, foundSessionID, "HOTPLEX_SESSION_ID should be in env")
	require.True(t, foundWorkerType, "HOTPLEX_WORKER_TYPE should be in env")
}

func TestBuildEnv_SessionVarsOverride(t *testing.T) {
	os.Setenv("MY_VAR", "original-value")
	defer os.Unsetenv("MY_VAR")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env: map[string]string{
			"MY_VAR": "session-value",
		},
	}

	whitelist := []string{"HOME", "MY_VAR"}
	env := BuildEnv(session, whitelist, "test-worker")

	// MY_VAR is whitelisted, so it appears once (from os.Environ).
	// The session.Env loop skips keys already in whitelist.
	count := 0
	for _, e := range env {
		if strings.HasPrefix(e, "MY_VAR=") {
			count++
		}
	}
	require.Equal(t, 1, count, "MY_VAR should appear once (whitelisted keys skip session.Env loop)")

	// The value should be from os.Environ, not session.Env.
	found := false
	for _, e := range env {
		if e == "MY_VAR=original-value" {
			found = true
			break
		}
	}
	require.True(t, found, "MY_VAR should have value from os.Environ")
}

func TestBuildEnv_HotPlexVars(t *testing.T) {
	session := worker.SessionInfo{
		SessionID:  "session-123",
		UserID:     "user-456",
		ProjectDir: "/tmp",
		Env:        nil,
	}

	whitelist := []string{}
	env := BuildEnv(session, whitelist, "claude-code")

	foundSessionID := false
	foundWorkerType := false
	for _, e := range env {
		if e == "HOTPLEX_SESSION_ID=session-123" {
			foundSessionID = true
		}
		if e == "HOTPLEX_WORKER_TYPE=claude-code" {
			foundWorkerType = true
		}
	}
	require.True(t, foundSessionID, "HOTPLEX_SESSION_ID should be set correctly")
	require.True(t, foundWorkerType, "HOTPLEX_WORKER_TYPE should be set correctly")
}

func TestBuildEnv_StripNestedAgent(t *testing.T) {
	// Set up test environment with CLAUDECODE= present.
	os.Setenv("CLAUDECODE", "nested-agent-config")
	os.Setenv("HOME", "/home/test")
	defer os.Unsetenv("CLAUDECODE")
	defer os.Unsetenv("HOME")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env:        nil,
	}

	whitelist := []string{"HOME", "CLAUDECODE"}
	env := BuildEnv(session, whitelist, "test-worker")

	// CLAUDECODE= should be stripped even if whitelisted.
	for _, e := range env {
		require.NotEqual(t, "CLAUDECODE=nested-agent-config", e, "CLAUDECODE should be stripped")
		require.NotEqual(t, "CLAUDECODE=", e, "CLAUDECODE should be stripped")
	}
}

func TestBuildEnv_EmptyWhitelist(t *testing.T) {
	os.Setenv("HOME", "/home/test")
	defer os.Unsetenv("HOME")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env:        nil,
	}

	whitelist := []string{}
	env := BuildEnv(session, whitelist, "test-worker")

	// Should only have HOTPLEX vars plus HOME from os.Environ() if it matches session.Env.
	// Since session.Env is nil and whitelist is empty, HOME should NOT be included.
	foundHOME := false
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") {
			foundHOME = true
		}
	}
	require.False(t, foundHOME, "HOME should not be in env when not whitelisted and not in session.Env")
}

func TestBuildEnv_SessionOnlyVars(t *testing.T) {
	os.Setenv("HOME", "/home/test")
	defer os.Unsetenv("HOME")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env: map[string]string{
			"CUSTOM_VAR": "custom-value",
			"ANOTHER":    "another-value",
		},
	}

	whitelist := []string{"HOME"}
	env := BuildEnv(session, whitelist, "test-worker")

	// Custom vars from session.Env should be included.
	foundCustom := false
	foundAnother := false
	for _, e := range env {
		if e == "CUSTOM_VAR=custom-value" {
			foundCustom = true
		}
		if e == "ANOTHER=another-value" {
			foundAnother = true
		}
	}
	require.True(t, foundCustom, "CUSTOM_VAR from session.Env should be included")
	require.True(t, foundAnother, "ANOTHER from session.Env should be included")
}

func TestBuildEnv_PrefixWhitelist(t *testing.T) {
	os.Setenv("HOME", "/home/test")
	os.Setenv("OTEL_SERVICE_NAME", "hotplex-gateway")
	os.Setenv("OTEL_EXPORTER", "console")
	os.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "512")
	os.Setenv("NON_OTEL_VAR", "should-be-filtered")
	defer os.Unsetenv("HOME")
	defer os.Unsetenv("OTEL_SERVICE_NAME")
	defer os.Unsetenv("OTEL_EXPORTER")
	defer os.Unsetenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE")
	defer os.Unsetenv("NON_OTEL_VAR")

	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Env:        nil,
	}

	// OTEL_ with trailing "_" is a prefix match.
	whitelist := []string{"HOME", "OTEL_"}
	env := BuildEnv(session, whitelist, "test-worker")

	foundServiceName := false
	foundExporter := false
	foundBatchSize := false
	foundNonOtel := false
	for _, e := range env {
		if e == "OTEL_SERVICE_NAME=hotplex-gateway" {
			foundServiceName = true
		}
		if e == "OTEL_EXPORTER=console" {
			foundExporter = true
		}
		if e == "OTEL_BSP_MAX_EXPORT_BATCH_SIZE=512" {
			foundBatchSize = true
		}
		if strings.HasPrefix(e, "NON_OTEL_VAR=") {
			foundNonOtel = true
		}
	}
	require.True(t, foundServiceName, "OTEL_SERVICE_NAME should be included via prefix match")
	require.True(t, foundExporter, "OTEL_EXPORTER should be included via prefix match")
	require.True(t, foundBatchSize, "OTEL_BSP_MAX_EXPORT_BATCH_SIZE should be included via prefix match")
	require.False(t, foundNonOtel, "NON_OTEL_VAR should not be included")
}
