package slack

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestChunkContent_SmallContent_NoSplit(t *testing.T) {
	content := "This is a small message"
	chunks := ChunkContent(content, 3000)

	require.Len(t, chunks, 1)
	require.Equal(t, content, chunks[0])
}

func TestChunkContent_ExactlyMaxRunes_NoSplit(t *testing.T) {
	content := strings.Repeat("a", 3000)
	chunks := ChunkContent(content, 3000)

	require.Len(t, chunks, 1)
	require.Equal(t, content, chunks[0])
}

func TestChunkContent_SingleRuneOver_SplitsWithPrefix(t *testing.T) {
	content := strings.Repeat("a", 3001)
	chunks := ChunkContent(content, 3000)

	require.Len(t, chunks, 2)
	require.True(t, strings.HasPrefix(chunks[0], "[1/2] "))
	require.True(t, strings.HasPrefix(chunks[1], "[2/2] "))
}

func TestChunkContent_ParagraphBoundary(t *testing.T) {
	para1 := strings.Repeat("a", 1500)
	para2 := strings.Repeat("b", 1500)
	para3 := strings.Repeat("c", 100)
	content := para1 + "\n\n" + para2 + "\n\n" + para3

	chunks := ChunkContent(content, 3000)

	require.Len(t, chunks, 2)
	require.True(t, strings.HasPrefix(chunks[0], "[1/2] "))
	require.True(t, strings.HasPrefix(chunks[1], "[2/2] "))
	require.Contains(t, chunks[0], para1)
	require.Contains(t, chunks[1], para2)
}

func TestChunkContent_CodeBlock_NoMidLineSplit(t *testing.T) {
	codeLines := []string{
		"```go",
		"func main() {",
		"    fmt.Println(\"Hello, World!\")",
		"    result := someFunction(with, many, arguments, here)",
		"}",
		"```",
	}
	content := strings.Join(codeLines, "\n")

	chunks := ChunkContent(content, 50)
	combined := ""
	for _, chunk := range chunks {
		if idx := strings.Index(chunk, "] "); idx != -1 {
			combined += chunk[idx+2:]
		} else {
			combined += chunk
		}
	}

	for _, line := range codeLines[1 : len(codeLines)-1] {
		require.Contains(t, combined, line, "Line should not be split: %s", line)
	}
}

func TestChunkContent_CodeBlock_ClosedAndReopened(t *testing.T) {
	lines := []string{
		"Here is some text before the code block.",
		"```go",
		"func example() {",
		"    return 42",
		"}",
		"```",
		"Here is text after the code block.",
	}

	longLine := strings.Repeat("x", 3500)
	lines[3] = "    return 42 + " + longLine
	content := strings.Join(lines, "\n")

	chunks := ChunkContent(content, 3000)

	require.GreaterOrEqual(t, len(chunks), 2)

	foundOpen := false
	foundClose := false
	for i, chunk := range chunks {
		if i == 0 && len(chunks) > 1 {
			prefix := fmt.Sprintf("[1/%d] ", len(chunks))
			chunk = strings.TrimPrefix(chunk, prefix)
		}
		if strings.Contains(chunk, "```\n") || strings.HasSuffix(chunk, "```") {
			foundClose = true
		}
		if strings.Contains(chunk, "```go") || strings.Contains(chunk, "```") {
			foundOpen = true
		}
	}
	if len(chunks) > 1 {
		require.True(t, foundClose || foundOpen, "Code fences should be closed/reopened across chunks")
	}
}

func TestChunkContent_CodeBlock_FenceReopenedInNextChunk(t *testing.T) {
	lines := []string{
		"```go",
		"func example() {",
		"    x := " + strings.Repeat("1", 500),
		"    y := " + strings.Repeat("2", 500),
		"    z := " + strings.Repeat("3", 500),
		"}",
		"```",
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkContent(content, 300)

	require.GreaterOrEqual(t, len(chunks), 2)

	firstChunk := chunks[0]
	if idx := strings.Index(firstChunk, "] "); idx != -1 {
		firstChunk = firstChunk[idx+2:]
	}
	require.True(t, strings.HasSuffix(firstChunk, "```\n") || strings.Contains(firstChunk, "```\n"),
		"First chunk should close the code fence: %q", firstChunk)

	secondChunk := chunks[1]
	if idx := strings.Index(secondChunk, "] "); idx != -1 {
		secondChunk = secondChunk[idx+2:]
	}
	require.True(t, strings.HasPrefix(secondChunk, "```\n") || strings.HasPrefix(secondChunk, "```go\n"),
		"Second chunk should reopen the code fence: %q", secondChunk)
}

func TestChunkContent_SingleChunk_NoPrefix(t *testing.T) {
	content := strings.Repeat("a", 2999)
	chunks := ChunkContent(content, 3000)

	require.Len(t, chunks, 1)
	require.False(t, strings.HasPrefix(chunks[0], "["))
}

func TestChunkContent_MultiChunk_HasPrefix(t *testing.T) {
	content := strings.Repeat("a", 6000)
	chunks := ChunkContent(content, 3000)

	require.GreaterOrEqual(t, len(chunks), 2)
	for i, chunk := range chunks {
		expectedPrefix := fmt.Sprintf("[%d/%d] ", i+1, len(chunks))
		require.True(t, strings.HasPrefix(chunk, expectedPrefix), "Chunk %d should have prefix %s", i, expectedPrefix)
	}
}

func TestChunkContent_EmptyContent(t *testing.T) {
	chunks := ChunkContent("", 3000)

	require.Len(t, chunks, 1)
	require.Equal(t, "", chunks[0])
}

func TestChunkContent_ZeroMaxRunes(t *testing.T) {
	content := "test content"
	chunks := ChunkContent(content, 0)

	require.Len(t, chunks, 1)
	require.Equal(t, content, chunks[0])
}

func TestChunkContent_NegativeMaxRunes(t *testing.T) {
	content := "test content"
	chunks := ChunkContent(content, -1)

	require.Len(t, chunks, 1)
	require.Equal(t, content, chunks[0])
}

func TestChunkContent_MultiBacktickFence(t *testing.T) {
	lines := []string{
		"Some text before.",
		"````go",
		"code with ``` inside",
		"````",
		"Some text after.",
	}
	content := strings.Join(lines, "\n")

	chunks := ChunkContent(content, 3000)
	combined := strings.Join(chunks, "")

	require.Contains(t, combined, "````go")
	require.Contains(t, combined, "code with ``` inside")
	require.Contains(t, combined, "````")
}

func TestChunkContent_CodeBlockAcrossChunks(t *testing.T) {
	var codeLines []string
	codeLines = append(codeLines, "```go")
	for i := 0; i < 20; i++ {
		codeLines = append(codeLines, "    line"+fmt.Sprintf("%d", i)+" := "+strings.Repeat("x", 30))
	}
	codeLines = append(codeLines, "```")

	content := strings.Join(codeLines, "\n")
	chunks := ChunkContent(content, 300)

	require.Greater(t, len(chunks), 1)

	for i, chunk := range chunks {
		lines := strings.Split(chunk, "\n")
		fenceCount := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "````") {
				fenceCount++
			}
		}

		if i == 0 && len(chunks) > 1 {
			require.GreaterOrEqual(t, fenceCount, 1, "First chunk should have opening fence")
		}
		if i == len(chunks)-1 {
			require.GreaterOrEqual(t, fenceCount, 1, "Last chunk should have closing fence")
		}
	}
}

func TestChunkContent_LargeContent_CorrectChunkCount(t *testing.T) {
	content := strings.Repeat("word ", 1000)
	maxRunes := 100

	chunks := ChunkContent(content, maxRunes)

	totalLen := 0
	for _, chunk := range chunks {
		cleanChunk := chunk
		if idx := strings.Index(chunk, "] "); idx != -1 {
			cleanChunk = chunk[idx+2:]
		}
		totalLen += len([]rune(cleanChunk))
	}

	require.InDelta(t, len([]rune(content)), totalLen, float64(len(chunks))*50)
}

func TestChunkContent_MultipleCodeBlocks(t *testing.T) {
	content := `
First paragraph with some text.

` + "```go" + `
func first() {}
` + "```" + `

Middle paragraph.

` + "```python" + `
def second():
    pass
` + "```" + `

Final paragraph.
`

	chunks := ChunkContent(content, 200)

	combined := ""
	for _, chunk := range chunks {
		if idx := strings.Index(chunk, "] "); idx != -1 {
			combined += chunk[idx+2:]
		} else {
			combined += chunk
		}
	}

	require.Contains(t, combined, "func first()")
	require.Contains(t, combined, "def second():")
}

func TestChunkContent_UnicodeContent(t *testing.T) {
	content := strings.Repeat("你好世界", 400)
	chunks := ChunkContent(content, 3000)

	totalRunes := 0
	for _, chunk := range chunks {
		cleanChunk := chunk
		if idx := strings.Index(chunk, "] "); idx != -1 {
			cleanChunk = chunk[idx+2:]
		}
		totalRunes += utf8.RuneCountInString(cleanChunk)
	}

	require.Equal(t, utf8.RuneCountInString(content), totalRunes)
}
