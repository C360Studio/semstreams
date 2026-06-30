//go:build integration

package objectstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
