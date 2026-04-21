package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// withRegistry saves the current registry state, runs fn, then restores it.
// This prevents test pollution when Register modifies the global registry.
func withRegistry(t *testing.T, fn func()) {
	t.Helper()
	orig := registry
	registry = make(map[WorkerType]Builder)
	fn()
	registry = orig
}

func TestRegister(t *testing.T) {
	// Registry is global shared state; disable parallel subtests to avoid races.

	t.Run("normal registration allows lookup", func(t *testing.T) {
		withRegistry(t, func() {
			Register(TypeClaudeCode, func() (Worker, error) {
				return nil, nil
			})
			b, ok := registry[TypeClaudeCode]
			require.True(t, ok, "type should be registered")
			require.NotNil(t, b)
		})
	})

	t.Run("nil builder triggers panic", func(t *testing.T) {
		withRegistry(t, func() {
			require.Panics(t, func() {
				Register(TypeOpenCodeSrv, nil)
			}, "Register with nil builder must panic")
		})
	})

	t.Run("duplicate registration triggers panic", func(t *testing.T) {
		withRegistry(t, func() {
			Register(TypePimon, func() (Worker, error) { return nil, nil })
			require.Panics(t, func() {
				Register(TypePimon, func() (Worker, error) { return nil, nil })
			}, "Register called twice for same type must panic")
		})
	})

	t.Run("registered type appears in RegisteredTypes", func(t *testing.T) {
		withRegistry(t, func() {
			Register(TypeOpenCodeSrv, func() (Worker, error) { return nil, nil })
			types := RegisteredTypes()
			found := false
			for _, t2 := range types {
				if t2 == TypeOpenCodeSrv {
					found = true
					break
				}
			}
			require.True(t, found, "registered type must appear in RegisteredTypes()")
		})
	})
}

func TestNewWorker(t *testing.T) {
	t.Run("known type returns worker instance with no error", func(t *testing.T) {
		withRegistry(t, func() {
			Register(TypeClaudeCode, func() (Worker, error) {
				return &registryTestWorker{}, nil
			})
			w, err := NewWorker(TypeClaudeCode)
			require.NoError(t, err)
			require.NotNil(t, w)
		})
	})

	t.Run("unknown type returns error containing unknown", func(t *testing.T) {
		withRegistry(t, func() {
			w, err := NewWorker("nonexistent_type")
			require.Error(t, err)
			require.Contains(t, err.Error(), "unknown")
			require.Nil(t, w)
		})
	})
}

func TestRegisteredTypes(t *testing.T) {
	t.Run("empty registry returns empty slice", func(t *testing.T) {
		withRegistry(t, func() {
			types := RegisteredTypes()
			require.Empty(t, types)
		})
	})

	t.Run("multiple registrations returns all types", func(t *testing.T) {
		withRegistry(t, func() {
			Register(TypeClaudeCode, func() (Worker, error) { return nil, nil })
			Register(TypeOpenCodeSrv, func() (Worker, error) { return nil, nil })
			Register(TypePimon, func() (Worker, error) { return nil, nil })

			types := RegisteredTypes()
			require.Len(t, types, 3)

			typeSet := make(map[WorkerType]bool)
			for _, t2 := range types {
				typeSet[t2] = true
			}
			require.True(t, typeSet[TypeClaudeCode])
			require.True(t, typeSet[TypeOpenCodeSrv])
			require.True(t, typeSet[TypePimon])
		})
	})
}

// registryTestWorker is a minimal Worker stub used only in TestNewWorker.
type registryTestWorker struct{}

var _ Worker = (*registryTestWorker)(nil)

func (*registryTestWorker) Start(context.Context, SessionInfo) error            { return nil }
func (*registryTestWorker) Input(context.Context, string, map[string]any) error { return nil }
func (*registryTestWorker) Resume(context.Context, SessionInfo) error           { return nil }
func (*registryTestWorker) Terminate(context.Context) error                     { return nil }
func (*registryTestWorker) Kill() error                                         { return nil }
func (*registryTestWorker) Wait() (int, error)                                  { return 0, nil }
func (*registryTestWorker) Conn() SessionConn                                   { return nil }
func (*registryTestWorker) Health() WorkerHealth                                { return WorkerHealth{} }
func (*registryTestWorker) LastIO() time.Time                                   { return time.Time{} }
func (*registryTestWorker) ResetContext(context.Context) error                  { return nil }
func (*registryTestWorker) Type() WorkerType                                    { return TypeClaudeCode }
func (*registryTestWorker) SupportsResume() bool                                { return false }
func (*registryTestWorker) SupportsStreaming() bool                             { return false }
func (*registryTestWorker) SupportsTools() bool                                 { return false }
func (*registryTestWorker) EnvWhitelist() []string                              { return nil }
func (*registryTestWorker) SessionStoreDir() string                             { return "" }
func (*registryTestWorker) MaxTurns() int                                       { return 0 }
func (*registryTestWorker) Modalities() []string                                { return nil }
