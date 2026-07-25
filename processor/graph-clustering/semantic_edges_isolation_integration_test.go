//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SemanticEdges_ScopedToDetectionNotStructural proves the B2 §2
// isolation Codex flagged: enabling the semantic-edge tier must change COMMUNITY
// membership (the detector votes over mutual-kNN edges) but MUST NOT leak into
// the STRUCTURAL_INDEX (k-core / pivot), which the spec scopes to explicit +
// EntityID edges only. It seeds two entities that are ONLY connected by a
// mutual-kNN semantic edge — no explicit edge, different type prefix, different
// system — so:
//
//   - with the tier enabled they co-locate in one community (semantic edge is
//     live in detection), yet
//   - their k-core numbers stay 0 (structurally isolated), which they could not
//     be if the k-core computer read the semantic decorator instead of the base
//     EntityID chain.
//
// Regression guard: before the fix, initProviderAndDetector set
// c.graphProvider = topProvider (the semantic decorator), so runStructuralComputation
// fed the mutual-kNN edge into the k-core degrees and both entities would come
// back at core 1.
func TestIntegration_SemanticEdges_ScopedToDetectionNotStructural(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Two entities that are structurally heterogeneous (different system AND
	// different type prefix) so NO sibling/system-peer virtual edge qualifies —
	// their only possible connection is the mutual-kNN semantic edge.
	const (
		entA = "c360.plat.docs.alpha.report.a1"
		entB = "c360.plat.telemetry.beta.sensor.b1"
	)

	// Fake graph-embedding similarity responder: A and B are each other's sole
	// top-k neighbor above threshold -> a mutual-kNN pair. Every other entity
	// (there are none here) gets an empty result.
	respond := func(id string) ([]similarEntity, bool) {
		switch id {
		case entA:
			return []similarEntity{{EntityID: entB, Similarity: 0.95}}, true
		case entB:
			return []similarEntity{{EntityID: entA, Similarity: 0.95}}, true
		default:
			return nil, true
		}
	}
	sub, err := nc.SubscribeForRequests(ctx, similarQuerySubject,
		func(_ context.Context, data []byte) ([]byte, error) {
			var req similarRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, err
			}
			sims, _ := respond(req.EntityID)
			return json.Marshal(similarResponse{EntityID: req.EntityID, Similar: sims, Duration: "0s"})
		})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	config := Config{
		AllowUngatedReads: true, // standalone test reads without a co-deployed graph-index handler
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "entity_watch", Type: "kv-watch", Subject: graph.BucketEntityStates},
			},
			Outputs: []component.PortDefinition{
				{Name: "communities", Type: "kv-write", Subject: graph.BucketCommunityIndex},
			},
		},
		DetectionIntervalStr: "1s",
		MinCommunitySize:     2,
		MaxIterations:        10,
		EnableLLM:            false,
		EnableStructural:     true, // structural must run so we can inspect k-core
		PivotCount:           2,
		MaxHopDistance:       5,
		// The tier under test: enabled.
		SemanticEdges: &SemanticEdgesConfig{EnableSemanticEdges: true},
	}
	config.ApplyDefaults()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphClustering(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	clusteringComp := comp.(*Component)
	require.NoError(t, clusteringComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)

	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketEntityStates})
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketOutgoingIndex})
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: graph.BucketIncomingIndex})
	require.NoError(t, err)

	require.NoError(t, clusteringComp.Start(ctx))
	defer clusteringComp.Stop(5 * time.Second)

	// Seed the two entities with NO explicit edges between them.
	now := time.Now().UTC()
	for _, id := range []string{entA, entB} {
		state := graph.EntityState{
			ID: id,
			Triples: []message.Triple{
				{Subject: id, Predicate: "entity.type.class", Object: "thing", Source: "test", Timestamp: now},
			},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		}
		data, err := json.Marshal(state)
		require.NoError(t, err)
		_, err = entityBucket.Put(ctx, id, data)
		require.NoError(t, err)
	}

	// 1) Detection co-locates A and B via the semantic edge (proves the tier is
	//    live — this assertion is vacuous if the edge never fired).
	communityBucket, err := js.KeyValue(ctx, graph.BucketCommunityIndex)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		ea, erra := communityBucket.Get(ctx, "entity.0."+entA)
		eb, errb := communityBucket.Get(ctx, "entity.0."+entB)
		if erra != nil || errb != nil {
			return false
		}
		return len(ea.Value()) > 0 && string(ea.Value()) == string(eb.Value())
	}, 20*time.Second, 250*time.Millisecond,
		"with semantic edges enabled, the mutual-kNN pair must land in the SAME community")

	// 2) The STRUCTURAL_INDEX must NOT reflect the semantic edge: with only the
	//    base explicit/EntityID chain feeding k-core, both entities are isolated
	//    (degree 0 -> core 0). A leak would give them core 1.
	structuralBucket, err := js.KeyValue(ctx, graph.BucketStructuralIndex)
	require.NoError(t, err)
	structStorage := structural.NewNATSStructuralIndexStorage(structuralBucket)

	require.Eventually(t, func() bool {
		idx, err := structStorage.GetKCoreIndex(ctx)
		if err != nil || idx == nil {
			return false
		}
		_, okA := idx.CoreNumbers[entA]
		_, okB := idx.CoreNumbers[entB]
		return okA && okB
	}, 20*time.Second, 250*time.Millisecond, "k-core index should include both entities")

	idx, err := structStorage.GetKCoreIndex(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, idx.GetCore(entA),
		"semantic edge must NOT reach k-core: A is structurally isolated (leak would make it core 1)")
	assert.Equal(t, 0, idx.GetCore(entB),
		"semantic edge must NOT reach k-core: B is structurally isolated (leak would make it core 1)")
	assert.Equal(t, 0, idx.MaxCore,
		"a structurally edgeless graph has max core 0; a non-zero max core means the semantic edge leaked")
}
