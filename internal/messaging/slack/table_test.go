package slack

import (
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ExtractTables
// ---------------------------------------------------------------------------

func TestExtractTables(t *testing.T) {
	t.Parallel()

	t.Run("no tables", func(t *testing.T) {
		t.Parallel()
		segments, tables := ExtractTables("hello world")
		require.Len(t, tables, 0)
		require.Len(t, segments, 1)
		require.Equal(t, "hello world", segments[0].Text)
	})

	t.Run("single table", func(t *testing.T) {
		t.Parallel()
		input := "before\n\n| H1 | H2 |\n|---|---|\n| A | B |\n\nafter"
		segments, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Len(t, segments, 2)
		require.Equal(t, []string{"H1", "H2"}, tables[0].Headers)
		require.Len(t, tables[0].Rows, 1)
		require.Equal(t, []string{"A", "B"}, tables[0].Rows[0])
	})

	t.Run("multiple tables", func(t *testing.T) {
		t.Parallel()
		input := "| A | B |\n|---|---|\n| 1 | 2 |\n\ntext\n\n| C | D |\n|---|---|\n| 3 | 4 |"
		segments, tables := ExtractTables(input)
		require.Len(t, tables, 2)
		require.Len(t, segments, 1) // only text between tables
		require.Equal(t, []string{"A", "B"}, tables[0].Headers)
		require.Equal(t, []string{"C", "D"}, tables[1].Headers)
	})

	t.Run("table inside code block ignored", func(t *testing.T) {
		t.Parallel()
		input := "```\n| H1 | H2 |\n|---|---|\n| A | B |\n```\n\ntext"
		segments, tables := ExtractTables(input)
		require.Len(t, tables, 0)
		require.Len(t, segments, 1)
	})

	t.Run("incomplete table no separator", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n| A | B |"
		segments, tables := ExtractTables(input)
		require.Len(t, tables, 0)
		require.Len(t, segments, 1)
	})

	t.Run("header and separator only no data rows", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---|---|"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Len(t, tables[0].Rows, 0)
		require.Equal(t, []string{"H1", "H2"}, tables[0].Headers)
	})

	t.Run("header and separator with trailing newline", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---|---|\n"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Len(t, tables[0].Rows, 0)
	})

	t.Run("empty cells", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---|---|\n| A |  |"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Equal(t, "", tables[0].Rows[0][1])
	})

	t.Run("table at start of text", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---|---|\n| A | B |\n\nafter"
		segments, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Len(t, segments, 1) // only "after"
	})
}

// ---------------------------------------------------------------------------
// Column alignment
// ---------------------------------------------------------------------------

func TestColumnAlignment(t *testing.T) {
	t.Parallel()

	t.Run("left align", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|:---|---|\n| A | B |"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Equal(t, slack.ColumnAlignmentLeft, tables[0].ColAligns[0])
	})

	t.Run("right align", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---:|---|\n| A | B |"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Equal(t, slack.ColumnAlignmentRight, tables[0].ColAligns[0])
	})

	t.Run("center align", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|:---:|---|\n| A | B |"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Equal(t, slack.ColumnAlignmentCenter, tables[0].ColAligns[0])
	})

	t.Run("default left", func(t *testing.T) {
		t.Parallel()
		input := "| H1 | H2 |\n|---|---|\n| A | B |"
		_, tables := ExtractTables(input)
		require.Len(t, tables, 1)
		require.Equal(t, slack.ColumnAlignmentLeft, tables[0].ColAligns[0])
	})
}

// ---------------------------------------------------------------------------
// BuildTableBlocks
// ---------------------------------------------------------------------------

func TestBuildTableBlocks(t *testing.T) {
	t.Parallel()

	t.Run("no tables returns nil", func(t *testing.T) {
		t.Parallel()
		blocks := BuildTableBlocks("text", []TextSegment{{Text: "text"}}, nil)
		require.Nil(t, blocks)
	})

	t.Run("single table produces MarkdownBlock + TableBlock", func(t *testing.T) {
		t.Parallel()
		segments := []TextSegment{{Text: "before"}, {Text: "after"}}
		tables := []ParsedTable{{Headers: []string{"H1", "H2"}, Rows: [][]string{{"A", "B"}}, ColAligns: []slack.ColumnAlignment{slack.ColumnAlignmentLeft, slack.ColumnAlignmentLeft}}}
		blocks := BuildTableBlocks("before\n\n| H1 | H2 |\n|---|---|\n| A | B |\n\nafter", segments, tables)
		require.Len(t, blocks, 2)
		require.IsType(t, &slack.MarkdownBlock{}, blocks[0])
		require.IsType(t, &slack.TableBlock{}, blocks[1])
	})

	t.Run("multiple tables produces single MarkdownBlock with code blocks", func(t *testing.T) {
		t.Parallel()
		content := "| A | B |\n|---|---|\n| 1 | 2 |\n\ntext\n\n| C | D |\n|---|---|\n| 3 | 4 |"
		_, tables := ExtractTables(content)
		require.Len(t, tables, 2)
		blocks := BuildTableBlocks(content, nil, tables)
		require.Len(t, blocks, 1)
		require.IsType(t, &slack.MarkdownBlock{}, blocks[0])
		mb := blocks[0].(*slack.MarkdownBlock)
		require.Contains(t, mb.Text, "```")
	})

	t.Run("oversized columns returns nil", func(t *testing.T) {
		t.Parallel()
		headers := make([]string, 21)
		tables := []ParsedTable{{Headers: headers, Rows: nil, ColAligns: nil}}
		blocks := BuildTableBlocks("", nil, tables)
		require.Nil(t, blocks)
	})

	t.Run("oversized rows returns nil", func(t *testing.T) {
		t.Parallel()
		rows := make([][]string, 100)
		tables := []ParsedTable{{Headers: []string{"H"}, Rows: rows, ColAligns: nil}}
		blocks := BuildTableBlocks("", nil, tables)
		require.Nil(t, blocks)
	})
}

// ---------------------------------------------------------------------------
// wrapTablesInCodeBlocks
// ---------------------------------------------------------------------------

func TestWrapTablesInCodeBlocks(t *testing.T) {
	t.Parallel()

	t.Run("no tables unchanged", func(t *testing.T) {
		t.Parallel()
		result := wrapTablesInCodeBlocks("hello world")
		require.Equal(t, "hello world", result)
	})

	t.Run("wraps single table", func(t *testing.T) {
		t.Parallel()
		input := "before\n\n| H1 | H2 |\n|---|---|\n| A | B |\n\nafter"
		result := wrapTablesInCodeBlocks(input)
		require.Contains(t, result, "```\n| H1 | H2 |")
		require.Contains(t, result, "```")
	})

	t.Run("table inside code block untouched", func(t *testing.T) {
		t.Parallel()
		input := "```\n| H1 | H2 |\n|---|---|\n| A | B |\n```"
		result := wrapTablesInCodeBlocks(input)
		require.Equal(t, input, result)
	})

	t.Run("multiple tables all wrapped", func(t *testing.T) {
		t.Parallel()
		input := "| A | B |\n|---|---|\n| 1 | 2 |\n\ntext\n\n| C | D |\n|---|---|\n| 3 | 4 |"
		result := wrapTablesInCodeBlocks(input)
		// Should have 2 code fences wrapping each table
		count := 0
		for i := 0; i < len(result)-2; i++ {
			if result[i:i+3] == "```" {
				count++
			}
		}
		require.Equal(t, 4, count) // 2 opening + 2 closing
	})
}

// ---------------------------------------------------------------------------
// FormatMrkdwn table integration
// ---------------------------------------------------------------------------

func TestFormatMrkdwn_TableWrap(t *testing.T) {
	t.Parallel()

	t.Run("table wrapped in code block", func(t *testing.T) {
		t.Parallel()
		input := "text\n\n| H1 | H2 |\n|---|---|\n| A | B |"
		result := FormatMrkdwn(input)
		require.Contains(t, result, "```")
		require.Contains(t, result, "| H1 | H2 |")
	})

	t.Run("table in existing code block not double-wrapped", func(t *testing.T) {
		t.Parallel()
		input := "```\n| H1 | H2 |\n|---|---|\n| A | B |\n```"
		result := FormatMrkdwn(input)
		require.Contains(t, result, "| H1 | H2 |")
		// Should not have nested code fences
		require.NotContains(t, result, "``````")
	})
}
