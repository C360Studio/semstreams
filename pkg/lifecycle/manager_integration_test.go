//go:build integration

// Integration test for pkg/lifecycle covering the production canonical graph
// mutation request/reply path through a testcontainer. Unit tests in
// manager_test.go drive the fake emitter; this file drives graphEmitterNATS.
//
// Build-tagged so the unit-test layer stays Docker-free; run with
// `go test -tags=integration -race ./pkg/lifecycle/...`.

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type afterReconcileHook func(context.Context, jetstream.KeyValue, string, uint64) error

// startKVBackedResponders wires create, reconcile, and delete responders that apply to the
// REAL ENTITY_STATES KV bucket, so Manager reads (Get) and the KV watcher
// (Watch/WatchEvents) observe genuine puts and deletes. This drives the
// production graphEmitterNATS wire without importing processor/graph-ingest
// (the layering graph_emit.go deliberately avoids). Returns the KV handle.
func startKVBackedResponders(
	t *testing.T,
	tc *natsclient.TestClient,
	afterReconcile ...afterReconcileHook,
) jetstream.KeyValue {
	t.Helper()
	ctx := context.Background()
	kv, err := tc.Client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	reconcileSubject, err := graphmutation.ResolveSubject(graphmutation.SubjectFamily, graphmutation.ReconcilePredicates)
	require.NoError(t, err)
	_, err = tc.Client.SubscribeForRequests(ctx, reconcileSubject, func(requestCtx context.Context, data []byte) ([]byte, error) {
		var req graph.ReconcilePredicatesRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		entry, err := kv.Get(requestCtx, req.EntityID)
		if err != nil {
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, err)
			}
			return nil, err
		}
		var state graph.EntityState
		if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
			return nil, err
		}
		selected := make(map[string]struct{}, len(req.Predicates))
		for _, predicate := range req.Predicates {
			selected[predicate] = struct{}{}
		}
		triples := make([]message.Triple, 0, len(state.Triples)+len(req.Desired))
		for _, triple := range state.Triples {
			if _, replace := selected[triple.Predicate]; !replace {
				triples = append(triples, triple)
			}
		}
		state.Triples = append(triples, req.Desired...)
		body, err := graph.MarshalEntityState(&state)
		if err != nil {
			return nil, err
		}
		revision, err := kv.Update(requestCtx, req.EntityID, body, req.ExpectedRevision)
		if err != nil {
			if natsclient.IsKVConflictError(err) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeRevisionMismatch, err)
			}
			return nil, err
		}
		for _, hook := range afterReconcile {
			if hook != nil {
				if err := hook(requestCtx, kv, req.EntityID, revision); err != nil {
					return nil, err
				}
			}
		}
		return json.Marshal(graph.ReconcilePredicatesResponse{
			Outcome: graph.MutationApplied, Entity: state.Clone(), KVRevision: revision,
		})
	})
	require.NoError(t, err)
	startExactEntityResponder(t, tc, kv)

	createSubject, err := graphmutation.ResolveSubject(graphmutation.SubjectFamily, graphmutation.CreateEntity)
	require.NoError(t, err)
	_, err = tc.Client.SubscribeForRequests(ctx, createSubject, func(_ context.Context, data []byte) ([]byte, error) {
		var req graph.CreateEntityRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		st := *req.Entity
		st.Triples = req.Triples
		body, err := json.Marshal(&st)
		if err != nil {
			return nil, err
		}
		rev, err := kv.Put(ctx, st.ID, body)
		if err != nil {
			return nil, err
		}
		return json.Marshal(graph.CreateEntityResponse{
			Outcome: graph.MutationApplied, Entity: &st, KVRevision: rev,
		})
	})
	require.NoError(t, err)

	deleteSubject, err := graphmutation.ResolveSubject(graphmutation.SubjectFamily, graphmutation.DeleteEntity)
	require.NoError(t, err)
	_, err = tc.Client.SubscribeForRequests(ctx, deleteSubject, func(_ context.Context, data []byte) ([]byte, error) {
		var req graph.DeleteEntityRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		if err := kv.Delete(ctx, req.EntityID, jetstream.LastRevision(req.ExpectedRevision)); err != nil {
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, err)
			}
			if natsclient.IsKVConflictError(err) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeRevisionMismatch, err)
			}
			return nil, err
		}
		return json.Marshal(graph.DeleteEntityResponse{
			EntityID: req.EntityID, Outcome: graph.MutationApplied, ExpectedRevision: req.ExpectedRevision,
		})
	})
	require.NoError(t, err)

	return kv
}

func startExactEntityResponder(t *testing.T, tc *natsclient.TestClient, kv jetstream.KeyValue) {
	t.Helper()
	_, err := tc.Client.SubscribeForRequests(context.Background(), "graph.ingest.query.entity", func(ctx context.Context, data []byte) ([]byte, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeInvalidRequest, err)
		}
		entry, err := kv.Get(ctx, req.ID)
		if err != nil {
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, err)
			}
			return nil, err
		}
		var entity graph.EntityState
		if err := graph.UnmarshalEntityState(entry.Value(), &entity); err != nil {
			return nil, err
		}
		return json.Marshal(graph.ExactEntity{Entity: entity.Clone(), KVRevision: entry.Revision()})
	})
	require.NoError(t, err)
}

// TestIntegration_Despawn_RemovesEntity proves Manager.Despawn round-trips the
// real graph.mutation.entity.delete wire and reclaims the entity: a subsequent
// Get returns ErrEntityNotFound (gh#497, task 4.2).
func TestIntegration_Despawn_RemovesEntity(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	startKVBackedResponders(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	id := "c360.platform1.gcs.lifecycle.mission.int-dsp"
	require.NoError(t, mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}))
	_, err := mgr.Get(ctx, "fixture", id)
	require.NoError(t, err, "entity should exist after Create")

	require.NoError(t, mgr.Despawn(ctx, "fixture", id))

	_, err = mgr.Get(ctx, "fixture", id)
	require.ErrorIs(t, err, ErrEntityNotFound, "entity should be gone from ENTITY_STATES after Despawn")
}

func TestIntegration_DespawnWith_DoesNotDeleteNewerStateAfterTerminalCommit(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	var transitionRevision uint64
	var newerRevision uint64
	mutateAfterTransition := func(
		hookCtx context.Context,
		kv jetstream.KeyValue,
		entityID string,
		committedRevision uint64,
	) error {
		transitionRevision = committedRevision
		entry, err := kv.Get(hookCtx, entityID)
		if err != nil {
			return err
		}
		var state graph.EntityState
		if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
			return err
		}
		state.Triples = append(state.Triples, message.Triple{
			Subject: entityID, Predicate: "test.concurrent.value", Object: "newer",
		})
		body, err := graph.MarshalEntityState(&state)
		if err != nil {
			return err
		}
		newerRevision, err = kv.Update(hookCtx, entityID, body, committedRevision)
		return err
	}
	kv := startKVBackedResponders(t, tc, mutateAfterTransition)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const id = "c360.platform1.gcs.lifecycle.mission.int-dsp-cas"
	require.NoError(t, mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}))
	err := mgr.DespawnWith(ctx, "fixture", id, TransitionSourceRule, "raced-cull")
	require.ErrorIs(t, err, errs.ErrRevisionMismatch)
	require.NotZero(t, transitionRevision)
	require.Greater(t, newerRevision, transitionRevision)

	entry, err := kv.Get(ctx, id)
	require.NoError(t, err, "newer state must survive the stale conditional delete")
	require.Equal(t, newerRevision, entry.Revision())
	var preserved graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(entry.Value(), &preserved))
	require.Equal(t, "newer", extractTripleScalar(preserved.Triples, id, "test.concurrent.value"))
}

// requireEvent waits for an event matching (wantOp, wantID) on ch,
// skipping unrelated entities, and fails on timeout.
func requireEvent(t *testing.T, ch <-chan Event, wantOp EventOp, wantID string) {
	t.Helper()
	for {
		select {
		case ev, ok := <-ch:
			require.True(t, ok, "WatchEvents channel closed before %s %s", wantOp, wantID)
			if ev.EntityID != wantID {
				continue
			}
			require.Equal(t, wantOp, ev.Op, "op for %s", wantID)
			if wantOp == Upserted {
				require.NotNil(t, ev.Participant, "Upserted event must carry a Participant")
			} else {
				require.Nil(t, ev.Participant, "Deleted event must carry a nil Participant")
			}
			return
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s event for %s", wantOp, wantID)
		}
	}
}

// TestIntegration_WatchEvents_DeliversUpsertAndDelete proves the new
// delete-visible surface: WatchEvents delivers Upserted on create and Deleted
// on reclaim, while the existing upsert-only Watch delivers the upsert but NOT
// the delete (gh#497, tasks 3.4 + 4.3).
func TestIntegration_WatchEvents_DeliversUpsertAndDelete(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startKVBackedResponders(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	eventsCh, err := mgr.WatchEvents(ctx, "fixture")
	require.NoError(t, err)
	watchCh, err := mgr.Watch(ctx, "fixture")
	require.NoError(t, err)

	id := "c360.platform1.gcs.lifecycle.mission.int-we"

	require.NoError(t, mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"}))
	requireEvent(t, eventsCh, Upserted, id)

	// The companion Watch sees the upsert too.
	select {
	case p := <-watchCh:
		require.Equal(t, id, p.EntityID())
	case <-time.After(10 * time.Second):
		t.Fatal("Watch did not deliver the upsert")
	}

	require.NoError(t, mgr.Despawn(ctx, "fixture", id))
	requireEvent(t, eventsCh, Deleted, id)

	// Watch must NOT deliver the delete (upsert-only). WatchEvents already
	// observed the Deleted above, so the KV delete has propagated; any Watch
	// delivery within the guard window is an upsert-only-contract violation.
	select {
	case p, ok := <-watchCh:
		if ok {
			t.Fatalf("Watch delivered %q on delete — upsert-only surface violated", p.EntityID())
		}
	case <-time.After(1 * time.Second):
		// no delivery — correct
	}
}

// TestIntegration_CreateFromOperator_BirthLane drives gh#814's create lane
// against the real graph-mutation wire: an operator-supplied JSON initial
// state becomes a committed, readable, transitionable instance.
//
// This is the half the gateway's own tests cannot prove — they run against a
// fake manager, so they pin the HTTP contract while this pins that the decode
// → Create → read-back path actually round-trips through KV.
func TestIntegration_CreateFromOperator_BirthLane(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	startKVBackedResponders(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const id = "c360.platform1.gcs.lifecycle.mission.int-create"
	initial := []byte(`{"entity_id":"` + id + `","phase":"planning","owner_org_id":"acme"}`)

	result, err := mgr.CreateFromOperator(ctx, "fixture", initial)
	require.NoError(t, err, "create from an operator-supplied initial state")
	created := result.Instance
	require.NotNil(t, created)
	require.Equal(t, id, created.EntityID())
	require.Equal(t, "planning", created.Phase())

	// The returned value is the authoritative read-back, so an operator-
	// writable field supplied on the envelope must be visible on it — if the
	// create lane dropped the envelope and only wrote identity + phase, this
	// is what catches it.
	require.Equal(t, "acme", created.(*fixtureMission).OwnerOrgID,
		"initial-state envelope did not survive the create lane")

	// The returned instance is projected from the CAUSAL mutation response, not
	// from a later read — so it reflects what THIS request committed.

	// Independently readable through the normal Get path.
	got, err := mgr.Get(ctx, "fixture", id)
	require.NoError(t, err)
	require.Equal(t, "planning", got.Phase())

	// NOT COVERED HERE, deliberately: the subsequent transition and its
	// history replay. startKVBackedResponders serves canonical create and delete
	// only — Transition emits predicate reconcile, and standing up a fake
	// responder for it would mean hand-rolling graph-ingest's authority merge.
	// A fake merge that drifts from the real
	// one proves nothing about the real one, so this test asserts the create
	// lane and stops there.
	//
	// The full acceptance — fresh volume → create via the public route →
	// transition → restart → bounded history intact in the current entity — is an
	// e2e-level obligation against a running stack (it is also semdragon's
	// beta.159 replay path). Tracked as the coverage gap on gh#814 rather than
	// simulated here.
}

// TestIntegration_CreateFromOperator_IsCreateOrFail proves a duplicate is
// refused and does not clobber.
//
// SCOPE, corrected after review: this does NOT reach the CAS create-or-fail arm.
// The first create Puts the entity, so the second create's pre-read finds it and
// returns ErrAlreadyExists from the hasTriple check long before the emitter —
// mutation-proved by review, which disabled the emitter's ErrAlreadyExists
// classification and kept everything green. Reaching that arm needs a responder
// returning a classified graph.ErrorCodeEntityExists, which this harness does
// not have. What this test pins is the operator-visible contract: duplicate
// refused, original intact.
func TestIntegration_CreateFromOperator_IsCreateOrFail(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	startKVBackedResponders(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const id = "c360.platform1.gcs.lifecycle.mission.int-dup"
	first := []byte(`{"entity_id":"` + id + `","phase":"planning","owner_org_id":"first"}`)
	_, err := mgr.CreateFromOperator(ctx, "fixture", first)
	require.NoError(t, err)

	second := []byte(`{"entity_id":"` + id + `","phase":"planning","owner_org_id":"second"}`)
	_, err = mgr.CreateFromOperator(ctx, "fixture", second)
	require.ErrorIs(t, err, ErrAlreadyExists, "a duplicate create must fail, not upsert")

	got, err := mgr.Get(ctx, "fixture", id)
	require.NoError(t, err)
	require.Equal(t, "first", got.(*fixtureMission).OwnerOrgID,
		"the refused duplicate overwrote the original")
}

// TestIntegration_CreateFromOperator_RejectsUndeclaredInitialPhase pins that
// the phase validation inside Create still applies on this lane, and that it
// is reported as an invalid TRANSITION rather than an invalid initial state —
// the payload was well formed; the phase it named is not declared. The two
// need different corrections.
func TestIntegration_CreateFromOperator_RejectsUndeclaredInitialPhase(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	startKVBackedResponders(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const id = "c360.platform1.gcs.lifecycle.mission.int-badphase"
	initial := []byte(`{"entity_id":"` + id + `","phase":"not-a-declared-phase"}`)

	_, err := mgr.CreateFromOperator(ctx, "fixture", initial)
	require.ErrorIs(t, err, ErrInvalidTransition)
	require.NotErrorIs(t, err, ErrInvalidInitialState,
		"an undeclared phase is not a malformed payload — collapsing them sends operators to the wrong fix")

	_, getErr := mgr.Get(ctx, "fixture", id)
	require.ErrorIs(t, getErr, ErrEntityNotFound, "a refused create must leave nothing behind")
}
