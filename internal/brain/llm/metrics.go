package llm

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsCollector collects and exports LLM metrics via OpenTelemetry.
type MetricsCollector struct {
	meter metric.Meter

	// Request latency histogram (in milliseconds)
	requestLatency metric.Float64Histogram

	// Token usage counters
	inputTokenCounter  metric.Int64Counter
	outputTokenCounter metric.Int64Counter

	// Cost tracker
	costCounter metric.Float64Counter

	// Error rate counter
	errorCounter metric.Int64Counter

	// Routing decision counter
	routingCounter metric.Int64Counter

	// Active requests gauge
	activeRequestsGauge metric.Int64UpDownCounter

	// Local counters for aggregation (fallback if OTel not available)
	mu                sync.RWMutex
	totalInputTokens  int64
	totalOutputTokens int64
	totalCost         float64
	totalErrors       int64
	totalRequests     int64
	requestLatencies  []float64
	maxLatencySamples int
}

// MetricsConfig holds configuration for metrics collection.
type MetricsConfig struct {
	// Enabled enables metrics collection.
	Enabled bool
	// ServiceName is the name for the metrics service.
	ServiceName string
	// MaxLatencySamples is the max number of latency samples to keep locally.
	MaxLatencySamples int
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(config MetricsConfig) *MetricsCollector {
	mc := &MetricsCollector{
		maxLatencySamples: config.MaxLatencySamples,
		requestLatencies:  make([]float64, 0, config.MaxLatencySamples),
	}

	if !config.Enabled {
		return mc
	}

	// Initialize OpenTelemetry meter
	mc.meter = otel.Meter(config.ServiceName)

	var err error

	// Request latency histogram
	mc.requestLatency, err = mc.meter.Float64Histogram(
		"brain.request.latency.ms",
		metric.WithDescription("Request latency in milliseconds"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(10, 50, 100, 250, 500, 1000, 2500, 5000, 10000),
	)
	if err != nil {
		// Fallback to local tracking
		mc.meter = nil
	}

	// Input token counter
	mc.inputTokenCounter, err = mc.meter.Int64Counter(
		"brain.tokens.input",
		metric.WithDescription("Total input tokens processed"),
		metric.WithUnit("{tokens}"),
	)
	if err != nil {
		mc.meter = nil
	}

	// Output token counter
	mc.outputTokenCounter, err = mc.meter.Int64Counter(
		"brain.tokens.output",
		metric.WithDescription("Total output tokens generated"),
		metric.WithUnit("{tokens}"),
	)
	if err != nil {
		mc.meter = nil
	}

	// Cost counter
	mc.costCounter, err = mc.meter.Float64Counter(
		"brain.cost.usd",
		metric.WithDescription("Total cost in USD"),
		metric.WithUnit("USD"),
	)
	if err != nil {
		mc.meter = nil
	}

	// Error counter
	mc.errorCounter, err = mc.meter.Int64Counter(
		"brain.errors.total",
		metric.WithDescription("Total number of errors"),
		metric.WithUnit("{errors}"),
	)
	if err != nil {
		mc.meter = nil
	}

	// Routing decision counter
	mc.routingCounter, err = mc.meter.Int64Counter(
		"brain.routing.decisions",
		metric.WithDescription("Number of routing decisions"),
		metric.WithUnit("{decisions}"),
	)
	if err != nil {
		mc.meter = nil
	}

	// Active requests gauge
	mc.activeRequestsGauge, err = mc.meter.Int64UpDownCounter(
		"brain.requests.active",
		metric.WithDescription("Number of active requests"),
		metric.WithUnit("{requests}"),
	)
	if err != nil {
		mc.meter = nil
	}

	return mc
}

// RecordRequest records a completed request with metrics.
func (mc *MetricsCollector) RecordRequest(
	model string,
	scenario string,
	inputTokens int64,
	outputTokens int64,
	cost float64,
	latencyMs float64,
	err error,
) {
	mc.mu.Lock()
	mc.totalRequests++
	if err != nil {
		mc.totalErrors++
	}
	mc.totalInputTokens += inputTokens
	mc.totalOutputTokens += outputTokens
	mc.totalCost += cost

	// Keep rolling window of latencies
	if len(mc.requestLatencies) >= mc.maxLatencySamples {
		mc.requestLatencies = append(mc.requestLatencies[1:], latencyMs)
	} else {
		mc.requestLatencies = append(mc.requestLatencies, latencyMs)
	}
	mc.mu.Unlock()

	// Record to OpenTelemetry if available
	if mc.meter != nil {
		attrs := []attribute.KeyValue{
			attribute.String("model", model),
			attribute.String("scenario", scenario),
		}

		if err != nil {
			attrs = append(attrs, attribute.Bool("error", true))
		}

		// Record latency
		mc.requestLatency.Record(context.Background(), latencyMs, metric.WithAttributes(attrs...))

		// Record tokens
		mc.inputTokenCounter.Add(context.Background(), inputTokens, metric.WithAttributes(attrs...))
		mc.outputTokenCounter.Add(context.Background(), outputTokens, metric.WithAttributes(attrs...))

		// Record cost
		if cost > 0 {
			mc.costCounter.Add(context.Background(), cost, metric.WithAttributes(attrs...))
		}

		// Record error
		if err != nil {
			mc.errorCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
		}
	}
}

// RecordRoutingDecision records a routing decision.
func (mc *MetricsCollector) RecordRoutingDecision(scenario Scenario, strategy RouteStrategy, model string) {
	if mc.meter != nil {
		attrs := []attribute.KeyValue{
			attribute.String("scenario", string(scenario)),
			attribute.String("strategy", string(strategy)),
			attribute.String("model", model),
		}
		mc.routingCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	}
}

// StartRequest increments the active request counter.
func (mc *MetricsCollector) StartRequest() {
	if mc.meter != nil {
		mc.activeRequestsGauge.Add(context.Background(), 1)
	}
}

// EndRequest decrements the active request counter.
func (mc *MetricsCollector) EndRequest() {
	if mc.meter != nil {
		mc.activeRequestsGauge.Add(context.Background(), -1)
	}
}

// GetStats returns current metrics statistics.
func (mc *MetricsCollector) GetStats() MetricsStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Calculate average latency
	var avgLatency float64
	if len(mc.requestLatencies) > 0 {
		var sum float64
		for _, l := range mc.requestLatencies {
			sum += l
		}
		avgLatency = sum / float64(len(mc.requestLatencies))
	}

	// Calculate error rate
	errorRate := 0.0
	if mc.totalRequests > 0 {
		errorRate = float64(mc.totalErrors) / float64(mc.totalRequests)
	}

	return MetricsStats{
		TotalRequests:     mc.totalRequests,
		TotalInputTokens:  mc.totalInputTokens,
		TotalOutputTokens: mc.totalOutputTokens,
		TotalCost:         mc.totalCost,
		TotalErrors:       mc.totalErrors,
		ErrorRate:         errorRate,
		AvgLatencyMs:      avgLatency,
		ActiveRequests:    0, // Would need separate tracking
	}
}

// MetricsStats represents aggregated metrics statistics.
type MetricsStats struct {
	TotalRequests     int64
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCost         float64
	TotalErrors       int64
	ErrorRate         float64
	AvgLatencyMs      float64
	ActiveRequests    int64
}

// RequestTimer helps track request timing.
type RequestTimer struct {
	startTime time.Time
	model     string
	scenario  string
	metrics   *MetricsCollector
}

// NewRequestTimer creates a new request timer.
func NewRequestTimer(metrics *MetricsCollector, model, scenario string) *RequestTimer {
	if metrics != nil {
		metrics.StartRequest()
	}
	return &RequestTimer{
		startTime: time.Now(),
		model:     model,
		scenario:  scenario,
		metrics:   metrics,
	}
}

// Record completes the timer and records metrics.
func (rt *RequestTimer) Record(inputTokens, outputTokens int64, cost float64, err error) {
	if rt.metrics == nil {
		return
	}

	latencyMs := float64(time.Since(rt.startTime).Milliseconds())
	rt.metrics.RecordRequest(rt.model, rt.scenario, inputTokens, outputTokens, cost, latencyMs, err)
	rt.metrics.EndRequest()
}

// MetricsClient wraps an LLM client with metrics collection.
type MetricsClient struct {
	client  LLMClient
	metrics *MetricsCollector
	model   string
}

// Client returns the underlying client for component extraction.
func (m *MetricsClient) Client() LLMClient {
	return m.client
}

// NewMetricsClient creates a new metrics-enabled client wrapper.
func NewMetricsClient(client LLMClient, metrics *MetricsCollector, model string) *MetricsClient {
	return &MetricsClient{
		client:  client,
		metrics: metrics,
		model:   model,
	}
}

// Chat implements the Chat method with metrics collection.
func (m *MetricsClient) Chat(ctx context.Context, prompt string) (string, error) {
	timer := NewRequestTimer(m.metrics, m.model, "chat")
	result, err := m.client.Chat(ctx, prompt)
	timer.Record(0, 0, 0, err) // Token counts estimated elsewhere
	return result, err
}

// Analyze implements the Analyze method with metrics collection.
func (m *MetricsClient) Analyze(ctx context.Context, prompt string, target any) error {
	timer := NewRequestTimer(m.metrics, m.model, "analyze")
	err := m.client.Analyze(ctx, prompt, target)
	timer.Record(0, 0, 0, err)
	return err
}

// ChatStream implements the ChatStream method with metrics collection.
func (m *MetricsClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	timer := NewRequestTimer(m.metrics, m.model, "stream")
	result, err := m.client.ChatStream(ctx, prompt)
	timer.Record(0, 0, 0, err)
	return result, err
}

// HealthCheck implements the HealthCheck method.
func (m *MetricsClient) HealthCheck(ctx context.Context) HealthStatus {
	return m.client.HealthCheck(ctx)
}

// GetMetrics returns the underlying metrics collector for stats retrieval.
func (m *MetricsClient) GetMetrics() *MetricsCollector {
	return m.metrics
}
