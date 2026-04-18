// Package feishu provides a Feishu (Lark) WebSocket platform adapter.
package feishu

import (
	"regexp"
	"strconv"
	"strings"
)

// FEISHU_CARD_TABLE_LIMIT is the maximum number of markdown tables allowed in a
// single Feishu interactive card before excess tables must be wrapped in fenced
// code blocks to avoid CardKit error 230099 (ErrCode: 11310).
//
// This limit was empirically determined in March 2026: 4 or more tables in a
// single card body triggers the error. Cards with 0-3 tables render normally.
const FEISHU_CARD_TABLE_LIMIT = 3

// tableMatch records the byte offsets of one complete markdown table.
// All offsets are relative to the original input string.
type tableMatch struct {
	start int // byte index of the first '|' in the header row
	end   int // byte index one past the last newline of the table
}

// ---------------------------------------------------------------------------
// Table detection
// ---------------------------------------------------------------------------

// codeBlockRe matches fenced code blocks (``` or ~~~). Nested fences are
// not valid per CommonMark, so we stop at the first closing fence.
var codeBlockRe = regexp.MustCompile("```[\\s\\S]*?```|~~~[\\s\\S]*?~~~")

// findTablesOutsideCodeBlocks scans text for complete markdown tables that are
// not inside fenced code blocks. A valid table has:
//   - A header row: one or more pipe-separated cells, preceded by \n\n or string start
//   - A separator row: cells made entirely of -, :, space
//   - Zero or more data rows
//
// A table ends when we encounter a non-table line (no leading |) or EOF.

func findTablesOutsideCodeBlocks(text string) []tableMatch {
	if !strings.Contains(text, "|") {
		return nil
	}

	// Collect code block ranges for exclusion.
	cbRanges := codeBlockRe.FindAllStringIndex(text, -1)
	isInsideCodeBlock := func(idx int) bool {
		for _, r := range cbRanges {
			if idx >= r[0] && idx < r[1] {
				return true
			}
		}
		return false
	}

	var matches []tableMatch
	searchFrom := 0

	// Check for a table at the very start of text (no preceding \n\n).
	if len(text) > 0 && text[0] == '|' && !isInsideCodeBlock(0) {
		if m, ok := tryParseTableAt(text, 0); ok {
			matches = append(matches, m)
			searchFrom = nextSearchFrom(text, m.end)
		}
	}

	// Scan for \n\n paragraph boundaries — table starts.
	for {
		if searchFrom >= len(text) {
			break
		}
		np := strings.Index(text[searchFrom:], "\n\n")
		if np < 0 {
			break
		}
		pos := searchFrom + np

		// After \n\n, skip any blank lines to find the header.
		afterBlank := pos + 2
		if afterBlank >= len(text) {
			break
		}
		for afterBlank < len(text) && (text[afterBlank] == '\n' || text[afterBlank] == ' ') {
			afterBlank++
		}
		if afterBlank >= len(text) {
			break
		}

		if isInsideCodeBlock(afterBlank) {
			searchFrom = afterBlank
			continue
		}

		if m, ok := tryParseTableAt(text, afterBlank); ok {
			matches = append(matches, m)
			searchFrom = nextSearchFrom(text, m.end)
		} else {
			searchFrom = afterBlank
		}
	}

	return matches
}

// tryParseTableAt attempts to parse a complete markdown table starting at
// headerStart. Returns the table match and true on success.
func tryParseTableAt(text string, headerStart int) (tableMatch, bool) {
	// Find end of header line.
	headerEnd := headerStart
	for headerEnd < len(text) && text[headerEnd] != '\n' {
		headerEnd++
	}
	headerLine := text[headerStart:headerEnd]
	if !isMarkdownTableHeader(headerLine) {
		return tableMatch{}, false
	}

	// Find separator line (skip the \n after header, if present).
	sepStart := headerEnd
	if sepStart < len(text) && text[sepStart] == '\n' {
		sepStart++
	}
	if sepStart >= len(text) {
		return tableMatch{}, false
	}
	sepEnd := sepStart
	for sepEnd < len(text) && text[sepEnd] != '\n' {
		sepEnd++
	}
	if !isSeparatorLine(text[sepStart:sepEnd]) {
		return tableMatch{}, false
	}

	// Scan data rows until a non-table line, blank line, or EOF.
	tableEnd := sepEnd + 1
	for tableEnd < len(text) {
		if text[tableEnd] == '\n' {
			break
		}
		lineEnd := tableEnd
		for lineEnd < len(text) && text[lineEnd] != '\n' {
			lineEnd++
		}
		if isMarkdownTableRow(text[tableEnd:lineEnd]) {
			if lineEnd < len(text) {
				tableEnd = lineEnd + 1
			} else {
				tableEnd = lineEnd
			}
		} else {
			break
		}
	}

	return tableMatch{start: headerStart, end: tableEnd}, true
}

// nextSearchFrom returns the position to resume scanning after a table ending
// at tableEnd. Backs up one byte when the table ends on a \n so the main loop
// can detect the \n\n boundary formed by the table's trailing \n and the next \n.
func nextSearchFrom(text string, tableEnd int) int {
	if tableEnd > 0 && tableEnd < len(text) && text[tableEnd] == '\n' {
		return tableEnd - 1
	}
	return tableEnd
}

// isMarkdownTableHeader returns true if s looks like a markdown table header row.
// A header row starts and ends with | and has two or more pipe-separated cells.
// It filters out separator rows (all cells being "-", ":", space) and
// code-block examples (cells containing fence characters like ``` or ~~~).
func isMarkdownTableHeader(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
		return false
	}
	cells := strings.Split(s[1:len(s)-1], "|")
	if len(cells) < 2 {
		return false
	}
	// Reject if all cells are separator-like (only -: chars).
	allSeparator := true
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if !isOnlySepChars(cell) {
			allSeparator = false
			break
		}
	}
	if allSeparator {
		return false
	}
	// Reject code-block examples: cells containing fence characters.
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if strings.Contains(cell, "```") || strings.Contains(cell, "~~~") {
			return false
		}
	}
	return true
}

// isOnlySepChars returns true if s consists only of -, :, and space.
func isOnlySepChars(s string) bool {
	for _, c := range s {
		if c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

// isMarkdownTableRow returns true if s is a data row (starts with |).
func isMarkdownTableRow(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "|")
}

// isSeparatorLine returns true if s is a valid markdown table separator row.
// Valid separators consist only of -, :, and space, and contain at least one -.
func isSeparatorLine(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
		return false
	}
	// Strip leading and trailing pipes.
	s = strings.Trim(s, "|")
	// Each cell must be made of -, :, space.
	hasDash := false
	for _, cell := range strings.Split(s, "|") {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		for _, ch := range cell {
			if ch != '-' && ch != ':' && ch != ' ' {
				return false
			}
			if ch == '-' {
				hasDash = true
			}
		}
	}
	return hasDash
}

// CountTables returns the number of markdown tables in text, excluding tables
// that appear inside fenced code blocks.
func CountTables(text string) int {
	return len(findTablesOutsideCodeBlocks(text))
}

// ---------------------------------------------------------------------------
// Card-use pre-check
// ---------------------------------------------------------------------------

// ShouldUseCard returns true when text benefits from being sent as an
// interactive Feishu card rather than a plain text message.
//
// Decision logic:
//   - Contains fenced code blocks  → use card
//   - Contains 1-3 tables        → use card
//   - Contains 4+ tables          → do not use card (exceeds limit)
//   - Plain text only             → do not use card
//
// The result governs the outer transport decision.  The caller must still
// run SanitizeForCard on the text before embedding it in a card.
func ShouldUseCard(text string) bool {
	if codeBlockRe.MatchString(text) {
		return true
	}
	n := CountTables(text)
	if n > 0 && n <= FEISHU_CARD_TABLE_LIMIT {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Table sanitization
// ---------------------------------------------------------------------------

// wrapTablesBeyondLimit wraps surplus tables (beyond FEISHU_CARD_TABLE_LIMIT)
// in fenced code blocks so Feishu does not parse them as card table elements.
// Tables are processed back-to-front to keep earlier indices stable during
// replacement.  Tables inside fenced code blocks are untouched.
func wrapTablesBeyondLimit(text string, matches []tableMatch, keepCount int) string {
	if len(matches) <= keepCount {
		return text
	}

	// Process back-to-front so slicing from the front doesn't shift indices.
	for i := len(matches) - 1; i >= keepCount; i-- {
		m := matches[i]
		fenced := "```\n" + text[m.start:m.end] + "```"
		text = text[:m.start] + fenced + text[m.end:]
	}
	return text
}

// SanitizeForCard transforms text so it is safe for a Feishu interactive card.
//   - First FEISHU_CARD_TABLE_LIMIT tables are kept as-is.
//   - Surplus tables are wrapped in fenced code blocks (back-to-front).
//   - Tables inside fenced code blocks are not counted or modified.
//
// This function is safe to call repeatedly; wrapping already-fenced tables
// has no additional effect.
func SanitizeForCard(text string) string {
	matches := findTablesOutsideCodeBlocks(text)
	if len(matches) <= FEISHU_CARD_TABLE_LIMIT {
		return text
	}
	return wrapTablesBeyondLimit(text, matches, FEISHU_CARD_TABLE_LIMIT)
}

// ---------------------------------------------------------------------------
// Markdown style optimization
// ---------------------------------------------------------------------------

const (
	codePlaceholderPrefix = "___CB_"
	codePlaceholderSuffix = "___"
)

// extractCodeBlocks replaces every fenced code block with a placeholder token
// and returns the cleaned text plus the original blocks in order.
func extractCodeBlocks(text string) (string, []string) {
	blocks := []string{}
	replaced := 0
	result := codeBlockRe.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := codePlaceholderPrefix + strconv.Itoa(replaced) + codePlaceholderSuffix
		blocks = append(blocks, match)
		replaced++
		return placeholder
	})
	return result, blocks
}

// restoreCodeBlocks replaces placeholder tokens with their original code blocks,
// padding each with a leading and trailing <br> so they are visually separated
// from surrounding markdown content in Feishu cards.
func restoreCodeBlocks(text string, blocks []string) string {
	for i, block := range blocks {
		placeholder := codePlaceholderPrefix + strconv.Itoa(i) + codePlaceholderSuffix
		padded := "\n<br>\n" + block + "\n<br>\n"
		text = strings.Replace(text, placeholder, padded, 1)
	}
	return text
}

// hasHeading reports whether text contains any H1-H3 heading.
var hasHeadingRe = regexp.MustCompile(`(?m)^#{1,3} `)

// hasHeadingH4H5Re reports whether text contains any H4-H5 heading (used
// by OptimizeMarkdownStyle to detect consecutive-heading spacing needs).
var hasHeadingH4H5Re = regexp.MustCompile(`(?m)^#{4,5} `)

func hasHeading(text string) bool {
	return hasHeadingRe.MatchString(text)
}

// headingDemotionRe1 matches H2-H6 headings.
var headingDemotionRe1 = regexp.MustCompile(`(?m)^#{2,6} `)

// headingDemotionRe2 matches H1 headings (applied after H2-H6 to avoid
// re-matching the H1 that was just changed to H4).
var headingDemotionRe2 = regexp.MustCompile(`(?m)^# `)

// dedupeNewlinesRe replaces runs of 3+ newlines with exactly 2.
var dedupeNewlinesRe = regexp.MustCompile(`(?:\n){3,}`)

// tableBeforeTextRe adds a blank line before a table if preceded by non-blank
// non-table text (to prevent the table from being rendered as continuation
// of the previous paragraph).
var tableBeforeTextRe = regexp.MustCompile(`(?m)^([^|\n].*)\n(\|[^\n]+\|)`)

// tableBeforeBreakRe adds a <br> before the blank line that precedes a table.
var tableBeforeBreakRe = regexp.MustCompile(`\n\n(\|[^\n]+\|(?:\n\|[^\n]+\|)*\n)`)

// tableAfterBreakRe adds a <br> after a table block when followed by plain
// text (not a heading, bold, or end-of-string).
var tableAfterBreakRe = regexp.MustCompile(`(?m)^(\|[^\n]+\|(?:\n\|[^\n]+\|)*\n)`)

// consecutiveHeadingsRe adds <br> between consecutive H4/H5 headings to prevent
// CardKit rendering collapse.
var consecutiveHeadingsRe = regexp.MustCompile(`(?m)^(#{4,5} .+)\n{1,2}(#{4,5} )`)

// OptimizeMarkdownStyle applies 6 stylistic passes to improve Feishu card
// rendering:
//
//  1. Extract fenced code blocks (replace with placeholders).
//  2. H1→H4, H2-H6→H5  (only when the document contains H1-H3).
//  3. <br> between consecutive H4/H5 headings.
//  4. <br> before and after tables.
//  5. Restore code blocks with <br> padding.
//  6. Collapse runs of 3+ newlines to 2.
//
// Code block content is never modified.
func OptimizeMarkdownStyle(text string) string {
	if text == "" {
		return text
	}
	// Fast path: skip if no markdown constructs that need processing.
	// hasHeadingRe covers H1-H3 (demotion); hasHeadingH4H5Re covers H4-H5 (consecutive spacing).
	if !hasHeadingRe.MatchString(text) && !hasHeadingH4H5Re.MatchString(text) && !strings.Contains(text, "|") && !codeBlockRe.MatchString(text) {
		return text
	}

	// 1. Extract code blocks.
	clean, blocks := extractCodeBlocks(text)

	// 2. Heading demotion (only if document has H1-H3).
	if hasHeading(clean) {
		clean = headingDemotionRe1.ReplaceAllString(clean, "##### ")
		clean = headingDemotionRe2.ReplaceAllString(clean, "#### ")
	}

	// 3. Consecutive heading spacing.
	clean = consecutiveHeadingsRe.ReplaceAllString(clean, "$1\n<br>\n$2")

	// 4. Table spacing: <br> before and after tables.
	// 4a. Ensure blank line before table.
	clean = tableBeforeTextRe.ReplaceAllString(clean, "$1\n\n$2")
	// 4b. Insert <br> before table (on the blank-line side).
	clean = tableBeforeBreakRe.ReplaceAllString(clean, "\n<br>\n\n$1")
	// 4c. Append <br> after table when followed by plain text.
	clean = tableAfterBreakRe.ReplaceAllStringFunc(clean, func(match string) string {
		// Find the text following this table block.
		idx := strings.Index(text, match)
		if idx < 0 {
			return match
		}
		after := text[idx+len(match):]
		after = strings.TrimPrefix(after, "\n")
		// Only add <br> if followed by plain text (not heading/bold/end).
		if after == "" || strings.HasPrefix(after, "#") || strings.HasPrefix(after, "**") {
			return match
		}
		return match + "<br>\n"
	})

	// 5. Restore code blocks with <br> padding.
	clean = restoreCodeBlocks(clean, blocks)

	// 6. Collapse extra newlines.
	clean = dedupeNewlinesRe.ReplaceAllString(clean, "\n\n")

	return clean
}

// ---------------------------------------------------------------------------
// Image key filtering
// ---------------------------------------------------------------------------

// imageRe matches markdown image syntax: ![alt](value)
var imageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

// StripInvalidImageKeys removes markdown image references that point to URLs
// or keys that are not valid Feishu image keys (i.e. do not start with "img_").
// Invalid references are replaced with empty string.  Valid references are
// left unchanged.
func StripInvalidImageKeys(text string) string {
	if !strings.Contains(text, "![") {
		return text
	}
	return imageRe.ReplaceAllStringFunc(text, func(match string) string {
		// Extract URL from match format: ![alt](url)
		// Find the opening paren and closing paren.
		open := strings.Index(match, "](")
		if open < 0 {
			return match
		}
		url := match[open+2 : len(match)-1]
		if strings.HasPrefix(url, "img_") {
			return match
		}
		return ""
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
