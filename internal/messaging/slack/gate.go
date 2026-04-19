package slack

// Gate policy constants.
const (
	PolicyOpen      = "open"
	PolicyAllowlist = "allowlist"
	PolicyDisabled  = "disabled"

	// Channel types.
	ChannelIM    = "im"
	ChannelGroup = "channel"
	ChannelMPIM  = "mpim"

	// Rejection reasons.
	ReasonDMDisabled     = "dm_disabled"
	ReasonGroupDisabled  = "group_disabled"
	ReasonNotInAllowlist = "not_in_allowlist"
	ReasonNoMention      = "no_mention"
)

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
	if channelType == ChannelIM {
		switch g.dmPolicy {
		case PolicyDisabled:
			return &GateResult{false, ReasonDMDisabled}
		case PolicyAllowlist:
			if !g.allowFrom[userID] {
				return &GateResult{false, ReasonNotInAllowlist}
			}
		}
		return &GateResult{true, ""}
	}

	// Group/channel/MPIM
	switch g.groupPolicy {
	case PolicyDisabled:
		return &GateResult{false, ReasonGroupDisabled}
	case PolicyAllowlist:
		if !g.allowFrom[userID] {
			return &GateResult{false, ReasonNotInAllowlist}
		}
	}
	if g.requireMention && !botMentioned {
		return &GateResult{false, ReasonNoMention}
	}
	return &GateResult{true, ""}
}
