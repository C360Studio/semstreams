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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIntegrationTest creates a real NATS container and component using natsclient.TestClient
// Each test gets its own NATS container, so bucket isolation is automatic.
func setupIntegrationTest(t *testing.T) (*Component, *natsclient.Client, func()) {
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
	_, natsClient, cleanup := setupIntegrationTest(t)
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
				Predicate: predicate,
				Object:    targetID,
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
	request := map[string]string{"entity_id": entityID}
	requestJSON, err := json.Marshal(request)
	require.NoError(t, err)

	// Send query request
	msg, err := nc.Request("graph.index.query.outgoing", requestJSON, 2*time.Second)
	require.NoError(t, err)

	// Parse response (envelope: {"data": {"relationships": [...]}, ...})
	var response graph.OutgoingQueryResponse
	err = json.Unmarshal(msg.Data, &response)
	require.NoError(t, err)

	// Verify response
	assert.Len(t, response.Data.Relationships, 1, "should have one outgoing relationship")
	if len(response.Data.Relationships) > 0 {
		assert.Equal(t, targetID, response.Data.Relationships[0].ToEntityID)
		assert.Equal(t, predicate, response.Data.Relationships[0].Predicate)
	}
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
				Predicate: predicate,
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
	predicate := "robotics.type.drone"
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
					Predicate: predicate,
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
	request := map[string]string{"predicate": predicate}
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
	require.NoError(t, comp.UpdateOutgoingIndex(ctx, entityID, "target", "predicate"))

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
		require.NoError(t, comp.UpdateOutgoingIndex(ctx, entityID, "target", "predicate"))
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
		request := map[string]string{"entity_id": "non.existent.entity"}
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
		request := map[string]string{"entity_id": "non.existent.entity"}
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
			request: []byte(`{"predicate": ""}`),
		},
		{
			name:    "predicateStats empty predicate",
			subject: "graph.index.query.predicateStats",
			request: []byte(`{"predicate": ""}`),
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
func TestQueryStatus_RevisionLag_Integration(t *testing.T) {
	_, natsClient, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	nc := natsClient.GetConnection()

	statusNow := func(t *testing.T) graph.IndexStatusResponse {
		t.Helper()
		msg, err := nc.Request("graph.index.query.status", []byte(`{}`), 2*time.Second)
		require.NoError(t, err)
		var st graph.IndexStatusResponse
		require.NoError(t, json.Unmarshal(msg.Data, &st))
		return st
	}

	// Empty ENTITY_STATES: LastSeq==0 → not ready, building. The old sticky signal
	// would also say building here, but for the wrong reason (NAME_INDEX empty);
	// this asserts the honest target-based path.
	st := statusNow(t)
	assert.False(t, st.Ready, "empty bucket must not read ready")
	assert.Equal(t, graph.IndexStateBuilding, st.State)
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
	var final graph.IndexStatusResponse
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		final = statusNow(t)
		if final.Ready {
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
