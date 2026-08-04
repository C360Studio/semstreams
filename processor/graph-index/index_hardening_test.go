package graphindex

// Tests for Layer 1 of the graph-index-hardening change (gh#474, design.md).
// Covers tasks 4.1–4.4: key round-trip, load linearity, cutover inertness,
// graph-index reader parity (handleQueryIncomingNATS), entity-delete cleanup,
// and the NAME production-wire query test. The graph/query `GetIncomingEdges`
// bugfix is covered separately in graph/query (incoming_shard_integration_test.go),
// since that reader lives in a different package on a real KV bucket.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 4.1 Key build/parse round-trips + prefix isolation
// ============================================================================

func TestIncomingIndex_KeyRoundTrip(t *testing.T) {
	targetID := "acme.ops.robotics.gcs.mission.001"
	sourceID := "acme.ops.robotics.gcs.drone.001"
	predicate := "robotics.assigned.mission"

	key := incomingIndexKey(targetID, sourceID, predicate)
	got, ok := incomingEntryFromKey(key, targetID)
	require.True(t, ok, "should parse the key we just built")
	assert.Equal(t, sourceID, got.FromEntityID)
	assert.Equal(t, predicate, got.Predicate)
}

func TestIncomingIndex_NoncanonicalPredicateRejectedOnDecode(t *testing.T) {
	targetID := "acme.ops.robotics.gcs.mission.001"
	sourceID := "acme.ops.robotics.gcs.drone.001"
	predicate := "robot.arm.joint.angle.current" // predicate-audit:invalid {"kind":"stored-predicate","value":"robot.arm.joint.angle.current","reason":"arity"}

	key := incomingIndexKey(targetID, sourceID, predicate)
	_, ok := incomingEntryFromKey(key, targetID)
	assert.False(t, ok, "stored noncanonical predicates must be poison rows, not query results")
}

func TestIncomingIndex_SiblingEntityIDsIsolated(t *testing.T) {
	// Two sibling entity IDs that share a token prefix ("mission.001" vs "mission.002").
	// A prefix scan for target1 must NOT return target2's keys.
	target1 := "acme.ops.robotics.gcs.mission.001"
	target2 := "acme.ops.robotics.gcs.mission.002"
	source := "acme.ops.robotics.gcs.drone.001"
	pred := "assigned.mission"

	key1 := incomingIndexKey(target1, source, pred)
	key2 := incomingIndexKey(target2, source, pred)

	// key2 must not parse as belonging to target1.
	_, ok := incomingEntryFromKey(key2, target1)
	assert.False(t, ok, "target2's key must not parse under target1's prefix")

	// And vice versa.
	_, ok = incomingEntryFromKey(key1, target2)
	assert.False(t, ok)
}

func TestIncomingIndex_MalformedIDSkipped(t *testing.T) {
	// A key whose suffix does not have enough tokens to reconstruct a 6-part
	// sourceID must be skipped by incomingEntryFromKey (returns ok=false).
	targetID := "acme.ops.robotics.gcs.mission.001"
	malformedKey := targetID + ".only-3-tokens"

	_, ok := incomingEntryFromKey(malformedKey, targetID)
	assert.False(t, ok, "malformed key (too few tokens for a valid sourceID) must be skipped")
}

func TestIncomingIndex_EmptyPredicateRejected(t *testing.T) {
	// validateIncomingKeyInputs must reject an empty predicate.
	logger := slog.Default()
	ok := validateIncomingKeyInputs(
		"acme.ops.robotics.gcs.mission.001",
		"acme.ops.robotics.gcs.drone.001",
		"", // empty predicate
		logger,
	)
	assert.False(t, ok, "empty predicate must be rejected by validateIncomingKeyInputs")
}

func TestIncomingIndex_InvalidEntityIDRejected(t *testing.T) {
	// validateIncomingKeyInputs must reject entity IDs that are not 6-part federated IDs.
	logger := slog.Default()

	// Invalid targetID (not 6 parts)
	ok := validateIncomingKeyInputs("not-a-valid-id", "acme.ops.robotics.gcs.drone.001", "pred", logger)
	assert.False(t, ok, "invalid targetID must be rejected")

	// Invalid sourceID (only 2 parts)
	ok = validateIncomingKeyInputs("acme.ops.robotics.gcs.mission.001", "too.short", "pred", logger)
	assert.False(t, ok, "invalid sourceID must be rejected")
}

// NAME index key tests

func TestNameIndex_HashCollisionFree(t *testing.T) {
	// Two names that share a string prefix but differ as a whole must hash differently,
	// so they never interfere with each other's prefix scans.
	h1 := nameIndexKey("alpha")
	h2 := nameIndexKey("alphabeta")
	assert.NotEqual(t, h1, h2)
}

func TestNameIndex_CompositeKeyRoundTrip(t *testing.T) {
	name := "DroneAlpha"
	entityID := "acme.ops.robotics.gcs.drone.001"
	predicate := "dc.terms.title"

	nameHash := nameIndexKey(name)
	key := nameCompositeKey(nameHash, entityID, predicate)
	gotEntityID, gotPred, ok := nameEntryFromKey(key, nameHash)
	require.True(t, ok)
	assert.Equal(t, entityID, gotEntityID)
	assert.Equal(t, predicate, gotPred)
}

func TestNameIndex_EmptyPredicateRejected(t *testing.T) {
	// A composite key with an empty predicate segment must not parse successfully
	// because that key is ambiguous — the predicate encodes semantic meaning.
	nameHash := nameIndexKey("SomeName")
	entityID := "acme.ops.robotics.gcs.drone.001"
	key := nameCompositeKey(nameHash, entityID, "")
	_, _, ok := nameEntryFromKey(key, nameHash)
	assert.False(t, ok, "composite key with empty predicate must not parse successfully")
}

func TestNameIndex_WriteGuardRejectsMalformedKeyInputs(t *testing.T) {
	// validateNameKeyInputs (the WRITE-side structural guard, symmetric with
	// validateIncomingKeyInputs) must skip a malformed entity ID or empty
	// predicate so no key the reader would reject/mis-split is ever stored.
	logger := slog.Default()
	valid := "acme.ops.robotics.gcs.drone.001"

	assert.True(t, validateNameKeyInputs(valid, "test.value.predicate", logger), "valid inputs pass")
	assert.False(t, validateNameKeyInputs("not-a-valid-id", "test.value.predicate", logger), "invalid entity ID rejected")
	assert.False(t, validateNameKeyInputs("too.short", "test.value.predicate", logger), "non-6-token entity ID rejected")
	assert.False(t, validateNameKeyInputs(valid, "", logger), "empty predicate rejected")
}

// ============================================================================
// 4.2 Load test — O(N) writes, not O(N²)
// ============================================================================

// TestIncomingIndex_HubDimensionWritesAreLinear proves that a hub entity receiving
// N incoming edges results in exactly N KV Put calls. With the old CAS list-merge
// approach every new edge triggered O(in-degree) reads and O(1) overwrites — the
// full update sequence was O(N²) Puts. With composite-key sharding each edge gets
// its own key and a single unconditional Put: total writes = N.
func TestIncomingIndex_HubDimensionWritesAreLinear(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	targetID := "acme.ops.robotics.gcs.mission.hub" // valid 6-part entity ID
	const N = 50

	// Intercept Put calls on the incoming bucket to count them.
	var putCount int64
	mock := incomingMock(comp)
	mock.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		atomic.AddInt64(&putCount, 1)
		mock.mu.Lock()
		mock.data[key] = value
		mock.mu.Unlock()
		return 1, nil
	}

	for i := 0; i < N; i++ {
		sourceID := fmt.Sprintf("acme.ops.robotics.gcs.drone.%03d", i)
		require.NoError(t, comp.UpdateIncomingIndex(ctx, targetID, sourceID, "robotics.assigned.mission"))
	}

	// With composite-key sharding: one Put per edge, total = N (not N*(N+1)/2).
	assert.EqualValues(t, N, putCount,
		"incoming index writes must be O(N): one unconditional Put per edge")

	// Read back: a prefix scan of targetID.">" must enumerate all N edges.
	keys, err := comp.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	assert.Len(t, keys, N, "prefix scan must return all N edge keys")
}

// ============================================================================
// 4.3 Cutover inertness + reader parity + entity-delete
// ============================================================================

// TestIncomingIndex_OldMonolithicKeyInert verifies that a pre-migration key written
// at the bare entityID (old format: JSON array at targetID) is NOT returned by the
// new composite-key prefix scan (design.md D5). Old data is silently inert.
func TestIncomingIndex_OldMonolithicKeyInert(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	targetID := "acme.ops.robotics.gcs.mission.001"

	// Simulate a pre-migration write: bare entityID key with JSON array value.
	oldValue, _ := json.Marshal([]graph.IncomingEntry{
		{FromEntityID: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.assigned.mission"},
	})
	mock := incomingMock(comp)
	mock.mu.Lock()
	mock.data[targetID] = oldValue // bare key, NOT composite
	mock.mu.Unlock()

	// Prefix scan must not match the bare key — it has no "." suffix token.
	keys, err := comp.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	assert.Empty(t, keys,
		"old monolithic key at bare targetID must be inert under the composite-key prefix scan")
}

// TestIncomingIndex_ReaderParity drives handleQueryIncomingNATS end-to-end to verify
// that the production query handler correctly reconstructs IncomingEntry values from
// composite keys (and that the old broken single-Get path is gone).
func TestIncomingIndex_ReaderParity(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	targetID := "acme.ops.robotics.gcs.mission.001"
	type edge struct {
		id   string
		pred string
	}
	edges := []edge{
		{"acme.ops.robotics.gcs.drone.001", "robotics.assigned.mission"},
		{"acme.ops.robotics.gcs.drone.002", "robotics.backup.mission"},
	}

	for _, e := range edges {
		require.NoError(t, comp.UpdateIncomingIndex(ctx, targetID, e.id, e.pred))
	}

	// Drive through the production NATS handler — NOT through the raw bucket.
	reqData, _ := json.Marshal(map[string]string{"entity_id": targetID})
	respData, err := comp.handleQueryIncomingNATS(ctx, reqData)
	require.NoError(t, err)

	var resp graph.QueryResponse[graph.IncomingRelationshipsData]
	require.NoError(t, json.Unmarshal(respData, &resp))

	require.Len(t, resp.Data.Relationships, 2, "both incoming edges must be returned")
	byFrom := make(map[string]string)
	for _, rel := range resp.Data.Relationships {
		byFrom[rel.FromEntityID] = rel.Predicate
	}
	for _, e := range edges {
		assert.Equal(t, e.pred, byFrom[e.id],
			"edge from %s must carry predicate %s", e.id, e.pred)
	}
}

// TestEntityDelete_PreservesLiveSourceIncomingKeyset verifies that deleting a target
// does not erase assertions owned by sources that still point at it.
func TestEntityDelete_PreservesLiveSourceIncomingKeyset(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	targetID := "acme.ops.robotics.gcs.mission.001"
	type edge struct {
		id   string
		pred string
	}
	edges := []edge{
		{"acme.ops.robotics.gcs.drone.001", "robotics.assigned.mission"},
		{"acme.ops.robotics.gcs.drone.002", "robotics.backup.mission"},
		{"acme.ops.robotics.gcs.drone.003", "robotics.assigned.mission"},
	}

	for _, e := range edges {
		require.NoError(t, comp.UpdateIncomingIndex(ctx, targetID, e.id, e.pred))
	}

	// Confirm all three keys exist before delete.
	keys, err := comp.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	assert.Len(t, keys, 3, "should have 3 incoming edge keys before delete")

	// Delete the target entity.
	require.NoError(t, comp.DeleteFromIndexes(ctx, targetID))

	// All source-owned assertions under the retired target prefix must survive.
	keysAfter, err := comp.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	assert.Len(t, keysAfter, 3, "target retirement must preserve live-source assertions")

	// Re-put one existing edge remains idempotent.
	require.NoError(t, comp.UpdateIncomingIndex(ctx, targetID, edges[0].id, edges[0].pred))
	keysReAdded, err := comp.incomingBucket.KeysByPrefix(ctx, incomingIndexPrefix(targetID))
	require.NoError(t, err)
	assert.Len(t, keysReAdded, 3, "idempotent re-put must not duplicate a source assertion")
}

// ============================================================================
// 4.4 NAME production-wire integration test
// ============================================================================

// TestNameIndex_ProductionWireByName drives the full write→query pipeline for
// NAME_INDEX through the production handleQueryByNameNATS handler. It verifies that
// the reconstructed NameMatch fields {EntityID, MatchedName, Predicate, ExactCase}
// are correct after the composite-key format migration, and that ranking
// (exact-case first, then salience, then entity ID tiebreak) is preserved.
func TestNameIndex_ProductionWireByName(t *testing.T) {
	comp := newNameTestComponent(t)
	ctx := context.Background()

	// Three entities carrying the same normalized name under different predicates
	// and priorities. One re-index of drone.001 verifies idempotent Put.
	require.NoError(t, comp.UpdateNameIndex(ctx, "DroneAlpha", "acme.ops.robotics.gcs.drone.001", "dc.terms.title", 1))
	require.NoError(t, comp.UpdateNameIndex(ctx, "dronealpha", "acme.ops.robotics.gcs.drone.002", "skos.core.pref-label", 0))
	require.NoError(t, comp.UpdateNameIndex(ctx, "DRONEALPHA", "acme.ops.robotics.gcs.drone.003", "dc.terms.title", 1))
	// Re-index drone.001 — idempotent Put must not create a duplicate entry.
	require.NoError(t, comp.UpdateNameIndex(ctx, "DroneAlpha", "acme.ops.robotics.gcs.drone.001", "dc.terms.title", 1))

	matches := queryByName(t, comp, "DroneAlpha", 0)
	require.Len(t, matches, 3, "three distinct entities must be returned")

	byID := make(map[string]graph.NameMatch)
	for _, m := range matches {
		byID[m.EntityID] = m
	}

	// Verify wire fields are correctly reconstructed from composite key + value.
	m1 := byID["acme.ops.robotics.gcs.drone.001"]
	assert.Equal(t, "DroneAlpha", m1.MatchedName,
		"original-case name must be preserved in the composite-key value")
	assert.Equal(t, "dc.terms.title", m1.Predicate,
		"predicate must be reconstructed from the composite key")
	assert.True(t, m1.ExactCase, "drone.001 carries the exact query string — must be flagged exact-case")

	m2 := byID["acme.ops.robotics.gcs.drone.002"]
	assert.Equal(t, "skos.core.pref-label", m2.Predicate)
	assert.False(t, m2.ExactCase, "case-folded-only match must not be flagged exact-case")

	m3 := byID["acme.ops.robotics.gcs.drone.003"]
	assert.Equal(t, "DRONEALPHA", m3.MatchedName)
	assert.False(t, m3.ExactCase)

	// Ranking: exact-case (drone.001) first; then highest salience (priority=0,
	// drone.002) over lower salience (priority=1); tiebreak by entity ID asc.
	assert.Equal(t, "acme.ops.robotics.gcs.drone.001", matches[0].EntityID, "exact-case ranks first")
	assert.Equal(t, "acme.ops.robotics.gcs.drone.002", matches[1].EntityID, "higher salience (priority=0) ranks second")
	assert.Equal(t, "acme.ops.robotics.gcs.drone.003", matches[2].EntityID)
}

// ============================================================================
// No-op projection instrumentation (D6)
// ============================================================================

// TestProjectionInstrumentation_IdenticalProjectionCountedUnchanged verifies that
// when processEntityUpdateFromData is called twice with the same entity state, the
// second call increments reindexUnchanged (the no-op counter) while all writes
// still proceed (OBSERVE ONLY — the instrumentation never skips writes).
func TestProjectionInstrumentation_IdenticalProjectionCountedUnchanged(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	entityID := "acme.ops.robotics.gcs.drone.001"
	// Use a literal (non-relationship, non-name) triple so no additional index
	// family is involved in the instrumentation assertion.
	state := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "robotics.battery.level", Object: 85.0},
		},
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)

	// First call — projection is new, unchanged counter must stay at 0.
	comp.processEntityUpdateFromData(ctx, entityID, data)
	assert.EqualValues(t, 1, atomic.LoadInt64(&comp.reindexTotal))
	assert.EqualValues(t, 0, atomic.LoadInt64(&comp.reindexUnchanged))

	// Second call with identical data — projection matches → unchanged counter increments.
	comp.processEntityUpdateFromData(ctx, entityID, data)
	assert.EqualValues(t, 2, atomic.LoadInt64(&comp.reindexTotal))
	assert.EqualValues(t, 1, atomic.LoadInt64(&comp.reindexUnchanged))
}

// TestProjectionInstrumentation_ChangedProjectionNotCounted verifies that when the
// entity state changes between calls, reindexUnchanged does NOT increment.
func TestProjectionInstrumentation_ChangedProjectionNotCounted(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	entityID := "acme.ops.robotics.gcs.drone.001"
	state1 := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "robotics.battery.level", Object: 85.0},
		},
	}
	state2 := graph.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: "robotics.battery.level", Object: 85.0},
			{Subject: entityID, Predicate: "robotics.altitude.meters", Object: 120.0},
		},
	}
	data1, _ := json.Marshal(state1)
	data2, _ := json.Marshal(state2)

	comp.processEntityUpdateFromData(ctx, entityID, data1)
	comp.processEntityUpdateFromData(ctx, entityID, data2) // different projection

	assert.EqualValues(t, 2, atomic.LoadInt64(&comp.reindexTotal))
	assert.EqualValues(t, 0, atomic.LoadInt64(&comp.reindexUnchanged),
		"changed projection must not increment the unchanged counter")
}
