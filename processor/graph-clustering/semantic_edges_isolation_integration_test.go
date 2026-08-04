//go:build integration

package graphclustering

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publishHealthyReadiness drives the PRODUCTION ADR-083 readiness contract in the
// fixture: it creates GRAPH_STATUS and heartbeats a healthy envelope on the given key
// (KeyGraphIndex / KeyGraphEmbedding) exactly as graph-index/graph-embedding do
// (readiness.NewPublisher(bucket, key) -> Publish per tick). The semantic-edge gate
// (B2 §4) has NO ungated escape — evaluateSemanticReadiness returns false on an
// unknown embedding feed — so the tier only ACTIVATES when graph-embedding's envelope
// is genuinely READY here; without this the isolation test's semantic edge never fires.
// It republishes on a fast tick so the envelope stays inside the freshness window for
// the whole Eventually deadline, then stops on ctx cancel.
func publishHealthyReadiness(ctx context.Context, t *testing.T, nc *natsclient.Client, key string) {
	t.Helper()
	bucket, err := readiness.EnsureBucket(ctx, nc)
	require.NoError(t, err)
	pub := readiness.NewPublisher(bucket, key)
	require.NotNil(t, pub)
	// State=ready + BootstrapComplete=true is the shape graph.EvaluateReadinessGate
	// admits (the same healthyStatus() the §4 unit tests use).
	healthy := graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady, BootstrapComplete: true}
	require.NoError(t, pub.Publish(ctx, healthy)) // first envelope before the watcher binds
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = pub.Publish(ctx, healthy)
			}
		}
	}()
}

// TestIntegration_SemanticEdges_ScopedToDetectionNotStructural proves the B2 §2
// isolation Codex flagged: enabling the semantic-edge tier must change COMMUNITY
// membership (the detector votes over mutual-kNN edges) but MUST NOT leak into
// the fresh in-memory k-core/pivot inputs, which the spec scopes to explicit +
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
		DetectionIntervalStr:   "1s",
		MinCommunitySize:       2,
		MaxIterations:          10,
		EnableLLM:              false,
		EnableAnomalyDetection: true, // owns the ephemeral structural prerequisite
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

	// Drive BOTH readiness axes to READY before the component's watchers bind. The
	// base index gate would also clear via AllowUngatedReads, but publishing a healthy
	// graph-index envelope exercises the real gate rather than the standalone escape;
	// the graph-embedding envelope is REQUIRED — the semantic gate has no ungated path,
	// so the tier stays INACTIVE (structural-only) without it and the same-community
	// assertion below times out. This is the readiness envelope the §4 gate needs.
	publishHealthyReadiness(ctx, t, nc, readiness.KeyGraphIndex)
	publishHealthyReadiness(ctx, t, nc, readiness.KeyGraphEmbedding)

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

	// 2) Fresh anomaly prerequisites must NOT reflect the semantic edge: with
	//    only the base explicit/EntityID provider feeding k-core, both entities
	//    are isolated (degree 0 -> core 0). A leak would give them core 1.
	idx, pivot, err := clusteringComp.runStructuralComputation(ctx)
	require.NoError(t, err)
	require.NotNil(t, idx)
	require.NotNil(t, pivot)
	require.Contains(t, idx.CoreNumbers, entA)
	require.Contains(t, idx.CoreNumbers, entB)
	assert.Equal(t, 0, idx.GetCore(entA),
		"semantic edge must NOT reach k-core: A is structurally isolated (leak would make it core 1)")
	assert.Equal(t, 0, idx.GetCore(entB),
		"semantic edge must NOT reach k-core: B is structurally isolated (leak would make it core 1)")
	assert.Equal(t, 0, idx.MaxCore,
		"a structurally edgeless graph has max core 0; a non-zero max core means the semantic edge leaked")
	_, err = js.KeyValue(ctx, "STRUCTURAL_INDEX")
	assert.Error(t, err, "in-process structural analysis must not create retired persistence")
}
