package session

import (
	"crypto/sha1"

	"github.com/google/uuid"
	"github.com/hotplex/hotplex-worker/internal/worker"
)

// hotplexNamespace is the HotPlex namespace UUID (RFC 4122 §4.3).
// Using a fixed value ensures cross-environment consistency.
var hotplexNamespace = uuid.MustParse("urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// DeriveSessionKey generates a deterministic server-side session ID using UUIDv5.
// Same (ownerID, workerType, clientSessionID, workDir) always maps to the same session.
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID string, workDir string) string {
	// UUIDv5 = SHA-1(namespace+name) — deterministic, no randomness.
	name := ownerID + "|" + string(wt) + "|" + clientSessionID + "|" + workDir
	id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
	return id.String()
}
