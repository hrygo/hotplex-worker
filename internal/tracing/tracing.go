package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracer is the global tracer instance. Implements trace.Tracer.
// nil means tracing is disabled.
var Tracer trace.Tracer

var (
	initOnce sync.Once
	tp       *sdktrace.TracerProvider // nil if disabled
)

// Init initializes the OpenTelemetry tracer.
// If OTEL_SDK_DISABLED=true or no OTEL_EXPORTER_OTLP_ENDPOINT is set,
// a no-op tracer is installed and this function logs a warning.
// Init is idempotent — subsequent calls are no-ops.
func Init(ctx context.Context, log *slog.Logger, serviceName string) {
	initOnce.Do(func() {
		if os.Getenv("OTEL_SDK_DISABLED") == "true" {
			Tracer = noopTracer()
			log.Info("tracing: disabled via OTEL_SDK_DISABLED=true")
			return
		}

		endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		if endpoint == "" {
			// No endpoint configured — install noop tracer (don't fail startup)
			Tracer = noopTracer()
			log.Info("tracing: no OTEL_EXPORTER_OTLP_ENDPOINT set, tracing disabled")
			return
		}

		// Use stdout exporter for local development; in production replace with otlptrace exporter.
		// stdout is safe for containers (JSON lines to stdout → log collector).
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			log.Warn("tracing: stdout exporter creation failed, disabling", "err", err)
			Tracer = noopTracer()
			return
		}

		res, err := resource.New(ctx,
			resource.WithAttributes(
				semconv.ServiceName(serviceName),
				semconv.ServiceVersion("1.0.0"),
			),
		)
		if err != nil {
			log.Warn("tracing: resource creation failed, disabling", "err", err)
			Tracer = noopTracer()
			return
		}

		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exp),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)

		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		Tracer = tp.Tracer(serviceName)
		log.Info("tracing: initialized", "endpoint", endpoint)
	})
}

// Shutdown flushes and shuts down the tracer provider. Call during graceful shutdown.
func Shutdown(ctx context.Context) error {
	if tp != nil {
		return tp.Shutdown(ctx)
	}
	return nil
}

func noopTracer() trace.Tracer {
	return noop.NewTracerProvider().Tracer("hotplex-worker")
}

// SpanFromContext returns Tracer if non-nil, otherwise returns a no-op tracer.
func SpanFromContext(ctx context.Context) trace.Tracer {
	if Tracer != nil {
		return Tracer
	}
	return noop.NewTracerProvider().Tracer("hotplex-worker")
}

// Attr returns a trace attribute for span attributes.
func Attr(key string, value any) attribute.KeyValue {
	switch v := value.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case bool:
		return attribute.Bool(key, v)
	default:
		return attribute.String(key, fmt.Sprintf("%v", v))
	}
}

// SpanStatusFromError sets span status to Error if err is non-nil, otherwise sets Ok.
// Returns true if err was non-nil.
func SpanStatusFromError(span trace.Span, err error) bool {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return true
	}
	span.SetStatus(codes.Ok, "")
	return false
}
