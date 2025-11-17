package natsclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemporalResolver_BinarySearch(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	defer tc.Terminate()
	ctx := context.Background()

	// Create KV bucket
	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "test_temporal",
		History: 64,
	})
	require.NoError(t, err)

	resolver := NewTemporalResolver(ctx, bucket)

	// Create many revisions with known timestamps
	entityID := "test.entity"
	var timestamps []time.Time
	baseTime := time.Now().Add(-1 * time.Hour)

	// Create 50 revisions over an hour period
	for i := 0; i < 50; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		timestamps = append(timestamps, timestamp)

		data := map[string]any{
			"id":      entityID,
			"value":   i,
			"created": timestamp,
		}
		bytes, _ := json.Marshal(data)
		_, err := bucket.Put(ctx, entityID, bytes)
		require.NoError(t, err)

		// Small delay to ensure different Created() timestamps
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("Boundary_BeforeAllHistory", func(t *testing.T) {
		// Query before any history exists
		targetTime := baseTime.Add(-1 * time.Hour)
		entry, err := resolver.GetAtTimestamp(ctx, entityID, targetTime)
		require.NoError(t, err)

		var result map[string]any
		json.Unmarshal(entry.Value(), &result)
		assert.Equal(t, float64(0), result["value"]) // Should get first entry
	})

	t.Run("Boundary_AfterAllHistory", func(t *testing.T) {
		// Query after all history
		targetTime := time.Now().Add(1 * time.Hour)
		entry, err := resolver.GetAtTimestamp(ctx, entityID, targetTime)
		require.NoError(t, err)

		var result map[string]any
		json.Unmarshal(entry.Value(), &result)
		assert.Equal(t, float64(49), result["value"]) // Should get last entry
	})

	t.Run("ExactMatch", func(t *testing.T) {
		// Let's use actual entry Created() time instead of our timestamps
		history, err := bucket.History(ctx, entityID)
		require.NoError(t, err)

		// Use middle entry's actual Created() time
		midIdx := len(history) / 2
		targetTime := history[midIdx].Created()

		entry, err := resolver.GetAtTimestamp(ctx, entityID, targetTime)
		require.NoError(t, err)

		var result map[string]any
		json.Unmarshal(entry.Value(), &result)
		// Should get the exact entry or very close
		value := result["value"].(float64)
		t.Logf("Target time: %v, Got value: %v", targetTime, value)
		assert.True(t, value >= float64(midIdx-2) && value <= float64(midIdx+2),
			"Expected value around %d, got %v", midIdx, value)
	})

	t.Run("BetweenRevisions", func(t *testing.T) {
		// Get actual history to work with real timestamps
		history, err := bucket.History(ctx, entityID)
		require.NoError(t, err)

		if len(history) > 20 {
			// Query between two revisions
			rev10Time := history[10].Created()
			rev11Time := history[11].Created()
			targetTime := rev10Time.Add(rev11Time.Sub(rev10Time) / 2) // Midpoint

			entry, err := resolver.GetAtTimestamp(ctx, entityID, targetTime)
			require.NoError(t, err)

			var result map[string]any
			json.Unmarshal(entry.Value(), &result)
			value := result["value"].(float64)
			t.Logf("Between revisions: target=%v, got value=%v", targetTime, value)
			assert.Equal(t, float64(10), value, "Should get revision 10")
		}
	})

	t.Run("TimeWindow", func(t *testing.T) {
		// Get actual history
		history, err := bucket.History(ctx, entityID)
		require.NoError(t, err)

		if len(history) > 30 {
			// Find revisions in a window
			startTime := history[20].Created()
			endTime := history[30].Created()

			entries, err := resolver.GetInTimeRange(ctx, entityID, startTime, endTime)
			require.NoError(t, err)

			t.Logf("Time window: found %d entries between indices 20-30", len(entries))
			// Should get entries 20-30 (inclusive)
			assert.GreaterOrEqual(t, len(entries), 10)
			assert.LessOrEqual(t, len(entries), 11)

			// Verify entries are within the window
			for _, entry := range entries {
				assert.False(t, entry.Created().Before(startTime))
				assert.False(t, entry.Created().After(endTime))
			}
		}
	})
}

func TestTemporalResolver_Performance(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	defer tc.Terminate()
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "perf_test",
		History: 64,
	})
	require.NoError(t, err)

	// Create maximum history
	entityID := "perf.entity"
	for i := 0; i < 64; i++ {
		data := map[string]any{"value": i}
		bytes, _ := json.Marshal(data)
		_, err := bucket.Put(ctx, entityID, bytes)
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}

	resolver := NewTemporalResolver(ctx, bucket)

	// Benchmark binary search
	targetTime := time.Now().Add(-30 * time.Minute)

	start := time.Now()
	iterations := 1000
	for i := 0; i < iterations; i++ {
		_, err := resolver.GetAtTimestamp(ctx, entityID, targetTime)
		require.NoError(t, err)
	}
	duration := time.Since(start)

	avgTime := duration / time.Duration(iterations)
	t.Logf("Binary search average time: %v per query", avgTime)

	// Should be very fast even with network overhead
	assert.Less(t, avgTime, 10*time.Millisecond,
		"Binary search should be fast")
}

func TestTemporalResolver_RangeQueries(t *testing.T) {
	tc := NewTestClient(t, WithKV())
	defer tc.Terminate()
	ctx := context.Background()

	js, err := tc.Client.JetStream()
	require.NoError(t, err)

	bucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  "range_test",
		History: 64,
	})
	require.NoError(t, err)

	// Create multiple entities
	entities := []string{"entity.1", "entity.2", "entity.3"}
	for _, entityID := range entities {
		for i := 0; i < 10; i++ {
			data := map[string]any{
				"id":    entityID,
				"value": i,
			}
			bytes, _ := json.Marshal(data)
			_, err := bucket.Put(ctx, entityID, bytes)
			require.NoError(t, err)
			time.Sleep(10 * time.Millisecond)
		}
	}

	resolver := NewTemporalResolver(ctx, bucket)

	t.Run("GetRangeAtTimestamp", func(t *testing.T) {
		targetTime := time.Now().Add(-5 * time.Second)
		results, err := resolver.GetRangeAtTimestamp(ctx, entities, targetTime)
		require.NoError(t, err)

		// Should get all three entities
		assert.Len(t, results, 3)
		for _, entityID := range entities {
			assert.Contains(t, results, entityID)
		}
	})
}
