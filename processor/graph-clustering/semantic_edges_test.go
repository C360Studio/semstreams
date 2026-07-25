// Package graphclustering — Epic B B2 (ADR-086) semantic co-location edges.
//
// These tests lock the operator-facing config surface for the semantic-edge
// tier: the tri-state resolution (omitted block -> feature OFF), the
// strict-decode no-silent-drop guard, the JSON round-trip for every
// operator-reachable field, and — critically — that enabling the tier does not
// disturb the gh#461 default-preservation invariant when it is left OFF.
package graphclustering

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- resolver: omitted block == feature OFF ---

func TestSemanticEdgesConfig_Resolve_NilKeepsOff(t *testing.T) {
	var cfg *SemanticEdgesConfig // omitted block
	got := cfg.resolve()
	assert.False(t, got.enabled, "omitted semantic_edges block MUST resolve to feature OFF")
	// Defaults are still populated so an enabling operator inherits the starting
	// values, but enabled=false is what gates the chain.
	assert.Equal(t, clustering.DefaultSemanticThreshold, got.threshold)
	assert.Equal(t, clustering.DefaultSemanticMaxNeighbors, got.k)
	assert.Equal(t, clustering.DefaultSemanticEdgeWeight, got.weight)
}

func TestSemanticEdgesConfig_Resolve_Overrides(t *testing.T) {
	t.Run("enable with defaults", func(t *testing.T) {
		got := (&SemanticEdgesConfig{EnableSemanticEdges: true}).resolve()
		assert.True(t, got.enabled)
		assert.Equal(t, clustering.DefaultSemanticThreshold, got.threshold)
		assert.Equal(t, clustering.DefaultSemanticMaxNeighbors, got.k)
		assert.Equal(t, clustering.DefaultSemanticEdgeWeight, got.weight)
	})

	t.Run("positive overrides applied, zero keeps default", func(t *testing.T) {
		got := (&SemanticEdgesConfig{
			EnableSemanticEdges:         true,
			SemanticSimilarityThreshold: 0.8,
			SemanticMaxNeighbors:        12,
			// SemanticEdgeWeight left zero -> default
		}).resolve()
		assert.InDelta(t, 0.8, got.threshold, 1e-9)
		assert.Equal(t, 12, got.k)
		assert.Equal(t, clustering.DefaultSemanticEdgeWeight, got.weight, "zero numeric keeps default")
	})
}

// --- ApplyDefaults: the gh#461 default-off invariant survives the new tier ---

func TestApplyDefaults_SemanticDisabled_KeepsEntityIDDefaults(t *testing.T) {
	// The load-bearing proof: with the semantic tier OFF (the default), the
	// resolved EntityID edge config is byte-identical to today's default. This
	// is the same invariant TestApplyDefaults_ResolvesEntityIDEdges pins, re-run
	// here with the new tier present-but-off to prove it did not shift the caps.
	c := DefaultConfig()
	c.SemanticEdges = nil
	c.EntityIDEdges = nil
	c.ApplyDefaults()
	assert.False(t, c.semanticEdges.enabled, "tier OFF by default")
	assert.Equal(t, clustering.DefaultEntityIDProviderConfig(), c.entityIDEdges,
		"semantic tier OFF must reproduce today's sibling/system-peer weights and caps exactly (0.7/10, 0.3/15)")
}

func TestApplyDefaults_SemanticEnabled_RebalancesStructuralCaps(t *testing.T) {
	// Starting values (EMPIRICAL, ADR-086) — asserted here as the resolved
	// STARTING profile, not as final tuned values.
	c := DefaultConfig()
	c.SemanticEdges = &SemanticEdgesConfig{EnableSemanticEdges: true}
	c.EntityIDEdges = nil
	c.ApplyDefaults()

	assert.True(t, c.semanticEdges.enabled)
	assert.Equal(t, semanticEnabledMaxSiblings, c.entityIDEdges.MaxSiblings, "sibling cap 10 -> 5")
	assert.Equal(t, semanticEnabledMaxSystemPeers, c.entityIDEdges.MaxSystemPeers, "system-peer cap 15 -> 8")
	assert.InDelta(t, semanticEnabledSystemPeerWeight, c.entityIDEdges.SystemPeerWeight, 1e-9, "system-peer weight 0.3 -> 0.2")
	assert.InDelta(t, semanticEnabledSiblingWeight, c.entityIDEdges.SiblingWeight, 1e-9, "sibling weight unchanged 0.7")
	assert.True(t, c.entityIDEdges.IncludeSiblings)
	assert.True(t, c.entityIDEdges.IncludeSystemPeers)
}

func TestApplyDefaults_SemanticEnabled_ExplicitEntityIDOverrideStillWins(t *testing.T) {
	// An operator who both enables the tier AND sets an explicit entity_id_edges
	// cap keeps their override over the rebalanced baseline.
	c := DefaultConfig()
	c.SemanticEdges = &SemanticEdgesConfig{EnableSemanticEdges: true}
	c.EntityIDEdges = &EntityIDEdgesConfig{MaxSiblings: 3}
	c.ApplyDefaults()
	assert.Equal(t, 3, c.entityIDEdges.MaxSiblings, "explicit override wins over rebalanced baseline")
	assert.Equal(t, semanticEnabledMaxSystemPeers, c.entityIDEdges.MaxSystemPeers, "unset field keeps rebalanced baseline")
}

// --- JSON round-trip: every operator-reachable field (house rule) ---

func TestConfig_JSONRoundTrip_SemanticEdges(t *testing.T) {
	t.Run("all fields survive round-trip", func(t *testing.T) {
		in := Config{SemanticEdges: &SemanticEdgesConfig{
			EnableSemanticEdges:         true,
			SemanticSimilarityThreshold: 0.72,
			SemanticMaxNeighbors:        10,
			SemanticEdgeWeight:          0.85,
		}}
		data, err := json.Marshal(in)
		require.NoError(t, err)

		var out Config
		require.NoError(t, json.Unmarshal(data, &out))
		require.NotNil(t, out.SemanticEdges)
		assert.True(t, out.SemanticEdges.EnableSemanticEdges)
		assert.InDelta(t, 0.72, out.SemanticEdges.SemanticSimilarityThreshold, 1e-9)
		assert.Equal(t, 10, out.SemanticEdges.SemanticMaxNeighbors)
		assert.InDelta(t, 0.85, out.SemanticEdges.SemanticEdgeWeight, 1e-9)
	})

	t.Run("omitted semantic_edges unmarshals to nil -> resolves OFF", func(t *testing.T) {
		var out Config
		require.NoError(t, json.Unmarshal([]byte(`{"batch_size":10}`), &out))
		assert.Nil(t, out.SemanticEdges, "absent block must be nil, not a zero struct")
		assert.False(t, out.SemanticEdges.resolve().enabled)
	})

	t.Run("json keys match the operator surface", func(t *testing.T) {
		data, err := json.Marshal(Config{SemanticEdges: &SemanticEdgesConfig{
			EnableSemanticEdges:         true,
			SemanticSimilarityThreshold: 0.75,
			SemanticMaxNeighbors:        8,
			SemanticEdgeWeight:          0.9,
		}})
		require.NoError(t, err)
		s := string(data)
		assert.Contains(t, s, `"semantic_edges"`)
		assert.Contains(t, s, `"enable_semantic_edges"`)
		assert.Contains(t, s, `"semantic_similarity_threshold"`)
		assert.Contains(t, s, `"semantic_max_neighbors"`)
		assert.Contains(t, s, `"semantic_edge_weight"`)
	})
}

// --- no-silent-drop: an enable-toggle typo fails loudly at the factory ---

func TestCreateGraphClustering_RejectsUnknownSemanticEdgeKey(t *testing.T) {
	// An operator enabling the tier via a typo'd key (e.g. "enable_semantic_edge"
	// singular) must fail loudly at the factory, not silently bind to nothing and
	// leave the feature OFF — the precise silent-drop this guard prevents
	// (ADR-086/ADR-054). Drives the production factory wire.
	typoJSON := []byte(`{"semantic_edges": {"enable_semantic_edge": true}}`)

	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	deps := component.Dependencies{NATSClient: natsClient}

	comp, err := CreateGraphClustering(typoJSON, deps)
	require.Error(t, err)
	assert.Nil(t, comp)
	assert.Contains(t, err.Error(), "semantic_edges")
}

func TestRejectUnknownSemanticEdgeKeys(t *testing.T) {
	t.Run("empty block is fine", func(t *testing.T) {
		assert.NoError(t, rejectUnknownSemanticEdgeKeys(nil))
	})
	t.Run("known keys bind", func(t *testing.T) {
		assert.NoError(t, rejectUnknownSemanticEdgeKeys(json.RawMessage(
			`{"enable_semantic_edges":true,"semantic_similarity_threshold":0.75,"semantic_max_neighbors":8,"semantic_edge_weight":0.9}`)))
	})
	t.Run("unknown key rejected", func(t *testing.T) {
		err := rejectUnknownSemanticEdgeKeys(json.RawMessage(`{"enable_semantic_edge":true}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "semantic_edges")
	})
}

// --- default-off provider chain: disabled == EntityIDProvider verbatim ---

// This proves the wiring intent at the config layer: with the tier OFF,
// ApplyDefaults leaves the resolved semantic config disabled, so
// initProviderAndDetector never wraps the EntityIDProvider. We assert the
// resolved config the seam branches on, and (through the provider-layer test in
// graph/clustering) that a wrapped-but-empty tier is behaviorally identical.
func TestSemanticEdges_DefaultOff_ChainStaysTwoProviders(t *testing.T) {
	c := DefaultConfig()
	c.ApplyDefaults()
	require.False(t, c.semanticEdges.enabled,
		"default config keeps the semantic tier OFF, so the provider chain stays kvProvider -> EntityIDProvider")

	// And the resolved EntityID config is the untouched default, so the wrapped
	// EntityIDProvider (were it ever built) carries today's caps.
	assert.Equal(t, clustering.DefaultEntityIDProviderConfig(), c.entityIDEdges)
}
