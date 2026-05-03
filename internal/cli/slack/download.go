package slackcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slack-go/slack"
)

func DownloadFile(ctx context.Context, client *slack.Client, fileID, outputPath string) error {
	info, _, _, err := client.GetFileInfoContext(ctx, fileID, 0, 0)
	if err != nil {
		return fmt.Errorf("get file info: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := client.GetFileContext(ctx, info.URLPrivateDownload, f); err != nil {
		return fmt.Errorf("download file: %w", err)
	}

	return nil
}
