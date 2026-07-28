//go:build integration

package clustering

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_SummaryStore_RoundTrip proves the content-addressed summary
// store's miss → put → hit → count contract against real NATS KV.
func TestIntegration_SummaryStore_RoundTrip(t *testing.T) {
	natsClient := getSharedNATSClient(t)
	ctx := context.Background()

	kv, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_SUMMARIES_TEST",
	})
	require.NoError(t, err)
	defer func() {
		keys, _ := kv.Keys(ctx)
		for _, key := range keys {
			_ = kv.Delete(ctx, key)
		}
	}()

	store := NewNATSSummaryStore(kv)

	members := []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002"}
	hash := MembershipHash(members)

	// Empty bucket → count 0, and a miss returns (nil, nil), not an error.
	n, err := store.CountSummaries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	miss, err := store.GetSummary(ctx, 0, hash)
	require.NoError(t, err)
	assert.Nil(t, miss)

	// Write an enhanced record.
	rec := &CommunitySummaryRecord{
		MembershipHash: hash,
		Level:          0,
		LLMSummary:     "a fleet of autonomous drones",
		Model:          "test-model",
		Status:         SummaryStatusEnhanced,
		MemberCount:    len(members),
		GeneratedAt:    time.Now(),
	}
	require.NoError(t, store.PutSummary(ctx, rec))

	// Hit returns the stored record.
	got, err := store.GetSummary(ctx, 0, hash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, SummaryStatusEnhanced, got.Status)
	assert.Equal(t, "a fleet of autonomous drones", got.LLMSummary)
	assert.Equal(t, hash, got.MembershipHash)

	// A same-membership double-write is idempotent (no error, count stays 1).
	require.NoError(t, store.PutSummary(ctx, rec))

	n, err = store.CountSummaries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// A different level for the same hash is a DISTINCT key.
	rec1 := *rec
	rec1.Level = 1
	require.NoError(t, store.PutSummary(ctx, &rec1))
	n, err = store.CountSummaries(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// The level-0 lookup is unaffected by the level-1 write.
	got0, err := store.GetSummary(ctx, 0, hash)
	require.NoError(t, err)
	require.NotNil(t, got0)
	assert.Equal(t, 0, got0.Level)
}
