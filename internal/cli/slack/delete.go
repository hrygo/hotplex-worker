package slackcli

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

func DeleteFile(ctx context.Context, client *slack.Client, fileID string) error {
	if fileID == "" {
		return fmt.Errorf("file-id is required")
	}

	if err := client.DeleteFileContext(ctx, fileID); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}

	return nil
}
