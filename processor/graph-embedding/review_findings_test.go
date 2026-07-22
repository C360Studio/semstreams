package graphembedding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/pkg/revlag"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func newFindingsTestComponent(t *testing.T, index jetstream.KeyValue) *Component {
	t.Helper()
	return &Component{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		lifecycleReporter: component.NewNoOpLifecycleReporter(),
		storage:           embedding.NewStorage(index, newMockKVBucket()),
		watermark:         revlag.New(),
		failed:            make(map[string]failureInfo),
		failedGauge:       newEmbeddingFailedGauge(),
		config:            DefaultConfig(),
	}
}

// TestFailedMap_RevisionAware_StaleCompletionKeepsNewerFailure is the #613 F1 statement:
// the current-failed map is revision-aware, so a SUPERSEDED older-revision non-failed
// completion (which storage correctly dropped as superseded, reporting OutcomeDeleted/Skipped)
// must NOT clear a newer-revision failure. The durable EMBEDDING_INDEX record is still failed
// at the newer revision, so readiness must keep counting it. A completion at the failure's own
// (or a newer) revision does clear it — real recovery.
//
// Fails without the fix because applyTerminalOutcome deleted the entity on ANY non-failed
// outcome regardless of revision, reporting failed_count=0 over a still-failed durable record.
func TestFailedMap_RevisionAware_StaleCompletionKeepsNewerFailure(t *testing.T) {
	t.Parallel()
	c := &Component{failed: make(map[string]failureInfo), failedGauge: newEmbeddingFailedGauge()}

	// Revision 11 fails.
	c.completeEmbedding("ent", 11, embedding.OutcomeFailed, "connection_refused")
	count, _, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count)

	// An OLDER revision 10 completes: storage dropped the older write as superseded and the
	// worker reported OutcomeDeleted. It must be a NO-OP for the map.
	c.completeEmbedding("ent", 10, embedding.OutcomeDeleted, "")
	count, _, _ = c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a superseded older completion must NOT clear a newer failure (#613 F1)")
	require.Equal(t, 1.0, testutil.ToFloat64(c.failedGauge))

	// A completion at the failure's own revision (recovery) clears it.
	c.completeEmbedding("ent", 11, embedding.OutcomeGenerated, "")
	count, _, _ = c.failedSnapshot()
	require.Equal(t, uint64(0), count,
		"a completion at the failure's revision clears it (recovery)")
	require.Equal(t, 0.0, testutil.ToFloat64(c.failedGauge))
}

// TestFailedMap_RevisionAware_StaleFailureDoesNotOverrideNewer proves the symmetric F1 half:
// an OLDER-revision failure arriving after a NEWER one already resolved the entity (generated)
// must not resurrect the entity into the failed map.
func TestFailedMap_RevisionAware_StaleFailureDoesNotOverrideNewer(t *testing.T) {
	t.Parallel()
	c := &Component{failed: make(map[string]failureInfo), failedGauge: newEmbeddingFailedGauge()}

	// Newer revision 20 generated successfully — entity is not failed.
	c.completeEmbedding("ent", 20, embedding.OutcomeGenerated, "")
	// Seed a failure at a newer revision, then have an OLDER failure arrive.
	c.completeEmbedding("ent", 20, embedding.OutcomeFailed, "timeout")
	count, _, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count)

	// An OLDER-revision (15) failure must NOT override the newer state.
	c.completeEmbedding("ent", 15, embedding.OutcomeFailed, "content_error")
	_, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), reasons["timeout"], "the newer failure's reason must stand")
	require.Equal(t, uint64(0), reasons["content_error"], "an older failure must not override the newer reason")
}

// textEntityJSON builds an EntityState with a text-bearing triple (extractable for embedding).
func textEntityJSON(id string) []byte {
	return []byte(`{"id":"` + id + `","triples":[{"subject":"` + id +
		`","predicate":"doc.meta.title","object":"survey the north field"}]}`)
}

// noTextEntityJSON builds a telemetry-only EntityState (no text predicate → no-text path).
func noTextEntityJSON(id string) []byte {
	return []byte(`{"id":"` + id + `","triples":[{"subject":"` + id +
		`","predicate":"robotics.status.armed","object":true}]}`)
}

// TestSavePendingFailure_IsNonTerminal is the #613 F2 component-side statement: a transient
// SavePending failure (the durable record never persisted) must NOT advance the readiness
// watermark or clear the current-failed map. The entity re-drives on the next ENTITY_STATES
// delivery; a sustained outage degrades honestly via the stuck detector rather than reporting
// a false-ready.
//
// Fails without the fix because the SavePending error path completed the watermark
// (OutcomeSkipped), draining it and clearing any prior failure over un-persisted work.
func TestSavePendingFailure_IsNonTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	index := newMockKVBucket()
	index.putFunc = func(context.Context, string, []byte) (uint64, error) {
		return 0, errors.New("kv write unavailable")
	}
	c := newFindingsTestComponent(t, index)

	const entityID = "acme.ops.robotics.gcs.drone.savepend"
	// A prior failure for this entity at revision 5 (in-memory), to prove it is not cleared.
	c.failed[entityID] = failureInfo{reason: "connection_refused", at: time.Now(), rev: 5}
	c.setFailedGauge(1)

	// The entity is re-delivered at revision 6 with text; SavePending's Put fails.
	c.watermark.Observe(6, entityID, time.Now())
	c.queueEntityForEmbedding(ctx, entityID, 6, textEntityJSON(entityID))

	require.Equal(t, uint64(0), c.embeddingCompletions.Load(),
		"a SavePending persistence failure must not complete the watermark (#613 F2)")
	count, _, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a SavePending persistence failure must not clear the current-failed map (#613 F2)")
}

// TestNoTextPath_RemovesStaleDurableFailedRecord is the #613 F2 no-text statement: when an
// entity that previously FAILED transitions to no text, the hop-1 no-text terminal must remove
// the stale durable StatusFailed record before clearing readiness, so durable and in-memory
// state stay consistent (a restart's seed scan would otherwise re-add it as failed).
//
// Fails without the fix because the no-text path cleared the in-memory map but left the durable
// failed record in EMBEDDING_INDEX.
func TestNoTextPath_RemovesStaleDurableFailedRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	index := newMockKVBucket()
	c := newFindingsTestComponent(t, index)

	const entityID = "acme.ops.robotics.gcs.drone.notext"
	// Seed a DURABLE failed record at revision 5, and the matching in-memory entry.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 5))
	require.NoError(t, c.storage.SaveFailed(ctx, entityID, "boom", "content_error", 5))
	c.failed[entityID] = failureInfo{reason: "content_error", at: time.Now(), rev: 5}
	c.setFailedGauge(1)

	// The entity is re-delivered at revision 6 with NO text.
	c.queueEntityForEmbedding(ctx, entityID, 6, noTextEntityJSON(entityID))

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec,
		"the no-text path must remove the stale durable failed record before clearing readiness (#613 F2)")

	count, _, _ := c.failedSnapshot()
	require.Equal(t, uint64(0), count, "the in-memory failure clears too (revision 6 >= 5)")
}
