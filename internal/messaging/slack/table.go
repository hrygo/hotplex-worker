package slack

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// TextSegment represents a non-table portion of the original markdown text.
type TextSegment struct {
	Text string
}

// ParsedTable holds the structured data extracted from a markdown table.
type ParsedTable struct {
	Headers   []string
	Rows      [][]string
	ColAligns []slack.ColumnAlignment
}

// ---------------------------------------------------------------------------
// Table detection (reuses Feishu-validated regex patterns)
// ---------------------------------------------------------------------------

var codeBlockTableRe = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")

// isMarkdownTableHeader returns true if s looks like a markdown table header row.
func isMarkdownTableHeader(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
		return false
	}
	cells := strings.Split(s[1:len(s)-1], "|")
	if len(cells) < 2 {
		return false
	}
	allSep := true
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if !isOnlySepChars(c) {
			allSep = false
		}
	}
	if allSep {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if strings.Contains(c, "```") || strings.Contains(c, "~~~") {
			return false
		}
	}
	return true
}

func isOnlySepChars(s string) bool {
	for _, c := range s {
		if c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

func isSeparatorLine(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "|") || !strings.HasSuffix(s, "|") {
		return false
	}
	s = strings.Trim(s, "|")
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

func isMarkdownTableRow(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "|")
}

// ---------------------------------------------------------------------------
// Column alignment from separator
// ---------------------------------------------------------------------------

func parseColumnAlign(sepRow string) []slack.ColumnAlignment {
	sepRow = strings.TrimSpace(sepRow)
	sepRow = strings.Trim(sepRow, "|")
	cells := strings.Split(sepRow, "|")
	aligns := make([]slack.ColumnAlignment, len(cells))
	for i, cell := range cells {
		cell = strings.TrimSpace(cell)
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			aligns[i] = slack.ColumnAlignmentCenter
		case right:
			aligns[i] = slack.ColumnAlignmentRight
		default:
			aligns[i] = slack.ColumnAlignmentLeft
		}
	}
	return aligns
}

// ---------------------------------------------------------------------------
// ExtractTables
// ---------------------------------------------------------------------------

type tableMatch struct {
	start int
	end   int
}

// ExtractTables splits text into non-table segments and parsed tables.
// Tables inside fenced code blocks are ignored.
func ExtractTables(text string) ([]TextSegment, []ParsedTable) {
	if !strings.Contains(text, "|") {
		return []TextSegment{{Text: text}}, nil
	}

	// Collect code block ranges for exclusion.
	cbRanges := codeBlockTableRe.FindAllStringIndex(text, -1)
	inCodeBlock := func(idx int) bool {
		for _, r := range cbRanges {
			if idx >= r[0] && idx < r[1] {
				return true
			}
		}
		return false
	}

	var matches []tableMatch
	searchFrom := 0

	// Check for table at start of text.
	if text != "" && text[0] == '|' && !inCodeBlock(0) {
		if m, ok := tryParseTableAt(text, 0); ok {
			matches = append(matches, m)
			searchFrom = nextSearchFrom(text, m.end)
		}
	}

	// Scan for \n\n paragraph boundaries.
	for searchFrom < len(text) {
		np := strings.Index(text[searchFrom:], "\n\n")
		if np < 0 {
			break
		}
		pos := searchFrom + np
		afterBlank := pos + 2
		for afterBlank < len(text) && (text[afterBlank] == '\n' || text[afterBlank] == ' ') {
			afterBlank++
		}
		if afterBlank >= len(text) {
			break
		}
		if inCodeBlock(afterBlank) {
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

	if len(matches) == 0 {
		return []TextSegment{{Text: text}}, nil
	}

	// Build segments and parsed tables.
	var segments []TextSegment
	var tables []ParsedTable
	prev := 0

	for _, m := range matches {
		if m.start > prev {
			segments = append(segments, TextSegment{Text: text[prev:m.start]})
		}
		tables = append(tables, parseTable(text[m.start:m.end]))
		prev = m.end
	}
	if prev < len(text) {
		segments = append(segments, TextSegment{Text: text[prev:]})
	}

	return segments, tables
}

func tryParseTableAt(text string, headerStart int) (tableMatch, bool) {
	headerEnd := headerStart
	for headerEnd < len(text) && text[headerEnd] != '\n' {
		headerEnd++
	}
	headerLine := text[headerStart:headerEnd]
	if !isMarkdownTableHeader(headerLine) {
		return tableMatch{}, false
	}

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

func nextSearchFrom(text string, tableEnd int) int {
	if tableEnd > 0 && tableEnd < len(text) && text[tableEnd] == '\n' {
		return tableEnd - 1
	}
	return tableEnd
}

func parseTable(raw string) ParsedTable {
	raw = strings.TrimRight(raw, "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) < 2 {
		return ParsedTable{}
	}

	headers := parseRowCells(lines[0])
	aligns := parseColumnAlign(lines[1])

	var rows [][]string
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, parseRowCells(line))
	}

	return ParsedTable{
		Headers:   headers,
		Rows:      rows,
		ColAligns: aligns,
	}
}

func parseRowCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// ---------------------------------------------------------------------------
// BuildTableBlocks
// ---------------------------------------------------------------------------

const maxMarkdownBlockChars = 11000 // safety margin under Slack's 12K cumulative limit

// BuildTableBlocks constructs Block Kit blocks from extracted tables and text.
//
//   - 1 table: MarkdownBlock(text) + TableBlock → best visual
//   - 2+ tables: MarkdownBlock(full text with tables wrapped in code blocks)
//   - oversized or no tables: returns nil
func BuildTableBlocks(content string, segments []TextSegment, tables []ParsedTable) []slack.Block {
	if len(tables) == 0 {
		return nil
	}
	for _, t := range tables {
		if len(t.Headers) > 20 || len(t.Rows) > 99 {
			return nil
		}
	}
	if len(tables) == 1 {
		return buildSingleTableBlocks(segments, tables[0])
	}
	return buildMultiTableBlocks(content)
}

func buildSingleTableBlocks(segments []TextSegment, table ParsedTable) []slack.Block {
	var blocks []slack.Block

	text := joinSegments(segments)
	if text != "" {
		if len(text) > maxMarkdownBlockChars {
			return nil
		}
		blocks = append(blocks, slack.NewMarkdownBlock("md_text", text))
	}

	blocks = append(blocks, buildOneTableBlock("md_table", table))
	return blocks
}

func buildMultiTableBlocks(content string) []slack.Block {
	wrapped := wrapTablesInCodeBlocks(content)
	if len(wrapped) > maxMarkdownBlockChars {
		return nil
	}
	return []slack.Block{slack.NewMarkdownBlock("md_full", wrapped)}
}

func buildOneTableBlock(blockID string, t ParsedTable) *slack.TableBlock {
	table := slack.NewTableBlock(blockID)
	settings := make([]slack.ColumnSetting, len(t.Headers))
	for j, align := range t.ColAligns {
		settings[j] = slack.ColumnSetting{Align: align, IsWrapped: true}
	}
	table = table.WithColumnSettings(settings...)

	headerRow := make([]*slack.RichTextBlock, len(t.Headers))
	for j, h := range t.Headers {
		headerRow[j] = richTextCell(h)
	}
	table.AddRow(headerRow...)

	for _, row := range t.Rows {
		cells := make([]*slack.RichTextBlock, len(row))
		for j, cell := range row {
			cells[j] = richTextCell(cell)
		}
		table.AddRow(cells...)
	}
	return table
}

func joinSegments(segments []TextSegment) string {
	var sb strings.Builder
	for i, s := range segments {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(s.Text)
	}
	return strings.TrimSpace(sb.String())
}

// ---------------------------------------------------------------------------
// wrapTablesInCodeBlocks
// ---------------------------------------------------------------------------

// wrapTablesInCodeBlocks wraps markdown tables that are not inside fenced code
// blocks in ``` fences for monospace rendering.
func wrapTablesInCodeBlocks(text string) string {
	if !strings.Contains(text, "|") {
		return text
	}

	// Use ExtractTables to find table positions.
	cbRanges := codeBlockTableRe.FindAllStringIndex(text, -1)
	inCodeBlock := func(idx int) bool {
		for _, r := range cbRanges {
			if idx >= r[0] && idx < r[1] {
				return true
			}
		}
		return false
	}

	var matches []tableMatch
	searchFrom := 0

	if text != "" && text[0] == '|' && !inCodeBlock(0) {
		if m, ok := tryParseTableAt(text, 0); ok {
			matches = append(matches, m)
			searchFrom = nextSearchFrom(text, m.end)
		}
	}

	for searchFrom < len(text) {
		np := strings.Index(text[searchFrom:], "\n\n")
		if np < 0 {
			break
		}
		pos := searchFrom + np
		afterBlank := pos + 2
		for afterBlank < len(text) && (text[afterBlank] == '\n' || text[afterBlank] == ' ') {
			afterBlank++
		}
		if afterBlank >= len(text) {
			break
		}
		if inCodeBlock(afterBlank) {
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

	if len(matches) == 0 {
		return text
	}

	// Process back-to-front to keep indices stable.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		content := strings.TrimRight(text[m.start:m.end], "\n")
		fenced := fmt.Sprintf("\n```\n%s\n```\n", content)
		text = text[:m.start] + fenced + text[m.end:]
	}
	return text
}
