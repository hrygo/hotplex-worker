package llm

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// TokenUsage represents token usage for a single request.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Model        string
	Timestamp    time.Time
}

// CostCalculator calculates costs based on model pricing.
type CostCalculator struct {
	mu       sync.RWMutex
	pricing  map[string]ModelPricing
	sessions map[string]*SessionCost
}

// ModelPricing represents pricing for a model.
type ModelPricing struct {
	ModelName       string
	Provider        string
	CostPer1KInput  float64 // USD per 1K input tokens
	CostPer1KOutput float64 // USD per 1K output tokens
	CostPer1KCache  float64 // USD per 1K cached input tokens (if applicable)
	Currency        string
	EffectiveDate   time.Time
}

// SessionCost tracks costs for a single session.
type SessionCost struct {
	SessionID      string
	TotalInput     int64
	TotalOutput    int64
	TotalCost      float64
	RequestCount   int64
	FirstRequest   time.Time
	LastRequest    time.Time
	ModelBreakdown map[string]*ModelUsage
	BudgetLimit    float64 // Optional budget limit
	BudgetAlerted  bool    // Whether budget alert has been triggered
}

// ModelUsage tracks usage for a specific model within a session.
type ModelUsage struct {
	ModelName    string
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	RequestCount int64
}

// DefaultModelPricing returns default pricing for common models.
func DefaultModelPricing() []ModelPricing {
	return []ModelPricing{
		// OpenAI Models
		{
			ModelName:       "gpt-4o-mini",
			Provider:        "openai",
			CostPer1KInput:  0.00015,
			CostPer1KOutput: 0.0006,
			Currency:        "USD",
		},
		{
			ModelName:       "gpt-4o",
			Provider:        "openai",
			CostPer1KInput:  0.005,
			CostPer1KOutput: 0.015,
			Currency:        "USD",
		},
		{
			ModelName:       "gpt-4-turbo",
			Provider:        "openai",
			CostPer1KInput:  0.01,
			CostPer1KOutput: 0.03,
			Currency:        "USD",
		},
		// Anthropic Models
		{
			ModelName:       "claude-3-haiku",
			Provider:        "anthropic",
			CostPer1KInput:  0.00025,
			CostPer1KOutput: 0.00125,
			Currency:        "USD",
		},
		{
			ModelName:       "claude-3-sonnet",
			Provider:        "anthropic",
			CostPer1KInput:  0.003,
			CostPer1KOutput: 0.015,
			Currency:        "USD",
		},
		{
			ModelName:       "claude-3-opus",
			Provider:        "anthropic",
			CostPer1KInput:  0.015,
			CostPer1KOutput: 0.075,
			Currency:        "USD",
		},
		// Google Models
		{
			ModelName:       "gemini-1.5-flash",
			Provider:        "google",
			CostPer1KInput:  0.000075,
			CostPer1KOutput: 0.0003,
			Currency:        "USD",
		},
		{
			ModelName:       "gemini-1.5-pro",
			Provider:        "google",
			CostPer1KInput:  0.00125,
			CostPer1KOutput: 0.005,
			Currency:        "USD",
		},
		// Alibaba/DashScope Models
		{
			ModelName:       "qwen-turbo",
			Provider:        "dashscope",
			CostPer1KInput:  0.0003,
			CostPer1KOutput: 0.0006,
			Currency:        "USD",
		},
		{
			ModelName:       "qwen-plus",
			Provider:        "dashscope",
			CostPer1KInput:  0.0006,
			CostPer1KOutput: 0.0012,
			Currency:        "USD",
		},
		{
			ModelName:       "qwen-max",
			Provider:        "dashscope",
			CostPer1KInput:  0.005,
			CostPer1KOutput: 0.015,
			Currency:        "USD",
		},
		// DeepSeek Models
		{
			ModelName:       "deepseek-chat",
			Provider:        "deepseek",
			CostPer1KInput:  0.00027,
			CostPer1KOutput: 0.0011,
			Currency:        "USD",
		},
	}
}

// NewCostCalculator creates a new cost calculator.
func NewCostCalculator() *CostCalculator {
	cc := &CostCalculator{
		pricing:  make(map[string]ModelPricing),
		sessions: make(map[string]*SessionCost),
	}

	// Initialize with default pricing
	for _, p := range DefaultModelPricing() {
		cc.pricing[p.ModelName] = p
	}

	return cc
}

// AddPricing adds or updates pricing for a model.
func (cc *CostCalculator) AddPricing(pricing ModelPricing) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.pricing[pricing.ModelName] = pricing
}

// GetPricing returns pricing for a model.
func (cc *CostCalculator) GetPricing(modelName string) (ModelPricing, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	pricing, ok := cc.pricing[modelName]
	return pricing, ok
}

// CalculateCost calculates the cost for a request.
func (cc *CostCalculator) CalculateCost(modelName string, inputTokens, outputTokens int) (float64, error) {
	cc.mu.RLock()
	pricing, ok := cc.pricing[modelName]
	cc.mu.RUnlock()

	if !ok {
		// Try to find partial match
		for k, p := range cc.pricing {
			if strings.Contains(modelName, k) || strings.Contains(k, modelName) {
				pricing = p
				ok = true
				break
			}
		}
	}

	if !ok {
		return 0, fmt.Errorf("no pricing found for model: %s", modelName)
	}

	inputCost := float64(inputTokens) / 1000.0 * pricing.CostPer1KInput
	outputCost := float64(outputTokens) / 1000.0 * pricing.CostPer1KOutput

	return inputCost + outputCost, nil
}

// CountTokens estimates token count from text using CJK-aware estimation.
func (cc *CostCalculator) CountTokens(text string) int {
	return EstimateTokens(text)
}

// CountTokensFromMessages counts tokens from OpenAI messages.
func (cc *CostCalculator) CountTokensFromMessages(messages []openai.ChatCompletionMessage) int {
	total := 0
	for _, msg := range messages {
		// Each message has overhead
		total += 4 // message overhead
		total += cc.CountTokens(msg.Content)
		if msg.Role != "" {
			total += cc.CountTokens(msg.Role)
		}
	}
	return total
}

// TrackRequest tracks a request's cost for a session.
func (cc *CostCalculator) TrackRequest(sessionID, modelName string, inputTokens, outputTokens int) (*SessionCost, float64, error) {
	cost, err := cc.CalculateCost(modelName, inputTokens, outputTokens)
	if err != nil {
		return nil, 0, err
	}

	cc.mu.Lock()
	defer cc.mu.Unlock()

	session, ok := cc.sessions[sessionID]
	if !ok {
		session = &SessionCost{
			SessionID:      sessionID,
			ModelBreakdown: make(map[string]*ModelUsage),
			FirstRequest:   time.Now(),
		}
		cc.sessions[sessionID] = session
	}

	now := time.Now()
	session.LastRequest = now
	session.TotalInput += int64(inputTokens)
	session.TotalOutput += int64(outputTokens)
	session.TotalCost += cost
	session.RequestCount++

	// Update model breakdown
	modelUsage, ok := session.ModelBreakdown[modelName]
	if !ok {
		modelUsage = &ModelUsage{
			ModelName: modelName,
		}
		session.ModelBreakdown[modelName] = modelUsage
	}
	modelUsage.InputTokens += int64(inputTokens)
	modelUsage.OutputTokens += int64(outputTokens)
	modelUsage.Cost += cost
	modelUsage.RequestCount++

	// Check budget alert
	if session.BudgetLimit > 0 && session.TotalCost >= session.BudgetLimit && !session.BudgetAlerted {
		session.BudgetAlerted = true
		// Budget alert triggered - caller should handle notification
	}

	return session, cost, nil
}

// GetSessionCost returns cost statistics for a session.
func (cc *CostCalculator) GetSessionCost(sessionID string) (*SessionCost, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	session, ok := cc.sessions[sessionID]
	return session, ok
}

// SetSessionBudget sets a budget limit for a session.
func (cc *CostCalculator) SetSessionBudget(sessionID string, budget float64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	session, ok := cc.sessions[sessionID]
	if !ok {
		session = &SessionCost{
			SessionID:      sessionID,
			ModelBreakdown: make(map[string]*ModelUsage),
			FirstRequest:   time.Now(),
			LastRequest:    time.Now(),
		}
		cc.sessions[sessionID] = session
	}
	session.BudgetLimit = budget
	session.BudgetAlerted = session.TotalCost >= budget
}

// GetAllSessions returns all session costs.
func (cc *CostCalculator) GetAllSessions() map[string]*SessionCost {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*SessionCost)
	for k, v := range cc.sessions {
		result[k] = v
	}
	return result
}

// GetTotalCost returns total cost across all sessions.
func (cc *CostCalculator) GetTotalCost() float64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	var total float64
	for _, session := range cc.sessions {
		total += session.TotalCost
	}
	return total
}

// ResetSession resets a session's cost tracking.
func (cc *CostCalculator) ResetSession(sessionID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	delete(cc.sessions, sessionID)
}
