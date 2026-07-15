package graphingest

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-055 Wave 0 — Fact-arrival consumer guards in graph-ingest:
//   - §5 StorageRef extraction (consumer half): a ContentStorable/Storable
//     Graphable's ObjectStore pointer is lifted onto the EntityState so
//     offloaded content stays linked and embeddable.
//   - §5 T2 WARN-and-regroup: a Graphable's cross-entity edges (Subject !=
//     EntityID) are routed onto their correct subject instead of being misfiled
//     under the primary key.
// These drive the production extract → ingest wire (extractEntityFromMessage +
// ingestEntity), the same orchestration handleMessage runs after decode.

const (
	flParentID = "c360.platform.test.sys.widget.001"
	flChildID  = "c360.platform.test.sys.widget.002"
)

// testStorablePayload implements message.ContentStorable (Storable + content
// maps) so the consumer can lift its StorageRef. It is a valid message.Payload.
type testStorablePayload struct {
	id      string
	triples []message.Triple
	ref     *message.StorageReference
}

func (p *testStorablePayload) EntityID() string                      { return p.id }
func (p *testStorablePayload) Triples() []message.Triple             { return p.triples }
func (p *testStorablePayload) StorageRef() *message.StorageReference { return p.ref }
func (p *testStorablePayload) Validate() error                       { return nil }
func (p *testStorablePayload) MarshalJSON() ([]byte, error)          { return []byte("{}"), nil }
func (p *testStorablePayload) UnmarshalJSON([]byte) error            { return nil }
func (p *testStorablePayload) Schema() message.Type {
	return message.Type{Domain: "test", Category: "storable", Version: "v1"}
}
func (p *testStorablePayload) ContentFields() map[string]string {
	return map[string]string{message.ContentRoleBody: "body"}
}
func (p *testStorablePayload) RawContent() map[string]string {
	return map[string]string{"body": "offloaded text"}
}

func hasPredicate(es *graph.EntityState, predicate string) bool {
	for _, t := range es.Triples {
		if t.Predicate == predicate {
			return true
		}
	}
	return false
}

// --- §5 T2 partition helper (pure) ---

func TestPartitionTriplesBySubject(t *testing.T) {
	primary := flParentID
	triples := []message.Triple{
		{Subject: primary, Predicate: "test.own.a", Object: 1},
		{Subject: flChildID, Predicate: "test.foreign.b", Object: 2},
		{Subject: "", Predicate: "test.subject.empty-primary", Object: 3},
		{Subject: primary, Predicate: "test.own.c", Object: 4},
		{Subject: "c360.platform.test.sys.widget.003", Predicate: "test.foreign.d", Object: 5},
	}

	own, foreign := partitionTriplesBySubject(primary, triples)

	require.Len(t, own, 3, "own = subject==entityID plus the empty-subject (historical primary filing)")
	require.Len(t, foreign, 2, "foreign = every triple naming a different subject")
	assert.True(t, hasPredicateInTriples(own, "test.subject.empty-primary"),
		"an empty Subject must stay on the primary (preserves pre-ADR filing)")
	assert.True(t, hasPredicateInTriples(foreign, "test.foreign.b"))
	assert.True(t, hasPredicateInTriples(foreign, "test.foreign.d"))
}

func TestPartitionTriplesBySubject_AllOwn(t *testing.T) {
	own, foreign := partitionTriplesBySubject(flParentID, []message.Triple{
		{Subject: flParentID, Predicate: "test.own.a"},
		{Subject: flParentID, Predicate: "test.own.b"},
	})
	assert.Len(t, own, 2)
	assert.Empty(t, foreign, "a single-Subject Graphable produces no foreign edges")
}

func TestIngestEntity_FillsOnlyEmptyFactProjectionSubject(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	entity := &graph.EntityState{
		ID: flParentID,
		Triples: []message.Triple{
			{Subject: "", Predicate: "test.subject.omitted", Object: "filled"},
			{Subject: flParentID, Predicate: "test.subject.explicit", Object: "unchanged"},
		},
	}

	require.NoError(t, comp.ingestEntity(context.Background(), entity))
	stored := storedEntity(t, comp, flParentID)
	require.Len(t, stored.Triples, 3) // two facts plus the indexing-profile floor
	for _, triple := range stored.Triples {
		if triple.Predicate == "test.subject.omitted" || triple.Predicate == "test.subject.explicit" {
			assert.Equal(t, flParentID, triple.Subject)
		}
	}
}

func TestIngestEntity_DoesNotRepairNonEmptyFactProjectionSubject(t *testing.T) {
	comp, bucket := createTestComponentWithMockKVBucket(t)
	getCalls := 0
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		getCalls++
		return nil, jetstream.ErrKeyNotFound
	}
	entity := &graph.EntityState{
		ID: flParentID,
		Triples: []message.Triple{{
			Subject: "malformed", Predicate: "test.subject.explicit", Object: "unchanged",
		}},
	}

	require.Error(t, comp.ingestEntity(context.Background(), entity))
	assert.Equal(t, 0, getCalls, "malformed projected state must fail before KV probing")
	assert.Equal(t, "malformed", entity.Triples[0].Subject, "non-empty subject bytes must not be repaired")
}

func TestIngestEntity_DoesNotFillFromInvalidEnvelopeIdentity(t *testing.T) {
	comp, bucket := createTestComponentWithMockKVBucket(t)
	getCalls := 0
	bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		getCalls++
		return nil, jetstream.ErrKeyNotFound
	}
	entity := &graph.EntityState{
		ID:      "malformed",
		Triples: []message.Triple{{Subject: "", Predicate: "test.subject.omitted"}},
	}

	require.Error(t, comp.ingestEntity(context.Background(), entity))
	assert.Equal(t, 0, getCalls, "invalid envelope must fail before KV probing")
	assert.Empty(t, entity.Triples[0].Subject, "fill requires an already-canonical envelope ID")
}

func TestDistinctSubjects_SortedAndDeduped(t *testing.T) {
	got := distinctSubjects([]message.Triple{
		{Subject: "b"}, {Subject: "a"}, {Subject: "b"}, {Subject: "c"}, {Subject: "a"},
	})
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func hasPredicateInTriples(triples []message.Triple, predicate string) bool {
	for _, t := range triples {
		if t.Predicate == predicate {
			return true
		}
	}
	return false
}

// --- §5 StorageRef extraction (consumer half) ---

func TestStorageRefExtraction_ConsumerLiftsRefOntoEntity(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ref := &message.StorageReference{
		StorageInstance: "objectstore-primary",
		Key:             "2026/06/12/step-001",
		ContentType:     "application/json",
		Size:            128,
	}
	payload := &testStorablePayload{
		id:      flParentID,
		triples: []message.Triple{{Subject: flParentID, Predicate: "doc.body.ref", Object: "body", Timestamp: time.Now()}},
		ref:     ref,
	}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	entity, err := comp.extractEntityFromMessage(msg)
	require.NoError(t, err)
	require.NotNil(t, entity.StorageRef, "a Storable payload's ref must be lifted onto the EntityState at extract")
	assert.Equal(t, ref.Key, entity.StorageRef.Key)

	// And it must survive the merge into the bucket (create-branch marshal).
	comp.ingestEntity(context.Background(), entity)
	es := storedEntity(t, comp, flParentID)
	require.NotNil(t, es.StorageRef, "StorageRef must persist through MergeEntity to the stored entity")
	assert.Equal(t, "objectstore-primary", es.StorageRef.StorageInstance)
	assert.Equal(t, ref.Key, es.StorageRef.Key)
	assert.Equal(t, int64(128), es.StorageRef.Size)
}

func TestStorageRefExtraction_NonStorablePayloadLeavesRefNil(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// testGraphablePayload (indexing_profile_test.go) is Graphable but NOT Storable.
	payload := &testGraphablePayload{id: flParentID, triples: []message.Triple{userTriple("test.fixture.value", "v")}}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	entity, err := comp.extractEntityFromMessage(msg)
	require.NoError(t, err)
	assert.Nil(t, entity.StorageRef, "a plain Graphable carries no StorageRef")
}

// --- §5 T2 WARN-and-regroup through the ingest wire ---

func TestIngestEntity_RegroupsForeignSubjectEdgeOntoChild(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	// A parent Graphable that, like sensorml.Asset, emits an inverse edge whose
	// Subject is the CHILD, not the parent (its EntityID).
	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: testWidgetMessageType(),
		Version:     1,
		Triples: []message.Triple{
			{Subject: flParentID, Predicate: "graph.rel.hosts", Object: flChildID, Timestamp: time.Now()},
			{Subject: flChildID, Predicate: "graph.rel.is-hosted-by", Object: flParentID, Timestamp: time.Now()},
		},
	}

	comp.ingestEntity(ctx, entity)

	// Parent keeps only its own fact (+ the framework profile stamp); the inverse
	// edge must NOT be misfiled onto it.
	parent := storedEntity(t, comp, flParentID)
	assert.True(t, hasPredicate(parent, "graph.rel.hosts"), "parent keeps its own forward edge")
	assert.False(t, hasPredicate(parent, "graph.rel.is-hosted-by"),
		"the child-subject inverse edge must not stay misfiled on the parent")

	// The inverse edge lands on the CHILD entity, under the correct subject.
	child := storedEntity(t, comp, flChildID)
	require.True(t, hasPredicate(child, "graph.rel.is-hosted-by"), "the inverse edge must be regrouped onto its own subject")
	for _, tr := range child.Triples {
		if tr.Predicate == "graph.rel.is-hosted-by" {
			assert.Equal(t, flChildID, tr.Subject, "the regrouped edge keeps its child Subject")
		}
	}
}

func TestIngestEntity_SingleSubjectNoForeignEntityCreated(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	entity := &graph.EntityState{
		ID:          flParentID,
		MessageType: testWidgetMessageType(),
		Version:     1,
		Triples:     []message.Triple{{Subject: flParentID, Predicate: "graph.rel.hosts", Object: "x", Timestamp: time.Now()}},
	}
	comp.ingestEntity(ctx, entity)

	parent := storedEntity(t, comp, flParentID)
	assert.True(t, hasPredicate(parent, "graph.rel.hosts"))
	assert.Equal(t, []string{vocabulary.IndexingProfileControl}, profileValues(parent),
		"a single-Subject entity still gets the floor profile and is otherwise unchanged")

	// No phantom child entity should have been conjured.
	_, err := comp.entityBucket.Get(ctx, flChildID)
	assert.Error(t, err, "a single-Subject Graphable must not create any other entity")
}
