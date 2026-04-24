package messaging_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// mockPlatformConn captures WriteCtx calls for verification.
type mockPlatformConn struct {
	mu       sync.Mutex
	written  []*events.Envelope
	closed   bool
	writeErr error
}

func (m *mockPlatformConn) WriteCtx(_ context.Context, env *events.Envelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if m.writeErr != nil {
		return m.writeErr
	}
	m.written = append(m.written, env)
	return nil
}

func (m *mockPlatformConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockPlatformConn) Written() []*events.Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*events.Envelope, len(m.written))
	copy(result, m.written)
	return result
}

// TestPlatformConn_Interface verifies compile-time interface compliance.
func TestPlatformConn_Interface(t *testing.T) {
	t.Parallel()
	var _ messaging.PlatformConn = (*mockPlatformConn)(nil)
}

func TestPlatformConn_WriteAndClose(t *testing.T) {
	t.Parallel()

	pc := &mockPlatformConn{}
	env := &events.Envelope{SessionID: "test:1", Event: events.Event{Type: events.MessageDelta}}

	err := pc.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	require.Len(t, pc.Written(), 1)

	err = pc.Close()
	require.NoError(t, err)

	// After close, writes should still succeed (no-op on mock) but not record.
	err = pc.WriteCtx(context.Background(), env)
	require.NoError(t, err)
	require.Len(t, pc.Written(), 1) // unchanged
}
