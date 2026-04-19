package session

import (
	"crypto/sha1"
	"strings"

	"github.com/google/uuid"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

// hotplexNamespace is the HotPlex namespace UUID (RFC 4122 §4.3).
// Using a fixed value ensures cross-environment consistency.
var hotplexNamespace = uuid.MustParse("urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// DeriveSessionKey generates a deterministic server-side session ID using UUIDv5.
// Same (ownerID, workerType, clientSessionID, workDir) always maps to the same session.
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID, workDir string) string {
	// UUIDv5 = SHA-1(namespace+name) — deterministic, no randomness.
	name := ownerID + "|" + string(wt) + "|" + clientSessionID + "|" + workDir
	id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
	return id.String()
}

// PlatformContext holds the platform-specific fields used to derive a platform session key.
// Each field is optional — only non-empty fields participate in the hash.
// ThreadTS is cross-platform: used by both Slack threads and Feishu chat threads.
type PlatformContext struct {
	Platform string
	// Slack fields
	TeamID    string
	ChannelID string
	ThreadTS  string
	// Feishu fields
	ChatID string
	// Universal
	UserID  string
	WorkDir string
}

// DerivePlatformSessionKey generates a deterministic UUIDv5 for a messaging platform session.
// Inputs are intentionally narrow: (ownerID, workerType, platformContext) — all platform identity
// fields are namespaced by platform type, so Feishu and Slack never collide even if raw IDs match.
// WorkDir is included so that the same conversation on different working directories produces
// different session IDs, preventing "Session ID already in use" errors when workDir changes.
//
// Canonical inputs per platform:
//   - Slack channel:  PlatformContext{Platform="slack", TeamID, ChannelID="C...", ThreadTS, UserID, WorkDir}
//   - Slack DM:      PlatformContext{Platform="slack", TeamID, ChannelID="D...", ThreadTS="", UserID, WorkDir}
//   - Feishu:        PlatformContext{Platform="feishu", ChatID, ThreadTS, UserID, WorkDir}
//   - Web:           caller should use DeriveSessionKey directly (it includes workDir + clientSessionID)
//
// Empty fields are excluded from the hash to ensure "no value" and "absent field"
// produce the same session for the same logical conversation.
func DerivePlatformSessionKey(ownerID string, wt worker.WorkerType, ctx PlatformContext) string {
	// Direct concatenation avoids heap-allocating a []string slice on every call.
	// For Slack: owner|wt|slack[|teamID][|channelID][|threadTS][|userID]
	// For Feishu: owner|wt|feishu[|chatID][|threadTS][|userID]
	var b strings.Builder
	b.Grow(64) // pre-allocate to avoid reallocations
	b.WriteString(ownerID)
	b.WriteByte('|')
	b.WriteString(string(wt))
	b.WriteByte('|')
	b.WriteString(ctx.Platform)
	if ctx.Platform == "slack" {
		if ctx.TeamID != "" {
			b.WriteByte('|')
			b.WriteString(ctx.TeamID)
		}
		if ctx.ChannelID != "" {
			b.WriteByte('|')
			b.WriteString(ctx.ChannelID)
		}
		if ctx.ThreadTS != "" {
			b.WriteByte('|')
			b.WriteString(ctx.ThreadTS)
		}
	} else if ctx.Platform == "feishu" {
		if ctx.ChatID != "" {
			b.WriteByte('|')
			b.WriteString(ctx.ChatID)
		}
		if ctx.ThreadTS != "" {
			b.WriteByte('|')
			b.WriteString(ctx.ThreadTS)
		}
	}
	if ctx.UserID != "" {
		b.WriteByte('|')
		b.WriteString(ctx.UserID)
	}
	if ctx.WorkDir != "" {
		b.WriteByte('|')
		b.WriteString(ctx.WorkDir)
	}
	name := b.String()
	id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
	return id.String()
}
