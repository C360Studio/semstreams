package rule

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
)

func TestGraphStateContractFailureLatchesRuleEvaluationOff(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}

	var state graph.EntityState
	err = graph.UnmarshalEntityState([]byte(`{"id":"entity-1","triples":[{"subject":"entity-1","predicate":"legacy.predicate","object":"value"}]}`), &state)
	if err == nil {
		t.Fatal("expected shared graph-state decoder to reject noncanonical predicate")
	}
	if !processor.markGraphStateResetRequired(context.Background(), "entity-1", err) {
		t.Fatal("expected graph-state contract failure to be recognized")
	}
	if !processor.graphStateResetRequired.Load() {
		t.Fatal("expected reset-required state to remain latched")
	}

	processor.evaluateRulesForEntityState(context.Background(), "entity-2", entitySnapshot{
		State:  &graph.EntityState{ID: "entity-2"},
		Action: "UPDATED",
	}, false)
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
		t.Fatalf("messages evaluated after poison = %d, want zero", got)
	}
	if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
		t.Fatalf("rules triggered after poison = %d, want zero actions", got)
	}
}
