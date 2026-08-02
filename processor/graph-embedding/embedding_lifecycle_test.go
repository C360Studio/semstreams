package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/pkg/revlag"
)

// tombstoneEntry is a KV entry with a caller-chosen key and operation, which the
// package's shared mockKVEntry (fixed key, always Put) cannot express.
type tombstoneEntry struct {
	key string
	rev uint64
	op  jetstream.KeyValueOp
}

func (e *tombstoneEntry) Key() string                     { return e.key }
func (e *tombstoneEntry) Value() []byte                   { return nil }
func (e *tombstoneEntry) Revision() uint64                { return e.rev }
func (e *tombstoneEntry) Created() time.Time              { return time.Now() }
func (e *tombstoneEntry) Delta() uint64                   { return 0 }
func (e *tombstoneEntry) Operation() jetstream.KeyValueOp { return e.op }
func (e *tombstoneEntry) Bucket() string                  { return "ENTITY_STATES" }

func newTombstoneComponent(t *testing.T, index *mockKVBucket) *Component {
	t.Helper()
	return &Component{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
		storage:           embedding.NewStorage(nil, index, newMockKVBucket()),
		watermark:         revlag.New(),
	}
}

// TestEntityTombstoneDeletesEmbedding guards gh#614 part 1.
//
// The ENTITY_STATES tombstone branch completed the readiness watermark and
// returned without ever touching EMBEDDING_INDEX. The deleted entity's vector
// therefore stayed queryable forever: semantic search kept returning a dead
// entity ID, graph-query could not resolve it, and the query fell back to the
// text path — so a deletion silently degraded search AND pushed queries off the
// semantic path. It also meant Storage's in-memory vector cache never evicted,
// because that eviction fires on a KV delete of the embedding key that nothing
// issued.
func TestEntityTombstoneDeletesEmbedding(t *testing.T) {
	t.Parallel()

	const entityID = "acme.ops.robotics.gcs.drone.001"
	ctx := context.Background()

	index := newMockKVBucket()
	c := newTombstoneComponent(t, index)

	// The entity has a generated embedding in EMBEDDING_INDEX. Seeded through the
	// real pending -> generated transition because that is how a record legitimately
	// comes to exist: SaveGenerated is an UPDATE lane that carries the pending
	// record's content hash forward.
	//
	// This ordering used to be load-bearing for the wrong reason — SaveGenerated
	// nil-dereferenced without a pending record, so the seed was routing around a
	// panic. It no longer does; the missing-record case is a defined outcome, and
	// asserting that here keeps this test from re-encoding the bug as expected
	// behaviour. See graph/embedding.ErrRecordGone and the tombstone-race coverage
	// in graph/embedding/storage_record_gone_test.go.
	if err := c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25-384", 384, "hash-1", 41); !errors.Is(err, embedding.ErrRecordGone) {
		t.Fatalf("SaveGenerated with no pending record = %v, want ErrRecordGone (it must not panic or resurrect)", err)
	}

	if err := c.storage.SavePending(ctx, entityID, "hash-1", "some text", 41); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if err := c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25-384", 384, "hash-1", 41); err != nil {
		t.Fatalf("seed embedding: %v", err)
	}
	if rec, err := c.storage.GetEmbedding(ctx, entityID); err != nil || rec == nil {
		t.Fatalf("seed not readable: rec=%v err=%v", rec, err)
	}

	// The entity is deleted from ENTITY_STATES.
	c.applyEntityWatchEntry(ctx, &tombstoneEntry{key: entityID, rev: 42, op: jetstream.KeyValueDelete})

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding after tombstone: %v", err)
	}
	if rec != nil {
		t.Fatalf("embedding survived entity deletion: %+v", rec)
	}

	// The watermark must still have drained — a delete is a terminal outcome.
	if got := c.embeddingCompletions.Load(); got != 1 {
		t.Fatalf("embeddingCompletions = %d, want 1 (watermark must drain on delete)", got)
	}
}

// TestEntityTombstoneCompletesWatermarkWhenDeleteFails pins the failure policy:
// a failed EMBEDDING_INDEX delete is logged, never fatal, and MUST NOT skip the
// watermark completion. Dropping the completion would pin embedding readiness on
// that revision forever (ADR-066 §3).
func TestEntityTombstoneCompletesWatermarkWhenDeleteFails(t *testing.T) {
	t.Parallel()

	const entityID = "acme.ops.robotics.gcs.drone.002"
	ctx := context.Background()

	index := newMockKVBucket()
	index.deleteFunc = func(context.Context, string, ...jetstream.KVDeleteOpt) error {
		return errors.New("kv unavailable")
	}
	c := newTombstoneComponent(t, index)

	c.applyEntityWatchEntry(ctx, &tombstoneEntry{key: entityID, rev: 7, op: jetstream.KeyValueDelete})

	if got := c.embeddingCompletions.Load(); got != 1 {
		t.Fatalf("embeddingCompletions = %d, want 1 (a failed delete must not strand the watermark)", got)
	}
}

// TestQueueEntityForEmbedding_WritesEmptyContentHash pins the hop-2 key move (#623)
// at the production call site: hop 1 writes a pending record with an EMPTY
// ContentHash — a reference, not a key — because hop 2 now derives the dedup key
// over the resolved and truncated bytes it embeds.
//
// The embedder-identity distinction that gh#612 requires (a bm25 -> http switch must
// not serve a stale vector) is now enforced by DedupKey, which hop 2 applies over the
// embedded text; it is covered in graph/embedding/dedup_key_test.go. Hop 1 no longer
// carries it, so asserting a non-empty identity-bearing key here would re-encode the
// split-across-hops design that #623 removed.
func TestQueueEntityForEmbedding_WritesEmptyContentHash(t *testing.T) {
	t.Parallel()

	const entityID = "acme.ops.robotics.gcs.drone.003"
	entity := []byte(`{"id":"` + entityID + `","triples":[{"subject":"` + entityID +
		`","predicate":"drone.mission.description","object":"survey the north field at low altitude"}]}`)

	index := newMockKVBucket()
	c := &Component{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
		storage:           embedding.NewStorage(nil, index, newMockKVBucket()),
		watermark:         revlag.New(),
		embedder:          embedding.NewBM25Embedder(embedding.BM25Config{Dimensions: 384, K1: 1.5, B: 0.75}),
		config:            Config{EmbedderType: "bm25"},
	}

	c.queueEntityForEmbedding(context.Background(), entityID, 1, entity)

	entry, err := index.Get(context.Background(), entityID)
	if err != nil {
		t.Fatalf("no pending record written for %s: %v", entityID, err)
	}
	var rec embedding.Record
	if err := json.Unmarshal(entry.Value(), &rec); err != nil {
		t.Fatalf("decode pending record: %v", err)
	}

	if rec.ContentHash != "" {
		t.Fatalf("hop-1 wrote ContentHash %q, want empty: hop-2 derives the dedup key over the embedded bytes now (#623)", rec.ContentHash)
	}
	if rec.SourceText == "" {
		t.Fatal("hop-1 must still carry the source text for hop-2 to embed")
	}
	if rec.SourceRevision != 1 {
		t.Fatalf("SourceRevision = %d, want 1 (threaded for the ADR-066 readiness watermark)", rec.SourceRevision)
	}
}
