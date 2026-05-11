package feishu

import (
	"context"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func (c *FeishuConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.sendSkillsText(ctx, messaging.FormatEmptySkillsMsg(d.Filter))
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	pages := messaging.PaginateSkillGroups(groups, messaging.SkillsPerPage)

	for i, page := range pages {
		header := messaging.SkillsHeader(d, i+1, len(pages))

		var sb strings.Builder
		sb.WriteString(header)
		sb.WriteByte('\n')

		for _, g := range page {
			for _, s := range g.Entries {
				desc := messaging.TruncateDesc(s.Description)
				emoji := messaging.SourceEmoji(s.Source)
				fmt.Fprintf(&sb, "%s *%s* — %s\n", emoji, s.Name, desc)
			}
		}

		if err := c.sendSkillsText(ctx, sb.String()); err != nil {
			return err
		}
	}
	return nil
}

func (c *FeishuConn) sendSkillsText(ctx context.Context, text string) error {
	return c.sendOrReply(ctx, text)
}
