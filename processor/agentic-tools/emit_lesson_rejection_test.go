package agentictools

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/c360studio/semstreams/agentic"
)

// TestEmitLessonExecutor_RejectionCounterReasons proves each of the four
// ADR-080 writer gates increments the rejection counter with its own reason
// label (task 4.4). A spy recorder captures reasons deterministically, free of
// the package metrics singleton.
func TestEmitLessonExecutor_RejectionCounterReasons(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *agentic.ToolCall)
		perLoop int
		// preCalls are gate-passing calls issued before the asserted call
		// (used to exhaust the per-loop cap).
		preCalls   int
		wantReason string
	}{
		{
			name:       "evidence — empty citation list",
			mutate:     func(c *agentic.ToolCall) { c.Arguments["evidence_entity_ids"] = []any{} },
			wantReason: lessonRejectEvidence,
		},
		{
			name:       "evidence — malformed entity ID",
			mutate:     func(c *agentic.ToolCall) { c.Arguments["evidence_entity_ids"] = []any{"loop-abc"} },
			wantReason: lessonRejectEvidence,
		},
		{
			name: "bound — over-bound injection form",
			mutate: func(c *agentic.ToolCall) {
				over := make([]byte, agentic.LessonInjectionFormMaxBytes+1)
				for i := range over {
					over[i] = 'x'
				}
				c.Arguments["injection_form"] = string(over)
			},
			wantReason: lessonRejectBound,
		},
		{
			name:       "grammar — untyped scope key",
			mutate:     func(c *agentic.ToolCall) { c.Arguments["applies_to"] = []any{"c360"} },
			wantReason: lessonRejectGrammar,
		},
		{
			name:       "cap — per-loop emission cap exhausted",
			perLoop:    1,
			preCalls:   1,
			mutate:     func(_ *agentic.ToolCall) {},
			wantReason: lessonRejectCap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingLessonStore{}
			e := newEmitLessonExecutor(store)
			if tt.perLoop > 0 {
				e.perLoopCap = tt.perLoop
			}
			var reasons []string
			e.recordRejection = func(r string) { reasons = append(reasons, r) }

			// Exhaust the cap with gate-passing calls first (distinct summaries
			// so each mints a distinct lesson, all on the same loop).
			for i := 0; i < tt.preCalls; i++ {
				pre := validEmitLessonCall()
				pre.ID = "pre"
				pre.Arguments["summary"] = "pre-fill lesson"
				if res, _ := e.Execute(context.Background(), pre); res.Error != "" {
					t.Fatalf("pre-fill emit rejected unexpectedly: %s", res.Error)
				}
			}
			if len(reasons) != 0 {
				t.Fatalf("pre-fill calls must not record any rejection, got %v", reasons)
			}

			call := validEmitLessonCall()
			tt.mutate(&call)
			res, err := e.Execute(context.Background(), call)
			if err != nil {
				t.Fatalf("a gate rejection must not return a wrapped err: %v", err)
			}
			if res.Error == "" {
				t.Fatalf("expected a rejection, got a successful result")
			}
			if len(reasons) != 1 || reasons[0] != tt.wantReason {
				t.Fatalf("recorded reasons = %v, want exactly [%s]", reasons, tt.wantReason)
			}
		})
	}
}

// TestEmitLessonExecutor_UncountedRejectsHaveNoReason proves input-hygiene
// rejects (not one of the four contract gates) are NOT counted.
func TestEmitLessonExecutor_UncountedRejectsHaveNoReason(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(c *agentic.ToolCall)
	}{
		{"invalid polarity", func(c *agentic.ToolCall) { c.Arguments["polarity"] = "maybe" }},
		{"missing summary", func(c *agentic.ToolCall) { delete(c.Arguments, "summary") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingLessonStore{}
			e := newEmitLessonExecutor(store)
			var reasons []string
			e.recordRejection = func(r string) { reasons = append(reasons, r) }

			call := validEmitLessonCall()
			tc.mutate(&call)
			res, _ := e.Execute(context.Background(), call)
			if res.Error == "" {
				t.Fatalf("expected a rejection for %s", tc.name)
			}
			if len(reasons) != 0 {
				t.Fatalf("input-hygiene reject must be uncounted, got reasons %v", reasons)
			}
		})
	}
}

// TestEmitLessonExecutor_RejectionCounterWiresToPrometheus proves the default
// recorder threads into the package metrics singleton so the real Prometheus
// counter advances (before/after delta is robust to other tests touching it).
func TestEmitLessonExecutor_RejectionCounterWiresToPrometheus(t *testing.T) {
	// Initialise the singleton against the default registry (metricsOnce makes
	// this idempotent across the package's tests).
	m := getMetrics(nil)
	before := testutil.ToFloat64(m.rejectionsTotal.WithLabelValues(EmitLessonToolName, lessonRejectEvidence))

	e := newEmitLessonExecutor(&recordingLessonStore{}) // default recorder, no spy
	call := validEmitLessonCall()
	call.Arguments["evidence_entity_ids"] = []any{}
	if res, _ := e.Execute(context.Background(), call); res.Error == "" {
		t.Fatal("expected an evidence rejection")
	}

	after := testutil.ToFloat64(m.rejectionsTotal.WithLabelValues(EmitLessonToolName, lessonRejectEvidence))
	if after-before != 1 {
		t.Errorf("evidence rejection counter delta = %v, want 1", after-before)
	}
}
