//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestIntegration_OffloadedEntityEmbedsIdentityAlongsideBody drives the whole
// offloaded lane end to end on real NATS + a real ObjectStore (#601): two entities
// whose BODIES are byte-identical and offloaded to the store, each carrying a distinct
// inline `.signature` identity triple selected by a configured text suffix. Before the
// fix both would embed body-only — identical vectors, indistinguishable by any query —
// so a query naming one entity's signature could not single it out. After the fix each
// entity embeds its identity AHEAD of the shared body, so a query naming the signature
// retrieves that entity and ranks it above the sibling with the same body.
//
// This is the production wire: CreateGraphEmbedding → ENTITY_STATES watcher (hop 1,
// extractTextForEmbedding → SavePendingWithStorageRef) → worker (hop 2, getSourceText
// fetches the offloaded body and concatenates identity-first) → EMBEDDING_INDEX →
// findSimilarEntities search over the real BM25 vectors.
func TestIntegration_OffloadedEntityEmbedsIdentityAlongsideBody(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Real ObjectStore holding the offloaded bodies, registered under a resolvable
	// StorageInstance so the worker's federated resolver (ADR-063) fetches them.
	const storeInstance = "content"
	store, err := objectstore.NewStoreWithConfig(ctx, nc, objectstore.Config{
		BucketName:   "CONTENT",
		InstanceName: storeInstance,
	})
	require.NoError(t, err)

	// Both bodies are IDENTICAL and contain none of either entity's identity terms.
	// Body-only embedding (the pre-fix behavior) would therefore make the two vectors
	// indistinguishable; only the prepended identity can separate them.
	const sharedBody = "the device reports voltage and current at fixed intervals over the radio downlink"
	const key1 = "content/e1"
	const key2 = "content/e2"
	require.NoError(t, store.Put(ctx, key1, []byte(sharedBody)))
	require.NoError(t, store.Put(ctx, key2, []byte(sharedBody)))

	reg := storeregistry.New()
	require.NoError(t, reg.Register(storeInstance, store))

	// text_suffixes = [".signature"]: the identity predicate is a code signature, so the
	// test also proves the text-suffix config takes effect on the offloaded lane.
	config := DefaultConfig()
	config.TextSuffixes = []string{".signature"}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := CreateGraphEmbedding(configJSON, component.Dependencies{
		NATSClient:    nc,
		StoreRegistry: reg,
	})
	require.NoError(t, err)
	embeddingComp := comp.(*Component)
	require.NoError(t, embeddingComp.Initialize())

	js, err := nc.JetStream()
	require.NoError(t, err)
	entityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: graph.BucketEntityStates, Description: "Test entity states",
	})
	require.NoError(t, err)

	require.NoError(t, embeddingComp.Start(ctx))
	defer embeddingComp.Stop(context.Background())

	var embeddingBucket jetstream.KeyValue
	require.Eventually(t, func() bool {
		embeddingBucket, err = js.KeyValue(ctx, graph.BucketEmbeddingIndex)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "EMBEDDING_INDEX bucket should be created")

	now := time.Now().UTC()
	const e1 = "c360.semspec.golang.pkg.fn.peregrine"
	const e2 = "c360.semspec.golang.pkg.fn.widget"
	// e1's signature carries distinctive terms present nowhere in the shared body.
	writeOffloaded := func(id, key, signature string) {
		state := graph.EntityState{
			ID: id,
			Triples: []message.Triple{
				{Subject: id, Predicate: "code.symbol.signature", Object: signature, Source: "test", Timestamp: now},
			},
			StorageRef:  &message.StorageReference{StorageInstance: storeInstance, Key: key},
			MessageType: message.Type{Domain: "test", Category: "entity", Version: "v1"},
			Version:     1,
			UpdatedAt:   now,
		}
		data, mErr := json.Marshal(state)
		require.NoError(t, mErr)
		_, pErr := entityBucket.Put(ctx, id, data)
		require.NoError(t, pErr)
	}
	writeOffloaded(e1, key1, "Peregrine Falcon Telemetry Subsystem")
	writeOffloaded(e2, key2, "Unrelated Widget Housing Bracket")

	// Both entities must reach a generated embedding.
	waitGenerated := func(id string) embedding.Record {
		var rec embedding.Record
		require.Eventually(t, func() bool {
			entry, gErr := embeddingBucket.Get(ctx, id)
			if gErr != nil {
				return false
			}
			if json.Unmarshal(entry.Value(), &rec) != nil {
				return false
			}
			return rec.Status == embedding.StatusGenerated
		}, 15*time.Second, 200*time.Millisecond, "entity %s should have a generated embedding", id)
		return rec
	}
	waitGenerated(e1)
	waitGenerated(e2)

	// A query naming e1's signature terms must retrieve e1 and rank it above e2 — which
	// is impossible if only the shared body was embedded (both vectors would be equal).
	queryVecs, err := embeddingComp.embedder.GenerateQuery(ctx, []string{"Peregrine Falcon Telemetry"})
	require.NoError(t, err)
	require.Len(t, queryVecs, 1)

	results, err := embeddingComp.findSimilarEntities(ctx, "", queryVecs[0], nil, 10)
	require.NoError(t, err)
	require.NotEmpty(t, results, "the identity-naming query must retrieve at least one entity")

	require.Equal(t, e1, results[0].EntityID,
		"a query naming e1's offloaded identity must rank e1 first; body-only embedding could not distinguish it from e2")

	sim := map[string]float64{}
	for _, r := range results {
		sim[r.EntityID] = r.Similarity
	}
	require.Greater(t, sim[e1], sim[e2],
		"e1 (whose embedded identity contains the query terms) must out-score e2 (identical body, different identity)")
	require.Greater(t, sim[e1], 0.0, "the identity terms must be present in e1's vector, giving a positive score")
}
