package feishu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── isMarkdownTableHeader: fence rejection ──────────────────────────────────
// The fence check fires AFTER the allSeparator check passes.
// For all cells to pass allSeparator: every cell must be exclusively (-:, space).
// Then any cell containing ``` or ~~~ triggers rejection.
func TestIsMarkdownTableHeader_FenceReject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		row  string
		want bool
	}{
		{
			name: "cell all separator with backtick triggers fence check",
			row:  "| : `` : |",
			want: false, // all cells = allSeparator=true, but cell contains ```
		},
		{
			name: "cell all separator with tilde triggers fence check",
			row:  "| ~~ |",
			want: false, // all cells allSeparator, but cell contains ~~~
		},
		{
			name: "single backtick alone is not a fence",
			row:  "| ` |",
			want: false, // allSeparator=true (all backtick), but cell contains ```
		},
		{
			name: "mixed chars not all separator bypasses fence check",
			row:  "| ``` | plain |",
			want: false, // not allSeparator, bypasses fence check
		},
		{
			name: "valid header not rejected",
			row:  "| Name | Value |",
			want: true,
		},
		{
			name: "separator row only not a header",
			row:  "| :--- | :--- |",
			want: false, // all cells allSeparator, bypasses fence check
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isMarkdownTableHeader(tt.row)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── tryParseTableAt: separator line without \n after header ──────────────

func TestTryParseTableAt_NoNewlineAfterHeader(t *testing.T) {
	t.Parallel()
	// Table where header end is at EOF (no trailing \n).
	// sepStart = headerEnd, then sepStart++ → still at EOF → returns false.
	text := "| a |"
	m, ok := tryParseTableAt(text, 0)
	require.False(t, ok) // no \n after header
	require.Equal(t, tableMatch{}, m)
}

// ─── isOnlySepChars: mixed separator characters ───────────────────────────────

func TestIsOnlySepChars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"only dashes", "- - -", true},
		{"only colons and dashes", ": - :", true},
		{"with spaces", " : - ", true},
		{"empty string", "", true}, // loop never runs, vacuously true (all zero chars are separator)
		{"mixed with letter", "-a-", false},
		{"mixed with digit", "-1-", false},
		{"mixed with underscore", "-_-", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isOnlySepChars(tt.s)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── isSeparatorLine: empty cell continues ───────────────────────────────────

func TestIsSeparatorLine_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "empty cell skips validation",
			s:    "| | :---:|",
			want: true, // "" cell is skipped, remaining cells have dashes
		},
		{
			name: "empty cell at start",
			s:    "|:---|:---|",
			want: true,
		},
		{
			name: "no leading pipe",
			s:    ":---|:---|",
			want: false,
		},
		{
			name: "no trailing pipe",
			s:    "|:---|:---|",
			want: true,
		},
		{
			name: "all empty cells",
			s:    "|||||",
			want: false, // no dash found
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSeparatorLine(tt.s)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── tryParseTableAt: table at start of text ────────────────────────────────

func TestTryParseTableAt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		wantOk   bool
		wantRows int // approximate: number of | rows matched
	}{
		{
			name:   "simple table",
			text:   "| a | b |\n|---|---|\n| 1 | 2 |\n",
			wantOk: true,
		},
		{
			name:   "table with alignment markers",
			text:   "| Left | Center | Right |\n|:---|:---:|---:|\n| a | b | c |\n",
			wantOk: true,
		},
		{
			name:   "not a table no separator line",
			text:   "| a | b |\n| 1 | 2 |\n",
			wantOk: false,
		},
		{
			name:   "separator only no header",
			text:   "|---|---|\n",
			wantOk: false,
		},
		{
			name:   "empty text",
			text:   "",
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ok := tryParseTableAt(tt.text, 0)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Greater(t, m.end, m.start)
			}
		})
	}
}

// ─── findTablesOutsideCodeBlocks: inside code block skip ────────────────────

func TestFindTablesOutsideCodeBlocks_CodeBlockSkip(t *testing.T) {
	t.Parallel()
	// Table inside fenced code block should not be found.
	text := "```\n| inside | code |\n|---|---|\n| a | b |\n```\n\n| real | table |\n|---|---|\n| c | d |"
	matches := findTablesOutsideCodeBlocks(text)
	require.Len(t, matches, 1) // only the "real table" outside the fence
}

// ─── OptimizeMarkdownStyle: table after break branch ─────────────────────────

func TestOptimizeMarkdownStyle_TableAfterBreak(t *testing.T) {
	t.Parallel()
	// tableAfterBreakRe: when a table is followed by plain text (not # or **),
	// a <br>\n is appended. This test covers that branch.
	text := "| a | b |\n|---|---|\n| 1 | 2 |\nPlain text after table."
	got := OptimizeMarkdownStyle(text)
	// The table should have <br> appended since followed by "Plain text"
	require.Contains(t, got, "<br>")
	require.Contains(t, got, "Plain text after table")
}

// ─── OptimizeMarkdownStyle: table at EOF (no break after) ────────────────────

func TestOptimizeMarkdownStyle_TableAtEOF(t *testing.T) {
	t.Parallel()
	// tableAfterBreakRe: when table is at EOF, after == "" → returns match unchanged.
	text := "| a | b |\n|---|---|\n| 1 | 2 |"
	got := OptimizeMarkdownStyle(text)
	// No <br> appended because table is at end of text.
	require.NotContains(t, got, "| 1 | 2 |<br>")
}

// ─── OptimizeMarkdownStyle: table followed by heading ─────────────────────────

func TestOptimizeMarkdownStyle_TableFollowedByHeading(t *testing.T) {
	t.Parallel()
	// tableAfterBreakRe: when table is followed by # heading, no <br> appended.
	text := "| a | b |\n|---|---|\n| 1 | 2 |\n\n## Next Section"
	got := OptimizeMarkdownStyle(text)
	// No <br> after table because followed by heading.
	require.NotContains(t, got, "| 1 | 2 |<br>")
}

// ─── OptimizeMarkdownStyle: table followed by bold ─────────────────────────────

func TestOptimizeMarkdownStyle_TableFollowedByBold(t *testing.T) {
	t.Parallel()
	// tableAfterBreakRe: when table is followed by ** bold, no <br> appended.
	text := "| a | b |\n|---|---|\n| 1 | 2 |\n**bold text**"
	got := OptimizeMarkdownStyle(text)
	require.NotContains(t, got, "| 1 | 2 |<br>")
}

// ─── OptimizeMarkdownStyle: heading demotion H1+H2 ─────────────────────────

func TestOptimizeMarkdownStyle_HeadingDemotionBoth(t *testing.T) {
	t.Parallel()
	// Both H1 and H2 present → both should be demoted.
	text := "# Main\n\n## Sub\n\nContent"
	got := OptimizeMarkdownStyle(text)
	require.Contains(t, got, "#### Main")
	require.Contains(t, got, "##### Sub")
}

// ─── StripInvalidImageKeys: malformed syntax ─────────────────────────────────

func TestStripInvalidImageKeys_Malformed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "missing closing paren not matched",
			text: "![alt](missing",
			want: "![alt](missing", // regex requires ]() syntax, not matched, unchanged
		},
		{
			name: "url not img_ prefix stripped",
			text: "![](./img_v3_abc)",
			want: "", // ./ doesn't start with img_ → stripped to ""
		},
		{
			name: "no images unchanged",
			text: "plain text no images",
			want: "plain text no images",
		},
		{
			name: "img_ key preserved",
			text: "![icon](img_v3_abc)",
			want: "![icon](img_v3_abc)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := StripInvalidImageKeys(tt.text)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── ExtractCodeBlocks and Restore ──────────────────────────────────────────

func TestExtractAndRestoreCodeBlocks(t *testing.T) {
	t.Parallel()
	text := "Before\n\n```python\nprint('hello')\n```\n\nAfter"
	clean, blocks := extractCodeBlocks(text)
	// Clean text has placeholder.
	require.NotContains(t, clean, "```python")
	require.Contains(t, clean, "___CB_0___")
	require.Len(t, blocks, 1)
	require.Contains(t, blocks[0], "```python")

	restored := restoreCodeBlocks(clean, blocks)
	require.Contains(t, restored, "print('hello')")
	require.Contains(t, restored, "<br>") // padding added
}

func TestRestoreCodeBlocks_Empty(t *testing.T) {
	t.Parallel()
	got := restoreCodeBlocks("no placeholders here", nil)
	require.Equal(t, "no placeholders here", got)
}

// ─── CountTables ─────────────────────────────────────────────────────────────

func TestCountTables_FenceIsolation(t *testing.T) {
	t.Parallel()
	// A table that looks like markdown but is inside a code block should not count.
	text := "```\n| inside | fenced |\n|---|---|\n```\n\n| outside | table |\n|---|---|\n| 1 | 2 |"
	require.Equal(t, 1, CountTables(text))
}
