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
	p := &Phrases{entries: map[string][]string{
		"test": {"a", "b", "c"},
	}}

	seen := make(map[string]bool)
	for range 100 {
		v := p.Random("test")
		seen[v] = true
	}
	require.Len(t, seen, 3)
}

func TestAllReturnsCopy(t *testing.T) {
	t.Parallel()
	p := &Phrases{entries: map[string][]string{
		"test": {"a", "b"},
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

func TestMergeAppends(t *testing.T) {
	t.Parallel()
	dst := map[string][]string{
		"a": {"1"},
		"b": {"2"},
	}
	src := map[string][]string{
		"a": {"3"},
		"c": {"4"},
	}
	merge(dst, src)

	require.Equal(t, []string{"1", "3"}, dst["a"])
	require.Equal(t, []string{"2"}, dst["b"])
	require.Equal(t, []string{"4"}, dst["c"])
}

func TestLoadCascadeAppend(t *testing.T) {
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

	// Should contain defaults + global + platform + bot greetings
	all := p.All("greetings")
	require.Contains(t, all, "global")
	require.Contains(t, all, "platform")
	require.Contains(t, all, "bot")
	// Also has defaults
	require.True(t, len(all) > 3)
}

func TestLoadMissingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	p, err := Load(dir, "slack", "")
	require.NoError(t, err)
	// Falls back to defaults
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
	p := &Phrases{entries: map[string][]string{
		"zebra": {"z"},
		"alpha": {"a"},
		"mid":   {"m"},
	}}
	require.Equal(t, []string{"alpha", "mid", "zebra"}, p.Categories())
}
