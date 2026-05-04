package slackcli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteFile(t *testing.T) {
	t.Parallel()

	t.Run("empty file id returns error", func(t *testing.T) {
		t.Parallel()

		err := DeleteFile(context.Background(), nil, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "file-id is required")
	})
}
