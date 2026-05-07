package brain

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIntegrationBrain initializes Brain via Init() (which uses worker config
// extraction, env vars, etc.) and skips the test if Brain is unavailable.
// CI runs with -short, so these tests never execute in CI.
func setupIntegrationBrain(t *testing.T) Brain {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Save and restore global brain state.
	oldBrain := globalBrain
	t.Cleanup(func() { globalBrain = oldBrain })
	globalBrain = nil

	if err := Init(slog.Default()); err != nil {
		t.Skipf("skipping: brain init failed (no API key available): %v", err)
	}

	b := Global()
	if b == nil {
		t.Skip("skipping: brain not initialized (no API key available)")
	}
	return b
}

func TestIntegration_Chat(t *testing.T) {
	b := setupIntegrationBrain(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := b.Chat(ctx, "Say exactly: hello from brain")
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	t.Logf("Brain Chat response: %s", resp)
}

func TestIntegration_Summary(t *testing.T) {
	b := setupIntegrationBrain(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	longText := "以下是代码实现：\n\n```go\nfunc main() { fmt.Println(\"hello\") }\n```\n\n" +
		"代码实现了 Hello World。参数表：\n| 参数 | 值 |\n|------|-----|\n| 语言 | Go |\n\n" +
		"详情参考 https://example.com"

	resp, err := b.Chat(ctx, "用一句话总结，不超过50字：\n\n"+longText)
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	assert.Less(t, len(resp), len(longText), "summary should be shorter than input")
	t.Logf("Brain Summary response: %s", resp)
}

func TestIntegration_ContextTimeout(t *testing.T) {
	b := setupIntegrationBrain(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := b.Chat(ctx, "Say exactly: timeout test passed")
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	t.Logf("Brain TimeoutTest response: %s", resp)
}
