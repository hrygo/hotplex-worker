package events

func IntFloat(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func StrVal(v any) string {
	s, _ := v.(string)
	return s
}

func SliceVal(v any) []any {
	s, _ := v.([]any)
	return s
}

func MapContextUsageResponse(raw map[string]any) *ContextUsageData {
	if raw == nil {
		return &ContextUsageData{}
	}
	data := &ContextUsageData{
		TotalTokens: IntFloat(raw["totalTokens"]),
		MaxTokens:   IntFloat(raw["maxTokens"]),
		Percentage:  IntFloat(raw["percentage"]),
		Model:       StrVal(raw["model"]),
		MemoryFiles: IntFloat(raw["memoryFiles"]),
		MCPTools:    IntFloat(raw["mcpTools"]),
		Agents:      IntFloat(raw["agents"]),
	}
	for _, c := range SliceVal(raw["categories"]) {
		m, _ := c.(map[string]any)
		data.Categories = append(data.Categories, ContextCategory{
			Name:   StrVal(m["name"]),
			Tokens: IntFloat(m["tokens"]),
		})
	}
	if s, ok := raw["skills"].(map[string]any); ok {
		info := ContextSkillInfo{
			Total:    IntFloat(s["totalSkills"]),
			Included: IntFloat(s["includedSkills"]),
			Tokens:   IntFloat(s["tokens"]),
		}
		if details, ok := s["details"].([]any); ok {
			for _, d := range details {
				if m, ok := d.(map[string]any); ok {
					if name := StrVal(m["name"]); name != "" {
						info.Names = append(info.Names, name)
					}
				}
			}
		}
		data.Skills = info
	}
	return data
}

func MapMCPStatusResponse(raw map[string]any) *MCPStatusData {
	result := &MCPStatusData{}
	if raw == nil {
		return result
	}
	if servers, ok := raw["servers"].([]any); ok {
		for _, s := range servers {
			if m, ok := s.(map[string]any); ok {
				result.Servers = append(result.Servers, MCPServerInfo{
					Name:   StrVal(m["name"]),
					Status: StrVal(m["status"]),
				})
			}
		}
	}
	return result
}
