package graphclustering

import (
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-054 Phase 1 is LENIENT: graph-clustering observes the indexing profile
// (Debug dry-run signal) but never excludes an entity from community/structural
// detection. observeIndexingProfileForClustering only logs — it must handle
// every profile (and an absent profile) without panicking, and it has no return
// value, so by construction nothing is filtered. Strict enforcement is Phase 3,
// gated on the cost-ledger preconditions.
func TestObserveIndexingProfileForClustering_LenientNoPanic(t *testing.T) {
	logger := slog.Default()
	for _, profile := range []string{
		vocabulary.IndexingProfileTrace,
		vocabulary.IndexingProfileSignal,
		vocabulary.IndexingProfileContent,
		vocabulary.IndexingProfileControl,
		"",
		"garbage",
	} {
		es := &graph.EntityState{ID: "c360.platform.test.sys.widget.001"}
		if profile != "" {
			es.Triples = []message.Triple{{
				Subject:   es.ID,
				Predicate: vocabulary.EntityIndexingProfile,
				Object:    profile,
			}}
		}
		// Must not panic and must not mutate the entity (observation only).
		before := len(es.Triples)
		observeIndexingProfileForClustering(logger, es)
		if len(es.Triples) != before {
			t.Errorf("observeIndexingProfileForClustering mutated the entity (profile %q)", profile)
		}
	}
}
