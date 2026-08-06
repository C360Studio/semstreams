package graphingest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func appendDedupTriple(ctx context.Context, t *testing.T, comp *Component, triple message.Triple) {
	t.Helper()
	_, _, err := comp.addTripleLane(ctx, triple, dedupLaneAddBatch)
	require.NoError(t, err)
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

	appendDedupTriple(ctx, t, comp, triple)
	afterFirst, revAfterFirst := readDedupEntity(t, comp, dedupSubject)
	require.Equal(t, 1, countDedupTriples(afterFirst, triple.Predicate))

	errorsBefore := atomic.LoadInt64(&comp.errors)
	suppressedBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAddBatch)))

	appendDedupTriple(ctx, t, comp, triple)

	afterSecond, revAfterSecond := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revAfterFirst, revAfterSecond, "no KV revision may advance on a duplicate add")
	assert.Equal(t, afterFirst.Version, afterSecond.Version, "entity version must be unchanged")
	assert.True(t, afterFirst.UpdatedAt.Equal(afterSecond.UpdatedAt), "UpdatedAt must be unchanged")
	assert.Equal(t, 1, countDedupTriples(afterSecond, triple.Predicate), "exactly one copy stored")
	assert.Equal(t, errorsBefore, atomic.LoadInt64(&comp.errors),
		"suppression must not increment the component error counter")
	assert.InDelta(t, suppressedBefore+1,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAddBatch))), 0.0001,
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
			appendDedupTriple(ctx, t, comp, first)
			_, revAfterFirst := readDedupEntity(t, comp, subject)

			second := dedupTriple(subject)
			tt.mutate(&second)
			appendDedupTriple(ctx, t, comp, second)

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
	appendDedupTriple(ctx, t, comp, first)
	_, revAfterFirst := readDedupEntity(t, comp, dedupSubject)

	second := dedupTriple(dedupSubject)
	second.Object = "c360.platform.robotics.mav1.drone.003"
	appendDedupTriple(ctx, t, comp, second)

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
			_, _, errsOut[index] = comp.addTripleLane(ctx, triple, dedupLaneAddBatch)
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
	appendDedupTriple(ctx, t, comp, first)
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
	appendDedupTriple(ctx, t, comp, present)

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
	appendDedupTriple(ctx, t, comp, triple)

	adder := &tripleAdderAdapter{component: comp}
	hierarchyBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy)))
	addBefore := testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAddBatch)))

	require.NoError(t, adder.AddTriple(ctx, triple))

	assert.InDelta(t, hierarchyBefore+1,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneHierarchy))), 0.0001,
		"hierarchy inference's in-process adder must be attributable to its own lane")
	assert.InDelta(t, addBefore,
		testutil.ToFloat64(comp.duplicateTriplesSuppressed.WithLabelValues(string(dedupLaneAddBatch))), 0.0001,
		"and must not be charged to the operator-mutation lane")
}

// The canonical append outcome is the wire-level dedup discriminator. A real
// append is applied; a duplicate-only append is unchanged and carries the
// current revision without advancing it.
func TestHandleCanonicalAppend_ReportsAppliedThenUnchanged(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)
	triple.Predicate = "robotics.battery.level"
	triple.Object = 85.5

	requestData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{triple}})
	require.NoError(t, err)

	firstBody, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)
	var first graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(firstBody, &first))
	require.Len(t, first.Results, 1)
	assert.Equal(t, graph.MutationApplied, first.Results[0].Outcome)
	require.NotZero(t, first.Results[0].KVRevision)

	secondBody, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err, "a duplicate add must be a success reply, not a typed error")
	var second graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(secondBody, &second))
	require.Len(t, second.Results, 1)
	assert.Equal(t, graph.MutationUnchanged, second.Results[0].Outcome)
	assert.Equal(t, first.Results[0].KVRevision, second.Results[0].KVRevision,
		"a suppressed write reports the live UNCHANGED revision, never zero")
}

func TestHandleCanonicalAppend_CollapsesWithinRequestDuplicates(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	triple := dedupTriple(dedupSubject)

	requestData, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{triple, triple}})
	require.NoError(t, err)

	firstBody, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)
	var first graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(firstBody, &first))
	require.Len(t, first.Results, 1)
	assert.Equal(t, graph.MutationApplied, first.Results[0].Outcome)
	require.NotZero(t, first.Results[0].KVRevision)
	stored, _ := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, 1, countDedupTriples(stored, triple.Predicate), "the within-request repeat collapses")

	secondBody, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)
	var second graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(secondBody, &second))
	require.Len(t, second.Results, 1)
	assert.Equal(t, graph.MutationUnchanged, second.Results[0].Outcome)
	assert.Equal(t, first.Results[0].KVRevision, second.Results[0].KVRevision,
		"a suppressed batch reports the live UNCHANGED revision, never zero")
}

func TestHandleCanonicalAppend_MultiSubjectReportsIndependentRevisions(t *testing.T) {
	comp, _ := seedDedupEntity(t, dedupSubject)
	ctx := context.Background()
	const otherSubject = "c360.platform.robotics.mav1.drone.007"
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID: otherSubject, Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now(),
	}))

	first := dedupTriple(dedupSubject)
	second := dedupTriple(otherSubject)
	requestData, err := json.Marshal(graph.AppendTriplesRequest{
		Triples: []message.Triple{first, second},
	})
	require.NoError(t, err)

	body, err := comp.handleCanonicalAppend(ctx, requestData)
	require.NoError(t, err)
	var resp graph.AppendTriplesResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Results, 2)
	for _, result := range resp.Results {
		assert.Equal(t, graph.MutationApplied, result.Outcome)
		assert.NotZero(t, result.KVRevision)
		assert.Nil(t, result.Error)
	}
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

	appendDedupTriple(ctx, t, comp, triple)

	after, revAfter := readDedupEntity(t, comp, dedupSubject)
	assert.Equal(t, revBefore, revAfter)
	assert.Equal(t, 2, countDedupTriples(after, triple.Predicate),
		"no backfill: the duplicates stay, and nothing is added")
}
