package agentic

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestNewTestTaskRequiresQueryEntity(t *testing.T) {
	task := newTestTask(time.Unix(0, 42))

	if len(task.Tools) != 1 || task.Tools[0].Name != "query_entity" {
		t.Fatalf("tools = %#v, want explicit query_entity allowlist", task.Tools)
	}
	if task.ToolChoice == nil || task.ToolChoice.Mode != "function" || task.ToolChoice.FunctionName != "query_entity" {
		t.Fatalf("tool choice = %#v, want forced query_entity", task.ToolChoice)
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("task validation failed: %v", err)
	}
}

func TestValidateResultsRequiresTargetTrajectory(t *testing.T) {
	s := NewScenario(nil, DefaultConfig())

	if err := s.validateResults(context.Background(), &scenarios.Result{
		Details: map[string]any{"completion_method": "target_trajectory"},
	}); err != nil {
		t.Fatalf("validateResults() error = %v, want nil", err)
	}

	if err := s.validateResults(context.Background(), &scenarios.Result{
		Details: map[string]any{"completion_method": "metrics"},
	}); err == nil {
		t.Fatal("validateResults() error = nil for aggregate metric completion")
	}
}

func TestRequiredComponentsIncludesRuleProcessor(t *testing.T) {
	for _, name := range requiredComponents() {
		if name == "rule" {
			return
		}
	}
	t.Fatal("requiredComponents() does not include rule processor")
}

func TestSumSnapshotMetric(t *testing.T) {
	snapshot := &client.MetricsSnapshot{Metrics: map[string]client.Metric{
		`loops{outcome="complete"}`: {
			Name:  "semstreams_agentic_loop_loops_completed_total",
			Value: 2,
		},
		`loops{outcome="failed"}`: {
			Name:  "semstreams_agentic_loop_loops_completed_total",
			Value: 1,
		},
		"other": {Name: "other_metric", Value: 99},
	}}

	if got := sumSnapshotMetric(snapshot, "semstreams_agentic_loop_loops_completed_total"); got != 3 {
		t.Fatalf("sumSnapshotMetric() = %v, want 3", got)
	}
}
