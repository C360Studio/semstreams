package rule

import (
	"context"
	"testing"
)

// Keep these interface checks fast and independent of NATS. The integration
// fixture runs the production processor through component.StandardLifecycleTests.
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
