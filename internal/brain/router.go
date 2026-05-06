// Package brain provides intelligent orchestration capabilities for HotPlex.
//
// The IntentRouter component (this file) classifies incoming messages to determine
// the optimal processing path:
//
//	┌─────────────┐     ┌──────────────┐
//	│ User Message │────▶│ IntentRouter │
//	└─────────────┘     └──────┬───────┘
//	                           │
//	            ┌──────────────┼──────────────┐
//	            ▼              ▼              ▼
//	        [chat]        [command]       [task]
//	      Brain handles   Brain handles   Engine handles
//	      casual chat     status/config   code, debugging
//
// # Intent Types
//
//   - chat: Casual conversation, greetings, small talk → Brain generates response
//   - command: Status queries, config commands → Brain generates response
//   - task: Code operations, debugging, analysis → Forward to Engine Provider
//   - unknown: Ambiguous intent → Default to Engine for safety
//
// # Optimization
//
// Fast-path detection handles obvious cases without Brain API calls:
//   - Greetings ("hi", "hello") → chat
//   - Status commands ("ping", "status") → command
//   - Code keywords ("function", "debug") → task
//
// Cache reduces repeated Brain API calls for similar messages.
package brain

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// IntentType represents the classification of user intent.
type IntentType string

const (
	// IntentTypeChat indicates casual conversation that can be handled by Brain.
	IntentTypeChat IntentType = "chat"
	// IntentTypeCommand indicates a configuration or status command.
	IntentTypeCommand IntentType = "command"
	// IntentTypeTask indicates a complex task requiring Engine Provider.
	IntentTypeTask IntentType = "task"
	// IntentTypeUnknown indicates unclear intent, defaults to Engine.
	IntentTypeUnknown IntentType = "unknown"
)

// Intent detection thresholds and limits.
const (
	// MinMessageLength is the minimum message length for fast-path detection.
	// Messages shorter than this are classified as chat.
	MinMessageLength = 3
	// MaxQuickMessageLength is the maximum length for quick classification.
	// Messages longer than this require deeper analysis.
	MaxQuickMessageLength = 50
	// MaxThankMessageLength is the max length for gratitude detection.
	MaxThankMessageLength = 30
	// MaxContextHistory is the maximum number of history items to include.
	MaxContextHistory = 5
)

// IntentResult represents the result of intent detection.
type IntentResult struct {
	Type       IntentType `json:"type"`
	Confidence float64    `json:"confidence"`
	Response   string     `json:"response,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

// IntentRouter performs intent detection for incoming messages.
// It determines whether a message should be handled by the Brain
// (for chat/commands) or forwarded to the Engine Provider (for tasks).
//
// Thread Safety: All public methods are safe for concurrent use.
// The cache is protected by a dedicated mutex.
type IntentRouter struct {
	brain  Brain        // AI backend for intelligent classification
	logger *slog.Logger // Structured logger for routing decisions

	// Configuration
	enabled             bool    // Master switch (disabled → all messages go to Engine)
	confidenceThreshold float64 // Minimum confidence for Brain classification (0.0-1.0)
	cacheSize           int     // Maximum cached intent results

	// LRU Cache for recent intent results
	// Key: SHA256 hash of normalized message, Value: IntentResult
	cache    map[string]*IntentResult
	lruList  *list.List               // List of cache keys, front = most recent, back = LRU
	lruIndex map[string]*list.Element // Quick lookup for list elements
	cacheMu  sync.RWMutex

	totalProcessed atomic.Int64
	cacheHits      atomic.Int64
}

// IntentRouterConfig holds configuration for IntentRouter.
type IntentRouterConfig struct {
	Enabled             bool
	ConfidenceThreshold float64
	CacheSize           int
}

// NewIntentRouter creates a new IntentRouter instance.
func NewIntentRouter(brain Brain, config IntentRouterConfig, logger *slog.Logger) *IntentRouter {
	if config.ConfidenceThreshold <= 0 {
		config.ConfidenceThreshold = 0.7
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 1000
	}

	return &IntentRouter{
		brain:               brain,
		logger:              logger,
		enabled:             config.Enabled,
		confidenceThreshold: config.ConfidenceThreshold,
		cacheSize:           config.CacheSize,
		cache:               make(map[string]*IntentResult),
		lruList:             list.New(),
		lruIndex:            make(map[string]*list.Element),
	}
}

// Route determines the intent of a message and returns the routing decision.
// It performs a two-stage classification:
//
//  1. Fast path: Rule-based detection for obvious cases (greetings, status commands)
//     → Returns immediately without Brain API call
//
//  2. Brain analysis: Sends prompt to Brain for intelligent classification
//     → Returns intent type with confidence score
//
// If the Brain is disabled or unavailable, returns IntentTypeTask to ensure
// the message is processed by the Engine (safe default).
func (r *IntentRouter) Route(ctx context.Context, msg string) *IntentResult {
	if !r.enabled || r.brain == nil {
		return &IntentResult{
			Type:       IntentTypeTask,
			Confidence: 1.0,
			Reason:     "brain disabled or not configured",
		}
	}

	// Check cache first
	cacheKey := r.cacheKey(msg)
	if cached := r.getFromCache(cacheKey); cached != nil {
		r.cacheHits.Add(1)
		return cached
	}

	// Perform intent detection
	result := r.detectIntent(ctx, msg)
	r.totalProcessed.Add(1)

	// Cache the result
	r.addToCache(cacheKey, result)

	return result
}

// RouteWithHistory determines intent considering conversation history.
func (r *IntentRouter) RouteWithHistory(ctx context.Context, msg string, history []string) *IntentResult {
	if !r.enabled || r.brain == nil {
		return &IntentResult{
			Type:       IntentTypeTask,
			Confidence: 1.0,
			Reason:     "brain disabled or not configured",
		}
	}

	// For messages with history context, we include context in analysis
	contextualMsg := r.buildContextualPrompt(msg, history)
	return r.Route(ctx, contextualMsg)
}

// detectIntent uses the Brain to classify the message intent.
func (r *IntentRouter) detectIntent(ctx context.Context, msg string) *IntentResult {
	// Fast path: rule-based detection for common patterns
	if result := r.fastPathDetection(msg); result != nil {
		return result
	}

	// Use Brain for intelligent classification
	var analysis struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
		Response   string  `json:"response,omitempty"`
	}

	prompt := r.buildIntentPrompt(msg)
	if err := r.brain.Analyze(ctx, prompt, &analysis); err != nil {
		r.logger.Warn("Intent detection failed, defaulting to task", "error", err)
		return &IntentResult{
			Type:       IntentTypeTask,
			Confidence: 0.5,
			Reason:     fmt.Sprintf("detection error: %v", err),
		}
	}

	intentType := IntentTypeUnknown
	switch strings.ToLower(analysis.Intent) {
	case "chat", "greeting", "casual", "small_talk":
		intentType = IntentTypeChat
	case "command", "config", "status", "query":
		intentType = IntentTypeCommand
	case "task", "complex", "code", "analysis", "execution":
		intentType = IntentTypeTask
	}

	return &IntentResult{
		Type:       intentType,
		Confidence: analysis.Confidence,
		Response:   analysis.Response,
		Reason:     analysis.Reason,
	}
}

// fastPathDetection performs rule-based detection for obvious cases.
// This avoids Brain API calls for trivial classifications.
//
// Patterns detected (returns immediately):
//   - Very short messages (<3 chars) → chat
//   - Greetings ("hi", "hello", "hey") → chat
//   - Thank you messages → chat
//   - Status commands ("ping", "status") → command
//   - Help requests → command
//
// Returns nil if the case is not clear-cut and needs Brain analysis.
// Code keywords ("function", "debug", etc.) force Brain analysis for accuracy.
func (r *IntentRouter) fastPathDetection(msg string) *IntentResult {
	msg = strings.TrimSpace(strings.ToLower(msg))

	// Empty or very short messages
	if len(msg) < MinMessageLength {
		return &IntentResult{
			Type:       IntentTypeChat,
			Confidence: 0.9,
			Reason:     "very short message",
			Response:   "I'm here to help! What would you like me to do?",
		}
	}

	// Greetings
	greetings := []string{"hi", "hello", "hey", "good morning", "good afternoon", "good evening", "howdy"}
	for _, g := range greetings {
		if msg == g || strings.HasPrefix(msg, g+" ") || strings.HasPrefix(msg, g+"!") {
			return &IntentResult{
				Type:       IntentTypeChat,
				Confidence: 0.95,
				Reason:     "greeting detected",
				Response:   "Hello! I'm your AI assistant. How can I help you today?",
			}
		}
	}

	// Thank you messages
	thanks := []string{"thank", "thanks", "thx", "ty"}
	for _, t := range thanks {
		if strings.Contains(msg, t) && len(msg) < MaxThankMessageLength {
			return &IntentResult{
				Type:       IntentTypeChat,
				Confidence: 0.9,
				Reason:     "gratitude detected",
				Response:   "You're welcome! Let me know if you need anything else.",
			}
		}
	}

	// Status commands
	statusCmds := []string{"status", "ping", "are you there", "you there", "are you online"}
	for _, s := range statusCmds {
		if strings.Contains(msg, s) {
			return &IntentResult{
				Type:       IntentTypeCommand,
				Confidence: 0.9,
				Reason:     "status query detected",
				Response:   "I'm online and ready to help! All systems operational.",
			}
		}
	}

	// Help requests
	if strings.Contains(msg, "help") && len(msg) < MaxQuickMessageLength {
		return &IntentResult{
			Type:       IntentTypeCommand,
			Confidence: 0.85,
			Reason:     "help request detected",
			Response:   "I can help with code analysis, file operations, debugging, and more. Just tell me what you need!",
		}
	}

	// Code-related keywords strongly suggest task intent
	codeKeywords := []string{"function", "class", "method", "variable", "bug", "error", "fix", "implement", "refactor", "test", "deploy", "build", "compile", "debug", "code", "file", "directory"}
	for _, kw := range codeKeywords {
		if strings.Contains(msg, kw) {
			return nil // Needs deeper analysis
		}
	}

	return nil // No clear pattern, defer to Brain
}

// buildIntentPrompt creates the prompt for intent detection.
func (r *IntentRouter) buildIntentPrompt(msg string) string {
	return fmt.Sprintf(`Analyze this user message and classify its intent.

Message: "%s"

Classify into one of these categories:
- "chat": Casual conversation, greetings, small talk, gratitude
- "command": Status queries, configuration commands, help requests
- "task": Code tasks, file operations, debugging, analysis, complex requests

Return JSON with:
{
  "intent": "chat|command|task",
  "confidence": 0.0-1.0,
  "reason": "brief explanation",
  "response": "optional quick response for chat/command types"
}`, msg)
}

// buildContextualPrompt includes history for better context.
func (r *IntentRouter) buildContextualPrompt(msg string, history []string) string {
	var sb strings.Builder
	sb.WriteString("Recent conversation context:\n")

	if len(history) > MaxContextHistory {
		history = history[len(history)-MaxContextHistory:]
	}

	for i, h := range history {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, h)
	}

	fmt.Fprintf(&sb, "\nCurrent message: %s", msg)
	return sb.String()
}

// cacheKey generates a cache key for a message using SHA256 hash.
func (r *IntentRouter) cacheKey(msg string) string {
	// Normalize message
	msg = strings.TrimSpace(strings.ToLower(msg))
	// Use SHA256 hash for unique cache key
	hash := sha256.Sum256([]byte(msg))
	return hex.EncodeToString(hash[:])
}

// getFromCache retrieves a cached intent result and updates LRU position.
func (r *IntentRouter) getFromCache(key string) *IntentResult {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	result, exists := r.cache[key]
	if !exists {
		return nil
	}

	// Move to front (most recently used)
	if elem, ok := r.lruIndex[key]; ok {
		r.lruList.MoveToFront(elem)
	}

	return result
}

// addToCache adds a result to the cache with proper LRU eviction.
func (r *IntentRouter) addToCache(key string, result *IntentResult) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// Check if already exists (update case)
	if elem, exists := r.lruIndex[key]; exists {
		r.cache[key] = result
		r.lruList.MoveToFront(elem)
		return
	}

	// Evict LRU entry if cache is full
	if len(r.cache) >= r.cacheSize {
		// Remove least recently used (back of list)
		if elem := r.lruList.Back(); elem != nil {
			lruKey, ok := elem.Value.(string)
			if ok {
				delete(r.cache, lruKey)
				r.lruList.Remove(elem)
				delete(r.lruIndex, lruKey)
			}
		}
	}

	// Add new entry
	r.cache[key] = result
	elem := r.lruList.PushFront(key)
	r.lruIndex[key] = elem
}

// IsRelevant checks if a message in a group chat is relevant to the bot.
// This is used for noise filtering in group channels.
func (r *IntentRouter) IsRelevant(ctx context.Context, msg string, botMentioned bool) bool {
	if !r.enabled || r.brain == nil {
		// Without brain, only process if bot is explicitly mentioned
		return botMentioned
	}

	// If bot is mentioned, always consider relevant
	if botMentioned {
		return true
	}

	// Use Brain to determine relevance
	var analysis struct {
		Relevant   bool    `json:"relevant"`
		Confidence float64 `json:"confidence"`
	}

	prompt := fmt.Sprintf(`Determine if this message in a group chat is directed at an AI assistant.

Message: "%s"

Consider:
- Is it a question that needs an answer?
- Does it contain keywords suggesting the user wants help?
- Is it a command or instruction?

Return JSON:
{
  "relevant": true/false,
  "confidence": 0.0-1.0
}`, msg)

	if err := r.brain.Analyze(ctx, prompt, &analysis); err != nil {
		r.logger.Warn("Relevance detection failed", "error", err)
		return false // Default to not relevant on error
	}

	return analysis.Relevant && analysis.Confidence >= r.confidenceThreshold
}

// GenerateResponse generates a quick response for chat/command intents.
func (r *IntentRouter) GenerateResponse(ctx context.Context, msg string, intent *IntentResult) (string, error) {
	if !r.enabled || r.brain == nil {
		return "", fmt.Errorf("brain not available")
	}

	// Use pre-computed response if available
	if intent.Response != "" {
		return intent.Response, nil
	}

	// Generate response using Brain
	prompt := fmt.Sprintf(`You are a friendly AI assistant. Respond briefly and helpfully to this message.

Message: "%s"

Guidelines:
- Keep responses under 2 sentences for chat
- For commands, provide status or guidance
- Be friendly but professional
- If the user seems to want complex help, suggest they provide more details`, msg)

	response, err := r.brain.Chat(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("generate response: %w", err)
	}

	return response, nil
}

// Stats returns router statistics.
func (r *IntentRouter) Stats() map[string]interface{} {
	r.cacheMu.RLock()
	cacheLen := len(r.cache)
	r.cacheMu.RUnlock()

	return map[string]interface{}{
		"enabled":         r.enabled,
		"total_processed": r.totalProcessed.Load(),
		"cache_hits":      r.cacheHits.Load(),
		"cache_size":      cacheLen,
		"hit_rate":        float64(r.cacheHits.Load()) / float64(r.totalProcessed.Load()+1),
	}
}

// ShouldUseEngine returns true if the message should be forwarded to the Engine.
func (r *IntentRouter) ShouldUseEngine(result *IntentResult) bool {
	switch result.Type {
	case IntentTypeTask:
		return true
	case IntentTypeUnknown:
		// Unknown defaults to Engine for safety
		return result.Confidence < r.confidenceThreshold
	default:
		return false
	}
}

// SetEnabled enables or disables the router at runtime.
func (r *IntentRouter) SetEnabled(enabled bool) {
	r.enabled = enabled
}

// GetEnabled returns whether the router is enabled.
func (r *IntentRouter) GetEnabled() bool {
	return r.enabled
}

// ClearCache clears the intent cache.
func (r *IntentRouter) ClearCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	r.cache = make(map[string]*IntentResult)
	r.lruList = list.New()
	r.lruIndex = make(map[string]*list.Element)
}

// DefaultIntentRouterConfig returns default configuration.
func DefaultIntentRouterConfig() IntentRouterConfig {
	return IntentRouterConfig{
		Enabled:             true,
		ConfidenceThreshold: 0.7,
		CacheSize:           1000,
	}
}

// GlobalIntentRouter returns the global IntentRouter instance.
// It uses the global Brain if available.
func GlobalIntentRouter() *IntentRouter {
	globalRouterOnce.Do(func() {
		if Global() != nil {
			globalIntentRouter = NewIntentRouter(Global(), DefaultIntentRouterConfig(), slog.Default())
		}
	})
	return globalIntentRouter
}

var (
	globalIntentRouter *IntentRouter
	globalRouterOnce   sync.Once
)

// InitIntentRouter initializes the global intent router.
func InitIntentRouter(config IntentRouterConfig, logger *slog.Logger) {
	if Global() == nil {
		logger.Debug("Cannot init IntentRouter: Brain not configured")
		return
	}
	globalIntentRouter = NewIntentRouter(Global(), config, logger)
	logger.Info("IntentRouter initialized", "enabled", config.Enabled)
}

// Route is a convenience function that uses the global IntentRouter.
func Route(ctx context.Context, msg string) *IntentResult {
	router := GlobalIntentRouter()
	if router == nil {
		return &IntentResult{
			Type:       IntentTypeTask,
			Confidence: 1.0,
			Reason:     "no router configured",
		}
	}
	return router.Route(ctx, msg)
}

// IsRelevant is a convenience function for group chat filtering.
func IsRelevant(ctx context.Context, msg string, botMentioned bool) bool {
	router := GlobalIntentRouter()
	if router == nil {
		return botMentioned
	}
	return router.IsRelevant(ctx, msg, botMentioned)
}

// QuickResponse generates a quick response for non-task intents.
func QuickResponse(ctx context.Context, msg string, result *IntentResult) (string, error) {
	router := GlobalIntentRouter()
	if router == nil {
		return "", fmt.Errorf("router not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return router.GenerateResponse(ctx, msg, result)
}
