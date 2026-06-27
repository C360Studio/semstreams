package gateddagexec

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
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
}
