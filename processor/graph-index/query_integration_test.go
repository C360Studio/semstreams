//go:build integration

package graphindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIntegrationTest creates a real NATS container and component using natsclient.TestClient
// Each test gets its own NATS container, so bucket isolation is automatic.
// setupIntegrationTest starts a real graph-index against a real NATS. preStart hooks
// run after Initialize and BEFORE Start, which is the only safe window for the
// test-seam fields the tick goroutine reads at creation (statusInterval).
func setupIntegrationTest(t *testing.T, preStart ...func(*Component)) (*Component, *natsclient.Client, func()) {
	t.Helper()

	ctx := context.Background()

	// Use natsclient.NewTestClient with pre-created ENTITY_STATES bucket
	// The component waits for this bucket in Start() with retry.Persistent()
	testClient := natsclient.NewTestClient(t,
		natsclient.WithKVBuckets(graph.BucketEntityStates),
	)

	// Create component
	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: testClient.Client,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIndex(configJSON, deps)
	require.NoError(t, err)

	graphIndexComp := comp.(*Component)

	// Initialize and start component (ENTITY_STATES bucket already exists)
	// Component.Start() calls setupQueryHandlers() which registers NATS subscriptions
	require.NoError(t, graphIndexComp.Initialize())
	for _, hook := range preStart {
		hook(graphIndexComp)
	}
	require.NoError(t, graphIndexComp.Start(ctx))

	// Wait for component and its query handlers to be ready
	time.Sleep(200 * time.Millisecond)

	// Cleanup function - testClient.Terminate() is called by t.Cleanup automatically
	cleanup := func() {
		graphIndexComp.Stop(5 * time.Second)
	}

	return graphIndexComp, testClient.Client, cleanup
}

// TestQueryOutgoing_Integration tests outgoing query with real NATS
func TestQueryOutgoing_Integration(t *testing.T) {
	comp, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test data - add entity state to trigger indexing
	entityID := "c360.platform.robotics.mav1.drone.001"
	targetID := "c360.platform.robotics.mav1.mission.001"
	predicate := "robotics.assigned.mission"

	// Write entity state to trigger indexing
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: semantictest.Predicate(t, "robotics", "assigned", "mission"),
				Object:    targetID,
			},
		},
	}

	stateJSON, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateJSON)
	require.NoError(t, err)

	// Create query request
	request := map[string]string{"entity_id": entityID}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Synchronize on the query-visible projection rather than sleeping. The classified
	// request path rejects typed index-not-ready responses instead of letting their JSON
	// envelopes decode as a false empty success.
	require.Eventually(t, func() bool {
		data, requestErr := natsClient.RequestClassified(
			ctx, "graph.index.query.outgoing", requestJSON, 2*time.Second,
		)
		if requestErr != nil {
			return false
		}
		var current graph.OutgoingQueryResponse
		if json.Unmarshal(data, &current) != nil || len(current.Data.Relationships) != 1 {
			return false
		}
		return current.Data.Relationships[0].ToEntityID == targetID &&
			current.Data.Relationships[0].Predicate == predicate
	}, 3*time.Second, 25*time.Millisecond, "initial relationship never became query-visible")

	// The entity remains present while its authoritative relationship set becomes
	// empty. The watcher reconciliation must overwrite the owner key with [] and the
	// query surface must stop returning the removed edge.
	state.Triples = nil
	stateJSON, err = json.Marshal(state)
	require.NoError(t, err)
	_, err = entityBucket.Put(ctx, entityID, stateJSON)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		data, requestErr := natsClient.RequestClassified(
			ctx, "graph.index.query.outgoing", requestJSON, 2*time.Second,
		)
		if requestErr != nil {
			return false
		}
		var current graph.OutgoingQueryResponse
		if json.Unmarshal(data, &current) != nil {
			return false
		}
		return len(current.Data.Relationships) == 0
	}, 3*time.Second, 25*time.Millisecond, "removed relationship remained query-visible")

	ownerEntry, err := comp.outgoingBucket.Get(ctx, entityID)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(ownerEntry.Value))
}

// TestQueryIncoming_Integration tests incoming query with real NATS
func TestQueryIncoming_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test data - add entity state to trigger indexing
	sourceID := "c360.platform.robotics.mav1.drone.001"
	targetID := "c360.platform.robotics.mav1.mission.001"
	predicate := "robotics.assigned.mission"

	// Write entity state to trigger indexing
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	state := graph.EntityState{
		ID: sourceID,
		Triples: []message.Triple{
			{
				Subject:   sourceID,
				Predicate: semantictest.Predicate(t, "robotics", "assigned", "mission"),
				Object:    targetID,
			},
		},
	}

	stateJSON, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, sourceID, stateJSON)
	require.NoError(t, err)

	// Wait for indexing
	time.Sleep(300 * time.Millisecond)

	// Create query request for incoming relationships
	nc := natsClient.GetConnection()
	request := map[string]string{"entity_id": targetID}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Send query request
	msg, err := nc.Request("graph.index.query.incoming", requestJSON, 2*time.Second)
	require.NoError(t, err)

	// Parse response (envelope: {"data": {"relationships": [...]}, ...})
	var response graph.IncomingQueryResponse
	err = json.Unmarshal(msg.Data, &response)
	require.NoError(t, err)

	// Verify response
	assert.Len(t, response.Data.Relationships, 1, "should have one incoming relationship")
	if len(response.Data.Relationships) > 0 {
		assert.Equal(t, sourceID, response.Data.Relationships[0].FromEntityID)
		assert.Equal(t, predicate, response.Data.Relationships[0].Predicate)
	}
}

// TestQueryAlias_Integration tests alias query with real NATS
func TestQueryAlias_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test data with alias
	entityID := "c360.platform.robotics.mav1.drone.001"
	alias := "drone-001"

	// Write entity state with alias to trigger indexing
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{
				Subject:   entityID,
				Predicate: "core.identity.alias",
				Object:    alias,
			},
		},
	}

	stateJSON, err := json.Marshal(state)
	require.NoError(t, err)

	_, err = entityBucket.Put(ctx, entityID, stateJSON)
	require.NoError(t, err)

	// Wait for indexing
	time.Sleep(300 * time.Millisecond)

	// Create query request
	nc := natsClient.GetConnection()
	request := map[string]string{"alias": alias}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Send query request
	msg, err := nc.Request("graph.index.query.alias", requestJSON, 2*time.Second)
	require.NoError(t, err)

	// Parse response (envelope: {"data": {"canonical_id": "..."}, ...})
	var response graph.AliasQueryResponse
	err = json.Unmarshal(msg.Data, &response)
	require.NoError(t, err)

	// Verify response
	require.NotNil(t, response.Data.CanonicalID, "canonical_id should not be nil")
	assert.Equal(t, entityID, *response.Data.CanonicalID)
}

// TestQueryPredicate_Integration tests predicate query with real NATS
func TestQueryPredicate_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test data with multiple entities using same predicate
	entities := []string{
		"c360.platform.robotics.mav1.drone.001",
		"c360.platform.robotics.mav1.drone.002",
	}

	// Write entity states to trigger indexing
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	for _, entityID := range entities {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: semantictest.Predicate(t, "robotics", "type", "drone"),
					Object:    "drone",
				},
			},
		}

		stateJSON, err := json.Marshal(state)
		require.NoError(t, err)

		_, err = entityBucket.Put(ctx, entityID, stateJSON)
		require.NoError(t, err)
	}

	// Wait for indexing
	time.Sleep(300 * time.Millisecond)

	// Create query request
	nc := natsClient.GetConnection()
	request := map[string]string{"predicate": semantictest.Predicate(t, "robotics", "type", "drone")}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Send query request
	msg, err := nc.Request("graph.index.query.predicate", requestJSON, 2*time.Second)
	require.NoError(t, err)

	// Parse response (envelope: {"data": {"entities": [...]}, ...})
	var response graph.PredicateQueryResponse
	err = json.Unmarshal(msg.Data, &response)
	require.NoError(t, err)

	// Verify response
	assert.Len(t, response.Data.Entities, 2, "should have two entities with predicate")

	// Verify all expected entities are present
	entityMap := make(map[string]bool)
	for _, id := range response.Data.Entities {
		entityMap[id] = true
	}

	for _, expected := range entities {
		assert.True(t, entityMap[expected], "entity %s should be in response", expected)
	}
}

// TestContextTimeout_Integration tests context timeout behavior
func TestContextTimeout_Integration(t *testing.T) {
	comp, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Add data first
	entityID := "c360.platform.robotics.mav1.drone.001"
	require.NoError(t, comp.UpdateOutgoingIndex(ctx, entityID,
		"c360.platform.robotics.mav1.mission.001", "robotics.assigned.mission"))

	// Create query request
	nc := natsClient.GetConnection()
	request := map[string]string{"entity_id": entityID}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Send query request (should complete within timeout)
	msg, err := nc.Request("graph.index.query.outgoing", requestJSON, 2*time.Second)
	require.NoError(t, err)

	// Verify we got a response (envelope format)
	var response graph.OutgoingQueryResponse
	err = json.Unmarshal(msg.Data, &response)
	require.NoError(t, err)
}

// TestConcurrentQueries_Integration tests concurrent query requests
func TestConcurrentQueries_Integration(t *testing.T) {
	comp, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Add multiple entities
	for i := 0; i < 10; i++ {
		entityID := "c360.platform.robotics.mav1.drone." + string(rune('0'+i))
		require.NoError(t, comp.UpdateOutgoingIndex(ctx, entityID,
			"c360.platform.robotics.mav1.mission.001", "robotics.assigned.mission"))
	}

	// Create query requests concurrently
	nc := natsClient.GetConnection()
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			entityID := "c360.platform.robotics.mav1.drone." + string(rune('0'+idx))
			request := map[string]string{"entity_id": entityID}
			requestJSON, _ := json.Marshal(request)

			// Send query request
			msg, err := nc.Request("graph.index.query.outgoing", requestJSON, 2*time.Second)
			assert.NoError(t, err)

			if err == nil {
				var response graph.OutgoingQueryResponse
				err = json.Unmarshal(msg.Data, &response)
				assert.NoError(t, err)
			}

			done <- true
		}(i)
	}

	// Wait for all queries to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// Query completed
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent query timed out")
		}
	}
}

// TestQueryNotFound_Integration tests not found scenarios
func TestQueryNotFound_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	nc := natsClient.GetConnection()

	t.Run("outgoing not found", func(t *testing.T) {
		request := map[string]string{"entity_id": "test.graph.index.query.entity.missing"}
		requestJSON, err := json.Marshal(request)
		require.NoError(t, err)

		msg, err := nc.Request("graph.index.query.outgoing", requestJSON, 2*time.Second)
		require.NoError(t, err)

		var response graph.OutgoingQueryResponse
		err = json.Unmarshal(msg.Data, &response)
		require.NoError(t, err)
		assert.Empty(t, response.Data.Relationships, "relationships should be empty for not found")
	})

	t.Run("incoming not found", func(t *testing.T) {
		request := map[string]string{"entity_id": "test.graph.index.query.entity.missing"}
		requestJSON, err := json.Marshal(request)
		require.NoError(t, err)

		msg, err := nc.Request("graph.index.query.incoming", requestJSON, 2*time.Second)
		require.NoError(t, err)

		var response graph.IncomingQueryResponse
		err = json.Unmarshal(msg.Data, &response)
		require.NoError(t, err)
		assert.Empty(t, response.Data.Relationships, "relationships should be empty for not found")
	})

	t.Run("alias not found", func(t *testing.T) {
		request := map[string]string{"alias": "non-existent-alias"}
		requestJSON, err := json.Marshal(request)
		require.NoError(t, err)

		msg, err := nc.Request("graph.index.query.alias", requestJSON, 2*time.Second)
		require.NoError(t, err)

		var response graph.AliasQueryResponse
		err = json.Unmarshal(msg.Data, &response)
		require.NoError(t, err)
		assert.Nil(t, response.Data.CanonicalID, "canonical_id should be nil for not found")
	})

	t.Run("predicate not found", func(t *testing.T) {
		request := map[string]string{"predicate": "non.existent.predicate"}
		requestJSON, err := json.Marshal(request)
		require.NoError(t, err)

		msg, err := nc.Request("graph.index.query.predicate", requestJSON, 2*time.Second)
		require.NoError(t, err)

		var response graph.PredicateQueryResponse
		err = json.Unmarshal(msg.Data, &response)
		require.NoError(t, err)
		assert.Empty(t, response.Data.Entities, "entities should be empty for not found")
	})
}

// TestQueryInvalidRequest_Integration tests invalid request handling.
//
// ADR-060 PR-B: the *NATS query handlers now return a classified Go
// error (errs.ClassifiedCode(ErrorInvalid, ErrorCodeInvalidRequest, ...))
// instead of a QueryResponse[T]{Error} success envelope. Drive the
// round-trip through the production RequestClassified path and assert the
// reconstructed *errs.ClassifiedError carries the invalid class + the
// stable invalid_request code.
func TestQueryInvalidRequest_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name    string
		subject string
		request []byte
	}{
		{
			name:    "outgoing malformed JSON",
			subject: "graph.index.query.outgoing",
			request: []byte(`{invalid json}`),
		},
		{
			name:    "outgoing empty entity_id",
			subject: "graph.index.query.outgoing",
			request: []byte(`{"entity_id": ""}`),
		},
		{
			name:    "incoming empty entity_id",
			subject: "graph.index.query.incoming",
			request: []byte(`{"entity_id": ""}`),
		},
		{
			name:    "alias empty alias",
			subject: "graph.index.query.alias",
			request: []byte(`{"alias": ""}`),
		},
		{
			name:    "predicate empty predicate",
			subject: "graph.index.query.predicate",
			request: []byte(`{"predicate": ""}`), // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		},
		{
			name:    "predicateStats empty predicate",
			subject: "graph.index.query.predicateStats",
			request: []byte(`{"predicate": ""}`), // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		},
		{
			name:    "predicateCompound empty predicates",
			subject: "graph.index.query.predicateCompound",
			request: []byte(`{"predicates": [], "operator": "AND"}`),
		},
		{
			name:    "predicateCompound bad operator",
			subject: "graph.index.query.predicateCompound",
			request: []byte(`{"predicates": ["p"], "operator": "XOR"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Drive the production caller path: RequestClassified runs the
			// reply through ClassifyReply so a handler Go-error arrives as a
			// classified error rather than a success envelope.
			respData, err := natsClient.RequestClassified(ctx, tt.subject, tt.request, 2*time.Second)
			require.Error(t, err, "invalid request must surface as a classified error")
			require.Nil(t, respData, "no success body on the failure path")

			// Coarse class branch (works for every consumer today).
			assert.True(t, errs.IsInvalid(err), "invalid request should classify as ErrorInvalid")

			// Stable machine code reachable via errors.As (ADR-060).
			var ce *errs.ClassifiedError
			require.True(t, errors.As(err, &ce), "error must be a *errs.ClassifiedError")
			assert.Equal(t, graph.ErrorCodeInvalidRequest, ce.Code,
				"invalid request must carry the invalid_request code")
		})
	}
}

// TestQueryStatus_RevisionLag_Integration drives the full production wire (real
// WatchAll delivery, real entry.Revision(), real BucketLastSeq against the KV
// backing stream) to prove the ADR-066 caught-up contract end to end: an empty
// bucket is not-ready with a zero target, and after the writes settle the status
// catches up to Lag==0 with the numeric revision fields populated (not the old
// sticky NAME_INDEX-non-empty false-ready).
// TestQueryStatus_NonEmptyReplay_NotReadyUntilCaughtUp covers Codex #1: the WatchAll
// initial-sync sentinel means pre-existing entries were DELIVERED, not that their async
// worker writes completed. A non-empty preloaded bucket must NOT read ready (nor serve
// reverse-index queries) until the watermark is actually caught up. Preloads entities
// BEFORE Start, then asserts the query gate and status agree throughout replay: an
// incoming query never succeeds while status reports not-ready (which the pre-fix
// sentinel-sets-indexBootstrapped bug would violate).
func TestQueryStatus_NonEmptyReplay_NotReadyUntilCaughtUp(t *testing.T) {
	ctx := context.Background()
	testClient := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	nc := testClient.Client.GetConnection()

	// Preload the bucket with entities BEFORE the component starts, so the initial-sync
	// sentinel fires with a non-empty target.
	js, err := testClient.Client.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	const n = 60
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c360.platform.robotics.mav1.drone.%03d", i)
		target := fmt.Sprintf("c360.platform.robotics.mav1.mission.%03d", i)
		data, mErr := json.Marshal(graph.EntityState{
			ID:      id,
			Triples: []message.Triple{{Subject: id, Predicate: "robotics.assigned.mission", Object: target}},
		})
		require.NoError(t, mErr)
		_, pErr := entityBucket.Put(ctx, id, data)
		require.NoError(t, pErr)
	}

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	graphIndexComp := comp.(*Component)
	require.NoError(t, graphIndexComp.Initialize())
	// Shorten the readiness heartbeat (pre-Start, the only safe window) so the
	// GRAPH_STATUS assertion below observes the caught-up transition promptly instead
	// of waiting out the 5s production cadence.
	graphIndexComp.statusInterval = 100 * time.Millisecond
	require.NoError(t, graphIndexComp.Start(ctx))
	defer graphIndexComp.Stop(5 * time.Second)

	// Readiness is monotonic (the sticky bootstrap flag never un-sets, and there are no
	// write failures here), so the race-free invariant is: an incoming query succeeds
	// ONLY when the index is ready. Query incoming FIRST; the moment it succeeds, status
	// queried immediately after MUST report ready. The pre-fix bug (sentinel sets the
	// sticky flag, bypassing the watermark) would let incoming succeed during replay while
	// status still reports building — caught here. (Ordering status-then-incoming would be
	// a TOCTOU flake: the index can catch up between the two requests.)
	inReq, _ := json.Marshal(map[string]string{"entity_id": "c360.platform.robotics.mav1.mission.001"})
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		inMsg, iErr := nc.Request("graph.index.query.incoming", inReq, 2*time.Second)
		require.NoError(t, iErr)
		if !incomingQuerySucceeded(inMsg.Data) {
			time.Sleep(5 * time.Millisecond)
			continue // still not ready — the expected pre-catch-up window
		}
		// Incoming succeeded ⇒ the index must be authoritatively ready. The projection
		// is read IN-PROCESS, not from GRAPH_STATUS: the published key advances on the
		// heartbeat tick, so a KV read here would lag the instant this invariant is
		// about and turn a race-free assertion into a flake. (It read the status
		// request/reply before ADR-083 removed the subject; that request answered from
		// this same projection, so the assertion is unchanged.) The published key is
		// asserted separately below, once it has had a tick to catch up.
		st := graphIndexComp.computeIndexStatus(ctx)
		assert.True(t, st.Ready,
			"incoming query succeeded but status is not ready — the query gate bypassed the watermark (Codex #1)")

		// Readiness stays externally observable after the subject removal: the same
		// caught-up envelope reaches consumers as GRAPH_STATUS KV state.
		requireStatusKeyEventually(ctx, t, testClient.Client,
			func(s graph.IndexStatusResponse) bool { return s.Ready },
			"published GRAPH_STATUS envelope never reported the caught-up index")
		return
	}
	t.Fatal("index never became ready within the deadline")
}

// readStatusKey reads graph-index's published readiness envelope the way an operator
// (`nats kv get GRAPH_STATUS graph-index`) and every migrated consumer now does. It
// replaces the `graph.index.query.status` request/reply these tests used before
// ADR-083 removed it. found is false while the bucket or key is absent — the producer
// has not published yet — which is distinct from a decode failure.
func readStatusKey(ctx context.Context, t *testing.T, nc *natsclient.Client) (status graph.IndexStatusResponse, found bool) {
	t.Helper()
	bucket, err := nc.GetKeyValueBucket(ctx, readiness.BucketGraphStatus)
	if err != nil {
		return graph.IndexStatusResponse{}, false
	}
	entry, err := bucket.Get(ctx, readiness.KeyGraphIndex)
	if err != nil {
		return graph.IndexStatusResponse{}, false
	}
	require.NoError(t, json.Unmarshal(entry.Value(), &status),
		"published status value must decode as graph.IndexStatusResponse: %s", entry.Value())
	return status, true
}

// requireStatusKeyEventually waits for the published envelope to satisfy pred. The
// wait is inherent: the producer publishes on its heartbeat, so a consumer observes a
// transition one tick after it happens. Callers that need a short wait shorten the
// heartbeat via the setupIntegrationTest preStart hook.
func requireStatusKeyEventually(
	ctx context.Context, t *testing.T, nc *natsclient.Client,
	pred func(graph.IndexStatusResponse) bool, msg string,
) graph.IndexStatusResponse {
	t.Helper()
	var last graph.IndexStatusResponse
	require.Eventually(t, func() bool {
		status, found := readStatusKey(ctx, t, nc)
		if !found {
			return false
		}
		last = status
		return pred(status)
	}, 15*time.Second, 50*time.Millisecond, msg)
	return last
}

// incomingQuerySucceeded reports whether a NATS reply body is a valid incoming-query
// success envelope (as opposed to a classified error reply such as index_not_ready,
// which does not decode into this envelope with a non-nil relationships slice).
func incomingQuerySucceeded(body []byte) bool {
	var resp graph.IncomingQueryResponse
	return json.Unmarshal(body, &resp) == nil && resp.Data.Relationships != nil
}

// TestQueryStatus_RevisionLag_Integration drives the full production wire — real
// WatchAll delivery, real entry.Revision(), real BucketLastSeq against the KV backing
// stream — to prove the ADR-066 caught-up contract end to end, and reads the result
// the way ADR-083 distributes it: from the GRAPH_STATUS KV key the component publishes
// on its heartbeat, not from the removed `graph.index.query.status` request/reply. The
// readout changed; every assertion about the envelope did not.
func TestQueryStatus_RevisionLag_Integration(t *testing.T) {
	// A short heartbeat so each readiness transition is observable in KV promptly.
	// The publish path itself is production — the same refreshReadinessMetrics tick
	// that feeds the Prometheus gauges.
	_, natsClient, cleanup := setupIntegrationTest(t, func(c *Component) {
		c.statusInterval = 100 * time.Millisecond
	})
	defer cleanup()

	ctx := context.Background()

	statusNow := func(t *testing.T) graph.IndexStatusResponse {
		t.Helper()
		status, found := readStatusKey(ctx, t, natsClient)
		require.True(t, found, "no readiness envelope published to %s/%s",
			readiness.BucketGraphStatus, readiness.KeyGraphIndex)
		return status
	}

	// Empty ENTITY_STATES: an authoritatively empty 0/0 graph is READY once initial
	// enumeration completes (gh#474 Codex #5) — it must not reject queries forever
	// just because target==0. The enumeration-complete sentinel is async, so wait.
	st := requireStatusKeyEventually(ctx, t, natsClient,
		func(s graph.IndexStatusResponse) bool { return s.Ready },
		"empty enumerated graph must become ready")
	assert.True(t, st.Ready, "empty enumerated graph reads ready (Codex #5)")
	assert.Equal(t, graph.IndexStateReady, st.State)
	assert.Zero(t, st.TargetRevision, "empty bucket target should be 0")

	// Write several entities — each Put advances ENTITY_STATES LastSeq (the target).
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.KeyValue(ctx, graph.BucketEntityStates)
	require.NoError(t, err)

	const n = 8
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c360.platform.robotics.mav1.drone.%03d", i)
		state := graph.EntityState{
			ID:      id,
			Triples: []message.Triple{{Subject: id, Predicate: "dc.terms.title", Object: fmt.Sprintf("Drone %d", i)}},
		}
		stateJSON, err := json.Marshal(state)
		require.NoError(t, err)
		_, err = entityBucket.Put(ctx, id, stateJSON)
		require.NoError(t, err)
	}

	// Poll until caught up: Ready flips only when IndexedRevision >= TargetRevision.
	// (statusNow runs in THIS goroutine — require.Eventually would run it in another,
	// where testify's FailNow is illegal.)
	//
	// The published envelope trails the writes by up to one heartbeat, so Ready alone
	// is not the stop condition here: the key still holds the pre-write ready envelope
	// (0/0) for a tick after the Puts land. Wait for a ready envelope that has ALSO
	// seen the writes. The old on-demand status request computed at read time and so
	// needed no such qualifier — this is the readout changing, not the contract.
	var final graph.IndexStatusResponse
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		final = statusNow(t)
		if final.Ready && final.IndexedRevision >= uint64(n) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.True(t, final.Ready, "index must catch up to the writes within timeout")
	assert.Equal(t, graph.IndexStateReady, final.State)
	assert.Zero(t, final.Lag, "caught up means zero lag")
	assert.GreaterOrEqual(t, final.IndexedRevision, uint64(n), "indexed revision reflects the writes")
	assert.Equal(t, final.TargetRevision, final.IndexedRevision, "indexed == target when caught up")
	assert.NotEmpty(t, final.Revision, "the string Revision field is now populated")
}
