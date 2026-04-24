package main

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionText(t *testing.T) {
	t.Parallel()

	cmd := newVersionCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	require.Contains(t, output, "hotplex")
	require.Contains(t, output, "Build:")
	require.Contains(t, output, "Go:")
	require.Contains(t, output, "OS/Arch:")
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()

	cmd := newVersionCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--format", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	require.NotEmpty(t, result["version"])
	require.NotEmpty(t, result["go"])
	require.Equal(t, runtime.GOOS, result["os"])
	require.Equal(t, runtime.GOARCH, result["arch"])
}

func TestLoadEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	envPath := tmpDir + "/.env"

	content := strings.Join([]string{
		"# comment",
		"TEST_VAR_1=hello",
		"TEST_VAR_2=\"quoted value\"",
		"TEST_VAR_3='single quoted'",
		"",
		"# another comment",
		"TEST_VAR_4=noquotes",
	}, "\n")
	err := os.WriteFile(envPath, []byte(content), 0o644)
	require.NoError(t, err)

	loadEnvFile(tmpDir)

	require.Equal(t, "hello", os.Getenv("TEST_VAR_1"))
	require.Equal(t, "quoted value", os.Getenv("TEST_VAR_2"))
	require.Equal(t, "single quoted", os.Getenv("TEST_VAR_3"))
	require.Equal(t, "noquotes", os.Getenv("TEST_VAR_4"))

	t.Cleanup(func() {
		_ = os.Unsetenv("TEST_VAR_1")
		_ = os.Unsetenv("TEST_VAR_2")
		_ = os.Unsetenv("TEST_VAR_3")
		_ = os.Unsetenv("TEST_VAR_4")
	})
}
