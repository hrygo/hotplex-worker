package feishu

import (
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func ResolveMentions(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
	if len(mentions) == 0 {
		return text
	}
	for _, m := range mentions {
		if m.Key == nil || m.Id == nil {
			continue
		}
		key := *m.Key
		openID := ptrStr(m.Id.OpenId)

		if key == "@_all" {
			continue
		}

		if openID == botOpenID {
			text = strings.ReplaceAll(text, key+" ", "")
			text = strings.ReplaceAll(text, key, "")
		} else {
			name := ptrStr(m.Name)
			if name != "" {
				text = strings.ReplaceAll(text, key, "@"+name)
			}
		}
	}
	return strings.TrimSpace(text)
}
