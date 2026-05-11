package cron

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

// mockAttachedRouter implements AttachedSessionRouter for testing.
type mockAttachedRouter struct {
	info        *session.SessionInfo
	injectErr   error
	resumeErr   error
	injectCalls int
	resumeCalls int
}

func (m *mockAttachedRouter) GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error) {
	if m.info == nil {
		return nil, fmt.Errorf("not found")
	}
	return m.info, nil
}

func (m *mockAttachedRouter) InjectInput(ctx context.Context, sessionID string, prompt string, metadata map[string]any) error {
	m.injectCalls++
	return m.injectErr
}

func (m *mockAttachedRouter) ResumeAndInput(ctx context.Context, sessionID string, workDir string, prompt string, metadata map[string]any) error {
	m.resumeCalls++
	return m.resumeErr
}

func TestCallbackHandler_Running(t *testing.T) {
	router := &mockAttachedRouter{info: &session.SessionInfo{State: events.StateRunning}}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test1",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check result",
			TargetSessionID: "sess_123",
		},
	}

	err := h.Execute(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, 1, router.injectCalls)
	require.Equal(t, 0, router.resumeCalls)
}

func TestCallbackHandler_Idle(t *testing.T) {
	router := &mockAttachedRouter{info: &session.SessionInfo{State: events.StateIdle, WorkDir: "/tmp"}}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test2",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "continue",
			TargetSessionID: "sess_456",
		},
	}

	err := h.Execute(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, 0, router.injectCalls)
	require.Equal(t, 1, router.resumeCalls)
}

func TestCallbackHandler_Terminated(t *testing.T) {
	router := &mockAttachedRouter{info: &session.SessionInfo{State: events.StateTerminated}}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test3",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "resume",
			TargetSessionID: "sess_789",
		},
	}

	err := h.Execute(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, 1, router.resumeCalls)
}

func TestCallbackHandler_Deleted(t *testing.T) {
	router := &mockAttachedRouter{info: &session.SessionInfo{State: events.StateDeleted}}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test4",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check",
			TargetSessionID: "sess_del",
		},
	}

	err := h.Execute(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "deleted")
}

func TestCallbackHandler_Created(t *testing.T) {
	router := &mockAttachedRouter{info: &session.SessionInfo{State: events.StateCreated}}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test5",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check",
			TargetSessionID: "sess_new",
		},
	}

	err := h.Execute(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "CREATED")
}

func TestCallbackHandler_NotFound(t *testing.T) {
	router := &mockAttachedRouter{info: nil}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test6",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check",
			TargetSessionID: "sess_missing",
		},
	}

	err := h.Execute(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestCallbackHandler_InjectError(t *testing.T) {
	router := &mockAttachedRouter{
		info:      &session.SessionInfo{State: events.StateRunning},
		injectErr: fmt.Errorf("worker gone"),
	}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test7",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check",
			TargetSessionID: "sess_inj",
		},
	}

	err := h.Execute(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "inject")
}

func TestCallbackHandler_ResumeError(t *testing.T) {
	router := &mockAttachedRouter{
		info:      &session.SessionInfo{State: events.StateIdle},
		resumeErr: fmt.Errorf("resume failed"),
	}
	h := NewAttachedSessionHandler(slog.Default(), router)

	job := &CronJob{
		ID:   "cron_test8",
		Name: "test-callback",
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "check",
			TargetSessionID: "sess_res",
		},
	}

	err := h.Execute(context.Background(), job)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resume")
}

func TestCleanupForSession(t *testing.T) {
	s := newTestScheduler(t)

	targetSession := "sess_target"
	for i := 0; i < 3; i++ {
		job := testCallbackJob(fmt.Sprintf("callback-%d", i), targetSession)
		require.NoError(t, s.CreateJob(context.Background(), job))
	}

	for i := 0; i < 2; i++ {
		job := testRecurringJob(fmt.Sprintf("isolated-%d", i), "do stuff")
		require.NoError(t, s.CreateJob(context.Background(), job))
	}

	s.CleanupForSession(targetSession)

	jobs, err := s.ListJobs(context.Background())
	require.NoError(t, err)

	var attached, isolated int
	for _, j := range jobs {
		if j.Payload.Kind == PayloadAttachedSession {
			attached++
		} else {
			isolated++
		}
	}
	require.Equal(t, 0, attached, "attached jobs should be cleaned up")
	require.Equal(t, 2, isolated, "isolated jobs should remain")
}

func TestCleanupForSession_NoCallbackJobs(t *testing.T) {
	s := newTestScheduler(t)

	for i := 0; i < 2; i++ {
		job := testRecurringJob(fmt.Sprintf("isolated-%d", i), "do stuff")
		require.NoError(t, s.CreateJob(context.Background(), job))
	}

	s.CleanupForSession("nonexistent_session")

	jobs, err := s.ListJobs(context.Background())
	require.NoError(t, err)
	require.Len(t, jobs, 2)
}

func testCallbackJob(name, targetSessionID string) *CronJob {
	return &CronJob{
		Name:     name,
		OwnerID:  "user1",
		BotID:    "bot1",
		Schedule: CronSchedule{Kind: ScheduleAt, At: "2027-01-01T00:00:00Z"},
		Payload: CronPayload{
			Kind:            PayloadAttachedSession,
			Message:         "follow up",
			TargetSessionID: targetSessionID,
		},
	}
}
