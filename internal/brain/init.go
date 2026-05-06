package brain

import (
	"context"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// Init initializes the global Brain from environmental variables.
// It detects the provider and sets the Global Brain instance.
//
// IMPORTANT: This function MUST be called before using any Brain-dependent features:
//   - GlobalIntentRouter() requires Global() to be non-nil
//   - GlobalCompressor() requires Global() to be non-nil
//   - GlobalGuard() requires Global() to be non-nil
//
// If HOTPLEX_BRAIN_API_KEY is not set, Brain is disabled and features gracefully degrade.
func Init(logger *slog.Logger) error {
	config := LoadConfigFromEnv()

	if !config.Enabled {
		logger.Debug("Native Brain is disabled or missing configuration. Skipping.")
		return nil
	}

	var baseClient llm.LLMClient

	// 1. Initialize base client based on Protocol
	switch config.Model.Protocol {
	case "anthropic":
		baseClient = llm.NewAnthropicClient("", config.Model.Endpoint, config.Model.Model, logger)
		logger.Info("Anthropic brain client initialized", "model", config.Model.Model)
	case "openai":
	default:
		// Default to OpenAI compatible client
		baseClient = llm.NewOpenAIClient("", config.Model.Endpoint, config.Model.Model, logger)
		logger.Debug("OpenAI brain client initialized", "model", config.Model.Model)
	}

	// 2. Initialize orchestration & observability components (shared)
	var metricsCollector *llm.MetricsCollector
	if config.Metrics.Enabled {
		metricsCollector = llm.NewMetricsCollector(llm.MetricsConfig{
			Enabled:           true,
			ServiceName:       config.Metrics.ServiceName,
			MaxLatencySamples: 1000,
		})
	}

	var costCalculator *llm.CostCalculator
	if config.Cost.Enabled {
		costCalculator = llm.NewCostCalculator()
	}

	var rateLimiter *llm.RateLimiter
	if config.RateLimit.Enabled {
		rateLimiter = llm.NewRateLimiter(llm.RateLimitConfig{
			RequestsPerSecond: config.RateLimit.RPS,
			BurstSize:         config.RateLimit.Burst,
			MaxQueueSize:      config.RateLimit.QueueSize,
			QueueTimeout:      config.RateLimit.QueueTimeout,
			PerModel:          config.RateLimit.PerModel,
		})
	}

	var router *llm.Router
	if config.Router.Enabled {
		modelConfigs := config.Router.Models
		if len(modelConfigs) == 0 {
			pricing := llm.DefaultModelPricing()
			for _, p := range pricing {
				modelConfigs = append(modelConfigs, llm.ModelConfig{
					Name:            p.ModelName,
					Provider:        p.Provider,
					CostPer1KInput:  p.CostPer1KInput,
					CostPer1KOutput: p.CostPer1KOutput,
					Enabled:         true,
				})
			}
		}

		router = llm.NewRouter(llm.RouterConfig{
			DefaultStrategy:  llm.RouteStrategy(config.Router.DefaultStage),
			Models:           modelConfigs,
			ScenarioModelMap: make(map[llm.Scenario]string),
			FallbackModel:    config.Model.Model,
			Logger:           logger,
		}, metricsCollector)
	}

	// 3. Apply shared middleware wrapping
	client := baseClient

	// Retry
	client = llm.NewRetryClient(client, config.Retry.MaxAttempts, config.Retry.MinWaitMs, config.Retry.MaxWaitMs)

	// Cache
	if config.Cache.Enabled && config.Cache.Size > 0 {
		client = llm.NewCachedClient(client, config.Cache.Size)
	}

	// Rate limiting
	if rateLimiter != nil {
		client = llm.NewRateLimitedClient(client, rateLimiter)
	}

	// 4. Register global brain instance
	SetGlobal(&enhancedBrainWrapper{
		client:         client,
		config:         config,
		metrics:        metricsCollector,
		costCalculator: costCalculator,
		router:         router,
		rateLimiter:    rateLimiter,
		logger:         logger,
		timeout:        time.Duration(config.Model.TimeoutS) * time.Second, // Pre-compute timeout
	})

	// 5. Initialize specialized brain components
	if config.IntentRouter.Enabled {
		InitIntentRouter(IntentRouterConfig{
			Enabled:             config.IntentRouter.Enabled,
			ConfidenceThreshold: config.IntentRouter.ConfidenceThreshold,
			CacheSize:           config.IntentRouter.CacheSize,
		}, logger)
	}

	if config.Memory.Enabled {
		sessionTTL, _ := time.ParseDuration(config.Memory.SessionTTL)
		if sessionTTL == 0 {
			sessionTTL = 24 * time.Hour
		}
		InitMemory(CompressionConfig{
			Enabled:          config.Memory.Enabled,
			TokenThreshold:   config.Memory.TokenThreshold,
			TargetTokenCount: config.Memory.TargetTokenCount,
			PreserveTurns:    config.Memory.PreserveTurns,
			MaxSummaryTokens: config.Memory.MaxSummaryTokens,
			CompressionRatio: config.Memory.CompressionRatio,
			SessionTTL:       sessionTTL,
		}, logger)
	}

	if config.Guard.Enabled {
		if err := InitGuard(GuardConfig{
			Enabled:            config.Guard.Enabled,
			InputGuardEnabled:  config.Guard.InputGuardEnabled,
			OutputGuardEnabled: config.Guard.OutputGuardEnabled,
			Chat2ConfigEnabled: config.Guard.Chat2ConfigEnabled,
			MaxInputLength:     config.Guard.MaxInputLength,
			ScanDepth:          config.Guard.ScanDepth,
			Sensitivity:        config.Guard.Sensitivity,
			AdminUsers:         config.Guard.AdminUsers,
			AdminChannels:      config.Guard.AdminChannels,
			ResponseTimeout:    config.Guard.ResponseTimeout,
			RateLimitRPS:       config.Guard.RateLimitRPS,
			RateLimitBurst:     config.Guard.RateLimitBurst,
		}, logger); err != nil {
			logger.Warn("Failed to initialize SafetyGuard", "error", err)
		}
	}

	logger.Info("Native Brain initialized",
		"provider", config.Model.Provider,
		"protocol", config.Model.Protocol,
		"model", config.Model.Model,
		"cache", config.Cache.Enabled,
		"metrics", config.Metrics.Enabled,
		"intent_router", config.IntentRouter.Enabled)

	return nil
}

// llmClient defines the interface for LLM client operations used by Brain.
// This enables composition and middleware wrapping at the client level.
type llmClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
	Analyze(ctx context.Context, prompt string, target any) error
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
	HealthCheck(ctx context.Context) HealthStatus
}

// Compile-time interface verification for llmClient
var _ llmClient = llm.LLMClient(nil)

// enhancedBrainWrapper satisfies Brain, StreamingBrain, RoutableBrain, and ObservableBrain interfaces.
type enhancedBrainWrapper struct {
	client         llmClient
	config         Config
	metrics        *llm.MetricsCollector
	costCalculator *llm.CostCalculator
	router         *llm.Router
	rateLimiter    *llm.RateLimiter
	logger         *slog.Logger
	timeout        time.Duration // Pre-computed timeout for hot path
}

func (w *enhancedBrainWrapper) Chat(ctx context.Context, prompt string) (string, error) {
	return w.ChatWithModel(ctx, "", prompt)
}

func (w *enhancedBrainWrapper) Analyze(ctx context.Context, prompt string, target any) error {
	return w.AnalyzeWithModel(ctx, "", prompt, target)
}

func (w *enhancedBrainWrapper) ChatWithModel(ctx context.Context, model, prompt string) (string, error) {
	ctx, cancel := w.applyTimeout(ctx)
	defer cancel()

	model = w.selectModel(ctx, model, llm.ScenarioChat)

	if err := w.applyRateLimit(ctx, model); err != nil {
		return "", err
	}

	timer := w.startMetricsTimer(model, "chat")
	result, err := w.client.Chat(ctx, prompt)
	w.recordMetrics(timer, model, prompt, result, err)

	return result, err
}

func (w *enhancedBrainWrapper) AnalyzeWithModel(ctx context.Context, model, prompt string, target any) error {
	ctx, cancel := w.applyTimeout(ctx)
	defer cancel()

	model = w.selectModel(ctx, model, llm.ScenarioAnalyze)

	if err := w.applyRateLimit(ctx, model); err != nil {
		return err
	}

	timer := w.startMetricsTimer(model, "analyze")
	err := w.client.Analyze(ctx, prompt, target)
	w.recordMetricsForAnalyze(timer, model, prompt, err)

	return err
}

// applyTimeout applies the configured timeout to the context.
// Returns the timeout context and its cancel function.
// The caller must arrange for cancel to be called (typically via defer).
func (w *enhancedBrainWrapper) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if w.timeout > 0 {
		return context.WithTimeout(ctx, w.timeout)
	}
	return ctx, func() {}
}

// selectModel selects a model using the router, or falls back to default.
func (w *enhancedBrainWrapper) selectModel(ctx context.Context, model string, scenario llm.Scenario) string {
	if model != "" || w.router == nil {
		if model == "" {
			model = w.config.Model.Model
		}
		return model
	}

	if scenario == "" {
		scenario = w.router.DetectScenario("")
	}

	strategy := llm.StrategyCostPriority
	if w.router.GetDefaultStrategy() != "" {
		strategy = w.router.GetDefaultStrategy()
	}

	selectedModel, err := w.router.SelectModel(ctx, scenario, strategy)
	if err == nil {
		model = selectedModel.Name
	} else if w.logger != nil {
		w.logger.Warn("Model selection failed, using default", "error", err)
		model = w.config.Model.Model
	}

	return model
}

// applyRateLimit applies rate limiting for the specified model.
func (w *enhancedBrainWrapper) applyRateLimit(ctx context.Context, model string) error {
	if w.rateLimiter != nil {
		return w.rateLimiter.WaitModel(ctx, model)
	}
	return nil
}

// startMetricsTimer starts a metrics timer for the given model and operation.
func (w *enhancedBrainWrapper) startMetricsTimer(model, operation string) *llm.RequestTimer {
	if w.metrics != nil {
		return llm.NewRequestTimer(w.metrics, model, operation)
	}
	return nil
}

// recordMetrics records metrics for a chat operation.
func (w *enhancedBrainWrapper) recordMetrics(timer *llm.RequestTimer, model, prompt, result string, err error) {
	if timer == nil {
		return
	}

	inputTokens := 0
	outputTokens := 0
	cost := 0.0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
		outputTokens = w.costCalculator.CountTokens(result)
		cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
		_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, outputTokens)
	}
	timer.Record(int64(inputTokens), int64(outputTokens), cost, err)
}

// recordMetricsForAnalyze records metrics for an analyze operation.
func (w *enhancedBrainWrapper) recordMetricsForAnalyze(timer *llm.RequestTimer, model, prompt string, err error) {
	if timer == nil {
		return
	}

	inputTokens := 0
	cost := 0.0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
		cost, _ = w.costCalculator.CalculateCost(model, inputTokens, 100)
		_, _, _ = w.costCalculator.TrackRequest("default", model, inputTokens, 100)
	}
	timer.Record(int64(inputTokens), 100, cost, err)
}

func (w *enhancedBrainWrapper) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	// Apply pre-computed timeout from config
	if w.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.timeout)
		defer cancel()
	}

	// Select model and apply rate limiting
	model := w.selectModel(ctx, "", llm.ScenarioChat)
	if err := w.applyRateLimit(ctx, model); err != nil {
		return nil, err
	}

	// Start metrics timer
	timer := w.startMetricsTimer(model, "chat_stream")
	inputTokens := 0
	if w.costCalculator != nil {
		inputTokens = w.costCalculator.CountTokens(prompt)
	}

	// Get the stream from client
	stream, err := w.client.ChatStream(ctx, prompt)
	if err != nil {
		// Record metrics on error
		if timer != nil {
			timer.Record(int64(inputTokens), 0, 0, err)
		}
		return nil, err
	}

	// Wrap the stream to track output tokens and respect context cancellation
	outputChan := make(chan string)

	go func() {
		defer close(outputChan)
		if stream == nil {
			return
		}
		var outputTokens int
		for {
			select {
			case <-ctx.Done():
				return
			case token, ok := <-stream:
				if !ok {
					// Stream closed - record final metrics
					if timer != nil {
						cost := 0.0
						if w.costCalculator != nil {
							cost, _ = w.costCalculator.CalculateCost(model, inputTokens, outputTokens)
							_, _, _ = w.costCalculator.TrackRequest("stream", model, inputTokens, outputTokens)
						}
						timer.Record(int64(inputTokens), int64(outputTokens), cost, nil)
					}
					return
				}
				if w.costCalculator != nil {
					outputTokens += w.costCalculator.CountTokens(token)
				}
				select {
				case outputChan <- token:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return outputChan, nil
}

func (w *enhancedBrainWrapper) HealthCheck(ctx context.Context) HealthStatus {
	return w.client.HealthCheck(ctx)
}

func (w *enhancedBrainWrapper) GetMetrics() llm.MetricsStats {
	if w.metrics == nil {
		return llm.MetricsStats{}
	}
	return w.metrics.GetStats()
}

func (w *enhancedBrainWrapper) GetCostCalculator() *llm.CostCalculator {
	return w.costCalculator
}

func (w *enhancedBrainWrapper) GetRouter() *llm.Router {
	return w.router
}

func (w *enhancedBrainWrapper) GetRateLimiter() *llm.RateLimiter {
	return w.rateLimiter
}
