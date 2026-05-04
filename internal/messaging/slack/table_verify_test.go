//go:build slack_e2e

package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Table rendering verification against real Slack API
// ---------------------------------------------------------------------------

func loadVerifyClient(t *testing.T) (*slack.Client, string) {
	t.Helper()
	token := os.Getenv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN")
	if token == "" {
		token = os.Getenv("SLACK_BOT_TOKEN")
	}
	require.NotEmpty(t, token, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN required")

	channel := os.Getenv("SLACK_TEST_DM_CHANNEL")
	if channel == "" {
		channel = os.Getenv("HOTPLEX_SLACK_CHANNEL_ID")
	}
	require.NotEmpty(t, channel, "SLACK_TEST_DM_CHANNEL or HOTPLEX_SLACK_CHANNEL_ID required")

	return slack.New(token), channel
}

func buildTestTable() *slack.TableBlock {
	table := slack.NewTableBlock("verify_table")
	table = table.WithColumnSettings(
		slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: true},
		slack.ColumnSetting{Align: slack.ColumnAlignmentRight, IsWrapped: false},
		slack.ColumnSetting{Align: slack.ColumnAlignmentCenter, IsWrapped: true},
	)
	table.AddRow(richTextCell("Name"), richTextCell("Score"), richTextCell("Grade"))
	table.AddRow(richTextCell("Alice"), richTextCell("95"), richTextCell("A"))
	table.AddRow(richTextCell("Bob"), richTextCell("82"), richTextCell("B"))
	table.AddRow(richTextCell("Carol"), richTextCell("78"), richTextCell("C"))
	return table
}

// ---------------------------------------------------------------------------
// Test 1: PostMessage + TableBlock (single table, non-streaming)
// ---------------------------------------------------------------------------

func TestVerify_PostMessage_TableBlock(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	table := buildTestTable()
	text := "Verification: PostMessage + TableBlock (single table)"
	blocks := []slack.Block{
		slack.NewMarkdownBlock("md_text", text),
		table,
	}

	b, _ := json.MarshalIndent(blocks, "", "  ")
	t.Logf("Blocks JSON:\n%s", string(b))

	ch, ts, err := client.PostMessageContext(ctx, channel,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		t.Errorf("FAIL: PostMessage with TableBlock rejected: %v", err)
		if strings.Contains(err.Error(), "invalid_blocks") {
			t.Errorf("  → Slack returned invalid_blocks: TableBlock NOT supported by this workspace/app")
		}
	} else {
		t.Logf("OK: PostMessage with TableBlock accepted — channel=%s ts=%s", ch, ts)
	}
}

// ---------------------------------------------------------------------------
// Test 2: PostMessage + MarkdownBlock only (multi-table code fence)
// ---------------------------------------------------------------------------

func TestVerify_PostMessage_MarkdownBlock(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wrapped := "Verification: MarkdownBlock with table in code fence\n```\n| Name | Score | Grade |\n|------|-------|-------|\n| Alice | 95 | A |\n| Bob | 82 | B |\n```"
	blocks := []slack.Block{
		slack.NewMarkdownBlock("md_full", wrapped),
	}

	b, _ := json.MarshalIndent(blocks, "", "  ")
	t.Logf("Blocks JSON:\n%s", string(b))

	ch, ts, err := client.PostMessageContext(ctx, channel,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText("MarkdownBlock test", false),
	)
	if err != nil {
		t.Errorf("FAIL: PostMessage with MarkdownBlock rejected: %v", err)
	} else {
		t.Logf("OK: PostMessage with MarkdownBlock accepted — channel=%s ts=%s", ch, ts)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Streaming — StopStream with blocks (table upgrade path)
// ---------------------------------------------------------------------------

func TestVerify_Stream_StopWithBlocks(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Phase 1: Start stream
	_, streamTS, err := client.StartStreamContext(ctx, channel,
		slack.MsgOptionMarkdownText(":test_tube: Stream table verification starting..."),
	)
	require.NoError(t, err, "StartStream failed")
	t.Logf("Stream started: ts=%s", streamTS)

	// Phase 2: Append table content as markdown text
	tableMD := "\n\n| Name | Score | Grade |\n|------|-------|-------|\n| Alice | 95 | A |\n| Bob | 82 | B |\n| Carol | 78 | C |"
	_, _, err = client.AppendStreamContext(ctx, channel, streamTS,
		slack.MsgOptionMarkdownText(tableMD),
	)
	if err != nil {
		t.Errorf("AppendStream failed: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Phase 3: Stop stream with TableBlock
	table := buildTestTable()
	stopBlocks := []slack.Block{
		slack.NewMarkdownBlock("md_text", "Verification: StopStream with TableBlock (streaming table upgrade)"),
		table,
	}

	b, _ := json.MarshalIndent(stopBlocks, "", "  ")
	t.Logf("Stop blocks JSON:\n%s", string(b))

	ch, newTS, stopErr := client.StopStreamContext(ctx, channel, streamTS,
		slack.MsgOptionBlocks(stopBlocks...),
		slack.MsgOptionText("StopStream table test", false),
	)
	if stopErr != nil {
		t.Errorf("FAIL: StopStream with blocks returned error: %v", stopErr)
		// Retry plain stop
		_, _, _ = client.StopStreamContext(ctx, channel, streamTS)
		t.Logf("  → Retried plain StopStream (blocks NOT applied)")
	} else {
		t.Logf("OK: StopStream with blocks accepted — channel=%s ts=%s", ch, newTS)
		t.Logf("  → Check Slack: does message show proper table or pipe-delimited text?")
	}
}

// ---------------------------------------------------------------------------
// Test 4: Stream → Stop → chat.update with TableBlock
// ---------------------------------------------------------------------------

func TestVerify_Stream_ThenUpdateWithBlocks(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Phase 1: Start stream
	_, streamTS, err := client.StartStreamContext(ctx, channel,
		slack.MsgOptionMarkdownText(":test_tube: Stream then update verification..."),
	)
	require.NoError(t, err, "StartStream failed")
	t.Logf("Stream started: ts=%s", streamTS)

	// Phase 2: Append table content
	tableMD := "\n\n| Name | Score | Grade |\n|------|-------|-------|\n| Alice | 95 | A |\n| Bob | 82 | B |"
	_, _, err = client.AppendStreamContext(ctx, channel, streamTS,
		slack.MsgOptionMarkdownText(tableMD),
	)
	if err != nil {
		t.Errorf("AppendStream failed: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Phase 3: Stop stream WITHOUT blocks (plain stop)
	_, _, stopErr := client.StopStreamContext(ctx, channel, streamTS)
	if stopErr != nil {
		t.Errorf("Plain StopStream failed: %v", stopErr)
		return
	}
	t.Logf("Plain StopStream OK")

	time.Sleep(1 * time.Second)

	// Phase 4: Try chat.update with TableBlock to replace streamed content
	table := buildTestTable()
	updateBlocks := []slack.Block{
		slack.NewMarkdownBlock("md_text", "Verification: chat.update with TableBlock (post-stream replacement)"),
		table,
	}

	_, ch, newTS, updateErr := client.UpdateMessageContext(ctx, channel, streamTS,
		slack.MsgOptionBlocks(updateBlocks...),
		slack.MsgOptionText("Updated with table", false),
	)
	if updateErr != nil {
		t.Errorf("FAIL: chat.update with TableBlock rejected: %v", updateErr)
		if strings.Contains(updateErr.Error(), "block_mismatch") {
			t.Logf("  → block_mismatch confirmed: rich_text blocks from streaming cannot be replaced")
		} else if strings.Contains(updateErr.Error(), "invalid_blocks") {
			t.Logf("  → invalid_blocks: TableBlock NOT supported")
		}
	} else {
		t.Logf("OK: chat.update with TableBlock accepted — channel=%s ts=%s", ch, newTS)
	}
}

// ---------------------------------------------------------------------------
// Test 5: Stream → Stop → PostMessage follow-up with TableBlock
//   (This is the proposed fix approach)
// ---------------------------------------------------------------------------

func TestVerify_Stream_ThenFollowUpTable(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Phase 1: Start stream
	_, streamTS, err := client.StartStreamContext(ctx, channel,
		slack.MsgOptionMarkdownText(":test_tube: Follow-up table test..."),
	)
	require.NoError(t, err, "StartStream failed")
	t.Logf("Stream started: ts=%s", streamTS)

	// Phase 2: Append content with table
	content := "\n\nHere's the data:\n\n| Name | Score | Grade |\n|------|-------|-------|\n| Alice | 95 | A |\n| Bob | 82 | B |"
	_, _, err = client.AppendStreamContext(ctx, channel, streamTS,
		slack.MsgOptionMarkdownText(content),
	)
	if err != nil {
		t.Errorf("AppendStream failed: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Phase 3: Plain stop
	_, _, _ = client.StopStreamContext(ctx, channel, streamTS)
	t.Logf("Stream stopped")

	time.Sleep(500 * time.Millisecond)

	// Phase 4: Follow-up PostMessage with proper TableBlock
	table := buildTestTable()
	followUpText := fmt.Sprintf("Table for <https://hotplex.slack.com/archives/%s/p%s|streamed message>:", channel, strings.ReplaceAll(streamTS, ".", ""))
	followUpBlocks := []slack.Block{
		slack.NewMarkdownBlock("md_text", followUpText),
		table,
	}

	ch, newTS, err := client.PostMessageContext(ctx, channel,
		slack.MsgOptionBlocks(followUpBlocks...),
		slack.MsgOptionText("Follow-up table", false),
		slack.MsgOptionTS(streamTS), // reply in thread
	)
	if err != nil {
		t.Errorf("FAIL: Follow-up PostMessage with TableBlock rejected: %v", err)
	} else {
		t.Logf("OK: Follow-up PostMessage accepted — channel=%s ts=%s", ch, newTS)
		t.Logf("  → This is the proposed fix: separate message with proper table")
	}
}

// ---------------------------------------------------------------------------
// Test 6: Stream → Stop with markdown_text only (final text update)
// ---------------------------------------------------------------------------

func TestVerify_Stream_StopWithMarkdownText(t *testing.T) {
	client, channel := loadVerifyClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Phase 1: Start stream
	_, streamTS, err := client.StartStreamContext(ctx, channel,
		slack.MsgOptionMarkdownText(":test_tube: StopStream markdown_text test..."),
	)
	require.NoError(t, err, "StartStream failed")
	t.Logf("Stream started: ts=%s", streamTS)

	// Phase 2: Append content
	_, _, err = client.AppendStreamContext(ctx, channel, streamTS,
		slack.MsgOptionMarkdownText("\n\nStreaming some content..."),
	)
	if err != nil {
		t.Errorf("AppendStream failed: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Phase 3: Stop with markdown_text containing table in code fence
	finalText := "Verification: StopStream with markdown_text (code-fenced table)\n```\n| Name | Score | Grade |\n|------|-------|-------|\n| Alice | 95 | A |\n| Bob | 82 | B |\n```"
	ch, newTS, stopErr := client.StopStreamContext(ctx, channel, streamTS,
		slack.MsgOptionMarkdownText(finalText),
	)
	if stopErr != nil {
		t.Errorf("FAIL: StopStream with markdown_text failed: %v", stopErr)
	} else {
		t.Logf("OK: StopStream with markdown_text accepted — channel=%s ts=%s", ch, newTS)
	}
}
