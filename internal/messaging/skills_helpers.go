package messaging

import (
	"encoding/json"
	"fmt"

	"github.com/hrygo/hotplex/pkg/events"
)

const (
	SkillsDescMaxRunes   = 80
	SkillsDescCutRunes   = 77
	SkillsBlockSoftLimit = 48
	SkillsBlockHardLimit = 50
	SkillsPerPage        = 20

	SourceProject = "project"
	SourceGlobal  = "global"
)

type SkillGroup struct {
	Source  string
	Entries []events.SkillEntry
}

func ExtractSkillsListData(env *events.Envelope) (events.SkillsListData, error) {
	switch v := env.Event.Data.(type) {
	case events.SkillsListData:
		return v, nil
	case map[string]any:
		raw, _ := json.Marshal(v)
		var d events.SkillsListData
		_ = json.Unmarshal(raw, &d)
		return d, nil
	default:
		return events.SkillsListData{}, fmt.Errorf("unexpected skills data type: %T", env.Event.Data)
	}
}

func GroupSkillsBySource(skills []events.SkillEntry) []SkillGroup {
	idx := make(map[string]int)
	var groups []SkillGroup

	for _, s := range skills {
		pos, ok := idx[s.Source]
		if !ok {
			groups = append(groups, SkillGroup{Source: s.Source})
			pos = len(groups) - 1
			idx[s.Source] = pos
		}
		groups[pos].Entries = append(groups[pos].Entries, s)
	}

	sorted := make([]SkillGroup, 0, len(groups))
	for _, src := range []string{SourceProject, SourceGlobal} {
		if pos, ok := idx[src]; ok {
			sorted = append(sorted, groups[pos])
		}
	}
	for _, g := range groups {
		if g.Source != SourceProject && g.Source != SourceGlobal {
			sorted = append(sorted, g)
		}
	}
	return sorted
}

func SourceEmoji(source string) string {
	if source == SourceProject {
		return "📁"
	}
	return "🌐"
}

func TruncateDesc(desc string) string {
	runes := []rune(desc)
	if len(runes) <= SkillsDescMaxRunes {
		return desc
	}
	return string(runes[:SkillsDescCutRunes]) + "..."
}

func FormatEmptySkillsMsg(filter string) string {
	if filter != "" {
		return fmt.Sprintf("⚡ No skills matching `%s`.", filter)
	}
	return "⚡ No skills found."
}

func SkillsHeader(d events.SkillsListData, page, total int) string {
	h := fmt.Sprintf("⚡ Skills (%d)", d.Total)
	if d.Filter != "" {
		h = fmt.Sprintf("⚡ Skills matching `%s` (%d)", d.Filter, d.Total)
	}
	if total > 1 {
		h += fmt.Sprintf(" — Part %d/%d", page, total)
	}
	return h
}

func PaginateSkillGroups(groups []SkillGroup, perPage int) [][]SkillGroup {
	var total int
	for _, g := range groups {
		total += len(g.Entries)
	}
	if total <= perPage {
		return [][]SkillGroup{groups}
	}

	var pages [][]SkillGroup
	var currentPage []SkillGroup
	currentCount := 0

	for _, g := range groups {
		remaining := g.Entries
		for len(remaining) > 0 {
			space := perPage - currentCount
			if space <= 0 {
				if len(currentPage) > 0 {
					pages = append(pages, currentPage)
				}
				currentPage = nil
				currentCount = 0
				space = perPage
			}

			take := min(len(remaining), space)

			currentPage = append(currentPage, SkillGroup{
				Source:  g.Source,
				Entries: remaining[:take],
			})
			currentCount += take
			remaining = remaining[take:]

			if currentCount >= perPage {
				pages = append(pages, currentPage)
				currentPage = nil
				currentCount = 0
			}
		}
	}
	if len(currentPage) > 0 {
		pages = append(pages, currentPage)
	}
	return pages
}
