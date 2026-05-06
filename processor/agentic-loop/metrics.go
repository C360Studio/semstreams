// Package agenticloop provides Prometheus metrics for agentic-loop component.
package agenticloop

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// loopMetrics holds Prometheus metrics for the agentic-loop component.
type loopMetrics struct {
	// Loop lifecycle
	loopsCreated   prometheus.Counter
	loopsCompleted prometheus.Counter
	loopsFailed    *prometheus.CounterVec
	loopsTimeout   prometheus.Counter
	activeLoops    prometheus.Gauge

	// Iterations
	iterationsTotal   prometheus.Counter
	iterationsPerLoop prometheus.Histogram

	// Duration
	loopDuration *prometheus.HistogramVec

	// Trajectory
	trajectorySteps *prometheus.CounterVec

	// Tool calls
	toolCallsDispatched *prometheus.CounterVec
	toolResultsReceived *prometheus.CounterVec
	toolResultsDropped  *prometheus.CounterVec

	// Token usage per LLM request
	requestTokensIn  prometheus.Histogram
	requestTokensOut prometheus.Histogram

	// Tool result truncation
	toolResultsTruncated prometheus.Counter

	// Context management
	contextUtilization           prometheus.Gauge
	contextCompactionsTotal      prometheus.Counter
	contextCompactionTokensSaved prometheus.Histogram
	contextCompactedRegionTokens prometheus.Gauge

	// Boid coordination
	boidSignalsReceived  *prometheus.CounterVec
	boidPositionUpdates  prometheus.Counter
	boidEntitiesFiltered prometheus.Counter
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *loopMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *loopMetrics {
	metricsOnce.Do(func() {
		metrics = &loopMetrics{
			loopsCreated: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "loops_created_total",
				Help:      "Total number of agentic loops created",
			}),

			loopsCompleted: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "loops_completed_total",
				Help:      "Total number of agentic loops completed successfully",
			}),

			loopsFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "loops_failed_total",
				Help:      "Total number of agentic loops that failed",
			}, []string{"reason"}),

			loopsTimeout: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "loops_timeout_total",
				Help:      "Total number of agentic loops that timed out",
			}),

			activeLoops: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "active_loops",
				Help:      "Number of currently active agentic loops",
			}),

			iterationsTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "iterations_total",
				Help:      "Total number of iterations across all loops",
			}),

			iterationsPerLoop: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "iterations_per_loop",
				Help:      "Distribution of iterations per loop",
				Buckets:   []float64{1, 2, 3, 5, 10, 15, 20, 50},
			}),

			loopDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "duration_seconds",
				Help:      "Duration of agentic loops in seconds",
				Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
			}, []string{"status"}),

			trajectorySteps: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "trajectory_steps_total",
				Help:      "Total trajectory steps by type",
			}, []string{"step_type"}),

			toolCallsDispatched: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_calls_dispatched_total",
				Help:      "Total tool calls dispatched by tool name",
			}, []string{"tool_name"}),

			toolResultsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_results_received_total",
				Help:      "Total tool results received by status",
			}, []string{"status"}),

			toolResultsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_results_dropped_total",
				Help:      "Total tool results dropped at the wire because no loop mapping exists for the CallID. Sustained non-zero rate points at NATS redelivery or executor double-publish.",
			}, []string{"reason"}),

			boidSignalsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "boid_signals_received_total",
				Help:      "Total Boid steering signals received by type",
			}, []string{"signal_type"}),

			boidPositionUpdates: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "boid_position_updates_total",
				Help:      "Total Boid position updates in KV store",
			}),

			requestTokensIn: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "request_tokens_in",
				Help:      "Prompt tokens per LLM request",
				Buckets:   prometheus.ExponentialBuckets(100, 2, 12), // 100 to ~400k
			}),

			requestTokensOut: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "request_tokens_out",
				Help:      "Completion tokens per LLM request",
				Buckets:   prometheus.ExponentialBuckets(10, 2, 12), // 10 to ~40k
			}),

			toolResultsTruncated: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_results_truncated_total",
				Help:      "Total tool results truncated because they exceeded tool_result_max_bytes",
			}),

			contextUtilization: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "context_utilization",
				Help:      "Current context window utilization (0.0-1.0)",
			}),

			contextCompactionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "context_compactions_total",
				Help:      "Total number of context compactions performed",
			}),

			contextCompactionTokensSaved: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "context_compaction_tokens_saved",
				Help:      "Tokens saved per compaction (evicted minus summary)",
				Buckets:   prometheus.ExponentialBuckets(100, 2, 10), // 100 to ~100k
			}),

			contextCompactedRegionTokens: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "context_compacted_region_tokens",
				Help:      "Current tokens in compacted history region",
			}),

			boidEntitiesFiltered: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "boid_entities_filtered_total",
				Help:      "Total times entities were reordered by Boid steering",
			}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounter("agentic-loop", "loops_created_total", metrics.loopsCreated)
			_ = registry.RegisterCounter("agentic-loop", "loops_completed_total", metrics.loopsCompleted)
			_ = registry.RegisterCounterVec("agentic-loop", "loops_failed_total", metrics.loopsFailed)
			_ = registry.RegisterCounter("agentic-loop", "loops_timeout_total", metrics.loopsTimeout)
			_ = registry.RegisterGauge("agentic-loop", "active_loops", metrics.activeLoops)
			_ = registry.RegisterCounter("agentic-loop", "iterations_total", metrics.iterationsTotal)
			_ = registry.RegisterHistogram("agentic-loop", "iterations_per_loop", metrics.iterationsPerLoop)
			_ = registry.RegisterHistogramVec("agentic-loop", "duration_seconds", metrics.loopDuration)
			_ = registry.RegisterCounterVec("agentic-loop", "trajectory_steps_total", metrics.trajectorySteps)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_calls_dispatched_total", metrics.toolCallsDispatched)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_results_received_total", metrics.toolResultsReceived)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_results_dropped_total", metrics.toolResultsDropped)
			_ = registry.RegisterCounterVec("agentic-loop", "boid_signals_received_total", metrics.boidSignalsReceived)
			_ = registry.RegisterCounter("agentic-loop", "boid_position_updates_total", metrics.boidPositionUpdates)
			_ = registry.RegisterCounter("agentic-loop", "boid_entities_filtered_total", metrics.boidEntitiesFiltered)
			_ = registry.RegisterHistogram("agentic-loop", "request_tokens_in", metrics.requestTokensIn)
			_ = registry.RegisterHistogram("agentic-loop", "request_tokens_out", metrics.requestTokensOut)
			_ = registry.RegisterCounter("agentic-loop", "tool_results_truncated_total", metrics.toolResultsTruncated)
			_ = registry.RegisterGauge("agentic-loop", "context_utilization", metrics.contextUtilization)
			_ = registry.RegisterCounter("agentic-loop", "context_compactions_total", metrics.contextCompactionsTotal)
			_ = registry.RegisterHistogram("agentic-loop", "context_compaction_tokens_saved", metrics.contextCompactionTokensSaved)
			_ = registry.RegisterGauge("agentic-loop", "context_compacted_region_tokens", metrics.contextCompactedRegionTokens)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.loopsCreated)
			_ = prometheus.DefaultRegisterer.Register(metrics.loopsCompleted)
			_ = prometheus.DefaultRegisterer.Register(metrics.loopsFailed)
			_ = prometheus.DefaultRegisterer.Register(metrics.loopsTimeout)
			_ = prometheus.DefaultRegisterer.Register(metrics.activeLoops)
			_ = prometheus.DefaultRegisterer.Register(metrics.iterationsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.iterationsPerLoop)
			_ = prometheus.DefaultRegisterer.Register(metrics.loopDuration)
			_ = prometheus.DefaultRegisterer.Register(metrics.trajectorySteps)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolCallsDispatched)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsReceived)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsDropped)
			_ = prometheus.DefaultRegisterer.Register(metrics.boidSignalsReceived)
			_ = prometheus.DefaultRegisterer.Register(metrics.boidPositionUpdates)
			_ = prometheus.DefaultRegisterer.Register(metrics.boidEntitiesFiltered)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestTokensIn)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestTokensOut)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsTruncated)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextUtilization)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactionsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactionTokensSaved)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactedRegionTokens)
		}
	})
	return metrics
}

// recordLoopCreated increments the loops created counter and active gauge.
func (m *loopMetrics) recordLoopCreated() {
	m.loopsCreated.Inc()
	m.activeLoops.Inc()
}

// recordLoopCompleted records a successful loop completion.
func (m *loopMetrics) recordLoopCompleted(iterations int, durationSeconds float64) {
	m.loopsCompleted.Inc()
	m.activeLoops.Dec()
	m.iterationsPerLoop.Observe(float64(iterations))
	m.loopDuration.WithLabelValues("completed").Observe(durationSeconds)
}

// recordLoopFailed records a failed loop.
func (m *loopMetrics) recordLoopFailed(reason string, iterations int, durationSeconds float64) {
	m.loopsFailed.WithLabelValues(reason).Inc()
	m.activeLoops.Dec()
	m.iterationsPerLoop.Observe(float64(iterations))
	m.loopDuration.WithLabelValues("failed").Observe(durationSeconds)
}

// recordLoopTimeout records a loop timeout.
func (m *loopMetrics) recordLoopTimeout(iterations int, durationSeconds float64) {
	m.loopsTimeout.Inc()
	m.activeLoops.Dec()
	m.iterationsPerLoop.Observe(float64(iterations))
	m.loopDuration.WithLabelValues("timeout").Observe(durationSeconds)
}

// recordIteration increments the total iterations counter.
func (m *loopMetrics) recordIteration() {
	m.iterationsTotal.Inc()
}

// recordTrajectoryStep records a trajectory step by type.
func (m *loopMetrics) recordTrajectoryStep(stepType string) {
	m.trajectorySteps.WithLabelValues(stepType).Inc()
}

// recordToolCallDispatched records a tool call being dispatched.
func (m *loopMetrics) recordToolCallDispatched(toolName string) {
	m.toolCallsDispatched.WithLabelValues(toolName).Inc()
}

// recordToolResultReceived records a tool result being received.
func (m *loopMetrics) recordToolResultReceived(hasError bool) {
	status := "success"
	if hasError {
		status = "error"
	}
	m.toolResultsReceived.WithLabelValues(status).Inc()
}

// recordToolResultDropped records a tool result that arrived with no loop
// mapping for its CallID. Reason "stale_callid" is the dominant case after
// GetAndClearToolResults eviction — a re-delivered result for an already-
// drained call. A sustained non-zero rate points at NATS redelivery or an
// executor double-publishing.
func (m *loopMetrics) recordToolResultDropped(reason string) {
	m.toolResultsDropped.WithLabelValues(reason).Inc()
}

// recordBoidSignalReceived records a Boid steering signal received.
func (m *loopMetrics) recordBoidSignalReceived(signalType string) {
	m.boidSignalsReceived.WithLabelValues(signalType).Inc()
}

// recordBoidPositionUpdate records a Boid position update.
func (m *loopMetrics) recordBoidPositionUpdate() {
	m.boidPositionUpdates.Inc()
}

// recordBoidEntitiesFiltered records entity filtering by Boid steering.
func (m *loopMetrics) recordBoidEntitiesFiltered() {
	m.boidEntitiesFiltered.Inc()
}

// recordRequestTokens records prompt and completion token counts for an LLM request.
func (m *loopMetrics) recordRequestTokens(tokensIn, tokensOut int) {
	if tokensIn > 0 {
		m.requestTokensIn.Observe(float64(tokensIn))
	}
	if tokensOut > 0 {
		m.requestTokensOut.Observe(float64(tokensOut))
	}
}

// recordToolResultTruncated increments the truncation counter.
func (m *loopMetrics) recordToolResultTruncated() {
	m.toolResultsTruncated.Inc()
}

// recordContextUtilization updates the current context utilization gauge.
func (m *loopMetrics) recordContextUtilization(utilization float64) {
	m.contextUtilization.Set(utilization)
}

// recordContextCompaction records a compaction event and tokens saved.
func (m *loopMetrics) recordContextCompaction(tokensSaved int) {
	m.contextCompactionsTotal.Inc()
	m.contextCompactionTokensSaved.Observe(float64(tokensSaved))
}

// recordCompactedRegionTokens updates the compacted region tokens gauge.
func (m *loopMetrics) recordCompactedRegionTokens(tokens int) {
	m.contextCompactedRegionTokens.Set(float64(tokens))
}
