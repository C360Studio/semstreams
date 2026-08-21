package rule

import (
	"context"
	"testing"
)

// The shared lifecycle suite asserts concurrent Stop rejoin and retained
// terminal-result replay. RU1 intentionally implements the corrected one-shot
// contract, so this package pins only the interface invariants that still apply.
func TestRuleLifecycleNilContextsFailBeforeAction(t *testing.T) {
	processor := &Processor{}
	if err := processor.Start(nil); err == nil {
		t.Fatal("Start(nil) succeeded")
	}
	if err := processor.Stop(nil); err == nil {
		t.Fatal("Stop(nil) succeeded")
	}
}

func TestRuleLifecycleCompletedStopIsNilNoop(t *testing.T) {
	processor := &Processor{}
	if err := processor.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := processor.Stop(context.Background()); err != nil {
		t.Fatalf("repeated completed Stop: %v", err)
	}
}
