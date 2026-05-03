package slackcli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slack-go/slack"
)

type UploadParams struct {
	FilePath  string
	Title     string
	Comment   string
	Channel   string
	ThreadTS  string
	MaxSize   int64
}

type UploadResult struct {
	FileID  string `json:"file_id"`
	Title   string `json:"title"`
	Size    int64  `json:"size"`
	Channel string `json:"channel"`
}

func UploadFile(ctx context.Context, client *slack.Client, params *UploadParams) (*UploadResult, error) {
	stat, err := os.Stat(params.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	if params.MaxSize > 0 && stat.Size() > params.MaxSize {
		return nil, fmt.Errorf("file size %d exceeds limit %d", stat.Size(), params.MaxSize)
	}

	title := params.Title
	if title == "" {
		title = filepath.Base(params.FilePath)
	}

	uploadParams := slack.UploadFileParameters{
		Filename:       filepath.Base(params.FilePath),
		File:           params.FilePath,
		FileSize:       int(stat.Size()),
		Title:          title,
		InitialComment: params.Comment,
		Channel:        params.Channel,
		ThreadTimestamp: params.ThreadTS,
	}

	result, err := client.UploadFileContext(ctx, uploadParams)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %w", err)
	}

	return &UploadResult{
		FileID:  result.ID,
		Title:   result.Title,
		Size:    stat.Size(),
		Channel: params.Channel,
	}, nil
}
