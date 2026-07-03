package gateddagexec

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/gateddag"
	"github.com/stretchr/testify/require"
)

// unit builds an EntityState with the given (predicate, object) triples.
func unit(id string, triples ...message.Triple) graph.EntityState {
	return graph.EntityState{ID: id, Triples: triples}
}

func tri(pred string, obj any) message.Triple {
	return message.Triple{Predicate: pred, Object: obj}
}

func TestExtractGraph(t *testing.T) {
	cfg := validCfg()

	t.Run("presence markers + edges + claim", func(t *testing.T) {
		states := []graph.EntityState{
			unit("a", tri(cfg.CompletedPredicate, "a")),
			unit("b",
				tri(cfg.DependsOnPredicate, "a"),
				tri(cfg.FailedPredicate, true)),
			unit("c",
				tri(cfg.DependsOnPredicate, "a"),
				tri(cfg.DependsOnPredicate, "b"),
				tri(cfg.ClaimPredicate, "c")),
			unit("d", tri(cfg.DirtiedPredicate, "d")),
		}
		v := extractGraph(states, cfg)

		require.ElementsMatch(t, []string{"a", "b", "c", "d"}, v.unitIDs)
		require.True(t, v.markers.Completed["a"])
		require.True(t, v.markers.Failed["b"])
		require.True(t, v.markers.Dirtied["d"])
		require.ElementsMatch(t, []string{"a"}, v.dependsOn["b"])
		require.ElementsMatch(t, []string{"a", "b"}, v.dependsOn["c"])
		require.True(t, v.claimed["c"])
		require.False(t, v.claimed["a"])
	})

	t.Run("non-string depends_on object is skipped", func(t *testing.T) {
		states := []graph.EntityState{
			unit("x", tri(cfg.DependsOnPredicate, 42)), // not an entity-ID string
			unit("y", tri(cfg.DependsOnPredicate, "")), // empty string skipped
		}
		v := extractGraph(states, cfg)
		require.Empty(t, v.dependsOn["x"])
		require.Empty(t, v.dependsOn["y"])
	})

	t.Run("unit with no triples has no markers or edges", func(t *testing.T) {
		v := extractGraph([]graph.EntityState{unit("lonely")}, cfg)
		require.Equal(t, []string{"lonely"}, v.unitIDs)
		require.Empty(t, v.markers.Completed)
		require.Empty(t, v.dependsOn["lonely"])
		require.False(t, v.claimed["lonely"])
	})

	t.Run("empty input", func(t *testing.T) {
		v := extractGraph(nil, cfg)
		require.Empty(t, v.unitIDs)
	})

	t.Run("referential stubs excluded by envelope; real + upgraded units kept (gh#429)", func(t *testing.T) {
		stub := graph.EntityState{
			ID:          "stub-x",
			MessageType: graph.StubMessageType,
			Triples:     []message.Triple{{Predicate: graph.PredStubMarker, Object: true}},
		}
		realUnit := unit("real-y", tri(cfg.CompletedPredicate, "real-y"))
		// Post-birth the stub TRIPLE persists but the ENVELOPE flips to a real
		// type; such a unit MUST be kept — the filter is envelope-based, not
		// triple-based, so a persisted stub marker does not exclude a real unit.
		upgraded := graph.EntityState{
			ID:          "up-z",
			MessageType: message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"},
			Triples: []message.Triple{
				{Predicate: graph.PredStubMarker, Object: true}, // persisted stub triple
				tri(cfg.CompletedPredicate, "up-z"),
			},
		}
		v := extractGraph([]graph.EntityState{stub, realUnit, upgraded}, cfg)
		require.ElementsMatch(t, []string{"real-y", "up-z"}, v.unitIDs)
		require.Equal(t, 1, v.stubsSkipped)
		require.True(t, v.markers.Completed["up-z"], "upgraded unit's markers still read despite the persisted stub triple")
	})

	t.Run("dependent on a stubbed prerequisite is held, not dispatched (gh#429)", func(t *testing.T) {
		stub := graph.EntityState{
			ID:          "prereq",
			MessageType: graph.StubMessageType,
			Triples:     []message.Triple{{Predicate: graph.PredStubMarker, Object: true}},
		}
		dependent := unit("dependent", tri(cfg.DependsOnPredicate, "prereq"))
		v := extractGraph([]graph.EntityState{stub, dependent}, cfg)
		require.Equal(t, []string{"dependent"}, v.unitIDs, "the stub prerequisite is filtered out of the unit set")
		require.Equal(t, 1, v.stubsSkipped)
		// Dependency-closure correctness: DeriveStatus keys on marker membership,
		// so a dependent whose prerequisite is a filtered stub is still correctly
		// held (the prerequisite is not Done) — NOT wrongly dispatched.
		require.Empty(t, gateddag.SelectDispatchable(v.unitIDs, v.dependsOn, v.markers),
			"dependent must be held while its prerequisite is an unborn stub")
	})
}
