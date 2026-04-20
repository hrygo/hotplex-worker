package slack

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewGate(t *testing.T) {
	g := NewGate(PolicyOpen, PolicyOpen, false, []string{"U1"}, []string{"DM1"}, []string{"GP1"})
	require.NotNil(t, g)
	require.True(t, g.allowFrom["U1"])
	require.True(t, g.allowDMFrom["DM1"])
	require.True(t, g.allowGroupFrom["GP1"])
}

func TestGate_Check_DM(t *testing.T) {
	tests := []struct {
		name       string
		policy     string
		userID     string
		allowed    bool
		reason     string
		globalList []string
		dmList     []string
	}{
		{
			name:    "Disabled",
			policy:  PolicyDisabled,
			userID:  "U1",
			allowed: false,
			reason:  ReasonDMDisabled,
		},
		{
			name:    "Open",
			policy:  PolicyOpen,
			userID:  "U1",
			allowed: true,
		},
		{
			name:       "Allowlist_Global",
			policy:     PolicyAllowlist,
			userID:     "U_ADMIN",
			allowed:    true,
			globalList: []string{"U_ADMIN"},
		},
		{
			name:    "Allowlist_DM_Specific",
			policy:  PolicyAllowlist,
			userID:  "U_DM",
			allowed: true,
			dmList:  []string{"U_DM"},
		},
		{
			name:       "Allowlist_Denied",
			policy:     PolicyAllowlist,
			userID:     "U_EVIL",
			allowed:    false,
			reason:     ReasonNotInAllowlist,
			globalList: []string{"U_ADMIN"},
			dmList:     []string{"U_DM"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGate(tt.policy, PolicyOpen, false, tt.globalList, tt.dmList, nil)
			res := g.Check(ChannelIM, tt.userID, false)
			require.Equal(t, tt.allowed, res.Allowed)
			require.Equal(t, tt.reason, res.Reason)
		})
	}
}

func TestGate_Check_Group(t *testing.T) {
	tests := []struct {
		name       string
		policy     string
		userID     string
		mentioned  bool
		requireM   bool
		allowed    bool
		reason     string
		globalList []string
		groupList  []string
	}{
		{
			name:    "Disabled",
			policy:  PolicyDisabled,
			userID:  "U1",
			allowed: false,
			reason:  ReasonGroupDisabled,
		},
		{
			name:    "Open_NoMention_NotRequired",
			policy:  PolicyOpen,
			userID:  "U1",
			allowed: true,
		},
		{
			name:     "Open_NoMention_Required",
			policy:   PolicyOpen,
			userID:   "U1",
			requireM: true,
			allowed:  false,
			reason:   ReasonNoMention,
		},
		{
			name:      "Open_Mention_Required",
			policy:    PolicyOpen,
			userID:    "U1",
			mentioned: true,
			requireM:  true,
			allowed:   true,
		},
		{
			name:       "Allowlist_Global",
			policy:     PolicyAllowlist,
			userID:     "U_ADMIN",
			allowed:    true,
			globalList: []string{"U_ADMIN"},
		},
		{
			name:      "Allowlist_Group_Specific",
			policy:    PolicyAllowlist,
			userID:    "U_GP",
			allowed:   true,
			groupList: []string{"U_GP"},
		},
		{
			name:      "Allowlist_Denied",
			policy:    PolicyAllowlist,
			userID:    "U_DM_ONLY",
			allowed:   false,
			reason:    ReasonNotInAllowlist,
			groupList: []string{"U_GP"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGate(PolicyOpen, tt.policy, tt.requireM, tt.globalList, nil, tt.groupList)
			res := g.Check(ChannelGroup, tt.userID, tt.mentioned)
			require.Equal(t, tt.allowed, res.Allowed)
			require.Equal(t, tt.reason, res.Reason)
		})
	}
}
