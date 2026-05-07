package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// RouteStrategy defines the routing strategy for model selection.
type RouteStrategy string

const (
	// StrategyCostPriority routes to the cheapest model that meets requirements.
	StrategyCostPriority RouteStrategy = "cost_priority"
	// StrategyLatencyPriority routes to the fastest model.
	StrategyLatencyPriority RouteStrategy = "latency_priority"
	// StrategyQualityPriority routes to the highest quality model.
	StrategyQualityPriority RouteStrategy = "quality_priority"
	// StrategyBalanced routes with balanced cost/quality tradeoff.
	StrategyBalanced RouteStrategy = "balanced"
)

// Scenario defines the use case scenario for routing.
type Scenario string

const (
	// ScenarioChat is for conversational chat.
	ScenarioChat Scenario = "chat"
	// ScenarioAnalyze is for structured analysis/extraction.
	ScenarioAnalyze Scenario = "analyze"
	// ScenarioCode is for code generation/review.
	ScenarioCode Scenario = "code"
	// ScenarioReasoning is for complex reasoning tasks.
	ScenarioReasoning Scenario = "reasoning"
)

// ModelConfig represents configuration for a single model.
type ModelConfig struct {
	// Name is the model identifier (e.g., "gpt-4o-mini", "qwen-plus").
	Name string
	// Provider is the provider name (e.g., "openai", "dashscope").
	Provider string
	// APIKey is the API key for this model.
	APIKey string
	// Endpoint is the optional custom endpoint.
	Endpoint string
	// CostPer1KInput is the cost per 1K input tokens (USD).
	CostPer1KInput float64
	// CostPer1KOutput is the cost per 1K output tokens (USD).
	CostPer1KOutput float64
	// AvgLatencyMs is the average latency in milliseconds.
	AvgLatencyMs int64
	// MaxTokens is the maximum context window size.
	MaxTokens int
	// Enabled indicates if this model is available for routing.
	Enabled bool
}

// RouterConfig holds the configuration for the model router.
type RouterConfig struct {
	// DefaultStrategy is the default routing strategy.
	DefaultStrategy RouteStrategy
	// Models is the list of available models.
	Models []ModelConfig
	// ScenarioModelMap maps scenarios to preferred model names.
	ScenarioModelMap map[Scenario]string
	// FallbackModel is used when primary model fails.
	FallbackModel string
	// Logger for routing decisions.
	Logger *slog.Logger
}

// Router provides dynamic model routing based on scenario and strategy.
type Router struct {
	config RouterConfig
	mu     sync.RWMutex
	// metrics collector for routing decisions
	metrics *MetricsCollector
}

// NewRouter creates a new model router.
func NewRouter(config RouterConfig, metrics *MetricsCollector) *Router {
	return &Router{
		config:  config,
		metrics: metrics,
	}
}

// SelectModel selects the best model for the given scenario and strategy.
func (r *Router) SelectModel(ctx context.Context, scenario Scenario, strategy RouteStrategy) (*ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.config.Models) == 0 {
		return nil, fmt.Errorf("no models configured")
	}

	// Check scenario-specific mapping first
	if preferredModel, ok := r.config.ScenarioModelMap[scenario]; ok {
		for i := range r.config.Models {
			if r.config.Models[i].Name == preferredModel && r.config.Models[i].Enabled {
				r.logRouting(scenario, strategy, preferredModel, "scenario_mapping")
				return &r.config.Models[i], nil
			}
		}
	}

	// Apply routing strategy
	var selected *ModelConfig
	switch strategy {
	case StrategyCostPriority:
		selected = r.selectByCost()
	case StrategyLatencyPriority:
		selected = r.selectByLatency()
	case StrategyQualityPriority:
		selected = r.selectByQuality(scenario)
	case StrategyBalanced:
		selected = r.selectBalanced(scenario)
	default:
		selected = r.selectByCost() // Default to cost priority
	}

	if selected == nil {
		// Fallback to first enabled model
		for i := range r.config.Models {
			if r.config.Models[i].Enabled {
				selected = &r.config.Models[i]
				break
			}
		}
	}

	if selected == nil {
		return nil, fmt.Errorf("no enabled models available")
	}

	r.logRouting(scenario, strategy, selected.Name, string(strategy))
	return selected, nil
}

// selectByCost selects the cheapest enabled model.
func (r *Router) selectByCost() *ModelConfig {
	var cheapest *ModelConfig
	minCost := -1.0

	for i := range r.config.Models {
		m := &r.config.Models[i]
		if !m.Enabled {
			continue
		}
		// Estimate cost for typical request (100 input + 200 output tokens)
		estimatedCost := m.CostPer1KInput*0.1 + m.CostPer1KOutput*0.2
		if minCost < 0 || estimatedCost < minCost {
			minCost = estimatedCost
			cheapest = m
		}
	}
	return cheapest
}

// selectByLatency selects the fastest enabled model.
func (r *Router) selectByLatency() *ModelConfig {
	var fastest *ModelConfig
	minLatency := int64(-1)

	for i := range r.config.Models {
		m := &r.config.Models[i]
		if !m.Enabled {
			continue
		}
		if minLatency < 0 || m.AvgLatencyMs < minLatency {
			minLatency = m.AvgLatencyMs
			fastest = m
		}
	}
	return fastest
}

// selectByQuality selects the highest quality model for the scenario.
func (r *Router) selectByQuality(_ Scenario) *ModelConfig {
	// Quality heuristic: prefer models with larger context windows
	var best *ModelConfig
	maxTokens := 0

	for i := range r.config.Models {
		m := &r.config.Models[i]
		if !m.Enabled {
			continue
		}
		if m.MaxTokens > maxTokens {
			maxTokens = m.MaxTokens
			best = m
		}
	}
	return best
}

// selectBalanced selects a balanced model based on scenario.
func (r *Router) selectBalanced(scenario Scenario) *ModelConfig {
	// For chat: prefer cost-effective models
	// For analyze/reasoning: prefer quality models
	if scenario == ScenarioChat {
		return r.selectByCost()
	}
	return r.selectByQuality(scenario)
}

// logRouting logs the routing decision.
func (r *Router) logRouting(scenario Scenario, strategy RouteStrategy, model, reason string) {
	if r.config.Logger != nil {
		r.config.Logger.Debug("model routed",
			"scenario", scenario,
			"strategy", strategy,
			"model", model,
			"reason", reason)
	}
	if r.metrics != nil {
		r.metrics.RecordRoutingDecision(scenario, strategy, model)
	}
}

// DetectScenario infers the scenario from the prompt content.
func (r *Router) DetectScenario(prompt string) Scenario {
	promptLower := strings.ToLower(prompt)

	// Check for code-related keywords
	if strings.Contains(promptLower, "code") ||
		strings.Contains(promptLower, "function") ||
		strings.Contains(promptLower, "variable") ||
		strings.Contains(promptLower, "debug") ||
		strings.Contains(promptLower, "refactor") {
		return ScenarioCode
	}

	// Check for analysis/extraction keywords
	if strings.Contains(promptLower, "analyze") ||
		strings.Contains(promptLower, "extract") ||
		strings.Contains(promptLower, "json") ||
		strings.Contains(promptLower, "structure") ||
		strings.Contains(promptLower, "parse") {
		return ScenarioAnalyze
	}

	// Check for reasoning keywords
	if strings.Contains(promptLower, "reason") ||
		strings.Contains(promptLower, "think") ||
		strings.Contains(promptLower, "solve") ||
		strings.Contains(promptLower, "calculate") ||
		strings.Contains(promptLower, "explain why") {
		return ScenarioReasoning
	}

	// Default to chat
	return ScenarioChat
}

// AddModel dynamically adds a model to the router.
func (r *Router) AddModel(model ModelConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.Models = append(r.config.Models, model)
}

// RemoveModel removes a model by name.
func (r *Router) RemoveModel(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, m := range r.config.Models {
		if m.Name == name {
			r.config.Models = append(r.config.Models[:i], r.config.Models[i+1:]...)
			break
		}
	}
}

// GetModels returns all configured models.
func (r *Router) GetModels() []ModelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]ModelConfig{}, r.config.Models...)
}

// GetDefaultStrategy returns the default routing strategy.
func (r *Router) GetDefaultStrategy() RouteStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.DefaultStrategy
}
