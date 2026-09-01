// Package agenticdispatch provides Prometheus metrics for agentic-dispatch component.
package agenticdispatch

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// routerMetrics holds Prometheus metrics for the router component.
type routerMetrics struct {
	messagesReceived    *prometheus.CounterVec
	commandsExecuted    *prometheus.CounterVec
	tasksSubmitted      prometheus.Counter
	activeLoops         prometheus.Gauge
	routingDuration     prometheus.Histogram
	completionsReceived *prometheus.CounterVec
	terminalSettlements *prometheus.CounterVec

	// HTTP endpoint metrics
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec

	// Loop signal metrics
	loopSignalsSent *prometheus.CounterVec

	// Loop approval metrics — counts approval submissions through the
	// HTTP /loops/{id}/approval endpoint by decision and outcome.
	loopApprovalsSubmitted *prometheus.CounterVec

	// Loop admission metrics — every request naming an existing loop that
	// the one admission gate refused, by the seam it arrived on and the
	// single mapped refusal reason (loopAdmissionMetricReason).
	loopAdmissionRefusals *prometheus.CounterVec

	// SSE metrics
	sseConnectionsActive prometheus.Gauge
	sseEventsTotal       *prometheus.CounterVec
	sseErrorsTotal       *prometheus.CounterVec

	// Shared AGENT_LOOPS activity view metrics (ADR-081, graph-view-
	// subscription task 2.5) — fed by graphview hooks in activityViewHooks.
	activityViewCaughtUp            prometheus.Gauge
	activityViewAppliedRevision     prometheus.Gauge
	activityViewSubscribers         prometheus.Gauge
	activityViewMaxPendingKeys      prometheus.Gauge
	activityViewPoisonedTotal       prometheus.Counter
	activityViewCoalescedDropsTotal prometheus.Counter
	activityViewWatcherLostTotal    prometheus.Counter
}

// Package-level metrics cache (keyed by registry to allow test isolation)
var (
	metricsMu    sync.Mutex
	metricsCache = make(map[*metric.MetricsRegistry]*routerMetrics)
	nilMetrics   *routerMetrics
	nilOnce      sync.Once
)

// getMetrics returns the metrics instance for the given registry, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *routerMetrics {
	// Special handling for nil registry (production use with default Prometheus registry)
	if registry == nil {
		nilOnce.Do(func() {
			nilMetrics = createAndRegisterMetrics(nil)
		})
		return nilMetrics
	}

	// For non-nil registries, create per-registry instances (test isolation)
	metricsMu.Lock()
	defer metricsMu.Unlock()

	if m, exists := metricsCache[registry]; exists {
		return m
	}

	m := createAndRegisterMetrics(registry)
	metricsCache[registry] = m
	return m
}

// createAndRegisterMetrics creates a new metrics instance and registers it.
func createAndRegisterMetrics(registry *metric.MetricsRegistry) *routerMetrics {
	m := &routerMetrics{
		messagesReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "messages_received_total",
			Help:      "Total number of user messages received by channel type",
		}, []string{"channel_type"}),

		commandsExecuted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "commands_executed_total",
			Help:      "Total number of commands executed",
		}, []string{"command"}),

		tasksSubmitted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "tasks_submitted_total",
			Help:      "Total number of tasks submitted to agentic loops",
		}),

		activeLoops: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "active_loops",
			Help:      "Number of currently active agentic loops",
		}),

		routingDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "routing_duration_seconds",
			Help:      "Duration of message routing operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}),

		completionsReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "completions_received_total",
			Help:      "Total number of agent completions received by status",
		}, []string{"status"}),

		terminalSettlements: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "terminal_settlement_total",
			Help:      "Terminal settlement attempts by fixed bounded reason",
		}, []string{"reason"}),

		// HTTP endpoint metrics
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests by endpoint and status",
		}, []string{"endpoint", "method", "status"}),

		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		}, []string{"endpoint", "method"}),

		// Loop signal metrics
		loopSignalsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "loop_signals_sent_total",
			Help:      "Total number of loop control signals sent",
		}, []string{"signal_type", "accepted"}),

		// Loop approval metrics
		loopApprovalsSubmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "loop_approvals_submitted_total",
			Help:      "Total number of approval responses submitted via HTTP, labelled by decision and submission outcome (status: success/error)",
		}, []string{"decision", "status"}),

		// Loop admission metrics
		loopAdmissionRefusals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "loop_admission_refusals_total",
			Help:      "Requests naming an existing loop refused by the admission gate, by seam and reason (form_malformed, existence_absent, existence_unreadable, existence_conflict, state_terminal, ownership_not_owner, ownership_not_permitted)",
		}, []string{"seam", "reason"}),

		// SSE metrics
		sseConnectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_connections_active",
			Help:      "Number of active SSE connections",
		}),

		sseEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_events_total",
			Help:      "Total number of SSE events sent by type",
		}, []string{"event_type"}),

		sseErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "sse_errors_total",
			Help:      "Total number of SSE errors by type",
		}, []string{"error_type"}),

		// Shared AGENT_LOOPS activity view metrics
		activityViewCaughtUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_caught_up",
			Help:      "1 when the shared AGENT_LOOPS activity view is caught up and its watcher healthy, 0 while bootstrapping or after watcher loss (staleness signal)",
		}),

		activityViewAppliedRevision: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_applied_revision",
			Help:      "Highest AGENT_LOOPS KV revision applied by the shared activity view (watermark)",
		}),

		activityViewSubscribers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_subscribers",
			Help:      "Number of SSE subscriptions attached to the shared activity view",
		}),

		activityViewMaxPendingKeys: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_max_pending_keys",
			Help:      "Largest per-subscriber pending-delta buffer observed at the last fan-out window (slow-client backlog)",
		}),

		activityViewPoisonedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_poisoned_total",
			Help:      "Total AGENT_LOOPS writes that failed validating decode and were surfaced as per-key poison (G6)",
		}),

		activityViewCoalescedDropsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_coalesced_drops_total",
			Help:      "Total pending deltas overwritten before delivery across subscribers (at-most-once coalescing on slow clients)",
		}),

		activityViewWatcherLostTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "router",
			Name:      "activity_view_watcher_lost_total",
			Help:      "Total losses of the shared AGENT_LOOPS view watcher (each fails closed and requires re-bootstrap)",
		}),
	}

	// Register metrics with the metrics registry if available
	if registry != nil {
		_ = registry.RegisterCounterVec("router", "messages_received_total", m.messagesReceived)
		_ = registry.RegisterCounterVec("router", "commands_executed_total", m.commandsExecuted)
		_ = registry.RegisterCounter("router", "tasks_submitted_total", m.tasksSubmitted)
		_ = registry.RegisterGauge("router", "active_loops", m.activeLoops)
		_ = registry.RegisterHistogram("router", "routing_duration_seconds", m.routingDuration)
		_ = registry.RegisterCounterVec("router", "completions_received_total", m.completionsReceived)
		_ = registry.RegisterCounterVec("router", "terminal_settlement_total", m.terminalSettlements)
		_ = registry.RegisterCounterVec("router", "http_requests_total", m.httpRequestsTotal)
		_ = registry.RegisterHistogramVec("router", "http_request_duration_seconds", m.httpRequestDuration)
		_ = registry.RegisterCounterVec("router", "loop_signals_sent_total", m.loopSignalsSent)
		_ = registry.RegisterCounterVec("router", "loop_approvals_submitted_total", m.loopApprovalsSubmitted)
		_ = registry.RegisterCounterVec("router", "loop_admission_refusals_total", m.loopAdmissionRefusals)
		_ = registry.RegisterGauge("router", "sse_connections_active", m.sseConnectionsActive)
		_ = registry.RegisterCounterVec("router", "sse_events_total", m.sseEventsTotal)
		_ = registry.RegisterCounterVec("router", "sse_errors_total", m.sseErrorsTotal)
		_ = registry.RegisterGauge("router", "activity_view_caught_up", m.activityViewCaughtUp)
		_ = registry.RegisterGauge("router", "activity_view_applied_revision", m.activityViewAppliedRevision)
		_ = registry.RegisterGauge("router", "activity_view_subscribers", m.activityViewSubscribers)
		_ = registry.RegisterGauge("router", "activity_view_max_pending_keys", m.activityViewMaxPendingKeys)
		_ = registry.RegisterCounter("router", "activity_view_poisoned_total", m.activityViewPoisonedTotal)
		_ = registry.RegisterCounter("router", "activity_view_coalesced_drops_total", m.activityViewCoalescedDropsTotal)
		_ = registry.RegisterCounter("router", "activity_view_watcher_lost_total", m.activityViewWatcherLostTotal)
	} else {
		// Fallback to default prometheus registry for production
		_ = prometheus.DefaultRegisterer.Register(m.messagesReceived)
		_ = prometheus.DefaultRegisterer.Register(m.commandsExecuted)
		_ = prometheus.DefaultRegisterer.Register(m.tasksSubmitted)
		_ = prometheus.DefaultRegisterer.Register(m.activeLoops)
		_ = prometheus.DefaultRegisterer.Register(m.routingDuration)
		_ = prometheus.DefaultRegisterer.Register(m.completionsReceived)
		_ = prometheus.DefaultRegisterer.Register(m.terminalSettlements)
		_ = prometheus.DefaultRegisterer.Register(m.httpRequestsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.httpRequestDuration)
		_ = prometheus.DefaultRegisterer.Register(m.loopSignalsSent)
		_ = prometheus.DefaultRegisterer.Register(m.loopApprovalsSubmitted)
		_ = prometheus.DefaultRegisterer.Register(m.loopAdmissionRefusals)
		_ = prometheus.DefaultRegisterer.Register(m.sseConnectionsActive)
		_ = prometheus.DefaultRegisterer.Register(m.sseEventsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.sseErrorsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewCaughtUp)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewAppliedRevision)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewSubscribers)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewMaxPendingKeys)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewPoisonedTotal)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewCoalescedDropsTotal)
		_ = prometheus.DefaultRegisterer.Register(m.activityViewWatcherLostTotal)
	}

	return m
}

// recordMessageReceived increments the messages received counter for a channel type.
func (m *routerMetrics) recordMessageReceived(channelType string) {
	m.messagesReceived.WithLabelValues(channelType).Inc()
}

// recordCommandExecuted increments the commands executed counter for a command.
func (m *routerMetrics) recordCommandExecuted(command string) {
	m.commandsExecuted.WithLabelValues(command).Inc()
}

// recordTaskSubmitted increments the tasks submitted counter.
func (m *routerMetrics) recordTaskSubmitted() {
	m.tasksSubmitted.Inc()
}

// recordLoopStarted increments the active loops gauge.
func (m *routerMetrics) recordLoopStarted() {
	m.activeLoops.Inc()
}

// recordLoopEnded decrements the active loops gauge.
func (m *routerMetrics) recordLoopEnded() {
	m.activeLoops.Dec()
}

// recordRoutingDuration records the duration of a routing operation.
func (m *routerMetrics) recordRoutingDuration(seconds float64) {
	m.routingDuration.Observe(seconds)
}

// recordCompletionReceived increments the completions received counter for a status.
func (m *routerMetrics) recordCompletionReceived(status string) {
	m.completionsReceived.WithLabelValues(status).Inc()
}

func (m *routerMetrics) recordTerminalSettlement(reason string) {
	m.terminalSettlements.WithLabelValues(reason).Inc()
}

// recordHTTPRequest records an HTTP request with endpoint, method, and status.
func (m *routerMetrics) recordHTTPRequest(endpoint, method, status string) {
	m.httpRequestsTotal.WithLabelValues(endpoint, method, status).Inc()
}

// recordHTTPDuration records the duration of an HTTP request.
func (m *routerMetrics) recordHTTPDuration(endpoint, method string, seconds float64) {
	m.httpRequestDuration.WithLabelValues(endpoint, method).Observe(seconds)
}

// recordLoopSignal records a loop signal attempt.
func (m *routerMetrics) recordLoopSignal(signalType string, accepted bool) {
	acceptedStr := "false"
	if accepted {
		acceptedStr = "true"
	}
	m.loopSignalsSent.WithLabelValues(signalType, acceptedStr).Inc()
}

// recordLoopApproval records an HTTP approval submission attempt.
// success=true means the response was published successfully on
// agent.approval_response.<loop_id>; success=false means the publish
// failed or the request was rejected at the dispatch boundary
// (validation, missing pending state, etc.). The status label uses
// "success"/"error" rather than a boolean so it lines up with the
// agentic-tools executions_total convention.
func (m *routerMetrics) recordLoopApproval(decision string, success bool) {
	status := "error"
	if success {
		status = "success"
	}
	m.loopApprovalsSubmitted.WithLabelValues(decision, status).Inc()
}

// recordLoopAdmissionRefusal counts one refusal by the loop admission gate.
// Both labels are closed sets: seam is a fixed token naming where the request
// arrived, reason is one of the mapped refusal reasons. Called from exactly one
// place (Component.recordLoopAdmissionRefusal), which is what keeps "counted
// exactly once per refusal" a property of the code.
func (m *routerMetrics) recordLoopAdmissionRefusal(seam, reason string) {
	m.loopAdmissionRefusals.WithLabelValues(seam, reason).Inc()
}

// recordSSEConnect increments the active SSE connections gauge.
func (m *routerMetrics) recordSSEConnect() {
	m.sseConnectionsActive.Inc()
}

// recordSSEDisconnect decrements the active SSE connections gauge.
func (m *routerMetrics) recordSSEDisconnect() {
	m.sseConnectionsActive.Dec()
}

// recordSSEEvent records an SSE event by type.
func (m *routerMetrics) recordSSEEvent(eventType string) {
	m.sseEventsTotal.WithLabelValues(eventType).Inc()
}

// recordSSEError records an SSE error by type.
func (m *routerMetrics) recordSSEError(errorType string) {
	m.sseErrorsTotal.WithLabelValues(errorType).Inc()
}

// recordActivityViewCaughtUp sets the activity view staleness gauge: true on
// every caught-up transition, false on watcher loss (fail-closed).
func (m *routerMetrics) recordActivityViewCaughtUp(up bool) {
	v := 0.0
	if up {
		v = 1.0
	}
	m.activityViewCaughtUp.Set(v)
}

// recordActivityViewRevision advances the applied-revision watermark gauge.
// Applies arrive in watch order (revision-ascending), so Set is monotonic.
func (m *routerMetrics) recordActivityViewRevision(revision uint64) {
	m.activityViewAppliedRevision.Set(float64(revision))
}

// recordActivityViewWatcherLost counts shared-watcher losses.
func (m *routerMetrics) recordActivityViewWatcherLost() {
	m.activityViewWatcherLostTotal.Inc()
}

// recordActivityViewPoison counts poisoned AGENT_LOOPS writes — incremented
// once per write regardless of how many SSE clients are attached.
func (m *routerMetrics) recordActivityViewPoison() {
	m.activityViewPoisonedTotal.Inc()
}

// recordActivityViewSubscribers tracks the view's attached subscription count.
func (m *routerMetrics) recordActivityViewSubscribers(n int) {
	m.activityViewSubscribers.Set(float64(n))
}

// recordActivityViewFanOut records one fan-out window: pending deltas
// overwritten before delivery (slow-client at-most-once drops) and the
// largest per-subscriber pending buffer after enqueue.
func (m *routerMetrics) recordActivityViewFanOut(overwritten, maxPending int) {
	if overwritten > 0 {
		m.activityViewCoalescedDropsTotal.Add(float64(overwritten))
	}
	m.activityViewMaxPendingKeys.Set(float64(maxPending))
}
