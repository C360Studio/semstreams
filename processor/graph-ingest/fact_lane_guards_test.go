package graphingest

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// Fact-arrival consumer guards in graph-ingest: StorageRef extraction and the
// single-subject Graphable boundary.
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

// entity-id-audit:classify intentional-malformed "malformed" line=110 column=12 surface=go-field:EntityState.ID entity_id_invalid:arity malformed fact-lane state rejection fixture

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

func TestDistinctSortedSubjects(t *testing.T) {
	got := distinctSortedSubjects([]message.Triple{
		{Subject: "b"}, {Subject: "a"}, {Subject: "b"}, {Subject: "c"}, {Subject: "a"},
	})
	assert.Equal(t, []string{"a", "b", "c"}, got)
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
	payload := &testGraphablePayload{id: flParentID, triples: []message.Triple{{
		Subject: flParentID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"), Object: "v", Timestamp: time.Now(),
	}}}
	msg := message.NewBaseMessage(payload.Schema(), payload, "test")

	entity, err := comp.extractEntityFromMessage(msg)
	require.NoError(t, err)
	assert.Nil(t, entity.StorageRef, "a plain Graphable carries no StorageRef")
}

func TestIngestEntity_RejectsCrossSubjectFacts(t *testing.T) {
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

	require.Error(t, comp.ingestEntity(ctx, entity))
	_, parentErr := comp.entityBucket.Get(ctx, flParentID)
	_, childErr := comp.entityBucket.Get(ctx, flChildID)
	assert.Error(t, parentErr, "a rejected Graphable must not partially write its primary entity")
	assert.Error(t, childErr, "graph-ingest must not synthesize a foreign-subject entity")
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
