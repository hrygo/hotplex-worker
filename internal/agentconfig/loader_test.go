package agentconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("empty dir returns empty configs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("nonexistent dir returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("/nonexistent/path", "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("empty dir string returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("", "", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("loads base files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "---\nversion: 1\n---\nI am an AI assistant.")
		writeFile(t, dir, "AGENTS.md", "Workspace rules here.")
		writeFile(t, dir, "USER.md", "User profile data.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.False(t, cfg.IsEmpty())
		require.Equal(t, "I am an AI assistant.", cfg.Soul)
		require.Equal(t, "Workspace rules here.", cfg.Agents)
		require.Equal(t, "User profile data.", cfg.User)
		require.Empty(t, cfg.Skills)
		require.Empty(t, cfg.Memory)
	})

	t.Run("strips yaml frontmatter", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "---\nversion: 1\ndescription: test\n---\nActual content.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Actual content.", cfg.Soul)
	})

	t.Run("platform directory overrides global", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "Slack soul.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Slack soul.", cfg.Soul)
	})

	t.Run("bot-level overrides platform-level", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "Platform soul.")
		writeFile(t, dir, "slack/U12345/SOUL.md", "Bot soul.")

		cfg, err := Load(dir, "slack", "U12345")
		require.NoError(t, err)
		require.Equal(t, "Bot soul.", cfg.Soul)
	})

	t.Run("falls back to global when platform dir missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Global soul.", cfg.Soul)
	})

	t.Run("each file resolves independently", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "AGENTS.md", "Global agents.")
		writeFile(t, dir, "slack/SOUL.md", "Slack soul.")
		// AGENTS.md not in slack/ — should use global.

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Slack soul.", cfg.Soul)
		require.Equal(t, "Global agents.", cfg.Agents)
	})

	t.Run("suffix files are not loaded", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Base soul.")
		writeFile(t, dir, "SOUL.slack.md", "Old-style variant.")

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Base soul.", cfg.Soul)
		// SOUL.slack.md is NOT loaded — old suffix mechanism removed.
	})

	t.Run("path traversal botID rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := Load(dir, "slack", "../etc")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid botID")
	})

	t.Run("path traversal with dots rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := Load(dir, "slack", "foo/bar")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid botID")
	})

	t.Run("empty file falls through to next level", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Global soul.")
		writeFile(t, dir, "slack/SOUL.md", "---\n---\n") // frontmatter only = empty content

		cfg, err := Load(dir, "slack", "")
		require.NoError(t, err)
		require.Equal(t, "Global soul.", cfg.Soul)
	})

	t.Run("flat directory backward compatible", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Soul content.")
		writeFile(t, dir, "AGENTS.md", "Agents content.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, "Soul content.", cfg.Soul)
		require.Equal(t, "Agents content.", cfg.Agents)
	})

	t.Run("missing files are skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "AGENTS.md", "Rules.")

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul)
		require.Equal(t, "Rules.", cfg.Agents)
		require.Empty(t, cfg.User)
	})

	t.Run("permission denied returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		filePath := filepath.Join(dir, "SOUL.md")
		err := os.WriteFile(filePath, []byte("Content"), 0o000)
		require.NoError(t, err)

		_, err = Load(dir, "", "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "agentconfig: read")
	})
}

func TestSizeLimits(t *testing.T) {
	t.Parallel()

	t.Run("per file limit truncates", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		longContent := strings.Repeat("x", MaxFileChars+1000)
		writeFile(t, dir, "SOUL.md", longContent)

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		require.Equal(t, MaxFileChars, len(cfg.Soul))
	})

	t.Run("total limit enforced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := strings.Repeat("a", MaxTotalChars/2+1)
		writeFile(t, dir, "SOUL.md", content)
		writeFile(t, dir, "AGENTS.md", content)

		cfg, err := Load(dir, "", "")
		require.NoError(t, err)
		total := len(cfg.Soul) + len(cfg.Agents) + len(cfg.Skills) + len(cfg.User) + len(cfg.Memory)
		require.LessOrEqual(t, total, MaxTotalChars)
	})
}

func TestStripFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no frontmatter", "Hello", "Hello"},
		{"yaml frontmatter", "---\nversion: 1\n---\nContent", "Content"},
		{"empty frontmatter", "---\n---\nContent", "Content"},
		{"malformed no close", "---\nversion: 1\nContent", "---\nversion: 1\nContent"},
		{"multiline content", "---\nv: 1\n---\nLine1\nLine2", "Line1\nLine2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripFrontmatter(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	t.Parallel()

	t.Run("nil configs returns empty", func(t *testing.T) {
		require.Empty(t, BuildSystemPrompt(nil))
	})

	t.Run("empty configs returns empty", func(t *testing.T) {
		require.Empty(t, BuildSystemPrompt(&AgentConfigs{}))
	})

	t.Run("assembles B+C with nested XML tags", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "Persona", Agents: "Rules", Skills: "Tools", User: "User data", Memory: "Memory data"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<agent-configuration>")
		require.Contains(t, prompt, "</agent-configuration>")
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "</directives>")
		require.Contains(t, prompt, "<persona>")
		require.Contains(t, prompt, "Persona")
		require.Contains(t, prompt, "<rules>")
		require.Contains(t, prompt, "Rules")
		require.Contains(t, prompt, "<skills>")
		require.Contains(t, prompt, "Tools")
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "</context>")
		require.Contains(t, prompt, "<user>")
		require.Contains(t, prompt, "User data")
		require.Contains(t, prompt, "<memory>")
		require.Contains(t, prompt, "Memory data")
	})

	t.Run("B-channel only still includes hotplex metacognition", func(t *testing.T) {
		cfg := &AgentConfigs{Agents: "Rules only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "<rules>")
		require.NotContains(t, prompt, "<persona>")
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "<hotplex>")
		require.NotContains(t, prompt, "<user>")
		require.NotContains(t, prompt, "<memory>")
	})

	t.Run("C-channel includes hotplex metacognition + user content", func(t *testing.T) {
		cfg := &AgentConfigs{User: "User only", Memory: "Memory only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "<hotplex>")
		require.Contains(t, prompt, "<user>")
		require.Contains(t, prompt, "User only")
		require.Contains(t, prompt, "<memory>")
		require.Contains(t, prompt, "Memory only")
		require.NotContains(t, prompt, "<directives>")
		require.NotContains(t, prompt, "<persona>")
	})

	t.Run("directives before context", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "S", User: "U"}
		prompt := BuildSystemPrompt(cfg)
		bIdx := strings.Index(prompt, "<directives>")
		cIdx := strings.Index(prompt, "<context>")
		require.Less(t, bIdx, cIdx)
	})

	t.Run("behavioral directives present per section", func(t *testing.T) {
		cfg := &AgentConfigs{Soul: "S", Agents: "A", Skills: "K", User: "U", Memory: "M"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "Embody this persona naturally")
		require.Contains(t, prompt, "mandatory workspace constraints")
		require.Contains(t, prompt, "Apply these capabilities when relevant")
		require.Contains(t, prompt, "Tailor responses to this user")
		require.Contains(t, prompt, "Recall relevant past context")
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	err := os.WriteFile(fullPath, []byte(content), 0o644)
	require.NoError(t, err)
}
