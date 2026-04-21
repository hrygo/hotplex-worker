package slack

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewGate(t *testing.T) {
	t.Parallel()
	g := NewGate(PolicyOpen, PolicyOpen, false, []string{"U1"}, []string{"DM1"}, []string{"GP1"})
	require.NotNil(t, g)
	require.True(t, g.allowFrom["U1"])
	require.True(t, g.allowDMFrom["DM1"])
	require.True(t, g.allowGroupFrom["GP1"])
}

func TestGate_Check_DM(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGate(tt.policy, PolicyOpen, false, tt.globalList, tt.dmList, nil)
			res := g.Check(ChannelIM, tt.userID, false)
			require.Equal(t, tt.allowed, res.Allowed)
			require.Equal(t, tt.reason, res.Reason)
		})
	}
}

func TestGate_Check_Group(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewGate(PolicyOpen, tt.policy, tt.requireM, tt.globalList, nil, tt.groupList)
			res := g.Check(ChannelGroup, tt.userID, tt.mentioned)
			require.Equal(t, tt.allowed, res.Allowed)
			require.Equal(t, tt.reason, res.Reason)
		})
	}
}

func TestGate_UpdateAllowFrom(t *testing.T) {
	t.Parallel()
	g := NewGate(PolicyAllowlist, PolicyAllowlist, false, []string{"U1"}, []string{"DM1"}, []string{"GP1"})

	// Initial check
	require.True(t, g.Check(ChannelIM, "U1", false).Allowed)
	require.True(t, g.Check(ChannelIM, "DM1", false).Allowed)
	require.False(t, g.Check(ChannelIM, "U2", false).Allowed)

	// Update
	g.UpdateAllowFrom([]string{"U2"}, []string{"DM2"}, []string{"GP2"})

	// New check
	require.False(t, g.Check(ChannelIM, "U1", false).Allowed)
	require.True(t, g.Check(ChannelIM, "U2", false).Allowed)
	require.False(t, g.Check(ChannelIM, "DM1", false).Allowed)
	require.True(t, g.Check(ChannelIM, "DM2", false).Allowed)
	require.True(t, g.Check(ChannelGroup, "GP2", false).Allowed)
}
