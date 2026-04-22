package feishu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/messaging/stt"
)

// ---------------------------------------------------------------------------
// FeishuSTT
// ---------------------------------------------------------------------------

func TestFeishuSTT_ImplementsTranscriber(t *testing.T) {
	t.Parallel()
	var _ Transcriber = (*FeishuSTT)(nil)
}

func TestFeishuSTT_ImplementsSharedTranscriber(t *testing.T) {
	t.Parallel()
	var _ stt.Transcriber = (*FeishuSTT)(nil)
}

func TestFeishuSTT_RequiresDisk(t *testing.T) {
	t.Parallel()
	s := &FeishuSTT{}
	require.False(t, s.RequiresDisk())
}

func TestFeishuSTT_NilClient(t *testing.T) {
	t.Parallel()
	s := NewFeishuSTT(nil, nil)
	_, err := s.Transcribe(context.Background(), []byte("fake"))
	require.Error(t, err)
}
