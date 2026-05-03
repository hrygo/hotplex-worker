package slackcli

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

type ChannelInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       string `json:"user,omitempty"`
	IsDM       bool   `json:"is_dm"`
	IsGroup    bool   `json:"is_group"`
	IsIM       bool   `json:"is_im"`
	NumMembers int    `json:"num_members,omitempty"`
}

func ListChannels(ctx context.Context, client *slack.Client, types string, limit int) ([]ChannelInfo, error) {
	typeSlice := []string{"im"}
	if types != "" {
		typeSlice = strings.Split(types, ",")
	}

	params := &slack.GetConversationsParameters{
		Types:  typeSlice,
		Limit:  limit,
	}

	var result []ChannelInfo
	for {
		channels, next, err := client.GetConversationsContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}

		for _, ch := range channels {
			result = append(result, ChannelInfo{
				ID:         ch.ID,
				Name:       ch.Name,
				User:       ch.User,
				IsDM:       ch.IsIM,
				IsGroup:    ch.IsGroup,
				IsIM:       ch.IsIM,
				NumMembers: ch.NumMembers,
			})
		}

		if next == "" || len(result) >= limit {
			break
		}
		params.Cursor = next
	}

	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}
