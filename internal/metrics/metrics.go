// Package metrics provides Prometheus metrics for the gateway.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// All metric vectors are registered once at package init via promauto.
// The registry is then used by the /admin/metrics handler in gateway.

var (
	// SessionsActive tracks the number of currently active sessions by state.
	SessionsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hotplex",
		Name:      "sessions_active",
		Help:      "Number of active sessions by state (created, running, idle)",
	}, []string{"state"})

	// SessionsTotal tracks total sessions created (counter).
	SessionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "sessions_total",
		Help:      "Total number of sessions created, labeled by worker_type",
	}, []string{"worker_type"})

	// SessionsTerminated tracks terminated sessions by reason.
	SessionsTerminated = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "sessions_terminated_total",
		Help:      "Total sessions terminated by reason (idle_timeout, max_lifetime, client_kill, admin_kill, zombie, crash)",
	}, []string{"reason"})

	// SessionsDeleted tracks deleted sessions (GC retention).
	SessionsDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "sessions_deleted_total",
		Help:      "Total sessions deleted by the retention GC",
	})

	// WorkersRunning tracks the number of active worker processes by type.
	WorkersRunning = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hotplex",
		Name:      "workers_running",
		Help:      "Number of currently running worker processes by type",
	}, []string{"worker_type"})

	// WorkerStartsTotal tracks worker process starts by type and result.
	WorkerStartsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "worker_starts_total",
		Help:      "Total worker process starts by type and result (success, failed)",
	}, []string{"worker_type", "result"})

	// WorkerExecDuration tracks worker execution duration in seconds.
	WorkerExecDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "hotplex",
		Name:      "worker_exec_duration_seconds",
		Help:      "Worker execution duration in seconds",
		Buckets:   []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800},
	}, []string{"worker_type"})

	// GatewayConnectionsOpen tracks currently open WebSocket connections.
	GatewayConnectionsOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "hotplex",
		Name:      "gateway_connections_open",
		Help:      "Number of currently open WebSocket connections",
	})

	// GatewayMessagesTotal tracks total messages sent/received.
	GatewayMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_messages_total",
		Help:      "Total WebSocket messages by direction and event type",
	}, []string{"direction", "event_type"})

	// GatewayEventsTotal tracks pass-through events forwarded by Handler.Handle.
	// AEP-011 (reasoning, step, permission_request, permission_response) and
	// AEP-012 (message, message.start, message.end) are counted here.
	GatewayEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_events_total",
		Help:      "Total pass-through gateway events by event type and direction",
	}, []string{"event_type", "direction"})

	// GatewayDeltasDropped tracks dropped message.delta events due to backpressure.
	GatewayDeltasDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_deltas_dropped_total",
		Help:      "Total message.delta events dropped due to backpressure",
	})

	// GatewayPlatformDroppedTotal tracks events dropped at the per-conn platform buffer level.
	GatewayPlatformDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_platform_dropped_total",
		Help:      "Events dropped at platform conn buffer level",
	}, []string{"event_type"})

	// GatewayDeltaCoalescedTotal tracks delta events merged by the coalescer.
	GatewayDeltaCoalescedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_delta_coalesced_total",
		Help:      "Number of delta events merged by coalescer",
	}, []string{"session_id"})

	// GatewayDeltaFlushTotal tracks merged delta flushes sent to platform conns.
	GatewayDeltaFlushTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_delta_flush_total",
		Help:      "Number of merged delta flushes sent to platform",
	}, []string{"session_id"})

	// GatewayErrorsTotal tracks errors by type.
	GatewayErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "gateway_errors_total",
		Help:      "Total gateway errors by error code",
	}, []string{"error_code"})

	// PoolAcquireTotal tracks pool acquisition attempts by result.
	PoolAcquireTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "pool_acquire_total",
		Help:      "Total pool acquire attempts by result (success, pool_exhausted, user_quota_exceeded)",
	}, []string{"result"})

	// PoolReleaseErrorsTotal tracks pool release calls without a corresponding acquire (double-release bugs).
	PoolReleaseErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "pool_release_errors_total",
		Help:      "Total pool release without acquire errors (indicates double-release bugs)",
	})

	// PoolUtilization tracks pool utilization percentage.
	PoolUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "hotplex",
		Name:      "pool_utilization_ratio",
		Help:      "Current pool utilization as a ratio (0-1) of max concurrent sessions",
	})

	// WorkerCrashesTotal tracks worker process crashes by type and exit code.
	// Used to compute crash rate SLO: 1 - crashes/(starts+crashes) >= 0.99.
	WorkerCrashesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hotplex",
		Name:      "worker_crashes_total",
		Help:      "Total worker process crashes by worker_type and exit_code",
	}, []string{"worker_type", "exit_code"})

	// WorkerMemoryBytes tracks estimated memory usage per worker type (set to RLIMIT_AS cap).
	// Actual RSS should be scraped via node_exporter on the worker host.
	WorkerMemoryBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hotplex",
		Name:      "worker_memory_bytes",
		Help:      "Estimated worker memory by worker_type (set to RLIMIT_AS limit per active worker)",
	}, []string{"worker_type"})
)
