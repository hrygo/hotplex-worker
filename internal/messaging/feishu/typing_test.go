package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

func TestAddReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		},
	}
	_, err := a.addReaction(context.Background(), "msg123", "THINKING")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestRemoveReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		},
	}
	err := a.removeReaction(context.Background(), "msg123", "rid123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAddTypingIndicator_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		},
	}
	_, err := a.AddTypingIndicator(context.Background(), "msg123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestRemoveTypingIndicator_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		},
	}
	err := a.RemoveTypingIndicator(context.Background(), "msg123", "rid123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}
