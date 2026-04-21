package slack

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

// AC-3.1: ValidateBlocks rejects blocks with >100 elements
func TestValidateBlocks_MaxBlocks(t *testing.T) {
	t.Parallel()

	// Create 101 blocks (exceeds limit)
	blocks := make([]slack.Block, 101)
	for i := 0; i < 101; i++ {
		blocks[i] = slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		)
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum")
	require.Contains(t, err.Error(), "100")
}

func TestValidateBlocks_ValidBlockCount(t *testing.T) {
	t.Parallel()

	// Create exactly 100 blocks (at limit)
	blocks := make([]slack.Block, 100)
	for i := 0; i < 100; i++ {
		blocks[i] = slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		)
	}

	err := ValidateBlocks(blocks)
	require.NoError(t, err)
}

// AC-3.2: ValidateBlocks rejects section text >3000 chars
func TestValidateBlocks_SectionTextTooLong(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 3001)
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, longText, false, false),
			nil, nil,
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "section text")
	require.Contains(t, err.Error(), "3000")
}

func TestValidateBlocks_SectionTextAtLimit(t *testing.T) {
	t.Parallel()

	exactText := strings.Repeat("a", 3000)
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, exactText, false, false),
			nil, nil,
		),
	}

	err := ValidateBlocks(blocks)
	require.NoError(t, err)
}

// AC-3.3: SanitizeBlocks truncates text exceeding limits
func TestSanitizeBlocks_TruncatesSectionText(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 3001)
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, longText, false, false),
			nil, nil,
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	section, ok := sanitized[0].(*slack.SectionBlock)
	require.True(t, ok)
	require.Len(t, section.Text.Text, 3000)
	require.True(t, strings.HasSuffix(section.Text.Text, "..."))
}

// AC-3.4: SanitizeBlocks removes blocks beyond 100 limit
func TestSanitizeBlocks_RemovesExcessBlocks(t *testing.T) {
	t.Parallel()

	// Create 101 blocks
	blocks := make([]slack.Block, 101)
	for i := 0; i < 101; i++ {
		blocks[i] = slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		)
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 100)
}

func TestSanitizeBlocks_KeepsBlocksWithinLimit(t *testing.T) {
	t.Parallel()

	blocks := make([]slack.Block, 50)
	for i := 0; i < 50; i++ {
		blocks[i] = slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		)
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 50)
}

// Test context block validation
func TestValidateBlocks_ContextElementsCount(t *testing.T) {
	t.Parallel()

	// Create 11 context elements (exceeds limit of 10)
	elements := make([]slack.MixedElement, 11)
	for i := 0; i < 11; i++ {
		elements[i] = slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false)
	}

	blocks := []slack.Block{
		slack.NewContextBlock("ctx", elements...),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context elements")
	require.Contains(t, err.Error(), "10")
}

func TestValidateBlocks_ContextTextTooLong(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 3001)
	blocks := []slack.Block{
		slack.NewContextBlock("ctx",
			slack.NewTextBlockObject(slack.MarkdownType, longText, false, false),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "context text")
}

// Test action block validation
func TestValidateBlocks_ActionElementsCount(t *testing.T) {
	t.Parallel()

	// Create 26 buttons (exceeds limit of 25)
	buttons := make([]slack.BlockElement, 26)
	for i := 0; i < 26; i++ {
		buttons[i] = slack.NewButtonBlockElement(
			fmt.Sprintf("action_%d", i),
			"value",
			slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false),
		)
	}

	blocks := []slack.Block{
		slack.NewActionBlock("actions", buttons...),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action elements")
	require.Contains(t, err.Error(), "25")
}

func TestValidateBlocks_ButtonTextTooLong(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 76)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewButtonBlockElement(
				"action_id",
				"value",
				slack.NewTextBlockObject(slack.PlainTextType, longText, false, false),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "button text")
	require.Contains(t, err.Error(), "75")
}

func TestValidateBlocks_ButtonValueTooLong(t *testing.T) {
	t.Parallel()

	longValue := strings.Repeat("a", 2001)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewButtonBlockElement(
				"action_id",
				longValue,
				slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "button value")
	require.Contains(t, err.Error(), "2000")
}

func TestValidateBlocks_ActionIDTooLong(t *testing.T) {
	t.Parallel()

	longID := strings.Repeat("a", 256)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewButtonBlockElement(
				longID,
				"value",
				slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action_id")
	require.Contains(t, err.Error(), "255")
}

func TestValidateBlocks_DuplicateActionID(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewActionBlock("actions1",
			slack.NewButtonBlockElement(
				"duplicate_id",
				"value1",
				slack.NewTextBlockObject(slack.PlainTextType, "Button 1", false, false),
			),
		),
		slack.NewActionBlock("actions2",
			slack.NewButtonBlockElement(
				"duplicate_id",
				"value2",
				slack.NewTextBlockObject(slack.PlainTextType, "Button 2", false, false),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate action_id")
}

// Test image block validation
func TestValidateBlocks_ImageAltTextTooLong(t *testing.T) {
	t.Parallel()

	longAlt := strings.Repeat("a", 2001)
	blocks := []slack.Block{
		slack.NewImageBlock("https://example.com/image.png", longAlt, "", nil),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "image alt_text")
	require.Contains(t, err.Error(), "2000")
}

func TestValidateBlocks_ImageURLTooLong(t *testing.T) {
	t.Parallel()

	longURL := "https://example.com/" + strings.Repeat("a", 3000)
	blocks := []slack.Block{
		slack.NewImageBlock(longURL, "image", "", nil),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "image URL")
	require.Contains(t, err.Error(), "3000")
}

// AC-3.5: isInvalidBlocksError detects invalid_blocks errors
func TestIsInvalidBlocksError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "invalid_blocks error",
			err:      &mockError{msg: "slack API error: invalid_blocks"},
			expected: true,
		},
		{
			name:     "invalid block error (lowercase)",
			err:      &mockError{msg: "slack API error: invalid block"},
			expected: true,
		},
		{
			name:     "block_validation error",
			err:      &mockError{msg: "block_validation failed"},
			expected: true,
		},
		{
			name:     "other error",
			err:      &mockError{msg: "rate limited"},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isInvalidBlocksError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// Test sanitization of various block types
func TestSanitizeBlocks_ContextElements(t *testing.T) {
	t.Parallel()

	// Create 11 context elements
	elements := make([]slack.MixedElement, 11)
	for i := 0; i < 11; i++ {
		elements[i] = slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false)
	}

	blocks := []slack.Block{
		slack.NewContextBlock("ctx", elements...),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	ctx, ok := sanitized[0].(*slack.ContextBlock)
	require.True(t, ok)
	require.Len(t, ctx.ContextElements.Elements, 10)
}

func TestSanitizeBlocks_ContextTextTruncation(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 3001)
	blocks := []slack.Block{
		slack.NewContextBlock("ctx",
			slack.NewTextBlockObject(slack.MarkdownType, longText, false, false),
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	ctx, ok := sanitized[0].(*slack.ContextBlock)
	require.True(t, ok)
	require.Len(t, ctx.ContextElements.Elements, 1)

	text, ok := ctx.ContextElements.Elements[0].(*slack.TextBlockObject)
	require.True(t, ok)
	require.Len(t, text.Text, 3000)
	require.True(t, strings.HasSuffix(text.Text, "..."))
}

func TestSanitizeBlocks_ActionElements(t *testing.T) {
	t.Parallel()

	// Create 26 buttons
	buttons := make([]slack.BlockElement, 26)
	for i := 0; i < 26; i++ {
		buttons[i] = slack.NewButtonBlockElement(
			fmt.Sprintf("action_%d", i),
			"value",
			slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false),
		)
	}

	blocks := []slack.Block{
		slack.NewActionBlock("actions", buttons...),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	action, ok := sanitized[0].(*slack.ActionBlock)
	require.True(t, ok)
	require.Len(t, action.Elements.ElementSet, 25)
}

func TestSanitizeBlocks_DuplicateActionIDDeduplication(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewActionBlock("actions1",
			slack.NewButtonBlockElement(
				"duplicate_id",
				"value1",
				slack.NewTextBlockObject(slack.PlainTextType, "Button 1", false, false),
			),
		),
		slack.NewActionBlock("actions2",
			slack.NewButtonBlockElement(
				"duplicate_id",
				"value2",
				slack.NewTextBlockObject(slack.PlainTextType, "Button 2", false, false),
			),
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 2)

	// First action block's button should keep original ID
	action1, ok := sanitized[0].(*slack.ActionBlock)
	require.True(t, ok)
	btn1, ok := action1.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	require.True(t, ok)
	require.Equal(t, "duplicate_id", btn1.ActionID)

	// Second action block's button should have a deduplicated ID
	action2, ok := sanitized[1].(*slack.ActionBlock)
	require.True(t, ok)
	btn2, ok := action2.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	require.True(t, ok)
	require.NotEqual(t, "duplicate_id", btn2.ActionID)
	require.Contains(t, btn2.ActionID, "duplicate_id")
}

func TestSanitizeBlocks_ButtonTextTruncation(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 76)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewButtonBlockElement(
				"action_id",
				"value",
				slack.NewTextBlockObject(slack.PlainTextType, longText, false, false),
			),
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	action, ok := sanitized[0].(*slack.ActionBlock)
	require.True(t, ok)
	btn, ok := action.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	require.True(t, ok)
	require.Len(t, btn.Text.Text, 75)
	require.True(t, strings.HasSuffix(btn.Text.Text, "..."))
}

func TestSanitizeBlocks_ButtonValueTruncation(t *testing.T) {
	t.Parallel()

	longValue := strings.Repeat("a", 2001)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewButtonBlockElement(
				"action_id",
				longValue,
				slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false),
			),
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	action, ok := sanitized[0].(*slack.ActionBlock)
	require.True(t, ok)
	btn, ok := action.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	require.True(t, ok)
	require.Len(t, btn.Value, 2000)
	require.True(t, strings.HasSuffix(btn.Value, "..."))
}

func TestSanitizeBlocks_ImageAltTextTruncation(t *testing.T) {
	t.Parallel()

	longAlt := strings.Repeat("a", 2001)
	blocks := []slack.Block{
		slack.NewImageBlock("https://example.com/image.png", longAlt, "", nil),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	img, ok := sanitized[0].(*slack.ImageBlock)
	require.True(t, ok)
	require.Len(t, img.AltText, 2000)
	require.True(t, strings.HasSuffix(img.AltText, "..."))
}

func TestSanitizeBlocks_ImageURLTruncation(t *testing.T) {
	t.Parallel()

	longURL := "https://example.com/" + strings.Repeat("a", 3000)
	blocks := []slack.Block{
		slack.NewImageBlock(longURL, "image", "", nil),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	img, ok := sanitized[0].(*slack.ImageBlock)
	require.True(t, ok)
	require.Len(t, img.ImageURL, 3000)
}

func TestSanitizeBlocks_HeaderTextTruncation(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("a", 151)
	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, longText, false, false),
		),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 1)

	header, ok := sanitized[0].(*slack.HeaderBlock)
	require.True(t, ok)
	require.Len(t, header.Text.Text, 150)
	require.True(t, strings.HasSuffix(header.Text.Text, "..."))
}

func TestSanitizeBlocks_NilBlock(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		),
		nil,
		slack.NewDividerBlock(),
	}

	sanitized := SanitizeBlocks(blocks)
	require.Len(t, sanitized, 2)
}

func TestSanitizeBlocks_EmptyBlocks(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{}
	sanitized := SanitizeBlocks(blocks)
	require.Empty(t, sanitized)
}

// Test truncateWithSuffix helper
func TestTruncateWithSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short text", 100, "short text"},
		{strings.Repeat("a", 3000), 3000, strings.Repeat("a", 3000)},
		{strings.Repeat("a", 3001), 3000, strings.Repeat("a", 2997) + "..."},
		{"ab", 2, "ab"},
		{"abc", 2, "ab"}, // edge case: maxLen <= 3
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("len_%d_max_%d", len(tt.input), tt.maxLen), func(t *testing.T) {
			t.Parallel()
			result := truncateWithSuffix(tt.input, tt.maxLen)
			require.Equal(t, tt.expected, result)
		})
	}
}

// Test nil block validation
func TestValidateBlocks_NilBlock(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "Test", false, false),
			nil, nil,
		),
		nil,
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

// Test overflow and select elements
func TestValidateBlocks_OverflowActionIDTooLong(t *testing.T) {
	t.Parallel()

	longID := strings.Repeat("a", 256)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewOverflowBlockElement(longID,
				slack.NewOptionBlockObject("opt1", slack.NewTextBlockObject(slack.PlainTextType, "Option 1", false, false), nil),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action_id")
}

func TestValidateBlocks_StaticSelectActionIDTooLong(t *testing.T) {
	t.Parallel()

	longID := strings.Repeat("a", 256)
	blocks := []slack.Block{
		slack.NewActionBlock("actions",
			slack.NewOptionsSelectBlockElement(
				slack.OptTypeStatic,
				slack.NewTextBlockObject(slack.PlainTextType, "Select", false, false),
				longID,
				slack.NewOptionBlockObject("opt1", slack.NewTextBlockObject(slack.PlainTextType, "Option 1", false, false), nil),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.Error(t, err)
	require.Contains(t, err.Error(), "action_id")
}

func TestValidateBlocks_EmptyBlocks(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{}
	err := ValidateBlocks(blocks)
	require.NoError(t, err)
}

// Integration test: Full permission request style validation
func TestValidateBlocks_PermissionRequestStyle(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "*Tool Approval Required*\nClaude Code requests permission to run:\n`test_tool`", false, false),
			nil, nil,
		),
		slack.NewActionBlock(
			"permission_actions",
			slack.NewButtonBlockElement(
				"hp_interact/allow/req123",
				"allow",
				slack.NewTextBlockObject(slack.PlainTextType, "Allow", false, true),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				"hp_interact/deny/req123",
				"deny",
				slack.NewTextBlockObject(slack.PlainTextType, "Deny", false, true),
			).WithStyle(slack.StyleDanger),
		),
	}

	err := ValidateBlocks(blocks)
	require.NoError(t, err)
}

// Integration test: Full question request style validation
func TestValidateBlocks_QuestionRequestStyle(t *testing.T) {
	t.Parallel()

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "*Question*\nWhat would you like to do?", false, false),
			nil, nil,
		),
		slack.NewActionBlock(
			"question_actions",
			slack.NewButtonBlockElement(
				"hp_interact/answer/req123",
				"option1",
				slack.NewTextBlockObject(slack.PlainTextType, "Option 1 — Description", false, true),
			),
			slack.NewButtonBlockElement(
				"hp_interact/answer/req124",
				"option2",
				slack.NewTextBlockObject(slack.PlainTextType, "Option 2 — Description", false, true),
			),
		),
	}

	err := ValidateBlocks(blocks)
	require.NoError(t, err)
}

// Test min helper
func TestMin(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, min(1, 2))
	require.Equal(t, 1, min(2, 1))
	require.Equal(t, 5, min(5, 5))
}
