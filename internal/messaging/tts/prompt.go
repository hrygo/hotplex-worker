package tts

import "github.com/hrygo/hotplex/internal/brain"

// SummaryInputCap limits the input text length sent to the LLM for summarization.
// Independent of MaxChars to ensure sufficient context even when MaxChars is small.
const SummaryInputCap = 2000

// TTSSummaryPrompt converts AI assistant responses into natural speech text.
// Format string: fmt.Sprintf(TTSSummaryPrompt, maxChars, truncatedText)
const TTSSummaryPrompt = `你是一位语音播报编辑。将以下 AI 助手的回复改写为适合语音播报的自然口语。

要求：
- 直接输出播报文本，不要加任何前缀或说明
- 将所有 Markdown 格式（标题、加粗、代码块、列表）转为自然连贯的口语
- 代码和技术细节用一句话概括（如"已提供代码实现"）
- 表格和列表转为文字叙述
- 省略 URL、文件路径等技术性内容
- 保留核心结论和关键信息
- 中英文混合时，英文术语原样保留
- 控制在 %d 字符以内（约 3-4 句话）

AI 回复：
%s`

// SummaryChatOpts controls LLM generation for TTS summaries.
// MaxTokens=256 covers 150 Chinese characters (~225 tokens) with margin.
// Temperature=0.3 ensures consistent, focused summaries.
var SummaryChatOpts = brain.ChatOptions{
	MaxTokens:   256,
	Temperature: 0.3,
}
