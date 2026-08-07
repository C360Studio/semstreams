package graphindextemporal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEventTimeTestComponent builds a Component wired to in-memory mock buckets
// for exercising the indexing logic directly (no NATS).
func newEventTimeTestComponent() (*Component, *mockKVBucket, *mockKVBucket) {
	tb := newMockKVBucket()
	rb := newMockKVBucket()
	c := &Component{
		name:           "graph-index-temporal",
		config:         Config{TimeResolution: "hour"},
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		temporalBucket: tb,
		reverseBucket:  rb,
		metrics:        getMetrics(nil),
	}
	c.lastActivity.Store(time.Now())
	return c, tb, rb
}

func entityEntry(t *testing.T, state graph.EntityState) jetstream.KeyValueEntry {
	t.Helper()
	data, err := json.Marshal(state)
	require.NoError(t, err)
	return &mockKVEntry{data: data}
}

// bucketEntities returns the entity IDs present in a temporal bucket key, or nil
// if the bucket key does not exist.
func bucketEntities(t *testing.T, tb *mockKVBucket, key string) []string {
	t.Helper()
	tb.mu.Lock()
	defer tb.mu.Unlock()
	raw, ok := tb.data[key]
	if !ok {
		return nil
	}
	var d struct {
		Events []struct {
			Entity string `json:"entity"`
		} `json:"events"`
	}
	require.NoError(t, json.Unmarshal(raw, &d))
	ids := make([]string, 0, len(d.Events))
	for _, e := range d.Events {
		ids = append(ids, e.Entity)
	}
	return ids
}

func observedTriple(entityID, ts string) message.Triple {
	return message.Triple{Subject: entityID, Predicate: predicateObservationRecorded, Object: ts, Source: "test"}
}

// ---------------------------------------------------------------------------
// resolveIndexTimestamp precedence
// ---------------------------------------------------------------------------

func TestResolveIndexTimestamp_ObservedTakesPrecedence(t *testing.T) {
	observed := time.Date(2026, 1, 7, 14, 30, 0, 0, time.UTC)
	updated := time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC)
	state := graph.EntityState{
		ID:        "acme.ops.robotics.gcs.drone.001",
		Triples:   []message.Triple{observedTriple("acme.ops.robotics.gcs.drone.001", observed.Format(time.RFC3339))},
		UpdatedAt: updated,
	}

	ts, source, ok := resolveIndexTimestamp(state)
	require.True(t, ok)
	assert.Equal(t, indexSourceObserved, source)
	assert.True(t, observed.Equal(ts), "expected observed time %v, got %v", observed, ts)
}

func TestResolveIndexTimestamp_FallsBackToUpdatedAt(t *testing.T) {
	updated := time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC)
	state := graph.EntityState{
		ID:        "acme.ops.robotics.gcs.drone.001",
		Triples:   []message.Triple{{Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.status.armed", Object: true}},
		UpdatedAt: updated,
	}

	ts, source, ok := resolveIndexTimestamp(state)
	require.True(t, ok)
	assert.Equal(t, indexSourceWriteFallback, source)
	assert.True(t, updated.Equal(ts))
}

func TestResolveIndexTimestamp_MultipleObserved_LatestWins(t *testing.T) {
	early := time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 1, 7, 18, 0, 0, 0, time.UTC)
	id := "acme.ops.robotics.gcs.drone.001"
	state := graph.EntityState{
		ID: id,
		// Deliberately out of order to prove latest-wins, not first/last-match.
		Triples: []message.Triple{
			observedTriple(id, late.Format(time.RFC3339)),
			observedTriple(id, early.Format(time.RFC3339)),
		},
		UpdatedAt: time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
	}

	ts, source, ok := resolveIndexTimestamp(state)
	require.True(t, ok)
	assert.Equal(t, indexSourceObserved, source)
	assert.True(t, late.Equal(ts), "expected latest observed %v, got %v", late, ts)
}

func TestResolveIndexTimestamp_NoTimestamp_NotIndexable(t *testing.T) {
	state := graph.EntityState{
		ID:      "acme.ops.robotics.gcs.drone.001",
		Triples: []message.Triple{{Subject: "x", Predicate: "robotics.status.armed", Object: true}},
		// UpdatedAt zero, no observation predicate.
	}
	_, _, ok := resolveIndexTimestamp(state)
	assert.False(t, ok)
}

func TestResolveIndexTimestamp_UnparseableObserved_FallsBack(t *testing.T) {
	updated := time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC)
	id := "acme.ops.robotics.gcs.drone.001"
	state := graph.EntityState{
		ID:        id,
		Triples:   []message.Triple{observedTriple(id, "not-a-timestamp")},
		UpdatedAt: updated,
	}
	ts, source, ok := resolveIndexTimestamp(state)
	require.True(t, ok)
	assert.Equal(t, indexSourceWriteFallback, source)
	assert.True(t, updated.Equal(ts))
}

// ---------------------------------------------------------------------------
// processEntityUpdate: bucketing + cleanup
// ---------------------------------------------------------------------------

func TestProcessEntityUpdate_IndexesByObservedTime_NotWriteTime(t *testing.T) {
	c, tb, rb := newEventTimeTestComponent()
	ctx := context.Background()

	id := "acme.ops.robotics.gcs.drone.001"
	observedBucket := "2026.01.07.14"
	writeBucket := "2026.01.08.09"

	state := graph.EntityState{
		ID:        id,
		Triples:   []message.Triple{observedTriple(id, "2026-01-07T14:30:00Z")},
		UpdatedAt: time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC),
	}
	c.processEntityUpdate(ctx, entityEntry(t, state))

	assert.Contains(t, bucketEntities(t, tb, observedBucket), id, "entity should be in the OBSERVED-time bucket")
	assert.Nil(t, bucketEntities(t, tb, writeBucket), "entity must NOT be in the write-time bucket")

	rev, _ := rb.Get(ctx, id)
	require.NotNil(t, rev)
	assert.Equal(t, observedBucket, string(rev.Value()), "reverse index should point at the observed bucket")
}

func TestProcessEntityUpdate_ReindexMovesEntity_NoStaleEntry(t *testing.T) {
	c, tb, _ := newEventTimeTestComponent()
	ctx := context.Background()
	id := "acme.ops.robotics.gcs.drone.001"

	first := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T14:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, first))
	require.Contains(t, bucketEntities(t, tb, "2026.01.07.14"), id)

	// Re-observe later — entity must move buckets and leave no stale entry behind.
	second := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T16:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, second))

	assert.Contains(t, bucketEntities(t, tb, "2026.01.07.16"), id, "entity should be in the new bucket")
	assert.NotContains(t, bucketEntities(t, tb, "2026.01.07.14"), id, "entity must NOT linger in the prior bucket")
}

func TestProcessEntityUpdate_SameBucketReindex_Idempotent(t *testing.T) {
	c, tb, _ := newEventTimeTestComponent()
	ctx := context.Background()
	id := "acme.ops.robotics.gcs.drone.001"

	state := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T14:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, state))
	c.processEntityUpdate(ctx, entityEntry(t, state)) // e.g. restart WatchAll re-delivery

	got := bucketEntities(t, tb, "2026.01.07.14")
	count := 0
	for _, e := range got {
		if e == id {
			count++
		}
	}
	assert.Equal(t, 1, count, "re-indexing to the same bucket must not duplicate the entity event")
}

func TestProcessEntityUpdate_DistinctEntitiesShareBucket(t *testing.T) {
	c, tb, _ := newEventTimeTestComponent()
	ctx := context.Background()

	for _, id := range []string{"a.b.c.d.e.1", "a.b.c.d.e.2", "a.b.c.d.e.3"} {
		state := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T14:05:00Z")}}
		c.processEntityUpdate(ctx, entityEntry(t, state))
	}
	got := bucketEntities(t, tb, "2026.01.07.14")
	assert.ElementsMatch(t, []string{"a.b.c.d.e.1", "a.b.c.d.e.2", "a.b.c.d.e.3"}, got)
}

func TestProcessEntityUpdate_AddFailure_DoesNotRemoveFromPriorBucket(t *testing.T) {
	c, tb, _ := newEventTimeTestComponent()
	ctx := context.Background()
	id := "acme.ops.robotics.gcs.drone.001"

	// Index at T1 -> bucket "2026.01.07.14".
	first := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T14:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, first))
	require.Contains(t, bucketEntities(t, tb, "2026.01.07.14"), id)

	// Ensure the destination bucket exists (so the write takes the Update path),
	// then make every write fail.
	tb.mu.Lock()
	tb.data["2026.01.07.16"] = []byte(`{"events":[{"entity":"other.e.1","type":"update","timestamp":"2026-01-07T16:00:00Z"}],"entity_count":1}`)
	tb.mu.Unlock()
	tb.putFunc = func(_ context.Context, _ string, _ []byte) (uint64, error) {
		return 0, errors.New("injected write failure")
	}

	// Re-observe at T2 — the move's add fails; the prior-bucket removal must not run.
	second := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T16:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, second))

	tb.putFunc = nil
	assert.Contains(t, bucketEntities(t, tb, "2026.01.07.14"), id,
		"on add failure the entity must remain queryable in its prior bucket (no remove-before-add)")
}

func TestEntityTombstone_RemovesFromBucketAndReverse(t *testing.T) {
	for _, op := range []jetstream.KeyValueOp{jetstream.KeyValueDelete, jetstream.KeyValuePurge} {
		t.Run(op.String(), func(t *testing.T) {
			c, tb, rb := newEventTimeTestComponent()
			ctx := context.Background()
			id := "acme.ops.robotics.gcs.drone.001"

			state := graph.EntityState{ID: id, Triples: []message.Triple{observedTriple(id, "2026-01-07T14:30:00Z")}}
			c.processEntityUpdate(ctx, entityEntry(t, state))
			require.Contains(t, bucketEntities(t, tb, "2026.01.07.14"), id)

			c.applyEntityWatchEntry(ctx, &bootstrapEntry{key: id, revision: 8, op: op})

			assert.NotContains(t, bucketEntities(t, tb, "2026.01.07.14"), id,
				"%s entity must be gone from its bucket", op)
			_, err := rb.Get(ctx, id)
			assert.ErrorIs(t, err, jetstream.ErrKeyNotFound, "%s reverse entry should be removed", op)
		})
	}
}

// ---------------------------------------------------------------------------
// metric split
// ---------------------------------------------------------------------------

func TestProcessEntityUpdate_MetricSplit_ObservedVsFallback(t *testing.T) {
	c, _, _ := newEventTimeTestComponent()
	ctx := context.Background()

	observedBefore := testutil.ToFloat64(c.metrics.indexed.WithLabelValues(indexSourceObserved))
	fallbackBefore := testutil.ToFloat64(c.metrics.indexed.WithLabelValues(indexSourceWriteFallback))

	withObserved := graph.EntityState{ID: "a.b.c.d.e.1", Triples: []message.Triple{observedTriple("a.b.c.d.e.1", "2026-01-07T14:30:00Z")}}
	c.processEntityUpdate(ctx, entityEntry(t, withObserved))

	withoutObserved := graph.EntityState{
		ID:        "a.b.c.d.e.2",
		Triples:   []message.Triple{{Subject: "a.b.c.d.e.2", Predicate: "robotics.status.armed", Object: true}},
		UpdatedAt: time.Date(2026, 1, 8, 9, 0, 0, 0, time.UTC),
	}
	c.processEntityUpdate(ctx, entityEntry(t, withoutObserved))

	assert.Equal(t, observedBefore+1, testutil.ToFloat64(c.metrics.indexed.WithLabelValues(indexSourceObserved)))
	assert.Equal(t, fallbackBefore+1, testutil.ToFloat64(c.metrics.indexed.WithLabelValues(indexSourceWriteFallback)))
}
