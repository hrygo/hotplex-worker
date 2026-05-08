package tts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"empty string", "", 10, ""},
		{"zero maxLen", "hello", 0, ""},
		{"snaps to period", "第一句话。第二句话。第三句。", 10, "第一句话。第二句话。"},
		{"snaps to Chinese period", "你好世界。这是测试", 6, "你好世界。"},
		{"snaps to comma", "第一段，第二段，第三段", 5, "第一段，"},
		{"snaps to question mark", "是吗？不对", 3, "是吗？"},
		{"snaps to exclamation", "好！继续", 2, "好！"},
		{"no punctuation fallback", "abcdefghij", 5, "abcde"},
		{"mixed punctuation picks latest", "a，b。c", 5, "a，b。c"},
		{"all ascii punctuation", "one. two. three.", 8, "one."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TruncateText(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeForSpeech(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"markdown link", "click [here](https://example.com) for details", "click here for details"},
		{"markdown image", "![alt text](image.png) content", "content"},
		{"code fence", "```go\nfmt.Println()\n```", "go fmt.Println()"},
		{"inline code", "use `fmt.Println` to print", "use fmt.Println to print"},
		{"bold and italic", "**bold** and *italic* text", "bold and italic text"},
		{"heading markers", "## Title\n> quote", "Title quote"},
		{"bare path", "edit internal/messaging/tts/edge.go file", "edit 相关文件 file"},
		{"bare path with backslash", "open src\\utils\\helper.py please", "open 相关文件 please"},
		{"non-path dot not matched", "end. Then start", "end. Then start"},
		{"large number", "used 48434 tokens", "used 4.8万 tokens"},
		{"number under 10k", "count is 9999", "count is 9999"},
		{"hundred million", "revenue 200000000 yuan", "revenue 2.0亿 yuan"},
		{"combined formatting", "**Fix** in `cmd/main.go`: see [docs](http://x)", "Fix in 相关文件: see docs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeForSpeech(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}

	// Separate subtests for Unicode special characters (not representable in const strings).
	t.Run("zero-width character stripped", func(t *testing.T) {
		t.Parallel()
		input := "hello" + string(rune(0x200B)) + "world"
		assert.Equal(t, "helloworld", SanitizeForSpeech(input))
	})
	t.Run("BOM character stripped", func(t *testing.T) {
		t.Parallel()
		input := string(rune(0xFEFF)) + "start text"
		assert.Equal(t, "start text", SanitizeForSpeech(input))
	})
}

func TestNormalizeLargeNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"9999", "9999"},
		{"10000", "1.0万"},
		{"48434", "4.8万"},
		{"200000", "20.0万"},
		{"100000000", "1.0亿"},
		{"200000000", "2.0亿"},
		{"1500000", "150.0万"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeLargeNumber(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTTSPrompt(t *testing.T) {
	t.Parallel()

	t.Run("truncates long input", func(t *testing.T) {
		t.Parallel()
		longText := strings.Repeat("a", 3000)
		prompt := BuildTTSPrompt(longText)
		require.Contains(t, prompt, "将以下 AI 回复改写为口语播报稿")
		inputStart := len("将以下 AI 回复改写为口语播报稿：\n\n")
		require.LessOrEqual(t, len([]rune(prompt[inputStart:])), SummaryInputCap)
	})

	t.Run("short input preserved", func(t *testing.T) {
		t.Parallel()
		prompt := BuildTTSPrompt("short text")
		require.Contains(t, prompt, "short text")
	})
}

func TestBuildTTSChatOpts(t *testing.T) {
	t.Parallel()

	opts := BuildTTSChatOpts(200)
	require.NotNil(t, opts.Temperature)
	assert.InDelta(t, 0.3, *opts.Temperature, 0.001)
	assert.Equal(t, 256, opts.MaxTokens)
	require.NotEmpty(t, opts.SystemPrompt)
	assert.Contains(t, opts.SystemPrompt, "200")
	assert.Contains(t, opts.SystemPrompt, "语音播报编辑")
}

func TestSanitizeSSMLText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"ampersand", "a&b", "a&amp;b"},
		{"less than", "a<b", "a&lt;b"},
		{"greater than", "a>b", "a&gt;b"},
		{"single quote", "a'b", "a&apos;b"},
		{"double quote", "a\"b", "a&quot;b"},
		{"control chars stripped", "hello\x00world", "helloworld"},
		{"tab preserved", "hello\tworld", "hello\tworld"},
		{"newline preserved", "hello\nworld", "hello\nworld"},
		{"normal text unchanged", "你好世界", "你好世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeSSMLText(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGenerateSecMSGec(t *testing.T) {
	t.Parallel()

	token1 := generateSecMSGec()
	assert.Len(t, token1, 64, "SHA-256 hex should be 64 chars")
	assert.Equal(t, strings.ToUpper(token1), token1, "should be uppercase hex")

	// Same 5-min window should produce same token.
	token2 := generateSecMSGec()
	assert.Equal(t, token1, token2, "same 5-min window should produce same token")
}
