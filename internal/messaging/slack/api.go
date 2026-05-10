package slack

import (
	"context"
	"io"

	"github.com/slack-go/slack"
)

// SlackAPI abstracts the subset of slack.Client methods used by the adapter.
// *slack.Client satisfies this interface implicitly — no wrapper needed.
type SlackAPI interface {
	PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
	AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error)
	GetUserInfoContext(ctx context.Context, user string) (*slack.User, error)
	UploadFileContext(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)
	StartStreamContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
	AppendStreamContext(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, error)
	StopStreamContext(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, error)
	UpdateMessageContext(ctx context.Context, channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	PostEphemeralContext(ctx context.Context, channelID, userID string, options ...slack.MsgOption) (string, error)
	GetFileContext(ctx context.Context, downloadURL string, writer io.Writer) error
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	RemoveReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	SetAssistantThreadsStatusContext(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error
}

// Compile-time verification that *slack.Client satisfies SlackAPI.
var _ SlackAPI = (*slack.Client)(nil)
