// Package agenticloop provides Prometheus metrics for agentic-loop component.
package agenticloop

import (
	"sync"

	"github.com/c360studio/semstreams/agentic"
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
	trajectorySteps         *prometheus.CounterVec
	trajectoryAuditFailures *prometheus.CounterVec

	// Tool calls
	toolCallsDispatched *prometheus.CounterVec
	toolResultsReceived *prometheus.CounterVec
	toolResultsDropped  *prometheus.CounterVec

	// Model responses
	modelResponsesDropped *prometheus.CounterVec
	signalsDropped        *prometheus.CounterVec

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

	// Graph-write-before-publish ordering
	graphWritePublishTimeouts *prometheus.CounterVec
	// Permanent decoded task-intake rejection. Labels are bounded enums only.
	taskIntakeRejections *prometheus.CounterVec

	// Tool-call governance (ADR-039)
	governanceVerdictDuration                *prometheus.HistogramVec
	governanceVerdictTotal                   *prometheus.CounterVec
	governanceSubscribeBeforePublishFailures *prometheus.CounterVec

	// Lesson brief-assembly injection (ADR-080). kind=matched counts every
	// active lesson whose scope matched at dispatch; kind=included counts those
	// that survived the K + byte bounds. matched > included ⇒ truncation.
	lessonInjection *prometheus.CounterVec
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

			trajectoryAuditFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "trajectory_audit_failures_total",
				Help:      "Best-effort trajectory audit failures by bounded stage, fact kind, and reason",
			}, []string{"stage", "kind", "reason"}),

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
				Help:      "Total tool results acknowledged without effect, by reason. reason=\"stale_execution\": no loop mapping exists for the execution ID and the loop record is absent or terminal. reason=\"older_request\": the result names an earlier request than the loop record does, so the loop already applied it — counted on the warm lane and on the cold lane after the record has been brought forward to the loop newest retained request, whichever process holds the loop. reason=\"terminal_unproven\": the loop is terminal, so no result can still be applied to it. Sustained non-zero rate points at NATS redelivery or executor double-publish. A result the loop record still names is NOT counted here: it is retried until a process can apply it, and a result naming a request of no loop is quarantined rather than dropped.",
			}, []string{"reason"}),

			modelResponsesDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "model_responses_dropped_total",
				Help:      "Total model responses acknowledged without advancing a loop, by reason. reason=\"stale_request_id\": the RequestID maps to no loop and the loop record is absent or terminal — expected after a loop settles and releases its per-loop state. reason=\"superseded_request\": the response names an EARLIER request than the loop record does, so the loop already advanced past it — counted on the warm lane and on the cold lane after the record has been brought forward to the loop newest retained request, whichever process holds the loop. A sustained rate on either points at NATS redelivery. A response the loop record still names is NOT counted here: it is retried until a process can apply it, and a response naming a request of no loop is quarantined rather than dropped.",
			}, []string{"reason"}),

			signalsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "signals_dropped_total",
				Help:      "Total control signals acknowledged without effect because the loop they name cannot receive one. reason=\"already_terminal\" is a cancel for a loop that already finished — idempotent and expected on redelivery; reason=\"stale_loop_id\" is a signal whose loop has no durable record at all. A signal for a loop that is live but held by another process is NOT counted here: it is retried, not dropped.",
			}, []string{"reason"}),

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

			graphWritePublishTimeouts: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "graph_write_publish_timeout_total",
				Help:      "Total times the graph-write-before-publish budget expired before WriteLoopCompletion/WriteLoopFailure returned. The agent.complete.* event is withheld and the joined delivery fails closed. Sustained non-zero rate points at graph-gateway latency or NATS subscription propagation issues; a tightenable budget is at graphWritePublishBudget in component.go.",
			}, []string{"state"}),

			taskIntakeRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "task_intake_rejections_total",
				Help:      "Permanent structural task-intake rejections by bounded lane and reason",
			}, []string{"lane", "reason"}),

			// Tool-call governance (ADR-039) drives the timeout-tuning
			// decision in beta.70 — buckets span 1ms → 5s to capture
			// both fast in-process rule fires and slow networked
			// rule-engine paths.
			governanceVerdictDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_call_governance_verdict_duration_seconds",
				Help:      "Time from proposed-call publish to verdict arrival. decision label is approved|rejected|timeout; mode is audit|enforce. Drives the timeout-tuning decision in beta.70 — wide range now, tighten after measurement.",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
			}, []string{"decision", "mode"}),

			governanceVerdictTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_call_governance_verdict_total",
				Help:      "Total tool-call governance verdicts by decision and mode. Sum of decision=approved + decision=rejected + decision=timeout equals total proposed-call publishes (modulo in-flight). Sustained decision=timeout signals undersized timeout config or stuck rule-engine path.",
			}, []string{"decision", "mode"}),

			governanceSubscribeBeforePublishFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "tool_call_governance_subscribe_before_publish_failures_total",
				Help:      "Verdicts that reached no waiter, by reason. reason=\"missing_waiter\" is the subscribe-before-publish race regressing (ADR-039 race-fix option 3) or a late arrival — the loop record then decides ack-vs-retry. reason=\"unrecoverable_loop_identity\" is a verdict whose payload carries neither a canonical loop_id nor a request_id in the <loopID>:req: grammar, so no record can be read for it; those terminate as malformed rather than acknowledging like a settled loop. Non-zero on either reason means investigate.",
			}, []string{"reason"}),

			lessonInjection: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "agentic_loop",
				Name:      "lesson_injection_total",
				Help:      "Lessons pushed into agent briefs at assembly (ADR-080). kind=matched sums every active lesson whose scope matched the dispatched loop; kind=included sums those that survived the K + total-byte bounds. A sustained kind=matched greatly exceeding kind=included means briefs are truncating lessons — raise K/byte budget or tighten scope.",
			}, []string{"kind"}),
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
			_ = registry.RegisterCounterVec("agentic-loop", "trajectory_audit_failures_total", metrics.trajectoryAuditFailures)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_calls_dispatched_total", metrics.toolCallsDispatched)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_results_received_total", metrics.toolResultsReceived)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_results_dropped_total", metrics.toolResultsDropped)
			_ = registry.RegisterCounterVec("agentic-loop", "model_responses_dropped_total", metrics.modelResponsesDropped)
			_ = registry.RegisterCounterVec("agentic-loop", "signals_dropped_total", metrics.signalsDropped)
			_ = registry.RegisterHistogram("agentic-loop", "request_tokens_in", metrics.requestTokensIn)
			_ = registry.RegisterHistogram("agentic-loop", "request_tokens_out", metrics.requestTokensOut)
			_ = registry.RegisterCounter("agentic-loop", "tool_results_truncated_total", metrics.toolResultsTruncated)
			_ = registry.RegisterGauge("agentic-loop", "context_utilization", metrics.contextUtilization)
			_ = registry.RegisterCounter("agentic-loop", "context_compactions_total", metrics.contextCompactionsTotal)
			_ = registry.RegisterHistogram("agentic-loop", "context_compaction_tokens_saved", metrics.contextCompactionTokensSaved)
			_ = registry.RegisterGauge("agentic-loop", "context_compacted_region_tokens", metrics.contextCompactedRegionTokens)
			_ = registry.RegisterCounterVec("agentic-loop", "graph_write_publish_timeout_total", metrics.graphWritePublishTimeouts)
			_ = registry.RegisterCounterVec("agentic-loop", "task_intake_rejections_total", metrics.taskIntakeRejections)
			_ = registry.RegisterHistogramVec("agentic-loop", "tool_call_governance_verdict_duration_seconds", metrics.governanceVerdictDuration)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_call_governance_verdict_total", metrics.governanceVerdictTotal)
			_ = registry.RegisterCounterVec("agentic-loop", "tool_call_governance_subscribe_before_publish_failures_total", metrics.governanceSubscribeBeforePublishFailures)
			_ = registry.RegisterCounterVec("agentic-loop", "lesson_injection_total", metrics.lessonInjection)
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
			_ = prometheus.DefaultRegisterer.Register(metrics.trajectoryAuditFailures)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolCallsDispatched)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsReceived)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsDropped)
			_ = prometheus.DefaultRegisterer.Register(metrics.modelResponsesDropped)
			_ = prometheus.DefaultRegisterer.Register(metrics.signalsDropped)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestTokensIn)
			_ = prometheus.DefaultRegisterer.Register(metrics.requestTokensOut)
			_ = prometheus.DefaultRegisterer.Register(metrics.toolResultsTruncated)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextUtilization)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactionsTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactionTokensSaved)
			_ = prometheus.DefaultRegisterer.Register(metrics.contextCompactedRegionTokens)
			_ = prometheus.DefaultRegisterer.Register(metrics.graphWritePublishTimeouts)
			_ = prometheus.DefaultRegisterer.Register(metrics.taskIntakeRejections)
			_ = prometheus.DefaultRegisterer.Register(metrics.governanceVerdictDuration)
			_ = prometheus.DefaultRegisterer.Register(metrics.governanceVerdictTotal)
			_ = prometheus.DefaultRegisterer.Register(metrics.governanceSubscribeBeforePublishFailures)
			_ = prometheus.DefaultRegisterer.Register(metrics.lessonInjection)
		}
	})
	return metrics
}

// recordLessonInjection records the brief-assembly matched-vs-included counts
// for one dispatch (ADR-080). Called once per assembleLessonBlock even when
// counts are zero is unnecessary; the handler only calls it when a reader ran.
// A zero included with non-zero matched is itself a signal (byte budget too
// tight), so both are always emitted.
func (m *loopMetrics) recordLessonInjection(matched, included int) {
	if matched > 0 {
		m.lessonInjection.WithLabelValues("matched").Add(float64(matched))
	}
	if included > 0 {
		m.lessonInjection.WithLabelValues("included").Add(float64(included))
	}
}

// RecordGovernanceVerdict observes the verdict duration and
// increments the per-decision counter. Implements
// DispatcherMetrics — the GovernanceDispatcher calls this directly.
// Decision is "approved" | "rejected" | "timeout"; mode is "audit" |
// "enforce" — disabled mode never records verdicts (no publish, no
// wait).
func (m *loopMetrics) RecordGovernanceVerdict(decision, mode string, duration float64) {
	m.governanceVerdictDuration.WithLabelValues(decision, mode).Observe(duration)
	m.governanceVerdictTotal.WithLabelValues(decision, mode).Inc()
}

// The two reasons emitted on
// semstreams_agentic_loop_tool_call_governance_subscribe_before_publish_failures_total.
// Both are enumerated in the metric's Help; a value that is not one of these
// is a bug.
const (
	verdictDropMissingWaiter         = "missing_waiter"
	verdictDropUnrecoverableIdentity = "unrecoverable_loop_identity"
)

// RecordGovernanceVerdictMissingWaiter increments the
// subscribe-before-publish-failures counter. Implements
// DispatcherMetrics. Each non-zero increment signals a verdict
// arrived for an execution_id that no longer had a registered waiter —
// either the race-fix regressed (verdict beat the pre-register) or a
// verdict arrived after Propose's timeout already fired (benign in
// audit mode, signal in enforce mode).
func (m *loopMetrics) RecordGovernanceVerdictMissingWaiter() {
	m.governanceSubscribeBeforePublishFailures.WithLabelValues(verdictDropMissingWaiter).Inc()
}

// recordVerdictIdentityUnrecoverable counts a verdict whose loop identity
// cannot be recovered from its payload. It shares the waiter-miss counter
// because it is the same question — "this verdict reached no waiter, now
// what?" — answered differently: a waiter miss is decided by the loop record,
// while an unrecoverable identity has no record to read and is malformed input.
// Separating the two reasons is what keeps an adopter's non-canonical loop_id
// from reading as a settled loop on the same series.
func (m *loopMetrics) recordVerdictIdentityUnrecoverable() {
	m.governanceSubscribeBeforePublishFailures.WithLabelValues(verdictDropUnrecoverableIdentity).Inc()
}

// recordGraphWritePublishTimeout increments the counter when the
// graph-write-before-publish budget expired before WriteLoopCompletion
// or WriteLoopFailure returned. State is "complete" or "failure" to
// match persistHandlerResult's terminal-state branches.
func (m *loopMetrics) recordGraphWritePublishTimeout(state string) {
	m.graphWritePublishTimeouts.WithLabelValues(state).Inc()
}

func (m *loopMetrics) recordTaskIntakeRejection(lane, reason string) {
	m.taskIntakeRejections.WithLabelValues(lane, reason).Inc()
}

func (m *loopMetrics) recordTrajectoryAuditFailure(stage trajectoryAuditStage, kind agentic.TrajectoryKind, reason trajectoryAuditReason) {
	stage = boundedTrajectoryAuditStage(stage)
	kind = boundedTrajectoryAuditKind(kind)
	reason = boundedTrajectoryAuditReason(reason)
	m.trajectoryAuditFailures.WithLabelValues(string(stage), string(kind), string(reason)).Inc()
}

func boundedTrajectoryAuditStage(stage trajectoryAuditStage) trajectoryAuditStage {
	switch stage {
	case trajectoryStageProviderResolve, trajectoryStageEvidenceGet, trajectoryStageEvidencePut,
		trajectoryStageEvidenceVerify, trajectoryStageFactEncode, trajectoryStageFactCreate,
		trajectoryStageFactVerify:
		return stage
	default:
		return trajectoryStageFactEncode
	}
}

func boundedTrajectoryAuditKind(kind agentic.TrajectoryKind) agentic.TrajectoryKind {
	switch kind {
	case agentic.TrajectoryKindLoopStarted, agentic.TrajectoryKindModelRequested,
		agentic.TrajectoryKindModelCompleted, agentic.TrajectoryKindToolRequested,
		agentic.TrajectoryKindToolCompleted, agentic.TrajectoryKindContextCompacted,
		agentic.TrajectoryKindLoopTerminal:
		return kind
	default:
		return agentic.TrajectoryKindLoopStarted
	}
}

func boundedTrajectoryAuditReason(reason trajectoryAuditReason) trajectoryAuditReason {
	switch reason {
	case trajectoryReasonProviderUnavailable, trajectoryReasonBackend,
		trajectoryReasonIntegrity, trajectoryReasonEncode, trajectoryReasonTimeout:
		return reason
	default:
		return trajectoryReasonBackend
	}
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

// recordToolResultDropped records a tool result acknowledged without effect.
// Three reasons are emitted:
//
//   - "stale_execution" — no loop mapping exists for the execution ID. The
//     dominant case after GetAndClearToolResults eviction: a re-delivered
//     result for an already-drained execution. A sustained non-zero rate
//     points at NATS redelivery or an executor double-publishing.
//   - "older_request" — the result names an EARLIER request than the loop
//     record does (#1330). A loop cannot advance past a request until its
//     whole tool batch is in, so an earlier request is one this loop already
//     applied; the redelivery is settled rather than re-applied, which would
//     otherwise re-send the result to the model in the next turn.
//   - "terminal_unproven" — the loop is terminal, so no result can be applied
//     to it any more (owner ruling Q7 on #1330). Whether this particular
//     result was applied before the loop settled is deliberately not
//     re-derived: it would change nothing this delivery can do.
//
// What is deliberately NOT counted here is a result the loop record still
// names: that delivery returns an error and is retried, and a retried result
// is not a dropped one. settleToolResultWithoutLoop warns with the execution
// id and leaves the work owed to whichever process holds the loop. Nor is a
// result naming a request of no loop — that is quarantined, not dropped.
// "older_request" is counted on BOTH lanes, warm and cold, and being held by
// another process does not exempt it: the cold arm brings the record forward
// to the loop's newest retained request first, and after that a result naming
// an earlier request is owed to nobody at all.
func (m *loopMetrics) recordToolResultDropped(reason string) {
	m.toolResultsDropped.WithLabelValues(reason).Inc()
}

// recordSignalDropped records a control signal acknowledged without effect.
func (m *loopMetrics) recordSignalDropped(reason string) {
	m.signalsDropped.WithLabelValues(reason).Inc()
}

// recordModelResponseDropped records a model response acknowledged without
// advancing a loop. Reason "stale_request_id" is the settled-loop case: terminal
// release takes the request routing with it, so a response that arrives after
// the loop settled resolves nothing. Reason "superseded_request" is the
// advanced-loop case: the record names a LATER request, so this response
// answers a question the loop is already past. It is counted on both lanes —
// warm, and cold after step 0 has brought the record forward — and being held
// by another process does not exempt it, because a response older than the
// record is owed to no process at all.
//
// What is not counted here is a response the record still names (retried until
// a process can apply it) and one naming a request of no loop (quarantined).
// Both drops above are deliberate and safe, and both are counted so that "safe"
// stays a claim an operator can check rather than one only the code makes.
func (m *loopMetrics) recordModelResponseDropped(reason string) {
	m.modelResponsesDropped.WithLabelValues(reason).Inc()
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
