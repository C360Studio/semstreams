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
	defer embeddingComp.Stop(5 * time.Second)

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
