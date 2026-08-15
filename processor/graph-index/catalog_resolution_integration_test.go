//go:build integration

package graphindex

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

func TestIntegration_Start_DoesNotCreateRetiredContextIndex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	nc := testClient.Client
	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Authoritative state for retired context-index startup proof",
	})
	require.NoError(t, err)

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	created, err := CreateGraphIndex(configJSON, component.Dependencies{NATSClient: nc})
	require.NoError(t, err)
	indexComponent := created.(component.LifecycleComponent)
	require.NoError(t, indexComponent.Initialize())
	require.NoError(t, indexComponent.Start(ctx))
	defer func() { require.NoError(t, indexComponent.Stop(context.Background())) }()

	_, err = nc.GetKeyValueBucket(ctx, "CONTEXT_INDEX")
	require.ErrorIs(t, err, jetstream.ErrBucketNotFound,
		"a fresh graph-index start must not create the retired provenance-only bucket")
}

// TestIntegration_Start_OffCatalogOutputSubjectFailsBoot is the F2 closure
// test: a graph-index configuration whose KV output port subject names a
// bucket absent from the framework KV catalog (an operator typo of
// OUTGOING_INDEX) must fail boot naming the unresolved subject — and must NOT
// silently create a stray bucket that no guard protects and no reader
// consumes. The rejection now lands at FACTORY config validation (any output
// outside graph-index's four owned subjects is invalid), with the Start-loop
// belt behind it for a config that never passed Validate.
//
// Config.Validate already rejects a config MISSING one of the four required
// index buckets, so the reachable F2 hole was an ADDITIONAL output port whose
// subject resolves to nothing: pre-catalog, Start silently created it.
func TestIntegration_Start_OffCatalogOutputSubjectFailsBoot(t *testing.T) {
	const typoSubject = "OUTGOING_INDEX_TYPO"
	ctx := context.Background()
	tc := getSharedNATSClient(t)

	// Guard against a stray bucket left by a prior (pre-fix) run on a reused
	// NATS container: the non-creation assertion below must start from absent.
	_ = tc.Client.DeleteKeyValueBucket(ctx, typoSubject)

	cfg := DefaultConfig()
	cfg.Ports.Outputs = append(cfg.Ports.Outputs, component.PortDefinition{
		Name: "outgoing_index_typo", Config: component.KVWritePort{Bucket: typoSubject}, // the operator typo
	})

	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, cerr := CreateGraphIndex(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	if cerr == nil {
		lc := comp.(component.LifecycleComponent)
		require.NoError(t, lc.Initialize())
		cerr = lc.Start(ctx)
		if cerr == nil {
			defer func() { _ = lc.Stop(context.Background()) }()
		}
	}
	require.Error(t, cerr,
		"an off-catalog output subject must fail boot, not silently create a stray bucket")
	assert.Contains(t, cerr.Error(), typoSubject,
		"the boot failure must name the unresolved subject")

	// The stray bucket must not exist.
	_, gerr := tc.Client.GetKeyValueBucket(ctx, typoSubject)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"the off-catalog subject must not have been created")
}
