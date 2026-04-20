// Package feishu provides access control (gate) for Feishu platform messages.
package feishu

import (
	"sync"
)

// GateResult represents the outcome of an access control check.
type GateResult struct {
	Allowed bool
	Reason  string
}

// Gate enforces access control policies for DM and group messages.
type Gate struct {
	dmPolicy       string          // open | allowlist | disabled
	groupPolicy    string          // open | allowlist | disabled
	requireMention bool            // in groups, bot must be @mentioned
	allowFrom      map[string]bool // global allowlist
	allowDMFrom    map[string]bool // dm-only allowlist
	allowGroupFrom map[string]bool // group-only allowlist

	mu sync.RWMutex
}

// NewGate creates a Gate from the given FeishuConfig access control settings.
func NewGate(dmPolicy, groupPolicy string, requireMention bool, allowFrom, allowDMFrom, allowGroupFrom []string) *Gate {
	af := make(map[string]bool, len(allowFrom))
	for _, id := range allowFrom {
		af[id] = true
	}
	adm := make(map[string]bool, len(allowDMFrom))
	for _, id := range allowDMFrom {
		adm[id] = true
	}
	agp := make(map[string]bool, len(allowGroupFrom))
	for _, id := range allowGroupFrom {
		agp[id] = true
	}
	return &Gate{
		dmPolicy:       dmPolicy,
		groupPolicy:    groupPolicy,
		requireMention: requireMention,
		allowFrom:      af,
		allowDMFrom:    adm,
		allowGroupFrom: agp,
	}
}

// Check evaluates whether a message should be allowed based on chat type,
// user identity, and mention status.
//
// chatType: "p2p" (DM), "group", or "topic_group".
// userID: the sender's open_id.
// botMentioned: true if the bot was @mentioned in the message.
func (g *Gate) Check(chatType, userID string, botMentioned bool) *GateResult {
	switch chatType {
	case "p2p":
		return g.checkDM(userID)
	default:
		// "group" and "topic_group" use group policy.
		return g.checkGroup(userID, botMentioned)
	}
}

func (g *Gate) checkDM(userID string) *GateResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.dmPolicy {
	case "disabled":
		return &GateResult{Allowed: false, Reason: "dm_disabled"}
	case "allowlist":
		// Check both global and DM-specific whitelists.
		if !g.allowFrom[userID] && !g.allowDMFrom[userID] {
			return &GateResult{Allowed: false, Reason: "not_in_allowlist"}
		}
	}
	// "open" or "pairing" → allowed.
	return &GateResult{Allowed: true}
}

func (g *Gate) checkGroup(userID string, botMentioned bool) *GateResult {
	g.mu.RLock()
	defer g.mu.RUnlock()

	switch g.groupPolicy {
	case "disabled":
		return &GateResult{Allowed: false, Reason: "group_disabled"}
	case "allowlist":
		// Check both global and group-specific whitelists.
		if !g.allowFrom[userID] && !g.allowGroupFrom[userID] {
			return &GateResult{Allowed: false, Reason: "not_in_allowlist"}
		}
	}
	if g.requireMention && !botMentioned {
		return &GateResult{Allowed: false, Reason: "no_mention"}
	}
	return &GateResult{Allowed: true}
}

// UpdateAllowFrom replaces the allowlists with new sets of user IDs.
func (g *Gate) UpdateAllowFrom(allowFrom, allowDMFrom, allowGroupFrom []string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	af := make(map[string]bool, len(allowFrom))
	for _, id := range allowFrom {
		af[id] = true
	}
	adm := make(map[string]bool, len(allowDMFrom))
	for _, id := range allowDMFrom {
		adm[id] = true
	}
	agp := make(map[string]bool, len(allowGroupFrom))
	for _, id := range allowGroupFrom {
		agp[id] = true
	}

	g.allowFrom = af
	g.allowDMFrom = adm
	g.allowGroupFrom = agp
}
