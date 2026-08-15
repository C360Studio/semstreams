//go:build integration

package graphembedding

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
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestIntegration_CoalescedLane_TombstoneConverges (T8) drives the #629 shape
// against real NATS through the full production wire: coalescing ENABLED
// (coalesce_ms=50, the lane semsource defaults on and no shipped semstreams
// config exercised before this change), an update burst inside the debounce
// window, then a tombstone racing whatever flush the window produces.
//
// Whatever the interleaving — flush before the tombstone, after it, or
// straddling it — the derived state MUST converge on authoritative absence:
// the EMBEDDING_INDEX key ends absent, nothing is stranded in the failed
// accounting, and readiness reaches ready (which also proves the watermark
// drained through the tombstone, #624). Assertions are on STATE PREDICATES
// only via require.Eventually — no timing bounds, because the coalescer's
// flush schedule is deliberately not part of the contract.
func TestIntegration_CoalescedLane_TombstoneConverges(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	config := DefaultConfig()
	config.CoalesceMs = 50
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	embeddingComp := comp.(*Component)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, embeddingComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(context.Background())

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	const entityID = "c360.platform.robotics.mav1.drone.001"
	now := time.Now().UTC()

	// Update burst: several revisions land inside one coalescing window so the
	// flush's fresh authoritative Get is the lane that runs.
	for i := 0; i < 3; i++ {
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{
				{
					Subject:   entityID,
					Predicate: "dc.terms.title",
					Object:    fmt.Sprintf("Autonomous Reconnaissance Drone rev %d", i),
					Source:    "test",
					Timestamp: now,
				},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     uint64(i + 1),
			UpdatedAt:   now,
		}
		stateData, merr := json.Marshal(state)
		require.NoError(t, merr)
		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	// The tombstone lands while the burst's window/flush is in flight.
	require.NoError(t, entityBucket.Delete(ctx, entityID))

	require.Eventually(t, func() bool {
		if _, gerr := embeddingBucket.Get(ctx, entityID); !errors.Is(gerr, jetstream.ErrKeyNotFound) {
			return false // record still present (or transient read error): not converged
		}
		count, _, _ := embeddingComp.failedSnapshot()
		if count != 0 {
			return false // something stranded: repair has not converged it yet
		}
		// Ready proves the watermark drained through the burst AND the tombstone
		// (#624) over the full production compute (BucketLastSeq target included).
		return embeddingComp.computeEmbeddingStatus(ctx).State == graph.IndexStateReady
	}, 20*time.Second, 100*time.Millisecond,
		"derived embedding state must converge to authoritative absence with nothing stranded and readiness ready")
}

// TestIntegration_PreloadedBootstrap_TakesCoalescedLane (Codex #722 HIGH 3):
// Start used to assign c.entityCoalescer AFTER launching the entity watcher —
// a data race on the pointer (the watcher goroutine reads it unsynchronized)
// AND a bootstrap bypass: with a PRELOADED ENTITY_STATES bucket, the initial
// replay arrives while the field is still nil, so bootstrap entries take the
// immediate lane despite coalesce_ms > 0.
//
// The T8 test above starts against an EMPTY bucket and cannot see either
// defect (its first deliveries happen long after Start returns), so this test
// seeds entities BEFORE Start. The coalesce window is deliberately enormous
// (60s — no flush inside the test) to make the lane deterministic: under the
// fix every bootstrap entry sits in the coalescer's pending set and NOTHING is
// written to EMBEDDING_INDEX; on the pre-fix ordering, entries delivered before
// the assignment take the immediate lane and pending records appear, and the
// unsynchronized pointer read is a -race-detectable data race.
//
// HONESTY NOTE: this test is a TRIPWIRE, not a fails-first proof — observed to
// PASS on the pre-fix ordering, because the watcher's WatchAll network
// round-trip in practice orders the (microseconds-later) coalescer assignment
// first, and the sub-millisecond window did not surface under -race in this
// harness. The defect is the ordering itself (a Go data race is undefined
// behavior regardless of who usually wins); the authoritative evidence for the
// fix is the construct-before-launch ordering in Start, which this test pins
// against regression.
func TestIntegration_PreloadedBootstrap_TakesCoalescedLane(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Test entity states",
	})
	require.NoError(t, err)

	// Preload the bucket BEFORE Start — the bootstrap replay is the racing lane.
	now := time.Now().UTC()
	const seeded = 10
	entityIDs := make([]string, 0, seeded)
	for i := 0; i < seeded; i++ {
		entityID := fmt.Sprintf("c360.platform.robotics.mav1.drone.%03d", i)
		entityIDs = append(entityIDs, entityID)
		state := graph.EntityState{
			ID: entityID,
			Triples: []message.Triple{{
				Subject:   entityID,
				Predicate: "dc.terms.title",
				Object:    fmt.Sprintf("Preloaded drone %d", i),
				Source:    "test",
				Timestamp: now,
			}},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		}
		stateData, merr := json.Marshal(state)
		require.NoError(t, merr)
		_, err = entityBucket.Put(ctx, entityID, stateData)
		require.NoError(t, err)
	}

	config := DefaultConfig()
	config.CoalesceMs = 60_000 // no flush during the test: the pending set IS the lane evidence
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	embeddingComp := comp.(*Component)
	require.NoError(t, embeddingComp.Initialize())
	require.NoError(t, embeddingComp.Start(ctx))
	// Stop must not hang on the 60s window: it cancels the component ctx before
	// entityCoalescer.Close(), which unblocks the coalescer's run goroutine.
	defer embeddingComp.Stop(context.Background())

	// Every bootstrap entry flows through the coalesced lane: the pending set
	// fills to the seeded count and stays there (no flush for 60s).
	require.Eventually(t, func() bool {
		return embeddingComp.entityCoalescer != nil && embeddingComp.entityCoalescer.PendingCount() == seeded
	}, 10*time.Second, 50*time.Millisecond,
		"bootstrap entries must be routed through the coalescer, not the immediate lane")

	// And the immediate lane wrote NOTHING: no derived record exists while the
	// window is open (pre-fix, immediate-lane SavePending records appear here).
	embeddingBucket, err := js.KeyValue(ctx, graph.BucketEmbeddingIndex)
	require.NoError(t, err)
	for _, entityID := range entityIDs {
		_, gerr := embeddingBucket.Get(ctx, entityID)
		require.ErrorIs(t, gerr, jetstream.ErrKeyNotFound,
			"entity %s must not have an immediate-lane derived record while the coalesce window is open", entityID)
	}
}
