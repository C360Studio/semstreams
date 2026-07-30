package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

const absentSubjectID = "c360.platform.robotics.mav1.drone.absent01"

// Codex C1. "Degraded" means A WRITE COMMITTED but its post-write read-back
// failed. A FAILED mutation and a NO-OP are neither, and marking them degraded
// is actively dangerous: pkg/projection's AppendEvidence checks
// response.Degraded BEFORE it looks at FailedSubjects, so a degraded-flagged
// failure enters committed verification and can be reported as committed.
//
// An absent entity produces exactly that shape when the revision read-back is
// issued unconditionally — the append writes nothing AND the read-back cannot
// find the entity.
func TestHandleTripleAddBatch_AbsentEntityIsFailedNotDegraded(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	triple := dedupTriple(absentSubjectID)
	requestData, err := json.Marshal(graph.AddTriplesBatchRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)

	body, err := comp.handleTripleAddBatch(ctx, requestData)
	require.NoError(t, err, "an absent subject is a partial-failure body, not a typed error")

	var resp graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	require.Contains(t, resp.FailedSubjects, absentSubjectID,
		"fixture sanity: the absent subject must be reported as failed")
	assert.False(t, resp.Degraded,
		"a subject that FAILED did not commit, so it must not be flagged degraded — "+
			"the client checks Degraded before FailedSubjects and would treat it as committed")
	assert.Empty(t, resp.DegradedReason)
	assert.Zero(t, resp.KVRevision, "nothing committed, so there is no revision to report")
	assert.Zero(t, resp.WrittenCount)
}

func TestHandleTripleRemove_AbsentEntityIsNoOpNotDegraded(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	requestData, err := json.Marshal(graph.RemoveTripleRequest{
		Subject: absentSubjectID, Predicate: "robotics.battery.level",
	})
	require.NoError(t, err)

	body, err := comp.handleTripleRemove(ctx, requestData)
	require.NoError(t, err, "removing from an absent entity is an idempotent no-op success")

	var resp graph.RemoveTripleResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	assert.False(t, resp.Removed, "nothing was removed")
	assert.False(t, resp.Degraded,
		"a no-op committed nothing, so there is no committed write whose echo could be degraded")
	assert.Empty(t, resp.DegradedReason)
	assert.Zero(t, resp.KVRevision)
}

// A SUPPRESSED add is also a no-op: it must report the entity's live revision
// (so read-your-writes still works) but must never be degraded.
func TestHandleTripleAdd_SuppressedIsNeverDegraded(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	requestData, err := json.Marshal(graph.AddTripleRequest{Triple: triple})
	require.NoError(t, err)
	_, err = comp.handleTripleAdd(ctx, requestData)
	require.NoError(t, err)

	body, err := comp.handleTripleAdd(ctx, requestData)
	require.NoError(t, err)
	var resp graph.AddTripleResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	require.True(t, resp.Deduplicated, "fixture sanity: the second add must be suppressed")
	assert.False(t, resp.Degraded, "a suppressed write committed nothing")
	assert.NotZero(t, resp.KVRevision, "but it still reports the live revision for read-your-writes")
}

// Codex C2. revisionAfterMutation issued an INDEPENDENT live Get after the CAS,
// so a writer committing in the window between them made the handler report
// THAT writer's revision. Because our own mutation genuinely committed, the
// rule engine's tracker records the later revision as its own and shouldSkipRule
// then consumes it — silently dropping the external change.
//
// The external commit is injected deterministically on the read-back Get, which
// is exactly the window in question. No sleeps.
func TestHandleTripleAdd_ReportsItsOwnCASRevisionNotALaterWriters(t *testing.T) {
	comp, bucket := createTestComponentWithMockKVBucket(t)
	ctx := context.Background()
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID: dedupSubject, Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now(),
	}))

	triple := dedupTriple(dedupSubject)
	requestData, err := json.Marshal(graph.AddTripleRequest{Triple: triple})
	require.NoError(t, err)

	// One Get belongs to the CAS itself. Any Get AFTER that is a post-hoc
	// re-read, and the hook makes an external writer win that window.
	probe := injectExternalCommitOnReadback(t, comp, bucket, 1)

	body, err := comp.handleTripleAdd(ctx, requestData)
	// Disarm before any verification read, or this test's own Get would trip
	// the injection and move the revision it is about to compare against.
	probe.disarm()
	require.NoError(t, err)
	var resp graph.AddTripleResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	assert.Zero(t, probe.postCASGets(),
		"the committed path must not re-read the revision: a re-read is exactly the window "+
			"in which another writer commits and the handler returns THAT writer's revision")
	if external := probe.externalRevision(); external != 0 {
		assert.NotEqual(t, external, resp.KVRevision,
			"the handler returned the later external writer's revision")
	}
	assert.NotZero(t, resp.KVRevision, "a committed write must report its own revision")
	_, liveRevision := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, liveRevision, resp.KVRevision,
		"with no external writer, the reported revision is the one this CAS produced")
}

func TestHandleTripleRemove_ReportsItsOwnCASRevisionNotALaterWriters(t *testing.T) {
	comp, bucket := createTestComponentWithMockKVBucket(t)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID: dedupSubject, Triples: []message.Triple{triple}, Version: 1, UpdatedAt: time.Now(),
	}))

	requestData, err := json.Marshal(graph.RemoveTripleRequest{
		Subject: dedupSubject, Predicate: triple.Predicate,
	})
	require.NoError(t, err)

	// RemoveTriple does an existence Get, then the CAS Get. Anything after is a
	// post-hoc re-read.
	probe := injectExternalCommitOnReadback(t, comp, bucket, 2)

	body, err := comp.handleTripleRemove(ctx, requestData)
	probe.disarm() // see the add-lane test: disarm before the verification read
	require.NoError(t, err)
	var resp graph.RemoveTripleResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	require.True(t, resp.Removed, "fixture sanity: the removal must have committed")
	assert.Zero(t, probe.postCASGets(),
		"the committed path must not re-read the revision")
	if external := probe.externalRevision(); external != 0 {
		assert.NotEqual(t, external, resp.KVRevision,
			"the handler returned the later external writer's revision")
	}
	assert.NotZero(t, resp.KVRevision)
	_, liveRevision := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, liveRevision, resp.KVRevision,
		"with no external writer, the reported revision is the one this CAS produced")
}

// readbackProbe reports what happened at the post-CAS seam.
type readbackProbe struct {
	gets             *int
	casGets          int
	externalRevision func() uint64
	disarm           func()
}

// postCASGets is how many Get calls the handler made AFTER its CAS — i.e. how
// many post-hoc re-reads it performed. Zero is the contract.
func (p readbackProbe) postCASGets() int {
	extra := *p.gets - p.casGets
	if extra < 0 {
		return 0
	}
	return extra
}

// injectExternalCommitOnReadback makes an external writer WIN the window
// between the handler's own CAS and any post-hoc revision re-read — the window
// Codex C2 names. casGets is how many Get calls the handler legitimately makes
// for the CAS itself; the hook fires on the first Get beyond that.
//
// Under a handler that re-reads, the hook fires, an external write lands, and
// the handler observes the external writer's revision. Under a handler that
// returns its own CAS revision there is no post-CAS Get at all, the hook never
// fires, and postCASGets stays zero — which is itself the assertion.
//
// Deterministic injection at the exact seam, no sleeps.
func injectExternalCommitOnReadback(t *testing.T, comp *Component, bucket *mockKVBucket, casGets int) readbackProbe {
	t.Helper()
	var externalRevision uint64
	gets := 0
	bucket.getFunc = func(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
		gets++
		if gets != casGets+1 {
			return mockGetPassthrough(bucket, key)
		}
		// Another writer commits, right here in the window. Clearing the hook
		// first keeps this write (and everything after) on the normal path.
		bucket.getFunc = nil
		external := dedupTriple(key)
		external.Object = "c360.platform.robotics.mav1.drone.external"
		if err := comp.AddTriple(ctx, external); err != nil {
			t.Errorf("external write failed: %v", err)
		}
		entry, err := bucket.Get(ctx, key)
		if err == nil {
			externalRevision = entry.Revision()
		}
		return entry, err
	}
	t.Cleanup(func() { bucket.getFunc = nil })
	return readbackProbe{
		gets:             &gets,
		casGets:          casGets,
		externalRevision: func() uint64 { return externalRevision },
		disarm:           func() { bucket.getFunc = nil },
	}
}

// structObject has fields whose DECLARATION order (Zebra, Alpha) differs from
// their sorted-key order (Alpha, Zebra). encoding/json emits a struct in
// declaration order but a map in sorted-key order, so this value and the
// map[string]any it decodes back into from ENTITY_STATES produce different JSON
// unless the identity key normalizes.
type structObject struct {
	Zebra string `json:"zebra"`
	Alpha int    `json:"alpha"`
}

// Codex C3. A struct-valued Object keyed differently from its persisted
// map[string]any form, so replaying the same valid in-process triple appended
// it again on EVERY restart — advancing revisions and refiring watchers, which
// is precisely the corruption this change exists to eliminate. "Fails safe" was
// the wrong lens: a missed suppression is the failure the requirement forbids.
func TestAddLane_StructuredObjectReplayIsSuppressed(t *testing.T) {
	structured := func(subject string) message.Triple {
		tr := dedupTriple(subject)
		tr.Predicate = "robotics.payload.descriptor"
		tr.Object = structObject{Zebra: "z", Alpha: 1}
		return tr
	}

	t.Run("single add lane", func(t *testing.T) {
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()

		require.NoError(t, comp.AddTriple(ctx, structured(dedupSubject)))
		first, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
		require.Equal(t, 1, countDedupTriples(first, "robotics.payload.descriptor"),
			"fixture sanity: the structured triple must have been stored")

		// The replay: the identical in-process value, submitted again against
		// the stored map[string]any form.
		require.NoError(t, comp.AddTriple(ctx, structured(dedupSubject)))

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed structured object must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.descriptor"),
			"cardinality must be unchanged across the replay")
	})

	t.Run("large-int scalar, single add lane", func(t *testing.T) {
		// int64 above float64's exact range: the raw value and its persisted
		// form encode differently unless the key normalizes, so a replay would
		// re-append on every restart.
		bigInt := func(subject string) message.Triple {
			tr := dedupTriple(subject)
			tr.Predicate = "robotics.payload.sequence"
			tr.Object = int64(9007199254740993)
			return tr
		}
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()

		require.NoError(t, comp.AddTriple(ctx, bigInt(dedupSubject)))
		first, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
		require.Equal(t, 1, countDedupTriples(first, "robotics.payload.sequence"),
			"fixture sanity: the triple must have been stored")

		require.NoError(t, comp.AddTriple(ctx, bigInt(dedupSubject)))

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed large-int scalar must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.sequence"),
			"cardinality must be unchanged across the replay")
	})

	t.Run("batch add lane", func(t *testing.T) {
		comp, _ := seedDedupEntity(t, dedupSubject)
		ctx := context.Background()
		batch := []message.Triple{structured(dedupSubject)}

		result, err := comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)
		require.NoError(t, err)
		require.Equal(t, 1, result.Written, "fixture sanity: the first batch must commit")
		_, revAfterFirst := readDedupEntity(t, comp, dedupSubject)

		result, err = comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)
		require.NoError(t, err)
		assert.Empty(t, result.FailedSubjects)
		assert.Equal(t, 0, result.Written, "the replayed batch must write nothing")
		assert.Equal(t, 1, result.Deduplicated, "and must report the tuple as already present")

		after, revAfter := readDedupEntity(t, comp, dedupSubject)
		assert.Equal(t, revAfterFirst, revAfter,
			"a replayed structured object must not advance the revision")
		assert.Equal(t, 1, countDedupTriples(after, "robotics.payload.descriptor"),
			"cardinality must be unchanged across the replay")
	})
}
