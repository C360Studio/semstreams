package graphclustering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// poisonedEntityStateBytes is resident ENTITY_STATES poison under a valid key:
// stored bytes whose predicate violates the canonical three-part grammar.
func poisonedEntityStateBytes() []byte {
	return []byte(`{"id":"acme.ops.robotics.gcs.drone.002","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":"old"}]}`) // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.predicate","reason":"arity"}
}

// TestConsumedPoisonLatchesStickyProjectionReset is the coverage-equivalence
// proof for retiring the dedicated ENTITY_STATES contract watch
// (poison-response-scoping D7 / graph-clustering delta scenario "one watcher,
// same sticky latch"): a poisoned authoritative value consumed on clustering's
// input path — the polled reads that drive a detection cycle — must latch the
// same sticky whole-view reset-required projection state the dedicated watch
// latched, with no watcher involved at all.
func TestConsumedPoisonLatchesStickyProjectionReset(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.ApplyDefaults()

	entityBucket := newMockKVBucket()
	c := &Component{
		name:           "graph-clustering",
		config:         cfg,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		entityBucket:   entityBucket,
		outgoingBucket: newMockKVBucket(),
		incomingBucket: newMockKVBucket(),
		storage:        clustering.NewNATSCommunityStorage(newMockKVBucket()),
	}
	// Production wiring: the same constructor path Start uses to build the
	// detector and its consume-path entity reads.
	c.initProviderAndDetector()

	ctx := context.Background()
	validID := semantictest.EntityID(t, "acme", "ops", "robotics", "gcs", "drone", "001")
	poisonedID := semantictest.EntityID(t, "acme", "ops", "robotics", "gcs", "drone", "002")
	validData, err := graph.MarshalEntityState(&graph.EntityState{
		ID: validID,
		Triples: []message.Triple{{
			Subject: validID, Predicate: semantictest.Predicate(t, "robotics", "status", "armed"), Object: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entityBucket.Put(ctx, validID, validData); err != nil {
		t.Fatal(err)
	}
	if _, err := entityBucket.Put(ctx, poisonedID, poisonedEntityStateBytes()); err != nil {
		t.Fatal(err)
	}

	// Drive one detection cycle through the production entry point. The cycle
	// consumes authoritative entity bytes (corpus/summarization reads), which
	// must observe the resident poison.
	c.runCommunityDetection(ctx)

	contractErr := c.graphStatePoison.Load()
	if contractErr == nil {
		t.Fatal("poisoned ENTITY_STATES value consumed on the input path did not latch the sticky projection reset state")
	}
	if contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
		t.Fatalf("latched reason = %s, want %s", contractErr.Reason, graph.GraphStateReasonNoncanonicalPredicate)
	}

	// Whole-view response: the query surface refuses with the typed fatal
	// reset-required error, exactly as it did when the dedicated watch latched.
	result, err := c.handleQueryLevelNATS(ctx, []byte(`{"level":0}`))
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("query error after latch = %T %v, want fatal reset-required", err, err)
	}
	if result != nil {
		t.Fatalf("query result after latch = %s, want nil", result)
	}
	var typedErr *graph.StateContractError
	if !errors.As(err, &typedErr) {
		t.Fatalf("query error after latch = %v, want *graph.StateContractError", err)
	}

	// Sticky: a later valid overwrite of the poisoned key cannot clear the
	// latch — previously derived community output is still unproven. The next
	// cycle must skip instead of deriving output.
	repairedData, err := graph.MarshalEntityState(&graph.EntityState{
		ID: poisonedID,
		Triples: []message.Triple{{
			Subject: poisonedID, Predicate: semantictest.Predicate(t, "robotics", "status", "armed"), Object: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entityBucket.Put(ctx, poisonedID, repairedData); err != nil {
		t.Fatal(err)
	}
	c.runCommunityDetection(ctx)
	if c.graphStatePoison.Load() == nil {
		t.Fatal("valid overwrite cleared the sticky projection reset latch; poison must be sticky until restart")
	}
}

// TestEntityStateQuerierLatchesComponentPoison drives the production querier
// constructor shared by initProviderAndDetector and startEnhancementWorker:
// every consume-site decode failure must latch the component's sticky
// projection reset even when the caller downgrades the batch error (the LPA
// summarizer and enhancement worker both log-and-continue).
func TestEntityStateQuerierLatchesComponentPoison(t *testing.T) {
	t.Parallel()

	bucket := newMockKVBucket()
	c := &Component{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		entityBucket: bucket,
	}
	querier := c.newEntityStateQuerier()

	ctx := context.Background()
	poisonedID := semantictest.EntityID(t, "acme", "ops", "robotics", "gcs", "drone", "002")
	if _, err := bucket.Put(ctx, poisonedID, poisonedEntityStateBytes()); err != nil {
		t.Fatal(err)
	}

	entities, err := querier.GetEntities(ctx, []string{poisonedID})
	if err == nil || !errs.IsFatal(err) {
		t.Fatalf("GetEntities error = %T %v, want fatal graph-state poison", err, err)
	}
	if entities != nil {
		t.Fatalf("GetEntities returned partial result %#v before poison", entities)
	}
	contractErr := c.graphStatePoison.Load()
	if contractErr == nil {
		t.Fatal("consume-site decode failure did not latch the component poison state")
	}
	if contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
		t.Fatalf("latched reason = %s, want %s", contractErr.Reason, graph.GraphStateReasonNoncanonicalPredicate)
	}
}
