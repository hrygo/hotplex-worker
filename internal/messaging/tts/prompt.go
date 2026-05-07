package tts

import (
	"regexp"
	"strings"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/brain/llm"
)

// SummaryInputCap limits the input text length sent to the LLM for summarization.
// Independent of MaxChars to ensure sufficient context even when MaxChars is small.
const SummaryInputCap = 2000

// TTSSummaryPrompt converts AI assistant responses into natural spoken Chinese.
// Format string: fmt.Sprintf(TTSSummaryPrompt, maxChars, truncatedText)
//
// Strategy: tiered English retention for developer audience.
//   - Tier A (keep English): high-frequency dev terms that developers say in English daily
//   - Tier B (Chinese + English parenthetical): terms needing explanation
//   - Tier C (Chinese only): code details, format symbols, paths, URLs
const TTSSummaryPrompt = "你是一位面向开发者的语音播报编辑。将 AI 助手的回复改写成自然口语播报稿。\n\n" +
	"禁止出现：Markdown格式符号、代码片段、文件路径、URL。\n" +
	"禁止出现：井号、星号、反引号、竖线、方括号、花括号。\n" +
	"可以使用中文标点（逗号、句号、问号、叹号），有助于语音停顿。\n\n" +
	"英文处理规则（按开发者日常口语习惯）：\n" +
	"1. 开发者高频术语保留英文原词，如 API、PR、CI、Git、Bug、Deploy、Build、Token、Docker、WebSocket\n" +
	"2. 需要解释的术语用中文括注，如 RBAC（基于角色的访问控制）\n" +
	"3. 代码细节、提交信息、路径用中文概括，如 fix(tts): replace 即 修复了语音合成的依赖问题\n" +
	"4. 纯文本输出，无格式符号\n" +
	"5. 保留核心结论，控制在 %d 字符以内（3到4句话）\n\n" +
	"示例：\n" +
	"PR #295 已创建，CI 全绿，可以合并。\n" +
	"修复了 WebSocket 连接失败的问题，原因是缺少安全令牌。\n" +
	"已将 Bug 修复代码推送到 Git 仓库，Build 通过。\n" +
	"建议升级 Docker 镜像版本以解决 API 兼容性问题。\n\n" +
	"AI 回复：\n%s"

// SummaryChatOpts controls LLM generation for TTS summaries.
// MaxTokens=256 covers 150 Chinese characters (~225 tokens) with margin.
// Temperature=0.3 ensures consistent, focused summaries.
var SummaryChatOpts = brain.ChatOptions{
	MaxTokens:   256,
	Temperature: llm.FloatPtr(0.3),
}

// reMDLink matches Markdown link syntax [text](url).
var reMDLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// reMDImage matches Markdown image syntax ![alt](url).
var reMDImage = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)

// SanitizeForSpeech strips Markdown artifacts and formatting characters
// that Edge TTS would read aloud as punctuation noise.
func SanitizeForSpeech(s string) string {
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
