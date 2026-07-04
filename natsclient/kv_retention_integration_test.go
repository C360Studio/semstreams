//go:build integration

package natsclient

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_BucketRetention_D1Guardrail proves the boot-time guardrail
// against a real server: a clean bucket reads MaxAge 0 and passes; a bucket
// created with a TTL reads it back and trips ErrGraphBucketRetention (ADR-068 D1).
func TestIntegration_BucketRetention_D1Guardrail(t *testing.T) {
	ctx := context.Background()
	natsContainer, natsURL := startTestNATSContainerWithJS(ctx, t)
	defer natsContainer.Terminate(ctx)

	client, err := NewClient(natsURL, WithMaxReconnects(0))
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	defer client.Close(ctx)

	// Clean bucket (no retention) — reads MaxAge 0 and passes the guardrail.
	clean, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: "CLEAN_STATES"})
	require.NoError(t, err)
	maxAge, _, err := BucketRetention(ctx, clean)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), maxAge)
	require.NoError(t, client.NewKVStore(clean).AssertNoLifecycleRetention(ctx, "CLEAN_STATES"))

	// Bucket with a TTL — reads it back and trips the guardrail.
	ttlBucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: "TTL_STATES", TTL: 24 * time.Hour})
	require.NoError(t, err)
	maxAge, _, err = BucketRetention(ctx, ttlBucket)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, maxAge)
	err = client.NewKVStore(ttlBucket).AssertNoLifecycleRetention(ctx, "TTL_STATES")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGraphBucketRetention)
}
