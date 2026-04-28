package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func (c *SlackConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.postSkillsMessage(ctx, messaging.FormatEmptySkillsMsg(d.Filter), nil)
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	pages := messaging.PaginateSkillGroups(groups, messaging.SkillsPerPage)

	for i, page := range pages {
		var blocks []slack.Block

		header := messaging.SkillsHeader(d, i+1, len(pages))
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.PlainTextType, header, false, false), nil, nil))

		for _, g := range page {
			emoji := messaging.SourceEmoji(g.Source)

			var sb strings.Builder
			fmt.Fprintf(&sb, "*%s %s (%d)*\n", emoji, g.Source, len(g.Entries))
			for _, s := range g.Entries {
				desc := messaging.TruncateDesc(s.Description)
				fmt.Fprintf(&sb, "• %s — %s\n", s.Name, desc)
			}
			blocks = append(blocks, slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, sb.String(), false, false), nil, nil))

			if len(blocks) >= messaging.SkillsBlockSoftLimit {
				break
			}
		}

		if len(blocks) > messaging.SkillsBlockHardLimit {
			blocks = blocks[:messaging.SkillsBlockHardLimit]
		}

		fallback := header + "\n" + formatSkillsPlainText(page)
		if err := c.postSkillsMessage(ctx, fallback, blocks); err != nil {
			return err
		}
	}

	return nil
}

func (c *SlackConn) postSkillsMessageFallback(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.postSkillsMessage(ctx, messaging.FormatEmptySkillsMsg(d.Filter), nil)
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	pages := messaging.PaginateSkillGroups(groups, messaging.SkillsPerPage)

	for i, page := range pages {
		header := messaging.SkillsHeader(d, i+1, len(pages))
		text := header + "\n" + formatSkillsPlainText(page)
		if err := c.postSkillsMessage(ctx, text, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *SlackConn) postSkillsMessage(ctx context.Context, fallback string, blocks []slack.Block) error {
	opts := []slack.MsgOption{slack.MsgOptionText(fallback, false)}
	if len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
	}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	return err
}

func formatSkillsPlainText(page []messaging.SkillGroup) string {
	var sb strings.Builder
	for _, g := range page {
		emoji := messaging.SourceEmoji(g.Source)
		fmt.Fprintf(&sb, "\n*%s %s (%d)*\n", emoji, g.Source, len(g.Entries))
		for _, s := range g.Entries {
			desc := messaging.TruncateDesc(s.Description)
			fmt.Fprintf(&sb, "• %s — %s\n", s.Name, desc)
		}
	}
	return sb.String()
}
