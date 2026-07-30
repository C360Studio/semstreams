//go:build integration

// gh#713 regression: a graph-ingest restart against an existing graph must not
// append already-present triples or advance entity revisions when nothing in
// the world changed.
//
// The trigger is NOT in hierarchy.go. createEntity calls GetHierarchyTriples
// UNCONDITIONALLY (component.go, before the KV write), and that call is not a
// pure read: it commits container-inverse and sibling-inverse edges as side
// effects through tripleAdder. On an already-present ID the subsequent atomic
// Create returns natsclient.ErrKVKeyExists and createEntity returns early —
// but the inverse edges have ALREADY committed. MergeEntity, by contrast,
// gates hierarchy behind an absence probe, which is why the fact lane does not
// re-fire and the request lane does.
//
// A test that only re-submits triples through add_batch never reaches that
// trigger, so this test replays through CreateEntityStrict — the same 409 path
// a re-registering producer takes.

package graphingest

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/vocabulary"
)

// replayEntityIDs are three same-type siblings, the shape gh#713 measured on
// the model registry (containers 6->9, endpoints 4->6 / 3->5 / 2->4).
var replayEntityIDs = []string{
	"c360.platform.robotics.mav1.drone.replay001",
	"c360.platform.robotics.mav1.drone.replay002",
	"c360.platform.robotics.mav1.drone.replay003",
}

// replayContainerIDs are the three containers hierarchy inference materialises
// for that type, each of which accumulates an inverse `contains` edge per child.
var replayContainerIDs = []string{
	"c360.platform.robotics.mav1.drone.group",
	"c360.platform.robotics.mav1.group.container",
	"c360.platform.robotics.group.container.level",
}

// replayEntity builds the registration payload for one entity. A fresh struct
// per call is required: createEntity APPENDS hierarchy triples onto the
// candidate it is handed, so reusing one would make the second registration a
// different request.
func replayEntity(id string) *graph.EntityState {
	return &graph.EntityState{
		ID: id,
		Triples: []message.Triple{{
			Subject:   id,
			Predicate: "entity.type.class",
			Object:    "robotics.drone",
			Timestamp: time.Now(),
		}},
		Version:   1,
		UpdatedAt: time.Now(),
	}
}

// entitySnapshot is everything a replay must leave untouched: the KV revision,
// the entity version, and the exact multiset of stored triple identities.
type entitySnapshot struct {
	revision uint64
	version  uint64
	triples  []string
}

func snapshotStore(t *testing.T, ctx context.Context, comp *Component) map[string]entitySnapshot {
	t.Helper()
	keys, err := comp.entityBucket.Keys(ctx)
	require.NoError(t, err)

	snapshot := make(map[string]entitySnapshot, len(keys))
	for _, key := range keys {
		entry, getErr := comp.entityBucket.Get(ctx, key)
		require.NoError(t, getErr)
		var stored graph.EntityState
		require.NoError(t, graph.UnmarshalEntityState(entry.Value, &stored))

		identities := make([]string, 0, len(stored.Triples))
		for i := range stored.Triples {
			identities = append(identities, message.AppendIdentityKey(stored.Triples[i]))
		}
		sort.Strings(identities)
		snapshot[key] = entitySnapshot{
			revision: entry.Revision,
			version:  stored.Version,
			triples:  identities,
		}
	}
	return snapshot
}

func countStoredPredicate(t *testing.T, ctx context.Context, comp *Component, id, predicate string) int {
	t.Helper()
	entry, err := comp.entityBucket.Get(ctx, id)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(entry.Value, &stored))
	count := 0
	for _, triple := range stored.Triples {
		if triple.Predicate == predicate {
			count++
		}
	}
	return count
}

// Tasks 7.1 / 7.3 / 6.3.
func TestComponent_HierarchyReplay_UnchangedEntitiesAdvanceNoRevision(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// ---- First boot: seed the graph. -------------------------------------
	seed := createHierarchyComponentOnClient(t, testClient.Client, true)
	require.NoError(t, seed.Initialize())
	require.NoError(t, seed.Start(ctx))
	for _, id := range replayEntityIDs {
		require.NoError(t, seed.CreateEntityStrict(ctx, replayEntity(id)))
	}
	require.NoError(t, seed.Stop(5*time.Second))

	// ---- Restart: a FRESH component over the SAME store. -----------------
	// Initialize + Start is the production startup path; initStorage re-acquires
	// ENTITY_STATES through the catalog seam exactly as a new process would.
	replay := createHierarchyComponentOnClient(t, testClient.Client, true)
	require.NoError(t, replay.Initialize())
	require.NoError(t, replay.Start(ctx))
	defer func() { _ = replay.Stop(5 * time.Second) }()

	// Nothing is writing now (the seed component is stopped, the replay
	// component has not been asked to do anything), so this is a quiescent read.
	before := snapshotStore(t, ctx, replay)

	// Seed sanity — WITHOUT these the comparison below could pass vacuously on
	// an empty or degenerate store. These are the counts gh#713's arithmetic
	// depends on: 3 entities + 3 containers; the type container holds one
	// inverse `contains` edge per child; each entity holds one sibling edge per
	// OTHER entity.
	require.Len(t, before, len(replayEntityIDs)+len(replayContainerIDs),
		"seed must have produced 3 entities and 3 containers")
	require.Equal(t, 3, countStoredPredicate(t, ctx, replay, replayContainerIDs[0], vocabulary.HierarchyTypeContains),
		"the type container must hold one inverse contains edge per child")
	for _, id := range replayEntityIDs {
		require.Equal(t, 2, countStoredPredicate(t, ctx, replay, id, vocabulary.HierarchyTypeSibling),
			"%s must hold one sibling edge per other entity", id)
	}

	suppressedBefore := testutil.ToFloat64(
		replay.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy)))

	// ---- Replay the UNCHANGED entities through the 409 path. -------------
	for _, id := range replayEntityIDs {
		err := replay.CreateEntityStrict(ctx, replayEntity(id))
		require.ErrorIs(t, err, natsclient.ErrKVKeyExists,
			"%s already exists, so its re-registration must be a 409 — that is the trigger", id)
	}

	// ---- Nothing may have moved. ----------------------------------------
	after := snapshotStore(t, ctx, replay)

	require.Len(t, after, len(before), "replay must not create keys")
	compared := 0
	for key, want := range before {
		got, ok := after[key]
		require.True(t, ok, "%s disappeared across the replay", key)
		assert.Equal(t, want.revision, got.revision,
			"%s: KV revision advanced on a replay with no source change", key)
		assert.Equal(t, want.version, got.version, "%s: entity version advanced", key)
		assert.Equal(t, want.triples, got.triples,
			"%s: stored triple cardinality changed across the replay", key)
		compared++
	}
	require.Equal(t, len(replayEntityIDs)+len(replayContainerIDs), compared,
		"every seeded key must have been compared — a skipped comparison proves nothing")

	// Task 6.3: a suppression that is invisible is indistinguishable from a lane
	// that never ran. Each re-registration re-derives 3 container-inverse edges
	// (type, system, domain) and 2 sibling-inverse edges (one per other
	// entity) = 5 suppressed tuples per entity, 15 across the three.
	suppressedAfter := testutil.ToFloat64(
		replay.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy)))
	assert.InDelta(t, suppressedBefore+15, suppressedAfter, 0.0001,
		"the hierarchy lane's suppressed duplicates must be observable and attributable")
}
