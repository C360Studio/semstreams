package gateddagexec

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/gateddag"
	"github.com/stretchr/testify/require"
)

// unit builds an EntityState with the given (predicate, object) triples.
func unit(id string, triples ...message.Triple) graph.EntityState {
	return graph.EntityState{ID: id, Triples: triples}
}

func TestExtractGraph(t *testing.T) {
	cfg := validCfg()

	t.Run("presence markers + edges + claim", func(t *testing.T) {
		a := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "a")
		b := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "b")
		c := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "c")
		d := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "d")
		states := []graph.EntityState{
			unit(a, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "completed"), Object: a}),
			unit(b,
				message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: a},
				message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "failed"), Object: true}),
			unit(c,
				message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: a},
				message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: b},
				message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "claim"), Object: c}),
			unit(d, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "dirtied"), Object: d}),
		}
		v := extractGraph(states, cfg)

		require.ElementsMatch(t, []string{a, b, c, d}, v.unitIDs)
		require.True(t, v.markers.Completed[a])
		require.True(t, v.markers.Failed[b])
		require.True(t, v.markers.Dirtied[d])
		require.ElementsMatch(t, []string{a}, v.dependsOn[b])
		require.ElementsMatch(t, []string{a, b}, v.dependsOn[c])
		require.True(t, v.claimed[c])
		require.False(t, v.claimed[a])
	})

	t.Run("non-string depends_on object is skipped", func(t *testing.T) {
		x := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "x")
		y := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "y")
		states := []graph.EntityState{
			unit(x, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: 42}), // not an entity-ID string
			unit(y, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: ""}), // empty string skipped
		}
		v := extractGraph(states, cfg)
		require.Empty(t, v.dependsOn[x])
		require.Empty(t, v.dependsOn[y])
	})

	t.Run("unit with no triples has no markers or edges", func(t *testing.T) {
		lonely := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "lonely")
		v := extractGraph([]graph.EntityState{unit(lonely)}, cfg)
		require.Equal(t, []string{lonely}, v.unitIDs)
		require.Empty(t, v.markers.Completed)
		require.Empty(t, v.dependsOn[lonely])
		require.False(t, v.claimed[lonely])
	})

	t.Run("empty input", func(t *testing.T) {
		v := extractGraph(nil, cfg)
		require.Empty(t, v.unitIDs)
	})

	t.Run("referential stubs excluded by envelope; real + upgraded units kept (gh#429)", func(t *testing.T) {
		stubID := semantictest.EntityID(t, "test", "gateddag", "reader", "stub", "node", "x")
		realID := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "real-y")
		upgradedID := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "up-z")
		stub := graph.EntityState{
			ID:          stubID,
			MessageType: graph.StubMessageType,
			Triples:     []message.Triple{{Predicate: semantictest.Predicate(t, "core", "entity", "stub"), Object: true}},
		}
		realUnit := unit(realID, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "completed"), Object: realID})
		// Post-birth the stub TRIPLE persists but the ENVELOPE flips to a real
		// type; such a unit MUST be kept — the filter is envelope-based, not
		// triple-based, so a persisted stub marker does not exclude a real unit.
		upgraded := graph.EntityState{
			ID:          upgradedID,
			MessageType: message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"},
			Triples: []message.Triple{
				{Predicate: semantictest.Predicate(t, "core", "entity", "stub"), Object: true}, // persisted stub triple
				{Predicate: semantictest.Predicate(t, "gateddag", "unit", "completed"), Object: upgradedID},
			},
		}
		v := extractGraph([]graph.EntityState{stub, realUnit, upgraded}, cfg)
		require.ElementsMatch(t, []string{realID, upgradedID}, v.unitIDs)
		require.Equal(t, 1, v.stubsSkipped)
		require.True(t, v.markers.Completed[upgradedID], "upgraded unit's markers still read despite the persisted stub triple")
	})

	t.Run("dependent on a stubbed prerequisite is held, not dispatched (gh#429)", func(t *testing.T) {
		prereqID := semantictest.EntityID(t, "test", "gateddag", "reader", "stub", "node", "prereq")
		dependentID := semantictest.EntityID(t, "test", "gateddag", "reader", "unit", "node", "dependent")
		stub := graph.EntityState{
			ID:          prereqID,
			MessageType: graph.StubMessageType,
			Triples:     []message.Triple{{Predicate: semantictest.Predicate(t, "core", "entity", "stub"), Object: true}},
		}
		dependent := unit(dependentID, message.Triple{Predicate: semantictest.Predicate(t, "gateddag", "unit", "depends-on"), Object: prereqID})
		v := extractGraph([]graph.EntityState{stub, dependent}, cfg)
		require.Equal(t, []string{dependentID}, v.unitIDs, "the stub prerequisite is filtered out of the unit set")
		require.Equal(t, 1, v.stubsSkipped)
		// Dependency-closure correctness: DeriveStatus keys on marker membership,
		// so a dependent whose prerequisite is a filtered stub is still correctly
		// held (the prerequisite is not Done) — NOT wrongly dispatched.
		require.Empty(t, gateddag.SelectDispatchable(v.unitIDs, v.dependsOn, v.markers),
			"dependent must be held while its prerequisite is an unborn stub")
	})
}
