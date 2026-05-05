//go:build integration

// Package rule_test - Integration test for count_in_window with a real NATS KV bucket.
//
// This test closes the architect's verification step #7: restart-recovery of
// window state via a real testcontainer NATS. Unit tests in
// expression/window_operator_test.go and window_tracker_test.go cover the
// bucketed-counter semantics against in-memory mocks; this test exercises the
// full round-trip including JSON serialisation, KV CAS, and state survival
// across a simulated process restart (two WindowTracker instances, same bucket).
package rule_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/rule"
)

// TestIntegration_CountInWindow_RealKV verifies that WindowTracker persists and
// recovers sliding-window counts across tracker restarts using a real NATS KV
// bucket.
//
// Test plan:
//  1. Create a real RULE_WINDOWS bucket via testcontainer NATS.
//  2. Increment "alice" under "rate-rule" 10 times.
//  3. Discard the tracker (simulate process restart).
//  4. Construct a new tracker against the same bucket.
//  5. Increment once more.
//  6. Assert count == 11 (state survived the restart).
func TestIntegration_CountInWindow_RealKV(t *testing.T) {
	natsClient := getTestNATSClient(t)
	ctx := context.Background()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Create the RULE_WINDOWS bucket for this test.
	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: rule.WindowsBucketName,
	})
	require.NoError(t, err, "failed to create RULE_WINDOWS bucket")

	const ruleID = "rate-rule"
	const dimension = "alice"
	const window = 10 * time.Minute
	now := time.Now()

	// Phase 1: first tracker lifetime — 10 increments.
	wt1 := rule.NewWindowTracker(natsclient.WrapKV(bucket), nil)
	for i := 0; i < 10; i++ {
		count, err := wt1.Increment(ctx, ruleID, dimension, now, window)
		require.NoError(t, err, "increment %d failed", i+1)
		assert.Equal(t, i+1, count, "count after %d increments should be %d", i+1, i+1)
	}

	// Phase 2: simulate restart — discard wt1 and construct a new tracker
	// against the same bucket. All previously-written state must survive.
	wt2 := rule.NewWindowTracker(natsclient.WrapKV(bucket), nil)
	count, err := wt2.Increment(ctx, ruleID, dimension, now.Add(30*time.Second), window)
	require.NoError(t, err, "post-restart increment failed")

	// The 30-second advance keeps both increments inside the same 1-minute bucket,
	// so the total is 10 (pre-restart) + 1 (post-restart) = 11.
	assert.Equal(t, 11, count, "count must be 11 after restart; window state must persist in NATS KV")
}
