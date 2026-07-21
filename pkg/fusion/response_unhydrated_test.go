package fusion_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/pkg/fusion"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponse_Unhydrated pins ADR-084 D4's fusion-facing half: partial hydration is
// reported on the response, distinctly from a miss, and the wire is unchanged when
// nothing was lost.
func TestResponse_Unhydrated(t *testing.T) {
	found := entity("acme.ops.code.repo.symbol.Found", "Found", "found.go")
	lost := "acme.ops.code.repo.symbol.Lost"

	newEngine := func(g *fakeGraph) *fusion.Engine {
		return fusion.NewEngine(g, fusion.NewBodyResolver(fusion.MapStoreResolver{}))
	}

	t.Run("a partially hydrated response reports what was lost", func(t *testing.T) {
		g := &fakeGraph{
			status:     readyStatus(),
			seeds:      map[string][]string{"q": {found.ID, lost}},
			entities:   map[string]*fusion.Entity{found.ID: found},
			unhydrated: []fusion.Unhydrated{{Handle: lost, Reason: fusion.UnhydratedNotFound}},
		}
		resp, err := newEngine(g).Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{})
		require.NoError(t, err)

		assert.Len(t, resp.Nodes, 1, "what hydrated must still be returned")
		require.Len(t, resp.Unhydrated, 1)
		assert.Equal(t, lost, resp.Unhydrated[0].Handle)
		assert.Empty(t, resp.Misses, "an unhydrated seed is not a miss")
	})

	t.Run("all seeds unhydrated synthesizes no miss", func(t *testing.T) {
		// The load-bearing case. A miss claims the graph was asked and had nothing;
		// here the graph was never successfully asked, so claiming one would assert
		// exactly what the failed read left unknown.
		g := &fakeGraph{
			status:     readyStatus(),
			seeds:      map[string][]string{"q": {lost}},
			entities:   map[string]*fusion.Entity{},
			unhydrated: []fusion.Unhydrated{{Handle: lost, Reason: fusion.UnhydratedNotFound}},
		}
		resp, err := newEngine(g).Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{})
		require.NoError(t, err)

		assert.Empty(t, resp.Nodes)
		assert.Empty(t, resp.Misses, "no miss may be synthesized when nothing could be read")
		require.Len(t, resp.Unhydrated, 1)
		assert.Equal(t, lost, resp.Unhydrated[0].Handle)
	})

	t.Run("a resolve that genuinely found nothing is still a miss", func(t *testing.T) {
		// The counterpart: seeds resolved to nothing at all, so there is nothing to
		// hydrate and "the graph had nothing" IS the honest answer.
		g := &fakeGraph{status: readyStatus(), seeds: map[string][]string{}, names: []string{"Founded"}}
		resp, err := newEngine(g).Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{})
		require.NoError(t, err)

		require.Len(t, resp.Misses, 1)
		assert.Empty(t, resp.Unhydrated)
	})

	t.Run("a complete response omits the field on the wire", func(t *testing.T) {
		g := &fakeGraph{
			status:   readyStatus(),
			seeds:    map[string][]string{"q": {found.ID}},
			entities: map[string]*fusion.Entity{found.ID: found},
		}
		resp, err := newEngine(g).Fuse(context.Background(), fusion.Request{Query: "q"}, refLens{})
		require.NoError(t, err)

		raw, err := json.Marshal(resp)
		require.NoError(t, err)
		var keyed map[string]any
		require.NoError(t, json.Unmarshal(raw, &keyed))
		assert.NotContains(t, keyed, "unhydrated",
			"a fully-hydrated response must be byte-unchanged for existing consumers: %s", raw)
	})

	t.Run("the reported entry round-trips", func(t *testing.T) {
		raw, err := json.Marshal(fusion.Response{
			Unhydrated: []fusion.Unhydrated{{Handle: lost, Reason: fusion.UnhydratedUnknown}},
		})
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"unhydrated":[{"handle":"`+lost+`","reason":"unknown"}]`)

		var back fusion.Response
		require.NoError(t, json.Unmarshal(raw, &back))
		assert.Equal(t, fusion.Unhydrated{Handle: lost, Reason: fusion.UnhydratedUnknown}, back.Unhydrated[0])
	})
}
