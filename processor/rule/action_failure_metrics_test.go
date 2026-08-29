package rule

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// actionFailuresTestMetrics returns a fresh, unregistered *Metrics carrying
// only actionFailuresTotal — the field this test file exercises. Mirrors
// publisherContractMetrics's pattern (publisher_graph_event_contract_test.go):
// a bare struct literal, not newRuleMetrics's process-wide sync.Once
// singleton, so each test gets isolated counters with no cross-test bleed.
func actionFailuresTestMetrics() *Metrics {
	return &Metrics{
		actionFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_rule_action_failures_total",
		}, []string{"action_type"}),
	}
}

// TestRunActions_ActionExecutionFailure_IncrementsActionFailuresMetric
// closes the H1 pre-merge review finding: the spec scenario "non-integer
// substitution fails loudly" promises a classified error AND a bounded
// rejection metric, but runActions' generic action-error branch
// (stateful_evaluator.go) previously only logged — an un-alertable
// fail-closed action is exactly the silent-failure class this repo
// hunts. Drives the REAL production path: StatefulEvaluator.Evaluate →
// runActions → the real ActionExecutor.Execute → executePublishAgent →
// stampLoopMaxIterations's non-integer rejection — not a hand-rolled
// call to the metric.
func TestRunActions_ActionExecutionFailure_IncrementsActionFailuresMetric(t *testing.T) {
	bucket := newMockKVBucket()
	tracker := NewStateTracker(bucket, slog.Default())
	mockPub := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mockPub, testExecutorPlatform())
	evaluator := NewStatefulEvaluator(tracker, executor, slog.Default())

	metrics := actionFailuresTestMetrics()
	evaluator.SetMetrics(metrics)

	ruleDef := Definition{
		ID:   "rule-loop-max-iter-metric",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:              ActionTypePublishAgent,
				Subject:           "agent.task.test",
				Role:              "general",
				Model:             "mock-model",
				Prompt:            "p",
				LoopMaxIterations: "unbounded", // non-integer after substitution
			},
		},
	}

	ctx := context.Background()
	const entityID = "ent-action-failure-metric"

	// Force false → true so on_enter fires (mirrors driveEntries in
	// action_maxiterations_test.go, inlined here since this test only
	// needs a single firing).
	if _, err := evaluator.Evaluate(ctx, Evaluation{
		Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false,
	}); err != nil {
		t.Fatalf("evaluate exit: %v", err)
	}
	if _, err := evaluator.Evaluate(ctx, Evaluation{
		Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true,
	}); err != nil {
		t.Fatalf("evaluate enter: %v", err)
	}

	if got := testutil.ToFloat64(metrics.actionFailuresTotal.WithLabelValues(ActionTypePublishAgent)); got != 1 {
		t.Errorf("actionFailuresTotal{action_type=%q} = %v, want 1", ActionTypePublishAgent, got)
	}
	if len(mockPub.published) != 0 {
		t.Errorf("published = %d messages, want 0 (task must not publish when loop_max_iterations fails to resolve)", len(mockPub.published))
	}
}

// TestRunActions_SuccessfulAction_DoesNotIncrementActionFailuresMetric is
// the negative-space guard: a successful publish_agent dispatch must not
// bump the failure counter, and SetMetrics(nil) (a deployment without
// metrics wired) must not panic runActions.
func TestRunActions_SuccessfulAction_DoesNotIncrementActionFailuresMetric(t *testing.T) {
	bucket := newMockKVBucket()
	tracker := NewStateTracker(bucket, slog.Default())
	mockPub := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mockPub, testExecutorPlatform())
	evaluator := NewStatefulEvaluator(tracker, executor, slog.Default())

	metrics := actionFailuresTestMetrics()
	evaluator.SetMetrics(metrics)

	ruleDef := Definition{
		ID:   "rule-loop-max-iter-metric-success",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "general",
				Model:   "mock-model",
				Prompt:  "p",
				// LoopMaxIterations omitted — valid, back-compat dispatch.
			},
		},
	}

	ctx := context.Background()
	const entityID = "ent-action-success-metric"

	if _, err := evaluator.Evaluate(ctx, Evaluation{
		Rule: ruleDef, EntityID: entityID, CurrentlyMatching: false,
	}); err != nil {
		t.Fatalf("evaluate exit: %v", err)
	}
	if _, err := evaluator.Evaluate(ctx, Evaluation{
		Rule: ruleDef, EntityID: entityID, CurrentlyMatching: true,
	}); err != nil {
		t.Fatalf("evaluate enter: %v", err)
	}

	if got := testutil.ToFloat64(metrics.actionFailuresTotal.WithLabelValues(ActionTypePublishAgent)); got != 0 {
		t.Errorf("actionFailuresTotal{action_type=%q} = %v, want 0 for a successful dispatch", ActionTypePublishAgent, got)
	}
	if len(mockPub.published) != 1 {
		t.Fatalf("published = %d messages, want 1", len(mockPub.published))
	}

	// A nil-metrics evaluator (SetMetrics never called, or explicitly nil)
	// must not panic on an action failure either.
	nilMetricsEvaluator := NewStatefulEvaluator(tracker, executor, slog.Default())
	failRule := Definition{
		ID:   "rule-loop-max-iter-nil-metrics",
		Type: "expression",
		OnEnter: []Action{
			{
				Type:              ActionTypePublishAgent,
				Subject:           "agent.task.test",
				Role:              "general",
				Model:             "mock-model",
				Prompt:            "p",
				LoopMaxIterations: "unbounded",
			},
		},
	}
	const nilMetricsEntityID = "ent-nil-metrics"
	if _, err := nilMetricsEvaluator.Evaluate(ctx, Evaluation{
		Rule: failRule, EntityID: nilMetricsEntityID, CurrentlyMatching: false,
	}); err != nil {
		t.Fatalf("evaluate exit (nil metrics): %v", err)
	}
	if _, err := nilMetricsEvaluator.Evaluate(ctx, Evaluation{
		Rule: failRule, EntityID: nilMetricsEntityID, CurrentlyMatching: true,
	}); err != nil {
		t.Fatalf("evaluate enter (nil metrics) should not panic or error: %v", err)
	}
}
