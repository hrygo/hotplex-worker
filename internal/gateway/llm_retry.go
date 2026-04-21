package gateway

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Default retryable error patterns (compiled once at startup).
var defaultPatterns = []string{
	`(?i)(429|rate.?limit|too many requests)`,
	`(?i)(529|overloaded|service unavailable)`,
	`(?i)API Error.*reject`,
	`(?i)(network|connection.*reset|ECONNREFUSED|timeout|request failed)`,
	`(?i)(500|502|503|server error)`,
}

// LLMRetryController detects retryable errors and manages exponential backoff.
type LLMRetryController struct {
	config   config.AutoRetryConfig
	patterns []*regexp.Regexp
	mu       sync.Mutex
	attempts map[string]int // sessionID → retry count
	log      *slog.Logger
}

// NewLLMRetryController creates a controller from config.
func NewLLMRetryController(cfg config.AutoRetryConfig, log *slog.Logger) *LLMRetryController {
	cfg = cfg.Defaults()
	patterns := defaultPatterns
	if len(cfg.Patterns) > 0 {
		patterns = cfg.Patterns
	}
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		compiled[i] = regexp.MustCompile(p)
	}
	return &LLMRetryController{
		config:   cfg,
		patterns: compiled,
		attempts: make(map[string]int),
		log:      log.With("component", "llm_retry"),
	}
}

// ShouldRetry checks if the turn text or error event matches a retryable pattern.
// Returns (shouldRetry, currentAttempt). currentAttempt starts at 1.
func (c *LLMRetryController) ShouldRetry(sessionID, turnText string, errData *events.ErrorData) (bool, int) {
	if !c.config.Enabled {
		return false, 0
	}

	// Build error text from both Error event and accumulated output.
	text := turnText
	if errData != nil {
		if errData.Message != "" {
			text += "\n" + errData.Message
		}
		if errData.Code != "" {
			text += "\n" + string(errData.Code)
		}
	}

	matched := false
	for _, pat := range c.patterns {
		if pat.MatchString(text) {
			matched = true
			break
		}
	}
	if !matched {
		return false, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	attempt := c.attempts[sessionID] + 1
	if attempt > c.config.MaxRetries {
		c.log.Info("llm_retry: max retries exhausted", "session_id", sessionID, "max", c.config.MaxRetries)
		return false, 0
	}
	c.attempts[sessionID] = attempt
	c.log.Info("llm_retry: retry triggered", "session_id", sessionID, "attempt", attempt, "max", c.config.MaxRetries)
	return true, attempt
}

// Delay calculates exponential backoff with ±25% jitter for the given attempt.
func (c *LLMRetryController) Delay(attempt int) time.Duration {
	delay := float64(c.config.BaseDelay)
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > float64(c.config.MaxDelay) {
			delay = float64(c.config.MaxDelay)
			break
		}
	}
	// Add ±25% jitter using a simple deterministic spread.
	jitter := delay * 0.25
	delay += jitter * (-1 + 2*float64(attempt%2))
	return time.Duration(delay)
}

// RecordSuccess resets the retry counter for a session on successful turn completion.
func (c *LLMRetryController) RecordSuccess(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.attempts, sessionID)
}

// RetryInput returns the input text to send to the worker on retry.
func (c *LLMRetryController) RetryInput() string {
	return c.config.RetryInput
}

// ShouldNotify returns whether user notifications are enabled.
func (c *LLMRetryController) ShouldNotify() bool {
	return c.config.NotifyUser
}

// MaxRetries returns the configured max retry count.
func (c *LLMRetryController) MaxRetries() int {
	return c.config.MaxRetries
}

// NotifyMessage returns the user-facing notification message.
func (c *LLMRetryController) NotifyMessage(attempt int) string {
	return fmt.Sprintf("🔄 遇到临时错误，正在自动重试 (%d/%d)...", attempt, c.config.MaxRetries)
}

// ExhaustedMessage returns the message shown when all retries are exhausted.
func (c *LLMRetryController) ExhaustedMessage() string {
	return fmt.Sprintf("⚠️ 自动重试已耗尽 (%d次)，请手动发送「继续」或重新提问。", c.config.MaxRetries)
}
