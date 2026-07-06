package graphingest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- laneGuard (in-memory guard tier, ADR-072) ---

func TestLaneGuard_GetSet(t *testing.T) {
	g := newLaneGuard(8)
	_, ok := g.get("k")
	assert.False(t, ok, "absent key is a miss")

	g.set("k", 5)
	v, ok := g.get("k")
	assert.True(t, ok)
	assert.Equal(t, uint64(5), v)

	g.set("k", 9) // update in place
	v, _ = g.get("k")
	assert.Equal(t, uint64(9), v)
}

func TestLaneGuard_BoundedEviction(t *testing.T) {
	g := newLaneGuard(2)
	g.set("a", 1)
	g.set("b", 2)
	require.Len(t, g.seq, 2)

	// Inserting a third distinct key evicts one arbitrary entry (durable tier
	// backs correctness, so which one is immaterial) — the map stays bounded.
	g.set("c", 3)
	assert.Len(t, g.seq, 2, "map stays bounded at max")
	v, ok := g.get("c")
	assert.True(t, ok, "the just-inserted key is present")
	assert.Equal(t, uint64(3), v)
}

func TestLaneGuard_UpdateAtCapDoesNotEvict(t *testing.T) {
	g := newLaneGuard(2)
	g.set("a", 1)
	g.set("b", 2)
	// Updating an EXISTING key at capacity must not evict (no new entry).
	g.set("a", 10)
	assert.Len(t, g.seq, 2)
	va, _ := g.get("a")
	vb, okb := g.get("b")
	assert.Equal(t, uint64(10), va)
	assert.True(t, okb, "existing sibling not evicted by an in-place update")
	assert.Equal(t, uint64(2), vb)
}

func TestGuardKey(t *testing.T) {
	assert.Equal(t, "c360.ops.robotics.gcs.drone.001/SENSOR",
		guardKey("c360.ops.robotics.gcs.drone.001", "SENSOR"))
}

// --- ingestGuardStale: in-memory tier + nil-durable (no NATS) ---

func TestIngestGuardStale_MemoryTierAndFirstSeen(t *testing.T) {
	c := &Component{
		ingestGuardMem:    []*laneGuard{newLaneGuard(16)},
		ingestGuardBucket: nil, // no durable tier in this unit test
	}
	ctx := context.Background()
	work := ingestWork{entityID: "c360.a.b.c.d.001", stream: "SENSOR", seq: 5}

	// First-seen (tier-1 miss + nil durable) → not stale.
	stale, err := c.ingestGuardStale(ctx, 0, work)
	require.NoError(t, err)
	assert.False(t, stale, "an unseen (entity,stream) is not stale")

	// Stamp the in-memory tier at seq 5, then re-check.
	c.ingestGuardMem[0].set(guardKey(work.entityID, work.stream), 5)

	older := work
	older.seq = 3
	stale, err = c.ingestGuardStale(ctx, 0, older)
	require.NoError(t, err)
	assert.True(t, stale, "a lower sequence than last-applied is a stale redelivery")

	same := work
	same.seq = 5
	stale, err = c.ingestGuardStale(ctx, 0, same)
	require.NoError(t, err)
	assert.True(t, stale, "an equal sequence (redelivery of the applied message) is stale")

	newer := work
	newer.seq = 6
	stale, err = c.ingestGuardStale(ctx, 0, newer)
	require.NoError(t, err)
	assert.False(t, stale, "a higher sequence is a fresh update, not stale")
}

// A different stream for the same entity must NOT be judged against another
// stream's sequence (independent per-stream sequence spaces — round-2 fix).
func TestIngestGuardStale_PerStreamIndependence(t *testing.T) {
	c := &Component{
		ingestGuardMem:    []*laneGuard{newLaneGuard(16)},
		ingestGuardBucket: nil,
	}
	ctx := context.Background()
	entity := "c360.a.b.c.d.001"

	// Stream A applied a high sequence.
	c.ingestGuardMem[0].set(guardKey(entity, "STREAM_A"), 1000)

	// A low sequence from stream B is a DIFFERENT key → not stale.
	fromB := ingestWork{entityID: entity, stream: "STREAM_B", seq: 5}
	stale, err := c.ingestGuardStale(ctx, 0, fromB)
	require.NoError(t, err)
	assert.False(t, stale, "a low seq from stream B is not silenced by stream A's high seq")
}
