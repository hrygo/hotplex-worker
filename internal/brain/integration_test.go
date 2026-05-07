package brain

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_OpenAIChat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")
	if apiKey == "" {
		t.Skip("HOTPLEX_BRAIN_API_KEY not set")
	}

	err := Init(slog.Default())
	require.NoError(t, err)

	b := Global()
	require.NotNil(t, b, "brain should be initialized")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := b.Chat(ctx, "Say exactly: hello from brain")
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	t.Logf("Brain Chat response: %s", resp)
}

func TestIntegration_BrainSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")
	if apiKey == "" {
		t.Skip("HOTPLEX_BRAIN_API_KEY not set")
	}

	err := Init(slog.Default())
	require.NoError(t, err)

	b := Global()
	require.NotNil(t, b)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	longText := `以下是一个代码实现：

` + "```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```" + `

代码实现了基本的 Hello World 程序。
以下是相关参数表格：

| 参数 | 值 |
|------|-----|
| 语言 | Go |
| 版本 | 1.22 |

更多详情请参考 https://example.com`

	prompt := "用一句话总结以下内容，不超过50字：\n\n" + longText
	resp, err := b.Chat(ctx, prompt)
	require.NoError(t, err)
	assert.NotEmpty(t, resp)
	// Summary should be much shorter than original
	assert.Less(t, len(resp), len(longText), "summary should be shorter than input")
	t.Logf("Brain Summary response: %s", resp)
}

func TestIntegration_BrainChatTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	apiKey := os.Getenv("HOTPLEX_BRAIN_API_KEY")
	if apiKey == "" {
		t.Skip("HOTPLEX_BRAIN_API_KEY not set")
	}

	err := Init(slog.Default())
	require.NoError(t, err)

	b := Global()
	require.NotNil(t, b)

	// Verify chat works with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := b.Chat(ctx, "Say exactly: timeout test passed")
	require.NoError(t, err)
	assert.Contains(t, resp, "timeout test passed")
}
