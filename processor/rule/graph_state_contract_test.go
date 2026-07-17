package rule

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
)

func TestGraphStateContractFailureLatchesRuleEvaluationOff(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	processor, err := NewProcessor(nil, &cfg)
	if err != nil {
		t.Fatalf("NewProcessor: %v", err)
	}

	poisonedID := semantictest.EntityID(t, "test", "rule", "graph-state", "poison", "entity", "001")
	var state graph.EntityState
	err = graph.UnmarshalEntityState([]byte(`{"id":"`+poisonedID+`","triples":[{"subject":"`+poisonedID+`","predicate":"legacy.predicate","object":"value"}]}`), &state) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
	if err == nil {
		t.Fatal("expected shared graph-state decoder to reject noncanonical predicate")
	}
	if !processor.markGraphStateResetRequired(context.Background(), poisonedID, err) {
		t.Fatal("expected graph-state contract failure to be recognized")
	}
	if !processor.graphStateResetRequired.Load() {
		t.Fatal("expected reset-required state to remain latched")
	}

	healthyID := semantictest.EntityID(t, "test", "rule", "graph-state", "healthy", "entity", "002")
	processor.evaluateRulesForEntityState(context.Background(), healthyID, entitySnapshot{
		State:  &graph.EntityState{ID: healthyID},
		Action: "UPDATED",
	}, false)
	if got := atomic.LoadInt64(&processor.messagesEvaluated); got != 0 {
		t.Fatalf("messages evaluated after poison = %d, want zero", got)
	}
	if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
		t.Fatalf("rules triggered after poison = %d, want zero actions", got)
	}
}
