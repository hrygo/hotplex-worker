package feishu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

// ─── Gate access control tests ──────────────────────────────────────────────

func TestNewGate(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "open", false, []string{"ou_1", "ou_2"}, []string{"ou_dm"}, []string{"ou_group"})
	require.NotNil(t, g)
}

func TestNewGate_EmptyAllowlist(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "open", false, nil, nil, nil)
	require.NotNil(t, g)
}

// TC-4.1-1: dm_policy=disabled rejects all DMs
func TestGate_DM_Disabled(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("disabled", "open", false, nil, nil, nil)
	result := g.Check(true, "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonDMDisabled, result.Reason)
}

// TC-4.1-2: dm_policy=open allows all DMs
func TestGate_DM_Open(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "open", false, nil, nil, nil)
	result := g.Check(true, "ou_any", false)
	require.True(t, result.Allowed)
	require.Empty(t, result.Reason)
}

// TC-4.1-3: dm_policy=allowlist only allows whitelisted users
func TestGate_DM_Allowlist(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("allowlist", "open", false, []string{"ou_global"}, []string{"ou_dm"}, nil)
	require.True(t, g.Check(true, "ou_global", false).Allowed)
	require.True(t, g.Check(true, "ou_dm", false).Allowed)
	require.False(t, g.Check(true, "ou_group", false).Allowed)
	result := g.Check(true, "ou_denied", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonNotInAllowlist, result.Reason)
}

// TC-4.1-4: group_policy=disabled rejects all group messages
func TestGate_Group_Disabled(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "disabled", false, nil, nil, nil)
	result := g.Check(false, "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonGroupDisabled, result.Reason)
}

// TC-4.1-5: require_mention=true rejects without @bot
func TestGate_Group_NoMention(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "open", true, nil, nil, nil)
	result := g.Check(false, "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonNoMention, result.Reason)
}

// TC-4.1-6: require_mention=true allows with @bot
func TestGate_Group_WithMention(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "open", true, nil, nil, nil)
	result := g.Check(false, "ou_any", true)
	require.True(t, result.Allowed)
}

// TC-4.1-7: topic_group uses same policy as group
func TestGate_TopicGroup_UsesGroupPolicy(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "disabled", false, nil, nil, nil)
	result := g.Check(false, "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonGroupDisabled, result.Reason)
}

// TC-4.1-3 extended: group allowlist
func TestGate_Group_Allowlist(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "allowlist", false, []string{"ou_global"}, nil, []string{"ou_group"})
	require.True(t, g.Check(false, "ou_global", false).Allowed)
	require.True(t, g.Check(false, "ou_group", false).Allowed)
	require.False(t, g.Check(false, "ou_dm", false).Allowed)
	result := g.Check(false, "ou_denied", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonNotInAllowlist, result.Reason)
}

// TC-4.1-6 extended: group with mention + allowlist
func TestGate_Group_RequireMention_Allowlist(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("open", "allowlist", true, []string{"ou_allowed"}, nil, nil)
	result := g.Check(false, "ou_allowed", true)
	require.True(t, result.Allowed)

	result = g.Check(false, "ou_allowed", false)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonNoMention, result.Reason)

	result = g.Check(false, "ou_denied", true)
	require.False(t, result.Allowed)
	require.Equal(t, messaging.ReasonNotInAllowlist, result.Reason)
}

func TestGate_UpdateAllowFrom(t *testing.T) {
	t.Parallel()
	g := messaging.NewGate("allowlist", "allowlist", false, []string{"ou_1"}, []string{"ou_dm1"}, []string{"ou_gp1"})
	require.True(t, g.Check(true, "ou_1", false).Allowed)
	require.True(t, g.Check(true, "ou_dm1", false).Allowed)
	require.False(t, g.Check(true, "ou_dm2", false).Allowed)

	g.UpdateAllowFrom([]string{"ou_2"}, []string{"ou_dm2"}, []string{"ou_gp2"})
	require.False(t, g.Check(true, "ou_1", false).Allowed)
	require.True(t, g.Check(true, "ou_2", false).Allowed)
	require.True(t, g.Check(true, "ou_dm2", false).Allowed)
	require.True(t, g.Check(false, "ou_gp2", false).Allowed)
}

// ─── Dedup: message expiry tests ─────────────────────────────────────────────

// TC-4.4-1: messages older than 30 minutes are discarded
func TestIsMessageExpired_Old(t *testing.T) {
	t.Parallel()
	oldMs := (31 * 60 * 1000)
	require.True(t, IsMessageExpired(int64(oldMs)))
}

// TC-4.4-2: zero/negative create_time is not expired
func TestIsMessageExpired_Zero(t *testing.T) {
	t.Parallel()
	require.False(t, IsMessageExpired(0))
	require.False(t, IsMessageExpired(-1))
}

// TC-4.4-3: fresh messages are not expired
func TestIsMessageExpired_Fresh(t *testing.T) {
	t.Parallel()
	freshMs := -(1 * 60 * 1000)
	require.False(t, IsMessageExpired(int64(freshMs)))
}

// TC-4.3-3: expired entries are cleaned by Sweep
func TestDedup_Sweep(t *testing.T) {
	t.Parallel()
	d := messaging.NewDedup(100, 50*time.Millisecond)
	d.TryRecord("fresh1")
	d.TryRecord("fresh2")

	d.Sweep()
	require.Equal(t, 2, d.Len())
}

// TC-4.3-4: bounded capacity
func TestDedup_BoundedCapacity(t *testing.T) {
	t.Parallel()
	d := messaging.NewDedup(5, 12*time.Hour)
	for i := range 100 {
		d.TryRecord(string(rune('a' + i%26)))
	}
	require.LessOrEqual(t, d.Len(), 5)
}

// ─── Rate limiter tests ───────────────────────────────────────────────────────

func TestRateLimiter_AllowCardKit(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	cardID := "card_abc"

	require.True(t, r.AllowCardKit(cardID))
	require.False(t, r.AllowCardKit(cardID))
	require.False(t, r.AllowCardKit(cardID))
	require.True(t, r.AllowCardKit("card_xyz"))
}

func TestRateLimiter_AllowPatch(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	msgID := "msg_123"

	require.True(t, r.AllowPatch(msgID))
	require.False(t, r.AllowPatch(msgID))
	require.False(t, r.AllowPatch(msgID))
	require.True(t, r.AllowPatch("msg_456"))
}

func TestRateLimiter_IndependentLimits(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	require.True(t, r.AllowCardKit("card_1"))
	require.False(t, r.AllowCardKit("card_1"))
	require.True(t, r.AllowPatch("card_1"))
	require.False(t, r.AllowPatch("card_1"))
	require.True(t, r.AllowPatch("msg_1"))
}

func TestRateLimiter_Sweep(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()

	require.True(t, r.AllowCardKit("card_stale"))
	require.False(t, r.AllowCardKit("card_stale"))

	r.mu.Lock()
	r.lastCardKit["card_stale"] = time.Now().Add(-2 * time.Second)
	r.mu.Unlock()

	r.Sweep()

	r.mu.Lock()
	defer r.mu.Unlock()
	_, hasCard := r.lastCardKit["card_stale"]
	require.False(t, hasCard, "stale CardKit entry should be removed")
}
