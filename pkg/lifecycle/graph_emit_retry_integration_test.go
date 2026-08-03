//go:build integration

// Integration coverage for the create-retry narrowing (gh#861).
//
// graphEmitterNATS.create re-sends ONLY on failures that PROVE the request was
// never delivered to a graph-ingest handler: "no responders" from the server,
// and the client's own refusals to send (open circuit breaker, no connection).
// Every other transport failure (notably a per-attempt timeout against a handler
// that is still running) leaves the outcome UNKNOWN, and re-sending a
// non-idempotent create against a live handler is how a request manufactures a
// conflict with itself.
//
// Three facts have to hold on the real wire for that policy to be correct, and
// all three are pinned here rather than asserted in prose:
//
//  1. an absent responder really does surface as nats.ErrNoResponders on this
//     server configuration (natsclient.IsNoResponders is documented as
//     server-config dependent),
//  2. a per-attempt timeout against a live-but-slow handler produces exactly
//     one delivery, and
//  3. a create rides through the client's OWN breaker opening mid-loop — the
//     class a per-attempt request re-checks and the replaced retry loop did
//     not, so narrowing without it would silently drop the cold-start budget.
//
// Build-tagged; run with `go test -tags=integration -race ./pkg/lifecycle/...`.

package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestIntegration_AbsentResponderIsNoRespondersNotTimeout is the empirical gate
// the whole narrowing rests on. natsclient.IsNoResponders documents that
// "whether an absent responder surfaces as ErrNoResponders vs a plain timeout is
// server-config dependent" — if this server answered an unsubscribed subject
// with a bare timeout, narrowing create's retry to the no-responders class would
// silently delete the gh#170 cold-start protection instead of narrowing it.
//
// Also asserts the failure is FAST (well inside the request timeout): a
// no-responders reply is a server-sent 503, so it returns immediately, whereas a
// timeout would consume the whole deadline. A slow "no responders" would mean the
// signal was synthesized somewhere else and is not the server's.
func TestIntegration_AbsentResponderIsNoRespondersNotTimeout(t *testing.T) {
	tc := natsclient.NewTestClient(t)
	ctx := context.Background()

	const timeout = 3 * time.Second
	start := time.Now()
	_, err := tc.Client.RequestClassified(ctx, graphSubjectCreateWithTriples, []byte(`{}`), timeout)
	elapsed := time.Since(start)

	t.Logf("unsubscribed-subject request returned after %s: %v (IsNoResponders=%v)",
		elapsed, err, natsclient.IsNoResponders(err))
	require.Error(t, err, "a request to an unsubscribed subject must fail")
	require.Truef(t, natsclient.IsNoResponders(err),
		"absent responder surfaced as %T %v, not nats.ErrNoResponders — "+
			"create's retry narrowing would delete gh#170 cold-start protection on this server config", err, err)
	require.Lessf(t, elapsed, timeout,
		"no-responders took %s of a %s deadline — that is a timeout wearing the wrong label", elapsed, timeout)
}

// TestIntegration_CreateDoesNotResendOnTimeout pins the narrowing itself: a
// live handler that is slower than the per-attempt deadline must receive the
// create EXACTLY ONCE. Before gh#861 the emitter used
// RequestWithRetryClassified, whose loop re-sends on ANY non-nil error — so a
// 5s lifecycle deadline against a 30s graph-ingest handler deadline re-sent a
// NON-IDEMPOTENT create up to ten times against a handler that was still
// executing the first one. The second delivery then answered
// entity_already_exists for a birth this same request had just made.
func TestIntegration_CreateDoesNotResendOnTimeout(t *testing.T) {
	tc := natsclient.NewTestClient(t)
	ctx := context.Background()

	var deliveries atomic.Int32
	handlerDone := make(chan struct{})
	_, err := tc.Client.SubscribeForRequests(ctx, graphSubjectCreateWithTriples,
		func(_ context.Context, data []byte) ([]byte, error) {
			deliveries.Add(1)
			// Slower than the emitter's per-attempt deadline below: this is the
			// live-handler-still-running case, NOT a missing responder.
			time.Sleep(2 * time.Second)
			var req graph.CreateEntityWithTriplesRequest
			if uerr := json.Unmarshal(data, &req); uerr != nil {
				return nil, uerr
			}
			select {
			case handlerDone <- struct{}{}:
			default:
			}
			return json.Marshal(graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 1},
				Entity:           req.Entity,
				TriplesAdded:     len(req.Triples),
			})
		})
	require.NoError(t, err)

	const entityID = "c360.platform1.lifecycle.gcs.mission.slowhandler"
	emitter := newGraphEmitterNATS(tc.Client, 300*time.Millisecond)
	_, err = emitter.create(ctx, &graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: entityID, Version: 1, UpdatedAt: time.Now(), MessageType: lifecycleMessageType},
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "workflow.lifecycle.phase", Object: "planning", Confidence: 1.0,
		}},
	})
	require.Error(t, err, "a per-attempt timeout must surface as an error, not be retried into a false conflict")
	require.False(t, natsclient.IsNoResponders(err), "the responder was subscribed; this is a timeout, not no-responders")

	// Let the in-flight handler finish, then give the retry loop that USED to
	// exist a generous window to fire a second delivery.
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
	}
	time.Sleep(1 * time.Second)
	require.Equalf(t, int32(1), deliveries.Load(),
		"create was delivered %d times to a live handler — a non-idempotent create must not be re-sent on a timeout",
		deliveries.Load())
}

// TestIntegration_CreateSurvivesBreakerOpeningMidColdStart pins the half of the
// retry predicate that is NOT about the wire: a create must ride through the
// client's OWN circuit breaker opening while it waits for graph-ingest.
//
// The breaker counts CONSECUTIVE failures on the SHARED client (threshold 15)
// and each cold-start create burns up to 11, so two concurrent creates — the
// shape processor/gated-dag/executor.go produces by calling Manager.Create per
// node — trip it partway through the second. The replaced loop
// (natsclient.requestMsgWithRetry) checked connection and breaker state ONCE
// before its first attempt, so a breaker that opened mid-loop never terminated
// it; a per-attempt RequestClassified re-checks both EVERY call
// (natsclient/request.go RequestWithHeaders) and would abort with
// ErrCircuitOpen, discarding the remaining cold-start budget. That is the gh#170
// failure the budget exists to prevent, re-entering through the fix for gh#861.
//
// The breaker is driven to the threshold explicitly rather than by hoping a
// concurrent create trips it: an unfired guard and a green test look identical.
func TestIntegration_CreateSurvivesBreakerOpeningMidColdStart(t *testing.T) {
	tc := natsclient.NewTestClient(t)
	ctx := context.Background()

	// Prime the shared client to one failure below the threshold. Each failed
	// request records a failure; no success intervenes, so none of them reset.
	const threshold = 15
	for i := 0; i < threshold-1; i++ {
		_, err := tc.Client.RequestClassified(ctx, "graph.mutation.entity.create_with_triples.nobody",
			[]byte(`{}`), 500*time.Millisecond)
		require.Error(t, err)
	}
	require.NotEqual(t, natsclient.StatusCircuitOpen, tc.Client.Status(),
		"fixture is wrong: the breaker must still be closed when the create starts")

	// The create's own first attempt is failure #15 and opens the breaker. Its
	// SECOND attempt is the one that used to die on ErrCircuitOpen.
	const entityID = "c360.platform1.lifecycle.gcs.mission.breaker"
	emitter := newGraphEmitterNATS(tc.Client, 500*time.Millisecond)
	createErr := make(chan error, 1)
	go func() {
		_, err := emitter.create(ctx, &graph.CreateEntityWithTriplesRequest{
			Entity: &graph.EntityState{ID: entityID, Version: 1, UpdatedAt: time.Now(), MessageType: lifecycleMessageType},
			Triples: []message.Triple{{
				Subject: entityID, Predicate: "workflow.lifecycle.phase", Object: "planning", Confidence: 1.0,
			}},
		})
		createErr <- err
	}()

	// Land the responder after the breaker has had time to open and self-close
	// (recordFailure schedules testCircuit one backoff later, initial 1s), so
	// the create can only converge by having ridden THROUGH the open breaker.
	time.Sleep(1500 * time.Millisecond)
	var deliveries atomic.Int32
	_, err := tc.Client.SubscribeForRequests(ctx, graphSubjectCreateWithTriples,
		func(_ context.Context, data []byte) ([]byte, error) {
			deliveries.Add(1)
			var req graph.CreateEntityWithTriplesRequest
			if uerr := json.Unmarshal(data, &req); uerr != nil {
				return nil, uerr
			}
			return json.Marshal(graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 1},
				Entity:           req.Entity,
				TriplesAdded:     len(req.Triples),
			})
		})
	require.NoError(t, err)

	select {
	case err := <-createErr:
		require.NoErrorf(t, err,
			"create abandoned its cold-start budget when the breaker opened mid-loop; "+
				"the replaced loop rode through this and %d deliveries reached the handler", deliveries.Load())
	case <-time.After(20 * time.Second):
		t.Fatal("create did not complete within the retry budget")
	}
	require.Equalf(t, int32(1), deliveries.Load(),
		"the converged create was delivered %d times; a non-idempotent create must land exactly once",
		deliveries.Load())
}

// TestIntegration_ConcurrentCreateOnlyOneWins drives concurrent births of the
// SAME entity ID through the production graphEmitterNATS, the wire envelope, and
// the classified entity_already_exists path that becomes ErrAlreadyExists — the
// seam the lifecycle gateway maps to 409 — arbitrated by an atomic KV create
// rather than by a mutex in a fake.
//
// It is a CONTRACT PIN, NOT the gh#861 regression gate, and the difference is
// measured rather than assumed: against the pre-fix manager this test is green
// 3/3, because each goroutine's `now` is spread by a real NATS round trip and the
// deleted re-read only false-positives when two writers' RFC3339Nano stamps
// collide. The gate that fails without the fix is the unit-level
// TestManager_ConcurrentCreateOnlyOneWins (in-process, no I/O between the
// stamps), run in its powered form — see the change's tasks.md. Reporting this
// test as the gate would be claiming coverage it does not have.
func TestIntegration_ConcurrentCreateOnlyOneWins(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()
	startArbitratingCreateResponder(t, tc)

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const id = "c360.platform1.lifecycle.gcs.mission.int-race"
	const n = 8
	results := make(chan error, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		go func() {
			start.Wait()
			results <- mgr.Create(ctx, &fixtureMission{ID: id, PhaseF: "planning"})
		}()
	}
	start.Done()

	var successes, conflicts int
	var others []error
	for i := 0; i < n; i++ {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyExists):
			conflicts++
		default:
			others = append(others, err)
		}
	}
	require.Equalf(t, 1, successes,
		"expected exactly 1 winner across %d concurrent births, got %d (conflicts=%d, other=%v)",
		n, successes, conflicts, others)
	require.Emptyf(t, others, "losers must surface ErrAlreadyExists, got %v", others)
}

// startArbitratingCreateResponder wires a create_with_triples responder that
// ARBITRATES: jetstream KV Create is atomic create-or-fail, and a loser gets the
// same classified graph.ErrorCodeEntityExists that
// processor/graph-ingest returns (rejectInvalidDetail). The
// startKVBackedResponders helper in manager_integration_test.go uses Put, which
// silently lets every concurrent writer "win" — fine for the round-trip tests it
// serves, useless for a race.
func startArbitratingCreateResponder(t *testing.T, tc *natsclient.TestClient) {
	t.Helper()
	ctx := context.Background()
	kv, err := tc.Client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	_, err = tc.Client.SubscribeForRequests(ctx, graphSubjectCreateWithTriples,
		func(_ context.Context, data []byte) ([]byte, error) {
			var req graph.CreateEntityWithTriplesRequest
			if uerr := json.Unmarshal(data, &req); uerr != nil {
				return nil, uerr
			}
			st := *req.Entity
			st.Triples = req.Triples
			body, merr := json.Marshal(&st)
			if merr != nil {
				return nil, merr
			}
			rev, cerr := kv.Create(ctx, st.ID, body)
			if cerr != nil {
				if errors.Is(cerr, jetstream.ErrKeyExists) {
					return nil, errs.ClassifiedCodeDetail(errs.ErrorInvalid, graph.ErrorCodeEntityExists,
						map[string]any{"entity": st.ID},
						fmt.Errorf("entity already exists: %s", st.ID))
				}
				return nil, cerr
			}
			return json.Marshal(graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: rev},
				Entity:           &st,
				TriplesAdded:     len(req.Triples),
			})
		})
	require.NoError(t, err)
}
