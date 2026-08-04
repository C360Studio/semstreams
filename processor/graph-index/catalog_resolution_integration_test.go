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
	defer func() { require.NoError(t, indexComponent.Stop(5*time.Second)) }()

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
		Name:    "outgoing_index_typo",
		Type:    "kv-write",
		Subject: typoSubject, // the operator typo
	})

	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, cerr := CreateGraphIndex(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	if cerr == nil {
		lc := comp.(component.LifecycleComponent)
		require.NoError(t, lc.Initialize())
		cerr = lc.Start(ctx)
		if cerr == nil {
			defer func() { _ = lc.Stop(2 * time.Second) }()
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

// TestIntegration_Start_ForeignOwnedOutputSubjectFailsBoot closes the
// owner-enforcement hole ADJACENT to F2: an extra graph-index output whose
// subject names a catalog bucket OWNED BY ANOTHER COMPONENT (ENTITY_STATES,
// owned by graph-ingest) must fail boot naming the foreign subject and its
// owner — graph-index may never invoke the OWNER seam (create + destructive
// History reconcile) for a bucket it does not own, and assignBucket would
// silently drop the handle anyway. Owner enforcement is call-site selection;
// a config string must not be able to defeat it.
func TestIntegration_Start_ForeignOwnedOutputSubjectFailsBoot(t *testing.T) {
	ctx := context.Background()
	tc := getSharedNATSClient(t)

	// The shared TestMain pre-creates ENTITY_STATES for the other tests, so
	// non-creation is asserted on a DIFFERENT foreign-owned catalog bucket
	// (OWNER_CLAIMS, owned by the ownership registry) that nothing in this
	// package provisions.
	_ = tc.Client.DeleteKeyValueBucket(ctx, graph.BucketOwnerClaims)

	cfg := DefaultConfig()
	cfg.Ports.Outputs = append(cfg.Ports.Outputs, component.PortDefinition{
		Name:    "foreign_owned_output",
		Type:    "kv-write",
		Subject: graph.BucketOwnerClaims, // catalog member, foreign owner
	})

	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, cerr := CreateGraphIndex(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	if cerr == nil {
		lc := comp.(component.LifecycleComponent)
		require.NoError(t, lc.Initialize())
		cerr = lc.Start(ctx)
		if cerr == nil {
			defer func() { _ = lc.Stop(2 * time.Second) }()
		}
	}
	require.Error(t, cerr,
		"a foreign-owned output subject must fail boot (factory validation or Start), never invoke the owner seam")
	assert.Contains(t, cerr.Error(), graph.BucketOwnerClaims,
		"the failure must name the foreign subject")

	// The foreign bucket must NOT have been created by graph-index.
	_, gerr := tc.Client.GetKeyValueBucket(ctx, graph.BucketOwnerClaims)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"graph-index must not have provisioned another owner's bucket")
}

// TestIntegration_CreateOutputBuckets_BeltRejectsForeignSubject exercises the
// Start-loop BELT directly: a Component whose config never passed
// Config.Validate (the dynamically-supplied shape) still may not Ensure a
// foreign-owned bucket — createOutputBuckets itself re-checks the owned set.
func TestIntegration_CreateOutputBuckets_BeltRejectsForeignSubject(t *testing.T) {
	ctx := context.Background()
	tc := getSharedNATSClient(t)
	_ = tc.Client.DeleteKeyValueBucket(ctx, graph.BucketOwnerClaims)

	cfg := DefaultConfig()
	cfg.Ports.Outputs = append(cfg.Ports.Outputs, component.PortDefinition{
		Name:    "foreign_owned_output",
		Type:    "kv-write",
		Subject: graph.BucketOwnerClaims,
	})
	// Construct the component directly, bypassing the factory's validation —
	// the belt must hold on its own.
	c := &Component{config: cfg, natsClient: tc.Client}

	err := c.createOutputBuckets(ctx)
	require.Error(t, err, "the Start-loop belt must reject a foreign-owned subject on its own")
	assert.Contains(t, err.Error(), graph.BucketOwnerClaims)
	assert.Contains(t, err.Error(), "ownership registry",
		"the rejection must name the catalog owner")

	_, gerr := tc.Client.GetKeyValueBucket(ctx, graph.BucketOwnerClaims)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"the belt must reject BEFORE any seam invocation for the foreign bucket")
}
