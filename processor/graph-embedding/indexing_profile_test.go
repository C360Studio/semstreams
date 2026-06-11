package graphembedding

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// ADR-054 Phase 1 is LENIENT: graph-embedding consults the indexing profile but
// never excludes an entity. indexingEligible must return true for EVERY profile
// (and for an absent profile), so the gate is a provable no-op — strict
// enforcement is Phase 3, gated on the cost-ledger preconditions.
func TestIndexingEligible_Phase1IsAlwaysEligible(t *testing.T) {
	comp := &Component{logger: slog.Default()}

	cases := []struct {
		name    string
		profile string // "" => no profile triple at all
	}{
		{"content", vocabulary.IndexingProfileContent},
		{"control", vocabulary.IndexingProfileControl},
		{"signal", vocabulary.IndexingProfileSignal},
		{"trace", vocabulary.IndexingProfileTrace},
		{"absent", ""},
		{"unrecognized", "garbage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es := &graph.EntityState{ID: "c360.platform.test.sys.widget.001"}
			if tc.profile != "" {
				es.Triples = []message.Triple{{
					Subject:   es.ID,
					Predicate: vocabulary.EntityIndexingProfile,
					Object:    tc.profile,
				}}
			}
			assert.True(t, comp.indexingEligible(es),
				"Phase 1 lenient: profile %q must remain eligible for embedding", tc.profile)
		})
	}
}
