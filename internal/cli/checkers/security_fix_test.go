package checkers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupTestConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origConfigPath := configPath
	configPath = filepath.Join(dir, "config.yaml")
	t.Cleanup(func() { configPath = origConfigPath })
	require.NoError(t, os.WriteFile(configPath, []byte{}, 0o600))
	return dir
}

func setupTestWd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	return dir
}

func TestFixJWTStrength(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")

	require.NoError(t, fixJWTStrength())

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "HOTPLEX_JWT_SECRET=")
	require.False(t, strings.Contains(content, "JWT_SECRET=") && !strings.Contains(content, "HOTPLEX_JWT_SECRET="))
}

func TestFixJWTStrength_RemovesLegacy(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("JWT_SECRET=old_value\n"), 0o600))

	require.NoError(t, fixJWTStrength())

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "HOTPLEX_JWT_SECRET=")
	require.NotContains(t, content, "JWT_SECRET=old_value")
}

func TestFixAdminToken(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("ADMIN_TOKEN=old\n"), 0o600))

	require.NoError(t, fixAdminToken())

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "HOTPLEX_ADMIN_TOKEN_1=")
	require.NotContains(t, content, "ADMIN_TOKEN=old")

	var token string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "HOTPLEX_ADMIN_TOKEN_1=") {
			token = strings.TrimPrefix(line, "HOTPLEX_ADMIN_TOKEN_1=")
			break
		}
	}
	require.NotEmpty(t, token, "HOTPLEX_ADMIN_TOKEN_1 should be present")
	require.Len(t, token, 64)
}

func TestFixEnvInGit_CreatesGitignore(t *testing.T) {
	dir := setupTestWd(t)

	require.NoError(t, fixEnvInGit())

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	require.Contains(t, string(data), ".env")
}

func TestFixEnvInGit_Appends(t *testing.T) {
	dir := setupTestWd(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644))

	require.NoError(t, fixEnvInGit())

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "*.log")
	require.Contains(t, content, ".env")
}

func TestFixEnvInGit_SkipsIfPresent(t *testing.T) {
	dir := setupTestWd(t)
	original := "*.log\n.env\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(original), 0o644))

	require.NoError(t, fixEnvInGit())

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	require.Equal(t, original, string(data))
}

func TestWriteEnvVar(t *testing.T) {
	dir := setupTestConfigDir(t)

	require.NoError(t, writeEnvVar("TEST_KEY", "test_value"))

	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "TEST_KEY=test_value\n")
}

func TestWriteEnvVar_AppendsToExisting(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("EXISTING=val\n"), 0o600))
	require.NoError(t, writeEnvVar("NEW_KEY", "new_val"))

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "EXISTING=val")
	require.Contains(t, content, "NEW_KEY=new_val")
}

func TestUnsetEnvVar(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("KEEP=this\nREMOVE=that\n"), 0o600))

	require.NoError(t, unsetEnvVar("REMOVE"))

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "KEEP=this")
	require.NotContains(t, string(data), "REMOVE=that")
}

func TestUnsetEnvVar_NoFile(t *testing.T) {
	setupTestConfigDir(t)
	require.NoError(t, unsetEnvVar("NONEXISTENT"))
}

func TestUnsetEnvVar_KeyNotFound(t *testing.T) {
	dir := setupTestConfigDir(t)
	envPath := filepath.Join(dir, ".env")
	original := "OTHER=val\n"
	require.NoError(t, os.WriteFile(envPath, []byte(original), 0o600))

	require.NoError(t, unsetEnvVar("NOT_HERE"))

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "OTHER=val")
}
