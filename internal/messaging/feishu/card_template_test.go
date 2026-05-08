package feishu

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCardHeaderToMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		h    cardHeader
		want map[string]any
	}{
		{
			name: "title only",
			h:    cardHeader{Title: "Bot"},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
			},
		},
		{
			name: "title and subtitle",
			h:    cardHeader{Title: "Bot", Subtitle: "生成中..."},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"subtitle": map[string]any{"tag": "plain_text", "content": "生成中..."},
			},
		},
		{
			name: "title and template",
			h:    cardHeader{Title: "Bot", Template: "blue"},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"template": "blue",
			},
		},
		{
			name: "title with tags",
			h: cardHeader{Title: "Bot", Tags: []cardTag{
				{Text: "pending", Color: "orange"},
			}},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
				"text_tag_list": []map[string]any{
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "pending"}, "color": "orange"},
				},
			},
		},
		{
			name: "all fields",
			h: cardHeader{
				Title: "Bot", Subtitle: "sub", Template: "wathet",
				Tags: []cardTag{{Text: "v1", Color: "blue"}, {Text: "v2"}},
			},
			want: map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Bot"},
				"subtitle": map[string]any{"tag": "plain_text", "content": "sub"},
				"template": "wathet",
			},
		},
		{
			name: "empty title returns nil",
			h:    cardHeader{Template: "blue"},
			want: nil,
		},
		{
			name: "tag with empty text skipped",
			h: cardHeader{Title: "Bot", Tags: []cardTag{
				{Text: "", Color: "red"},
				{Text: "ok", Color: "green"},
			}},
			want: map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": "Bot"},
				"text_tag_list": []map[string]any{
					{"tag": "text_tag", "text": map[string]any{"tag": "plain_text", "content": "ok"}, "color": "green"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.h.toMap()
			if tt.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.want["title"], got["title"])
			if s, ok := tt.want["subtitle"]; ok {
				require.Equal(t, s, got["subtitle"])
			}
			if s, ok := tt.want["template"]; ok {
				require.Equal(t, s, got["template"])
			}
			if s, ok := tt.want["text_tag_list"]; ok {
				require.Equal(t, s, got["text_tag_list"])
			}
		})
	}
}

func TestBuildCard(t *testing.T) {
	t.Parallel()
	t.Run("no header", func(t *testing.T) {
		t.Parallel()
		got := buildCard(cardHeader{}, map[string]any{"wide_screen_mode": true},
			[]map[string]any{{"tag": "markdown", "content": "hello"}})
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Equal(t, "2.0", card["schema"])
		require.Nil(t, card["header"])
	})

	t.Run("with header", func(t *testing.T) {
		t.Parallel()
		got := buildCard(cardHeader{Title: "Bot", Template: "blue"},
			map[string]any{"wide_screen_mode": true},
			[]map[string]any{{"tag": "markdown", "content": "hello"}})
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		hdr, ok := card["header"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "blue", hdr["template"])
	})
}

func TestBuildStreamingCard(t *testing.T) {
	t.Parallel()
	t.Run("no header", func(t *testing.T) {
		t.Parallel()
		got := buildStreamingCard(cardHeader{}, "summary", "content")
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		require.Nil(t, card["header"])
		cfg := card["config"].(map[string]any)
		require.Equal(t, true, cfg["streaming_mode"])
	})

	t.Run("with wathet header", func(t *testing.T) {
		t.Parallel()
		got := buildStreamingCard(
			cardHeader{Title: "Bot", Subtitle: "生成中...", Template: "wathet"},
			"summary", "content")
		var card map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &card))
		hdr, ok := card["header"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "wathet", hdr["template"])
		sub := hdr["subtitle"].(map[string]any)
		require.Equal(t, "生成中...", sub["content"])
	})
}

func TestStringPtr(t *testing.T) {
	t.Parallel()
	p := stringPtr("test")
	require.NotNil(t, p)
	require.Equal(t, "test", *p)
}
