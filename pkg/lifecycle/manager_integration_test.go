//go:build integration

// Integration test for pkg/lifecycle covering the production NATS
// wire path through a testcontainer. Unit tests in manager_test.go
// drive the fake emitter; this file drives the real
// graphEmitterNATS through a NATS testcontainer, exercising the
// graph.mutation.entity.* request/reply contract and the
// RequestWithRetry resilience that closes gh#170.
//
// Build-tagged so the unit-test layer stays Docker-free; run with
// `go test -tags=integration -race ./pkg/lifecycle/...`.

package lifecycle

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ManagerCreate_SurvivesGraphIngestColdStart is the
// regression test for gh#170. The lifecycle Manager.Create emits to
// graph.mutation.entity.create_with_triples; if graph-ingest's
// subscription hasn't propagated when the request lands, NATS returns
// "no responders" and the prior code surfaced that as a fatal error,
// terminating any lifecycle participant that fired Create on a fast
// boot path (cmd/e2e-semstreams/main.go --lifecycle-seed in the
// originating report).
//
// Setup: fire Manager.Create from a goroutine, wait until the first
// emit attempt has definitely happened (signaled via the trace
// timing-out at least once on a real no-responders error from NATS),
// then subscribe a stub responder. Asserts Create converges within
// the retry budget. Uses a sync point rather than a fixed sleep so
// the test exercises the retry-backoff path deterministically across
// host-load variation (reviewer feedback).
//
// Pre-fix this test fails on the first emit attempt; post-fix it
// converges on the retry that lands after the responder is up.
func TestIntegration_ManagerCreate_SurvivesGraphIngestColdStart(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()

	mgr := NewManager(tc.Client, nil)
	require.NoError(t, mgr.Register(lifecycle{}.fixtureWorkflow()))

	const entityID = "c360.platform1.lifecycle.gcs.mission.gh170"
	createCh := make(chan error, 1)
	createStarted := make(chan struct{})
	go func() {
		close(createStarted)
		createCh <- mgr.Create(ctx, &fixtureMission{
			ID:     entityID,
			PhaseF: "planning",
		})
	}()

	// Wait until the goroutine has at least scheduled the Create
	// call. The first emit attempt will fail synchronously with
	// nats.ErrNoResponders; retry-loop backoff (200ms initial) is
	// what gives us room to land the responder before the second
	// attempt fires. A tiny sleep after the start signal is enough
	// to let the first attempt land + fail.
	<-createStarted
	time.Sleep(50 * time.Millisecond)

	var responderHits atomic.Int32
	_, err := tc.Client.SubscribeForRequests(ctx, graphSubjectCreateWithTriples, func(_ context.Context, data []byte) ([]byte, error) {
		responderHits.Add(1)
		var req graph.CreateEntityWithTriplesRequest
		require.NoError(t, json.Unmarshal(data, &req))
		resp := graph.CreateEntityWithTriplesResponse{
			MutationResponse: graph.MutationResponse{Success: true, KVRevision: 1},
			Entity:           req.Entity,
			TriplesAdded:     len(req.Triples),
		}
		return json.Marshal(resp)
	})
	require.NoError(t, err)

	select {
	case err := <-createCh:
		require.NoError(t, err, "Manager.Create should converge on a retry after responder is up")
	case <-time.After(15 * time.Second):
		t.Fatal("Manager.Create did not complete within 15s — retry did not converge")
	}
	require.GreaterOrEqual(t, int(responderHits.Load()), 1, "responder should have received at least one delivery")
}
