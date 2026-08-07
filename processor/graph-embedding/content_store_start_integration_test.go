//go:build integration

package graphembedding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_GraphEmbeddingStart_ContentStoreRetentionGraceful drives the
// FINDING-1 graph-embedding wire end-to-end against real NATS: Start →
// initStorageAndWorker → createContentStore → the shared store constructor's D2
// retention guard (#600/#616). A store-read port pointed at a backing stream that
// carries a legacy 24h TTL must be STRIPPED in place and Start must boot cleanly —
// the guard's graceful (strippable) path, which the new fatal branch must not
// disturb. This proves the component actually runs the guard on its content store
// and that graceful degradation is intact.
//
// Isolated NATS client (not the shared one): the component keys its subjects on a
// fixed instance name, and a store-read bucket seeded with legacy retention must
// not race other tests.
func TestIntegration_GraphEmbeddingStart_ContentStoreRetentionGraceful(t *testing.T) {
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

	// Start must NOT fail closed here: the legacy TTL is strippable, so the guard
	// reconciles it and the component boots with the content store wired.
	require.NoError(t, c.Start(ctx),
		"a strippable legacy retention must reconcile, not fail Start")
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	require.NotNil(t, c.contentStore, "the content store must be wired through Start")

	// The backing stream's TTL must have been stripped in place by the guard.
	stream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), info.Config.MaxAge,
		"Start's content-store guard must have stripped the legacy TTL in place")
}
