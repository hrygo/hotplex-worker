package slack

import (
	"testing"

	"github.com/stretchr/testify/require"

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
