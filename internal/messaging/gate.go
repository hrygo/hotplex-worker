package messaging

import "sync"

// Gate policy constants.
const (
	PolicyOpen      = "open"
	PolicyAllowlist = "allowlist"
	PolicyDisabled  = "disabled"

	// Rejection reasons.
	ReasonDMDisabled     = "dm_disabled"
	ReasonGroupDisabled  = "group_disabled"
	ReasonNotInAllowlist = "not_in_allowlist"
	ReasonNoMention      = "no_mention"
)

// GateResult holds the access decision.
type GateResult struct {
	Allowed bool
	Reason  string
}

// Gate controls access to the bot based on channel type and user identity.
type Gate struct {
	dmPolicy       string // open | allowlist | disabled
	groupPolicy    string // open | allowlist | disabled
	requireMention bool
	allowFrom      map[string]bool // global
	allowDMFrom    map[string]bool // dm
	allowGroupFrom map[string]bool // group

	mu sync.RWMutex
}

// NewGate creates a new access control gate.
func NewGate(dmPolicy, groupPolicy string, requireMention bool, allowFrom, allowDMFrom, allowGroupFrom []string) *Gate {
	return &Gate{
		dmPolicy:       dmPolicy,
		groupPolicy:    groupPolicy,
		requireMention: requireMention,
		allowFrom:      toSet(allowFrom),
		allowDMFrom:    toSet(allowDMFrom),
		allowGroupFrom: toSet(allowGroupFrom),
	}
}

// Check evaluates whether a message should be processed.
// channelType: platform-specific DM type (e.g., "im" for Slack, "p2p" for Feishu).
// The isDM parameter distinguishes DM from group traffic.
func (g *Gate) Check(isDM bool, userID string, botMentioned bool) *GateResult {
	if isDM {
		return g.checkDM(userID)
	}
	return g.checkGroup(userID, botMentioned)
}

func (g *Gate) checkDM(userID string) *GateResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.dmPolicy {
	case PolicyDisabled:
		return &GateResult{false, ReasonDMDisabled}
	case PolicyAllowlist:
		if !g.allowFrom[userID] && !g.allowDMFrom[userID] {
			return &GateResult{false, ReasonNotInAllowlist}
		}
	}
	return &GateResult{true, ""}
}

func (g *Gate) checkGroup(userID string, botMentioned bool) *GateResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.groupPolicy {
	case PolicyDisabled:
		return &GateResult{false, ReasonGroupDisabled}
	case PolicyAllowlist:
		if !g.allowFrom[userID] && !g.allowGroupFrom[userID] {
			return &GateResult{false, ReasonNotInAllowlist}
		}
	}
	if g.requireMention && !botMentioned {
		return &GateResult{false, ReasonNoMention}
	}
	return &GateResult{true, ""}
}

// UpdateAllowFrom replaces the allowlists with new sets of user IDs.
func (g *Gate) UpdateAllowFrom(allowFrom, allowDMFrom, allowGroupFrom []string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.allowFrom = toSet(allowFrom)
	g.allowDMFrom = toSet(allowDMFrom)
	g.allowGroupFrom = toSet(allowGroupFrom)
}

func toSet(ids []string) map[string]bool {
	s := make(map[string]bool, len(ids))
	for _, id := range ids {
		s[id] = true
	}
	return s
}
