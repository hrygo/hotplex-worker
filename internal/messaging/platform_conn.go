package messaging

import (
	"context"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// PlatformConn models the write side of a platform connection.
// It is the minimal interface required by Hub.JoinPlatformSession.
type PlatformConn interface {
	// WriteCtx writes an AEP envelope to the platform and returns when the write
	// completes or the context is cancelled.
	WriteCtx(ctx context.Context, env *events.Envelope) error

	// Close permanently closes the connection and its associated goroutines.
	// It is called during shutdown with a deadline-bearing context.
	// Implementations that need cancellable cleanup should also implement
	// CloseCtx for better shutdown control.
	Close() error
}
