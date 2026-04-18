package feishu

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountTables(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want int
	}{
		{
			name: "no tables",
			text: "Hello world\n\nNo tables here.",
			want: 0,
		},
		{
			name: "one table",
			text: "| a | b |\n|---|---|\n| 1 | 2 |",
			want: 1,
		},
		{
			name: "two separate tables",
			text: "| a | b |\n|---|---|\n| 1 | 2 |\n\nSome text\n\n| c | d |\n|---|---|\n| 3 | 4 |",
			want: 2,
		},
		{
			name: "three tables at limit",
			text: "| Name | Value |\n|------|-------|\n| foo  | 1     |\n\n| ColA | ColB |\n|------|------|\n| bar  | 2     |\n\n| Header1 | Header2 |\n|---------|---------|\n| baz     | 3       |",
			want: 3,
		},
		{
			name: "four tables over limit",
			text: "| Name | Value |\n|------|-------|\n| foo  | 1     |\n\n| ColA | ColB |\n|------|------|\n| bar  | 2     |\n\n| Header1 | Header2 |\n|---------|---------|\n| baz     | 3       |\n\n| X | Y |\n|---|---|\n| p | q |",
			want: 4,
		},
		{
			name: "tables inside fenced code blocks not counted",
			text: "```\n| inside | code |\n|---|---|\n| a | b |\n```\n\n| real | table |\n|---|---|\n| c | d |",
			want: 1,
		},
		{
			name: "tables inside triple-backtick with lang",
			text: "```python\n| inside | python |\n|--------|--------|\n| x | y |\n```\n\n| outside | table |\n|---------|--------|\n| 1 | 2 |",
			want: 1,
		},
		{
			name: "tables inside tilde fences not counted",
			text: "~~~\n| inside | tilde |\n|---|---|\n| a | b |\n~~~\n\n| real | table |\n|---|---|\n| c | d |",
			want: 1,
		},
		{
			name: "table with alignment markers",
			text: "| left | center | right |\n|:------|:------:|------:|\n| 1 | 2 | 3 |",
			want: 1,
		},
		{
			name: "table without separator dashes not matched",
			text: "| a | b |\n| 1 | 2 |\n| c | d |",
			want: 0,
		},
		{
			name: "pipeline character in regular text not matched",
			text: "Use | as OR in regex patterns:\n| pattern | matches |",
			want: 0,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CountTables(tt.text)
			require.Equal(t, tt.want, got, "CountTables(%q)", tt.text)
		})
	}
}

func TestSanitizeForCard(t *testing.T) {
	t.Parallel()

	table1 := "| Name | Value |\n|------|-------|\n| foo  | 1     |"
	table2 := "| ColA | ColB |\n|------|------|\n| bar  | 2     |"
	table3 := "| Header1 | Header2 |\n|---------|---------|\n| baz     | 3       |"
	table4 := "| Key | Data |\n|-----|------|\n| p   | q    |"
	tableInCode := "```\n| inside | code |\n|---|---|\n| x | y |\n```"

	tests := []struct {
		name          string
		text          string
		wantFenced    int  // number of ``` fences added
		wantUnchanged bool // true if text should be unchanged
	}{
		{
			name:          "zero tables unchanged",
			text:          "Hello world",
			wantUnchanged: true,
		},
		{
			name:          "one table unchanged",
			text:          table1,
			wantUnchanged: true,
		},
		{
			name:          "three tables at limit unchanged",
			text:          table1 + "\n\n" + table2 + "\n\n" + table3,
			wantUnchanged: true,
		},
		{
			name:       "fourth table wrapped",
			text:       table1 + "\n\n" + table2 + "\n\n" + table3 + "\n\n" + table4,
			wantFenced: 1,
		},
		{
			name:       "six tables: three wrapped",
			text:       table1 + "\n\n" + table2 + "\n\n" + table3 + "\n\n" + table4 + "\n\n" + table1 + "\n\n" + table2,
			wantFenced: 3,
		},
		{
			name:          "table inside code fence not counted or wrapped",
			text:          tableInCode + "\n\n" + table1 + "\n\n" + table2 + "\n\n" + table3,
			wantUnchanged: true,
			wantFenced:    1, // original code block contributes 1 fence pair
		},
		{
			name:       "table inside code fence not counted but real tables over limit",
			text:       tableInCode + "\n\n" + table1 + "\n\n" + table2 + "\n\n" + table3 + "\n\n" + table4,
			wantFenced: 2, // 1 original code block + 1 wrapped table
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeForCard(tt.text)
			if tt.wantUnchanged {
				require.Equal(t, tt.text, got, "SanitizeForCard should return unchanged text")
			}
			fenced := strings.Count(got, "```")
			require.Equal(t, tt.wantFenced, fenced/2, "fenced code blocks (pairs) in result")
			// Verify idempotency: sanitizing twice should be same as once.
			again := SanitizeForCard(got)
			require.Equal(t, got, again, "SanitizeForCard should be idempotent")
		})
	}
}

func TestShouldUseCard(t *testing.T) {
	t.Parallel()
	table := "| Name | Value |\n|------|-------|\n| foo  | 1     |"
	code := "```go\nfmt.Println()\n```"

	tests := []struct {
		name string
		text string
		want bool
	}{
		{"plain text", "Hello world", false},
		{"plain text with pipes not tables", "Use | as OR in regex patterns", false},
		{"one table", table, true},
		{"two tables", table + "\n\n" + table, true},
		{"three tables at limit", table + "\n\n" + table + "\n\n" + table, true},
		{"four tables over limit", table + "\n\n" + table + "\n\n" + table + "\n\n" + table, false},
		{"code block", code, true},
		{"code block plus tables at limit", code + "\n\n" + table + "\n\n" + table, true},
		{"code block plus tables over limit", code + "\n\n" + table + "\n\n" + table + "\n\n" + table + "\n\n" + table, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ShouldUseCard(tt.text)
			require.Equal(t, tt.want, got, "ShouldUseCard(%q)", tt.text)
		})
	}
}

func TestOptimizeMarkdownStyle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		contains   []string
		notContain []string
	}{
		{
			name:     "empty",
			text:     "",
			contains: []string{""},
		},
		{
			name:     "plain text unchanged",
			text:     "Hello world",
			contains: []string{"Hello world"},
		},
		{
			name:     "H1 demoted to H4",
			text:     "# Title\n\nSome text",
			contains: []string{"#### Title"},
		},
		{
			name:     "H2 demoted to H5",
			text:     "## Subtitle\n\nSome text",
			contains: []string{"##### Subtitle"},
		},
		{
			name:     "H1+H2 both demoted",
			text:     "# Main\n\n## Sub\n\nText",
			contains: []string{"#### Main", "##### Sub"},
		},
		{
			name:     "H4+H5 not demoted (only H1-H3 trigger demotion)",
			text:     "#### Small\n\n##### Smaller\n\nText",
			contains: []string{"#### Small", "##### Smaller"},
		},
		{
			name:     "code block not modified",
			text:     "```go\nfmt.Println(\"hello\")\n```",
			contains: []string{"```go", "fmt.Println(\"hello\")", "```"},
		},
		{
			name:     "code block gets br padding",
			text:     "Before\n\n```\ncode\n```\n\nAfter",
			contains: []string{"<br>\n```", "```\n<br>"},
		},
		{
			name:     "consecutive H4 headings get br",
			text:     "#### Title1\n\n#### Title2",
			contains: []string{"<br>\n#### Title2"},
		},
		{
			name:       "extra newlines collapsed",
			text:       "# Title\n\n\n\n\nLine2",
			contains:   []string{"# Title\n\nLine2"},
			notContain: []string{"\n\n\n"},
		},
		{
			name:     "table gets br before and after",
			text:     "Text\n\n| Name | Value |\n|------|-------|\n| foo  | 1     |\n\nMore text",
			contains: []string{"<br>\n\n| Name | Value |", "| foo  | 1     |\n<br>"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OptimizeMarkdownStyle(tt.text)
			for _, want := range tt.contains {
				require.Contains(t, got, want, "OptimizeMarkdownStyle output should contain %q", want)
			}
			for _, notWant := range tt.notContain {
				require.NotContains(t, got, notWant, "OptimizeMarkdownStyle output should NOT contain %q", notWant)
			}
		})
	}
}

func TestStripInvalidImageKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "img_ key preserved",
			text: "See ![icon](img_abc123) for details",
			want: "See ![icon](img_abc123) for details",
		},
		{
			name: "http URL stripped",
			text: "![logo](https://example.com/logo.png)",
			want: "",
		},
		{
			name: "non-img_ key stripped",
			text: "![icon](icon_abc123) and ![icon](img_abc123)",
			want: " and ![icon](img_abc123)",
		},
		{
			name: "no images unchanged",
			text: "Plain text with no images",
			want: "Plain text with no images",
		},
		{
			name: "empty alt preserved for valid key",
			text: "![](img_empty_alt)",
			want: "![](img_empty_alt)",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripInvalidImageKeys(tt.text)
			require.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkSanitizeForCard_ThreeTables(b *testing.B) {
	text := strings.Repeat("| a | b |\n|---|---|\n| 1 | 2 |\n\n", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeForCard(text)
	}
}

func BenchmarkSanitizeForCard_OverLimit(b *testing.B) {
	// 4 tables repeated to exceed limit
	text := strings.Repeat("| a | b |\n|---|---|\n| 1 | 2 |\n\n| c | d |\n|---|---|\n| 3 | 4 |\n\n| e | f |\n|---|---|\n| 5 | 6 |\n\n| g | h |\n|---|---|\n| 7 | 8 |\n\n", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SanitizeForCard(text)
	}
}

func BenchmarkOptimizeMarkdownStyle_Complex(b *testing.B) {
	text := "# Title\n\n```go\ncode\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n## Sub\n\nMore text\n\n#### Small\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OptimizeMarkdownStyle(text)
	}
}

// TestPipelineIntegration tests the full pipeline: OptimizeMarkdownStyle(SanitizeForCard(text)).
// This is the actual call order used in adapter.go and streaming.go.
func TestPipelineIntegration(t *testing.T) {
	t.Parallel()

	table := "| a | b |\n|---|---|\n| 1 | 2 |"

	tests := []struct {
		name       string
		text       string
		contains   []string
		notContain []string
	}{
		{
			name: "four tables: excess wrapped without breaking subsequent markdown",
			text: table + "\n\n" + table + "\n\n" + table + "\n\n" + table + "\n\n# Heading\n\nParagraph text",
			contains: []string{
				"#### Heading",   // H1 demoted to H4
				"Paragraph text", // text after table preserved
				"```\n",          // code block wrapping 4th table
			},
			notContain: []string{
				"```\n```", // no empty code blocks (double fence on same line)
			},
		},
		{
			name: "five tables with content between: all subsequent text intact",
			text: table + "\n\n" + table + "\n\n" + table + "\n\n" + table + "\n\nMid text\n\n" + table + "\n\n## End\n\nFinal",
			contains: []string{
				"Mid text",
				"##### End", // H2 demoted to H5
				"Final",
			},
		},
		{
			name:     "three tables at limit: no wrapping needed",
			text:     table + "\n\n" + table + "\n\n" + table + "\n\n# After\n\nDone",
			contains: []string{"#### After", "Done"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OptimizeMarkdownStyle(SanitizeForCard(tt.text))
			for _, want := range tt.contains {
				require.Contains(t, got, want, "output should contain %q", want)
			}
			for _, notWant := range tt.notContain {
				require.NotContains(t, got, notWant, "output should NOT contain %q", notWant)
			}
			// Verify no unclosed code blocks: count ``` pairs must be even.
			fenceCount := strings.Count(got, "```")
			require.Equal(t, 0, fenceCount%2, "code block fences must be paired (found %d)", fenceCount)
		})
	}
}
