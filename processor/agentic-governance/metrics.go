// Package agenticgovernance provides Prometheus metrics for agentic-governance component.
package agenticgovernance

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// governanceMetrics holds Prometheus metrics for the agentic-governance component.
type governanceMetrics struct {
	// Filter invocations by filter name and result
	filterTotal *prometheus.CounterVec

	// Filter processing latency
	filterLatency *prometheus.HistogramVec

	// Violations by type and severity
	violationTotal *prometheus.CounterVec

	// PII detections by type
	piiDetected *prometheus.CounterVec

	// Injection attempts blocked by pattern
	injectionBlocked *prometheus.CounterVec

	// Content moderation actions by policy and action
	contentModerated *prometheus.CounterVec

	// Rate limit exceeded events by limit type
	rateLimitExceeded *prometheus.CounterVec

	// Messages processed by type and result
	messagesProcessed *prometheus.CounterVec

	// Embedding-classifier decisions by verdict and signal bucket
	// (ADR-043 Phase 2). Signal label is constrained to the ADR-043
	// enum + "unknown" + "no_match" so cardinality is bounded.
	classifierDecisions *prometheus.CounterVec

	// Embedding-classifier similarity scores. Buckets concentrate
	// near 1.0 because cosine similarity above 0.5 is the
	// interesting range.
	classifierScore prometheus.Histogram
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *governanceMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *governanceMetrics {
	metricsOnce.Do(func() {
		metrics = &governanceMetrics{
			filterTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "filter_total",
				Help:      "Total filter invocations by filter name and result",
			}, []string{"filter", "result"}),

			filterLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "filter_latency_seconds",
				Help:      "Filter processing latency in seconds",
				Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
			}, []string{"filter"}),

			violationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "violation_total",
				Help:      "Total policy violations by filter and severity",
			}, []string{"filter", "severity"}),

			piiDetected: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "pii_detected_total",
				Help:      "PII detections by type",
			}, []string{"pii_type"}),

			injectionBlocked: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "injection_blocked_total",
				Help:      "Injection attempts blocked by pattern name",
			}, []string{"pattern_name"}),

			contentModerated: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "content_moderated_total",
				Help:      "Content moderation actions by policy and action",
			}, []string{"policy", "action"}),

			rateLimitExceeded: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "rate_limit_exceeded_total",
				Help:      "Rate limit exceeded events by limit type",
			}, []string{"limit_type"}),

			messagesProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "messages_processed_total",
				Help:      "Messages processed by type and result",
			}, []string{"message_type", "result"}),

			classifierDecisions: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "injection_classifier_decisions_total",
				Help:      "Injection classifier decisions by verdict (allow/shadow/block) and signal bucket",
			}, []string{"verdict", "signal"}),

			classifierScore: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "governance",
				Name:      "injection_classifier_score",
				Help:      "Best-match cosine similarity score per classifier invocation",
				Buckets:   []float64{0.1, 0.3, 0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 0.99},
			}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounterVec("agentic-governance", "filter_total", metrics.filterTotal)
			_ = registry.RegisterHistogramVec("agentic-governance", "filter_latency_seconds", metrics.filterLatency)
			_ = registry.RegisterCounterVec("agentic-governance", "violation_total", metrics.violationTotal)
			_ = registry.RegisterCounterVec("agentic-governance", "pii_detected_total", metrics.piiDetected)
			_ = registry.RegisterCounterVec("agentic-governance", "injection_blocked_total", metrics.injectionBlocked)
			_ = registry.RegisterCounterVec("agentic-governance", "content_moderated_total", metrics.contentModerated)
			_ = registry.RegisterCounterVec("agentic-governance", "rate_limit_exceeded_total", metrics.rateLimitExceeded)
			_ = registry.RegisterCounterVec("agentic-governance", "messages_processed_total", metrics.messagesProcessed)
			_ = registry.RegisterCounterVec("agentic-governance", "injection_classifier_decisions_total", metrics.classifierDecisions)
			_ = registry.RegisterHistogram("agentic-governance", "injection_classifier_score", metrics.classifierScore)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.filterTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.filterLatency)
			_ = prometheus.DefaultRegisterer.Register(metrics.violationTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.piiDetected)
			_ = prometheus.DefaultRegisterer.Register(metrics.injectionBlocked)
			_ = prometheus.DefaultRegisterer.Register(metrics.contentModerated)
			_ = prometheus.DefaultRegisterer.Register(metrics.rateLimitExceeded)
			_ = prometheus.DefaultRegisterer.Register(metrics.messagesProcessed)
			_ = prometheus.DefaultRegisterer.Register(metrics.classifierDecisions)
			_ = prometheus.DefaultRegisterer.Register(metrics.classifierScore)
		}
	})
	return metrics
}

// recordClassifierDecision records a Phase 2 injection-classifier
// verdict. verdict is one of "allow", "shadow", "block"; signal is
// one of the ADR-043 buckets, "benign", "no_match", or "unknown".
func (m *governanceMetrics) recordClassifierDecision(verdict, signal string) {
	m.classifierDecisions.WithLabelValues(verdict, signal).Inc()
}

// recordClassifierScore records a similarity score sample.
func (m *governanceMetrics) recordClassifierScore(score float64) {
	m.classifierScore.Observe(score)
}

// recordFilterResult records filter invocation result.
func (m *governanceMetrics) recordFilterResult(filterName string, allowed bool) {
	result := "allowed"
	if !allowed {
		result = "blocked"
	}
	m.filterTotal.WithLabelValues(filterName, result).Inc()
}

// recordFilterLatency records filter processing time.
func (m *governanceMetrics) recordFilterLatency(filterName string, seconds float64) {
	m.filterLatency.WithLabelValues(filterName).Observe(seconds)
}

// recordViolation records a policy violation.
func (m *governanceMetrics) recordViolation(filterName string, severity Severity) {
	m.violationTotal.WithLabelValues(filterName, string(severity)).Inc()
}

// recordPIIDetected records a PII detection.
func (m *governanceMetrics) recordPIIDetected(piiType PIIType) {
	m.piiDetected.WithLabelValues(string(piiType)).Inc()
}

// recordInjectionBlocked records an injection attempt being blocked.
func (m *governanceMetrics) recordInjectionBlocked(patternName string) {
	m.injectionBlocked.WithLabelValues(patternName).Inc()
}

// recordContentModerated records a content moderation action.
func (m *governanceMetrics) recordContentModerated(policy string, action PolicyAction) {
	m.contentModerated.WithLabelValues(policy, string(action)).Inc()
}

// recordRateLimitExceeded records a rate limit exceeded event.
func (m *governanceMetrics) recordRateLimitExceeded(limitType string) {
	m.rateLimitExceeded.WithLabelValues(limitType).Inc()
}

// recordMessageProcessed records a message being processed.
func (m *governanceMetrics) recordMessageProcessed(msgType MessageType, allowed bool) {
	result := "allowed"
	if !allowed {
		result = "blocked"
	}
	m.messagesProcessed.WithLabelValues(string(msgType), result).Inc()
}
