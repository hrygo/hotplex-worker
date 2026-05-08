package tts

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/brain/llm"
)

// SummaryInputCap limits the input text length sent to the LLM for summarization.
// Independent of MaxChars to ensure sufficient context even when MaxChars is small.
const SummaryInputCap = 2000

// TTSSystemPrompt defines the persona and constraints for voice broadcast editing.
// Separated from the user prompt for stronger instruction adherence and less leakage.
const TTSSystemPrompt = "你是一位面向开发者的语音播报编辑。任务：将 AI 助手的技术回复改写为自然口语播报稿，供语音合成引擎朗读。\n\n" +
	"输出规范：\n" +
	"1. 纯文本，无任何格式符号（禁止井号、星号、反引号、竖线、方括号、花括号、尖括号）\n" +
	"2. 每句控制在 25 字以内，用逗号分隔短语，句号结束完整意思\n" +
	"3. 大数字用中文近似表达（如 48434 读作约4.8万，200000 读作 20万）\n" +
	"4. 控制在 %d 字符以内，3 到 4 句话\n\n" +
	"英文处理（按开发者口语习惯）：\n" +
	"- 高频术语保留英文：API、PR、CI、Git、Bug、Deploy、Build、Token、Docker、WebSocket、LLM、SDK\n" +
	"- 需解释的术语中文括注：RBAC（基于角色的访问控制）\n" +
	"- 代码细节、提交信息、路径、URL 一律用中文概括\n\n" +
	"禁止出现：Markdown、代码片段、文件路径、URL、数学符号、箭头符号"

// TTSUserPromptTemplate wraps the AI response text for summarization.
// Format string: fmt.Sprintf(TTSUserPromptTemplate, truncatedText)
const TTSUserPromptTemplate = "将以下 AI 回复改写为口语播报稿：\n\n%s"

// SummaryChatOpts controls LLM generation for TTS summaries.
// MaxTokens=256 covers 150 Chinese characters (~225 tokens) with margin.
// Temperature=0.3 ensures consistent, focused summaries.
var SummaryChatOpts = brain.ChatOptions{
	MaxTokens:    256,
	Temperature:  llm.FloatPtr(0.3),
	SystemPrompt: "", // populated dynamically by BuildTTSChatOpts
}

// BuildTTSChatOpts returns ChatOptions with the system prompt configured for the given maxChars.
func BuildTTSChatOpts(maxChars int) brain.ChatOptions {
	return brain.ChatOptions{
		MaxTokens:    256,
		Temperature:  llm.FloatPtr(0.3),
		SystemPrompt: fmt.Sprintf(TTSSystemPrompt, maxChars),
	}
}

// BuildTTSPrompt builds the user prompt from AI response text.
func BuildTTSPrompt(aiResponse string) string {
	capped := TruncateText(aiResponse, SummaryInputCap)
	return fmt.Sprintf(TTSUserPromptTemplate, capped)
}

// reMDLink matches Markdown link syntax [text](url).
var reMDLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// reMDImage matches Markdown image syntax ![alt](url).
var reMDImage = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)

// reBarePath matches bare file paths containing / or \ with common extensions.
var reBarePath = regexp.MustCompile(`[\w./\-\\]+\.(go|ts|js|py|rs|java|yaml|yml|json|toml|md|sql|mod|sum|proto)`)

// reLargeDigits matches sequences of 4+ consecutive digits (likely large numbers).
var reLargeDigits = regexp.MustCompile(`\b(\d{4,})\b`)

// reZeroWidth matches zero-width and invisible Unicode characters.
var reZeroWidth = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}\x{00AD}]`)

// SanitizeForSpeech strips Markdown artifacts, formatting characters, and
// TTS-unfriendly patterns that Edge TTS would read aloud as noise.
func SanitizeForSpeech(s string) string {
	// Strip zero-width and invisible characters.
	s = reZeroWidth.ReplaceAllString(s, "")

	// Strip images (keep alt text if present, otherwise remove entirely).
	s = reMDImage.ReplaceAllString(s, "")
	// Strip links (keep link text, drop URL).
	s = reMDLink.ReplaceAllString(s, "$1")
	// Strip code fences and inline code.
	s = strings.ReplaceAll(s, "```", "")
	s = strings.ReplaceAll(s, "`", "")
	// Strip bold/italic markers (order matters: ** before *).
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")

	// Replace bare file paths with generic description.
	s = reBarePath.ReplaceAllString(s, "相关文件")

	// Normalize large digit sequences to approximate Chinese readings.
	s = reLargeDigits.ReplaceAllStringFunc(s, normalizeLargeNumber)

	// Collapse lines, strip heading/blockquote markers.
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, "# >")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(trimmed)
		}
	}
	return strings.TrimSpace(b.String())
}

// normalizeLargeNumber converts large digit sequences to approximate Chinese readings.
// 48434 → "约4.8万", 200000 → "20万", 1500 → "1500" (under 1万, keep as-is).
func normalizeLargeNumber(digits string) string {
	n := 0
	for _, d := range digits {
		if d >= '0' && d <= '9' {
			n = n*10 + int(d-'0')
		}
	}
	if n < 10000 {
		return digits // under 1万, TTS reads fine
	}
	if n >= 100000000 {
		return fmt.Sprintf("%.1f亿", float64(n)/100000000)
	}
	return fmt.Sprintf("%.1f万", float64(n)/10000)
}
