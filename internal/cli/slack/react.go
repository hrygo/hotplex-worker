package slackcli

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"
)

func AddReaction(ctx context.Context, client *slack.Client, channel, ts, emoji string) error {
	item := slack.ItemRef{
		Channel:   channel,
		Timestamp: ts,
	}
	if err := client.AddReactionContext(ctx, emoji, item); err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}
	return nil
}

func RemoveReaction(ctx context.Context, client *slack.Client, channel, ts, emoji string) error {
	item := slack.ItemRef{
		Channel:   channel,
		Timestamp: ts,
	}
	if err := client.RemoveReactionContext(ctx, emoji, item); err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	return nil
}
