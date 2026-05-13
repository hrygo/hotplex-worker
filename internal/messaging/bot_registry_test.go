package messaging

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBotRegistry_RegisterAndGet(t *testing.T) {
	r := newBotRegistry()

	e := &BotEntry{Name: "test-bot", Platform: PlatformSlack, BotID: "U123", Status: BotStatusRunning}
	r.Register(e)

	got, ok := r.Get(PlatformSlack, "test-bot")
	require.True(t, ok)
	require.Equal(t, "U123", got.BotID)
	require.Equal(t, BotStatusRunning, got.Status)
}

func TestBotRegistry_GetNotFound(t *testing.T) {
	r := newBotRegistry()
	_, ok := r.Get(PlatformSlack, "nonexistent")
	require.False(t, ok)
}

func TestBotRegistry_Unregister(t *testing.T) {
	r := newBotRegistry()
	r.Register(&BotEntry{Name: "bot1", Platform: PlatformSlack})
	r.Unregister(PlatformSlack, "bot1")
	_, ok := r.Get(PlatformSlack, "bot1")
	require.False(t, ok)
}

func TestBotRegistry_ListAll(t *testing.T) {
	r := newBotRegistry()
	r.Register(&BotEntry{Name: "slack-bot", Platform: PlatformSlack})
	r.Register(&BotEntry{Name: "feishu-bot", Platform: PlatformFeishu})

	all := r.ListAll()
	require.Len(t, all, 2)
}

func TestBotRegistry_ListByPlatform(t *testing.T) {
	r := newBotRegistry()
	r.Register(&BotEntry{Name: "s1", Platform: PlatformSlack})
	r.Register(&BotEntry{Name: "s2", Platform: PlatformSlack})
	r.Register(&BotEntry{Name: "f1", Platform: PlatformFeishu})

	slackBots := r.ListByPlatform(PlatformSlack)
	require.Len(t, slackBots, 2)

	feishuBots := r.ListByPlatform(PlatformFeishu)
	require.Len(t, feishuBots, 1)
}

func TestBotRegistry_UpdateStatus(t *testing.T) {
	r := newBotRegistry()
	r.Register(&BotEntry{Name: "bot1", Platform: PlatformSlack, Status: BotStatusStarting})

	r.UpdateStatus(PlatformSlack, "bot1", BotStatusRunning, "U999")

	got, ok := r.Get(PlatformSlack, "bot1")
	require.True(t, ok)
	require.Equal(t, BotStatusRunning, got.Status)
	require.Equal(t, "U999", got.BotID)
}

func TestBotRegistry_Concurrent(t *testing.T) {
	r := newBotRegistry()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("bot-%d", i)
			r.Register(&BotEntry{Name: name, Platform: PlatformSlack})
			r.Get(PlatformSlack, name)
			r.ListAll()
		}(i)
	}
	wg.Wait()

	all := r.ListAll()
	require.Len(t, all, 100)
}
