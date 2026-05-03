package slackcli

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

type SearchResult struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Text      string `json:"text,omitempty"`
	User      string `json:"user,omitempty"`
	Title     string `json:"title,omitempty"`
	FileID    string `json:"file_id,omitempty"`
	Size      int    `json:"size,omitempty"`
}

func Search(ctx context.Context, client *slack.Client, query, searchType string, limit int) ([]SearchResult, error) {
	params := slack.SearchParameters{
		Count: limit,
		Sort:  "timestamp",
	}

	var results []SearchResult

	if searchType == "all" || searchType == "messages" {
		msgs, err := client.SearchMessagesContext(ctx, query, params)
		if err != nil {
			return nil, fmt.Errorf("search messages: %w", err)
		}
		if msgs != nil {
			for _, m := range msgs.Matches {
				results = append(results, SearchResult{
					Type:      "message",
					Timestamp: m.Timestamp,
					Channel:   m.Channel.Name,
					Text:      m.Text,
					User:      m.User,
				})
			}
		}
	}

	if searchType == "all" || searchType == "files" {
		files, err := client.SearchFilesContext(ctx, query, params)
		if err != nil {
			return nil, fmt.Errorf("search files: %w", err)
		}
		if files != nil {
			for _, f := range files.Matches {
				results = append(results, SearchResult{
					Type:   "file",
					Title:  f.Title,
					FileID: f.ID,
					Size:   f.Size,
				})
			}
		}
	}

	return results, nil
}
