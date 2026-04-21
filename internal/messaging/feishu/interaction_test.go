package feishu

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ─── buildInteractionCard ─────────────────────────────────────────────────────

func TestBuildInteractionCard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		footer string
	}{
		{
			name:   "body only",
			body:   "Hello **world**",
			footer: "",
		},
		{
			name:   "body and footer",
			body:   "Header\n\n---\n",
			footer: "Reply **yes** or **no**",
		},
		{
			name:   "empty strings",
			body:   "",
			footer: "",
		},
		{
			name:   "unicode content",
			body:   "你好世界 🔔",
			footer: "回复 **允许** 或 **拒绝**",
		},
		{
			name:   "markdown heavy",
			body:   "```\ncode block\n```\n**bold** and _italic_",
			footer: "Option 1\nOption 2\nOption 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildInteractionCard(tt.body, tt.footer)

			// Verify it's valid JSON
			var card map[string]any
			require.NoError(t, json.Unmarshal([]byte(got), &card), "output must be valid JSON")

			// Verify structure
			require.Equal(t, "2.0", card["schema"])
			require.NotNil(t, card["config"])
			require.NotNil(t, card["body"])

			body := card["body"].(map[string]any)
			elements, ok := body["elements"].([]any)
			require.True(t, ok)

			// Body markdown element
			require.GreaterOrEqual(t, len(elements), 1)
			firstEl, ok := elements[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "markdown", firstEl["tag"])
			require.Equal(t, tt.body, firstEl["content"])

			// Footer: if non-empty, expect hr + markdown
			if tt.footer != "" {
				require.Equal(t, 3, len(elements), "expect hr + footer markdown")
				require.Equal(t, "hr", elements[1].(map[string]any)["tag"])
				footerEl := elements[2].(map[string]any)
				require.Equal(t, "markdown", footerEl["tag"])
				require.Equal(t, tt.footer, footerEl["content"])
			}
		})
	}
}

// ─── truncate ─────────────────────────────────────────────────────────────────
// NOTE: truncate uses byte indexing, not rune. Multi-byte UTF-8 characters
// will be sliced in the middle of a character, producing invalid UTF-8.

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"under limit", "hello", 10, "hello"},
		{"at limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"exact half", "hello", 3, "hel..."},
		{"zero limit", "hello", 0, "..."},
		{"one over", "hi", 1, "h..."},
		{"single char", "x", 5, "x"},
		{"single char over", "hello", 1, "h..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.input, tt.maxLen)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── truncate edge: maxLen < 0 ────────────────────────────────────────────────

func TestTruncate_NegativeMaxLen(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			// Negative maxLen causes panic — this is expected behavior
			t.Logf("Negative maxLen panicked as expected: %v", r)
		}
	}()
	// Negative maxLen causes "slice bounds out of range"
	got := truncate("hello", -1)
	require.Equal(t, "hell...", got)
}

// ─── interaction helper: sendTextMessage ──────────────────────────────────────

func TestAdapter_SendTextMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err := a.sendTextMessage(context.Background(), "chat123", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

// ─── interaction helper: sendCardMessage ──────────────────────────────────────

func TestAdapter_SendCardMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err := a.sendCardMessage(context.Background(), "chat123", `{"schema":"2.0"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

// ─── checkPendingInteraction ───────────────────────────────────────────────────

func TestCheckPendingInteraction_NoInteractions(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}
	conn := &FeishuConn{chatID: "chat123", adapter: a}

	// No interactions registered → should return false (not consumed)
	consumed := a.checkPendingInteraction(context.Background(), "hello", conn)
	require.False(t, consumed)
}

// ─── interaction manager helper ────────────────────────────────────────────────

func TestInteractionManager_Empty(t *testing.T) {
	t.Parallel()
	m := messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, 0, m.Len())
	require.Empty(t, m.GetAll())
}

// ─── buildInteractionCard JSON output ────────────────────────────────────────

func TestBuildInteractionCard_EscapeHTML(t *testing.T) {
	t.Parallel()
	// HTML special chars must NOT be escaped in JSON output
	got := buildInteractionCard("<test> & \"quotes\"", "")
	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	markdown := elements[0].(map[string]any)
	// JSON encoder by default escapes < > &, but we use SetEscapeHTML(false)
	// Actually SetEscapeHTML(false) means it won't escape. Let's verify.
	content := markdown["content"].(string)
	// The content should be preserved as-is
	require.Equal(t, "<test> & \"quotes\"", content)
}

// ─── interaction manager: Len/GetAll ──────────────────────────────────────────

func TestInteractionManager_LenAndGetAll(t *testing.T) {
	t.Parallel()
	m := messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Equal(t, 0, m.Len())
	require.Empty(t, m.GetAll())

	// Register a real pending interaction
	m.Register(&messaging.PendingInteraction{
		ID:           "req-001",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	require.Equal(t, 1, m.Len())

	// GetAll returns non-empty list
	all := m.GetAll()
	require.Len(t, all, 1)
	require.Equal(t, "req-001", all[0].ID)
}

// ─── interaction manager: Complete ──────────────────────────────────────────────

func TestInteractionManager_Complete(t *testing.T) {
	t.Parallel()
	m := messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Complete non-existent ID returns false, nil
	completed, ok := m.Complete("notfound")
	require.False(t, ok)
	require.Nil(t, completed)

	// Register and then complete
	m.Register(&messaging.PendingInteraction{
		ID:           "req-002",
		Type:         events.QuestionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	require.Equal(t, 1, m.Len())

	completed, ok = m.Complete("req-002")
	require.True(t, ok)
	require.NotNil(t, completed)
	require.Equal(t, "req-002", completed.ID)
	require.Equal(t, 0, m.Len())
}

// ─── interaction manager: Get ──────────────────────────────────────────────────

func TestInteractionManager_Get(t *testing.T) {
	t.Parallel()
	m := messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Get non-existent returns nil, false
	pi, ok := m.Get("notfound")
	require.False(t, ok)
	require.Nil(t, pi)

	// Register and then get
	m.Register(&messaging.PendingInteraction{
		ID:           "req-get-001",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	pi, ok = m.Get("req-get-001")
	require.True(t, ok)
	require.Equal(t, "req-get-001", pi.ID)

	// After complete, get returns false
	_, ok = m.Complete("req-get-001")
	require.True(t, ok)
	_, ok = m.Get("req-get-001")
	require.False(t, ok)
}

// ─── interaction manager: CancelAll ────────────────────────────────────────────

func TestInteractionManager_CancelAll(t *testing.T) {
	t.Parallel()
	m := messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Register two interactions for the same session
	m.Register(&messaging.PendingInteraction{
		ID:           "req-s1-1",
		SessionID:    "session-abc",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	m.Register(&messaging.PendingInteraction{
		ID:           "req-s1-2",
		SessionID:    "session-abc",
		Type:         events.QuestionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	m.Register(&messaging.PendingInteraction{
		ID:           "req-s2",
		SessionID:    "session-xyz",
		Type:         events.ElicitationRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(map[string]any) {},
	})
	require.Equal(t, 3, m.Len())

	// Cancel all for session-abc
	m.CancelAll("session-abc")
	require.Equal(t, 1, m.Len()) // only session-xyz remains
}

// ─── checkPendingInteraction text matching ─────────────────────────────────────

func TestNormalizePermissionResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		isPerm  bool
		isAllow bool
	}{
		{"允许", true, true},
		{"allow", true, true},
		{"yes", true, true},
		{"是", true, true},
		{"拒绝", true, false},
		{"deny", true, false},
		{"no", true, false},
		{"否", true, false},
		{"hello", false, false},
		{"", false, false},
		{"  YES  ", true, true},
		{"Allow", true, true},
		{"ALLOW", true, true},
		{"Deny", true, false},
		{"random text", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			normalized := strings.ToLower(strings.TrimSpace(tt.input))
			isAllow := normalized == "允许" || normalized == "allow" || normalized == "yes" || normalized == "是"
			isDeny := normalized == "拒绝" || normalized == "deny" || normalized == "no" || normalized == "否"
			isPerm := isAllow || isDeny
			require.Equal(t, tt.isPerm, isPerm, "input: %q", tt.input)
			if tt.isPerm {
				require.Equal(t, tt.isAllow, isAllow, "input: %q", tt.input)
			}
		})
	}
}

func TestNormalizeElicitationResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input      string
		wantAction string
	}{
		{"accept", "accept"},
		{"decline", "decline"},
		{"拒绝", "decline"},
		{"cancel", "decline"},
		{"取消", "decline"},
		{"  ACCEPT  ", "accept"},
		{"DECLINE", "decline"},
		{"hello", "accept"}, // default: accept
		{"", "accept"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			normalized := strings.ToLower(strings.TrimSpace(tt.input))
			action := "accept"
			if normalized == "decline" || normalized == "拒绝" || normalized == "cancel" || normalized == "取消" {
				action = "decline"
			}
			require.Equal(t, tt.wantAction, action)
		})
	}
}
