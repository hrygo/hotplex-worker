package slack

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestBuildPermissionFallbackText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *events.PermissionRequestData
		want []string // substrings that must appear
	}{
		{
			name: "basic",
			data: &events.PermissionRequestData{ID: "req1", ToolName: "Bash"},
			want: []string{"Tool Approval Required", "Bash", "allow req1", "deny req1"},
		},
		{
			name: "with description",
			data: &events.PermissionRequestData{ID: "req2", ToolName: "Write", Description: "write a file"},
			want: []string{"write a file", "Write"},
		},
		{
			name: "with args",
			data: &events.PermissionRequestData{ID: "req3", ToolName: "Bash", Args: []string{"ls -la"}},
			want: []string{"Args: ls -la"},
		},
		{
			name: "empty args skipped",
			data: &events.PermissionRequestData{ID: "req4", ToolName: "Read", Args: []string{"{}"}},
			want: []string{"Read"},
		},
		{
			name: "long args truncated",
			data: &events.PermissionRequestData{ID: "req5", ToolName: "Edit", Args: []string{string(make([]byte, 600))}},
			want: []string{"..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildPermissionFallbackText(tt.data)
			for _, s := range tt.want {
				require.Contains(t, got, s)
			}
		})
	}
}

func TestBuildQuestionFallbackText(t *testing.T) {
	t.Parallel()

	data := &events.QuestionRequestData{
		ID: "q1",
		Questions: []events.Question{
			{
				Header:   "Choose option",
				Question: "Which framework?",
				Options: []events.QuestionOption{
					{Label: "React", Description: "Frontend library"},
					{Label: "Vue"},
				},
			},
		},
	}
	got := buildQuestionFallbackText(data)
	require.Contains(t, got, "Choose option")
	require.Contains(t, got, "Which framework?")
	require.Contains(t, got, "React — Frontend library")
	require.Contains(t, got, "Vue")
	require.Contains(t, got, "q1")
}

func TestBuildQuestionFallbackText_EmptyHeader(t *testing.T) {
	t.Parallel()

	data := &events.QuestionRequestData{
		ID: "q2",
		Questions: []events.Question{
			{Question: "What?"},
		},
	}
	got := buildQuestionFallbackText(data)
	require.Contains(t, got, "Question 1")
}

func TestBuildElicitationFallbackText(t *testing.T) {
	t.Parallel()

	data := &events.ElicitationRequestData{
		ID:            "e1",
		MCPServerName: "my-server",
		Message:       "Please confirm",
	}
	got := buildElicitationFallbackText(data)
	require.Contains(t, got, "my-server")
	require.Contains(t, got, "Please confirm")
	require.Contains(t, got, "accept e1")
	require.Contains(t, got, "decline e1")
}

func TestBuildElicitationFallbackText_WithURL(t *testing.T) {
	t.Parallel()

	data := &events.ElicitationRequestData{
		ID:            "e2",
		MCPServerName: "srv",
		Message:       "msg",
		URL:           "https://example.com/form",
	}
	got := buildElicitationFallbackText(data)
	require.Contains(t, got, "https://example.com/form")
}

// ---------------------------------------------------------------------------
// checkPendingInteraction tests
// ---------------------------------------------------------------------------

func newTestInteractionAdapter() *Adapter {
	return &Adapter{
		BaseAdapter: messaging.BaseAdapter[*SlackConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
		},
		client: slack.New("x-test-token"),
	}
}

func TestCheckPendingInteraction_NoInteractions(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	consumed := a.checkPendingInteraction(context.Background(), "allow abc-123", "C1", "123.456", "U1")
	require.False(t, consumed)
}

func TestCheckPendingInteraction_PermissionAllow(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-perm-allow",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow req-perm-allow", "C1", "123.456", "U1")
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.True(t, pr["allowed"].(bool))
	require.Equal(t, "req-perm-allow", pr["request_id"])
	require.Equal(t, 0, a.Interactions.Len())
}

func TestCheckPendingInteraction_PermissionDeny(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-perm-deny",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "deny req-perm-deny", "C1", "123.456", "U1")
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.False(t, pr["allowed"].(bool))
	require.Equal(t, "user denied", pr["reason"])
}

func TestCheckPendingInteraction_ElicitationAccept(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-elic-accept",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "accept req-elic-accept", "C1", "123.456", "U1")
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "accept", er["action"])
	require.Equal(t, "req-elic-accept", er["id"])
}

func TestCheckPendingInteraction_ElicitationDecline(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-elic-decline",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "decline req-elic-decline", "C1", "123.456", "U1")
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "decline", er["action"])
}

func TestCheckPendingInteraction_QuestionRawText(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-question",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.QuestionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	// Single-word input triggers raw text path (no action keyword).
	consumed := a.checkPendingInteraction(context.Background(), "yes", "C1", "123.456", "U1")
	require.True(t, consumed)
	qr := capturedMetadata["question_response"].(map[string]any)
	require.Equal(t, "req-question", qr["id"])
	answers := qr["answers"].(map[string]string)
	require.Equal(t, "yes", answers["_"])
}

func TestCheckPendingInteraction_OwnerMismatch(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-owner",
		SessionID:    "sess-1",
		OwnerID:      "U-correct",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow req-owner", "C1", "123.456", "U-wrong")
	require.False(t, consumed)
	require.Equal(t, 1, a.Interactions.Len())
}

func TestCheckPendingInteraction_FallbackCandidateMatch(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-fallback",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	// Use non-matching requestID to trigger fallback, then candidate type match.
	consumed := a.checkPendingInteraction(context.Background(), "allow nonexistent-id", "C1", "123.456", "U1")
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.True(t, pr["allowed"].(bool))
}

func TestCheckPendingInteraction_WrongActionForType(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-wrong-action",
		SessionID:    "sess-1",
		OwnerID:      "U1",
		Type:         events.ElicitationRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// "allow" is not valid for ElicitationRequest (needs accept/decline).
	consumed := a.checkPendingInteraction(context.Background(), "allow req-wrong-action", "C1", "123.456", "U1")
	require.False(t, consumed)
}

func TestCheckPendingInteraction_RawTextNotQuestion(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-not-question",
		SessionID:    "sess-1",
		OwnerID:      "U1",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// Raw text (no action keyword) only matches QuestionRequest.
	consumed := a.checkPendingInteraction(context.Background(), "some random text", "C1", "123.456", "U1")
	require.False(t, consumed)
}

// ---------------------------------------------------------------------------
// Args preview backtick stripping
// ---------------------------------------------------------------------------

func TestArgsPreview_BacktickStripping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"nested backticks", "plan with ```code``` inside", "plan with code inside"},
		{"multiple blocks", "```a``` and ```b```", "a and b"},
		{"triple at boundaries", "```start end```", "start end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preview := tt.args
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			preview = strings.ReplaceAll(preview, "```", "")
			require.Equal(t, tt.want, preview)
		})
	}
}
