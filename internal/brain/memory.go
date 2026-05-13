// Package brain provides intelligent orchestration capabilities for HotPlex.
//
// The ContextCompressor component (this file) manages conversation context
// to prevent context window overflow while preserving important information.
//
// # Problem
//
// Long conversations exceed LLM context limits, causing:
//   - API errors (context length exceeded)
//   - Loss of earlier conversation context
//   - Increased token costs
//
// # Solution
//
// Context compression reduces context size while preserving key information:
//
//	┌────────────────────────────────────────────────────────┐
//	│ Session History (before: 8000+ tokens)                 │
//	│ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐       │
//	│ │Turn1│ │Turn2│ │ ... │ │Turn8│ │Turn9│ │Turn10│       │
//	│ └─────┘ └─────┘ └─────┘ └─────┘ └─────┘ └─────┘       │
//	└────────────────────────────────────────────────────────┘
//	                         │
//	                         ▼ Compression (threshold reached)
//	┌────────────────────────────────────────────────────────┐
//	│ Compressed Session (after: ~2000 tokens)               │
//	│ ┌─────────────────┐ ┌─────┐ ┌─────┐ ┌─────┐           │
//	│ │ Summary of 1-7  │ │Turn8│ │Turn9│ │Turn10│           │
//	│ │ (~500 tokens)   │ └─────┘ └─────┘ └─────┘           │
//	│ └─────────────────┘                                   │
//	└────────────────────────────────────────────────────────┘
//
// # Compression Algorithm
//
//  1. Wait until token count exceeds TokenThreshold (default: 8000)
//  2. Keep last PreserveTurns (default: 5) turns intact
//  3. Summarize older turns using Brain AI (max MaxSummaryTokens)
//  4. Replace old turns with summary, update total token count
//
// # MemoryManager
//
// Extends ContextCompressor with user preference tracking:
//   - Stores user preferences across sessions (programming language, style)
//   - Extracts preferences from conversations using Brain AI
//   - Injects preferences into Engine prompts for personalization
package brain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// Turn represents a single conversation turn (user + assistant exchange).
type Turn struct {
	Role       string    `json:"role"` // "user" or "assistant"
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
	TokenCount int       `json:"token_count"`
}

// SessionHistory represents the conversation history of a session.
type SessionHistory struct {
	SessionID   string `json:"session_id"`
	Turns       []Turn `json:"turns"`
	TotalTokens int    `json:"total_tokens"`
	Compressed  bool   `json:"compressed"`
	Summary     string `json:"summary,omitempty"`
}

// CompressionConfig holds configuration for context compression.
type CompressionConfig struct {
	Enabled          bool          `json:"enabled"`            // Master switch for compression
	TokenThreshold   int           `json:"token_threshold"`    // Trigger compression at this token count (default: 8000)
	TargetTokenCount int           `json:"target_token_count"` // Target tokens after compression (default: 2000)
	PreserveTurns    int           `json:"preserve_turns"`     // Number of recent turns to keep intact (default: 5)
	MaxSummaryTokens int           `json:"max_summary_tokens"` // Max tokens for summary (default: 500)
	CompressionRatio float64       `json:"compression_ratio"`  // Target compression ratio 0.0-1.0 (default: 0.25)
	SessionTTL       time.Duration `json:"session_ttl"`        // Session memory TTL (default: 24h)
	CleanupInterval  time.Duration `json:"cleanup_interval"`   // Background cleanup interval (default: 1h)
}

// DefaultCompressionConfig returns default compression configuration.
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:          true,
		TokenThreshold:   8000, // Compress when approaching 8K tokens
		TargetTokenCount: 2000, // Target ~2K tokens after compression
		PreserveTurns:    5,    // Keep last 5 turns intact
		MaxSummaryTokens: 500,  // Summary should be ~500 tokens
		CompressionRatio: 0.25, // Target 25% of original
		SessionTTL:       24 * time.Hour,
		CleanupInterval:  1 * time.Hour, // Clean up expired sessions every hour
	}
}

// ContextCompressor manages context compression for sessions.
// It tracks conversation turns per session and compresses when threshold is reached.
//
// Thread Safety: All public methods are safe for concurrent use.
// The mu mutex protects sessions map and metrics.
//
// Lifecycle:
//  1. Create with NewContextCompressor()
//  2. Record turns with RecordTurn()
//  3. Check compression with CheckAndCompress()
//  4. Stop background cleanup with Stop()
type ContextCompressor struct {
	brain   Brain
	config  CompressionConfig
	logger  *slog.Logger
	enabled atomic.Bool // source of truth for Enabled, avoids locking on hot-path reads

	// Session storage (protected by mu)
	// Key: sessionID, Value: conversation history
	sessions map[string]*SessionHistory
	mu       sync.RWMutex

	// Background cleanup goroutine management
	ctx    context.Context    // Context for cancellation
	cancel context.CancelFunc // Cancels cleanup goroutine
	wg     sync.WaitGroup     // Waits for cleanup goroutine to exit

	// Metrics for monitoring compression effectiveness
	totalCompressions       int64   // Total compression operations performed
	totalTokensSaved        int64   // Tokens saved by compression
	averageCompressionRatio float64 // Running average of compression ratios

	// In-flight guard prevents redundant LLM calls for the same session.
	compressing sync.Map // sessionID → struct{}
}

// NewContextCompressor creates a new ContextCompressor.
func NewContextCompressor(brain Brain, config CompressionConfig, logger *slog.Logger) *ContextCompressor {
	ctx, cancel := context.WithCancel(context.Background())
	compressor := &ContextCompressor{
		brain:    brain,
		config:   config,
		logger:   logger,
		sessions: make(map[string]*SessionHistory),
		ctx:      ctx,
		cancel:   cancel,
	}
	compressor.enabled.Store(config.Enabled)

	// Start background cleanup daemon
	if config.CleanupInterval > 0 {
		compressor.startCleanupDaemon()
	}

	return compressor
}

// RecordTurn records a conversation turn in session history.
func (c *ContextCompressor) RecordTurn(sessionID, role, content string, tokenCount int) {
	if !c.enabled.Load() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	history, exists := c.sessions[sessionID]
	if !exists {
		history = &SessionHistory{
			SessionID: sessionID,
			Turns:     make([]Turn, 0),
		}
		c.sessions[sessionID] = history
	}

	turn := Turn{
		Role:       role,
		Content:    content,
		Timestamp:  time.Now(),
		TokenCount: tokenCount,
	}

	history.Turns = append(history.Turns, turn)
	history.TotalTokens += tokenCount
}

// CheckAndCompress checks if compression is needed and performs it.
// Returns the compressed history or nil if no compression occurred.
//
// Compression triggers when:
//   - Total tokens >= TokenThreshold
//   - Not already compressed recently (prevents repeated compression)
//
// Returns:
//   - (*SessionHistory, nil): Compression performed successfully
//   - (nil, nil): No compression needed (below threshold)
//   - (nil, error): Compression failed
func (c *ContextCompressor) CheckAndCompress(ctx context.Context, sessionID string) (*SessionHistory, error) {
	if !c.enabled.Load() || c.brain == nil {
		return nil, nil
	}

	c.mu.RLock()
	history, exists := c.sessions[sessionID]
	if !exists {
		c.mu.RUnlock()
		return nil, nil
	}

	// Check if compression is needed
	if history.TotalTokens < c.config.TokenThreshold {
		c.mu.RUnlock()
		return nil, nil
	}

	// Already compressed recently
	if history.Compressed && len(history.Turns) <= c.config.PreserveTurns*2 {
		c.mu.RUnlock()
		return nil, nil
	}
	c.mu.RUnlock()

	// Perform compression
	return c.compress(ctx, sessionID)
}

// compress performs the actual compression of session history.
// Uses lock-dropping to avoid blocking all sessions during the LLM call.
// An in-flight guard prevents redundant LLM calls for the same session.
func (c *ContextCompressor) compress(ctx context.Context, sessionID string) (*SessionHistory, error) {
	if _, loaded := c.compressing.LoadOrStore(sessionID, struct{}{}); loaded {
		return nil, nil // another compression already in progress
	}
	defer c.compressing.Delete(sessionID)

	// Phase 1: Extract turns to compress under lock (fast).
	c.mu.Lock()
	history, exists := c.sessions[sessionID]
	if !exists {
		c.mu.Unlock()
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if len(history.Turns) <= c.config.PreserveTurns*2 {
		c.mu.Unlock()
		return nil, nil
	}
	preserveStart := len(history.Turns) - c.config.PreserveTurns*2
	toCompress := make([]Turn, preserveStart)
	copy(toCompress, history.Turns[:preserveStart])
	totalTokens := history.TotalTokens
	c.mu.Unlock()

	// Phase 2: Generate summary outside lock (slow LLM call, 5-30s).
	summary, err := c.generateSummary(ctx, toCompress)
	if err != nil {
		c.logger.Warn("Failed to generate summary", "session_id", sessionID, "error", err)
		return nil, fmt.Errorf("generate summary: %w", err)
	}

	// Phase 3: Apply summary under lock (fast).
	c.mu.Lock()
	defer c.mu.Unlock()

	history, exists = c.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found after compression", sessionID)
	}

	// Recalculate: turns may have been added during the LLM call.
	if len(history.Turns) <= c.config.PreserveTurns*2 {
		return nil, nil
	}
	preserveStart = len(history.Turns) - c.config.PreserveTurns*2
	// Only drop turns up to what we compressed — newly added turns are preserved.
	dropCount := min(preserveStart, len(toCompress))
	oldTokens := 0
	for _, t := range history.Turns[:dropCount] {
		oldTokens += t.TokenCount
	}

	summaryTokens := llm.EstimateTokens(summary)
	history.Turns = history.Turns[dropCount:]
	history.Summary = summary
	history.Compressed = true
	history.TotalTokens = history.TotalTokens - oldTokens + summaryTokens

	c.totalCompressions++
	c.totalTokensSaved += int64(oldTokens - summaryTokens)
	c.updateCompressionRatio(float64(history.TotalTokens) / float64(oldTokens+totalTokens))

	c.logger.Info("Context compressed",
		"session_id", sessionID,
		"old_tokens", oldTokens,
		"summary_tokens", summaryTokens,
		"tokens_saved", oldTokens-summaryTokens,
		"preserved_turns", len(history.Turns))

	return history, nil
}

// generateSummary creates a summary of conversation turns.
func (c *ContextCompressor) generateSummary(ctx context.Context, turns []Turn) (string, error) {
	// Build conversation text
	var sb strings.Builder
	sb.WriteString("Previous conversation:\n\n")
	for i, t := range turns {
		fmt.Fprintf(&sb, "%s: %s\n", cases.Title(language.English).String(t.Role), t.Content)
		if i > MaxTurnsForSummary { // Limit context for summary generation
			sb.WriteString("... (earlier messages omitted)\n")
			break
		}
	}

	prompt := fmt.Sprintf(`Summarize this conversation history concisely.

%s

Create a brief summary (max %d tokens) that captures:
1. Key topics discussed
2. Important decisions or conclusions
3. Any pending tasks or questions

Focus on information that would be useful for continuing the conversation.
Do not include pleasantries or small talk.`, sb.String(), c.config.MaxSummaryTokens)

	summary, err := c.brain.Chat(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}

	return summary, nil
}

// GetHistory returns the session history for a given session.
func (c *ContextCompressor) GetHistory(sessionID string) *SessionHistory {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessions[sessionID]
}

// GetSessionSummary returns the summary for a session, if compressed.
func (c *ContextCompressor) GetSessionSummary(sessionID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if history, exists := c.sessions[sessionID]; exists && history.Summary != "" {
		return history.Summary
	}
	return ""
}

// GetContextForEngine returns the context to inject into Engine prompts.
// This includes any compressed summary and recent turns.
func (c *ContextCompressor) GetContextForEngine(sessionID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	history, exists := c.sessions[sessionID]
	if !exists {
		return ""
	}

	var sb strings.Builder

	// Include summary if available
	if history.Summary != "" {
		sb.WriteString("## Session Context\n\n")
		sb.WriteString(history.Summary)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// ClearSession clears history for a specific session.
func (c *ContextCompressor) ClearSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, sessionID)
}

// ClearExpired clears expired sessions based on TTL.
func (c *ContextCompressor) ClearExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cleared := 0

	for id, history := range c.sessions {
		if len(history.Turns) > 0 {
			lastTurn := history.Turns[len(history.Turns)-1]
			if now.Sub(lastTurn.Timestamp) > c.config.SessionTTL {
				delete(c.sessions, id)
				cleared++
			}
		}
	}

	if cleared > 0 {
		c.logger.Info("Cleared expired sessions", "count", cleared)
	}

	return cleared
}

// Stats returns compressor statistics.
func (c *ContextCompressor) Stats() map[string]interface{} {
	c.mu.RLock()
	stats := map[string]interface{}{
		"enabled":                   c.enabled.Load(),
		"session_count":             len(c.sessions),
		"total_compressions":        c.totalCompressions,
		"total_tokens_saved":        c.totalTokensSaved,
		"average_compression_ratio": c.averageCompressionRatio,
		"token_threshold":           c.config.TokenThreshold,
	}
	c.mu.RUnlock()
	return stats
}

// Context compression limits.
const (
	// MaxTurnsForSummary is the maximum number of turns to include in summary generation.
	// Prevents excessive token usage when building summary prompts.
	MaxTurnsForSummary = 20
)

// updateCompressionRatio updates the running average compression ratio.
func (c *ContextCompressor) updateCompressionRatio(ratio float64) {
	if c.totalCompressions == 1 {
		c.averageCompressionRatio = ratio
	} else {
		// Simple moving average
		c.averageCompressionRatio = (c.averageCompressionRatio*float64(c.totalCompressions-1) + ratio) / float64(c.totalCompressions)
	}
}

// startCleanupDaemon starts a background goroutine to clean up expired sessions.
func (c *ContextCompressor) startCleanupDaemon() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(c.config.CleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				cleared := c.ClearExpired()
				if cleared > 0 {
					c.logger.Debug("Cleaned up expired sessions", "count", cleared)
				}
			}
		}
	}()
}

// Stop stops the background cleanup daemon.
func (c *ContextCompressor) Stop() {
	c.cancel()
	c.wg.Wait()
}

// ForceCompress forces compression of a session regardless of threshold.
func (c *ContextCompressor) ForceCompress(ctx context.Context, sessionID string) (*SessionHistory, error) {
	if !c.enabled.Load() || c.brain == nil {
		return nil, fmt.Errorf("compression not enabled")
	}
	return c.compress(ctx, sessionID)
}

// SetEnabled enables or disables compression at runtime.
func (c *ContextCompressor) SetEnabled(enabled bool) {
	c.enabled.Store(enabled)
}

// UpdateConfig updates the compression configuration.
func (c *ContextCompressor) UpdateConfig(config CompressionConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
	c.enabled.Store(config.Enabled)
}

// MemoryManager provides high-level memory management capabilities.
// It combines context compression with persistent memory storage.
type MemoryManager struct {
	compressor *ContextCompressor
	brain      Brain
	logger     *slog.Logger

	// User preferences storage (persisted across sessions)
	preferences map[string]map[string]string // userID -> key -> value
	prefMu      sync.RWMutex
}

// NewMemoryManager creates a new MemoryManager.
func NewMemoryManager(compressor *ContextCompressor, brain Brain, logger *slog.Logger) *MemoryManager {
	return &MemoryManager{
		compressor:  compressor,
		brain:       brain,
		logger:      logger,
		preferences: make(map[string]map[string]string),
	}
}

// RecordUserPreference stores a user preference.
func (m *MemoryManager) RecordUserPreference(userID, key, value string) {
	m.prefMu.Lock()
	defer m.prefMu.Unlock()

	if m.preferences[userID] == nil {
		m.preferences[userID] = make(map[string]string)
	}
	m.preferences[userID][key] = value
}

// GetUserPreference retrieves a user preference.
func (m *MemoryManager) GetUserPreference(userID, key string) (string, bool) {
	m.prefMu.RLock()
	defer m.prefMu.RUnlock()

	if prefs, exists := m.preferences[userID]; exists {
		val, ok := prefs[key]
		return val, ok
	}
	return "", false
}

// GetAllUserPreferences returns all preferences for a user.
func (m *MemoryManager) GetAllUserPreferences(userID string) map[string]string {
	m.prefMu.RLock()
	defer m.prefMu.RUnlock()

	prefs := m.preferences[userID]
	if prefs == nil {
		return nil
	}

	// Return a copy
	result := make(map[string]string, len(prefs))
	for k, v := range prefs {
		result[k] = v
	}
	return result
}

// GenerateUserContext generates context string with user preferences.
func (m *MemoryManager) GenerateUserContext(userID string) string {
	prefs := m.GetAllUserPreferences(userID)
	if len(prefs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## User Preferences\n\n")
	for key, value := range prefs {
		fmt.Fprintf(&sb, "- %s: %s\n", key, value)
	}
	return sb.String()
}

// ExtractPreferences uses Brain to extract preferences from conversation.
func (m *MemoryManager) ExtractPreferences(ctx context.Context, userID, conversation string) error {
	if m.brain == nil {
		return ErrBrainNotConfigured
	}

	var extracted struct {
		Preferences map[string]string `json:"preferences"`
	}

	prompt := fmt.Sprintf(`Extract user preferences from this conversation.

Conversation:
%s

Look for explicit or implicit preferences like:
- Programming language preferences
- Framework preferences
- Coding style preferences
- Testing preferences

Return JSON:
{
  "preferences": {
    "key": "value"
  }
}

Only extract clear preferences, not assumptions.`, conversation)

	if err := m.brain.Analyze(ctx, prompt, &extracted); err != nil {
		return fmt.Errorf("extract preferences: %w", err)
	}

	// Store extracted preferences
	for key, value := range extracted.Preferences {
		m.RecordUserPreference(userID, key, value)
	}

	if len(extracted.Preferences) > 0 {
		m.logger.Info("Extracted user preferences",
			"user_id", userID,
			"count", len(extracted.Preferences))
	}

	return nil
}

// GetCompressor returns the underlying compressor.
func (m *MemoryManager) GetCompressor() *ContextCompressor {
	return m.compressor
}

// Global memory manager instances.
// Use atomic.Pointer for race-free concurrent access.
var (
	globalCompressor atomic.Pointer[ContextCompressor]
	globalMemoryMgr  atomic.Pointer[MemoryManager]
)

// GlobalCompressor returns the global ContextCompressor instance.
func GlobalCompressor() *ContextCompressor {
	return globalCompressor.Load()
}

// GlobalMemoryManager returns the global MemoryManager instance.
func GlobalMemoryManager() *MemoryManager {
	return globalMemoryMgr.Load()
}

// InitMemory initializes the global memory management system.
func InitMemory(config CompressionConfig, logger *slog.Logger) {
	if Global() == nil {
		logger.Debug("Cannot init Memory: Brain not configured")
		return
	}

	comp := NewContextCompressor(Global(), config, logger)
	globalCompressor.Store(comp)
	globalMemoryMgr.Store(NewMemoryManager(comp, Global(), logger))

	logger.Info("Memory management initialized",
		"enabled", config.Enabled,
		"token_threshold", config.TokenThreshold)
}

// RecordTurn is a convenience function to record a turn.
func RecordTurn(sessionID, role, content string, tokenCount int) {
	if c := globalCompressor.Load(); c != nil {
		c.RecordTurn(sessionID, role, content, tokenCount)
	}
}

// CheckCompress is a convenience function to check and compress.
func CheckCompress(ctx context.Context, sessionID string) (*SessionHistory, error) {
	if c := globalCompressor.Load(); c != nil {
		return c.CheckAndCompress(ctx, sessionID)
	}
	return nil, nil
}

// GetSessionSummary is a convenience function to get session summary.
func GetSessionSummary(sessionID string) string {
	if c := globalCompressor.Load(); c != nil {
		return c.GetSessionSummary(sessionID)
	}
	return ""
}
