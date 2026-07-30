//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/metric"
)

// TestIntegration_StorageInventory_EnumeratesTheAccountNotTheClient is the
// load-bearing proof of design decision 3: the inventory reports the ACCOUNT,
// not this client's memory of it.
//
// The setup is the falsifiable part. Every resource is created through a
// SEPARATE connection standing in for a prior deploy or a sister process, and
// the collecting client is built fresh with a metrics registry so its
// tracked-stream map is a real, empty map. The test asserts that map is empty
// while the inventory returns the resources anyway — so a regression to the
// jetstream_metrics.go tracked view fails here rather than silently reporting a
// subset of the account.
//
// It also pins, against a real server, the facts a fake cannot establish:
//   - both account listings return KV_* and OBJ_* backing streams, so KV buckets
//     and ObjectStores need no separate enumeration path;
//   - the info listing carries config AND state together, so no follow-up
//     describe per resource is needed;
//   - the server's encoding of "no limit" classifies as unbounded, and a real
//     finite MaxBytes classifies as bounded — the two do not collapse;
//   - on a HEALTHY account the name listing and the info listing agree, so
//     reconciling them adds no spurious unknown rows in the normal case. That
//     negative control is what keeps the second listing from becoming a source
//     of phantom findings in every deployment.
func TestIntegration_StorageInventory_EnumeratesTheAccountNotTheClient(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	const (
		priorStream      = "PRIOR_DEPLOY_EVENTS"
		priorBoundedName = "PRIOR_DEPLOY_BOUNDED"
		priorKVBucket    = "PRIOR_DEPLOY_STATE"
		priorObjBucket   = "PRIOR_DEPLOY_CONTENT"
		boundedMaxBytes  = int64(1 << 20)
	)

	// --- a prior deploy / sister process, on its own connection ---------------
	creator, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, creator.Connect(ctx))
	creatorJS, err := creator.JetStream()
	require.NoError(t, err)

	_, err = creatorJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     priorStream,
		Subjects: []string{"prior.deploy.>"},
		Storage:  jetstream.FileStorage,
	})
	require.NoError(t, err)
	_, err = creatorJS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     priorBoundedName,
		Subjects: []string{"prior.bounded.>"},
		Storage:  jetstream.MemoryStorage,
		MaxBytes: boundedMaxBytes,
	})
	require.NoError(t, err)
	_, err = creatorJS.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: priorKVBucket})
	require.NoError(t, err)
	_, err = creatorJS.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: priorObjBucket})
	require.NoError(t, err)
	require.NoError(t, creator.Close(ctx))

	// --- the collecting process: a fresh client that never touched any of them -
	registry := metric.NewMetricsRegistry()
	observer, err := NewClient(natsURL, WithMaxReconnects(0), WithMetrics(registry))
	require.NoError(t, err)
	require.NoError(t, observer.Connect(ctx))
	defer observer.Close(ctx)

	require.NotNil(t, observer.jsMetrics, "test premise: tracked-stream metrics are enabled")
	observer.jsMetrics.mu.RLock()
	trackedCount := len(observer.jsMetrics.streams)
	observer.jsMetrics.mu.RUnlock()
	require.Equal(t, 0, trackedCount,
		"test premise: this client has created or accessed no streams, so a client-touched "+
			"inventory would report nothing")

	collector, err := NewStorageInventoryCollector(
		observer.AccountStreamLister,
		StorageInventoryConfig{
			// Attribution is out of scope here: this test is about enumeration.
			// The catalog cross-pin lives in graph, where OwnerOf is defined.
			OwnerResolver: func(string) string { return "" },
			Timeout:       20 * time.Second,
			ProducedBy:    "integration-test",
		},
	)
	require.NoError(t, err)

	inv, err := collector.Collect(ctx)
	require.NoError(t, err)
	assert.False(t, inv.Stale)
	assert.False(t, inv.CollectedAt.IsZero())
	assert.Equal(t, "integration-test", inv.ProducedBy)

	got := make(map[string]StorageResource, len(inv.Resources))
	for _, res := range inv.Resources {
		got[res.Name] = res
	}

	t.Run("an untouched ordinary stream is enumerated", func(t *testing.T) {
		res, ok := got[priorStream]
		require.True(t, ok, "a stream this process never created or touched must still appear")
		assert.Equal(t, ResourceOrdinaryStream, res.Kind)
		assert.Equal(t, TierFile, res.Tier)
		assert.Equal(t, CapacityUnbounded, res.Bytes.State,
			"the server's no-limit encoding must classify as unbounded, not bounded-zero")
		used, ok := res.Bytes.Usage()
		require.True(t, ok, "actual usage comes from the listing itself")
		assert.GreaterOrEqual(t, used, int64(0))
	})

	t.Run("an untouched KV bucket is enumerated through its backing stream", func(t *testing.T) {
		res, ok := got[KVStreamPrefix+priorKVBucket]
		require.True(t, ok, "KV_* backing streams appear in the account listing")
		assert.Equal(t, ResourceKeyValue, res.Kind)
		assert.Equal(t, priorKVBucket, res.Bucket)
		assert.Equal(t, AttributionUnattributed, res.Attribution,
			"a bucket outside the catalog reports unattributed, not omitted")
		assert.Equal(t, "", res.Owner)
		assert.False(t, res.Attributed())
	})

	t.Run("an untouched ObjectStore is enumerated through its backing stream", func(t *testing.T) {
		res, ok := got[ObjectStoreStreamPrefix+priorObjBucket]
		require.True(t, ok, "OBJ_* backing streams appear in the account listing")
		assert.Equal(t, ResourceObjectStore, res.Kind)
		assert.Equal(t, priorObjBucket, res.Bucket)
		assert.Equal(t, AttributionNotApplicable, res.Attribution,
			"an ObjectStore has no owner concept — distinct from an escaped KV bucket")
	})

	t.Run("a real finite bound reads back as bounded on its own tier", func(t *testing.T) {
		res, ok := got[priorBoundedName]
		require.True(t, ok)
		assert.Equal(t, TierMemory, res.Tier, "memory and file are separate account tiers")
		require.Equal(t, CapacityBounded, res.Bytes.State)
		limit, ok := res.Bytes.Limit()
		require.True(t, ok)
		assert.Equal(t, boundedMaxBytes, limit, "the configured limit comes from the listing itself")
	})

	t.Run("the tracked-stream view still cannot see them", func(t *testing.T) {
		observer.jsMetrics.mu.RLock()
		defer observer.jsMetrics.mu.RUnlock()
		assert.Empty(t, observer.jsMetrics.streams,
			"enumeration must not have gone through the tracked-stream map")
	})

	t.Run("a healthy account produces no undescribable rows", func(t *testing.T) {
		// The negative control for the name/info reconciliation: if the real
		// server named anything the info listing did not describe, every
		// deployment would carry permanent phantom unknown rows.
		for _, res := range inv.Resources {
			assert.NotEmpty(t, res.Name, "a real server returns no nameless entries")
			assert.NotEqual(t, TierUnknown, res.Tier)
			assert.False(t, res.Undescribable(),
				"resource %q reported undescribable on a healthy account", res.Name)
		}
	})

	t.Run("the two account listings agree on a healthy server", func(t *testing.T) {
		// Pins the assumption the reconciliation rests on: the name listing is a
		// superset of the info listing, and on a healthy account they are equal.
		lister, err := observer.AccountStreamLister()
		require.NoError(t, err)

		infoNames := make(map[string]struct{})
		infoWalk := lister.ListStreams(ctx)
		for info := range infoWalk.Info() {
			require.NotNil(t, info)
			infoNames[info.Config.Name] = struct{}{}
		}
		require.NoError(t, infoWalk.Err())

		listedNames := make(map[string]struct{})
		nameWalk := lister.StreamNames(ctx)
		for name := range nameWalk.Name() {
			listedNames[name] = struct{}{}
		}
		require.NoError(t, nameWalk.Err())

		assert.Equal(t, listedNames, infoNames,
			"a healthy account describes everything it names")
		for name := range infoNames {
			assert.Contains(t, listedNames, name,
				"the name listing must be a superset of the info listing")
		}
		assert.Len(t, inv.Resources, len(listedNames),
			"the inventory covers exactly the account's named resources")
	})
}

// TestIntegration_StorageInventory_DegradesToLastGoodWhenNATSGoesAway proves the
// last-good-with-timestamp contract against a real failure rather than a
// scripted one: the server disappears, the collection fails, and the read path
// keeps serving the previous result with the timestamp it was collected at
// instead of reporting an empty account.
func TestIntegration_StorageInventory_DegradesToLastGoodWhenNATSGoesAway(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	terminated := false
	defer func() {
		if !terminated {
			natsContainer.Terminate(ctx)
		}
	}()

	client, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	js, err := client.JetStream()
	require.NoError(t, err)
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "DEGRADE_EVENTS",
		Subjects: []string{"degrade.>"},
	})
	require.NoError(t, err)

	collector, err := NewStorageInventoryCollector(
		client.AccountStreamLister,
		StorageInventoryConfig{
			OwnerResolver: func(string) string { return "" },
			Timeout:       3 * time.Second,
			ProducedBy:    "integration-test",
		},
	)
	require.NoError(t, err)

	good, err := collector.Collect(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, good.Resources)
	collectedAt := good.CollectedAt

	require.NoError(t, natsContainer.Terminate(ctx))
	terminated = true

	_, err = collector.Collect(ctx)
	require.Error(t, err, "a collection against a dead server must fail rather than report an empty account")

	degraded := collector.Latest()
	assert.Equal(t, good.Resources, degraded.Resources, "last-good survives the failure")
	assert.Equal(t, collectedAt, degraded.CollectedAt, "the timestamp stays the one the data came from")
	assert.True(t, degraded.Stale)
	assert.NotEmpty(t, degraded.StaleReason)
	require.NotNil(t, degraded.StaleSince)
	assert.False(t, degraded.StaleSince.IsZero())
}
