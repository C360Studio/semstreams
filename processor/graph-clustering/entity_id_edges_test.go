// Package graphclustering — gh#461 regression.
//
// Community detection synthesizes EntityID virtual edges (siblings + system
// peers) on top of explicit edges. For a homogeneous entity family those virtual
// edges bridge everything and LPA collapses to one community. This change makes
// the synthesis operator-configurable; these tests lock the tri-state config
// resolution (omitted → defaults ON) and prove a disabled config actually
// suppresses the virtual edges.
package graphclustering

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

// --- resolver: the tri-state invariant ---

func TestEntityIDEdgesConfig_Resolve_NilKeepsDefaults(t *testing.T) {
	var cfg *EntityIDEdgesConfig // omitted block
	got := cfg.resolve()
	assert.Equal(t, clustering.DefaultEntityIDProviderConfig(), got,
		"omitted config MUST resolve to the built-in defaults (synthesis ON)")
}

func TestEntityIDEdgesConfig_Resolve_TriState(t *testing.T) {
	def := clustering.DefaultEntityIDProviderConfig()

	t.Run("disable siblings only leaves system-peers at default", func(t *testing.T) {
		got := (&EntityIDEdgesConfig{IncludeSiblings: boolPtr(false)}).resolve()
		assert.False(t, got.IncludeSiblings, "explicit false honored")
		assert.Equal(t, def.IncludeSystemPeers, got.IncludeSystemPeers, "unset toggle keeps default")
		assert.Equal(t, def.SiblingWeight, got.SiblingWeight, "unset numeric keeps default")
	})

	t.Run("disable system-peers only leaves siblings at default", func(t *testing.T) {
		got := (&EntityIDEdgesConfig{IncludeSystemPeers: boolPtr(false)}).resolve()
		assert.Equal(t, def.IncludeSiblings, got.IncludeSiblings)
		assert.False(t, got.IncludeSystemPeers)
	})

	t.Run("explicit true is honored (not just unset)", func(t *testing.T) {
		got := (&EntityIDEdgesConfig{IncludeSiblings: boolPtr(true)}).resolve()
		assert.True(t, got.IncludeSiblings)
	})

	t.Run("numeric overrides apply when positive, defaults when zero", func(t *testing.T) {
		got := (&EntityIDEdgesConfig{SiblingWeight: 0.9, MaxSystemPeers: 3}).resolve()
		assert.Equal(t, 0.9, got.SiblingWeight, "positive override applied")
		assert.Equal(t, 3, got.MaxSystemPeers, "positive override applied")
		assert.Equal(t, def.MaxSiblings, got.MaxSiblings, "zero numeric keeps default")
	})
}

// --- ApplyDefaults wires the resolved config onto the Config ---

func TestApplyDefaults_ResolvesEntityIDEdges(t *testing.T) {
	t.Run("omitted block resolves to defaults on the Config", func(t *testing.T) {
		c := DefaultConfig()
		c.EntityIDEdges = nil
		c.ApplyDefaults()
		assert.Equal(t, clustering.DefaultEntityIDProviderConfig(), c.entityIDEdges)
	})

	t.Run("explicit disable flows through ApplyDefaults", func(t *testing.T) {
		c := DefaultConfig()
		c.EntityIDEdges = &EntityIDEdgesConfig{
			IncludeSiblings:    boolPtr(false),
			IncludeSystemPeers: boolPtr(false),
		}
		c.ApplyDefaults()
		assert.False(t, c.entityIDEdges.IncludeSiblings)
		assert.False(t, c.entityIDEdges.IncludeSystemPeers)
	})
}

// --- JSON round-trip (operator-reachable surface; house rule) ---

func TestConfig_JSONRoundTrip_EntityIDEdges(t *testing.T) {
	t.Run("pointer toggles and numerics survive round-trip", func(t *testing.T) {
		in := Config{EntityIDEdges: &EntityIDEdgesConfig{
			IncludeSiblings:    boolPtr(false),
			IncludeSystemPeers: boolPtr(true),
			SiblingWeight:      0.5,
			MaxSiblings:        4,
		}}
		data, err := json.Marshal(in)
		require.NoError(t, err)

		var out Config
		require.NoError(t, json.Unmarshal(data, &out))
		require.NotNil(t, out.EntityIDEdges)
		require.NotNil(t, out.EntityIDEdges.IncludeSiblings)
		assert.False(t, *out.EntityIDEdges.IncludeSiblings)
		require.NotNil(t, out.EntityIDEdges.IncludeSystemPeers)
		assert.True(t, *out.EntityIDEdges.IncludeSystemPeers)
		assert.Equal(t, 0.5, out.EntityIDEdges.SiblingWeight)
		assert.Equal(t, 4, out.EntityIDEdges.MaxSiblings)
	})

	t.Run("omitted entity_id_edges unmarshals to nil → resolves to defaults", func(t *testing.T) {
		var out Config
		require.NoError(t, json.Unmarshal([]byte(`{"batch_size":10}`), &out))
		assert.Nil(t, out.EntityIDEdges, "absent block must be nil, not a zero struct")
		assert.Equal(t, clustering.DefaultEntityIDProviderConfig(), out.EntityIDEdges.resolve())
	})
}

// --- no-silent-drop: an operator toggle typo fails loudly at the factory ---

func TestCreateGraphClustering_RejectsUnknownEntityIDEdgeKey(t *testing.T) {
	// gh#461: an operator disabling synthesis via a typo'd key (e.g.
	// "include_sibling" singular, or "disable_siblings") must fail loudly at the
	// factory, not silently bind to nothing and leave synthesis ON — which would
	// leave the collapse the operator is trying to fix in place with no error.
	// Drives the production factory wire (mirrors the anomaly_config guard test).
	typoJSON := []byte(`{"entity_id_edges": {"include_sibling": false}}`)

	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	deps := component.Dependencies{NATSClient: natsClient}

	comp, err := CreateGraphClustering(typoJSON, deps)
	require.Error(t, err)
	assert.Nil(t, comp)
	assert.Contains(t, err.Error(), "entity_id_edges")
}

// --- behavioral proof: the resolved config controls virtual-edge synthesis ---

// edgeTestProvider implements clustering.Provider with explicit-only adjacency.
type edgeTestProvider struct {
	entities  []string
	neighbors map[string][]string
}

func (p *edgeTestProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	return p.entities, nil
}
func (p *edgeTestProvider) GetNeighbors(_ context.Context, id, _ string) ([]string, error) {
	return p.neighbors[id], nil
}
func (p *edgeTestProvider) GetEdgeWeight(_ context.Context, _, _ string) (float64, error) {
	return 1.0, nil
}

// Two disjoint 4-cliques of the SAME 6-part type (share the 5-part prefix
// acme.ops.games.dragon.boid), linked only within each clique.
func disjointCliqueProvider() *edgeTestProvider {
	a := []string{"acme.ops.games.dragon.boid.a1", "acme.ops.games.dragon.boid.a2",
		"acme.ops.games.dragon.boid.a3", "acme.ops.games.dragon.boid.a4"}
	b := []string{"acme.ops.games.dragon.boid.b1", "acme.ops.games.dragon.boid.b2",
		"acme.ops.games.dragon.boid.b3", "acme.ops.games.dragon.boid.b4"}
	neighbors := map[string][]string{}
	for _, clique := range [][]string{a, b} {
		for _, id := range clique {
			for _, other := range clique {
				if other != id {
					neighbors[id] = append(neighbors[id], other)
				}
			}
		}
	}
	return &edgeTestProvider{entities: append(append([]string{}, a...), b...), neighbors: neighbors}
}

func containsAny(got []string, candidates ...string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, c := range candidates {
		if set[c] {
			return true
		}
	}
	return false
}

func TestEntityIDEdges_ResolvedConfigControlsVirtualEdges(t *testing.T) {
	ctx := context.Background()
	base := disjointCliqueProvider()
	const a1 = "acme.ops.games.dragon.boid.a1"
	cliqueB := []string{"acme.ops.games.dragon.boid.b1", "acme.ops.games.dragon.boid.b2",
		"acme.ops.games.dragon.boid.b3", "acme.ops.games.dragon.boid.b4"}

	t.Run("default (omitted) config synthesizes cross-clique sibling edges — the gh#461 bug", func(t *testing.T) {
		cfg := (*EntityIDEdgesConfig)(nil).resolve()
		prov := clustering.NewEntityIDProvider(base, cfg, nil)
		neighbors, err := prov.GetNeighbors(ctx, a1, "both")
		require.NoError(t, err)
		assert.True(t, containsAny(neighbors, cliqueB...),
			"with synthesis ON, a1 gets virtual sibling edges into the other clique (bridges the two)")
	})

	t.Run("disabled config yields explicit edges only — the fix", func(t *testing.T) {
		cfg := (&EntityIDEdgesConfig{
			IncludeSiblings:    boolPtr(false),
			IncludeSystemPeers: boolPtr(false),
		}).resolve()
		prov := clustering.NewEntityIDProvider(base, cfg, nil)
		neighbors, err := prov.GetNeighbors(ctx, a1, "both")
		require.NoError(t, err)
		assert.False(t, containsAny(neighbors, cliqueB...),
			"with synthesis OFF, a1 has no virtual edge into the other clique — detection runs on explicit topology")
		assert.ElementsMatch(t,
			[]string{"acme.ops.games.dragon.boid.a2", "acme.ops.games.dragon.boid.a3", "acme.ops.games.dragon.boid.a4"},
			neighbors, "only the explicit intra-clique neighbors remain")
	})
}
