package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestValidateInit_Direct(t *testing.T) {
	// Exactly replicate what the original test does
	data := map[string]any{
		"version":     events.Version,
		"worker_type": "claude-code",
	}

	env := envFromData(data)

	require.NotNil(t, env)
	require.Equal(t, events.Version, env.Version)
	require.NotEmpty(t, env.ID)
	require.Equal(t, int64(1), env.Seq)

	// Call ValidateInit
	initData, err := ValidateInit(env)

	t.Logf("err == nil: %v", err == nil)
	t.Logf("err != nil: %v", err != nil)
	t.Logf("err type: %T, value: %v", err, err)

	// ValidateInit returns (*InitError)(nil) on success — a typed nil that
	// testify/require.NoError cannot detect via reflection. Use a direct nil
	// check instead.
	if err != nil {
		require.NoError(t, err) // typed nil-safe: prints the actual error
	}
	require.NotEmpty(t, initData.WorkerType)
}
