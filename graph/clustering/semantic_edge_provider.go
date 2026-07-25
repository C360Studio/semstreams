package clustering

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/c360studio/semstreams/pkg/errs"
)

// Semantic-edge starting values (EMPIRICAL, ADR-086 / design.md). These are the
// semantic-enabled profile's STARTING point, to be tuned against
// `partition_colocation_mean` on the theme-spanning fixture queries — recorded
// here as measured-not-asserted, NOT final. They apply only when the operator
// enables the semantic-edge tier (`enable_semantic_edges`); an unopted
// deployment is untouched (gh#461 invariant).
const (
	// DefaultSemanticMaxNeighbors is the mutual-kNN k: the per-direction top-k
	// candidate set size. Bounds per-entity semantic degree so the new tier
	// competes with, rather than dominates, the structural edges.
	DefaultSemanticMaxNeighbors = 8
	// DefaultSemanticThreshold is the minimum similarity for a candidate to
	// count toward the top-k directed set.
	DefaultSemanticThreshold = 0.75
	// DefaultSemanticEdgeWeight is the synthesized semantic virtual-edge weight
	// — below explicit (1.0) but above the rebalanced structural tiers, so a
	// thematically-related but structurally-heterogeneous pair can still be
	// voted together by LPA.
	DefaultSemanticEdgeWeight = 0.9

	// Fallback weights used only if a caller constructs the provider without
	// supplying them; the production wire always resolves these from config.
	defaultSemanticSiblingWeight    = 0.7
	defaultSemanticSystemPeerWeight = 0.2
)

// SemanticNeighborFinder returns the entity IDs semantically similar to
// entityID at or above threshold, capped at limit (the directed top-k set for
// mutual-kNN synthesis). It is deliberately a narrow, clustering-local
// interface so this package does not import graph/inference: the production
// wire satisfies it with a thin adapter over the component's existing
// graph.embedding.query.similar finder (no second similarity RPC — B2 §1.2).
type SemanticNeighborFinder interface {
	SimilarNeighbors(ctx context.Context, entityID string, threshold float64, limit int) ([]string, error)
}

// WeightConfig is the single resolved home for every edge-tier weight used by
// SemanticEdgeProvider.GetEdgeWeight. Its resolve method (B2 §2, "one testable
// place") replaces EntityIDProvider's first-match cascade, which cannot be
// reused unmodified once a fourth (semantic) tier joins the vote: the cascade
// silently drops tier identity, so a pair that is both a sibling (0.7) and a
// mutual-kNN semantic match (0.9) would resolve to whichever tier the cascade
// checked first rather than to the max of the two.
type WeightConfig struct {
	// SiblingWeight is the resolved weight for an EntityID sibling edge (same
	// 5-part type prefix). Starting value 0.7 (unchanged from today).
	SiblingWeight float64
	// SystemPeerWeight is the resolved weight for an EntityID system-peer edge
	// (same system segment). Starting value 0.2 in the semantic-enabled profile
	// (0.3 today), so the total structural vote mass stays competitive once the
	// semantic tier is added rather than growing.
	SystemPeerWeight float64
	// SemanticWeight is the resolved weight for a mutual-kNN semantic edge.
	// Starting value 0.9.
	SemanticWeight float64
}

// qualifyingTiers records which edge tiers a pair qualifies under, plus the
// explicit edge weight (>0 iff an explicit edge exists). It is the input to
// WeightConfig.resolve.
type qualifyingTiers struct {
	// explicitWeight is the weight of an explicit (base-provider) edge, or 0 if
	// none exists. A value >0 is strictly dominant.
	explicitWeight float64
	sibling        bool
	systemPeer     bool
	semantic       bool
}

// resolve returns the final edge weight for a pair given the tiers it qualifies
// under. This is the load-bearing rule (B2 §2.1):
//
//  1. An explicit edge is STRICTLY DOMINANT: if one exists (weight > 0) its
//     weight is returned outright, over every virtual-edge tier.
//  2. Otherwise the weight is the MAX across the qualifying virtual-edge tiers
//     (sibling, system-peer, semantic) — NEVER a sum. A pair that is both a
//     sibling and a mutual-kNN match resolves to max(sibling, semantic).
//
// The max is accumulated with math.Max and never with `+`, which is the
// structural guarantee that this can never become a sum: there is no additive
// operator anywhere on this path.
func (w WeightConfig) resolve(t qualifyingTiers) float64 {
	// 1. Explicit strictly dominant.
	if t.explicitWeight > 0 {
		return t.explicitWeight
	}

	// 2. Max across qualifying virtual tiers (never a sum).
	weight := 0.0
	if t.sibling {
		weight = math.Max(weight, w.SiblingWeight)
	}
	if t.systemPeer {
		weight = math.Max(weight, w.SystemPeerWeight)
	}
	if t.semantic {
		weight = math.Max(weight, w.SemanticWeight)
	}
	return weight
}

// SemanticEdgeProvider decorates an EntityIDProvider with mutual-kNN semantic
// virtual edges, so entities that are thematically related but structurally
// heterogeneous (different type, different system) can still land in the same
// LPA community. It is the third link in the detection provider chain:
//
//	kvProvider (explicit edges)
//	  -> EntityIDProvider (sibling + system-peer virtual edges)
//	    -> SemanticEdgeProvider (mutual-kNN semantic virtual edges)
//
// It is inserted only when the operator enables the semantic-edge tier; when
// disabled the chain is exactly the two providers it is today and behavior is
// byte-identical (B2 §1.4).
//
// Semantic virtual edges are ephemeral — computed on demand from the similarity
// finder and never persisted — exactly like the EntityID virtual edges.
type SemanticEdgeProvider struct {
	// base is the wrapped EntityIDProvider. Held concretely (not as the Provider
	// interface) so GetEdgeWeight can query per-tier membership — explicit weight
	// from the underlying base, sibling/system-peer membership from the
	// EntityIDProvider — and resolve the max across tiers rather than inheriting
	// the first-match collapse.
	base *EntityIDProvider

	finder    SemanticNeighborFinder
	weights   WeightConfig
	k         int
	threshold float64
	logger    *slog.Logger

	// Lazily-built symmetric mutual-kNN adjacency, mirroring
	// EntityIDProvider.ensureTypePrefixCache's build-once-per-instance pattern.
	// mutualNeighbors[a][b] is true iff a and b are a mutual-kNN pair.
	cacheMu          sync.RWMutex
	mutualNeighbors  map[string]map[string]bool
	cacheInitialized atomic.Bool

	// queryCount counts SimilarNeighbors calls issued while building the cache,
	// for observability during development. (The bounded/cached cross-cycle
	// build and its metrics are B2 §7, a later slice.)
	queryCount atomic.Int64
}

// SemanticEdgeParams holds the mutual-kNN tuning knobs for SemanticEdgeProvider.
type SemanticEdgeParams struct {
	// K is the mutual-kNN k (per-direction top-k candidate set size).
	K int
	// Threshold is the minimum similarity for a candidate to count.
	Threshold float64
}

// Compile-time proof the decorator satisfies the graph Provider contract.
var _ Provider = (*SemanticEdgeProvider)(nil)

// NewSemanticEdgeProvider wraps an EntityIDProvider with the semantic-edge tier.
// Zero-valued params and weights fall back to the documented starting values.
func NewSemanticEdgeProvider(
	base *EntityIDProvider,
	finder SemanticNeighborFinder,
	weights WeightConfig,
	params SemanticEdgeParams,
	logger *slog.Logger,
) *SemanticEdgeProvider {
	if params.K <= 0 {
		params.K = DefaultSemanticMaxNeighbors
	}
	if params.Threshold <= 0 {
		params.Threshold = DefaultSemanticThreshold
	}
	if weights.SemanticWeight <= 0 {
		weights.SemanticWeight = DefaultSemanticEdgeWeight
	}
	if weights.SiblingWeight <= 0 {
		weights.SiblingWeight = defaultSemanticSiblingWeight
	}
	if weights.SystemPeerWeight <= 0 {
		weights.SystemPeerWeight = defaultSemanticSystemPeerWeight
	}
	return &SemanticEdgeProvider{
		base:            base,
		finder:          finder,
		weights:         weights,
		k:               params.K,
		threshold:       params.Threshold,
		logger:          logger,
		mutualNeighbors: make(map[string]map[string]bool),
	}
}

// GetAllEntityIDs delegates to the wrapped provider — the semantic tier adds
// edges, never entities.
func (p *SemanticEdgeProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	return p.base.GetAllEntityIDs(ctx)
}

// GetNeighbors returns the wrapped provider's neighbors (explicit + sibling +
// system-peer) plus this entity's mutual-kNN semantic neighbors, deduplicated.
// Semantic relationships are symmetric, so direction is respected only for the
// wrapped (explicit) edges, exactly as EntityIDProvider treats sibling edges.
//
// The semantic tier is strictly additive: if the mutual-kNN cache cannot be
// built (e.g. the embedding index is cold and the finder returns nothing), the
// structural neighbor set is returned unchanged rather than failing the cycle.
func (p *SemanticEdgeProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if entityID == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "SemanticEdgeProvider", "GetNeighbors", "entityID is empty")
	}

	base, err := p.base.GetNeighbors(ctx, entityID, direction)
	if err != nil {
		return nil, errs.WrapTransient(err, "SemanticEdgeProvider", "GetNeighbors", "base provider error")
	}

	if err := p.ensureCache(ctx); err != nil {
		// The semantic tier is additive over an always-valid structural floor:
		// a cache-build failure degrades to structural neighbors, it does not
		// fail the whole traversal.
		if p.logger != nil {
			p.logger.Warn("semantic edge cache build failed, returning structural neighbors only",
				slog.String("entity_id", entityID),
				slog.Any("error", err))
		}
		return base, nil
	}

	p.cacheMu.RLock()
	semantic := p.mutualNeighbors[entityID]
	p.cacheMu.RUnlock()
	if len(semantic) == 0 {
		return base, nil
	}

	// Deduplicate semantic additions against the structural neighbors, then sort
	// them for a deterministic result set (the base order is already stable).
	seen := make(map[string]bool, len(base))
	for _, id := range base {
		seen[id] = true
	}
	additions := make([]string, 0, len(semantic))
	for id := range semantic {
		if !seen[id] {
			additions = append(additions, id)
		}
	}
	sort.Strings(additions)

	result := make([]string, 0, len(base)+len(additions))
	result = append(result, base...)
	result = append(result, additions...)
	return result, nil
}

// GetEdgeWeight resolves the edge weight across all four tiers in one place
// (B2 §2). Explicit edges (from the underlying base provider) are strictly
// dominant; otherwise the weight is the max across the sibling, system-peer,
// and semantic tiers the pair qualifies under — never a sum. This deliberately
// does NOT delegate to EntityIDProvider.GetEdgeWeight, whose first-match cascade
// would collapse tier identity before the max could be computed.
func (p *SemanticEdgeProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	if fromID == "" || toID == "" {
		return 0.0, errs.WrapInvalid(errs.ErrMissingConfig, "SemanticEdgeProvider", "GetEdgeWeight", "entity IDs are empty")
	}

	tiers := qualifyingTiers{}

	// Explicit-edge membership from the base's ACTUAL neighbor set (both
	// directions), NOT its numeric weight. The wired kvProvider.GetEdgeWeight
	// returns 1.0 for EVERY pair (gh#665), so keying "explicit edge exists" off
	// explicitEdgeWeight > 0 would make every pair explicit-dominant and leave the
	// sibling/system-peer/semantic tiers dead. Only an ACTUAL explicit edge is
	// strictly dominant; its weight is read from the base only once membership is
	// confirmed.
	isExplicit, err := p.base.isExplicitEdge(ctx, fromID, toID)
	if err != nil {
		return 0.0, errs.WrapTransient(err, "SemanticEdgeProvider", "GetEdgeWeight", "base provider error")
	}
	if isExplicit {
		explicit, err := p.base.explicitEdgeWeight(ctx, fromID, toID)
		if err != nil {
			return 0.0, errs.WrapTransient(err, "SemanticEdgeProvider", "GetEdgeWeight", "base provider error")
		}
		tiers.explicitWeight = explicit
	}

	// Sibling / system-peer membership reuse the EntityIDProvider's tested
	// predicates and enabled flags — the semantic-identity logic lives in one
	// place.
	if p.base.siblingsEnabled() && p.base.areSiblings(fromID, toID) {
		tiers.sibling = true
	}
	if p.base.systemPeersEnabled() && areSystemPeers(fromID, toID) {
		tiers.systemPeer = true
	}

	// Semantic membership from the mutual-kNN cache.
	mutual, err := p.isMutualNeighbor(ctx, fromID, toID)
	if err != nil {
		return 0.0, err
	}
	tiers.semantic = mutual

	return p.weights.resolve(tiers), nil
}

// isMutualNeighbor reports whether fromID and toID are a mutual-kNN pair.
func (p *SemanticEdgeProvider) isMutualNeighbor(ctx context.Context, fromID, toID string) (bool, error) {
	if err := p.ensureCache(ctx); err != nil {
		return false, err
	}
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	return p.mutualNeighbors[fromID][toID], nil
}

// ensureCache builds the mutual-kNN adjacency once per provider instance,
// mirroring EntityIDProvider.ensureTypePrefixCache. It queries the similarity
// finder for each entity's directed top-k set, then keeps only the mutual
// pairs: an edge A-B survives iff B is in A's top-k AND A is in B's top-k
// (B2 §1.3). A finder error for a single entity yields no semantic neighbors
// for that entity (the semantic tier is additive) — the readiness-aware
// distinction between a cold index and a genuine empty result is B2 §5, a later
// slice.
func (p *SemanticEdgeProvider) ensureCache(ctx context.Context) error {
	if p.cacheInitialized.Load() {
		return nil
	}

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	if p.cacheInitialized.Load() {
		return nil
	}

	ids, err := p.base.GetAllEntityIDs(ctx)
	if err != nil {
		return errs.WrapTransient(err, "SemanticEdgeProvider", "ensureCache", "get all entity IDs")
	}

	// Directed top-k sets: directed[a] holds every b that appears in a's top-k
	// similarity result at or above the threshold.
	directed := make(map[string]map[string]bool, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		neighbors, ferr := p.finder.SimilarNeighbors(ctx, id, p.threshold, p.k)
		p.queryCount.Add(1)
		if ferr != nil {
			// Additive tier: this entity contributes no semantic edges this
			// build. Do not fail the whole cache.
			if p.logger != nil {
				p.logger.Debug("semantic neighbor lookup failed, skipping entity",
					slog.String("entity_id", id),
					slog.Any("error", ferr))
			}
			continue
		}
		set := make(map[string]bool, len(neighbors))
		for _, n := range neighbors {
			if n == id {
				continue // never a self-edge
			}
			set[n] = true
		}
		if len(set) > 0 {
			directed[id] = set
		}
	}

	// Intersect the directed sets to keep only mutual pairs. The result is
	// symmetric by construction: A in mutual[B] iff B in mutual[A].
	mutual := make(map[string]map[string]bool, len(directed))
	for a, aSet := range directed {
		for b := range aSet {
			if directed[b] != nil && directed[b][a] {
				if mutual[a] == nil {
					mutual[a] = make(map[string]bool)
				}
				mutual[a][b] = true
			}
		}
	}

	p.mutualNeighbors = mutual
	p.cacheInitialized.Store(true)

	if p.logger != nil {
		p.logger.Debug("semantic edge cache initialized",
			slog.Int("total_entities", len(ids)),
			slog.Int("entities_with_mutual_edges", len(mutual)),
			slog.Int64("similarity_queries", p.queryCount.Load()))
	}

	return nil
}

// ClearCache resets the mutual-kNN adjacency and propagates the clear to the
// wrapped provider, mirroring EntityIDProvider.ClearCache.
func (p *SemanticEdgeProvider) ClearCache() {
	p.cacheMu.Lock()
	p.mutualNeighbors = make(map[string]map[string]bool)
	p.cacheInitialized.Store(false)
	p.cacheMu.Unlock()

	if p.base != nil {
		p.base.ClearCache()
	}
}
