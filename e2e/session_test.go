package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	client "github.com/hotplex/hotplex-go-client"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestE2E_ConnectAndInit(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, ack.SessionID)
			require.True(t, ack.State == client.StateCreated || ack.State == client.StateRunning,
				"unexpected state: %s", ack.State)
			require.Equal(t, events.Version, ack.ServerCaps.ProtocolVersion)
			require.Equal(t, wt.workerType, ack.ServerCaps.WorkerType)

			require.NoError(t, c.Close())
		})
	}
}

func TestE2E_SessionTerminate(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, ack.SessionID)

			err = c.SendControl(context.Background(), client.ControlActionTerminate)
			require.NoError(t, err)

			evts := collectEvents(t, c.Events(), 5*time.Second)
			require.True(t, hasEventType(evts, client.EventError), "expected error event after terminate")

			require.NoError(t, c.Close())
		})
	}
}

func TestE2E_SessionDelete(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			sessionID := ack.SessionID

			err = c.SendControl(context.Background(), client.ControlActionDelete)
			require.NoError(t, err)

			// Delete is async — poll until the session is removed.
			require.Eventually(t, func() bool {
				_, err := tg.sm.Get(sessionID)
				return err != nil
			}, 2*time.Second, 50*time.Millisecond, "session should be deleted")

			require.NoError(t, c.Close())
		})
	}
}

// TestE2E_SessionReset: after Connect, session is RUNNING (Bridge.StartSession
// transitions CREATED→RUNNING). Reset attempts RUNNING→RUNNING which is invalid.
func TestE2E_SessionReset(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, ack.SessionID)

			err = c.SendReset(context.Background(), "test reset")
			require.NoError(t, err)

			evts := collectEvents(t, c.Events(), 5*time.Second)
			require.True(t, hasEventType(evts, client.EventError), "expected error event for reset from RUNNING state")

			require.NoError(t, c.Close())
		})
	}
}

func TestE2E_SessionGC(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, ack.SessionID)

			err = c.SendGC(context.Background(), "test gc")
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				si, err := tg.sm.Get(ack.SessionID)
				if err != nil {
					return false
				}
				return si.State == client.StateTerminated
			}, 2*time.Second, 50*time.Millisecond, "session should be TERMINATED after GC")

			require.NoError(t, c.Close())
		})
	}
}

func TestE2E_ResumeSession(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)

			token := tg.generateToken("test-user", 5*time.Minute)

			c1, err := client.New(context.Background(),
				client.URL(tg.wsURL()),
				client.WorkerType(wt.workerType),
				client.AuthToken(token),
				client.APIKey("test-key"),
			)
			require.NoError(t, err)

			ack1, err := c1.Connect(context.Background())
			require.NoError(t, err)
			sessionID := ack1.SessionID
			require.NotEmpty(t, sessionID)

			err = c1.SendInput(context.Background(), "first message")
			require.NoError(t, err)
			_ = collectEvents(t, c1.Events(), 5*time.Second)

			// Close first connection — session goes to IDLE.
			require.NoError(t, c1.Close())

			// Wait for session to transition to IDLE.
			time.Sleep(200 * time.Millisecond)

			// Resume with same session ID.
			c2, err := client.New(context.Background(),
				client.URL(tg.wsURL()),
				client.WorkerType(wt.workerType),
				client.AuthToken(token),
				client.APIKey("test-key"),
				client.ClientSessionID(sessionID),
			)
			require.NoError(t, err)

			ack2, err := c2.Connect(context.Background())
			require.NoError(t, err)
			// Session ID may be derived differently via DeriveSessionKey.
			require.NotEmpty(t, ack2.SessionID)
			require.True(t, ack2.State == client.StateRunning || ack2.State == client.StateIdle || ack2.State == client.StateCreated,
				"unexpected resume state: %s", ack2.State)

			require.NoError(t, c2.Close())
		})
	}
}

func TestE2E_CloseGracefully(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		require.NoError(t, c.Close())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close() blocked for too long")
	}
}
