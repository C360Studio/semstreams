//go:build integration

package graphingest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// TestIntegration_CacheStaleRepopulationRace is the deterministic regression
// lock for the read-through-cache stale-repopulation race in the entity query
// cache (30s TTL).
//
// The bug: the cache-miss read path did KV Get -> unmarshal -> Set with no guard
// against a concurrent invalidate landing between the Get and the Set. A reader
// that loaded rev1 from KV, then was pre-empted while a concurrent write
// committed rev2 and invalidated the (already-absent) cache entry, would Set rev1
// back into the cache AFTER the invalidate — resurrecting pre-write state for the
// full TTL and violating read-after-write coherence (the gated-DAG dispatch stall
// was one victim).
//
// The fix establishes a per-key invalidation-generation guard: the repopulating
// Set is dropped if the key was invalidated during the read window. This test
// drives that window deterministically with explicit channels (no sleeps): it
// pauses the reader between its KV Get (rev1) and its Set via a test hook,
// commits rev2 (marker) while paused, then releases the reader. Without the guard
// the reader's stale Set wins and the final read observes rev1 (no marker);
// with the guard the Set is skipped and the final read observes rev2.
func TestIntegration_CacheStaleRepopulationRace(t *testing.T) {
	ctx := context.Background()
	c, _ := startPrefixTestComponent(t)

	const id = "stalecache.ops.dom.sys.type.entity1"

	// rev1: create the entity WITHOUT the marker. CreateEntity invalidates the
	// cache, so the first read below is a guaranteed cache miss.
	seedPrefixEntity(t, ctx, c, id)

	// Coordinate the racing read and write with explicit channels (no sleeps).
	readerAtHook := make(chan struct{}) // reader paused post-Get, pre-Set
	releaseReader := make(chan struct{})
	var once sync.Once
	c.repopulateHook = func(hookID string) {
		if hookID != id {
			return
		}
		// Only the first read for id blocks; the final verification read (which
		// also passes through the hook) is a no-op and does not block.
		once.Do(func() {
			close(readerAtHook)
			<-releaseReader
		})
	}

	// Reader: misses the cache, reads rev1 from KV, then blocks in the hook
	// between the KV Get and the repopulating Set.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		_, _, _ = c.fetchEntitiesConcurrent(ctx, []string{id}, 1)
	}()

	// Wait until the reader has read rev1 and is paused before its Set.
	<-readerAtHook

	// rev2: commit the marker while the reader is paused. AddTriple commits a new
	// revision AND invalidates the cache (bumps the coherence generation) — this
	// is the concurrent write W2 the paused reader's rev1 read did not see.
	_, _, err := c.addTripleLane(ctx, message.Triple{
		Subject:    id,
		Predicate:  semantictest.Predicate(t, "test", "stale", "marker"),
		Object:     "committed",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}, dedupLaneAddBatch)
	require.NoError(t, err)

	// Release the paused reader so it attempts its (now stale) repopulating Set.
	close(releaseReader)
	<-readerDone

	// A fresh read MUST observe rev2 (the marker), never a resurrected rev1.
	// Without the guard the paused reader's Set writes rev1 back into the cache
	// after AddTriple's invalidate, so this read hits stale state and fails.
	got, _, err := c.fetchEntitiesConcurrent(ctx, []string{id}, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].GetTriple(semantictest.Predicate(t, "test", "stale", "marker")),
		"fresh read must observe the rev2 marker — a slow reader must not resurrect "+
			"pre-write rev1 into the cache past a concurrent invalidate")
}
