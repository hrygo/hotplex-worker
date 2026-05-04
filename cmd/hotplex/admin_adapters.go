package main

import (
	"context"
	"errors"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

type sessionManagerAdapter struct {
	sm *session.Manager
}

func (a *sessionManagerAdapter) Stats() (int, int, int) {
	return a.sm.Stats()
}

func (a *sessionManagerAdapter) List(ctx context.Context, userID, platform string, limit, offset int) ([]any, error) {
	sessions, err := a.sm.List(ctx, userID, platform, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]any, len(sessions))
	for i, s := range sessions {
		result[i] = s
	}
	return result, nil
}

func (a *sessionManagerAdapter) Get(id string) (any, error) {
	return a.sm.Get(id)
}

func (a *sessionManagerAdapter) Delete(ctx context.Context, id string) error {
	return a.sm.Delete(ctx, id)
}

func (a *sessionManagerAdapter) WorkerHealthStatuses() []worker.WorkerHealth {
	return a.sm.WorkerHealthStatuses()
}

func (a *sessionManagerAdapter) DebugSnapshot(id string) (admin.DebugSessionSnapshot, bool) {
	snap, ok := a.sm.DebugSnapshot(id)
	if !ok {
		return admin.DebugSessionSnapshot{}, false
	}
	return admin.DebugSessionSnapshot{
		TurnCount:    snap.TurnCount,
		WorkerHealth: snap.WorkerHealth,
		HasWorker:    snap.HasWorker,
	}, true
}

func (a *sessionManagerAdapter) Transition(ctx context.Context, id string, to events.SessionState) error {
	return a.sm.Transition(ctx, id, to)
}

func (a *sessionManagerAdapter) DeletePhysical(ctx context.Context, id string) error {
	return a.sm.DeletePhysical(ctx, id)
}

func (a *sessionManagerAdapter) ResetExpiry(ctx context.Context, id string) error {
	return a.sm.ResetExpiry(ctx, id)
}

type hubAdapter struct {
	hub *gateway.Hub
}

func (a *hubAdapter) ConnectionsOpen() int {
	return a.hub.ConnectionsOpen()
}

func (a *hubAdapter) NextSeqPeek(sessionID string) int64 {
	return a.hub.NextSeqPeek(sessionID)
}

type turnsStoreAdapter struct {
	es *eventstore.SQLiteStore
}

func (a *turnsStoreAdapter) TurnStats(ctx context.Context, sessionID string) (*eventstore.TurnStats, error) {
	return a.es.QueryTurnStats(ctx, sessionID)
}

type bridgeAdapter struct {
	bridge *gateway.Bridge
}

func (a *bridgeAdapter) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string, title string) error {
	return a.bridge.StartSession(ctx, id, userID, botID, wt, allowedTools, workDir, platform, platformKey, title)
}

type configAdapter struct {
	cfgStore *config.ConfigStore
}

func (a *configAdapter) Get() *config.Config {
	return a.cfgStore.Load()
}

type configWatcherAdapter struct {
	watcher *config.Watcher
}

func (a *configWatcherAdapter) Rollback(version int) (*config.Config, int, error) {
	if a.watcher == nil {
		return nil, -1, errors.New("config watcher is nil")
	}
	return a.watcher.Rollback(version)
}
