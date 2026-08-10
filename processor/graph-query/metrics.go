// Package graphquery provides Prometheus metrics for graph-query component.
package graphquery

import (
	"sync"

	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// queryMetrics holds Prometheus metrics for the graph-query component.
type queryMetrics struct {
	communityCacheHits     prometheus.Counter
	communityCacheMisses   prometheus.Counter
	communityStorageHits   prometheus.Counter
	communityStorageMisses prometheus.Counter
	communityLookups       *prometheus.CounterVec
	localSearchRequests    prometheus.Counter
	globalSearchRequests   prometheus.Counter

	// globalSearch path observability — added to surface the bug class where
	// queries silently route to a dead-end strategy / get filtered to empty.
	// Without these, "globalSearch returned count=0" is only visible via user
	// reports.
	globalSearchStrategy       *prometheus.CounterVec // labels: strategy
	globalSearchResponseCount  prometheus.Histogram   // distribution of returned entity counts
	globalSearchEmpty          *prometheus.CounterVec // labels: strategy, reason
	globalSearchTierTransition *prometheus.CounterVec // labels: from, to, reason
	globalSearchHitsDropped    *prometheus.CounterVec // labels: stage (threshold|type_filter)
	globalSearchDegraded       *prometheus.CounterVec // labels: reason — closes beta.45 debt

	// classifierGarbage tracks defenses firing against classifier output
	// emitted verbatim from a response template (e.g. qwen3-0.6b returning
	// `<entity_type>` as a literal type filter, or replacing a proper-noun
	// query with a generic example string). High rate indicates the
	// classifier model is too thin for the workload — operators should
	// upsize or relax single-token-bypass thresholds.
	classifierGarbage *prometheus.CounterVec // labels: type (template_filter|query_dropped)

	// typeFilterFallback counts graceful type-filter fallbacks (#645): the
	// classifier's type filters would have zeroed a NON-EMPTY semantic result
	// set, so the unfiltered set was kept. Deliberately NOT under
	// classifierGarbage — a zero match only proves no candidate in the current
	// retrieval window matched, which a valid inferred type can also cause; it
	// is not evidence the classifier emitted garbage.
	typeFilterFallback prometheus.Counter
}

// Package-level metrics (registered once to avoid duplicate registration errors)
var (
	metricsOnce sync.Once
	metrics     *queryMetrics
)

// getMetrics returns the singleton metrics instance, creating and registering it if needed.
func getMetrics(registry *metric.MetricsRegistry) *queryMetrics {
	metricsOnce.Do(func() {
		metrics = &queryMetrics{
			communityCacheHits: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "community_cache_hits_total",
				Help:      "Total community cache hits (entity found in cache)",
			}),

			communityCacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "community_cache_misses_total",
				Help:      "Total community cache misses (entity not in cache, falling back to storage)",
			}),

			communityStorageHits: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "community_storage_hits_total",
				Help:      "Total community storage fallback hits (entity found via NATS query)",
			}),

			communityStorageMisses: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "community_storage_misses_total",
				Help:      "Total community storage fallback misses (entity not in any community)",
			}),

			communityLookups: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "community_lookups_total",
				Help:      "Total community lookups by result type",
			}, []string{"result"}), // result: cache_hit, storage_hit, not_found

			localSearchRequests: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "local_search_requests_total",
				Help:      "Total GraphRAG local search requests",
			}),

			globalSearchRequests: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_requests_total",
				Help:      "Total GraphRAG global search requests",
			}),

			globalSearchStrategy: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_strategy_total",
				Help:      "Global search requests by resolved strategy (entity_lookup|pathrag|semantic|temporal|spatial|graphrag)",
			}, []string{"strategy"}),

			globalSearchResponseCount: prometheus.NewHistogram(prometheus.HistogramOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_response_count",
				Help:      "Distribution of entity counts returned by globalSearch responses (0 bucket flags 'returned nothing')",
				Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 250, 500, 1000},
			}),

			globalSearchEmpty: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_empty_total",
				Help:      "Global search requests that returned zero entities, by strategy and reason (no_communities|no_semantic_hits|threshold_filter|type_filter|relevance_filter|semantic_unavailable)",
			}, []string{"strategy", "reason"}),

			globalSearchTierTransition: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_tier_transition_total",
				Help:      "Tier transitions inside graphrag/path strategies, from=tier1 to=tier2 reason=(empty_hits|err)",
			}, []string{"from", "to", "reason"}),

			globalSearchHitsDropped: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_hits_dropped_total",
				Help:      "Semantic hits dropped during globalSearch processing, labelled by the stage that dropped them (threshold|type_filter)",
			}, []string{"stage"}),

			globalSearchDegraded: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "global_search_degraded_total",
				Help:      "Global search responses returned with Degraded=true, by reason (answer_synthesis_timeout|answer_synthesis_cancelled|answer_synthesis_error|community_cache_not_ready).",
			}, []string{"reason"}),

			classifierGarbage: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "classifier_garbage_total",
				Help:      "Defenses triggered against malformed classifier output: template_filter (literal placeholder in type_filters) or query_dropped (single-token original query replaced by classifier-emitted template). High rate = classifier model too thin for workload.",
			}, []string{"type"}),

			typeFilterFallback: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "semstreams",
				Subsystem: "graph_query",
				Name:      "type_filter_fallback_total",
				Help:      "Times the classifier type-filter would have zeroed a NON-EMPTY semantic result set and fell back to the unfiltered set (#645). Neutral by design: a valid inferred corpus type may simply have no candidate in the retrieval window, so this is NOT necessarily malformed classifier output — distinct from classifier_garbage_total.",
			}),
		}

		// Register metrics with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounter("graph-query", "community_cache_hits_total", metrics.communityCacheHits)
			_ = registry.RegisterCounter("graph-query", "community_cache_misses_total", metrics.communityCacheMisses)
			_ = registry.RegisterCounter("graph-query", "community_storage_hits_total", metrics.communityStorageHits)
			_ = registry.RegisterCounter("graph-query", "community_storage_misses_total", metrics.communityStorageMisses)
			_ = registry.RegisterCounterVec("graph-query", "community_lookups_total", metrics.communityLookups)
			_ = registry.RegisterCounter("graph-query", "local_search_requests_total", metrics.localSearchRequests)
			_ = registry.RegisterCounter("graph-query", "global_search_requests_total", metrics.globalSearchRequests)
			_ = registry.RegisterCounterVec("graph-query", "global_search_strategy_total", metrics.globalSearchStrategy)
			_ = registry.RegisterHistogram("graph-query", "global_search_response_count", metrics.globalSearchResponseCount)
			_ = registry.RegisterCounterVec("graph-query", "global_search_empty_total", metrics.globalSearchEmpty)
			_ = registry.RegisterCounterVec("graph-query", "global_search_tier_transition_total", metrics.globalSearchTierTransition)
			_ = registry.RegisterCounterVec("graph-query", "global_search_hits_dropped_total", metrics.globalSearchHitsDropped)
			_ = registry.RegisterCounterVec("graph-query", "global_search_degraded_total", metrics.globalSearchDegraded)
			_ = registry.RegisterCounterVec("graph-query", "classifier_garbage_total", metrics.classifierGarbage)
			_ = registry.RegisterCounter("graph-query", "type_filter_fallback_total", metrics.typeFilterFallback)
		} else {
			// Fallback to default prometheus registry for testing
			_ = prometheus.DefaultRegisterer.Register(metrics.communityCacheHits)
			_ = prometheus.DefaultRegisterer.Register(metrics.communityCacheMisses)
			_ = prometheus.DefaultRegisterer.Register(metrics.communityStorageHits)
			_ = prometheus.DefaultRegisterer.Register(metrics.communityStorageMisses)
			_ = prometheus.DefaultRegisterer.Register(metrics.communityLookups)
			_ = prometheus.DefaultRegisterer.Register(metrics.localSearchRequests)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchRequests)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchStrategy)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchResponseCount)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchEmpty)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchTierTransition)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchHitsDropped)
			_ = prometheus.DefaultRegisterer.Register(metrics.globalSearchDegraded)
			_ = prometheus.DefaultRegisterer.Register(metrics.classifierGarbage)
			_ = prometheus.DefaultRegisterer.Register(metrics.typeFilterFallback)
		}
	})
	return metrics
}

// recordCacheHit records a community cache hit.
func (m *queryMetrics) recordCacheHit() {
	m.communityCacheHits.Inc()
	m.communityLookups.WithLabelValues("cache_hit").Inc()
}

// recordCacheMiss records a community cache miss (triggering storage fallback).
func (m *queryMetrics) recordCacheMiss() {
	m.communityCacheMisses.Inc()
}

// recordStorageHit records a successful storage fallback lookup.
func (m *queryMetrics) recordStorageHit() {
	m.communityStorageHits.Inc()
	m.communityLookups.WithLabelValues("storage_hit").Inc()
}

// recordStorageMiss records a failed storage fallback (entity not in any community).
func (m *queryMetrics) recordStorageMiss() {
	m.communityStorageMisses.Inc()
	m.communityLookups.WithLabelValues("not_found").Inc()
}

// recordLocalSearch records a local search request.
func (m *queryMetrics) recordLocalSearch() {
	m.localSearchRequests.Inc()
}

// recordGlobalSearch records a global search request.
func (m *queryMetrics) recordGlobalSearch() {
	m.globalSearchRequests.Inc()
}

// recordGlobalSearchStrategy records the resolved dispatch strategy for a globalSearch request.
func (m *queryMetrics) recordGlobalSearchStrategy(strategy string) {
	m.globalSearchStrategy.WithLabelValues(strategy).Inc()
}

// observeGlobalSearchResponseCount records the entity count returned in a globalSearch response.
// The 0 bucket flags "returned nothing" — sudden ratio shifts toward 0 indicate the bug class
// where queries silently route to a dead-end strategy or get filtered to empty.
func (m *queryMetrics) observeGlobalSearchResponseCount(count int) {
	m.globalSearchResponseCount.Observe(float64(count))
}

// recordGlobalSearchEmpty records a globalSearch response with zero entities, with the strategy
// that produced it and the proximate reason (no_communities, no_semantic_hits, threshold_filter,
// type_filter, relevance_filter, semantic_unavailable).
func (m *queryMetrics) recordGlobalSearchEmpty(strategy, reason string) {
	m.globalSearchEmpty.WithLabelValues(strategy, reason).Inc()
}

// recordGlobalSearchTierTransition records a fall-through inside graphrag/path strategies, e.g.
// from semantic to community-text-based when semantic returns empty.
func (m *queryMetrics) recordGlobalSearchTierTransition(from, to, reason string) {
	m.globalSearchTierTransition.WithLabelValues(from, to, reason).Inc()
}

// recordGlobalSearchHitsDropped records semantic hits dropped by an internal filter stage,
// labelled by the stage that dropped them (threshold or type_filter). Use Add(N) for the count.
func (m *queryMetrics) recordGlobalSearchHitsDropped(stage string, n int) {
	if n > 0 {
		m.globalSearchHitsDropped.WithLabelValues(stage).Add(float64(n))
	}
}

// recordGlobalSearchDegraded records a globalSearch response returned with Degraded=true.
// Reasons are the bounded answer-synthesis classes plus community_cache_not_ready
// when requested community enrichment cannot be served from one valid generation.
func (m *queryMetrics) recordGlobalSearchDegraded(reason string) {
	m.globalSearchDegraded.WithLabelValues(reason).Inc()
}

// recordClassifierGarbage records a defense firing against malformed classifier
// output. Type is "template_filter" (literal `<...>` etc. in type_filters) or
// "query_dropped" (single-token original replaced by classifier template).
// LOUD signal — paired with a Warn log at the call site.
func (m *queryMetrics) recordClassifierGarbage(garbageType string) {
	m.classifierGarbage.WithLabelValues(garbageType).Inc()
}

// recordTypeFilterFallback records a graceful type-filter fallback (#645): the
// classifier's type filters would have zeroed a non-empty semantic result set,
// so the unfiltered set was kept. Neutral, NOT a classifier-garbage signal — a
// zero match only means nothing in the current retrieval window matched, which a
// valid inferred type can also cause. See the metric Help.
func (m *queryMetrics) recordTypeFilterFallback() {
	m.typeFilterFallback.Inc()
}
