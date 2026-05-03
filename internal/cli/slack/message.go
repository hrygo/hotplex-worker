package slackcli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/slack-go/slack"
)

type SendResult struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

func SendMessage(ctx context.Context, client *slack.Client, channel, threadTS, text string) (*SendResult, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	ch, ts, err := client.PostMessageContext(ctx, channel, opts...)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return &SendResult{Channel: ch, TS: ts}, nil
}

func UpdateMessage(ctx context.Context, client *slack.Client, channel, ts, text string) (*SendResult, error) {
	_, ch, newTS, err := client.UpdateMessageContext(ctx, channel, ts, slack.MsgOptionText(text, false))
	if err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}
	return &SendResult{Channel: ch, TS: newTS}, nil
}

func ScheduleMessage(ctx context.Context, client *slack.Client, channel string, postAt int64, text string) (string, error) {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	ch, ts, err := client.ScheduleMessageContext(ctx, channel, strconv.FormatInt(postAt, 10), opts...)
	if err != nil {
		return "", fmt.Errorf("schedule message: %w", err)
	}
	_ = ch
	return ts, nil
}
