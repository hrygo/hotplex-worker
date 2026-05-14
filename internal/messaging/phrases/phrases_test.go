package phrases

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	t.Parallel()
	p := Defaults()

	require.NotEmpty(t, p.Random("greetings"))
	require.NotEmpty(t, p.Random("tips"))
	require.Empty(t, p.Random("nonexistent"))

	cats := p.Categories()
	require.Contains(t, cats, "greetings")
	require.Contains(t, cats, "tips")
	require.Contains(t, cats, "status")
}

func TestRandomReturnsFromPool(t *testing.T) {
	t.Parallel()
	p := &Phrases{entries: map[string][]entry{
		"test": {
			{"a", 1},
			{"b", 1},
			{"c", 1},
		},
	}}

	seen := make(map[string]bool)
	for range 100 {
		v := p.Random("test")
		seen[v] = true
	}
	require.Len(t, seen, 3)
}

func TestRandomWeightedSelection(t *testing.T) {
	t.Parallel()
	p := &Phrases{entries: map[string][]entry{
		"test": {
			{"heavy", 100},
			{"light", 1},
		},
	}}

	heavyCount := 0
	for range 1000 {
		if p.Random("test") == "heavy" {
			heavyCount++
		}
	}
	// With 100:1 weight ratio, heavy should be selected ~99% of the time.
	require.Greater(t, heavyCount, 900)
}

func TestAllReturnsCopy(t *testing.T) {
	t.Parallel()
	p := &Phrases{entries: map[string][]entry{
		"test": {
			{"a", 1},
			{"b", 2},
		},
	}}
	all := p.All("test")
	require.Equal(t, []string{"a", "b"}, all)
	// Mutating returned slice must not affect internals.
	all[0] = "modified"
	require.Equal(t, "a", p.All("test")[0])
	require.Nil(t, p.All("nonexistent"))
}

func TestNilPhrasesReceiver(t *testing.T) {
	t.Parallel()
	var p *Phrases
	require.Equal(t, "", p.Random("greetings"))
	require.Nil(t, p.All("greetings"))
	require.Nil(t, p.Categories())
}

func TestParseMarkdown(t *testing.T) {
	t.Parallel()
	input := `## Greetings
- 你好
- 嗨

## Tips
- 输入 /help 查看帮助
- 输入 /reset 重置

some comment line

## Custom
- custom entry
`
	result := parseMarkdown(input)

	require.Equal(t, []string{"你好", "嗨"}, result["greetings"])
	require.Equal(t, []string{"输入 /help 查看帮助", "输入 /reset 重置"}, result["tips"])
	require.Equal(t, []string{"custom entry"}, result["custom"])
}

func TestParseMarkdownEmpty(t *testing.T) {
	t.Parallel()
	result := parseMarkdown("")
	require.Empty(t, result)
}

func TestParseMarkdownNoDash(t *testing.T) {
	t.Parallel()
	result := parseMarkdown("## Empty\njust a comment\n")
	require.Empty(t, result["empty"])
}

func TestLoadCascadeAppendWithWeights(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Global PHRASES.md
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PHRASES.md"), []byte("## Greetings\n- global\n"), 0o644))

	// Platform-level
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "feishu"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "feishu", "PHRASES.md"), []byte("## Greetings\n- platform\n"), 0o644))

	// Bot-level
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "feishu", "ou_123"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "feishu", "ou_123", "PHRASES.md"), []byte("## Greetings\n- bot\n"), 0o644))

	p, err := Load(dir, "feishu", "ou_123")
	require.NoError(t, err)

	entries := p.entries["greetings"]
	// External config overrides defaults: only 3 external entries, no defaults.
	require.Len(t, entries, 3)

	weightsByText := make(map[string]int)
	for _, e := range entries {
		weightsByText[e.text] = e.weight
	}
	require.Equal(t, WeightGlobal, weightsByText["global"])
	require.Equal(t, WeightPlatform, weightsByText["platform"])
	require.Equal(t, WeightBot, weightsByText["bot"])

	// Weighted random: bot entry should dominate.
	botCount := 0
	for range 1000 {
		if p.Random("greetings") == "bot" {
			botCount++
		}
	}
	require.Greater(t, botCount, 300) // bot (weight 4) out of total weight 9
}

func TestLoadFallbackToDefaultsWhenNoExternal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	p, err := Load(dir, "feishu", "")
	require.NoError(t, err)

	// No external files → defaults used as-is with weight 1.
	all := p.All("greetings")
	require.NotEmpty(t, all)
	for _, e := range p.entries["greetings"] {
		require.Equal(t, WeightDefault, e.weight)
	}
}

func TestLoadFallbackPartial(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Only configure "greetings" externally; "tips" and "status" fall back to defaults.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PHRASES.md"), []byte("## Greetings\n- custom greeting\n"), 0o644))

	p, err := Load(dir, "slack", "")
	require.NoError(t, err)

	// Greetings: external only, no defaults.
	require.Equal(t, []string{"custom greeting"}, p.All("greetings"))
	require.Equal(t, WeightGlobal, p.entries["greetings"][0].weight)

	// Tips: fallback to defaults.
	require.NotEmpty(t, p.All("tips"))
	for _, e := range p.entries["tips"] {
		require.Equal(t, WeightDefault, e.weight)
	}

	// Status: fallback to defaults.
	require.NotEmpty(t, p.All("status"))
}

func TestLoadMissingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	p, err := Load(dir, "slack", "")
	require.NoError(t, err)
	require.NotEmpty(t, p.Random("greetings"))
}

func TestLoadPathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := Load(dir, "feishu", "../etc/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "path traversal")
}

func TestLoadPathTraversalDotDot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := Load(dir, "feishu", "foo/../../etc")
	require.Error(t, err)
}

func TestLoadEmptyBotID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PHRASES.md"), []byte("## Test\n- item\n"), 0o644))

	p, err := Load(dir, "slack", "")
	require.NoError(t, err)
	require.Equal(t, []string{"item"}, p.All("test"))
}

func TestCategoriesSorted(t *testing.T) {
	t.Parallel()
	p := &Phrases{entries: map[string][]entry{
		"zebra": {{"z", 1}},
		"alpha": {{"a", 1}},
		"mid":   {{"m", 1}},
	}}
	require.Equal(t, []string{"alpha", "mid", "zebra"}, p.Categories())
}

func TestWeightConstants(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1, WeightDefault)
	require.Equal(t, 1, WeightPlatform)
	require.Equal(t, 2, WeightGlobal)
	require.Equal(t, 4, WeightBot)
	require.True(t, WeightBot > WeightGlobal)
	require.True(t, WeightGlobal > WeightPlatform)
}
