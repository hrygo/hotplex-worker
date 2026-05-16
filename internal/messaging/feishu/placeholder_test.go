package feishu

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging/phrases"
)

func TestBuildPersonaText(t *testing.T) {
	t.Parallel()

	t.Run("returns two distinct lines from defaults", func(t *testing.T) {
		t.Parallel()
		p := phrases.Defaults()
		text := buildPersonaText(p)
		require.NotEmpty(t, text)
		lines := strings.Split(text, "\n")
		require.Len(t, lines, 2)
		require.NotEqual(t, lines[0], lines[1])
	})

	t.Run("nil phrases returns empty", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, buildPersonaText(nil))
	})

	t.Run("empty category returns empty", func(t *testing.T) {
		t.Parallel()
		p := &phrases.Phrases{}
		require.Empty(t, buildPersonaText(p))
	})
}

func TestBuildClosingText(t *testing.T) {
	t.Parallel()

	t.Run("returns non-empty from defaults", func(t *testing.T) {
		t.Parallel()
		p := phrases.Defaults()
		require.NotEmpty(t, buildClosingText(p))
	})

	t.Run("nil phrases returns empty", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, buildClosingText(nil))
	})

	t.Run("returns varied values from pool", func(t *testing.T) {
		t.Parallel()
		p := phrases.Defaults()
		seen := make(map[string]bool)
		for range 100 {
			seen[buildClosingText(p)] = true
		}
		require.GreaterOrEqual(t, len(seen), 2)
	})
}

func TestBuildPlaceholderText(t *testing.T) {
	t.Parallel()

	t.Run("returns two lines from defaults", func(t *testing.T) {
		t.Parallel()
		p := phrases.Defaults()
		text := buildPlaceholderText(p)
		require.Contains(t, text, "\n")
		require.Contains(t, text, ":Get:")
		require.Contains(t, text, ":StatusFlashOfInspiration:")
	})

	t.Run("nil phrases returns stickers only", func(t *testing.T) {
		t.Parallel()
		text := buildPlaceholderText(nil)
		require.Contains(t, text, ":Get: \n:StatusFlashOfInspiration: ")
	})
}
