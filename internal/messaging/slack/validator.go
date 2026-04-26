package slack

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/slack-go/slack"
)

// Slack Block Kit constraints per Slack API documentation
const (
	maxBlocksPerMessage   = 100
	maxSectionTextLength  = 3000
	maxContextElements    = 10
	maxContextTextLength  = 3000
	maxActionsElements    = 25
	maxActionIDLength     = 255
	maxButtonTextLength   = 75
	maxButtonValueLength  = 2000
	maxImageAltTextLength = 2000
	maxImageURLLength     = 3000
)

// ValidateBlocks checks blocks against Slack's schema constraints.
// Returns an error describing the first violation found.
func ValidateBlocks(blocks []slack.Block) error {
	// AC-3.1: Max 100 blocks per message
	if len(blocks) > maxBlocksPerMessage {
		return fmt.Errorf("block count %d exceeds maximum of %d", len(blocks), maxBlocksPerMessage)
	}

	// Track action IDs for duplicate detection
	actionIDs := make(map[string]bool)

	for i, block := range blocks {
		if block == nil {
			return fmt.Errorf("block %d is nil", i)
		}

		// Get block type for error messages
		blockType := block.BlockType()

		switch b := block.(type) {
		case *slack.SectionBlock:
			// AC-3.2: SectionBlock text ≤ 3000 chars
			if b.Text != nil && utf8.RuneCountInString(b.Text.Text) > maxSectionTextLength {
				return fmt.Errorf("block %d (%s): section text length %d exceeds maximum of %d",
					i, blockType, utf8.RuneCountInString(b.Text.Text), maxSectionTextLength)
			}

		case *slack.ContextBlock:
			// ContextBlock elements ≤ 10
			if len(b.ContextElements.Elements) > maxContextElements {
				return fmt.Errorf("block %d (%s): context elements count %d exceeds maximum of %d",
					i, blockType, len(b.ContextElements.Elements), maxContextElements)
			}
			// Context text elements ≤ 3000 chars
			for j, elem := range b.ContextElements.Elements {
				if textElem, ok := elem.(*slack.TextBlockObject); ok {
					if utf8.RuneCountInString(textElem.Text) > maxContextTextLength {
						return fmt.Errorf("block %d (%s) element %d: context text length %d exceeds maximum of %d",
							i, blockType, j, utf8.RuneCountInString(textElem.Text), maxContextTextLength)
					}
				}
			}

		case *slack.ActionBlock:
			// ActionsBlock elements ≤ 25
			if len(b.Elements.ElementSet) > maxActionsElements {
				return fmt.Errorf("block %d (%s): action elements count %d exceeds maximum of %d",
					i, blockType, len(b.Elements.ElementSet), maxActionsElements)
			}

			// Validate elements within action block
			for _, elem := range b.Elements.ElementSet {
				if err := validateBlockElement(elem, i, string(blockType), actionIDs); err != nil {
					return err
				}
			}

		case *slack.ImageBlock:
			// ImageBlock alt_text ≤ 2000 chars, image_url ≤ 3000 chars
			if utf8.RuneCountInString(b.AltText) > maxImageAltTextLength {
				return fmt.Errorf("block %d (%s): image alt_text length %d exceeds maximum of %d",
					i, blockType, utf8.RuneCountInString(b.AltText), maxImageAltTextLength)
			}
			if len(b.ImageURL) > maxImageURLLength {
				return fmt.Errorf("block %d (%s): image URL length %d exceeds maximum of %d",
					i, blockType, len(b.ImageURL), maxImageURLLength)
			}

		case *slack.DividerBlock:
			// Divider blocks have no additional constraints
			continue

		case *slack.HeaderBlock:
			// HeaderBlock text ≤ 150 chars (Slack constraint)
			if b.Text != nil && utf8.RuneCountInString(b.Text.Text) > 150 {
				return fmt.Errorf("block %d (%s): header text length %d exceeds maximum of 150",
					i, blockType, utf8.RuneCountInString(b.Text.Text))
			}

		default:
			// For unknown block types, attempt basic validation
			if err := validateUnknownBlock(block, i); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateBlockElement validates elements within blocks (buttons, etc.)
func validateBlockElement(elem slack.BlockElement, blockIdx int, blockType string, actionIDs map[string]bool) error {
	switch e := elem.(type) {
	case *slack.ButtonBlockElement:
		// ActionID ≤ 255 chars and unique
		if utf8.RuneCountInString(e.ActionID) > maxActionIDLength {
			return fmt.Errorf("block %d (%s): action_id length %d exceeds maximum of %d",
				blockIdx, blockType, utf8.RuneCountInString(e.ActionID), maxActionIDLength)
		}
		if actionIDs[e.ActionID] {
			return fmt.Errorf("block %d (%s): duplicate action_id %q",
				blockIdx, blockType, e.ActionID)
		}
		actionIDs[e.ActionID] = true

		// Button text ≤ 75 chars
		if e.Text != nil && utf8.RuneCountInString(e.Text.Text) > maxButtonTextLength {
			return fmt.Errorf("block %d (%s): button text length %d exceeds maximum of %d",
				blockIdx, blockType, utf8.RuneCountInString(e.Text.Text), maxButtonTextLength)
		}

		// Button value ≤ 2000 chars
		if utf8.RuneCountInString(e.Value) > maxButtonValueLength {
			return fmt.Errorf("block %d (%s): button value length %d exceeds maximum of %d",
				blockIdx, blockType, utf8.RuneCountInString(e.Value), maxButtonValueLength)
		}

	case *slack.OverflowBlockElement:
		if utf8.RuneCountInString(e.ActionID) > maxActionIDLength {
			return fmt.Errorf("block %d (%s): overflow action_id length %d exceeds maximum of %d",
				blockIdx, blockType, utf8.RuneCountInString(e.ActionID), maxActionIDLength)
		}
		if actionIDs[e.ActionID] {
			return fmt.Errorf("block %d (%s): duplicate action_id %q",
				blockIdx, blockType, e.ActionID)
		}
		actionIDs[e.ActionID] = true

	case *slack.SelectBlockElement:
		if utf8.RuneCountInString(e.ActionID) > maxActionIDLength {
			return fmt.Errorf("block %d (%s): select action_id length %d exceeds maximum of %d",
				blockIdx, blockType, utf8.RuneCountInString(e.ActionID), maxActionIDLength)
		}
		if actionIDs[e.ActionID] {
			return fmt.Errorf("block %d (%s): duplicate action_id %q",
				blockIdx, blockType, e.ActionID)
		}
		actionIDs[e.ActionID] = true
	}

	return nil
}

// validateUnknownBlock attempts basic validation on unknown block types
func validateUnknownBlock(_ slack.Block, _ int) error {
	// Try to get any text or action IDs through reflection-like checks
	// This is a best-effort for extensibility
	return nil
}

// SanitizeBlocks fixes common violations by truncating/removing.
// Returns a new slice containing the sanitized blocks.
// Note: Block objects may be mutated in-place.
// AC-3.3: Truncates text exceeding limits
// AC-3.4: Removes blocks beyond 100 limit
func SanitizeBlocks(blocks []slack.Block) []slack.Block {
	if len(blocks) == 0 {
		return blocks
	}

	// AC-3.4: Remove blocks beyond 100 limit
	if len(blocks) > maxBlocksPerMessage {
		blocks = blocks[:maxBlocksPerMessage]
	}

	actionIDs := make(map[string]bool)
	sanitized := make([]slack.Block, 0, len(blocks))

	for _, block := range blocks {
		if block == nil {
			continue
		}

		switch b := block.(type) {
		case *slack.SectionBlock:
			sanitized = append(sanitized, sanitizeSectionBlock(b))

		case *slack.ContextBlock:
			sanitized = append(sanitized, sanitizeContextBlock(b))

		case *slack.ActionBlock:
			sanitizedBlock := sanitizeActionBlock(b, actionIDs)
			if sanitizedBlock != nil {
				sanitized = append(sanitized, sanitizedBlock)
			}

		case *slack.ImageBlock:
			sanitized = append(sanitized, sanitizeImageBlock(b))

		case *slack.HeaderBlock:
			sanitized = append(sanitized, sanitizeHeaderBlock(b))

		default:
			// Keep other block types as-is
			sanitized = append(sanitized, block)
		}
	}

	return sanitized
}

func sanitizeSectionBlock(b *slack.SectionBlock) *slack.SectionBlock {
	if b.Text != nil && utf8.RuneCountInString(b.Text.Text) > maxSectionTextLength {
		b.Text.Text = truncateWithSuffix(b.Text.Text, maxSectionTextLength)
	}
	return b
}

func sanitizeContextBlock(b *slack.ContextBlock) *slack.ContextBlock {
	// Limit elements to maxContextElements
	if len(b.ContextElements.Elements) > maxContextElements {
		b.ContextElements.Elements = b.ContextElements.Elements[:maxContextElements]
	}

	// Truncate text elements
	for _, elem := range b.ContextElements.Elements {
		if textElem, ok := elem.(*slack.TextBlockObject); ok {
			if utf8.RuneCountInString(textElem.Text) > maxContextTextLength {
				textElem.Text = truncateWithSuffix(textElem.Text, maxContextTextLength)
			}
		}
	}

	return b
}

func sanitizeActionBlock(b *slack.ActionBlock, actionIDs map[string]bool) *slack.ActionBlock {
	// Limit elements to maxActionsElements
	if len(b.Elements.ElementSet) > maxActionsElements {
		b.Elements.ElementSet = b.Elements.ElementSet[:maxActionsElements]
	}

	// Sanitize each element and deduplicate action IDs
	sanitizedElements := make([]slack.BlockElement, 0, len(b.Elements.ElementSet))
	for _, elem := range b.Elements.ElementSet {
		sanitizedElem := sanitizeBlockElement(elem, actionIDs)
		if sanitizedElem != nil {
			sanitizedElements = append(sanitizedElements, sanitizedElem)
		}
	}

	// If no valid elements remain, return nil to remove the block
	if len(sanitizedElements) == 0 {
		return nil
	}

	b.Elements.ElementSet = sanitizedElements
	return b
}

func sanitizeBlockElement(elem slack.BlockElement, actionIDs map[string]bool) slack.BlockElement {
	switch e := elem.(type) {
	case *slack.ButtonBlockElement:
		// Truncate action_id if too long and deduplicate
		actionID := e.ActionID
		if utf8.RuneCountInString(actionID) > maxActionIDLength {
			actionID = truncateWithSuffix(actionID, maxActionIDLength)
		}

		// Handle duplicate action IDs
		if actionIDs[actionID] {
			// Append unique suffix
			for i := 1; i < 10000; i++ {
				newID := fmt.Sprintf("%s_%d", actionID[:min(len(actionID), maxActionIDLength-10)], i)
				if !actionIDs[newID] {
					actionID = newID
					break
				}
			}
		}
		actionIDs[actionID] = true
		e.ActionID = actionID

		// Truncate button text
		if e.Text != nil && utf8.RuneCountInString(e.Text.Text) > maxButtonTextLength {
			e.Text.Text = truncateWithSuffix(e.Text.Text, maxButtonTextLength)
		}

		// Truncate value
		if utf8.RuneCountInString(e.Value) > maxButtonValueLength {
			e.Value = truncateWithSuffix(e.Value, maxButtonValueLength)
		}

		return e

	case *slack.OverflowBlockElement:
		actionID := e.ActionID
		if utf8.RuneCountInString(actionID) > maxActionIDLength {
			actionID = truncateWithSuffix(actionID, maxActionIDLength)
		}
		if actionIDs[actionID] {
			for i := 1; i < 10000; i++ {
				newID := fmt.Sprintf("%s_%d", actionID[:min(len(actionID), maxActionIDLength-10)], i)
				if !actionIDs[newID] {
					actionID = newID
					break
				}
			}
		}
		actionIDs[actionID] = true
		e.ActionID = actionID
		return e

	case *slack.SelectBlockElement:
		actionID := e.ActionID
		if utf8.RuneCountInString(actionID) > maxActionIDLength {
			actionID = truncateWithSuffix(actionID, maxActionIDLength)
		}
		if actionIDs[actionID] {
			for i := 1; i < 10000; i++ {
				newID := fmt.Sprintf("%s_%d", actionID[:min(len(actionID), maxActionIDLength-10)], i)
				if !actionIDs[newID] {
					actionID = newID
					break
				}
			}
		}
		actionIDs[actionID] = true
		e.ActionID = actionID
		return e
	}

	return elem
}

func sanitizeImageBlock(b *slack.ImageBlock) *slack.ImageBlock {
	if utf8.RuneCountInString(b.AltText) > maxImageAltTextLength {
		b.AltText = truncateWithSuffix(b.AltText, maxImageAltTextLength)
	}
	if len(b.ImageURL) > maxImageURLLength {
		b.ImageURL = b.ImageURL[:maxImageURLLength]
	}
	return b
}

func sanitizeHeaderBlock(b *slack.HeaderBlock) *slack.HeaderBlock {
	if b.Text != nil && utf8.RuneCountInString(b.Text.Text) > 150 {
		b.Text.Text = truncateWithSuffix(b.Text.Text, 150)
	}
	return b
}

// truncateWithSuffix truncates text to maxLen runes, adding "..." suffix if truncated
func truncateWithSuffix(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// isInvalidBlocksError checks if a Slack API error is an invalid_blocks rejection.
// AC-3.5: Detects "invalid_blocks" in error message string
func isInvalidBlocksError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "invalid_blocks") ||
		strings.Contains(errStr, "invalid block") ||
		strings.Contains(errStr, "block_validation")
}
