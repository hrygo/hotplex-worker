package gateway

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// Default retryable error patterns (compiled once at startup).
var defaultPatterns = []string{
	`(?i)(429|rate.?limit|too many requests)`,
	`(?i)(529|overloaded|service unavailable)`,
	`(?i)API Error.*reject`,
	`(?i)(network|connection.*reset|ECONNREFUSED|timeout|request failed)`,
	`(?i)(500|502|503|server error)`,
	`(?i)(INTERNAL_ERROR|internal error)`,
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
	patterns := make([]string, 0, len(defaultPatterns)+len(cfg.Patterns))
	patterns = append(patterns, defaultPatterns...)
	patterns = append(patterns, cfg.Patterns...)
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

// ShouldRetry checks if the error event matches a retryable pattern.
// The turnText parameter is accepted for backwards compatibility but is not used
// for matching — only actual Error events (errData) should trigger retries.
// Matching on accumulated output would cause false positives since Claude Code
// output may legitimately contain strings like "500" or "INTERNAL_ERROR"
// (e.g., in code comments, JSON data, error messages).
func (c *LLMRetryController) ShouldRetry(sessionID string, errData *events.ErrorData) (bool, int) {
	if !c.config.Enabled {
		return false, 0
	}
	if errData == nil {
		return false, 0
	}

	// Match only against the actual error event — code and message.
	text := errData.Message
	if errData.Code != "" {
		text += "\n" + string(errData.Code)
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

// UpdateConfig dynamically replaces the retry configuration.
// Existing per-session attempt counters are preserved.
func (c *LLMRetryController) UpdateConfig(cfg config.AutoRetryConfig) {
	cfg = cfg.Defaults()
	patterns := make([]string, 0, len(defaultPatterns)+len(cfg.Patterns))
	patterns = append(patterns, defaultPatterns...)
	patterns = append(patterns, cfg.Patterns...)
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			c.log.Warn("llm_retry: invalid pattern on reload, skipping", "pattern", p, "err", err)
			continue
		}
		compiled = append(compiled, re)
	}

	c.mu.Lock()
	c.config = cfg
	c.patterns = compiled
	c.mu.Unlock()

	c.log.Info("llm_retry: config updated",
		"enabled", cfg.Enabled,
		"max_retries", cfg.MaxRetries,
		"base_delay", cfg.BaseDelay,
		"max_delay", cfg.MaxDelay)
}
