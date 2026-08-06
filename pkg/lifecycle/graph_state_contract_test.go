package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestGetLatchesGraphStatePoisonAndReturnsNoProjection(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.poisoned"
	bucket.raw[entityID] = poisonedLifecycleState(entityID)

	participant, err := mgr.Get(context.Background(), "fixture", entityID)
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("Get error = %T %v, want fatal reset-required", err, err)
	}
	if participant != nil {
		t.Fatalf("Get participant = %#v, want nil", participant)
	}
	assertResetReason(t, err, graph.GraphStateReasonNoncanonicalPredicate)

	// Sticky means later calls fail before reading, even if the offending key
	// were repaired in place. Recovery requires the documented wipe/restart/reseed
	// and process restart boundary.
	delete(bucket.raw, entityID)
	participant, err = mgr.Get(context.Background(), "fixture", entityID)
	if err == nil || !errs.IsFatal(err) || participant != nil {
		t.Fatalf("second Get = (%#v, %v), want sticky fatal with nil projection", participant, err)
	}
}

func TestHistoryReturnsNoPartialEventsWhenCurrentEntityIsPoisoned(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.history"
	bucket.raw[entityID] = poisonedLifecycleState(entityID)

	events, err := mgr.History(context.Background(), "fixture", entityID)
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("History error = %T %v, want fatal reset-required", err, err)
	}
	if events != nil {
		t.Fatalf("History events = %#v, want nil rather than partial transition records", events)
	}
	assertResetReason(t, err, graph.GraphStateReasonNoncanonicalPredicate)
}

func TestHistoryRejectsMalformedTransitionRecordAsAWhole(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.malformed-history"
	bucket.put(entityID, &graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "planning"},
			{Subject: entityID, Predicate: transitionPredicateTo, Object: "planning", Context: "transition-1"},
		},
	})

	events, err := mgr.History(context.Background(), "fixture", entityID)
	if err == nil {
		t.Fatal("History error=nil, want malformed transition record error")
	}
	if events != nil {
		t.Fatalf("History events=%#v, want nil on malformed record", events)
	}
}

func TestHistoryRejectsTransitionRecordDriftFromCurrentPhase(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.drifted-history"
	record := newTransitionRecord("", "planning", time.Now(), TransitionSourceFramework, "created")
	bucket.put(entityID, &graph.EntityState{
		ID: entityID,
		Triples: append(
			[]message.Triple{{Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "flying"}},
			transitionRecordsToTriples(entityID, []transitionRecord{record})...,
		),
	})

	events, err := mgr.History(context.Background(), "fixture", entityID)
	if !errors.Is(err, ErrInvalidTransitionRecord) {
		t.Fatalf("History error=%v, want ErrInvalidTransitionRecord", err)
	}
	if events != nil {
		t.Fatalf("History events=%#v, want nil on phase/record drift", events)
	}
}

func poisonedLifecycleState(entityID string) []byte {
	return []byte(`{"id":"` + entityID + `","triples":[{"subject":"` + entityID + `","predicate":"legacy.predicate","object":"planning"}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
}

func assertResetReason(t *testing.T, err error, want graph.StateResetReason) {
	t.Helper()
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) || contractErr.Reason != want {
		t.Fatalf("error = %v, want graph-state reason %q", err, want)
	}
}
