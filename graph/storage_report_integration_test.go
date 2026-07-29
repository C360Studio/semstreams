//go:build integration

package graph

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
)

// storageReportRows ranges the published bucket the way an operator surface
// would — every live key, decoded — so the assertions run against what a
// consumer can actually see rather than against the publisher's return value.
func storageReportRows(
	ctx context.Context, t *testing.T, bucket jetstream.KeyValue,
) map[string]natsclient.ResourceReport {
	t.Helper()

	lister, err := bucket.ListKeys(ctx)
	require.NoError(t, err)
	defer func() { _ = lister.Stop() }()

	rows := make(map[string]natsclient.ResourceReport)
	for key := range lister.Keys() {
		entry, gerr := bucket.Get(ctx, key)
		require.NoError(t, gerr)
		var row natsclient.ResourceReport
		require.NoError(t, json.Unmarshal(entry.Value(), &row))
		rows[row.Resource.Name] = row
	}
	return rows
}

// TestIntegration_StorageReport_PublishesThroughTheCatalogSeam drives the whole
// production wire against a real server: the catalog descriptor acquires the
// bucket through the OWNER seam, the account collector enumerates real
// JetStream resources, and the publisher writes one key per resource that a
// consumer can range and decode.
//
// The retention assertion is the spec scenario stated against a real bucket
// rather than against the descriptor: a report row is reclaimed because the
// collector DECIDED the resource is gone, so there must be no policy on this
// bucket capable of expiring one behind its back.
func TestIntegration_StorageReport_PublishesThroughTheCatalogSeam(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	js, err := client.JetStream()
	require.NoError(t, err)

	// A resource this process will report on, created before the report bucket
	// exists so the inventory is genuinely account-scoped.
	const disappearing = "REPORT_INTEGRATION_TRANSIENT"
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     disappearing,
		Subjects: []string{"report.integration.transient.>"},
		Storage:  jetstream.FileStorage,
		MaxBytes: 8 << 20,
	})
	require.NoError(t, err)

	bucket, err := EnsureCatalogBucket(ctx, client, BucketStorageReport)
	require.NoError(t, err)

	collector, err := natsclient.NewStorageInventoryCollector(
		client.AccountStreamLister,
		natsclient.StorageInventoryConfig{
			OwnerResolver: OwnerOf,
			ProducedBy:    "integration-test",
			Timeout:       20 * time.Second,
		},
	)
	require.NoError(t, err)

	publisher, err := natsclient.NewStorageReportPublisher(bucket, natsclient.StorageReportConfig{
		Thresholds: func() natsclient.StoragePressureThresholds {
			return natsclient.DefaultStoragePressureThresholds()
		},
	})
	require.NoError(t, err)

	inventory, err := collector.Collect(ctx)
	require.NoError(t, err)
	require.False(t, inventory.Stale)
	require.NotEmpty(t, inventory.Resources)

	result, err := publisher.Publish(ctx, inventory)
	require.NoError(t, err)
	assert.Equal(t, len(inventory.Resources), result.Published,
		"every inventoried resource gets a key — the report is not a filtered view")

	rows := storageReportRows(ctx, t, bucket)
	assert.Len(t, rows, len(inventory.Resources))

	t.Run("the bucket declares no policy that could expire a row", func(t *testing.T) {
		maxAge, maxBytes, rerr := natsclient.BucketRetention(ctx, bucket)
		require.NoError(t, rerr)
		assert.Equal(t, time.Duration(0), maxAge,
			"a TTL would age out rows the collector never decided to drop")
		// Non-positive, not zero: the server writes -1 for "no limit" on a KV
		// backing stream, which is the same unlimited encoding NewCapacity
		// classifies. Asserting == 0 here would fail against a real server
		// while the bucket is in fact unbounded.
		assert.LessOrEqual(t, maxBytes, int64(0),
			"a byte ceiling would evict rows the collector never decided to drop")
	})

	t.Run("a bounded resource reports real capacity and a real headroom", func(t *testing.T) {
		row, ok := rows[disappearing]
		require.True(t, ok, "the resource must appear in the published report")
		require.Equal(t, natsclient.CapacityBounded, row.Resource.Bytes.State)
		limit, ok := row.Resource.Bytes.Limit()
		require.True(t, ok)
		assert.Equal(t, int64(8<<20), limit)
		require.NotNil(t, row.Projection.HeadroomBytes)
		assert.Positive(t, *row.Projection.HeadroomBytes)
		assert.True(t, row.Pressure.Evaluated)
		assert.Equal(t, natsclient.PressureNormal, row.Pressure.State)
		assert.Equal(t, "integration-test", row.ProducedBy)
	})

	t.Run("the report observes itself, attributed to its catalog owner", func(t *testing.T) {
		// Honest and expected: the observer IS a real storage resource, and its
		// bounded History is what keeps it from becoming the thing that fills
		// the account.
		row, ok := rows[natsclient.KVStreamPrefix+BucketStorageReport]
		require.True(t, ok, "the report bucket's own backing stream must be inventoried")
		assert.Equal(t, natsclient.ResourceKeyValue, row.Resource.Kind)
		assert.Equal(t, natsclient.AttributionAttributed, row.Resource.Attribution)

		declared, ok := SpecFor(BucketStorageReport)
		require.True(t, ok)
		assert.Equal(t, declared.Owner, row.Resource.Owner)
	})

	t.Run("the first observation of a resource projects no rate", func(t *testing.T) {
		row := rows[disappearing]
		assert.Equal(t, natsclient.GrowthUnknown, row.Growth.State,
			"one observation extrapolates nothing")
		assert.Nil(t, row.Projection.TimeToThreshold)
	})

	t.Run("a disappeared resource is deleted, not expired", func(t *testing.T) {
		require.NoError(t, js.DeleteStream(ctx, disappearing))

		next, cerr := collector.Collect(ctx)
		require.NoError(t, cerr)
		reclaimed, perr := publisher.Publish(ctx, next)
		require.NoError(t, perr)
		assert.Equal(t, 1, reclaimed.Deleted, "exactly the resource that went away")

		after := storageReportRows(ctx, t, bucket)
		assert.NotContains(t, after, disappearing,
			"the collector deleted the key; nothing aged it out")

		// The tombstone is a real KV delete marker, which is also what stops the
		// growth walk from differencing across a resource's disappearance.
		key, kerr := natsclient.StorageReportKey(disappearing)
		require.NoError(t, kerr)
		history, herr := bucket.History(ctx, key)
		require.NoError(t, herr)
		require.NotEmpty(t, history)
		assert.Equal(t, jetstream.KeyValueDelete, history[len(history)-1].Operation())
	})
}

// TestIntegration_StorageReport_GrowthSurvivesRestart is the spec's restart
// scenario against a real bucket: the growth series lives in the published
// report, so a process that comes up holding nothing measures a rate on its
// FIRST publication rather than after a smoothing window.
//
// The observations are synthesized with explicit timestamps rather than taken
// from two real collections, so the test proves the durable path (real KV
// history, real revision ordering, real JSON round trip through the catalog's
// declared History depth) without a wall-clock sleep deciding whether it
// passes. Account enumeration itself is covered by the collector's own
// integration test.
func TestIntegration_StorageReport_GrowthSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	bucket, err := EnsureCatalogBucket(ctx, client, BucketStorageReport)
	require.NoError(t, err)

	thresholds := func() natsclient.StoragePressureThresholds {
		return natsclient.DefaultStoragePressureThresholds()
	}
	newProcess := func() *natsclient.StorageReportPublisher {
		publisher, perr := natsclient.NewStorageReportPublisher(bucket,
			natsclient.StorageReportConfig{Thresholds: thresholds})
		require.NoError(t, perr)
		return publisher
	}

	const resource = "REPORT_INTEGRATION_GROWING"
	observation := func(at time.Time, used int64) natsclient.StorageInventory {
		return natsclient.StorageInventory{
			ProducedBy:  "integration-test",
			CollectedAt: at,
			Resources: []natsclient.StorageResource{{
				Name:        resource,
				Kind:        natsclient.ResourceOrdinaryStream,
				Attribution: natsclient.AttributionNotApplicable,
				Tier:        natsclient.TierFile,
				Bytes:       natsclient.NewCapacity(1<<30, used, true),
				Messages:    natsclient.NewCapacity(0, 1, true),
			}},
		}
	}

	first := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)

	_, err = newProcess().Publish(ctx, observation(first, 100<<20))
	require.NoError(t, err)

	// The process dies. A new one binds the same bucket with nothing in memory.
	_, err = newProcess().Publish(ctx, observation(first.Add(5*time.Minute), 160<<20))
	require.NoError(t, err)

	rows := storageReportRows(ctx, t, bucket)
	row, ok := rows[resource]
	require.True(t, ok)

	require.Equal(t, natsclient.GrowthKnown, row.Growth.State,
		"the restarted process read its baseline out of the published report")
	rate, ok := row.Growth.Rate()
	require.True(t, ok)
	assert.InDelta(t, float64(60<<20)/(5*60), rate, 0.001)
	assert.Equal(t, 5*time.Minute, row.Growth.ObservedOver)
	require.NotNil(t, row.Growth.ObservedFrom)
	assert.Equal(t, first, row.Growth.ObservedFrom.UTC())
	require.NotNil(t, row.Projection.TimeToThreshold,
		"a rate that survived the restart projects a time-to-threshold immediately")
}

// TestIntegration_StorageReport_StableSizeUnderChurnReportsZeroGrowth is the
// spec's churn scenario against a real KV bucket: contents continuously
// replaced under History 1, total size unchanged between successive
// observations, rate exactly zero.
//
// The falsified implementation is what makes this worth an integration test.
// The churned bucket's backing stream carries a real, non-trivial span between
// its first and last retained message, so dividing its byte count by that span
// would report sustained growth and project exhaustion for a resource that
// never grew. The observations are the only input the rate is derived from.
func TestIntegration_StorageReport_StableSizeUnderChurnReportsZeroGrowth(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	client := tc.Client

	const churned = "REPORT_INTEGRATION_CHURN"
	// MaxBytes here is a TEST FIXTURE, not a pattern: it exists only so the
	// resource has a bound for a projection to target, which is what lets this
	// test distinguish "measured, and not growing" from "unbounded, so nothing
	// to project". It is generous enough never to be reached — a ceiling
	// rejects a replacement before compaction reclaims the superseded revision,
	// which is the measurement that retired this capability's predecessor.
	churn, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:   churned,
		History:  1,
		MaxBytes: 64 << 20,
	})
	require.NoError(t, err)

	value := make([]byte, 4096)
	for i := 0; i < 50; i++ {
		_, perr := churn.Put(ctx, "hot", value)
		require.NoError(t, perr)
	}

	bucket, err := EnsureCatalogBucket(ctx, client, BucketStorageReport)
	require.NoError(t, err)
	collector, err := natsclient.NewStorageInventoryCollector(
		client.AccountStreamLister,
		natsclient.StorageInventoryConfig{OwnerResolver: OwnerOf, ProducedBy: "integration-test"},
	)
	require.NoError(t, err)
	publisher, err := natsclient.NewStorageReportPublisher(bucket, natsclient.StorageReportConfig{
		Thresholds: func() natsclient.StoragePressureThresholds {
			return natsclient.DefaultStoragePressureThresholds()
		},
	})
	require.NoError(t, err)

	inventory, err := collector.Collect(ctx)
	require.NoError(t, err)
	churnStream := natsclient.KVStreamPrefix + churned
	var observed int64
	for _, res := range inventory.Resources {
		if res.Name == churnStream {
			used, ok := res.Bytes.Usage()
			require.True(t, ok)
			observed = used
		}
	}
	require.Positive(t, observed, "test premise: the churned bucket holds bytes")

	// Two observations of the same size, five minutes apart on the report's
	// clock. Under History 1 the bucket compacts every superseded revision, so
	// this is what continuous replacement at a stable size looks like.
	base := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	stable := func(at time.Time) natsclient.StorageInventory {
		inv := inventory
		inv.CollectedAt = at
		return inv
	}
	_, err = publisher.Publish(ctx, stable(base))
	require.NoError(t, err)
	_, err = publisher.Publish(ctx, stable(base.Add(5*time.Minute)))
	require.NoError(t, err)

	rows := storageReportRows(ctx, t, bucket)
	row, ok := rows[churnStream]
	require.True(t, ok)

	require.Equal(t, natsclient.GrowthKnown, row.Growth.State)
	rate, ok := row.Growth.Rate()
	require.True(t, ok)
	assert.Zero(t, rate, "a resource whose size did not change between observations is not growing")
	assert.Nil(t, row.Projection.TimeToThreshold, "and no exhaustion is projected for it")
	assert.Equal(t, natsclient.ProjectionUnavailableNotGrowing, row.Projection.TimeToThresholdUnavailable)

	// The falsified alternative, computed here so the contrast is explicit
	// rather than asserted in prose: this resource's own retained span is real
	// and non-zero, and dividing by it would have reported growth.
	stream, err := client.GetStream(ctx, churnStream)
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	span := info.State.LastTime.Sub(info.State.FirstTime)
	if span > 0 {
		assert.Positive(t, float64(observed)/span.Seconds(),
			"test premise: bytes-over-own-span would have reported a positive rate here")
	}
}
