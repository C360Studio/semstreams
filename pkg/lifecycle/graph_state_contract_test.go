package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

func TestGetPoisonIsScopedAndRepairIsObservedOnNextRead(t *testing.T) {
	t.Parallel()

	mgr, _, bucket := newTestManager(t)
	poisonID := "acme.ops.lifecycle.gcs.mission.poisoned-a"
	validID := "acme.ops.lifecycle.gcs.mission.valid-b"
	bucket.raw[poisonID] = poisonedLifecycleState(poisonID)
	bucket.put(validID, validLifecycleState(validID))

	participant, err := mgr.Get(context.Background(), "fixture", poisonID)
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("Get error = %T %v, want fatal reset-required", err, err)
	}
	if participant != nil {
		t.Fatalf("Get participant = %#v, want nil", participant)
	}
	assertResetReason(t, err, graph.GraphStateReasonNoncanonicalPredicate)

	participant, err = mgr.Get(context.Background(), "fixture", validID)
	if err != nil || participant == nil || participant.EntityID() != validID {
		t.Fatalf("unrelated Get = (%#v, %v), want valid B", participant, err)
	}

	delete(bucket.raw, poisonID)
	bucket.put(poisonID, validLifecycleState(poisonID))
	participant, err = mgr.Get(context.Background(), "fixture", poisonID)
	if err != nil || participant == nil || participant.EntityID() != poisonID {
		t.Fatalf("repaired Get = (%#v, %v), want repaired A", participant, err)
	}
}

func TestPoisonedMutationPreconditionEmitsNoMutation(t *testing.T) {
	t.Parallel()
	mgr, emitter, bucket := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.poisoned"
	bucket.raw[entityID] = poisonedLifecycleState(entityID)

	err := mgr.Transition(context.Background(), "fixture", entityID, "flying", TransitionSourceRule, "go")
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("Transition error = %T %v, want fatal reset-required", err, err)
	}
	if len(emitter.requests) != 0 {
		t.Fatalf("mutation requests = %d, want zero", len(emitter.requests))
	}
}

func TestListFiltersWorkflowBeforeDecodeAndReturnsNoPartialOnMatchingPoison(t *testing.T) {
	t.Parallel()
	mgr, _, bucket := newTestManager(t)
	validID := "acme.ops.lifecycle.gcs.mission.valid"
	nonmatchingPoisonID := "acme.ops.other.gcs.device.poison"
	bucket.put(validID, validLifecycleState(validID))
	bucket.raw[nonmatchingPoisonID] = poisonedLifecycleState(nonmatchingPoisonID)
	bucket.listKeys = []string{nonmatchingPoisonID, validID}

	participants, err := mgr.List(context.Background(), "fixture", ListOptions{})
	if err != nil || len(participants) != 1 || participants[0].EntityID() != validID {
		t.Fatalf("List with nonmatching poison = (%#v, %v), want valid entity", participants, err)
	}

	matchingPoisonID := "acme.ops.lifecycle.gcs.mission.poison"
	bucket.raw[matchingPoisonID] = poisonedLifecycleState(matchingPoisonID)
	bucket.listKeys = []string{validID, matchingPoisonID}
	participants, err = mgr.List(context.Background(), "fixture", ListOptions{})
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("List matching poison error = %T %v, want fatal", err, err)
	}
	if participants != nil {
		t.Fatalf("List matching poison returned partial %#v, want nil", participants)
	}
}

type classifiedPoisonRequester struct {
	entityID string
}

func (r classifiedPoisonRequester) RequestClassified(
	context.Context,
	string,
	[]byte,
	time.Duration,
) ([]byte, error) {
	msg := &nats.Msg{
		Header: nats.Header{},
		Data: []byte(`{"message":"graph_state_reset_required: noncanonical_predicate: entity ` + r.entityID +
			`","detail":{"reason":"noncanonical_predicate","entity_id":"` + r.entityID + `"}}`),
	}
	msg.Header.Set(natsclient.HeaderStatus, natsclient.HeaderStatusError)
	msg.Header.Set(natsclient.HeaderErrorClass, natsclient.ErrorClassFatal)
	msg.Header.Set(natsclient.HeaderErrorCode, graph.ErrorCodeGraphStateResetRequired)
	return natsclient.ClassifyReply(msg)
}

func TestProductionExactRPCPoisonPreservesClassificationWithoutConcreteCause(t *testing.T) {
	t.Parallel()
	mgr, emitter, _ := newTestManager(t)
	entityID := "acme.ops.lifecycle.gcs.mission.poisoned"
	mgr.exactReader = graph.NewExactEntityReader(classifiedPoisonRequester{entityID: entityID}, time.Second)

	participant, err := mgr.Get(context.Background(), "fixture", entityID)
	if participant != nil || err == nil || !errs.IsFatal(err) {
		t.Fatalf("Get = (%#v, %v), want nil fatal", participant, err)
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("Get error = %v, want code %q", err, graph.ErrorCodeGraphStateResetRequired)
	}
	if classified.Detail["reason"] != string(graph.GraphStateReasonNoncanonicalPredicate) ||
		classified.Detail["entity_id"] != entityID {
		t.Fatalf("classified detail = %#v, want poison reason/entity", classified.Detail)
	}
	var contractErr *graph.StateContractError
	if errors.As(err, &contractErr) {
		t.Fatalf("production RPC unexpectedly preserved concrete StateContractError: %#v", contractErr)
	}
	if len(emitter.requests) != 0 {
		t.Fatalf("mutation requests = %d, want zero", len(emitter.requests))
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
