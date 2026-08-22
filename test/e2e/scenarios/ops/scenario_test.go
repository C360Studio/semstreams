package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestRunOpsStagesCountsOnlyCompletedStages(t *testing.T) {
	tests := []struct {
		name       string
		stages     []opsStage
		wantCount  int
		wantFailed string
	}{
		{
			name:      "all nine load-bearing stages",
			stages:    successfulOpsStages(9),
			wantCount: 9,
		},
		{
			name: "failed stage is not counted",
			stages: []opsStage{
				{name: "completed", fn: successfulOpsStage},
				{name: "failed", fn: func(context.Context, *scenarios.Result) error {
					return errors.New("stage failed")
				}},
				{name: "not-run", fn: func(context.Context, *scenarios.Result) error {
					t.Fatal("stage after a failure must not run")
					return nil
				}},
			},
			wantCount:  1,
			wantFailed: "failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := &scenarios.Result{Metrics: map[string]any{}}
			failed, err := runOpsStages(context.Background(), result, test.stages)
			if test.wantFailed == "" && err != nil {
				t.Fatalf("runOpsStages() error = %v", err)
			}
			if test.wantFailed != "" && err == nil {
				t.Fatal("runOpsStages() succeeded, want stage failure")
			}
			if failed != test.wantFailed {
				t.Errorf("failed stage = %q, want %q", failed, test.wantFailed)
			}
			if result.AssertionsRun != test.wantCount {
				t.Errorf("AssertionsRun = %d, want %d", result.AssertionsRun, test.wantCount)
			}
		})
	}
}

func successfulOpsStages(count int) []opsStage {
	stages := make([]opsStage, count)
	for index := range stages {
		stages[index] = opsStage{name: "completed", fn: successfulOpsStage}
	}
	return stages
}

func successfulOpsStage(context.Context, *scenarios.Result) error { return nil }
