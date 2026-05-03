package slackcli

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

type BookmarkResult struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Emoji string `json:"emoji,omitempty"`
}

func AddBookmark(ctx context.Context, client *slack.Client, channel, title, link, emoji string) (*BookmarkResult, error) {
	params := slack.AddBookmarkParameters{
		Title: title,
		Type:  "link",
		Link:  link,
		Emoji: emoji,
	}

	bm, err := client.AddBookmarkContext(ctx, channel, params)
	if err != nil {
		return nil, fmt.Errorf("add bookmark: %w", err)
	}

	return &BookmarkResult{
		ID:    bm.ID,
		Title: bm.Title,
		URL:   bm.Link,
		Emoji: bm.Emoji,
	}, nil
}

func ListBookmarks(ctx context.Context, client *slack.Client, channel string) ([]BookmarkResult, error) {
	bookmarks, err := client.ListBookmarksContext(ctx, channel)
	if err != nil {
		return nil, fmt.Errorf("list bookmarks: %w", err)
	}

	var result []BookmarkResult
	for _, bm := range bookmarks {
		result = append(result, BookmarkResult{
			ID:    bm.ID,
			Title: bm.Title,
			URL:   bm.Link,
			Emoji: bm.Emoji,
		})
	}

	return result, nil
}

func RemoveBookmark(ctx context.Context, client *slack.Client, channel, bookmarkID string) error {
	if err := client.RemoveBookmarkContext(ctx, channel, bookmarkID); err != nil {
		return fmt.Errorf("remove bookmark: %w", err)
	}
	return nil
}
