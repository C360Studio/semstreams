package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
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

func TestHistoryReturnsNoPartialEventsWhenAnyRevisionIsPoisoned(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.history"
	valid, err := graph.MarshalEntityState(&graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "mission.lifecycle.phase", Object: "planning",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bucket.history[entityID] = []jetstream.KeyValueEntry{
		&fakeKVEntry{key: entityID, value: valid, revision: 1, created: time.Now()},
		&fakeKVEntry{key: entityID, value: poisonedLifecycleState(entityID), revision: 2, created: time.Now()},
	}

	events, err := mgr.History(context.Background(), "fixture", entityID)
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("History error = %T %v, want fatal reset-required", err, err)
	}
	if events != nil {
		t.Fatalf("History events = %#v, want nil rather than partial revision replay", events)
	}
	assertResetReason(t, err, graph.GraphStateReasonNoncanonicalPredicate)
}

func poisonedLifecycleState(entityID string) []byte {
	return []byte(`{"id":"` + entityID + `","triples":[{"subject":"` + entityID + `","predicate":"legacy.predicate","object":"planning"}]}`)
}

func assertResetReason(t *testing.T, err error, want graph.StateResetReason) {
	t.Helper()
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) || contractErr.Reason != want {
		t.Fatalf("error = %v, want graph-state reason %q", err, want)
	}
}
