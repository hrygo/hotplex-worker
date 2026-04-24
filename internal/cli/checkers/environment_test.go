package checkers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestParseGoMinor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{"go1.26.0", 26},
		{"go1.30", 30},
		{"go1.25", 25},
		{"go2.0", 2},
		{"devel", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := parseGoMinor(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGoVersionChecker(t *testing.T) {
	t.Parallel()

	c := goVersionChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "environment.go_version", d.Name)
	require.Equal(t, "environment", d.Category)
	require.NotEmpty(t, d.Message)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusWarn, cli.StatusFail}, d.Status)
}

func TestOSArchChecker(t *testing.T) {
	t.Parallel()

	c := osArchChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "environment.os_arch", d.Name)
	require.Equal(t, "environment", d.Category)
	require.NotEmpty(t, d.Message)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestBuildToolsChecker(t *testing.T) {
	t.Parallel()

	c := buildToolsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "environment.build_tools", d.Name)
	require.Equal(t, "environment", d.Category)
	require.NotEmpty(t, d.Message)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusWarn, cli.StatusFail}, d.Status)
}
