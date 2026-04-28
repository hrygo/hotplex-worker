package checkers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestSTTRuntimeChecker_Name(t *testing.T) {
	t.Parallel()
	c := sttRuntimeChecker{}
	require.Equal(t, "stt.runtime", c.Name())
}

func TestSTTRuntimeChecker_Category(t *testing.T) {
	t.Parallel()
	c := sttRuntimeChecker{}
	require.Equal(t, "stt", c.Category())
}

func TestSTTRuntimeChecker_Check(t *testing.T) {
	t.Parallel()
	c := sttRuntimeChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "stt.runtime", d.Name)
	require.Equal(t, "stt", d.Category)
	// Either pass or fail depending on system python3 availability
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)

	if d.Status == cli.StatusPass {
		require.Contains(t, d.Message, "STT runtime")
	} else {
		require.NotEmpty(t, d.FixHint)
	}
}
