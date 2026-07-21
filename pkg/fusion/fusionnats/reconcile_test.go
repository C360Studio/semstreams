package fusionnats

import (
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ent(id string) graph.EntityState { return graph.EntityState{ID: id} }

// TestReconcileHydration covers the ADR-084 D4 set logic. The interesting cases are all
// about what a handler might do that a naive client would mishandle, which is why this
// is reconciliation rather than "trust the reply".
func TestReconcileHydration(t *testing.T) {
	a, b, c := "acme.ops.d.s.t.a", "acme.ops.d.s.t.b", "acme.ops.d.s.t.c"

	t.Run("restores request order", func(t *testing.T) {
		// The handler returns cache-hits-first. The engine ranks by position, so this
		// reordering is a silent relevance bug, not a cosmetic one.
		got := reconcileHydration([]string{a, b, c}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(c), ent(a), ent(b)},
		})
		require.Len(t, got.Entities, 3)
		assert.Equal(t, []string{a, b, c},
			[]string{got.Entities[0].ID, got.Entities[1].ID, got.Entities[2].ID},
			"hydration order is the engine's ranking prior and must match the request")
		assert.Empty(t, got.Unhydrated)
	})

	t.Run("carries the handler's reported reason", func(t *testing.T) {
		got := reconcileHydration([]string{a, b}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(a)},
			Missing:  []graph.MissingEntity{{ID: b, Reason: graph.MissingNotFound}},
		})
		require.Len(t, got.Entities, 1)
		require.Len(t, got.Unhydrated, 1)
		assert.Equal(t, fusion.Unhydrated{Handle: b, Reason: fusion.UnhydratedNotFound}, got.Unhydrated[0])
	})

	t.Run("an ID in neither list is unknown, not not_found", func(t *testing.T) {
		// An under-reporting handler. Synthesizing not_found here would assert
		// something nobody observed — the exact move that started this bug class.
		got := reconcileHydration([]string{a, b}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(a)},
		})
		require.Len(t, got.Unhydrated, 1)
		assert.Equal(t, fusion.UnhydratedUnknown, got.Unhydrated[0].Reason)
		assert.Equal(t, b, got.Unhydrated[0].Handle)
	})

	t.Run("every requested ID is accounted for exactly once", func(t *testing.T) {
		got := reconcileHydration([]string{a, b, c}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(b)},
			Missing:  []graph.MissingEntity{{ID: a, Reason: graph.MissingNotFound}},
		})
		seen := map[string]int{}
		for _, e := range got.Entities {
			seen[e.ID]++
		}
		for _, u := range got.Unhydrated {
			seen[u.Handle]++
		}
		assert.Equal(t, map[string]int{a: 1, b: 1, c: 1}, seen)
	})

	t.Run("a duplicate requested ID is reported once", func(t *testing.T) {
		// Counting instead of set-tracking would double-report this as unhydrated.
		got := reconcileHydration([]string{a, a}, graph.EntityBatchResponse{})
		assert.Len(t, got.Unhydrated, 1, "one entity, one report — even if asked for twice")
		got = reconcileHydration([]string{a, a}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(a)},
		})
		assert.Len(t, got.Entities, 1)
	})

	t.Run("an ID in BOTH lists counts as hydrated", func(t *testing.T) {
		// We hold the evidence; a contradictory report does not un-hold it. Counting
		// it as unhydrated would drop a result we actually have.
		got := reconcileHydration([]string{a}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(a)},
			Missing:  []graph.MissingEntity{{ID: a, Reason: graph.MissingNotFound}},
		})
		assert.Len(t, got.Entities, 1)
		assert.Empty(t, got.Unhydrated)
	})

	t.Run("an entity nobody asked for is dropped", func(t *testing.T) {
		got := reconcileHydration([]string{a}, graph.EntityBatchResponse{
			Entities: []graph.EntityState{ent(a), ent(b)},
		})
		require.Len(t, got.Entities, 1)
		assert.Equal(t, a, got.Entities[0].ID,
			"the response is scoped to what was requested; an extra entity is a handler bug, not a bonus")
	})

	t.Run("nothing hydrated reports every ID", func(t *testing.T) {
		got := reconcileHydration([]string{a, b}, graph.EntityBatchResponse{
			Missing: []graph.MissingEntity{
				{ID: a, Reason: graph.MissingNotFound},
				{ID: b, Reason: graph.MissingNotFound},
			},
		})
		assert.Empty(t, got.Entities)
		assert.Len(t, got.Unhydrated, 2,
			"an all-missing batch must not look like an empty graph")
	})
}

// TestUnhydratedReasonsMirrorGraph pins the value-parity between fusion's product-facing
// reason type and graph's wire type. They are separate types on purpose (different wire
// names, different layers), but a value drift would make the pass-through in
// reconcileHydration emit a reason no fusion consumer's switch handles.
func TestUnhydratedReasonsMirrorGraph(t *testing.T) {
	pairs := map[graph.MissingReason]fusion.UnhydratedReason{
		graph.MissingNotFound: fusion.UnhydratedNotFound,
		graph.MissingError:    fusion.UnhydratedError,
		graph.MissingUnknown:  fusion.UnhydratedUnknown,
	}
	for g, f := range pairs {
		assert.Equal(t, string(g), string(f),
			"reason values must match: reconcileHydration converts by cast")
	}
}
