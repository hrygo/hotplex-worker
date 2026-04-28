package feishu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestFeishuConn_SendSkillsList_NoSkills(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{},
				Total:  0,
			},
		},
	}

	// With nil lark client, sendSkillsText will fail but the "no skills found" path is exercised.
	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_SingleSkill(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{
					{Name: "commit", Description: "Create a git commit", Source: "project"},
				},
				Total: 1,
			},
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_MultipleSkills(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{
					{Name: "commit", Description: "Create a git commit", Source: "project"},
					{Name: "review", Description: "Review code changes", Source: "project"},
					{Name: "deploy", Description: "Deploy to production", Source: "user"},
				},
				Total: 3,
			},
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_LongDescription(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	// Description > 120 runes should be truncated.
	longDesc := ""
	for i := 0; i < 200; i++ {
		longDesc += "x"
	}

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{
					{Name: "long-skill", Description: longDesc, Source: "project"},
				},
				Total: 1,
			},
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
}

func TestFeishuConn_SendSkillsList_BatchSplit(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	// 25 skills should trigger 2 batches (20 + 5).
	skills := make([]events.SkillEntry, 25)
	for i := range skills {
		skills[i] = events.SkillEntry{
			Name:        "skill-" + string(rune('a'+i%26)),
			Description: "Description for skill",
			Source:      "project",
		}
	}

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: skills,
				Total:  25,
			},
		},
	}

	// First batch sends, then second batch also sends; both fail at lark client.
	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
}

func TestFeishuConn_SendSkillsList_MapData(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: map[string]any{
				"skills": []any{
					map[string]any{
						"name":        "commit",
						"description": "Create a git commit",
						"source":      "project",
					},
				},
				"total": 1,
			},
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_UnsupportedData(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: "invalid data type",
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.NoError(t, err, "unsupported data type should return nil")
}

func TestFeishuConn_SendSkillsText_WithReplyTo(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "msg_reply_to_123")

	// replyToMsgID is set, so it will try replyMessage (nil client → error).
	err := conn.sendSkillsText(context.Background(), "test text")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsText_NoReplyTo(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "")

	// No replyToMsgID, so it will try sendTextMessage (nil client → error).
	err := conn.sendSkillsText(context.Background(), "test text")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_WithReplyToMsgID(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "msg_reply_to_abc")

	env := &events.Envelope{
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{
					{Name: "commit", Description: "Create a git commit", Source: "project"},
				},
				Total: 1,
			},
		},
	}

	err := conn.sendSkillsList(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestFeishuConn_SendSkillsList_WriteCtxIntegration(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := newTestConn(adapter, "msg_platform")
	conn.platformMsgID = "msg_platform"

	env := &events.Envelope{
		SessionID: "session-1",
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{
					{Name: "commit", Description: "Create a git commit"},
				},
				Total: 1,
			},
		},
	}

	// WriteCtx dispatches to sendSkillsList which fails at lark client.
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}
