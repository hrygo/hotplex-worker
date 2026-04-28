package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSkillsCache_List(t *testing.T) {
	t.Parallel()

	t.Run("zero TTL delegates to inner", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{skills: []Skill{{Name: "test"}}}
		cache := NewSkillsCache(inner, 0)

		got, err := cache.List(context.Background(), "", "")
		require.NoError(t, err)
		require.Equal(t, []Skill{{Name: "test"}}, got)
		require.Equal(t, 1, inner.callCount)
	})

	t.Run("cache hit within TTL", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{skills: []Skill{{Name: "cached"}}}
		cache := NewSkillsCache(inner, time.Hour)

		// First call populates cache
		_, err := cache.List(context.Background(), "", "")
		require.NoError(t, err)

		// Second call should hit cache
		_, err = cache.List(context.Background(), "", "")
		require.NoError(t, err)
		require.Equal(t, 1, inner.callCount) // inner called only once
	})

	t.Run("cache miss after TTL expires", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{skills: []Skill{{Name: "fresh"}}}
		cache := NewSkillsCache(inner, time.Millisecond)

		_, err := cache.List(context.Background(), "", "")
		require.NoError(t, err)
		require.Equal(t, 1, inner.callCount)

		time.Sleep(10 * time.Millisecond)

		_, err = cache.List(context.Background(), "", "")
		require.NoError(t, err)
		require.Equal(t, 2, inner.callCount) // inner called again
	})

	t.Run("inner error propagates", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{err: context.DeadlineExceeded}
		cache := NewSkillsCache(inner, time.Hour)

		_, err := cache.List(context.Background(), "", "")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("concurrent access safe", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{skills: []Skill{{Name: "concurrent"}}}
		cache := NewSkillsCache(inner, time.Hour)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = cache.List(context.Background(), "", "")
			}()
		}
		wg.Wait()
		// Only one call to inner, rest hit cache
		require.Equal(t, 1, inner.callCount)
	})

}

func TestSkillsCache_TTL(t *testing.T) {
	t.Parallel()

	t.Run("negative TTL delegates", func(t *testing.T) {
		t.Parallel()
		inner := &mockSkillsLocator{skills: []Skill{{Name: "neg"}}}
		cache := NewSkillsCache(inner, -time.Second)

		_, err := cache.List(context.Background(), "", "")
		require.NoError(t, err)
		require.Equal(t, 1, inner.callCount)
	})
}

type mockSkillsLocator struct {
	skills    []Skill
	err       error
	callCount int
	mu        sync.Mutex
}

func (m *mockSkillsLocator) List(_ context.Context, _, _ string) ([]Skill, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()
	return m.skills, m.err
}
