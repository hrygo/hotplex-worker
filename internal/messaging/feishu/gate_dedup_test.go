package feishu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── Gate access control tests ──────────────────────────────────────────────

func TestNewGate(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", false, []string{"ou_1", "ou_2"})
	require.NotNil(t, g)
	require.True(t, g.allowFrom["ou_1"])
	require.True(t, g.allowFrom["ou_2"])
}

func TestNewGate_EmptyAllowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", false, nil)
	require.NotNil(t, g)
	require.Len(t, g.allowFrom, 0)
}

// TC-4.1-1: dm_policy=disabled 拒绝所有 DM
func TestGate_DM_Disabled(t *testing.T) {
	t.Parallel()
	g := NewGate("disabled", "open", false, nil)
	result := g.Check("p2p", "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, "dm_disabled", result.Reason)
}

// TC-4.1-2: dm_policy=open 允许所有 DM
func TestGate_DM_Open(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", false, nil)
	result := g.Check("p2p", "ou_any", false)
	require.True(t, result.Allowed)
	require.Empty(t, result.Reason)
}

// TC-4.1-3: dm_policy=allowlist 仅允许白名单用户 DM
func TestGate_DM_Allowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("allowlist", "open", false, []string{"ou_allowed"})
	result := g.Check("p2p", "ou_allowed", false)
	require.True(t, result.Allowed)

	result = g.Check("p2p", "ou_denied", false)
	require.False(t, result.Allowed)
	require.Equal(t, "not_in_allowlist", result.Reason)
}

// TC-4.1-4: group_policy=disabled 拒绝所有群消息
func TestGate_Group_Disabled(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "disabled", false, nil)
	result := g.Check("group", "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, "group_disabled", result.Reason)
}

// TC-4.1-5: require_mention=true 且未 @bot 时拒绝
func TestGate_Group_NoMention(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", true, nil)
	result := g.Check("group", "ou_any", false) // bot not mentioned
	require.False(t, result.Allowed)
	require.Equal(t, "no_mention", result.Reason)
}

// TC-4.1-6: require_mention=true 且已 @bot 时允许
func TestGate_Group_WithMention(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", true, nil)
	result := g.Check("group", "ou_any", true) // bot mentioned
	require.True(t, result.Allowed)
}

// TC-4.1-7: topic_group 与 group 策略一致
func TestGate_TopicGroup_UsesGroupPolicy(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "disabled", false, nil)
	result := g.Check("topic_group", "ou_any", false)
	require.False(t, result.Allowed)
	require.Equal(t, "group_disabled", result.Reason)
}

// TC-4.1-3 extended: group allowlist
func TestGate_Group_Allowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "allowlist", false, []string{"ou_allowed"})
	result := g.Check("group", "ou_allowed", false)
	require.True(t, result.Allowed)

	result = g.Check("group", "ou_denied", false)
	require.False(t, result.Allowed)
	require.Equal(t, "not_in_allowlist", result.Reason)
}

// TC-4.1-6 extended: group with mention + allowlist
func TestGate_Group_RequireMention_Allowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "allowlist", true, []string{"ou_allowed"})
	result := g.Check("group", "ou_allowed", true)
	require.True(t, result.Allowed)

	result = g.Check("group", "ou_allowed", false)
	require.False(t, result.Allowed)
	require.Equal(t, "no_mention", result.Reason)

	result = g.Check("group", "ou_denied", true)
	require.False(t, result.Allowed)
	require.Equal(t, "not_in_allowlist", result.Reason)
}

func TestGate_UpdateAllowFrom(t *testing.T) {
	t.Parallel()
	g := NewGate("allowlist", "allowlist", false, []string{"ou_1"})
	require.True(t, g.allowFrom["ou_1"])
	require.False(t, g.allowFrom["ou_2"])

	g.UpdateAllowFrom([]string{"ou_2", "ou_3"})
	require.False(t, g.allowFrom["ou_1"])
	require.True(t, g.allowFrom["ou_2"])
	require.True(t, g.allowFrom["ou_3"])
}

// ─── Dedup: message expiry tests ─────────────────────────────────────────────

// TC-4.4-1: 超过 30 分钟的旧消息被丢弃
func TestIsMessageExpired_Old(t *testing.T) {
	t.Parallel()
	// 31 minutes ago in milliseconds
	oldMs := (31 * 60 * 1000)
	require.True(t, IsMessageExpired(int64(oldMs)))
}

// TC-4.4-2: create_time 为 nil 时不丢弃 (we test zero value)
func TestIsMessageExpired_Zero(t *testing.T) {
	t.Parallel()
	require.False(t, IsMessageExpired(0))
	require.False(t, IsMessageExpired(-1))
}

// TC-4.4-3: 新鲜消息正常处理
func TestIsMessageExpired_Fresh(t *testing.T) {
	t.Parallel()
	// 1 minute ago
	freshMs := -(1 * 60 * 1000)
	require.False(t, IsMessageExpired(int64(freshMs)))
}

// TC-4.3-3: 过期条目被定期清理
func TestDedup_Sweep(t *testing.T) {
	t.Parallel()
	d := NewDedup(100, 50*1000*1000) // 50ms TTL for testing
	d.TryRecord("fresh1")
	d.TryRecord("fresh2")

	// Immediately sweep — both should still be valid.
	d.Sweep()
	d.mu.Lock()
	fresh1 := d.entries["fresh1"]
	fresh2 := d.entries["fresh2"]
	d.mu.Unlock()
	require.True(t, fresh1.IsZero() == false, "fresh1 should be present")
	require.True(t, fresh2.IsZero() == false, "fresh2 should be present")
}

// TC-4.3-4: 无容量无限增长 (bounded growth)
func TestDedup_BoundedCapacity(t *testing.T) {
	t.Parallel()
	d := NewDedup(5, 12*60*60*1e9)
	for i := range 100 {
		d.TryRecord(string(rune('a' + i%26)))
	}
	d.mu.Lock()
	size := len(d.entries)
	d.mu.Unlock()
	require.LessOrEqual(t, size, 5)
}

// ─── Rate limiter tests ───────────────────────────────────────────────────────

// TC-4.2-1: CardKit 同一卡片 100ms 内只允许 1 次
func TestRateLimiter_AllowCardKit(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	cardID := "card_abc"

	// First call should be allowed.
	require.True(t, r.AllowCardKit(cardID))

	// Immediate second call should be blocked.
	require.False(t, r.AllowCardKit(cardID))
	require.False(t, r.AllowCardKit(cardID))

	// Different card should be allowed.
	require.True(t, r.AllowCardKit("card_xyz"))
}

// TC-4.2-2: IM patch 同一消息 1500ms 内只允许 1 次
func TestRateLimiter_AllowPatch(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	msgID := "msg_123"

	require.True(t, r.AllowPatch(msgID))
	require.False(t, r.AllowPatch(msgID))
	require.False(t, r.AllowPatch(msgID))

	require.True(t, r.AllowPatch("msg_456"))
}

// TC-4.2-3: 不同卡片/消息独立限流
func TestRateLimiter_IndependentLimits(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()
	// CardKit and IM patch use separate maps, so a card ID used for CardKit
	// does not affect its use as an IM patch ID.
	require.True(t, r.AllowCardKit("card_1"))
	require.False(t, r.AllowCardKit("card_1")) // blocked - same resource
	require.True(t, r.AllowPatch("card_1"))    // independent - IM patch has its own counter
	require.False(t, r.AllowPatch("card_1"))   // blocked - IM patch same resource
	require.True(t, r.AllowPatch("msg_1"))
}

// Test: RateLimiter Sweep removes stale entries.
func TestRateLimiter_Sweep(t *testing.T) {
	t.Parallel()
	r := NewFeishuRateLimiter()

	// Use a card to populate the map.
	require.True(t, r.AllowCardKit("card_stale"))
	require.False(t, r.AllowCardKit("card_stale"))

	// Manually age the entry by setting it far in the past.
	// The sweep threshold is 10x the rate limit = 1 second for CardKit.
	r.mu.Lock()
	r.lastCardKit["card_stale"] = time.Now().Add(-2 * time.Second)
	r.mu.Unlock()

	// Sweep should remove the stale entry.
	r.Sweep()

	// After sweep, the card should be allowed again.
	r.mu.Lock()
	defer r.mu.Unlock()
	_, hasCard := r.lastCardKit["card_stale"]
	require.False(t, hasCard, "stale CardKit entry should be removed")
}
