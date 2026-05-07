package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ensureJSONPrompt appends a JSON instruction if the prompt doesn't mention JSON.
func ensureJSONPrompt(prompt string) string {
	if !strings.Contains(strings.ToLower(prompt), "json") {
		return prompt + "\n\nIMPORTANT: Return ONLY valid JSON."
	}
	return prompt
}

// cleanJSONResponse strips markdown code fences and trims whitespace.
func cleanJSONResponse(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if !strings.HasPrefix(cleaned, "```") {
		return cleaned
	}
	firstNL := strings.Index(cleaned, "\n")
	if firstNL < 0 {
		return cleaned
	}
	lastFence := strings.LastIndex(cleaned, "```")
	if lastFence <= firstNL {
		return cleaned
	}
	return strings.TrimSpace(cleaned[firstNL+1 : lastFence])
}

// truncateForError truncates a string for inclusion in error messages.
func truncateForError(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	return s[:maxRunes] + "...(truncated)"
}

// formatUnmarshalError creates a concise error for JSON unmarshal failures.
func formatUnmarshalError(err error, content string) error {
	return fmt.Errorf("failed to unmarshal JSON content: %w. CONTENT: %s", err, truncateForError(content, 200))
}

// Token estimation constants based on GPT tokenizer characteristics.
const (
	TokenCharsPerASCII = 4.0 // ASCII characters average ~4 chars per token
	TokenCharsPerCJK   = 1.5 // CJK characters average ~1.5 chars per token
	TokenCharsPerOther = 3.0 // Other Unicode characters average ~3 chars per token
)

// EstimateTokens estimates token count using character-type-aware estimation.
// More accurate than simple word-count, especially for mixed CJK/Latin text.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	var asciiCount, cjkCount, otherCount float64

	for _, r := range text {
		switch {
		case r < 128: // ASCII range
			asciiCount++
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			cjkCount++
		case r >= 0x3040 && r <= 0x30FF: // Japanese Hiragana/Katakana
			cjkCount++
		case r >= 0xAC00 && r <= 0xD7AF: // Korean Hangul
			cjkCount++
		default:
			otherCount++
		}
	}

	total := asciiCount/TokenCharsPerASCII + cjkCount/TokenCharsPerCJK + otherCount/TokenCharsPerOther
	return int(total + 0.5)
}

// healthCheckFromChat performs a health check by calling chatFn with "ping".
func healthCheckFromChat(ctx context.Context, chatFn func(ctx context.Context, prompt string) (string, error), provider, model string) HealthStatus {
	start := time.Now()
	_, err := chatFn(ctx, "ping")
	latency := time.Since(start).Milliseconds()

	status := HealthStatus{
		Healthy:   err == nil,
		Provider:  provider,
		Model:     model,
		LatencyMs: latency,
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}
