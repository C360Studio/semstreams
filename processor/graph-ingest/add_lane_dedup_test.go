package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
)

// Add-lane deduplication (gh#697 / gh#713). These drive the PRODUCTION
// KVStore.UpdateWithRetry wrap chain over a CAS-capable mock bucket, so the
// sentinel recovery, the revision arithmetic, and the response shape are all
// exercised the way the NATS handlers exercise them.

const dedupSubject = "c360.platform.robotics.mav1.drone.001"

func dedupTriple(subject string) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  "hierarchy.type.contains",
		Object:     "c360.platform.robotics.mav1.drone.002",
		Source:     "",
		Context:    "inference.hierarchy",
		Confidence: 1.0,
	}
}

// seedDedupEntity creates an empty entity and returns the component, its
// bucket, and the entity's revision after creation.
func seedDedupEntity(t *testing.T, id string) (*Component, uint64) {
	t.Helper()
	comp := createTestComponentWithMockKV(t)
	entity := &graph.EntityState{ID: id, Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now()}
	require.NoError(t, comp.CreateEntity(context.Background(), entity))
	entry, err := comp.entityBucket.Get(context.Background(), id)
	require.NoError(t, err)
	return comp, entry.Revision
}

func readDedupEntity(t *testing.T, comp *Component, id string) (graph.EntityState, uint64) {
	t.Helper()
	entry, err := comp.entityBucket.Get(context.Background(), id)
	require.NoError(t, err)
	var stored graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(entry.Value, &stored))
	return stored, entry.Revision
}

func countDedupTriples(stored graph.EntityState, predicate string) int {
	count := 0
	for _, triple := range stored.Triples {
		if triple.Predicate == predicate {
			count++
		}
	}
	return count
}

// Task 2.3: errors.Is must reach the sentinel through
// retry.NonRetryable(fmt.Errorf("update function error: %w", err)) — if it did
// not, every suppressed write would surface as a CAS failure instead of a
// silent success.
func TestErrNoOpAddDuplicate_SurvivesUpdateWithRetryWrapChain(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)

	err := comp.entityBucket.UpdateWithRetry(context.Background(), dedupSubject,
		func(_ []byte) ([]byte, error) { return nil, errNoOpAddDuplicate })

	require.Error(t, err)
	assert.ErrorIs(t, err, errNoOpAddDuplicate,
		"the sentinel must survive UpdateWithRetry's NonRetryable + fmt.Errorf wrap chain")
	assert.True(t, retry.IsNonRetryable(err), "and must stop the CAS loop rather than retry")
	assert.False(t, natsclient.IsKVConflictError(err),
		"the sentinel message must not trip IsKVConflictError's substring sniffing")
}

// Task 3.1 + the zero-write requirement: a duplicate add commits NOTHING.
func TestAddTriple_DuplicateSuppressed_NoRevisionAdvance(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	require.NoError(t, comp.AddTriple(ctx, triple))
	afterFirst, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
	require.Equal(t, 1, countDedupTriples(afterFirst, triple.Predicate))

	errorsBefore := atomic.LoadInt64(&comp.errors)
	suppressedBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAdd)))

	require.NoError(t, comp.AddTriple(ctx, triple), "a duplicate add is a success, not a failure")

	afterSecond, revAfterSecond := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revAfterFirst, revAfterSecond, "no KV revision may advance on a duplicate add")
	assert.Equal(t, afterFirst.Version, afterSecond.Version, "entity version must be unchanged")
	assert.True(t, afterFirst.UpdatedAt.Equal(afterSecond.UpdatedAt), "UpdatedAt must be unchanged")
	assert.Equal(t, 1, countDedupTriples(afterSecond, triple.Predicate), "exactly one copy stored")
	assert.Equal(t, errorsBefore, atomic.LoadInt64(&comp.errors),
		"suppression must not increment the component error counter")
	assert.InDelta(t, suppressedBefore+1,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAdd))), 0.0001,
		"the suppression must be observable on the add lane")
}

// Task 1.5 at the lane level: the three excluded fields must not defeat
// suppression, because every real producer varies at least Timestamp.
func TestAddTriple_ExcludedFieldsDoNotDefeatSuppression(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	tests := []struct {
		name   string
		mutate func(*message.Triple)
	}{
		{"restamped timestamp", func(tr *message.Triple) { tr.Timestamp = time.Now().Add(time.Minute) }},
		{"different confidence", func(tr *message.Triple) { tr.Confidence = 0.25 }},
		{"newly set expiry", func(tr *message.Triple) { tr.ExpiresAt = &expiry }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject := dedupSubject
			comp, _ := seedDedupEntity(t, subject)
			ctx := context.Background()

			first := dedupTriple(subject)
			first.Timestamp = time.Now()
			require.NoError(t, comp.AddTriple(ctx, first))
			_, revAfterFirst := readDedupEntity(t, comp, subject)

			second := dedupTriple(subject)
			tt.mutate(&second)
			require.NoError(t, comp.AddTriple(ctx, second))

			stored, rev := readDedupEntity(t, comp, subject)
			assert.Equal(t, revAfterFirst, rev, "%s must not make it a distinct assertion", tt.name)
			assert.Equal(t, 1, countDedupTriples(stored, first.Predicate))
		})
	}
}

// A differing SIX-field member is a distinct assertion and must still append —
// the add lane stays append-only for multi-valued predicates.
func TestAddTriple_DistinctTupleStillAppends(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()

	first := dedupTriple(dedupSubject)
	require.NoError(t, comp.AddTriple(ctx, first))
	_, revAfterFirst := readDedupEntity(t, comp, dedupSubject)

	second := dedupTriple(dedupSubject)
	second.Object = "c360.platform.robotics.mav1.drone.003"
	require.NoError(t, comp.AddTriple(ctx, second))

	stored, rev := readDedupEntity(t, comp, dedupSubject)
	assert.Greater(t, rev, revAfterFirst, "a new tuple must advance the revision")
	assert.Equal(t, 2, countDedupTriples(stored, first.Predicate),
		"two distinct objects under one predicate must both survive")
}

// Spec: "two concurrent identical appends store one tuple". The loser's
// revision-checked Update conflicts, UpdateWithRetry re-runs the closure
// against the winner's committed state, and the retry suppresses. A pre-read
// outside the CAS loop would let both observe the tuple absent and both append.
func TestAddTriple_ConcurrentIdenticalAppendsStoreOneTuple(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	const writers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errsOut := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start // explicit synchronization, no sleeps
			errsOut[index] = comp.AddTriple(ctx, triple)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errsOut {
		assert.NoError(t, err, "writer %d must report success", i)
	}
	stored, _ := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, 1, countDedupTriples(stored, triple.Predicate),
		"%d concurrent identical appends must store exactly one copy", writers)
}

// Task 4.1/4.3: a fully-duplicate batch is success with nothing written and no
// failed subjects — a previously impossible response state.
func TestAddTriples_FullyDuplicateBatchReportsZeroWrittenAndNoFailures(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()

	batch := []message.Triple{dedupTriple(dedupSubject), dedupTriple(dedupSubject)}
	batch[1].Object = "c360.platform.robotics.mav1.drone.003"

	result, err := comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)
	require.NoError(t, err)
	require.Equal(t, 2, result.Written)
	require.Equal(t, 0, result.Deduplicated)
	_, revAfterFirst := readDedupEntity(t, comp, dedupSubject)

	errorsBefore := atomic.LoadInt64(&comp.errors)
	result, err = comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)

	require.NoError(t, err, "a wholly duplicate batch is not an error")
	assert.Equal(t, 0, result.Written, "WrittenCount counts only NEW tuples")
	assert.Equal(t, 2, result.Deduplicated, "written + deduplicated must account for the whole request")
	assert.Empty(t, result.FailedSubjects, "a suppressed subject must never appear among failed subjects")
	assert.NotContains(t, result.CommittedRevisions, dedupSubject,
		"a wholly suppressed subject committed nothing, so it must not appear as committed")
	assert.Equal(t, errorsBefore, atomic.LoadInt64(&comp.errors))

	stored, rev := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revAfterFirst, rev, "no revision may advance")
	assert.Equal(t, 2, countDedupTriples(stored, batch[0].Predicate))
}

// Task 4.1: a partially duplicate batch commits exactly the new tuples and
// advances the revision exactly once.
func TestAddTriples_PartialDuplicateCommitsOnlyTheNewTuples(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()

	first := dedupTriple(dedupSubject)
	require.NoError(t, comp.AddTriple(ctx, first))
	_, revBefore := readDedupEntity(t, comp, dedupSubject)

	second := dedupTriple(dedupSubject)
	second.Object = "c360.platform.robotics.mav1.drone.004"

	result, err := comp.addTriplesLane(ctx,
		[]message.Triple{first, second}, dedupLaneAddBatch)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Written)
	assert.Equal(t, 1, result.Deduplicated)
	assert.Empty(t, result.FailedSubjects)

	stored, rev := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revBefore+1, rev, "the revision must advance exactly once")
	assert.Equal(t, rev, result.CommittedRevisions[dedupSubject],
		"the lane must report the exact revision its own CAS produced")
	assert.Equal(t, 2, countDedupTriples(stored, first.Predicate))
}

// Task 2.2: repeats WITHIN one request collapse, preserving first-input order.
func TestAddTriples_WithinRequestDuplicatesCollapsePreservingOrder(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()

	repeated := dedupTriple(dedupSubject)
	distinctA := dedupTriple(dedupSubject)
	distinctA.Object = "c360.platform.robotics.mav1.drone.00a"
	distinctB := dedupTriple(dedupSubject)
	distinctB.Object = "c360.platform.robotics.mav1.drone.00b"

	batch := []message.Triple{repeated, distinctA, repeated, distinctB, repeated}
	result, err := comp.addTriplesLane(ctx, batch, dedupLaneAddBatch)

	require.NoError(t, err)
	assert.Equal(t, 3, result.Written)
	assert.Equal(t, 2, result.Deduplicated)
	assert.Empty(t, result.FailedSubjects)

	stored, _ := readDedupEntity(t, comp, dedupSubject)
	require.Equal(t, 3, countDedupTriples(stored, repeated.Predicate),
		"one request commits at most one copy of a tuple")
	order := make([]string, 0, 3)
	for _, triple := range stored.Triples {
		if triple.Predicate == repeated.Predicate {
			order = append(order, fmt.Sprint(triple.Object))
		}
	}
	assert.Equal(t,
		[]string{fmt.Sprint(repeated.Object), fmt.Sprint(distinctA.Object), fmt.Sprint(distinctB.Object)},
		order, "survivor order must follow first appearance in the request")
}

// Task 3.2: a suppression counted into allAbsences misclassifies a mixed batch.
// Here subject A is wholly duplicate (suppressed, NOT a failure) and subject B
// is absent (a genuine ADR-055 rejection). The aggregated error must still
// carry ErrKVKeyNotFound so the handler replies entity_not_found; if the
// suppression were treated as a failure, allAbsences would flip false and the
// classification would be lost.
func TestAddTriples_SuppressionDoesNotMisclassifyMixedBatch(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()

	present := dedupTriple(dedupSubject)
	require.NoError(t, comp.AddTriple(ctx, present))

	const absentSubject = "c360.platform.robotics.mav1.drone.999"
	absent := dedupTriple(absentSubject)

	result, err := comp.addTriplesLane(ctx,
		[]message.Triple{present, absent}, dedupLaneAddBatch)

	require.Error(t, err)
	assert.ErrorIs(t, err, natsclient.ErrKVKeyNotFound,
		"an all-absence failure set must stay classifiable as entity_not_found")
	assert.Equal(t, 0, result.Written)
	assert.Equal(t, 1, result.Deduplicated)
	require.Len(t, result.FailedSubjects, 1, "only the absent subject failed")
	assert.Contains(t, result.FailedSubjects, absentSubject)
	assert.NotContains(t, result.FailedSubjects, dedupSubject, "a suppressed subject is not a failed subject")
}

// Task 6.1/6.2: the counter is labelled by a CLOSED lane enum, and the
// hierarchy lane is attributable separately from operator mutations.
func TestSuppressedDuplicateCounter_IsLaneAttributed(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)
	require.NoError(t, comp.AddTriple(ctx, triple))

	adder := &tripleAdderAdapter{component: comp}
	hierarchyBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy)))
	addBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAdd)))

	require.NoError(t, adder.AddTriple(ctx, triple))

	assert.InDelta(t, hierarchyBefore+1,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy))), 0.0001,
		"hierarchy inference's in-process adder must be attributable to its own lane")
	assert.InDelta(t, addBefore,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAdd))), 0.0001,
		"and must not be charged to the operator-mutation lane")
}

// Task 4.2/4.3 on the wire: the handler echoes Deduplicated and still reports
// the entity's LIVE revision, never zero, on a suppressed write.
func TestHandleTripleAdd_ReportsDeduplicatedAndLiveRevision(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)
	triple.Predicate = "robotics.battery.level"
	triple.Object = 85.5

	requestData, err := json.Marshal(graph.AddTripleRequest{Triple: triple})
	require.NoError(t, err)

	firstBody, err := comp.handleTripleAdd(ctx, requestData)
	require.NoError(t, err)
	var first graph.AddTripleResponse
	require.NoError(t, json.Unmarshal(firstBody, &first))
	assert.False(t, first.Deduplicated, "the first add is a real write")
	require.NotZero(t, first.KVRevision)

	secondBody, err := comp.handleTripleAdd(ctx, requestData)
	require.NoError(t, err, "a duplicate add must be a success reply, not a typed error")
	var second graph.AddTripleResponse
	require.NoError(t, json.Unmarshal(secondBody, &second))
	assert.True(t, second.Deduplicated, "the reply must say the tuple was already present")
	assert.Equal(t, first.KVRevision, second.KVRevision,
		"a suppressed write reports the live UNCHANGED revision, never zero")
}

func TestHandleTripleAddBatch_ReportsDeduplicatedAndLiveRevision(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	requestData, err := json.Marshal(graph.AddTriplesBatchRequest{Triples: []message.Triple{triple, triple}})
	require.NoError(t, err)

	firstBody, err := comp.handleTripleAddBatch(ctx, requestData)
	require.NoError(t, err)
	var first graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(firstBody, &first))
	assert.Equal(t, 1, first.WrittenCount, "the within-request repeat collapses")
	assert.Equal(t, 1, first.Deduplicated)
	require.NotZero(t, first.KVRevision)

	secondBody, err := comp.handleTripleAddBatch(ctx, requestData)
	require.NoError(t, err)
	var second graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(secondBody, &second))
	assert.Equal(t, 0, second.WrittenCount)
	assert.Equal(t, 2, second.Deduplicated,
		"written + deduplicated must equal the submitted count")
	assert.Empty(t, second.FailedSubjects)
	assert.Equal(t, first.KVRevision, second.KVRevision,
		"a suppressed batch reports the live UNCHANGED revision, never zero")
}

// Review H1: RemoveTripleResponse.Removed was hard-coded true, so a caller
// could not tell a committed removal from a no-op — and the revision it
// reports on a no-op is some other writer's.
func TestHandleTripleRemove_ReportsWhetherAnythingWasRemoved(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)
	triple.Predicate = "robotics.battery.level"
	triple.Object = 85.5
	require.NoError(t, comp.AddTriple(ctx, triple))

	requestData, err := json.Marshal(graph.RemoveTripleRequest{
		Subject: dedupSubject, Predicate: triple.Predicate,
	})
	require.NoError(t, err)

	firstBody, err := comp.handleTripleRemove(ctx, requestData)
	require.NoError(t, err)
	var first graph.RemoveTripleResponse
	require.NoError(t, json.Unmarshal(firstBody, &first))
	assert.True(t, first.Removed, "a removal that matched must report removed")
	require.NotZero(t, first.KVRevision)

	secondBody, err := comp.handleTripleRemove(ctx, requestData)
	require.NoError(t, err, "a no-op removal is still a success")
	var second graph.RemoveTripleResponse
	require.NoError(t, json.Unmarshal(secondBody, &second))
	assert.False(t, second.Removed,
		"nothing matched the predicate, so the response must not claim a removal")
	assert.Equal(t, first.KVRevision, second.KVRevision,
		"a no-op reports the live UNCHANGED revision — which is why Removed must be honest")
}

// mockGetPassthrough reproduces mockKVBucket.Get's default behavior so a
// getFunc override can serve the CAS read normally and fail only the
// post-write read-back that follows it.
func mockGetPassthrough(bucket *mockKVBucket, key string) (jetstream.KeyValueEntry, error) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if data, exists := bucket.data[key]; exists {
		return &mockKVEntry{data: data.value, revision: data.revision, key: key}, nil
	}
	return nil, jetstream.ErrKeyNotFound
}

// SUPERSEDED PREMISE, kept deliberately as a guard on what replaced it.
//
// Review M1 required a failed post-write read-back to degrade rather than
// report a bare zero, because the caller's read-your-writes check
// (IndexedRevision >= myRev) is satisfied vacuously by zero. Codex C2 then
// removed the post-write read-back from the COMMITTED path entirely — the
// handler now returns the exact revision its own CAS produced — so on that path
// there is no read to fail and the M1 hazard cannot arise at all.
//
// What survives is the NO-OP path, which still takes a live read because a
// suppressed write has no revision of its own to report. Codex C1 governs it:
// a no-op committed nothing, so a failed live read reports NO revision and is
// NOT degraded. Marking it degraded is the C1 defect, because AppendEvidence
// branches on Degraded before it inspects FailedSubjects.
//
// So this test now pins: committed → real revision, never degraded, whatever
// the bucket does afterwards; no-op with a failing live read → no revision,
// still not degraded.
func TestTripleHandlers_RevisionEchoFailureIsNeverDegraded(t *testing.T) {
	triple := dedupTriple(dedupSubject)
	addData, err := json.Marshal(graph.AddTripleRequest{Triple: triple})
	require.NoError(t, err)
	batchData, err := json.Marshal(graph.AddTriplesBatchRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)
	removeData, err := json.Marshal(graph.RemoveTripleRequest{
		Subject: dedupSubject, Predicate: triple.Predicate,
	})
	require.NoError(t, err)

	tests := []struct {
		name string
		// casGets is how many Get calls the handler makes for the CAS itself.
		casGets int
		// seeded is the entity's starting triples, chosen so the FIRST invoke
		// commits and the SECOND is a no-op: the add lanes start empty (first
		// add appends, second is a duplicate); remove starts with the triple
		// present (first remove commits, second matches nothing).
		seeded []message.Triple
		invoke func(comp *Component, ctx context.Context) ([]byte, error)
	}{
		{
			name:    "triple.add",
			casGets: 1,
			seeded:  []message.Triple{},
			invoke: func(comp *Component, ctx context.Context) ([]byte, error) {
				return comp.handleTripleAdd(ctx, addData)
			},
		},
		{
			name:    "triple.add_batch",
			casGets: 1,
			seeded:  []message.Triple{},
			invoke: func(comp *Component, ctx context.Context) ([]byte, error) {
				return comp.handleTripleAddBatch(ctx, batchData)
			},
		},
		{
			// RemoveTriple does an existence Get before its CAS Get.
			name:    "triple.remove",
			casGets: 2,
			seeded:  []message.Triple{triple},
			invoke: func(comp *Component, ctx context.Context) ([]byte, error) {
				return comp.handleTripleRemove(ctx, removeData)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp, bucket := createTestComponentWithMockKVBucket(t)
			ctx := context.Background()
			require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
				ID: dedupSubject, Triples: tt.seeded, Version: 1, UpdatedAt: time.Now(),
			}))

			// First call COMMITS (add appends a new tuple; remove removes one).
			// Every Get past the CAS fails, proving the committed path does not
			// depend on one: the revision comes from its own CAS.
			failPastCAS := func() {
				gets := 0
				bucket.getFunc = func(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
					gets++
					if gets > tt.casGets {
						return nil, errors.New("simulated post-write read failure")
					}
					return mockGetPassthrough(bucket, key)
				}
			}
			failPastCAS()
			body, handlerErr := tt.invoke(comp, ctx)
			bucket.getFunc = nil
			require.NoError(t, handlerErr)
			var committed graph.MutationResponse
			require.NoError(t, json.Unmarshal(body, &committed))
			assert.NotZero(t, committed.KVRevision,
				"a committed write reports its own CAS revision and needs no read-back")
			assert.False(t, committed.Degraded,
				"nothing was re-read, so nothing could fail; a commit is not degraded")

			// Second call is a NO-OP (the add is now a duplicate; the remove
			// matches nothing). Its live read fails.
			failPastCAS()
			body, handlerErr = tt.invoke(comp, ctx)
			bucket.getFunc = nil
			require.NoError(t, handlerErr, "a no-op is a success")
			var noop graph.MutationResponse
			require.NoError(t, json.Unmarshal(body, &noop))
			assert.False(t, noop.Degraded,
				"a no-op committed nothing, so there is no committed write whose echo could be "+
					"degraded — and a client branches on Degraded before FailedSubjects")
			assert.Empty(t, noop.DegradedReason)
			assert.Zero(t, noop.KVRevision,
				"the live read failed, so report no revision rather than a fabricated one")
		})
	}
}

// A multi-subject batch has no single entity revision. That is UNDEFINED, not
// degraded — reporting nothing is the correct answer.
func TestHandleTripleAddBatch_MultiSubjectReportsNoRevisionAndIsNotDegraded(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	const otherSubject = "c360.platform.robotics.mav1.drone.007"
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID: otherSubject, Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now(),
	}))

	first := dedupTriple(dedupSubject)
	second := dedupTriple(otherSubject)
	requestData, err := json.Marshal(graph.AddTriplesBatchRequest{
		Triples: []message.Triple{first, second},
	})
	require.NoError(t, err)

	body, err := comp.handleTripleAddBatch(ctx, requestData)
	require.NoError(t, err)
	var resp graph.AddTriplesBatchResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, 2, resp.WrittenCount)
	assert.Zero(t, resp.KVRevision, "a batch spanning entities has no single revision")
	assert.False(t, resp.Degraded, "having no single revision is undefined, not degraded")
}

// No-backfill posture: an entity that already accumulated duplicates keeps
// them, reads normally, and still suppresses a fresh re-assert.
func TestAddTriple_PreExistingStoredDuplicatesAreKeptAndStillSuppress(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	// Write the poisoned-by-history shape directly: two identical stored copies.
	entity := &graph.EntityState{
		ID:        dedupSubject,
		Triples:   []message.Triple{triple, triple},
		Version:   1,
		UpdatedAt: time.Now(),
	}
	require.NoError(t, comp.CreateEntity(ctx, entity))
	before, revBefore := readDedupEntity(t, comp, dedupSubject)
	require.Equal(t, 2, countDedupTriples(before, triple.Predicate),
		"the pre-existing duplicates must survive the read unchanged")

	require.NoError(t, comp.AddTriple(ctx, triple))

	after, revAfter := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revBefore, revAfter)
	assert.Equal(t, 2, countDedupTriples(after, triple.Predicate),
		"no backfill: the duplicates stay, and nothing is added")
}
