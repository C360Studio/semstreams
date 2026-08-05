package gateddagexec

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
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

}
