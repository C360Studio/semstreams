//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_ConcurrentAddTriple verifies that concurrent AddTriple operations
// don't lose updates due to race conditions. This is the critical test for CAS safety.
func TestIntegration_ConcurrentAddTriple(t *testing.T) {
	ctx := context.Background()

	// Create NATS test client with required streams
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	// Create component
	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	component := comp.(*Component)
	require.NoError(t, component.Initialize())
	require.NoError(t, component.Start(ctx))
	defer func() {
		_ = component.Stop(5 * time.Second)
	}()

	// Wait for component to be ready
	time.Sleep(100 * time.Millisecond)

	t.Run("concurrent_triple_additions_no_lost_updates", func(t *testing.T) {
		entityID := "c360.test.cas.concurrent.drone.001"

		// Create initial entity with one triple
		initialEntity := &graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:    entityID,
					Predicate:  "core.identity.type",
					Object:     "drone",
					Timestamp:  time.Now(),
					Confidence: 1.0,
				},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}
		require.NoError(t, component.CreateEntity(ctx, initialEntity))

		// Launch concurrent AddTriple operations
		concurrency := 20
		var wg sync.WaitGroup
		var successCount atomic.Int32
		var errorCount atomic.Int32

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				triple := message.Triple{
					Subject:    entityID,
					Predicate:  "test.concurrent.value",
					Object:     idx, // Each goroutine adds a unique value
					Timestamp:  time.Now(),
					Confidence: 1.0,
					Source:     "cas-test",
				}

				err := component.AddTriple(ctx, triple)
				if err != nil {
					errorCount.Add(1)
					t.Logf("AddTriple error (idx=%d): %v", idx, err)
				} else {
					successCount.Add(1)
				}
			}(i)
		}

		wg.Wait()

		// All operations should succeed (CAS retries handle conflicts)
		assert.Equal(t, int32(concurrency), successCount.Load(), "all AddTriple operations should succeed")
		assert.Equal(t, int32(0), errorCount.Load(), "no AddTriple operations should fail")

		// Verify entity has all triples (1 initial + concurrency new ones)
		entry, err := component.entityBucket.Get(ctx, entityID)
		require.NoError(t, err)

		var finalEntity graph.EntityState
		require.NoError(t, json.Unmarshal(entry.Value, &finalEntity))

		expectedTripleCount := 1 + concurrency
		assert.Equal(t, expectedTripleCount, nonProfileTripleCount(&finalEntity),
			"entity should have all triples (no lost updates); ADR-054 profile stamp excluded")

		// Verify version was incremented correctly
		assert.GreaterOrEqual(t, finalEntity.Version, uint64(concurrency+1),
			"version should be incremented for each AddTriple")
	})

	t.Run("concurrent_triple_removal_no_lost_updates", func(t *testing.T) {
		entityID := "c360.test.cas.removal.drone.002"

		// Create entity with many triples
		numTriples := 20
		triples := make([]message.Triple, numTriples)
		for i := 0; i < numTriples; i++ {
			triples[i] = message.Triple{
				Subject:    entityID,
				Predicate:  "test.removable.triple",
				Object:     i,
				Timestamp:  time.Now(),
				Confidence: 1.0,
			}
		}

		initialEntity := &graph.EntityState{
			ID:        entityID,
			Triples:   triples,
			Version:   1,
			UpdatedAt: time.Now(),
		}
		require.NoError(t, component.CreateEntity(ctx, initialEntity))

		// Launch concurrent RemoveTriple operations (all removing same predicate)
		concurrency := 10
		var wg sync.WaitGroup
		var successCount atomic.Int32

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := component.RemoveTriple(ctx, entityID, "test.removable.triple")
				if err == nil {
					successCount.Add(1)
				}
			}()
		}

		wg.Wait()

		// All operations should succeed (some may be no-ops after first removal)
		assert.Equal(t, int32(concurrency), successCount.Load(),
			"all RemoveTriple operations should succeed")

		// Verify entity has no more "test.removable.triple" triples
		entry, err := component.entityBucket.Get(ctx, entityID)
		require.NoError(t, err)

		var finalEntity graph.EntityState
		require.NoError(t, json.Unmarshal(entry.Value, &finalEntity))

		// Count remaining removable triples
		var removableCount int
		for _, t := range finalEntity.Triples {
			if t.Predicate == "test.removable.triple" {
				removableCount++
			}
		}
		assert.Equal(t, 0, removableCount, "all removable triples should be removed")
	})

	t.Run("mixed_concurrent_operations", func(t *testing.T) {
		entityID := "c360.test.cas.mixed.drone.003"

		// Create initial entity
		initialEntity := &graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:    entityID,
					Predicate:  "core.identity.type",
					Object:     "drone",
					Timestamp:  time.Now(),
					Confidence: 1.0,
				},
			},
			Version:   1,
			UpdatedAt: time.Now(),
		}
		require.NoError(t, component.CreateEntity(ctx, initialEntity))

		// Launch mixed concurrent operations
		concurrency := 10
		var wg sync.WaitGroup

		// Half add, half remove different predicates
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			if i%2 == 0 {
				go func(idx int) {
					defer wg.Done()
					triple := message.Triple{
						Subject:    entityID,
						Predicate:  "test.add.value",
						Object:     idx,
						Timestamp:  time.Now(),
						Confidence: 1.0,
					}
					_ = component.AddTriple(ctx, triple)
				}(i)
			} else {
				go func() {
					defer wg.Done()
					// Remove a predicate that may or may not exist
					_ = component.RemoveTriple(ctx, entityID, "test.nonexistent.predicate")
				}()
			}
		}

		wg.Wait()

		// Verify entity is in a consistent state
		entry, err := component.entityBucket.Get(ctx, entityID)
		require.NoError(t, err)

		var finalEntity graph.EntityState
		require.NoError(t, json.Unmarshal(entry.Value, &finalEntity))

		// Should have initial triple + added triples (concurrency/2)
		expectedMin := 1 + concurrency/2
		assert.GreaterOrEqual(t, len(finalEntity.Triples), expectedMin,
			"entity should have at least initial + added triples")
	})
}

// TestIntegration_CASRetryBehavior tests that CAS retries work correctly
// by verifying metrics/logging indicate retries occurred under contention.
func TestIntegration_CASRetryBehavior(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	config := DefaultConfig()
	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphIngest(configJSON, deps)
	require.NoError(t, err)

	component := comp.(*Component)
	require.NoError(t, component.Initialize())
	require.NoError(t, component.Start(ctx))
	defer func() {
		_ = component.Stop(5 * time.Second)
	}()

	time.Sleep(100 * time.Millisecond)

	t.Run("high_contention_completes_successfully", func(t *testing.T) {
		entityID := "c360.test.cas.highcontention.drone.001"

		// Create initial entity
		initialEntity := &graph.EntityState{
			ID:        entityID,
			Triples:   []message.Triple{},
			Version:   1,
			UpdatedAt: time.Now(),
		}
		require.NoError(t, component.CreateEntity(ctx, initialEntity))

		// High contention: many goroutines hitting the same entity.
		//
		// Concurrency 25 is the sweet spot: high enough to exercise the
		// CAS-retry path multiple times per goroutine (each retry observes
		// a different revision committed by a peer), low enough to fit
		// inside the default natsclient retry budget (MaxRetries=10 with
		// exponential backoff) even when Docker is under contention from
		// a full `go test -race -tags=integration ./...` sweep. Beta.77
		// + #107 evidence: 50 occasionally flaked under sweep because the
		// last few goroutines exhausted retries before winning the CAS race.
		// See [[project_issue_107_flake_hardening]] for the analysis.
		concurrency := 25
		var wg sync.WaitGroup
		var successCount atomic.Int32

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				triple := message.Triple{
					Subject:    entityID,
					Predicate:  "test.highcontention.value",
					Object:     idx,
					Timestamp:  time.Now(),
					Confidence: 1.0,
				}
				if err := component.AddTriple(ctx, triple); err == nil {
					successCount.Add(1)
				}
			}(i)
		}

		wg.Wait()

		// All should succeed eventually due to retries
		assert.Equal(t, int32(concurrency), successCount.Load(),
			"all operations should succeed under high contention")

		// Verify final state
		entry, err := component.entityBucket.Get(ctx, entityID)
		require.NoError(t, err)

		var finalEntity graph.EntityState
		require.NoError(t, json.Unmarshal(entry.Value, &finalEntity))

		assert.Equal(t, concurrency, nonProfileTripleCount(&finalEntity),
			"all triples should be present; ADR-054 profile stamp excluded")
	})
}

// TestIntegration_SharedSeam_CASFailureDoesNotRouteForeign locks the ADR-056
// Decision-4 invariant that routeForeignEdges runs ONLY after the primary commit
// succeeds — on the CAS lane too. A stale ExpectedRevision fails the primary
// update_with_triples; the foreign edge it carried must NOT be routed onto the
// child subject (the bug: routing on err==nil instead of decoded Success).
func TestIntegration_SharedSeam_CASFailureDoesNotRouteForeign(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()
	time.Sleep(100 * time.Millisecond)

	const parentID = "c360.test.seam.cas.system.001"
	const childID = "c360.test.seam.cas.child.001"

	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: parentID, Version: 1, UpdatedAt: time.Now(),
		Triples: []message.Triple{{Subject: parentID, Predicate: "system.label", Object: "P", Timestamp: time.Now(), Confidence: 1}},
	}))

	foreignEdge := message.Triple{Subject: childID, Predicate: "child.isHostedBy", Object: parentID, Timestamp: time.Now(), Confidence: 1}

	t.Run("stale_revision_does_not_route_foreign", func(t *testing.T) {
		req := graph.UpdateEntityWithTriplesRequest{
			Entity: &graph.EntityState{ID: parentID},
			AddTriples: []message.Triple{
				{Subject: parentID, Predicate: "system.note", Object: "n", Timestamp: time.Now(), Confidence: 1}, // own
				foreignEdge, // foreign
			},
			ExpectedRevision: 9999, // stale → CAS fails
		}
		data, _ := json.Marshal(req)
		respData, err := c.handleEntityUpdateWithTriples(ctx, data)
		// ADR-060: a stale CAS is the revision_mismatch sentinel reject (no body).
		requireClassifiedReject(t, respData, err, graph.ErrorCodeRevisionMismatch, "revision mismatch")

		// The failed primary write must NOT have routed the foreign edge — the
		// child was never created.
		_, getErr := c.entityBucket.Get(ctx, childID)
		assert.ErrorIs(t, getErr, natsclient.ErrKVKeyNotFound,
			"foreign edge must NOT be routed when the CAS primary write failed")
	})

	t.Run("correct_revision_routes_foreign", func(t *testing.T) {
		_, rev, err := c.fetchEntityState(ctx, parentID)
		require.NoError(t, err)

		// Pre-create the child entity so the foreign edge can land (ADR-055
		// must-exist: AddTriples rejects absent targets).
		require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{ID: childID}))

		req := graph.UpdateEntityWithTriplesRequest{
			Entity:           &graph.EntityState{ID: parentID},
			AddTriples:       []message.Triple{foreignEdge},
			ExpectedRevision: rev, // current → CAS succeeds
		}
		data, _ := json.Marshal(req)
		respData, err := c.handleEntityUpdateWithTriples(ctx, data)
		require.NoError(t, err)
		var resp graph.UpdateEntityWithTriplesResponse
		require.NoError(t, json.Unmarshal(respData, &resp))
		require.True(t, resp.Success, "current-revision CAS must succeed: %s", resp.Error)

		// On success the foreign edge IS routed onto the (pre-created) child.
		entry, getErr := c.entityBucket.Get(ctx, childID)
		require.NoError(t, getErr, "foreign edge must be routed onto the child when the CAS primary write committed")
		var child graph.EntityState
		require.NoError(t, json.Unmarshal(entry.Value, &child))
		assert.True(t, hasPredicate(&child, "child.isHostedBy"))
	})
}
