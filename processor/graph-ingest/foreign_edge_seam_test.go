package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
)

// fakeClassifier is an injected ownershipClaimReader for unit-testing the
// T2-seam (ADR-056 Decision 4) and the PR-3 owner-lease check without NATS. It
// records what the seam asked and returns a configured unclaimed set, per-predicate
// EdgeMode, and per-predicate OwnerOf results. A predicate absent from `modes` is
// reported UNCLAIMED by ForeignEdgeMode (ok=false), and absent from `owners` is
// reported unclaimed by OwnerOf (ok=false), so the observe-only seam tests (which
// configure only `unclaimed`) keep routing unchanged.
type fakeClassifier struct {
	unclaimed   map[string]bool               // predicate -> reported unclaimed (UnclaimedForeignEdges)
	modes       map[string]ownership.EdgeMode // predicate -> covering mode (ForeignEdgeMode; absent = unclaimed)
	owners      map[string]fakeOwnerEntry     // predicate -> OwnerOf result (absent = ok=false)
	err         error
	gotProducer string
	gotPreds    []string
}

// fakeOwnerEntry is the result fakeClassifier.OwnerOf returns for a predicate.
type fakeOwnerEntry struct {
	owner       string
	incarnation string
	err         error
}

func (f *fakeClassifier) UnclaimedForeignEdges(_ context.Context, producer string, preds []string) ([]string, error) {
	f.gotProducer = producer
	f.gotPreds = preds
	if f.err != nil {
		return nil, f.err
	}
	var out []string
	for _, p := range preds {
		if f.unclaimed[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeClassifier) ForeignEdgeMode(_ context.Context, producer, predicate string) (ownership.EdgeMode, bool, error) {
	f.gotProducer = producer
	if f.err != nil {
		return "", false, f.err
	}
	m, ok := f.modes[predicate]
	return m, ok, nil
}

func (f *fakeClassifier) OwnerOf(_ context.Context, _, predicate string) (owner, incarnation string, ok bool, err error) {
	if entry, found := f.owners[predicate]; found {
		if entry.err != nil {
			return "", "", false, entry.err
		}
		return entry.owner, entry.incarnation, true, nil
	}
	return "", "", false, nil
}

// The seam meters an unclaimed foreign edge, leaves a claimed one quiet, never
// classifies an own-subject triple (it is not foreign), and does NOT change
// routing — the edges still flow through AddTriples below (observe-only).
func TestForeignEdgeSeam_MetersUnclaimedOnly(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	fake := &fakeClassifier{unclaimed: map[string]bool{"test.edge.unclaimed": true}}
	comp.claimReader = fake

	mt := message.Type{Domain: "test", Category: "fixture", Version: "v1"}
	label := mt.Key()
	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: mt,
		Version:     1,
		Triples: []message.Triple{
			{Subject: flParentID, Predicate: "test.own.fact", Object: "x"},             // own — never foreign
			{Subject: flChildID, Predicate: "test.edge.claimed", Object: flParentID},   // foreign, claimed
			{Subject: flChildID, Predicate: "test.edge.unclaimed", Object: flParentID}, // foreign, unclaimed
		},
	}

	beforeUnclaimed := testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, "test.edge.unclaimed"))
	beforeClaimed := testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, "test.edge.claimed"))

	comp.ingestEntity(context.Background(), entity)

	assert.InDelta(t, beforeUnclaimed+1, testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, "test.edge.unclaimed")), 0.0001,
		"an unclaimed foreign edge must increment foreign_edge_unclaimed_total")
	assert.InDelta(t, beforeClaimed, testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, "test.edge.claimed")), 0.0001,
		"a claimed foreign edge must NOT be metered")
	assert.Equal(t, label, fake.gotProducer, "producer key = MessageType.Key()")
	assert.ElementsMatch(t, []string{"test.edge.claimed", "test.edge.unclaimed"}, fake.gotPreds,
		"only foreign-subject predicates reach the classifier; the own-subject triple does not")
	assert.NotContains(t, fake.gotPreds, "test.own.fact")
}

// No claim reader wired (resourceless/unmigrated deploy): the seam is a pure
// no-op — no panic, ingest proceeds, the foreign edge routing is attempted.
// ADR-055: after the must-exist flip, AddTriples rejects the absent target
// → entity stays absent, but the primary write is unaffected (warn-not-fails).
func TestForeignEdgeSeam_GracefulSkipWhenNoReader(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = nil

	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		Version:     1,
		Triples:     []message.Triple{{Subject: flChildID, Predicate: "test.edge.unclaimed", Object: flParentID}},
	}
	comp.ingestEntity(context.Background(), entity) // must not panic

	// ADR-055: routing is attempted (graceful-skip contract preserved) but
	// AddTriples rejects the absent target — entity must NOT be auto-vivified.
	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err),
		"ADR-055: graceful-skip must attempt routing but NOT auto-vivify absent target (warn-not-fails)")
}

// A producer with an invalid/zero MessageType is metered under a bounded
// "_invalid" label (cardinality guard), while the classifier still receives the
// raw producer key so a Producer-empty claim could match.
func TestForeignEdgeSeam_InvalidMessageTypeLabel(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	fake := &fakeClassifier{unclaimed: map[string]bool{"test.edge.unclaimed": true}}
	comp.claimReader = fake

	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: message.Type{}, // zero / invalid
		Version:     1,
		Triples:     []message.Triple{{Subject: flChildID, Predicate: "test.edge.unclaimed", Object: flParentID}},
	}
	before := testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues("_invalid", "test.edge.unclaimed"))
	comp.ingestEntity(context.Background(), entity)

	assert.InDelta(t, before+1, testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues("_invalid", "test.edge.unclaimed")), 0.0001,
		"an invalid MessageType must meter under the bounded _invalid label")
	assert.Equal(t, message.Type{}.Key(), fake.gotProducer,
		"the classifier still receives the raw producer key, so a Producer-empty claim can match")
}

// A classifier read error is fail-open: the seam logs and routes anyway (no
// metric, no panic, ingest not blocked).
// ADR-055: after the must-exist flip, AddTriples rejects the absent target
// → entity stays absent, but the primary write is unaffected (warn-not-fails).
func TestForeignEdgeSeam_ClassifierErrorFailsOpen(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = &fakeClassifier{err: assertErr}

	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		Version:     1,
		Triples:     []message.Triple{{Subject: flChildID, Predicate: "test.edge.unclaimed", Object: flParentID}},
	}
	comp.ingestEntity(context.Background(), entity) // must not panic

	// ADR-055: a classifier error causes fail-open routing (routing decision preserved),
	// but AddTriples rejects the absent target — entity must NOT be auto-vivified.
	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err),
		"ADR-055: fail-open must NOT auto-vivify absent target; AddTriples fails (warn-not-fails)")
}

var assertErr = errForTest("classifier read blip")

type errForTest string

func (e errForTest) Error() string { return string(e) }

// The SHARED projection-normalization seam (ADR-056 Decision 4) applies on the
// MUTATION API lane too, not just fact-arrival: a create_with_triples carrying a
// foreign-subject edge (Subject != Entity.ID) has it partitioned off the primary,
// classified, and routed onto its own subject — instead of being misfiled onto
// Entity.ID (the bug cs-api's singleSubject guard works around today).
func TestSharedSeam_CreateWithTriples_PartitionsClassifiesRoutesForeign(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	fake := &fakeClassifier{unclaimed: map[string]bool{"test.edge.is-hosted-by": true}}
	comp.claimReader = fake

	mt := message.Type{Domain: "test", Category: "fixture", Version: "v1"}
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: flParentID, MessageType: mt},
		Triples: []message.Triple{
			{Subject: flParentID, Predicate: "test.system.label", Object: "Parent"},       // own
			{Subject: flChildID, Predicate: "test.edge.is-hosted-by", Object: flParentID}, // foreign
		},
	}
	data, _ := json.Marshal(req)

	before := testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(mt.Key(), "test.edge.is-hosted-by"))

	respData, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	// The primary stores ONLY its own triple — the foreign edge is not misfiled.
	parent := storedEntity(t, comp, flParentID)
	assert.True(t, hasPredicate(parent, "test.system.label"))
	assert.False(t, hasPredicate(parent, "test.edge.is-hosted-by"),
		"foreign edge must NOT be misfiled onto the primary entity")

	// The foreign edge was classified at the shared seam (metric fired)…
	assert.InDelta(t, before+1, testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(mt.Key(), "test.edge.is-hosted-by")), 0.0001,
		"the mutation-lane foreign edge must be classified by the shared seam")
	assert.Equal(t, mt.Key(), fake.gotProducer)
	// …and routing was attempted (edge goes into toAppend, not dropped).
	// ADR-055: AddTriples now rejects absent targets — the child entity stays
	// absent (warn-not-fails). The metric alone confirms classification happened.
	_, childErr := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(childErr),
		"ADR-055: unclaimed absent child must NOT be auto-vivified; AddTriples fails (warn-not-fails)")
}

// A single-subject create_with_triples (every current caller) is a pure no-op for
// the shared seam — no classification, no routing, behaviour identical to before.
func TestSharedSeam_CreateWithTriples_SingleSubjectUnchanged(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	fake := &fakeClassifier{}
	comp.claimReader = fake

	mt := message.Type{Domain: "test", Category: "fixture", Version: "v1"}
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: flParentID, MessageType: mt},
		Triples: []message.Triple{
			{Subject: flParentID, Predicate: "test.system.label", Object: "Parent"},
			{Subject: flParentID, Predicate: "test.system.uid", Object: "u-1"},
		},
	}
	data, _ := json.Marshal(req)

	respData, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	parent := storedEntity(t, comp, flParentID)
	assert.True(t, hasPredicate(parent, "test.system.label"))
	assert.True(t, hasPredicate(parent, "test.system.uid"))
	assert.Nil(t, fake.gotPreds, "a single-subject batch must never reach the classifier")
}

func TestSharedSeam_CreateWithTriples_InvalidForeignPredicateCommitsNothing(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          flParentID,
			MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		},
		Triples: []message.Triple{
			{Subject: flParentID, Predicate: "test.system.label", Object: "Parent"},
			{Subject: flChildID, Predicate: "legacy.predicate", Object: flParentID}, // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errs.IsInvalid(err), "a malformed predicate is terminal invalid input")

	_, getErr := comp.entityBucket.Get(context.Background(), flParentID)
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound),
		"whole-projection preflight must reject before the primary entity commits")
}

func TestSharedSeam_UpdateWithTriples_InvalidForeignPredicateCommitsNothing(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	req := graph.UpdateEntityWithTriplesRequest{
		Entity: &graph.EntityState{
			ID:          flParentID,
			MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		},
		AddTriples: []message.Triple{
			{Subject: flParentID, Predicate: "test.system.label", Object: "Parent"},
			{Subject: flChildID, Predicate: "legacy.predicate", Object: flParentID}, // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := comp.handleEntityUpdateWithTriples(context.Background(), data)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errs.IsInvalid(err), "preflight must win over the missing-entity lookup")

	_, getErr := comp.entityBucket.Get(context.Background(), flParentID)
	assert.True(t, errors.Is(getErr, natsclient.ErrKVKeyNotFound),
		"an invalid update must not create or partially mutate the primary entity")
}
