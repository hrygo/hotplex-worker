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
// Returns (allowed, reason) where reason is non-empty when allowed is false.
func (g *Gate) Check(isDM bool, userID string, botMentioned bool) (bool, string) {
	if isDM {
		return g.checkDM(userID)
	}
	return g.checkGroup(userID, botMentioned)
}

func (g *Gate) checkDM(userID string) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.dmPolicy {
	case PolicyDisabled:
		return false, ReasonDMDisabled
	case PolicyAllowlist:
		if !g.allowFrom[userID] && !g.allowDMFrom[userID] {
			return false, ReasonNotInAllowlist
		}
	}
	return true, ""
}

func (g *Gate) checkGroup(userID string, botMentioned bool) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.groupPolicy {
	case PolicyDisabled:
		return false, ReasonGroupDisabled
	case PolicyAllowlist:
		if !g.allowFrom[userID] && !g.allowGroupFrom[userID] {
			return false, ReasonNotInAllowlist
		}
	}
	if g.requireMention && !botMentioned {
		return false, ReasonNoMention
	}
	return true, ""
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
