package clustering

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

// ErrSemanticIndexNotReady signals that the similarity source could not be
// queried because the embedding index is not ready — a classified transient, NOT
// a genuine empty result. A SemanticNeighborFinder returns it (wrapped) so
// refreshCache ABORTS the whole mutual-kNN build rather than committing a
// partial/empty adjacency: the cache is not settled, the cycle degrades to
// structural-only, and a later cycle (gated on a re-confirmed ready index)
// rebuilds it fresh. The concrete ErrorCodeIndexNotReady classification lives at
// the component's finder adapter (which imports graph); this leaf package stays
// free of that import and reacts only to the neutral sentinel (B2 §5.1).
var ErrSemanticIndexNotReady = errors.New("clustering: semantic similarity index not ready")

// ErrSemanticQueryTransient signals that a SINGLE entity's similarity query
// failed with a non-index_not_ready transient (a timeout or no-responders on
// that one RPC, NOT the whole index being cold). Unlike ErrSemanticIndexNotReady
// it does NOT abort the whole refresh: refreshCache counts it toward the
// coverage-threshold (B2 §7.3 / #662) and leaves the entity UN-cached so it is
// re-queried next cycle, never latching a hollow "no semantic neighbors" for it.
//
// It exists because the readiness-aware finder adapter used to swallow every
// non-index_not_ready error into a bare empty result, making a per-entity
// timeout indistinguishable from a genuine "this entity has no close neighbors"
// — the exact confusion that let a burst of query timeouts latch a
// semantically-blind cache permanently (#662). Surfacing the transient class
// separately is what lets the coverage-threshold abort see it.
var ErrSemanticQueryTransient = errors.New("clustering: semantic similarity query failed transiently")

// errCoverageThresholdExceeded is the internal sentinel a first-build coverage
// abort settles so GetNeighbors/GetEdgeWeight degrade to structural for the
// cycle (there is no prior cache to serve). When a prior good cache DOES exist a
// coverage abort keeps it and settles a nil error instead (serve prior).
var errCoverageThresholdExceeded = errors.New("clustering: semantic-edge refresh transient-error fraction exceeded coverage threshold")

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

// maxTransientErrorFraction is the coverage-threshold for a mutual-kNN refresh
// (#662 / B2 §7.3). If more than this fraction of the CURRENT ENTITY SET returns
// a non-index_not_ready transient (timeout / no-responders) during a refresh,
// the refresh is ABORTED rather than committed, so a burst of query timeouts
// under embedding/8B saturation cannot latch a hollow (semantically-blind)
// cache. Below the threshold the refresh commits: the few transiently-errored
// entities hold no cache entry and are re-queried next cycle, so a single
// persistently-flaky entity (a fraction of 1/N, below the threshold for any
// corpus larger than 1/maxTransientErrorFraction ≈ 10 entities) cannot force a
// permanent full-rebuild loop. On a SMALLER corpus (N ≤ 10) a single flaky
// entity is 1/N ≥ threshold, so that cycle degrades to structural (keeps the
// prior cache) rather than committing the miss — still NO livelock (the epoch
// latch bounds each cycle to one refresh pass); the production target is ~175
// entities.
//
// The denominator is the WHOLE current entity set, not just the entities
// re-queried this cycle: on an unchanged-corpus cycle only the still-missing
// entities are re-queried, and dividing a single flaky entity by that tiny
// re-query subset would spuriously exceed the threshold and abort forever. The
// corpus-relative fraction is the reading that satisfies both the mass-timeout
// abort and the single-flaky-entity commit the design requires (#662).
const maxTransientErrorFraction = 0.10

// emptyDirectedSet is the IMMUTABLE marker stored for an entity that was queried
// and returned a definitive "no semantic neighbors" (a genuine empty result or a
// genuine per-entity miss). Its PRESENCE in the directed map means "queried this
// entity, it has no directed neighbors," which is distinct from an ABSENT key
// ("never queried / errored, re-query next cycle"). Shared and never mutated, so
// aliasing it across entities and cycles is safe.
var emptyDirectedSet = map[string]bool{}

// SemanticNeighborFinder returns the entity IDs semantically similar to
// entityID at or above threshold, capped at limit (the directed top-k set for
// mutual-kNN synthesis). It is deliberately a narrow, clustering-local
// interface so this package does not import graph/inference: the production
// wire satisfies it with a thin adapter over the component's existing
// graph.embedding.query.similar finder (no second similarity RPC — B2 §1.2).
//
// The adapter maps the embedding service's error classes onto the two package
// sentinels above: ErrSemanticIndexNotReady (whole index cold — abort the
// refresh) and ErrSemanticQueryTransient (this one query timed out — count it
// toward the coverage threshold). A genuine miss ("no embedding yet") and a
// genuine empty result both return `(nil, nil)`.
type SemanticNeighborFinder interface {
	SimilarNeighbors(ctx context.Context, entityID string, threshold float64, limit int) ([]string, error)
}

// SemanticEdgeMetrics records the cost of a mutual-kNN cache refresh (B2 §7.2).
// It is a narrow sink so this leaf package needs no prometheus import; the
// graph-clustering component satisfies it with a Prometheus-backed adapter over
// its shared registry. A nil recorder — the default, and every direct provider
// unit test — skips recording.
type SemanticEdgeMetrics interface {
	// ObserveBuildMs records one refresh's wall-clock duration in milliseconds.
	// Observed on EVERY refresh that reaches the query loop, including near-zero
	// reuse cycles, so the distribution shows how often an actual rebuild happens
	// versus a cheap all-reused pass.
	ObserveBuildMs(ms float64)
	// AddSimilarQueries adds the number of FindSimilar calls a refresh issued
	// (0 on a fully-reused, unchanged-corpus cycle — the query load §7.1 bounds).
	AddSimilarQueries(n int)
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

	// metrics records refresh cost (B2 §7.2). Nil-safe; set once via WithMetrics
	// at construction, before any goroutine reads the provider.
	metrics SemanticEdgeMetrics

	// active is the per-cycle toggle behind the structural-floor guarantee
	// (B2 §4). The component sets it from graph-embedding readiness BEFORE each
	// detection cycle: true only when the embedding index is ready. When false,
	// GetNeighbors/GetEdgeWeight behave EXACTLY as the wrapped EntityIDProvider and
	// never trigger a refresh — so the cache is only ever built from a READY
	// embedding index and can never latch to the empty structural-only set during
	// a cold-embedding window. Defaults true (see NewSemanticEdgeProvider): a
	// provider is fully functional unless the component deactivates it, which keeps
	// direct provider unit tests independent of the component's gate.
	active atomic.Bool

	// Per-cycle refresh coordination (B2 §7.1/§7.3). cycleEpoch is bumped by
	// BeginCycle once per detection cycle; targetRevision is the coarse
	// embedding-index watermark for the current cycle. Both are atomic because
	// BeginCycle (the detector loop, sole writer) runs outside the cache lock,
	// while refreshCache reads them. When BeginCycle is never called (direct
	// provider unit tests) the epoch stays 0 and the provider behaves like the
	// build-once cache it replaced: the first ACTIVE call builds, later calls
	// reuse, and a not-ready abort re-attempts on the next call.
	cycleEpoch     atomic.Uint64
	targetRevision atomic.Uint64

	// Cache state, guarded by cacheMu. directed holds each entity's directed
	// top-k set keyed by embedding revision via cacheRevision; PRESENCE of a key
	// means "queried this entity" (an empty value = queried, no neighbors), an
	// ABSENT key means "never queried / errored, re-query next cycle." The
	// symmetric mutual-kNN adjacency is recomputed from directed on every commit.
	cacheMu         sync.RWMutex
	directed        map[string]map[string]bool
	mutualNeighbors map[string]map[string]bool
	// cacheRevision is the coarse embedding-index watermark the committed cache
	// was built at. A refresh reuses the whole directed cache while it is
	// unchanged and re-queries only the entities the coarse signal cannot vouch
	// for (missing / previously-errored), so an unchanged corpus issues ZERO
	// FindSimilar calls per cycle (§7.1).
	cacheRevision uint64
	// cacheInitialized reports that a SERVABLE cache has been committed at least
	// once. Kept as an atomic.Bool of the same name the §4-5 activation tests
	// assert on. A not-ready abort never sets it; a coverage abort with no prior
	// cache never sets it (so both re-attempt / degrade rather than latch).
	cacheInitialized atomic.Bool
	// settled/settledEpoch/settledErr latch this cycle's refresh DECISION so one
	// build serves every GetNeighbors/GetEdgeWeight call in a detection run and a
	// coverage abort does not re-storm the finder mid-cycle. settledErr is nil
	// when a cache is servable this cycle (fresh commit OR a kept prior cache) and
	// a transient error when the cycle must degrade to structural-only. A
	// not-ready abort deliberately does NOT settle, so it re-attempts on the next
	// call (preserving the build-once tests' recover-on-next-call behavior).
	settled      bool
	settledEpoch uint64
	settledErr   error

	// abortedThisCycle latches true when a mutual-kNN refresh ABORTS mid-sweep on a
	// producer-wide not-ready/fatal signal (ErrSemanticIndexNotReady, surfaced by
	// the readiness-aware finder adapter). It is the per-cycle no-retry guard
	// (B2 §4, Codex P1#2): because a not-ready abort deliberately does NOT settle
	// (§7 recover-within-cycle), this latch is what makes the abort CYCLE-SCOPED —
	// once a refresh aborts, the REMAINDER of this detection cycle stays
	// structural-only instead of re-issuing the O(N) similarity sweep on every
	// subsequent lookup. Without it a single DetectCommunities run would (a) let a
	// mid-cycle embedding recovery split the partition — early entities vote
	// structurally, later ones semantically — and (b) re-run the whole sweep
	// repeatedly, saturating the similarity endpoint while the index stays cold.
	// SetActive clears it at the next cycle boundary, so a later ready cycle rebuilds
	// fresh (the no-latch guarantee is preserved). It is DISTINCT from a coverage
	// abort, which settles and keeps the prior cache; only the not-ready/fatal
	// producer-wide sentinel latches here. atomic: set under cacheMu during a
	// refresh, read lock-free at the top of refreshCache and by AppliedSemanticEdges.
	abortedThisCycle atomic.Bool

	// degradedThisCycle latches true when a COVERAGE-threshold abort (§7.3) settled
	// with NO prior cache to serve — the first-cold-cycle / >maxTransientErrorFraction
	// burst case — so the cycle served structural-only despite being active and not
	// producer-wide-aborted. It is the actual-mode complement to abortedThisCycle for
	// the refreshable model: a coverage abort that KEEPS a prior cache serves real
	// (prior-epoch) semantic edges and does NOT set this; only a coverage abort that
	// degraded to structural-only does. AppliedSemanticEdges reads it so
	// semantic_edges_applied reports 0 for a structurally-degraded cycle rather than a
	// misleading 1. Distinct from a healthy build that committed zero mutual pairs —
	// that SERVED the (empty) semantic tier and stays applied=1 (the #618 "ran, found
	// nothing" distinction), so a successful commit CLEARS this latch. Reset on
	// SetActive/ClearCache like abortedThisCycle; atomic (set under cacheMu, read
	// lock-free by AppliedSemanticEdges).
	degradedThisCycle atomic.Bool

	// queryCount is a lifetime total of SimilarNeighbors calls, for the debug
	// log; the authoritative per-cycle load signal is the metrics recorder.
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
	p := &SemanticEdgeProvider{
		base:            base,
		finder:          finder,
		weights:         weights,
		k:               params.K,
		threshold:       params.Threshold,
		logger:          logger,
		directed:        make(map[string]map[string]bool),
		mutualNeighbors: make(map[string]map[string]bool),
	}
	// Default ACTIVE. In production the component overrides this from embedding
	// readiness before the first detection cycle reads the provider; the default
	// only governs direct provider unit tests, which expect the tier to apply
	// without wiring the component's gate.
	p.active.Store(true)
	return p
}

// WithMetrics attaches the refresh-cost recorder (B2 §7.2) and returns the
// provider for chaining. Call once at construction, before the provider is
// shared with any goroutine; a nil recorder leaves recording off.
func (p *SemanticEdgeProvider) WithMetrics(m SemanticEdgeMetrics) *SemanticEdgeProvider {
	p.metrics = m
	return p
}

// SetActive enables or disables the semantic tier WITHOUT advancing the cycle epoch
// — the standalone active toggle (B2 §4). The per-cycle production transition is
// BeginCycle's job now (Codex P2#4), which sets active atomically WITH the epoch +
// latch reset; SetActive is the narrow "flip active in place" primitive the §4
// activation tests drive to exercise the structural-only path in isolation, and the
// shared-provider race test uses to model an out-of-band toggle. When inactive,
// GetNeighbors and GetEdgeWeight behave exactly as the wrapped EntityIDProvider and
// never trigger a mutual-kNN refresh, so the cache is only ever built from a ready
// embedding index (the no-not-ready-latch guarantee). Concurrency-safe via the
// atomic flag.
func (p *SemanticEdgeProvider) SetActive(active bool) {
	// Clear the per-cycle abort/degrade latches so a reactivated tier gets a fresh
	// refresh attempt after a prior cycle aborted on a cold/reset embedding index
	// (B2 §4, Codex P1#2). Order: clear the latches BEFORE flipping active, so a
	// concurrent B3 reader that observes active=true never also observes a stale abort
	// latch from the previous cycle.
	p.abortedThisCycle.Store(false)
	p.degradedThisCycle.Store(false)
	p.active.Store(active)
}

// IsActive reports the current per-cycle tier state (observability / tests).
func (p *SemanticEdgeProvider) IsActive() bool { return p.active.Load() }

// AppliedSemanticEdges reports the ACTUAL completed mode of the cycle that just ran:
// true iff the tier was active AND the cycle actually SERVED semantic edges in the vote
// — it was neither producer-wide-aborted (abortedThisCycle) nor coverage-degraded to
// structural-only with nothing to serve (degradedThisCycle). It is read AFTER detection
// and is deliberately distinct from the preflight readiness INTENT SetActive stamps
// BEFORE it: an intended-active cycle that aborts/degrades mid-refresh serves
// structural-only, and the semantic_edges_applied signal must report what happened, not
// what was intended (B2 §4, Codex P1#2 + §7.3 seam close).
//
// The four cases the two latches resolve (all gated on active):
//
//   - producer-wide not-ready/fatal abort -> FALSE (abortedThisCycle). In the
//     refreshable model this leaves the prior epoch's adjacency populated but UN-served
//     (refreshCache returns the sentinel, so lookups degrade to structural), so a
//     len(mutualNeighbors)>0 check would falsely report applied — the latch is the truth.
//   - coverage-threshold abort that KEEPS a prior committed cache -> TRUE: the
//     prior epoch's semantic edges are in the vote. degradedThisCycle keys on
//     whether a prior refresh committed (cacheInitialized), NOT on emptiness — an
//     initialized-but-empty prior is still "ran, found nothing" (applied), so it
//     correctly reports TRUE too (neither latch set).
//   - coverage-threshold abort with NO prior cache (first cold cycle / >threshold burst)
//     -> FALSE (degradedThisCycle): the cycle served structural-only. This is the seam
//     the §7.3 mechanism otherwise left reporting a misleading 1.
//   - a healthy build that committed ZERO mutual pairs -> TRUE: the tier RAN and its
//     (empty) semantic edges were in the vote — the #618 "semantics ran, found nothing"
//     distinction the semantic_edges_applied metric exists to preserve (a completed
//     commit clears degradedThisCycle).
//
// Both latches are lock-free atomics reset per cycle by SetActive, so AppliedSemanticEdges
// stays a lock-free read (a concurrent B3 enhancement worker can call it safely).
func (p *SemanticEdgeProvider) AppliedSemanticEdges() bool {
	return p.active.Load() && !p.abortedThisCycle.Load() && !p.degradedThisCycle.Load()
}

// BeginCycle performs the ENTIRE per-cycle transition in one synchronized step
// (B2 §7.1, Codex P2#4): it records the coarse embedding-index watermark this
// cycle's refresh keys off, sets the tier active/inactive from the readiness
// verdict, resets the per-cycle abort/degrade latches, and LAST advances the cycle
// epoch so refreshCache does exactly one refresh pass per cycle (reusing unchanged
// directed sets and re-querying only what the watermark cannot vouch for). The
// component calls it from applySemanticGate before detection reads the provider.
//
// The store ORDER is load-bearing and must not be reshuffled: the epoch bump is the
// LAST atomic write, and every reader loads the epoch BEFORE the active flag / abort
// latches (GetNeighbors gates on active then refreshCache reads epoch-then-latches).
// Go's sync/atomic operations are sequentially consistent, so a reader that observes
// the NEW epoch is guaranteed to observe the reset latches + new active/revision too
// — it can never see a torn (new-epoch, prior-cycle-verdict/stale-latch) state. This
// closes the window the previous two-call BeginCycle-then-SetActive sequence left,
// where B3's concurrent enhancement-worker reader could observe the new epoch under
// the prior cycle's active verdict or an un-reset abort latch. Race-safe via atomics:
// the detector loop is the sole writer; B3's worker is a read-only consumer.
func (p *SemanticEdgeProvider) BeginCycle(embeddingRevision uint64, active bool) {
	p.targetRevision.Store(embeddingRevision)
	p.active.Store(active)
	p.abortedThisCycle.Store(false)
	p.degradedThisCycle.Store(false)
	p.cycleEpoch.Add(1) // MUST be last: publishes the whole transition to readers.
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

	// Structural-only cycle (B2 §4): when the component has deactivated the tier
	// for this cycle — graph-embedding is not ready — behave EXACTLY as the
	// wrapped EntityIDProvider and, critically, do NOT trigger a refresh. Building
	// it now, from a not-ready embedding index, would latch an empty/partial
	// mutual-kNN set; deferring the build to a ready cycle is what prevents that.
	if !p.active.Load() {
		return p.base.GetNeighbors(ctx, entityID, direction)
	}

	base, err := p.base.GetNeighbors(ctx, entityID, direction)
	if err != nil {
		return nil, errs.WrapTransient(err, "SemanticEdgeProvider", "GetNeighbors", "base provider error")
	}

	if err := p.refreshCache(ctx); err != nil {
		// The semantic tier is additive over an always-valid structural floor:
		// a refresh failure with nothing servable degrades to structural neighbors,
		// it does not fail the whole traversal.
		if p.logger != nil {
			if errors.Is(err, ErrSemanticIndexNotReady) {
				// Expected structural-only degrade (cold/reset embedding index, or the
				// per-cycle abort latch): the component surfaces this ONCE per transition
				// via semantic_edges_applied, so log at Debug — a whole cold cycle must
				// not flood WARN once per entity (B2 §4, Codex P1#2).
				p.logger.Debug("semantic edge tier structural-only this cycle (embedding index not ready)",
					slog.String("entity_id", entityID))
			} else {
				p.logger.Warn("semantic edge cache refresh failed, returning structural neighbors only",
					slog.String("entity_id", entityID),
					slog.Any("error", err))
			}
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

	// Structural-only cycle (B2 §4): delegate straight to the wrapped
	// EntityIDProvider — same first-match cascade over the (rebalanced) structural
	// tiers, no semantic tier, no refresh.
	if !p.active.Load() {
		return p.base.GetEdgeWeight(ctx, fromID, toID)
	}

	tiers := qualifyingTiers{}

	// Explicit-edge membership from the base's ACTUAL neighbor set (both
	// directions). Since gh#665 the wired kvProvider.GetEdgeWeight is
	// membership-correct (1.0 only for a real explicit neighbor, 0 otherwise), so
	// keying "explicit edge exists" off explicitEdgeWeight > 0 would also be correct
	// today. Consulting the neighbor set keeps the membership decision independent of
	// the base's numeric weight contract, so this max-across-tiers resolution stays
	// correct even if explicit edges later carry a non-unit confidence: only an
	// ACTUAL explicit edge is strictly dominant, and its weight is read from the base
	// only once membership is confirmed.
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

	// Semantic membership from the mutual-kNN cache. A refresh failure — including
	// the not-ready abort (refreshCache returning a transient when the embedding
	// index went cold mid-build) — degrades THIS pair to its structural tiers
	// rather than failing the cycle. LPA falls back to an unweighted 1.0 for an
	// edge whose weight lookup errors (lpa.go computeNewLabel), which would corrupt
	// the structural-only partition; resolving structural-only keeps it a clean
	// Tier-0/1 partition.
	if mutual, mErr := p.isMutualNeighbor(ctx, fromID, toID); mErr != nil {
		if p.logger != nil {
			if errors.Is(mErr, ErrSemanticIndexNotReady) {
				// Expected structural-only degrade (see GetNeighbors): Debug, not a
				// per-pair WARN flood. #618 is surfaced once by the component.
				p.logger.Debug("semantic membership structural-only this cycle (embedding index not ready)",
					slog.String("from_id", fromID),
					slog.String("to_id", toID))
			} else {
				p.logger.Warn("semantic membership unavailable; resolving structural tiers only",
					slog.String("from_id", fromID),
					slog.String("to_id", toID),
					slog.Any("error", mErr))
			}
		}
	} else {
		tiers.semantic = mutual
	}

	return p.weights.resolve(tiers), nil
}

// isMutualNeighbor reports whether fromID and toID are a mutual-kNN pair.
func (p *SemanticEdgeProvider) isMutualNeighbor(ctx context.Context, fromID, toID string) (bool, error) {
	if err := p.refreshCache(ctx); err != nil {
		return false, err
	}
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	return p.mutualNeighbors[fromID][toID], nil
}

// refreshCache maintains the mutual-kNN adjacency across detection cycles
// (B2 §7). It replaces the old build-once cache: per ACTIVE cycle it reuses the
// directed top-k set of every entity the coarse embedding-index watermark can
// vouch for (unchanged since the last commit) and re-queries only the entities
// it cannot (new / previously-errored), then recomputes the symmetric mutual
// intersection in-memory. An unchanged corpus therefore issues ZERO FindSimilar
// calls (§7.1). It runs at most once per cycle: the epoch latch settles the
// decision so every later GetNeighbors/GetEdgeWeight call in the run reuses it.
//
// Error handling makes the #662 latch structurally impossible:
//
//   - ErrSemanticIndexNotReady (whole index cold, race past the §4 gate): abort
//     the whole refresh WITHOUT settling — degrade to structural this cycle and
//     re-attempt next call / cycle, never committing a partial cache (B2 §5.1).
//   - ErrSemanticQueryTransient (one entity's query timed out): count it toward
//     the coverage-threshold and leave the entity un-cached; if the transient
//     fraction over the corpus exceeds maxTransientErrorFraction, ABORT and keep
//     the prior good cache (or degrade if there is none), else COMMIT and
//     re-query the few missing entities next cycle (B2 §7.3).
//   - a genuine miss / empty (this entity has no embedding / no close neighbors):
//     a definitive answer cached as "no neighbors," re-queried only when the
//     coarse watermark advances.
//
// Because the cache is per-cycle-refreshable, no transient-errored result is
// ever the permanent state: the next cycle re-queries it. This is the same
// mechanism that delivers §7.1's reuse and §7.3's no-latch — a hollow cache
// cannot persist because it is rebuilt whenever the watermark moves or an entry
// is missing.
func (p *SemanticEdgeProvider) refreshCache(ctx context.Context) error {
	epoch := p.cycleEpoch.Load()

	// Fast path: this cycle's refresh decision is already settled — reuse it
	// without re-querying (one build serves every call in a detection run).
	p.cacheMu.RLock()
	if p.settled && p.settledEpoch == epoch {
		err := p.settledErr
		p.cacheMu.RUnlock()
		return err
	}
	p.cacheMu.RUnlock()

	// Per-cycle no-retry latch (B2 §4, Codex P1#2): a refresh already aborted this cycle
	// on a producer-wide not-ready/fatal signal, which deliberately does NOT settle (so
	// the epoch fast-path above cannot catch it). Do NOT re-issue the O(N) similarity
	// sweep — stay structural-only for the rest of the cycle. SetActive clears the latch
	// at the next cycle boundary so a later ready cycle rebuilds fresh. Read lock-free
	// (atomic) after releasing the read lock; re-checked under the write lock below.
	if p.abortedThisCycle.Load() {
		return ErrSemanticIndexNotReady
	}

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if p.settled && p.settledEpoch == epoch {
		return p.settledErr
	}
	if p.abortedThisCycle.Load() {
		return ErrSemanticIndexNotReady
	}

	target := p.targetRevision.Load()
	signalChanged := !p.cacheInitialized.Load() || p.cacheRevision != target

	ids, err := p.base.GetAllEntityIDs(ctx)
	if err != nil {
		// Enumeration failed: do NOT settle (re-attempt next call), keep any prior
		// cache. GetNeighbors/GetEdgeWeight degrade to structural for this call.
		return errs.WrapTransient(err, "SemanticEdgeProvider", "refreshCache", "get all entity IDs")
	}

	start := time.Now()
	newDirected := make(map[string]map[string]bool, len(ids))
	queried := 0
	transientErrs := 0

	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			// Cancelled mid-build: do NOT settle, keep prior cache.
			return err
		}
		// Reuse a prior directed set when the coarse signal is unchanged and we
		// hold a definitive prior answer for this entity (present == queried,
		// possibly empty). A missing entry (new / previously-errored) is re-queried.
		if !signalChanged {
			if prev, ok := p.directed[id]; ok {
				newDirected[id] = prev
				continue
			}
		}
		neighbors, ferr := p.finder.SimilarNeighbors(ctx, id, p.threshold, p.k)
		queried++
		p.queryCount.Add(1)
		if ferr != nil {
			if errors.Is(ferr, ErrSemanticIndexNotReady) {
				// The embedding index passed the component's readiness gate but went cold
				// (or issued a producer-wide reset — the adapter maps both onto this
				// sentinel) mid-refresh, a race past the §4 gate. ABORT the whole refresh
				// WITHOUT settling: cacheInitialized/the prior cache are left untouched, so
				// GetNeighbors/GetEdgeWeight degrade to structural-only for this cycle and a
				// later ready cycle rebuilds fresh (B2 §5.1). LATCH abortedThisCycle so the
				// REST of this cycle stays structural-only rather than re-sweeping on the
				// next lookup (B2 §4, Codex P1#2): because a not-ready abort does not settle,
				// this latch — not the epoch fast-path — is what bounds the cycle to one
				// aborted sweep and keeps the partition consistent even if the index recovers
				// mid-cycle. SetActive clears it at the next cycle boundary.
				p.abortedThisCycle.Store(true)
				p.recordBuild(start, queried)
				return errs.WrapTransient(ferr, "SemanticEdgeProvider", "refreshCache",
					"embedding index not ready mid-build")
			}
			if errors.Is(ferr, ErrSemanticQueryTransient) {
				// A per-entity transient (timeout / no-responders), NOT the whole
				// index. Count it toward the coverage threshold and leave the entity
				// UN-cached so it is re-queried next cycle — never latch a hollow empty
				// for it (#662 / B2 §7.3).
				transientErrs++
				// Liveness early-abort (Codex P1#1): the coverage budget is over the
				// WHOLE corpus (len(ids)), so once transientErrs > floor(0.10*len(ids))
				// the fraction is IRREVERSIBLY exceeded — no later result can bring it
				// back under, and overCoverageThreshold is monotonic in transientErrs.
				// Stop the sweep the instant that happens rather than issuing the
				// remaining queries (on ~175 entities × the 30s finder timeout that is
				// ~87 min of wasted work while HOLDING the cache write lock and blocking
				// detection). Record the partial build and settle the abort right here;
				// keep-prior semantics are unchanged (settleAbort keeps any prior cache
				// and degrades only when there is none).
				if p.overCoverageThreshold(transientErrs, len(ids)) {
					p.recordBuild(start, queried)
					return p.settleAbort(epoch, transientErrs, len(ids))
				}
				continue
			}
			// A genuine miss (non-transient: this entity has no embedding yet) is a
			// definitive "no semantic neighbors." Cache it as queried-empty so it is
			// NOT re-queried every cycle while the corpus is unchanged; it re-queries
			// when the coarse watermark advances (which it does when the entity's
			// embedding lands).
			newDirected[id] = emptyDirectedSet
			if p.logger != nil {
				p.logger.Debug("semantic neighbor lookup returned no neighbors, caching empty",
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
		newDirected[id] = set
	}

	p.recordBuild(start, queried)

	// No post-loop coverage-threshold check is needed: the in-loop early-abort
	// (Codex P1#1) fires the instant transientErrs crosses the budget, so any
	// over-threshold refresh has already returned via settleAbort above. Reaching
	// here means transientErrs is within budget — commit the good majority and
	// re-query the few missing/errored entities next cycle (#662 / B2 §7.3).

	// Commit: recompute the symmetric mutual-kNN intersection from the directed
	// sets and publish the new cache.
	p.directed = newDirected
	p.mutualNeighbors = computeMutual(newDirected)
	p.cacheRevision = target
	p.cacheInitialized.Store(true)
	// A successful commit served the semantic tier this cycle (even a zero-mutual-pair
	// build ran and put its empty tier in the vote — #618 "ran, found nothing" = applied).
	p.degradedThisCycle.Store(false)
	p.settle(epoch, nil)

	if p.logger != nil {
		p.logger.Debug("semantic edge cache refreshed",
			slog.Int("total_entities", len(ids)),
			slog.Int("queried_this_cycle", queried),
			slog.Int("transient_errors", transientErrs),
			slog.Int("entities_with_mutual_edges", len(p.mutualNeighbors)),
			slog.Uint64("embedding_revision", target),
			slog.Bool("signal_changed", signalChanged),
			slog.Int64("lifetime_queries", p.queryCount.Load()))
	}

	return nil
}

// settle latches this cycle's refresh decision so later calls in the same run
// reuse it. Must be called under cacheMu.
func (p *SemanticEdgeProvider) settle(epoch uint64, err error) {
	p.settled = true
	p.settledEpoch = epoch
	p.settledErr = err
}

// settleAbort records a coverage-threshold abort. It keeps the PRIOR committed
// cache (never overwriting directed/mutual/cacheRevision), so a burst of query
// timeouts serves the last good semantic edges rather than a hollow set, and the
// next cycle retries. With no prior cache to serve it settles a transient error
// so the cycle degrades to structural-only. Must be called under cacheMu; it
// returns the settled error for refreshCache to hand back to the caller.
func (p *SemanticEdgeProvider) settleAbort(epoch uint64, transientErrs, total int) error {
	hadPrior := p.cacheInitialized.Load()
	var abortErr error
	if !hadPrior {
		abortErr = errs.WrapTransient(errCoverageThresholdExceeded, "SemanticEdgeProvider",
			"refreshCache", "transient-error fraction over coverage threshold; no prior cache to serve")
	}
	// Actual-mode (§7.3 seam): a coverage abort that KEEPS a non-empty prior cache still
	// serves real semantic edges (applied=1); one with NO prior cache degraded this cycle
	// to structural-only (applied=0). Stamp the latch AppliedSemanticEdges reads.
	p.degradedThisCycle.Store(!hadPrior)
	p.settle(epoch, abortErr)
	if p.logger != nil {
		p.logger.Warn("semantic edge refresh aborted on coverage threshold; keeping prior cache",
			slog.Int("transient_errors", transientErrs),
			slog.Int("total_entities", total),
			slog.Float64("threshold", maxTransientErrorFraction),
			slog.Bool("had_prior_cache", hadPrior))
	}
	return abortErr
}

// overCoverageThreshold reports whether the transient-error fraction over the
// whole current entity set exceeds maxTransientErrorFraction. The corpus-wide
// denominator is what keeps a single persistently-flaky entity (1/N) below the
// threshold — it commits and is re-queried next cycle rather than aborting the
// refresh forever.
func (p *SemanticEdgeProvider) overCoverageThreshold(transientErrs, total int) bool {
	if transientErrs == 0 || total <= 0 {
		return false
	}
	return float64(transientErrs)/float64(total) > maxTransientErrorFraction
}

// recordBuild reports the refresh's duration and query count to the metrics sink
// (B2 §7.2). Nil-safe.
func (p *SemanticEdgeProvider) recordBuild(start time.Time, queried int) {
	if p.metrics == nil {
		return
	}
	p.metrics.ObserveBuildMs(float64(time.Since(start).Microseconds()) / 1000.0)
	p.metrics.AddSimilarQueries(queried)
}

// computeMutual intersects the directed top-k sets to keep only mutual pairs:
// an edge A-B survives iff B is in A's top-k AND A is in B's top-k (B2 §1.3).
// The result is symmetric by construction — A in mutual[B] iff B in mutual[A].
func computeMutual(directed map[string]map[string]bool) map[string]map[string]bool {
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
	return mutual
}

// ResetEdgeCache forwards the per-cycle explicit-edge cache reset down the
// provider chain (gh#666). The mutual-kNN cache has its own per-cycle lifetime
// (BeginCycle/refreshCache), so this only forwards to the wrapped EntityIDProvider.
func (p *SemanticEdgeProvider) ResetEdgeCache() {
	if p.base != nil {
		p.base.ResetEdgeCache()
	}
}

// ClearCache resets the mutual-kNN adjacency and propagates the clear to the
// wrapped provider, mirroring EntityIDProvider.ClearCache.
func (p *SemanticEdgeProvider) ClearCache() {
	p.cacheMu.Lock()
	p.directed = make(map[string]map[string]bool)
	p.mutualNeighbors = make(map[string]map[string]bool)
	p.cacheRevision = 0
	p.cacheInitialized.Store(false)
	p.settled = false
	p.settledEpoch = 0
	p.settledErr = nil
	// A full cache clear is a fresh start: drop the per-cycle abort/degrade latches too so
	// the next refresh attempt is not suppressed or mis-reported by stale state from
	// before the clear.
	p.abortedThisCycle.Store(false)
	p.degradedThisCycle.Store(false)
	p.cacheMu.Unlock()

	if p.base != nil {
		p.base.ClearCache()
	}
}
