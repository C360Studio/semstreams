package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestInvalidReplayedPredicatePoisonsReadiness(t *testing.T) {
	t.Parallel()

	indexComponent := &Component{lifecycleReporter: component.NewNoOpLifecycleReporter()}
	state := graph.EntityState{
		ID: "acme.ops.test.system.widget.001",
		Triples: []message.Triple{{
			Subject:   "acme.ops.test.system.widget.001",
			Predicate: "legacy.invalid_name", // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.invalid_name","reason":"arity"}
		}},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	if err := indexComponent.processEntityUpdateFromData(context.Background(), state.ID, data); err == nil {
		t.Fatal("processEntityUpdateFromData() error = nil, want reset-required poison")
	}
	status := indexComponent.computeIndexStatus(context.Background())
	if status.Ready || status.State != graph.IndexStateResetRequired || status.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("status = %#v, want sticky reset-required", status)
	}

	queryErr := indexComponent.ensureQueryReady(context.Background())
	var classified *errs.ClassifiedError
	if !errors.As(queryErr, &classified) {
		t.Fatalf("ensureQueryReady() error = %T %v, want classified", queryErr, queryErr)
	}
	if classified.Class != errs.ErrorFatal || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("classified = %#v, want fatal %q", classified, graph.ErrorCodeGraphStateResetRequired)
	}
}

func TestUnreadableReplayedEntityPoisonsReadiness(t *testing.T) {
	t.Parallel()

	indexComponent := &Component{lifecycleReporter: component.NewNoOpLifecycleReporter()}
	if err := indexComponent.processEntityUpdateFromData(context.Background(), "broken", []byte("{")); err == nil {
		t.Fatal("processEntityUpdateFromData() error = nil, want reset-required poison")
	}
	status := indexComponent.computeIndexStatus(context.Background())
	if status.Reason != "unreadable_entity_state" {
		t.Fatalf("status reason = %q, want unreadable_entity_state", status.Reason)
	}
}
