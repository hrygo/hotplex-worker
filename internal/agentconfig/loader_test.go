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
		cfg, err := Load(dir, "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("nonexistent dir returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("/nonexistent/path", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("empty dir string returns empty configs", func(t *testing.T) {
		t.Parallel()
		cfg, err := Load("", "")
		require.NoError(t, err)
		require.True(t, cfg.IsEmpty())
	})

	t.Run("loads base files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "---\nversion: 1\n---\nI am an AI assistant.")
		writeFile(t, dir, "AGENTS.md", "Workspace rules here.")
		writeFile(t, dir, "USER.md", "User profile data.")

		cfg, err := Load(dir, "")
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

		cfg, err := Load(dir, "")
		require.NoError(t, err)
		require.Equal(t, "Actual content.", cfg.Soul)
	})

	t.Run("appends platform variant", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Base soul.")
		writeFile(t, dir, "SOUL.slack.md", "Slack specifics.")

		cfg, err := Load(dir, "slack")
		require.NoError(t, err)
		require.Contains(t, cfg.Soul, "Base soul.")
		require.Contains(t, cfg.Soul, "Slack specifics.")
	})

	t.Run("platform variant only without base", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.slack.md", "Slack only.")

		cfg, err := Load(dir, "slack")
		require.NoError(t, err)
		require.Equal(t, "Slack only.", cfg.Soul)
	})

	t.Run("no platform variant when empty", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "SOUL.md", "Base soul.")
		writeFile(t, dir, "SOUL.slack.md", "Slack specifics.")

		cfg, err := Load(dir, "")
		require.NoError(t, err)
		require.Equal(t, "Base soul.", cfg.Soul)
	})

	t.Run("missing files are skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, dir, "AGENTS.md", "Rules.")

		cfg, err := Load(dir, "")
		require.NoError(t, err)
		require.Empty(t, cfg.Soul)
		require.Equal(t, "Rules.", cfg.Agents)
		require.Empty(t, cfg.User)
	})
}

func TestSizeLimits(t *testing.T) {
	t.Parallel()

	t.Run("per file limit truncates", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		longContent := strings.Repeat("x", MaxFileChars+1000)
		writeFile(t, dir, "SOUL.md", longContent)

		cfg, err := Load(dir, "")
		require.NoError(t, err)
		require.Equal(t, MaxFileChars, len(cfg.Soul))
	})

	t.Run("total limit enforced", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Write files that individually are under limit but combined exceed total.
		content := strings.Repeat("a", MaxTotalChars/2+1)
		writeFile(t, dir, "SOUL.md", content)
		writeFile(t, dir, "AGENTS.md", content)

		cfg, err := Load(dir, "")
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

	t.Run("B-channel only omits context group", func(t *testing.T) {
		cfg := &AgentConfigs{Agents: "Rules only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<directives>")
		require.Contains(t, prompt, "<rules>")
		require.NotContains(t, prompt, "<persona>")
		require.NotContains(t, prompt, "<context>")
	})

	t.Run("C-channel only omits directives group", func(t *testing.T) {
		cfg := &AgentConfigs{User: "User only", Memory: "Memory only"}
		prompt := BuildSystemPrompt(cfg)
		require.Contains(t, prompt, "<context>")
		require.Contains(t, prompt, "<user>")
		require.Contains(t, prompt, "<memory>")
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
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	require.NoError(t, err)
}
