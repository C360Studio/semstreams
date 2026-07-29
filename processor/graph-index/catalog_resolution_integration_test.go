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
)

// TestIntegration_Start_OffCatalogOutputSubjectFailsBoot is the F2 closure
// test: a graph-index configuration whose KV output port subject names a
// bucket absent from the framework KV catalog (an operator typo of
// OUTGOING_INDEX) must fail the component's Start naming the unresolved
// subject — and must NOT silently create a stray bucket that no guard
// protects and no reader consumes.
//
// Config.Validate already rejects a config MISSING one of the four required
// index buckets, so the reachable F2 hole is an ADDITIONAL output port whose
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
	comp, err := CreateGraphIndex(cfgJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	lc := comp.(component.LifecycleComponent)
	require.NoError(t, lc.Initialize())

	err = lc.Start(ctx)
	if err == nil {
		defer func() { _ = lc.Stop(2 * time.Second) }()
	}
	require.Error(t, err,
		"an off-catalog output subject must fail boot, not silently create a stray bucket")
	assert.Contains(t, err.Error(), typoSubject,
		"the boot failure must name the unresolved subject")

	// The stray bucket must not exist.
	_, gerr := tc.Client.GetKeyValueBucket(ctx, typoSubject)
	assert.ErrorIs(t, gerr, jetstream.ErrBucketNotFound,
		"the off-catalog subject must not have been created")
}
