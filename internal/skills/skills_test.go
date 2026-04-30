package skills

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── Locator ─────────────────────────────────────────────────────────────────

func TestLocator_List_CacheAndScan(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	// Create skill file before creating locator (so first scan finds it)
	skillDir := filepath.Join(workDir, ".claude", "skills")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "test-skill.md"), []byte("---\nname: test-skill\ndescription: A test skill\n---\n# Content\n"), 0o644))

	l := NewLocator(slog.Default(), time.Minute)
	defer l.Close()

	skills, err := l.List(context.Background(), homeDir, workDir)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "test-skill", skills[0].Name)
	require.Equal(t, SourceProject, skills[0].Source)
}

func TestLocator_List_CacheHit(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	l := NewLocator(slog.Default(), time.Minute)
	defer l.Close()

	// First call populates cache
	_, err := l.List(context.Background(), homeDir, workDir)
	require.NoError(t, err)

	// Delete workDir to prove second call uses cache
	require.NoError(t, os.RemoveAll(workDir))

	skills, err := l.List(context.Background(), homeDir, workDir)
	require.NoError(t, err)
	require.Empty(t, skills) // cached empty result
}

func TestLocator_Close(t *testing.T) {
	t.Parallel()

	l := NewLocator(slog.Default(), time.Minute)
	require.NotPanics(t, func() { l.Close() })
}

func TestLocator_EvictOldest(t *testing.T) {
	t.Parallel()

	l := NewLocator(slog.Default(), time.Minute)
	defer l.Close()

	l.cache["old"] = &cacheEntry{skills: nil, expiresAt: time.Now().Add(-time.Hour)}
	l.cache["new"] = &cacheEntry{skills: nil, expiresAt: time.Now().Add(time.Hour)}

	l.mu.Lock()
	l.evictOldest()
	l.mu.Unlock()

	_, hasOld := l.cache["old"]
	_, hasNew := l.cache["new"]
	require.False(t, hasOld)
	require.True(t, hasNew)
}

// ─── scanDirs / scanDir ──────────────────────────────────────────────────────

func TestScanDirs_Empty(t *testing.T) {
	t.Parallel()

	skills := scanDirs(t.TempDir(), t.TempDir())
	require.Empty(t, skills)
}

func TestScanDir_WithMarkdownFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.md"), []byte("---\nname: hello\ndescription: Says hello\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("not a skill"), 0o644))

	skills, err := scanDir(dir, SourceGlobal)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "hello", skills[0].Name)
	require.Equal(t, SourceGlobal, skills[0].Source)
}

func TestScanDir_Subdirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sub := filepath.Join(dir, "my-tool")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte("---\nname: my-tool\ndescription: A tool\n---\n"), 0o644))

	skills, err := scanDir(dir, SourceProject)
	require.NoError(t, err)
	require.Len(t, skills, 1)
	require.Equal(t, "my-tool", skills[0].Name)
}

func TestScanDir_NonDir(t *testing.T) {
	t.Parallel()

	f := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	_, err := scanDir(f, SourceGlobal)
	require.Error(t, err)
}

// ─── parseSkillFile / extractFrontmatter ─────────────────────────────────────

func TestParseSkillFile_Valid(t *testing.T) {
	t.Parallel()

	f := filepath.Join(t.TempDir(), "skill.md")
	require.NoError(t, os.WriteFile(f, []byte("---\nname: my-skill\ndescription: Does things\n---\n# My Skill\n"), 0o644))

	s := parseSkillFile(f, SourceGlobal)
	require.NotNil(t, s)
	require.Equal(t, "my-skill", s.Name)
	require.Equal(t, "Does things", s.Description)
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	t.Parallel()

	f := filepath.Join(t.TempDir(), "skill.md")
	require.NoError(t, os.WriteFile(f, []byte("# No frontmatter\n"), 0o644))

	s := parseSkillFile(f, SourceGlobal)
	require.Nil(t, s)
}

func TestParseSkillFile_NoName(t *testing.T) {
	t.Parallel()

	f := filepath.Join(t.TempDir(), "skill.md")
	require.NoError(t, os.WriteFile(f, []byte("---\ndescription: no name field\n---\n"), 0o644))

	s := parseSkillFile(f, SourceGlobal)
	require.Nil(t, s)
}

func TestParseSkillFile_Nonexistent(t *testing.T) {
	t.Parallel()

	s := parseSkillFile("/nonexistent/file.md", SourceGlobal)
	require.Nil(t, s)
}

func TestParseFrontmatter(t *testing.T) {
	t.Parallel()

	f := filepath.Join(t.TempDir(), "skill.md")
	require.NoError(t, os.WriteFile(f, []byte("---\nname: test\ndescription: A skill with a desc\n---\n"), 0o644))

	name, desc, ok := ParseFrontmatter(f)
	require.True(t, ok)
	require.Equal(t, "test", name)
	require.Equal(t, "A skill with a desc", desc)
}

func TestParseFrontmatter_Nonexistent(t *testing.T) {
	t.Parallel()

	_, _, ok := ParseFrontmatter("/nonexistent.md")
	require.False(t, ok)
}

// ─── dedup ───────────────────────────────────────────────────────────────────

func TestDedup_NoDuplicates(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Name: "a", Source: SourceGlobal},
		{Name: "b", Source: SourceProject},
	}
	result := dedup(skills)
	require.Len(t, result, 2)
}

func TestDedup_ProjectOverridesGlobal(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Name: "a", Description: "global", Source: SourceGlobal},
		{Name: "a", Description: "project", Source: SourceProject},
	}
	result := dedup(skills)
	require.Len(t, result, 1)
	require.Equal(t, "project", result[0].Description)
	require.Equal(t, SourceProject, result[0].Source)
}

func TestDedup_GlobalDoesNotOverrideProject(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Name: "a", Description: "project", Source: SourceProject},
		{Name: "a", Description: "global", Source: SourceGlobal},
	}
	result := dedup(skills)
	require.Len(t, result, 1)
	require.Equal(t, "project", result[0].Description)
}

// ─── CollapseSpaces ──────────────────────────────────────────────────────────

func TestCollapseSpaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"hello  world", "hello world"},
		{"  a   b   c  ", " a b c "},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, CollapseSpaces(tt.input))
		})
	}
}

// ─── isSymlink ───────────────────────────────────────────────────────────────

func TestIsSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))

	require.True(t, isSymlink(link))
	require.False(t, isSymlink(target))
	require.False(t, isSymlink("/nonexistent/path"))
}

func TestNewLocator_ZeroTTL_UsesDefault(t *testing.T) {
	t.Parallel()

	l := NewLocator(slog.Default(), 0)
	defer l.Close()

	require.Equal(t, defaultTTL, l.ttl)
}

func TestNewLocator_NegativeTTL_UsesDefault(t *testing.T) {
	t.Parallel()

	l := NewLocator(slog.Default(), -5*time.Second)
	defer l.Close()

	require.Equal(t, defaultTTL, l.ttl)
}
