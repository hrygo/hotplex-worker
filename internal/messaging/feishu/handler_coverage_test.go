package feishu

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestHandleMessage_NilEvent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{})
	require.NoError(t, err)
}

func TestHandleMessage_NilMessage(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{},
	})
	require.NoError(t, err)
}

func TestHandleMessage_BotSelfMessage(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	sender := larkim.NewEventSenderBuilder().
		SenderType("app").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_bot").
		MessageType("text").
		Content(`{"text":"bot msg"}`).
		ChatId("ch1").
		ChatType("p2p").
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_ExpiredMessage(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	expiredMs := strconv.FormatInt(time.Now().Add(-31*time.Minute).UnixMilli(), 10)
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_expired").
		MessageType("text").
		Content(`{"text":"old"}`).
		CreateTime(expiredMs).
		ChatId("ch1").
		ChatType("p2p").
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_EmptyMessageID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("").
		MessageType("text").
		Content(`{"text":"hello"}`).
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_DedupDuplicate(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user_dedup").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_dedup_1").
		MessageType("text").
		Content(`{"text":"hello"}`).
		ChatId("chat_dedup").
		ChatType("p2p").
		Build()

	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	}

	// First call: no bridge → handleTextMessage returns nil.
	err := a.handleMessage(context.Background(), event)
	require.NoError(t, err)

	// Second call with same message ID → dedup returns nil.
	err = a.handleMessage(context.Background(), event)
	require.NoError(t, err)
}

func TestHandleMessage_UnsupportedType(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_unsup").
		MessageType("interactive").
		Content(`{}`).
		ChatId("ch1").
		ChatType("group").
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_AbortCommand(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_abort").
		MessageType("text").
		Content(`{"text":"/abort"}`).
		ChatId("chat_abort").
		ChatType("p2p").
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_TextNoBridge(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_text").
		MessageType("text").
		Content(`{"text":"hello world"}`).
		ChatId("chat_text").
		ChatType("p2p").
		Build()

	// No bridge → handleTextMessage returns nil.
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_TextWithGateAllowed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })
	a.gate = NewGate("", "", false, nil, nil, nil) // no restrictions → DM always allowed

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_gate").
		MessageType("text").
		Content(`{"text":"hello"}`).
		ChatId("ch1").
		ChatType("p2p").
		Build()

	// Gate allows DM → proceeds to chatQueue (no bridge → handleTextMessage returns nil).
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)
}

func TestHandleMessage_GateRejected(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })
	a.gate = NewGate("allowlist", "allowlist", true, []string{"allowed_user"}, nil, nil)

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("stranger").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_rejected").
		MessageType("text").
		Content(`{"text":"hello"}`).
		ChatId("ch1").
		ChatType("group").
		Build()

	// Gate rejects group message from unknown user.
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err) // returns nil, message silently dropped
}

func TestHandleMessage_HelpCommand(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_help").
		MessageType("text").
		Content(`{"text":"/help"}`).
		ChatId("ch_help").
		ChatType("p2p").
		Build()

	// Help command path → replyMessage fails (nil client) but error is ignored.
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)

	// Wait for chatQueue to process.
	time.Sleep(100 * time.Millisecond)
}

func TestHandleMessage_ImageNoBridge(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_img").
		MessageType("image").
		Content(`{"image_key":"img_abc"}`).
		ChatId("ch_img").
		ChatType("p2p").
		Build()

	// Image message → ConvertMessage returns ok=true, text="[用户发送了一张图片]".
	// No bridge → handleTextMessage returns nil.
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

func TestHandleMessage_PostWithThread(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("user1").Build()).
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_post").
		MessageType("post").
		Content(`{"title":"Hello","content":[[{"tag":"text","text":"world"}]]}`).
		ChatId("ch_post").
		ChatType("group").
		RootId("root_123").
		ParentId("parent_456").
		Build()

	// Post with thread → threadKey = rootId.
	// No bridge → handleTextMessage returns nil.
	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

func TestHandleMessage_NilSenderUserID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(func() { a.chatQueue.Close() })

	// Sender with no SenderId → userID stays empty.
	sender := larkim.NewEventSenderBuilder().
		SenderType("user").
		Build()
	msg := larkim.NewEventMessageBuilder().
		MessageId("msg_nosender").
		MessageType("text").
		Content(`{"text":"hello"}`).
		ChatId("ch1").
		ChatType("p2p").
		Build()

	err := a.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: msg},
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}
