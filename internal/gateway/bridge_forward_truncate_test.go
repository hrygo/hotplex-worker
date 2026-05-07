package gateway

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestTruncateToolResultOutput(t *testing.T) {
	t.Parallel()

	t.Run("string output under limit", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"id":"call_123","output":"short"}`)
		result := truncateToolResultOutput(raw)
		require.JSONEq(t, string(raw), string(result))
	})

	t.Run("string output over limit", func(t *testing.T) {
		t.Parallel()
		long := make([]byte, 200)
		for i := range long {
			long[i] = 'a'
		}
		input, _ := json.Marshal(map[string]any{
			"id":     "call_abc",
			"output": string(long),
		})
		result := truncateToolResultOutput(input)

		var v struct {
			ID     string `json:"id"`
			Output string `json:"output"`
		}
		require.NoError(t, json.Unmarshal(result, &v))
		require.Equal(t, "call_abc", v.ID)
		require.Equal(t, maxToolResultOutputLen, utf8.RuneCountInString(v.Output))
	})

	t.Run("multibyte output truncated by rune", func(t *testing.T) {
		t.Parallel()
		// 200 Chinese characters = 600 bytes in UTF-8
		runes := make([]rune, 200)
		for i := range runes {
			runes[i] = '中'
		}
		input, _ := json.Marshal(map[string]any{
			"id":     "call_cjk",
			"output": string(runes),
		})
		result := truncateToolResultOutput(input)

		var v struct {
			ID     string `json:"id"`
			Output string `json:"output"`
		}
		require.NoError(t, json.Unmarshal(result, &v))
		require.Equal(t, maxToolResultOutputLen, utf8.RuneCountInString(v.Output))
		require.Equal(t, "call_cjk", v.ID)
		// Verify valid UTF-8 — no mid-character cut
		require.True(t, utf8.ValidString(v.Output))
	})

	t.Run("nil output", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"id":"call_456","output":null}`)
		result := truncateToolResultOutput(raw)
		require.JSONEq(t, string(raw), string(result))
	})

	t.Run("non-string output", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"id":"call_789","output":{"nested":true}}`)
		result := truncateToolResultOutput(raw)
		require.JSONEq(t, string(raw), string(result))
	})

	t.Run("invalid json passthrough", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{invalid}`)
		result := truncateToolResultOutput(raw)
		require.Equal(t, string(raw), string(result))
	})
}
