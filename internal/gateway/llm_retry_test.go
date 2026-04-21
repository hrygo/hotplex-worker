package gateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestLLMRetryController_ShouldRetry(t *testing.T) {
	log := slog.Default()

	makeCfg := func(enabled bool, patterns []string) config.AutoRetryConfig {
		return config.AutoRetryConfig{
			Enabled:    enabled,
			MaxRetries: 3,
			BaseDelay:  5 * time.Second,
			MaxDelay:   120 * time.Second,
			RetryInput: "继续",
			NotifyUser: true,
			Patterns:   patterns,
		}
	}

	t.Run("disabled", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(false, nil), log)
		ok, attempt := ctrl.ShouldRetry("s1", "something went wrong", nil)
		assert.False(t, ok)
		assert.Zero(t, attempt)
	})

	t.Run("no match", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		ok, attempt := ctrl.ShouldRetry("s1", "normal response", nil)
		assert.False(t, ok)
		assert.Zero(t, attempt)
	})

	t.Run("429 rate limit in turn text", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		ok, attempt := ctrl.ShouldRetry("s1", "API rate limit exceeded", nil)
		assert.True(t, ok)
		assert.Equal(t, 1, attempt)
	})

	t.Run("529 overloaded in error data", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		errData := &events.ErrorData{Message: "service overloaded"}
		ok, attempt := ctrl.ShouldRetry("s1", "", errData)
		assert.True(t, ok)
		assert.Equal(t, 1, attempt)
	})

	t.Run("network error code", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		errData := &events.ErrorData{Code: events.ErrorCode("ECONNREFUSED")}
		ok, attempt := ctrl.ShouldRetry("s1", "", errData)
		assert.True(t, ok)
		assert.Equal(t, 1, attempt)
	})

	t.Run("custom patterns", func(t *testing.T) {
		cfg := makeCfg(true, []string{`(?i)quota exceeded`})
		ctrl := NewLLMRetryController(cfg, log)
		ok, attempt := ctrl.ShouldRetry("s1", "you have quota exceeded today", nil)
		assert.True(t, ok)
		assert.Equal(t, 1, attempt)
	})

	t.Run("custom pattern no match", func(t *testing.T) {
		cfg := makeCfg(true, []string{`(?i)quota exceeded`})
		ctrl := NewLLMRetryController(cfg, log)
		ok, _ := ctrl.ShouldRetry("s1", "normal request", nil)
		assert.False(t, ok)
	})

	t.Run("retry count increments", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		// Attempt 1
		ok1, attempt1 := ctrl.ShouldRetry("s1", "rate limit error", nil)
		require.True(t, ok1)
		assert.Equal(t, 1, attempt1)
		// Attempt 2
		ok2, attempt2 := ctrl.ShouldRetry("s1", "rate limit error", nil)
		require.True(t, ok2)
		assert.Equal(t, 2, attempt2)
		// Attempt 3
		ok3, attempt3 := ctrl.ShouldRetry("s1", "rate limit error", nil)
		require.True(t, ok3)
		assert.Equal(t, 3, attempt3)
		// Attempt 4 - exhausted
		ok4, _ := ctrl.ShouldRetry("s1", "rate limit error", nil)
		assert.False(t, ok4)
	})

	t.Run("exhausted resets on new session", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		// Exhaust s1
		for i := 0; i < 4; i++ {
			ctrl.ShouldRetry("s1", "error 429", nil)
		}
		// s2 should start fresh at attempt 1
		ok, attempt := ctrl.ShouldRetry("s2", "error 429", nil)
		assert.True(t, ok)
		assert.Equal(t, 1, attempt)
	})

	t.Run("case insensitive", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(true, nil), log)
		ok, _ := ctrl.ShouldRetry("s1", "TOO MANY REQUESTS", nil)
		assert.True(t, ok)
	})
}

func TestLLMRetryController_Delay(t *testing.T) {
	log := slog.Default()

	makeCfg := func(base, max time.Duration) config.AutoRetryConfig {
		return config.AutoRetryConfig{
			Enabled:    true,
			MaxRetries: 5,
			BaseDelay:  base,
			MaxDelay:   max,
			RetryInput: "继续",
			NotifyUser: false,
		}
	}

	t.Run("attempt 1 uses base delay", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(10*time.Second, 120*time.Second), log)
		delay := ctrl.Delay(1)
		// 10s ± 25% jitter → [7.5s, 12.5s]
		assert.InDelta(t, 10*time.Second.Seconds(), delay.Seconds(), 3)
	})

	t.Run("attempt 2 doubles", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(10*time.Second, 120*time.Second), log)
		delay := ctrl.Delay(2)
		// 20s ± 25% jitter → [15s, 25s]
		assert.InDelta(t, 20*time.Second.Seconds(), delay.Seconds(), 6)
	})

	t.Run("attempt 3 quadruples", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(10*time.Second, 120*time.Second), log)
		delay := ctrl.Delay(3)
		// 40s ± 25% jitter → [30s, 50s]
		assert.InDelta(t, 40*time.Second.Seconds(), delay.Seconds(), 11)
	})

	t.Run("caps at max delay", func(t *testing.T) {
		ctrl := NewLLMRetryController(makeCfg(10*time.Second, 30*time.Second), log)
		// Attempt 4 would be 80s, capped at 30s ± 25%
		delay := ctrl.Delay(4)
		assert.InDelta(t, 30*time.Second.Seconds(), delay.Seconds(), 8)
		// Attempt 5 also capped
		delay5 := ctrl.Delay(5)
		assert.InDelta(t, 30*time.Second.Seconds(), delay5.Seconds(), 8)
	})
}

func TestLLMRetryController_RecordSuccess(t *testing.T) {
	log := slog.Default()
	cfg := config.AutoRetryConfig{
		Enabled:    true,
		MaxRetries: 3,
		BaseDelay:  5 * time.Second,
		MaxDelay:   120 * time.Second,
		RetryInput: "继续",
		NotifyUser: true,
	}
	ctrl := NewLLMRetryController(cfg, log)

	// Build up attempts
	ctrl.ShouldRetry("s1", "429 error", nil)
	ctrl.ShouldRetry("s1", "429 error", nil)
	ctrl.ShouldRetry("s1", "429 error", nil)

	// Record success resets counter
	ctrl.RecordSuccess("s1")

	// Next retry should be attempt 1 again
	ok, attempt := ctrl.ShouldRetry("s1", "429 error", nil)
	assert.True(t, ok)
	assert.Equal(t, 1, attempt)
}

func TestLLMRetryController_NotifyMessage(t *testing.T) {
	log := slog.Default()
	cfg := config.AutoRetryConfig{
		Enabled:    true,
		MaxRetries: 3,
		BaseDelay:  5 * time.Second,
		MaxDelay:   120 * time.Second,
		RetryInput: "继续",
		NotifyUser: true,
	}
	ctrl := NewLLMRetryController(cfg, log)

	msg := ctrl.NotifyMessage(2)
	assert.Contains(t, msg, "2/3")
	assert.Contains(t, msg, "🔄")
}

func TestLLMRetryController_ExhaustedMessage(t *testing.T) {
	log := slog.Default()
	cfg := config.AutoRetryConfig{
		Enabled:    true,
		MaxRetries: 3,
		BaseDelay:  5 * time.Second,
		MaxDelay:   120 * time.Second,
		RetryInput: "继续",
		NotifyUser: true,
	}
	ctrl := NewLLMRetryController(cfg, log)

	msg := ctrl.ExhaustedMessage()
	assert.Contains(t, msg, "3次")
	assert.Contains(t, msg, "⚠️")
}

func TestLLMRetryController_RetryInput(t *testing.T) {
	log := slog.Default()
	cfg := config.AutoRetryConfig{
		Enabled:    true,
		MaxRetries: 3,
		RetryInput: "please continue",
	}
	ctrl := NewLLMRetryController(cfg, log)
	assert.Equal(t, "please continue", ctrl.RetryInput())
}

func TestLLMRetryController_ShouldNotify(t *testing.T) {
	log := slog.Default()

	ctrlOn := NewLLMRetryController(config.AutoRetryConfig{Enabled: true, NotifyUser: true}, log)
	assert.True(t, ctrlOn.ShouldNotify())

	ctrlOff := NewLLMRetryController(config.AutoRetryConfig{Enabled: true, NotifyUser: false}, log)
	assert.False(t, ctrlOff.ShouldNotify())
}

func TestLLMRetryController_MaxRetries(t *testing.T) {
	log := slog.Default()
	ctrl := NewLLMRetryController(config.AutoRetryConfig{Enabled: true, MaxRetries: 5}, log)
	assert.Equal(t, 5, ctrl.MaxRetries())
}
