//go:build integration

package objectstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_GH400_ComponentThreadsInstanceNameIntoStore is a white-box
// guard for the component-threading line (component.go: c.config.InstanceName =
// c.instanceName). It boots one real component and asserts the store it built
// stamps the COMPONENT instance name, not the bucket name. The store-level tests
// in store_integration_test.go lock StoreContent's stamping given a configured
// InstanceName; this locks that the component actually supplies it — so deleting
// or reordering the threading line (which would silently re-introduce gh#400 on
// the StoreContent path) fails a test instead of shipping green.
//
// Internal (package objectstore) so it can read the unexported c.store. Uses its
// own isolated NATS container — NOT the shared one — because the component's
// derived subjects all key on the fixed instance name "objectstore" and a second
// component on a shared bus would collide (the reason a production-wire variant
// of this test was avoided in favor of this direct assertion).
func TestIntegration_GH400_ComponentThreadsInstanceNameIntoStore(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())

	cfg := DefaultConfig()
	cfg.BucketName = "GH400_THREAD_BUCKET" // deliberately != the instance name
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	disc, err := NewComponent(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := disc.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	require.NotNil(t, c.store, "component must have built its store on Start")
	require.Equal(t, c.instanceName, c.store.InstanceName(),
		"component must thread its instance name into the store (gh#400)")
	require.NotEqual(t, cfg.BucketName, c.store.InstanceName(),
		"store must NOT stamp the bucket name on the component path (the gh#400 bug)")
}

// TestIntegration_ComponentStart_LegacyRetentionGracefulBoot drives the FINDING-3
// graceful path through the REAL Component.Start against real NATS: a backing stream
// carrying a legacy 24h TTL is stripped in place by the D2 guard the store
// constructor runs, and Start boots cleanly (no fail-closed) with the store wired.
// The fatal branch added at component.go's Start error handling must not disturb this
// strippable path. The FATAL branch itself is driven through the production decision
// seam (TestStartStoreError_PreservesClass, fed the real guard's fatal via a denied
// fake JS) because a true un-strippable retention (denied UpdateStream) is not
// deterministically reachable against cooperative real NATS.
func TestIntegration_ComponentStart_LegacyRetentionGracefulBoot(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV())
	const bucket = "COMP_START_LEGACY_RETENTION"

	// Seed a pre-contract backing stream: OBJ_<bucket> with the historical 24h TTL.
	js, err := tc.Client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket:      bucket,
		Description: "legacy retention fixture (component start)",
		TTL:         24 * time.Hour,
	})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.BucketName = bucket
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	disc, err := NewComponent(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := disc.(*Component)
	require.NoError(t, c.Initialize())

	// Start must reconcile-and-boot, not fail closed, for a strippable retention.
	require.NoError(t, c.Start(ctx),
		"a strippable legacy retention must reconcile through Start, not fail closed")
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	require.NotNil(t, c.store, "the store must be built through Start")

	stream, err := js.Stream(ctx, "OBJ_"+bucket)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), info.Config.MaxAge,
		"Start's store constructor guard must have stripped the legacy TTL in place")
}
