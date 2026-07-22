package fusion_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/c360studio/semstreams/storage"
)

// bodyFailureCount reads the value of the fusion body-hydration-failure counter
// for one reason from a MetricsRegistry. Each test uses its OWN fresh registry, so
// the value is isolated and deterministic — no process-global counter to sequence.
func bodyFailureCount(t *testing.T, reg *metric.MetricsRegistry, reason string) float64 {
	t.Helper()
	mfs, err := reg.PrometheusRegistry().Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "semstreams_fusion_body_hydration_failures_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == reason {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// withBodyEntity has a resolvable body reference (instance+key triples) — refLens
// returns a non-nil handle for it, so the failure path is the deref.
func withBodyEntity(id, name string) *fusion.Entity {
	return entity(id, name, "h/"+name+".go",
		message.Triple{Predicate: refStorageInstancePr, Object: "objectstore"},
		message.Triple{Predicate: refStorageKeyPr, Object: "k/" + name},
	)
}

// errHydrateLens is refLens with a Hydrate that faults — a genuine retrieval
// error producing the handle, DISTINCT from returning (nil,nil) for a body-less
// entity. It exercises the engine's Hydrate-error → BodyError branch.
type errHydrateLens struct{ refLens }

func (errHydrateLens) Hydrate(context.Context, *fusion.Entity) (*message.StorageReference, error) {
	return nil, errors.New("hydrate backend fault")
}

// TestNode_BodyReason pins gh#616/#600 at the corrected seam: a body that was
// requested (WantBody) but could not be loaded is reported as an empty body AND a
// bounded reason on the node itself, distinguishing an absent object (not_found)
// from a read fault (error) from a Hydrate fault (error) — while an entity that
// legitimately has NO body stays SILENT (no reason, no counter). The node always
// still ships (partial result, no defer, no miss).
func TestNode_BodyReason(t *testing.T) {
	req := fusion.Request{Query: "q", Want: []fusion.Want{fusion.WantBody}}

	newGraph := func(ents ...*fusion.Entity) *fakeGraph {
		seeds := make([]string, 0, len(ents))
		byID := make(map[string]*fusion.Entity, len(ents))
		for _, e := range ents {
			seeds = append(seeds, e.ID)
			byID[e.ID] = e
		}
		return &fakeGraph{status: readyStatus(), seeds: map[string][]string{"q": seeds}, entities: byID}
	}

	t.Run("an absent stored object reports reason not_found (the #600 case)", func(t *testing.T) {
		// Ref PRESENT on the entity, but the store resolves it to no object — the
		// storage layer signals this with the backend-agnostic not-found sentinel,
		// which ResolveBody wraps with %w. The engine must errors.Is it → not_found.
		ent := withBodyEntity("acme.ops.code.repo.symbol.Expired", "Expired")
		store := &fakeStore{getErr: storage.ErrObjectNotFound}
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(ent), fusion.NewBodyResolver(
			fusion.MapStoreResolver{"objectstore": store})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err)
		require.Len(t, resp.Nodes, 1, "the node still ships as a partial result")
		assert.Empty(t, resp.Nodes[0].Body)
		assert.Equal(t, fusion.BodyNotFound, resp.Nodes[0].BodyReason)
		assert.Equal(t, float64(1), bodyFailureCount(t, reg, "not_found"))
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "error"))
	})

	t.Run("a read fault reports reason error", func(t *testing.T) {
		// Ref present, the deref faults with a NON-not-found error → error.
		ent := withBodyEntity("acme.ops.code.repo.symbol.OnEvent", "OnEvent")
		store := &fakeStore{getErr: errors.New("backend down")}
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(ent), fusion.NewBodyResolver(
			fusion.MapStoreResolver{"objectstore": store})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err, "a body fault must NOT fail the fuse")
		require.Len(t, resp.Nodes, 1)
		assert.Empty(t, resp.Nodes[0].Body)
		assert.Equal(t, fusion.BodyError, resp.Nodes[0].BodyReason)
		assert.Equal(t, float64(1), bodyFailureCount(t, reg, "error"))
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "not_found"))
	})

	t.Run("a Hydrate fault reports reason error", func(t *testing.T) {
		// The lens itself errors producing the handle → a genuine retrieval fault.
		ent := withBodyEntity("acme.ops.code.repo.symbol.OnEvent", "OnEvent")
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(ent), fusion.NewBodyResolver(
			fusion.MapStoreResolver{})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, errHydrateLens{})
		require.NoError(t, err)
		require.Len(t, resp.Nodes, 1)
		assert.Empty(t, resp.Nodes[0].Body)
		assert.Equal(t, fusion.BodyError, resp.Nodes[0].BodyReason)
		assert.Equal(t, float64(1), bodyFailureCount(t, reg, "error"))
	})

	t.Run("a body-less entity is silent — no reason, no counter", func(t *testing.T) {
		// No storage triples → refLens.Hydrate returns (nil,nil): the entity
		// legitimately has NO verbatim body. That is normal, not a failure.
		ent := entity("acme.ops.code.repo.symbol.Bodyless", "Bodyless", "h/bodyless.go")
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(ent), fusion.NewBodyResolver(
			fusion.MapStoreResolver{})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err)
		require.Len(t, resp.Nodes, 1, "a body-less node still ships")
		assert.Empty(t, resp.Nodes[0].Body)
		assert.Empty(t, resp.Nodes[0].BodyReason, "a body-less node carries NO reason")
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "not_found"),
			"a body-less node must not increment the failure counter")
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "error"))
	})

	t.Run("a missing body is a partial result, not a defer or a miss", func(t *testing.T) {
		a := withBodyEntity("acme.ops.code.repo.symbol.A", "A")
		b := withBodyEntity("acme.ops.code.repo.symbol.B", "B")
		store := &fakeStore{getErr: storage.ErrObjectNotFound}
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(a, b), fusion.NewBodyResolver(
			fusion.MapStoreResolver{"objectstore": store})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err)
		require.Len(t, resp.Nodes, 2, "every affected node is present")
		for _, n := range resp.Nodes {
			assert.Equal(t, fusion.BodyNotFound, n.BodyReason)
		}
		assert.False(t, resp.Deferred, "a body failure never defers the response")
		assert.Empty(t, resp.Misses, "a body failure synthesizes no miss")
		assert.Equal(t, float64(2), bodyFailureCount(t, reg, "not_found"))
	})

	t.Run("a hydrated body carries no reason and is wire-unchanged", func(t *testing.T) {
		ent := withBodyEntity("acme.ops.code.repo.symbol.OnEvent", "OnEvent")
		store := &fakeStore{data: map[string][]byte{"k/OnEvent": []byte("package handlers")}}
		reg := metric.NewMetricsRegistry()
		eng := fusion.NewEngine(newGraph(ent), fusion.NewBodyResolver(
			fusion.MapStoreResolver{"objectstore": store})).WithMetrics(reg)

		resp, err := eng.Fuse(context.Background(), req, refLens{})
		require.NoError(t, err)
		require.Len(t, resp.Nodes, 1)
		require.Equal(t, "package handlers", resp.Nodes[0].Body)
		assert.Empty(t, resp.Nodes[0].BodyReason, "a hydrated node carries no reason")
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "not_found"))
		assert.Equal(t, float64(0), bodyFailureCount(t, reg, "error"))

		raw, err := json.Marshal(resp.Nodes[0])
		require.NoError(t, err)
		var keyed map[string]any
		require.NoError(t, json.Unmarshal(raw, &keyed))
		assert.NotContains(t, keyed, "body_reason",
			"a fully-hydrated node must be byte-unchanged for existing consumers: %s", raw)
	})
}

// TestNode_BodyReason_RoundTrip proves the new field survives the production JSON
// codec both ways: absent (omitempty) on a hydrated node, and present with its
// bounded value on a failed one.
func TestNode_BodyReason_RoundTrip(t *testing.T) {
	t.Run("omitempty absent when the body hydrated", func(t *testing.T) {
		raw, err := json.Marshal(fusion.Node{Name: "OnEvent", Body: "package handlers"})
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "body_reason")

		var back fusion.Node
		require.NoError(t, json.Unmarshal(raw, &back))
		assert.Empty(t, back.BodyReason)
	})

	t.Run("present with its value when the body failed", func(t *testing.T) {
		raw, err := json.Marshal(fusion.Node{Name: "OnEvent", BodyReason: fusion.BodyNotFound})
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"body_reason":"not_found"`)

		var back fusion.Node
		require.NoError(t, json.Unmarshal(raw, &back))
		assert.Equal(t, fusion.BodyNotFound, back.BodyReason)
	})
}

// TestBodyReason_SharesUnhydratedVocabulary pins the closed set to the seed-
// hydration vocabulary value-for-value (design D3 / task 3.1): body and seed are
// distinct types but read identically on the wire.
func TestBodyReason_SharesUnhydratedVocabulary(t *testing.T) {
	assert.Equal(t, string(fusion.UnhydratedNotFound), string(fusion.BodyNotFound))
	assert.Equal(t, string(fusion.UnhydratedError), string(fusion.BodyError))
}
