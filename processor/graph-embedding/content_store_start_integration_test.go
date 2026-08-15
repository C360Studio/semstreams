//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_GraphEmbeddingStart_DoesNotAcquireOrMutateStoreReadBucket proves
// graph-embedding treats store-read as an admission contract, not permission to
// construct a second ObjectStore owner. The storage component owns acquisition,
// retention reconciliation, registration, and Close. Starting graph-embedding must
// therefore leave an otherwise reachable backing stream untouched.
//
// Isolated NATS client (not the shared one): the component keys its subjects on a
// fixed instance name, and a store-read bucket seeded with legacy retention must
// not race other tests.
func TestIntegration_GraphEmbeddingStart_DoesNotAcquireOrMutateStoreReadBucket(t *testing.T) {
	const bucket = "GE_START_LEGACY_CONTENT"
	ctx := context.Background()

	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKV(),
		natsclient.WithKVBuckets(graph.BucketEntityStates),
	)

	// Seed a pre-contract content backing stream: OBJ_<bucket> with the historical
	// 24h TTL that the D2 guard exists to strip.
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket:      bucket,
		Description: "legacy retention fixture (graph-embedding start)",
		TTL:         24 * time.Hour,
	})
	require.NoError(t, err)

	// Point the component's store-read port at that seeded bucket.
	cfg := DefaultConfig()
	for i := range cfg.Ports.Inputs {
		if store, ok := cfg.Ports.Inputs[i].Config.(component.StoreReadPort); ok {
			store.Bucket = bucket
			cfg.Ports.Inputs[i].Config = store
		}
	}
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	disc, err := CreateGraphEmbedding(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := disc.(*Component)
	require.NoError(t, c.Initialize())

	// Start must not acquire this store or apply the storage owner's retention policy.
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	// The backing stream's TTL remains exactly as the owner configured it.
	stream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, info.Config.MaxAge,
		"graph-embedding must not mutate a backing stream it does not own")
}
