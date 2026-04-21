package slack

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ChunkContent splits content into chunks of at most maxRunes runes,
// respecting code block boundaries. When content is split into multiple
// chunks, each gets a [N/M] prefix. Code fences are properly
// closed and reopened at chunk boundaries.
func ChunkContent(content string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{content}
	}

	runes := []rune(content)
	totalRunes := len(runes)

	if totalRunes <= maxRunes {
		return []string{content}
	}

	var chunks []string
	start := 0
	var openFence string

	for start < totalRunes {
		end := start + maxRunes
		if end > totalRunes {
			end = totalRunes
		}

		if end == totalRunes {
			chunk := string(runes[start:end])
			if openFence != "" {
				chunk = openFence + "\n" + chunk
			}
			chunks = append(chunks, chunk)
			break
		}

		contentUpToMax := string(runes[start:end])
		inCodeBlock, fence := scanCodeBlockState(contentUpToMax)
		splitPoint := findSafeSplitPoint(runes, start, end, inCodeBlock)

		chunkRunes := runes[start:splitPoint]
		chunk := string(chunkRunes)

		if openFence != "" {
			chunk = openFence + "\n" + chunk
			openFence = ""
		}

		if inCodeBlock && fence != "" {
			chunk = chunk + "\n" + fence + "\n"
			openFence = fence
		}

		chunks = append(chunks, chunk)
		start = splitPoint
	}

	if len(chunks) > 1 {
		totalChunks := len(chunks)
		for i := range chunks {
			chunks[i] = fmt.Sprintf("[%d/%d] %s", i+1, totalChunks, chunks[i])
		}
	}

	return chunks
}

func scanCodeBlockState(content string) (bool, string) {
	lines := strings.Split(content, "\n")
	var inCodeBlock bool
	var codeFence string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isCodeFence(trimmed) {
			if !inCodeBlock {
				inCodeBlock = true
				codeFence = extractFence(trimmed)
			} else if strings.HasPrefix(trimmed, codeFence) {
				inCodeBlock = false
				codeFence = ""
			}
		}
	}

	return inCodeBlock, codeFence
}

func isCodeFence(line string) bool {
	if len(line) < 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		if line[i] != '`' {
			return false
		}
	}
	return true
}

func extractFence(line string) string {
	count := 0
	for i := 0; i < len(line) && line[i] == '`'; i++ {
		count++
	}
	return strings.Repeat("`", count)
}

func findSafeSplitPoint(runes []rune, start, maxEnd int, inCodeBlock bool) int {
	if start >= maxEnd || start >= len(runes) {
		return maxEnd
	}
	searchEnd := maxEnd
	if searchEnd > len(runes) {
		searchEnd = len(runes)
	}

	minChunkSize := 100
	minPos := start + minChunkSize
	if minPos > searchEnd {
		minPos = start + 1
	}

	content := string(runes[start:searchEnd])

	if inCodeBlock {
		lastNewline := strings.LastIndex(content[:len(content)-1], "\n")
		if lastNewline > 0 {
			return start + utf8.RuneCountInString(content[:lastNewline+1])
		}
	} else {
		paragraphBreak := strings.LastIndex(content[:len(content)-1], "\n\n")
		if paragraphBreak > 0 {
			runePos := utf8.RuneCountInString(content[:paragraphBreak+2])
			if start+runePos >= minPos {
				return start + runePos
			}
		}

		lastNewline := strings.LastIndex(content[:len(content)-1], "\n")
		if lastNewline > 0 {
			runePos := utf8.RuneCountInString(content[:lastNewline+1])
			if start+runePos >= minPos {
				return start + runePos
			}
		}
	}

	for i := searchEnd - 1; i >= minPos; i-- {
		if i < 0 || i >= len(runes) {
			break
		}
		if runes[i] == ' ' {
			return i + 1
		}
	}

	if searchEnd > len(runes) {
		return len(runes)
	}
	return searchEnd
}
