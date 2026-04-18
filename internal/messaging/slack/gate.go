package slack

// Gate controls access to the bot based on channel type and user identity.
type Gate struct {
	dmPolicy       string // open | allowlist | disabled
	groupPolicy    string // open | allowlist | disabled
	requireMention bool
	allowFrom      map[string]bool
}

// GateResult holds the access decision.
type GateResult struct {
	Allowed bool
	Reason  string
}

// NewGate creates a new access control gate.
func NewGate(dmPolicy, groupPolicy string, requireMention bool, allowFrom []string) *Gate {
	g := &Gate{
		dmPolicy:       dmPolicy,
		groupPolicy:    groupPolicy,
		requireMention: requireMention,
		allowFrom:      make(map[string]bool),
	}
	for _, u := range allowFrom {
		g.allowFrom[u] = true
	}
	return g
}

// Check evaluates whether a message should be processed.
func (g *Gate) Check(channelType, userID string, botMentioned bool) *GateResult {
	if channelType == "im" {
		switch g.dmPolicy {
		case "disabled":
			return &GateResult{false, "dm_disabled"}
		case "allowlist":
			if !g.allowFrom[userID] {
				return &GateResult{false, "not_in_allowlist"}
			}
		}
		return &GateResult{true, ""}
	}

	// Group/channel/MPIM
	switch g.groupPolicy {
	case "disabled":
		return &GateResult{false, "group_disabled"}
	case "allowlist":
		if !g.allowFrom[userID] {
			return &GateResult{false, "not_in_allowlist"}
		}
	}
	if g.requireMention && !botMentioned {
		return &GateResult{false, "no_mention"}
	}
	return &GateResult{true, ""}
}
