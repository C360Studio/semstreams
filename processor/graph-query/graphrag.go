// Package graphquery GraphRAG search handlers
package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// GraphRAG search constants
const (
	// DefaultMaxCommunities is the default number of communities to search in GlobalSearch
	DefaultMaxCommunities = 5

	// MaxTotalEntitiesInSearch limits the total number of entities that can be loaded
	// across all communities in GlobalSearch to prevent unbounded memory usage
	MaxTotalEntitiesInSearch = 10000

	// MinSemanticRelevance is the minimum similarity score for semantic search results.
	// Hits below this threshold are filtered out to prevent returning irrelevant entities
	// that happen to have weak textual overlap with the query.
	MinSemanticRelevance = 0.5

	// MinBM25Relevance is the minimum similarity score for BM25 statistical
	// embedding search results. Lower than MinSemanticRelevance because BM25
	// feature-hashed vectors produce inherently lower cosine similarity scores
	// (typical range 0.0-0.73) compared to neural embeddings (0.5-1.0).
	MinBM25Relevance = 0.4

	// MinTextRelevance is the minimum score for text-based entity matching.
	// Scored as proportion of query terms that match (0.0-1.0). Entities below
	// this threshold are excluded. Default 0.3 = at least 30% of terms must match.
	MinTextRelevance = 0.3

	// DefaultSummarizeThreshold auto-summarizes globalSearch results when entity
	// count exceeds this value. Returns community summaries + entity IDs instead
	// of full entity triples. Set to 0 to disable and always return full entities.
	DefaultSummarizeThreshold = 50

	// ScoreWeightSummary is the weight for summary text matches in community scoring
	ScoreWeightSummary = 2.0

	// ScoreWeightKeyword is the weight for keyword matches in community scoring
	ScoreWeightKeyword = 1.5

	// MaxAnswerClusters limits the number of communities included in answer synthesis
	MaxAnswerClusters = 5
)

// LocalSearchRequest is the request format for local search
type LocalSearchRequest struct {
	EntityID string `json:"entity_id"`
	Query    string `json:"query"`
	Level    int    `json:"level"`
}

// LocalSearchResponse is the response format for local search
type LocalSearchResponse struct {
	Entities         []*gtypes.EntityState `json:"entities"`
	CommunityID      string                `json:"communityId"`
	Count            int                   `json:"count"`
	DurationMs       int64                 `json:"durationMs"`
	CommunitySummary string                `json:"community_summary,omitempty"`
	Keywords         []string              `json:"keywords,omitempty"`
	MemberCount      int                   `json:"member_count,omitempty"`
	EntityDigests    []EntityDigest        `json:"entity_digests,omitempty"`
	Answer           string                `json:"answer,omitempty"`
	AnswerModel      string                `json:"answer_model,omitempty"`
	// Degraded is true when an LLM-configured answer synthesizer fell
	// back to template synthesis (LLM error or timeout). The Answer
	// field still carries useful text (template summary of entity
	// hits + community keyword/summary already in COMMUNITY_INDEX);
	// the flag tells the caller "this is a degraded response — the
	// rich LLM-synthesized answer didn't materialize." Pure-template
	// deployments (no LLM ever configured) return Degraded=false.
	Degraded bool `json:"degraded,omitempty"`
	// DegradedReason classifies WHY synthesis was degraded, populated
	// only when Degraded=true. Operators group dashboards by Reason.
	// Concrete values: "answer_synthesis_timeout",
	// "answer_synthesis_error". Empty when not degraded.
	DegradedReason string `json:"degraded_reason,omitempty"`
}

// GlobalSearchRequest is the request format for global search
type GlobalSearchRequest struct {
	Query                string `json:"query"`
	Level                int    `json:"level"`
	MaxCommunities       int    `json:"max_communities"`
	SummarizeThreshold   *int   `json:"summarize_threshold,omitempty"`   // Auto-summarize when results exceed this (default: 50, -1=disabled)
	IncludeSummaries     *bool  `json:"include_summaries,omitempty"`     // Include community summaries (default: true)
	IncludeRelationships bool   `json:"include_relationships,omitempty"` // Include relationships between entities (default: false)
	IncludeSources       bool   `json:"include_sources,omitempty"`       // Include source attribution (default: false)
}

// getSummarizeThreshold returns the summarize threshold. Defaults to DefaultSummarizeThreshold.
// A negative value disables auto-summarization.
func (r *GlobalSearchRequest) getSummarizeThreshold() int {
	if r.SummarizeThreshold == nil {
		return DefaultSummarizeThreshold
	}
	return *r.SummarizeThreshold
}

// shouldIncludeSummaries returns whether summaries should be included in the response.
// Defaults to true for backward compatibility.
func (r *GlobalSearchRequest) shouldIncludeSummaries() bool {
	if r.IncludeSummaries == nil {
		return true // Default to true for backward compatibility
	}
	return *r.IncludeSummaries
}

// GlobalSearchResponse is the response format for global search
type GlobalSearchResponse struct {
	Strategy           string                `json:"strategy,omitempty"` // which strategy handled this query
	Entities           []*gtypes.EntityState `json:"entities"`
	EntityIDs          []string              `json:"entity_ids,omitempty"`     // IDs only (when summarized)
	EntityDigests      []EntityDigest        `json:"entity_digests,omitempty"` // lightweight entity context
	Summarized         bool                  `json:"summarized,omitempty"`     // true when auto-summarized
	CommunitySummaries []CommunitySummary    `json:"community_summaries,omitempty"`
	Relationships      []Relationship        `json:"relationships,omitempty"`
	Sources            []Source              `json:"sources,omitempty"`
	Count              int                   `json:"count"`
	DurationMs         int64                 `json:"duration_ms"`
	Answer             string                `json:"answer,omitempty"`
	AnswerModel        string                `json:"answer_model,omitempty"`
	// Degraded is true when an LLM-configured answer synthesizer fell
	// back to template synthesis. See LocalSearchResponse.Degraded for
	// the contract. The agent / human consumer should surface this
	// flag — Answer + Entities + CommunitySummaries are still useful,
	// but the rich LLM synthesis the operator paid for didn't
	// materialize (likely due to LLM timeout or error under load).
	Degraded bool `json:"degraded,omitempty"`
	// DegradedReason classifies why synthesis was degraded.
	// "answer_synthesis_timeout" | "answer_synthesis_error" | "".
	DegradedReason string `json:"degraded_reason,omitempty"`
	// EntitiesTruncated is true when the candidate entity corpus collected from
	// the selected communities exceeded MaxTotalEntitiesInSearch and was capped
	// before loading. Entities, Count and Answer are then derived from a PREFIX
	// of the sorted candidate set, NOT from the full community membership —
	// consumers (and answer synthesis in particular) must not present the result
	// as exhaustive. Same contract as graph.SummaryData.EntitySampleTruncated.
	EntitiesTruncated bool `json:"entities_truncated,omitempty"`
}

// Relationship represents a relationship between two entities in search results
type Relationship struct {
	FromEntityID string `json:"from_entity_id"`
	ToEntityID   string `json:"to_entity_id"`
	Predicate    string `json:"predicate"`
}

// Source represents source attribution for search results
type Source struct {
	EntityID    string  `json:"entity_id"`
	CommunityID string  `json:"community_id,omitempty"`
	Relevance   float64 `json:"relevance"`
}

// CommunitySummary represents a community's summary used in global search
type CommunitySummary struct {
	CommunityID string         `json:"community_id"`
	Summary     string         `json:"summary"`
	Keywords    []string       `json:"keywords"`
	Level       int            `json:"level"`
	Relevance   float64        `json:"relevance"`
	MemberCount int            `json:"member_count,omitempty"` // total entities in community
	Entities    []EntityDigest `json:"entities,omitempty"`     // representative entity digests
}

// EntityDigest provides lightweight, agent-readable context for an entity
// without requiring a full EntityState load.
type EntityDigest struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`                // extracted from 5th segment of entity ID
	Label     string   `json:"label,omitempty"`     // human-readable name from key predicates
	Relevance float64  `json:"relevance,omitempty"` // semantic similarity score when available
	Tags      []string `json:"tags,omitempty"`      // classification tags (content.classification.tag triples), capped
}

// extractEntityType returns the type segment (5th part) of a 6-part entity ID.
// Returns empty string if the ID has fewer than 5 segments.
func extractEntityType(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// extractEntityInstance returns the instance segment (6th part) of a 6-part entity ID.
// Used as a fallback label when no predicate-based label is available.
func extractEntityInstance(entityID string) string {
	parts := strings.Split(entityID, ".")
	if len(parts) >= 6 {
		return parts[5]
	}
	return entityID
}

// setupGraphRAGHandlers registers the GraphRAG NATS request handlers
func (c *Component) setupGraphRAGHandlers(ctx context.Context) error {
	// Subscribe to local search (globalSearch is registered in setupQueryHandlers
	// to ensure it's available at all tiers, even before the community cache is ready).
	sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.query.localSearch", c.handleLocalSearch)
	if err != nil {
		return errs.WrapTransient(err, "GraphQuery", "setupGraphRAGHandlers", "subscribe to localSearch")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	c.logger.Info("GraphRAG handlers registered",
		"subjects", []string{"graph.query.localSearch"})

	return nil
}

// handleLocalSearch handles local search requests via NATS request/reply
func (c *Component) handleLocalSearch(ctx context.Context, data []byte) ([]byte, error) {
	startTime := time.Now()

	// Parse request
	var req LocalSearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleLocalSearch", "parse request")
	}

	// Validate request
	if req.EntityID == "" {
		return nil, errs.WrapInvalid(errors.New("empty entity_id"), "GraphQuery", "handleLocalSearch", "validate entity_id")
	}
	if req.Query == "" {
		return nil, errs.WrapInvalid(errors.New("empty query"), "GraphQuery", "handleLocalSearch", "validate query")
	}

	// Check if community cache is available
	if c.communityCache == nil {
		return nil, errs.WrapTransient(errors.New("community cache not available"), "GraphQuery", "handleLocalSearch", "check cache")
	}

	// Tiered community lookup with fallback
	community := c.findCommunityWithFallback(ctx, req.EntityID, req.Level)
	if community == nil {
		// Tier 3: Fall back to semantic search if available
		semanticHits, err := c.searchEntitiesSemantic(ctx, req.Query, 50)
		if err == nil && len(semanticHits) > 0 {
			// Extract entity IDs and load them
			entityIDs := make([]string, len(semanticHits))
			for i, hit := range semanticHits {
				entityIDs[i] = hit.EntityID
			}
			entities, loadErr := c.loadEntities(ctx, entityIDs)
			if gtypes.IsStateContractError(loadErr) {
				return nil, loadErr
			}
			if loadErr == nil {
				matchedEntities := filterEntitiesByQuery(entities, req.Query, c.minTextRelevance)
				response := LocalSearchResponse{
					Entities:      matchedEntities,
					CommunityID:   "semantic-fallback",
					Count:         len(matchedEntities),
					DurationMs:    time.Since(startTime).Milliseconds(),
					EntityDigests: buildEntityDigestsFromEntities(matchedEntities),
				}
				c.recordSuccess(len(data), 0)
				return json.Marshal(response)
			}
		}
		return nil, errs.WrapInvalid(errors.New("entity not in a community at level"), "GraphQuery", "handleLocalSearch", "find community")
	}

	// Load entities from community via graph-ingest
	entities, err := c.loadEntities(ctx, community.Members)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		return nil, errs.WrapTransient(err, "GraphQuery", "handleLocalSearch", "load entities")
	}

	// Filter entities based on query
	matchedEntities := filterEntitiesByQuery(entities, req.Query, c.minTextRelevance)

	// Resolve community summary via the tiered join (LLM summary store → statistical floor).
	commSummary := c.resolveCommunitySummary(community)

	// Synthesize answer from community context
	// Level is carried alongside CommunityID: an ID identifies a community only
	// within its level, so a summary that drops the level is not re-resolvable.
	cs := []CommunitySummary{{
		CommunityID: community.ID,
		Summary:     commSummary,
		Keywords:    community.Keywords,
		Level:       community.Level,
		MemberCount: len(community.Members),
	}}
	synth := c.synthesizeQueryAnswer(ctx, req.Query, cs, len(matchedEntities))

	// Build response
	response := LocalSearchResponse{
		Entities:         matchedEntities,
		CommunityID:      community.ID,
		Count:            len(matchedEntities),
		DurationMs:       time.Since(startTime).Milliseconds(),
		CommunitySummary: commSummary,
		Keywords:         community.Keywords,
		MemberCount:      len(community.Members),
		EntityDigests:    buildEntityDigestsFromEntities(matchedEntities),
		Answer:           synth.Answer,
		AnswerModel:      synth.Model,
		Degraded:         synth.Degraded,
		DegradedReason:   synth.Reason,
	}

	c.recordSuccess(len(data), 0)
	return json.Marshal(response)
}

// tryPathIntentSearch checks a classification result for path intent and routes to PathRAG.
// Returns (result, true, nil) if handled, (nil, false, nil) when another tier
// may run, or the fatal graph-state error that must stop all fallback.
func (c *Component) tryPathIntentSearch(ctx context.Context, cr *query.ClassificationResult, queryText string, startTime time.Time, requestSize int) ([]byte, bool, error) {
	if cr == nil {
		return nil, false, nil
	}
	pathIntent, _ := cr.Options["path_intent"].(bool)
	pathStartNode, _ := cr.Options["path_start_node"].(string)
	if !pathIntent || pathStartNode == "" {
		return nil, false, nil
	}

	var pathPredicates []string
	if pp, ok := cr.Options["path_predicates"].([]string); ok {
		pathPredicates = pp
	}

	c.logger.Debug("path intent detected in NL query",
		"query", queryText,
		"start_node", pathStartNode)

	// Resolve partial entity ID to full ID
	fullID, err := c.resolvePartialEntityID(ctx, pathStartNode)
	c.logger.Debug("entity ID resolution result",
		"partial", pathStartNode,
		"full", fullID,
		"error", err)

	if err == nil && fullID != "" {
		// Execute PathRAG search
		pathResult, pathErr := c.executePathSearchForGlobal(ctx, fullID, pathPredicates)
		if gtypes.IsStateContractError(pathErr) {
			return nil, false, pathErr
		}
		entityCount := 0
		if pathResult != nil {
			entityCount = len(pathResult.Entities)
		}
		c.logger.Debug("PathRAG search result",
			"start", fullID,
			"entities", entityCount,
			"error", pathErr)

		if pathErr == nil && pathResult != nil && len(pathResult.Entities) > 0 {
			// Extract entity IDs for community enrichment
			pathEntityIDs := make([]string, len(pathResult.Entities))
			for i, e := range pathResult.Entities {
				pathEntityIDs[i] = e.ID
			}
			response := GlobalSearchResponse{
				Entities:   pathResult.Entities,
				Count:      len(pathResult.Entities),
				DurationMs: time.Since(startTime).Milliseconds(),
			}
			if err := c.enrichGlobalResponse(ctx, &response, queryText, pathEntityIDs, nil); err != nil {
				return nil, false, err
			}
			c.recordSuccess(requestSize, 0)
			marshaledResult, _ := json.Marshal(response)
			return marshaledResult, true, nil
		}
		c.logger.Debug("PathRAG returned no results, falling through to other tiers",
			"error", pathErr)
		return nil, false, nil
	}

	// At structural tier without entity resolution, return empty result
	if c.communityCache == nil {
		c.logger.Debug("structural tier: cannot resolve entity ID, returning empty result",
			"partial", pathStartNode)
		response := GlobalSearchResponse{
			Entities:   []*gtypes.EntityState{},
			Count:      0,
			DurationMs: time.Since(startTime).Milliseconds(),
		}
		marshaledResult, _ := json.Marshal(response)
		return marshaledResult, true, nil
	}

	return nil, false, nil
}

// classifyQuery runs the classifier chain if available, returning nil otherwise.
func (c *Component) classifyQuery(ctx context.Context, queryText string) *query.ClassificationResult {
	if c.classifier == nil {
		return nil
	}
	return c.classifier.ClassifyQuery(ctx, queryText)
}

// classifierTemplatePattern matches template-placeholder strings that small
// classifier models (e.g. qwen3-0.6b) sometimes emit verbatim from their
// response template instead of resolving — `<entity_type>`, `{type}`,
// `$placeholder`. When seen in a type_filters slot, the literal placeholder
// reaches filterEntityIDsByType where it matches no real entity; that filter now
// falls back to the unfiltered set rather than dropping every hit (#645), but
// stripping the placeholder here keeps the fallback from firing needlessly. See
// semspec Meshtastic report (2026-05-07).
var classifierTemplatePattern = regexp.MustCompile(`^[<{$].*[>}]?$`)

// extractSearchRefinements pulls query reformulation and type filters from a
// classification result. Returns the raw query unchanged when no hints are available.
//
// Hardening (post-Meshtastic incident, 2026-05-07):
//
//  1. Single-token rawQuery (no spaces) bypasses classifier query refinement —
//     small models tend to replace proper-noun queries with generic example
//     templates ("Meshtastic" → "find paths between entities") that lose the
//     original intent.
//  2. Type filters that look like template placeholders (`<...>`, `{...}`,
//     `$...`) are dropped with a WARN — the classifier emitted its template
//     verbatim instead of resolving. Better to send the request without
//     filtering than to drop every hit silently.
func (c *Component) extractSearchRefinements(cr *query.ClassificationResult, rawQuery string) (string, []string) {
	searchQuery := rawQuery
	var typeFilters []string
	if cr == nil {
		return searchQuery, typeFilters
	}
	singleToken := !strings.ContainsAny(strings.TrimSpace(rawQuery), " \t\n")
	if q, ok := cr.Options["query"].(string); ok && q != "" {
		switch {
		case singleToken && !strings.Contains(strings.ToLower(q), strings.ToLower(rawQuery)):
			c.logger.Warn("classifier-refined query dropped — single-token original not preserved",
				"original", rawQuery,
				"refined", q)
			if c.promMetrics != nil {
				c.promMetrics.recordClassifierGarbage("query_dropped")
			}
		default:
			searchQuery = q
			c.logger.Debug("using classifier-refined query",
				"original", rawQuery,
				"refined", searchQuery)
		}
	}
	if t, ok := cr.Options["types"].([]string); ok && len(t) > 0 {
		typeFilters = filterClassifierTemplates(t, c.logger, c.promMetrics)
	}
	return searchQuery, typeFilters
}

// filterClassifierTemplates removes template-shaped placeholder strings from
// a list of classifier-emitted type filters, logs a WARN per dropped value,
// and increments the classifier-garbage counter. Returns the filtered list.
func filterClassifierTemplates(types []string, logger *slog.Logger, m *queryMetrics) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		if classifierTemplatePattern.MatchString(strings.TrimSpace(t)) {
			if logger != nil {
				logger.Warn("classifier-emitted type filter dropped — template placeholder",
					"value", t)
			}
			if m != nil {
				m.recordClassifierGarbage("template_filter")
			}
			continue
		}
		out = append(out, t)
	}
	return out
}

// recordGlobalSearchOutcome records the Prometheus signals for a globalSearch
// response: the entity-count histogram, the empty-with-reason counter (when
// Count is zero and a non-empty reason is supplied), and the degraded counter
// (when Degraded is true). Single helper keeps the strategy bodies under the
// revive function-length cap.
func (c *Component) recordGlobalSearchOutcome(strategy string, resp *GlobalSearchResponse, emptyReason string) {
	if c.promMetrics == nil {
		return
	}
	c.promMetrics.observeGlobalSearchResponseCount(resp.Count)
	if resp.Count == 0 && emptyReason != "" {
		c.promMetrics.recordGlobalSearchEmpty(strategy, emptyReason)
	}
	if resp.Degraded {
		c.promMetrics.recordGlobalSearchDegraded(resp.DegradedReason)
	}
}

// minScore returns the smallest Score in hits, or 0 when hits is empty.
// Used only for diagnostic logs in the globalSearch paths.
func minScore(hits []SemanticHit) float64 {
	if len(hits) == 0 {
		return 0
	}
	m := hits[0].Score
	for _, h := range hits[1:] {
		if h.Score < m {
			m = h.Score
		}
	}
	return m
}

// maxScore returns the largest Score in hits, or 0 when hits is empty.
// Used only for diagnostic logs in the globalSearch paths.
func maxScore(hits []SemanticHit) float64 {
	if len(hits) == 0 {
		return 0
	}
	m := hits[0].Score
	for _, h := range hits[1:] {
		if h.Score > m {
			m = h.Score
		}
	}
	return m
}

// MinSemanticRelevancePure is the minimum similarity score for pure semantic strategy
// queries (e.g. "find similar to X"). Higher than MinSemanticRelevance because pure
// semantic queries should only return genuinely similar entities.
const MinSemanticRelevancePure = 0.5

// resolveStrategy maps a ClassificationResult to one of the dispatch strategy strings:
// "entity_lookup", "pathrag", "semantic", "temporal", "spatial", or "graphrag" (default).
//
// Resolution order:
//  1. Explicit strategy from cr.Options["strategy"] (with alias normalisation)
//  2. Signal-based inference from individual cr.Options flags
//  3. Default: "graphrag"
func (c *Component) resolveStrategy(cr *query.ClassificationResult) string {
	if cr == nil {
		return "graphrag"
	}

	// Explicit strategy wins — normalize aliases so callers see canonical names.
	if raw, ok := cr.Options["strategy"].(string); ok && raw != "" {
		switch raw {
		case string(query.StrategyExact):
			return "entity_lookup"
		case string(query.StrategyGeoGraphRAG):
			return "spatial"
		case string(query.StrategyPathRAG):
			return "pathrag"
		case string(query.StrategySemantic):
			return "semantic"
		case string(query.StrategyTemporalGraphRAG):
			return "temporal"
		default:
			// Any other explicit strategy (hybrid, aggregation, …) falls to graphrag.
			return "graphrag"
		}
	}

	// Signal-based inference.
	hasTime := cr.Options["time_range"] != nil
	hasGeo := cr.Options["geo_bounds"] != nil
	pathIntent, _ := cr.Options["path_intent"].(bool)

	switch {
	case pathIntent:
		return "pathrag"
	case hasTime && !hasGeo:
		return "temporal"
	case hasGeo && !hasTime:
		return "spatial"
		// Combined temporal+spatial (both present) intentionally falls through
		// to graphrag, which handles multi-signal queries via community scoring.
	}

	return "graphrag"
}

// handleGlobalSearch handles global search requests via NATS request/reply.
// It classifies the query once, resolves a dispatch strategy, then delegates
// to the appropriate strategy handler. Handlers that find no results fall
// through to the default graphrag path.
func (c *Component) handleGlobalSearch(ctx context.Context, data []byte) ([]byte, error) {
	startTime := time.Now()

	// Parse request.
	var req GlobalSearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "handleGlobalSearch", "parse request")
	}

	// Validate request.
	if req.Query == "" {
		return nil, errs.WrapInvalid(errors.New("empty query"), "GraphQuery", "handleGlobalSearch", "validate query")
	}

	// Apply defaults.
	if req.MaxCommunities <= 0 {
		req.MaxCommunities = DefaultMaxCommunities
	}

	// Classify query once — drives both strategy selection and search refinement.
	classResult := c.classifyQuery(ctx, req.Query)

	// Extract refined query text and type filters regardless of strategy,
	// so strategy handlers can use them without re-classifying.
	searchQuery, typeFilters := c.extractSearchRefinements(classResult, req.Query)

	strategy := c.resolveStrategy(classResult)
	if c.promMetrics != nil {
		c.promMetrics.recordGlobalSearchStrategy(strategy)
	}
	c.logger.Debug("globalSearch strategy resolved",
		"query", req.Query,
		"refined_query", searchQuery,
		"strategy", strategy,
		"type_filters", typeFilters,
		"req_level", req.Level,
		"req_max_communities", req.MaxCommunities)

	switch strategy {
	case "entity_lookup":
		result, handled, err := c.handleStrategyEntityLookup(ctx, classResult, searchQuery, startTime)
		if err != nil {
			return nil, err
		}
		if handled {
			return result, nil
		}
		// entity_lookup couldn't resolve the query as a partial entity ID
		// (single-word queries, no path_start_node, fewer than 2 dots) —
		// fall through to graphrag so the semantic tier gets a chance.
	case "pathrag":
		result, handled, err := c.tryPathIntentSearch(ctx, classResult, req.Query, startTime, len(data))
		if err != nil {
			return nil, err
		}
		if handled {
			return result, nil
		}
		// Path search found no results — fall through to graphrag.
	case "semantic":
		return c.handleStrategySemantic(ctx, searchQuery, typeFilters, &req, startTime, len(data))
	case "temporal":
		return c.handleStrategyTemporal(ctx, classResult, &req, startTime, len(data))
	case "spatial":
		return c.handleStrategySpatial(ctx, classResult, &req, startTime, len(data))
	}

	// Default: graphrag (semantic → text-based community search).
	return c.handleStrategyGraphRAG(ctx, searchQuery, typeFilters, &req, startTime, len(data))
}

// handleStrategyGraphRAG is the default search path: try semantic search first,
// then fall back to text-based community scoring. This is a pure extraction of
// the former Tier 1 + Tier 2 blocks from handleGlobalSearch.
func (c *Component) handleStrategyGraphRAG(ctx context.Context, searchQuery string, typeFilters []string, req *GlobalSearchRequest, startTime time.Time, requestSize int) ([]byte, error) {
	// Tier 1: Try semantic search first (via graph-embedding).
	// Semantic search works independently of the community cache.
	semanticHits, err := c.searchEntitiesSemantic(ctx, searchQuery, 100)
	c.logger.Debug("graphrag tier-1 semantic call",
		"query", searchQuery,
		"err", err,
		"hits", len(semanticHits),
		"min_score", minScore(semanticHits),
		"max_score", maxScore(semanticHits))
	if err == nil && len(semanticHits) > 0 {
		c.logger.Debug("using semantic search results",
			"query", searchQuery,
			"hits", len(semanticHits))

		// Extract entity IDs from semantic hits.
		entityIDs := make([]string, len(semanticHits))
		for i, hit := range semanticHits {
			entityIDs[i] = hit.EntityID
		}

		// Apply entity type filters from classifier (narrows results to relevant types).
		if len(typeFilters) > 0 {
			filtered, fellBack := filterEntityIDsByType(entityIDs, typeFilters)
			if fellBack {
				// No candidate in the retrieval window matched the classifier's
				// type filters — do not zero a healthy semantic set (#645); keep the
				// unfiltered hits. Neutral fallback: a valid inferred type can also
				// miss the window, so this is NOT recorded as classifier garbage.
				c.logger.Debug("type filter would zero non-empty semantic set — falling back to unfiltered",
					"types", typeFilters,
					"hits", len(filtered))
				if c.promMetrics != nil {
					c.promMetrics.recordTypeFilterFallback()
				}
			}
			entityIDs = filtered
			c.logger.Debug("applied type filters",
				"types", typeFilters,
				"remaining", len(entityIDs))
		}

		// Find communities containing these entities (may be empty without cache).
		communityMatches := c.findCommunitiesForEntities(entityIDs)

		// Limit to requested number of communities.
		if len(communityMatches) > req.MaxCommunities {
			communityMatches = communityMatches[:req.MaxCommunities]
		}

		// Auto-summarize: when results exceed threshold, return summaries + IDs
		// instead of loading full entity triples (which can be 100MB+ for broad queries).
		threshold := req.getSummarizeThreshold()
		if threshold > 0 && len(entityIDs) > threshold {
			c.logger.Debug("auto-summarizing broad search results",
				"query", req.Query,
				"hits", len(entityIDs),
				"threshold", threshold)

			// Build the query-relevance score map from the FINAL surviving
			// entityIDs (narrowed above by the type filter), not the raw hits, so a
			// type-filtered-out entity cannot leak into representative selection via
			// a shared community. On type-filter fallback entityIDs is the full hit
			// set, so this is unchanged.
			semanticScores := relevanceScoresFor(entityIDs, semanticHits)

			// Enrich community summaries with query-relevant, tag-enriched digests
			// (Levers A+B): the semanticScores steer representative selection.
			enriched, enrichErr := c.enrichCommunitySummaries(ctx, communityMatches, semanticScores)
			if enrichErr != nil {
				return nil, enrichErr
			}

			// Collect labels from enriched summaries for entity digests
			labels := make(map[string]string)
			for _, s := range enriched {
				for _, e := range s.Entities {
					labels[e.ID] = e.Label
				}
			}

			synth := c.synthesizeQueryAnswer(ctx, searchQuery, enriched, len(entityIDs))
			response := GlobalSearchResponse{
				Summarized:         true,
				EntityIDs:          entityIDs,
				EntityDigests:      buildEntityDigests(entityIDs, semanticScores, labels),
				Count:              len(entityIDs),
				CommunitySummaries: enriched,
				Answer:             synth.Answer,
				AnswerModel:        synth.Model,
				Degraded:           synth.Degraded,
				DegradedReason:     synth.Reason,
				DurationMs:         time.Since(startTime).Milliseconds(),
			}
			c.recordSuccess(requestSize, 0)
			return json.Marshal(response)
		}

		// Load full entity data for the semantic hits.
		entities, loadErr := c.loadEntities(ctx, entityIDs)
		if loadErr != nil {
			if gtypes.IsStateContractError(loadErr) {
				return nil, loadErr
			}
			c.logger.Warn("failed to load semantic search entities, falling back to text",
				"error", loadErr)
			// Fall through to text-based search.
		} else {
			response := GlobalSearchResponse{
				Entities:   entities,
				Count:      len(entities),
				DurationMs: time.Since(startTime).Milliseconds(),
			}
			if req.shouldIncludeSummaries() {
				// Per-entity relevance is available here — thread it so digest
				// representative selection is query-relevant (Lever A). Keyed by the
				// FINAL surviving entityIDs (narrowed by the type filter) so a
				// type-filtered-out hit cannot leak into representative selection.
				scores := relevanceScoresFor(entityIDs, semanticHits)
				if err := c.enrichGlobalResponse(ctx, &response, searchQuery, entityIDs, scores); err != nil {
					return nil, err
				}
			}
			if req.IncludeRelationships {
				response.Relationships = c.extractRelationships(ctx, entities)
			}
			if req.IncludeSources {
				response.Sources = c.buildSources(entities, semanticHits, communityMatches)
			}
			c.recordGlobalSearchOutcome("graphrag", &response, "")
			c.recordSuccess(requestSize, 0)
			return json.Marshal(response)
		}
	} else if err != nil {
		c.logger.Debug("semantic search unavailable, using text fallback",
			"error", err)
	}

	// Tier 2: Fall back to text-based community scoring.
	return c.graphRAGTier2Fallback(ctx, req, startTime, requestSize, err)
}

// graphRAGTier2Fallback handles the graphrag fall-through when tier-1 semantic
// search is empty or errored. Records the tier transition Prometheus signal
// and either returns an empty response (community cache unavailable) or
// delegates to the text-based community search.
func (c *Component) graphRAGTier2Fallback(ctx context.Context, req *GlobalSearchRequest, startTime time.Time, requestSize int, semanticErr error) ([]byte, error) {
	tierReason := "empty_hits"
	if semanticErr != nil {
		tierReason = "err"
	}
	if c.promMetrics != nil {
		c.promMetrics.recordGlobalSearchTierTransition("semantic", "community_text", tierReason)
	}
	// Return an empty result instead of an error when the community cache
	// is unavailable — callers should not receive a hard error just because
	// clustering hasn't run yet.
	if c.communityCache == nil {
		c.logger.Debug("community cache not available, returning empty result",
			"query", req.Query)
		response := GlobalSearchResponse{
			Entities:   []*gtypes.EntityState{},
			Count:      0,
			DurationMs: time.Since(startTime).Milliseconds(),
		}
		if req.shouldIncludeSummaries() {
			response.CommunitySummaries = []CommunitySummary{}
		}
		c.recordGlobalSearchOutcome("graphrag", &response, "no_community_cache")
		return json.Marshal(response)
	}
	return c.globalSearchTextBased(ctx, *req, startTime, requestSize)
}

// handleStrategyEntityLookup resolves a named entity and returns it directly.
// This is used for exact-match queries like "show me sensor-001".
//
// Returns (result, true, nil) when the entity_lookup strategy produced a
// meaningful resolution; (nil, false, nil) when the classifier mis-routed a
// query so the caller can fall through to GraphRAG; or a fatal graph-state
// error that must stop fallback. Mirrors tryPathIntentSearch's contract.
//
// Prior behavior returned an empty `(result, nil)` in the no-fall-through
// case; the caller switch then unconditionally returned that empty
// payload — single-word queries like "forklift" / "hydraulic" /
// "temperature" hit this dead-end every time. The known-answer e2e
// surface caught it; the user-acknowledged "single-word search doesn't
// work" was the same root cause.
func (c *Component) handleStrategyEntityLookup(ctx context.Context, cr *query.ClassificationResult, searchQuery string, startTime time.Time) ([]byte, bool, error) {
	// Prefer path_start_node extracted by the classifier; fall back to the
	// (possibly refined) query text when it looks like a partial entity ID.
	ref := ""
	if cr != nil {
		ref, _ = cr.Options["path_start_node"].(string)
	}
	if ref == "" && strings.Count(searchQuery, ".") >= 2 {
		ref = searchQuery
	}
	if ref == "" {
		// Nothing to look up — signal fall-through to graphrag.
		return nil, false, nil
	}

	fullID, err := c.resolvePartialEntityID(ctx, ref)
	if err != nil || fullID == "" {
		c.logger.Debug("entity_lookup: could not resolve entity, falling through",
			"ref", ref,
			"error", err)
		return nil, false, nil
	}

	entities, loadErr := c.loadEntities(ctx, []string{fullID})
	if loadErr != nil {
		if gtypes.IsStateContractError(loadErr) {
			return nil, false, loadErr
		}
		c.logger.Debug("entity_lookup: load failed, falling through",
			"id", fullID,
			"error", loadErr)
		return nil, false, nil
	}

	response := GlobalSearchResponse{
		Entities:   entities,
		Count:      len(entities),
		DurationMs: time.Since(startTime).Milliseconds(),
	}
	if err := c.enrichGlobalResponse(ctx, &response, searchQuery, []string{fullID}, nil); err != nil {
		return nil, false, err
	}

	c.recordSuccess(0, 0)
	out, err := json.Marshal(response)
	if err != nil {
		c.logger.Debug("entity_lookup: marshal failed, falling through",
			"id", fullID,
			"error", err)
		return nil, false, nil
	}
	return out, true, nil
}

// handleStrategySemantic executes a pure vector similarity search.
// Uses a higher relevance threshold (0.5) than the graphrag path (0.3) because
// queries routed here explicitly request semantic similarity ("find similar to X"),
// so only genuinely similar entities should be returned.
func (c *Component) handleStrategySemantic(ctx context.Context, searchQuery string, typeFilters []string, _ *GlobalSearchRequest, startTime time.Time, requestSize int) ([]byte, error) {
	semanticHits, err := c.searchEntitiesSemantic(ctx, searchQuery, 100)
	if err != nil {
		c.logger.Debug("semantic strategy: search unavailable",
			"query", searchQuery,
			"error", err)
		if c.promMetrics != nil {
			c.promMetrics.recordGlobalSearchEmpty("semantic", "semantic_unavailable")
			c.promMetrics.observeGlobalSearchResponseCount(0)
		}
		// Graceful degradation: return empty rather than hard error.
		return json.Marshal(GlobalSearchResponse{
			Entities:   []*gtypes.EntityState{},
			Count:      0,
			DurationMs: time.Since(startTime).Milliseconds(),
		})
	}

	c.logger.Debug("semantic strategy: raw hits",
		"query", searchQuery,
		"hits", len(semanticHits),
		"min_score", minScore(semanticHits),
		"max_score", maxScore(semanticHits),
		"threshold", MinSemanticRelevancePure)

	// Apply higher relevance threshold for pure semantic queries.
	filtered := make([]SemanticHit, 0, len(semanticHits))
	for _, h := range semanticHits {
		if h.Score >= MinSemanticRelevancePure {
			filtered = append(filtered, h)
		}
	}

	c.logger.Debug("semantic strategy: after threshold filter",
		"query", searchQuery,
		"survived", len(filtered),
		"dropped", len(semanticHits)-len(filtered))
	if c.promMetrics != nil {
		c.promMetrics.recordGlobalSearchHitsDropped("threshold", len(semanticHits)-len(filtered))
	}

	entityIDs := make([]string, len(filtered))
	for i, h := range filtered {
		entityIDs[i] = h.EntityID
	}

	if len(typeFilters) > 0 {
		before := len(entityIDs)
		filtered, fellBack := filterEntityIDsByType(entityIDs, typeFilters)
		if fellBack {
			// No candidate in the retrieval window matched the classifier's type
			// filters — do not zero a healthy semantic set (#645); keep the
			// unfiltered hits. On a fallback before-after == 0, so hits-dropped
			// records 0 drops and the type_filter_fallback counter marks it.
			// Neutral: a valid inferred type can also miss the window, so this is
			// NOT classifier garbage.
			c.logger.Debug("type filter would zero non-empty semantic set — falling back to unfiltered",
				"types", typeFilters,
				"hits", len(filtered))
			if c.promMetrics != nil {
				c.promMetrics.recordTypeFilterFallback()
			}
		}
		entityIDs = filtered
		c.logger.Debug("semantic strategy: after type filter",
			"types", typeFilters,
			"before", before,
			"after", len(entityIDs))
		if c.promMetrics != nil {
			c.promMetrics.recordGlobalSearchHitsDropped("type_filter", before-len(entityIDs))
		}
	}

	entities, loadErr := c.loadEntities(ctx, entityIDs)
	if loadErr != nil {
		if gtypes.IsStateContractError(loadErr) {
			return nil, loadErr
		}
		return nil, errs.WrapTransient(loadErr, "GraphQuery", "handleStrategySemantic", "load entities")
	}

	response := GlobalSearchResponse{
		Entities:   entities,
		Count:      len(entities),
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	// Per-entity relevance is in scope here (the threshold-filtered hits) —
	// thread it so digest representative selection is query-relevant (Lever A),
	// mirroring the tier-1 full-load caller in handleStrategyGraphRAG. Keyed by
	// the FINAL surviving entityIDs (post type-filter), NOT the pre-filter
	// `filtered` hit set, so a type-filtered-out hit cannot leak into
	// representative selection via a shared community.
	scores := relevanceScoresFor(entityIDs, filtered)
	if err := c.enrichGlobalResponse(ctx, &response, searchQuery, entityIDs, scores); err != nil {
		return nil, err
	}

	if c.promMetrics != nil {
		c.promMetrics.observeGlobalSearchResponseCount(response.Count)
		if response.Count == 0 {
			// Pure semantic strategy + non-empty pipeline + zero entities → the
			// relevance threshold wiped them, or a partial type-filter narrow left
			// survivors that then failed to load. The type filter can no longer
			// zero a non-empty set on its own (#645 falls back), so a non-zero
			// hits_dropped{type_filter} here means a partial narrow, not a zeroing.
			// The empty counter aggregates the total class.
			c.promMetrics.recordGlobalSearchEmpty("semantic", "filtered_to_empty")
		}
	}

	c.recordSuccess(requestSize, 0)
	return json.Marshal(response)
}

// handleStrategyTemporal delegates to the graph-temporal component via NATS.
// It extracts the time range from the classification result and forwards the
// bounded range query, then loads the matched entities.
func (c *Component) handleStrategyTemporal(ctx context.Context, cr *query.ClassificationResult, req *GlobalSearchRequest, startTime time.Time, requestSize int) ([]byte, error) {
	timeRange, _ := cr.Options["time_range"].(*query.TimeRange)
	if timeRange == nil {
		c.logger.Debug("temporal strategy: no time range in classification, falling back to graphrag",
			"query", req.Query)
		return c.handleStrategyGraphRAG(ctx, req.Query, nil, req, startTime, requestSize)
	}

	subject := c.router.Route("temporal")
	if subject == "" {
		c.logger.Debug("temporal route unavailable, falling back to graphrag")
		return c.handleStrategyGraphRAG(ctx, req.Query, nil, req, startTime, requestSize)
	}

	reqData, err := json.Marshal(map[string]any{
		"startTime": timeRange.Start.Format(time.RFC3339),
		"endTime":   timeRange.End.Format(time.RFC3339),
		"limit":     100,
	})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleStrategyTemporal", "marshal request")
	}

	// ADR-060 (gh#326): RequestClassified surfaces a handler failure via err instead
	// of an "error: <msg>" body that parseEntityIDsFromResults would mis-decode as empty.
	respData, err := c.natsClient.RequestClassified(ctx, subject, reqData, c.config.QueryTimeout)
	if err != nil {
		return nil, errs.WrapTransient(err, "GraphQuery", "handleStrategyTemporal", "temporal query")
	}

	entityIDs, parseErr := parseEntityIDsFromResults(respData)
	if parseErr != nil {
		return nil, errs.WrapInvalid(parseErr, "GraphQuery", "handleStrategyTemporal", "parse response")
	}

	entities, loadErr := c.loadEntities(ctx, entityIDs)
	if loadErr != nil {
		if gtypes.IsStateContractError(loadErr) {
			return nil, loadErr
		}
		return nil, errs.WrapTransient(loadErr, "GraphQuery", "handleStrategyTemporal", "load entities")
	}

	response := GlobalSearchResponse{
		Entities:   entities,
		Count:      len(entities),
		DurationMs: time.Since(startTime).Milliseconds(),
	}
	if err := c.enrichGlobalResponse(ctx, &response, req.Query, entityIDs, nil); err != nil {
		return nil, err
	}

	c.recordSuccess(requestSize, 0)
	return json.Marshal(response)
}

// handleStrategySpatial delegates to the graph-spatial component via NATS.
// It extracts geographic bounds from the classification result and issues a
// bounding-box query, then loads the matched entities.
func (c *Component) handleStrategySpatial(ctx context.Context, cr *query.ClassificationResult, req *GlobalSearchRequest, startTime time.Time, requestSize int) ([]byte, error) {
	bounds, _ := cr.Options["geo_bounds"].(*query.SpatialBounds)
	if bounds == nil {
		c.logger.Debug("spatial strategy: no geo bounds in classification, falling back to graphrag",
			"query", req.Query)
		return c.handleStrategyGraphRAG(ctx, req.Query, nil, req, startTime, requestSize)
	}

	subject := c.router.Route("spatial")
	if subject == "" {
		c.logger.Debug("spatial route unavailable, falling back to graphrag")
		return c.handleStrategyGraphRAG(ctx, req.Query, nil, req, startTime, requestSize)
	}

	reqData, err := json.Marshal(map[string]any{
		"north": bounds.North,
		"south": bounds.South,
		"east":  bounds.East,
		"west":  bounds.West,
		"limit": 100,
	})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "handleStrategySpatial", "marshal request")
	}

	// ADR-060 (gh#326): RequestClassified surfaces a handler failure via err instead
	// of an "error: <msg>" body that parseEntityIDsFromResults would mis-decode as empty.
	respData, err := c.natsClient.RequestClassified(ctx, subject, reqData, c.config.QueryTimeout)
	if err != nil {
		return nil, errs.WrapTransient(err, "GraphQuery", "handleStrategySpatial", "spatial query")
	}

	entityIDs, parseErr := parseEntityIDsFromResults(respData)
	if parseErr != nil {
		return nil, errs.WrapInvalid(parseErr, "GraphQuery", "handleStrategySpatial", "parse response")
	}

	entities, loadErr := c.loadEntities(ctx, entityIDs)
	if loadErr != nil {
		if gtypes.IsStateContractError(loadErr) {
			return nil, loadErr
		}
		return nil, errs.WrapTransient(loadErr, "GraphQuery", "handleStrategySpatial", "load entities")
	}

	response := GlobalSearchResponse{
		Entities:   entities,
		Count:      len(entities),
		DurationMs: time.Since(startTime).Milliseconds(),
	}
	if err := c.enrichGlobalResponse(ctx, &response, req.Query, entityIDs, nil); err != nil {
		return nil, err
	}

	c.recordSuccess(requestSize, 0)
	return json.Marshal(response)
}

// parseEntityIDsFromResults extracts entity_id values from a JSON array of objects.
// Both temporal and spatial handlers return arrays where each element has an "entity_id" field.
func parseEntityIDsFromResults(data []byte) ([]string, error) {
	var results []struct {
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(results))
	for _, r := range results {
		if r.EntityID != "" {
			ids = append(ids, r.EntityID)
		}
	}
	return ids, nil
}

// sortAndCapEntityIDs turns a candidate entity-ID set into a deterministically
// ordered slice capped at limit, reporting whether the cap bit.
//
// Go randomizes map iteration order, so truncating a slice built by ranging a
// set retains an ARBITRARY subset that differs on every call: past the limit,
// the same query against an unchanged graph returned a different corpus — and
// therefore a different synthesized answer — run to run (gh#621). Sorting first
// makes the retained prefix a pure function of the candidate set. Set membership
// guarantees the IDs are unique, so lexical order is a total order on them and
// no secondary tie-break is required for stability.
//
// A limit <= 0 means no cap.
func sortAndCapEntityIDs(idSet map[string]bool, limit int) (ids []string, truncated bool) {
	ids = make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if limit > 0 && len(ids) > limit {
		return ids[:limit], true
	}
	return ids, false
}

// globalSearchTextBased performs text-based global search using community summaries.
// This is the fallback when semantic search is unavailable.
func (c *Component) globalSearchTextBased(ctx context.Context, req GlobalSearchRequest, startTime time.Time, requestSize int) ([]byte, error) {
	// Get all communities at the specified level from cache
	communities := c.communityCache.GetCommunitiesByLevel(req.Level)
	stats := c.communityCache.Stats()
	c.logger.Debug("globalSearchTextBased: cache fetch",
		"req_level", req.Level,
		"communities_at_level", len(communities),
		"cache_total_communities", stats.TotalCommunities,
		"cache_total_entities", stats.TotalEntities,
		"cache_by_level", stats.ByLevel,
		"cache_ready", stats.Ready)
	if len(communities) == 0 {
		response := GlobalSearchResponse{
			Entities:   []*gtypes.EntityState{},
			Count:      0,
			DurationMs: time.Since(startTime).Milliseconds(),
		}
		// Include empty summaries array for backward compatibility if summaries requested
		if req.shouldIncludeSummaries() {
			response.CommunitySummaries = []CommunitySummary{}
		}
		c.recordGlobalSearchOutcome("graphrag", &response, "no_communities")
		return json.Marshal(response)
	}

	// Score communities based on their summaries (tiered join → statistical floor).
	scoredCommunities := scoreCommunitySummaries(communities, req.Query, c.resolveCommunitySummary)

	// Select top-N communities
	selectedCount := req.MaxCommunities
	if len(scoredCommunities) < selectedCount {
		selectedCount = len(scoredCommunities)
	}
	topCommunities := scoredCommunities[:selectedCount]

	// Collect all entity IDs from selected communities
	entityIDSet := make(map[string]bool)
	for _, comm := range topCommunities {
		for _, memberID := range comm.Members {
			entityIDSet[memberID] = true
		}
	}

	// Order deterministically, THEN enforce the resource limit (gh#621).
	entityIDs, truncated := sortAndCapEntityIDs(entityIDSet, MaxTotalEntitiesInSearch)
	if truncated {
		c.logger.Warn("globalSearchTextBased: candidate corpus truncated",
			"candidates", len(entityIDSet),
			"limit", MaxTotalEntitiesInSearch,
			"communities", len(topCommunities))
	}

	// Load entities via graph-ingest
	entities, err := c.loadEntities(ctx, entityIDs)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		return nil, errs.WrapTransient(err, "GraphQuery", "globalSearchTextBased", "load entities")
	}

	// Filter entities based on query
	matchedEntities := filterEntitiesByQuery(entities, req.Query, c.minTextRelevance)

	// Build community summaries for response
	summaries := make([]CommunitySummary, len(topCommunities))
	for i, comm := range topCommunities {
		// Calculate relevance from position
		relevance := 1.0 - (float64(i) / float64(len(scoredCommunities)))

		// Resolve via the tiered join (LLM summary store → statistical floor).
		summary := c.resolveCommunitySummary(comm)

		summaries[i] = CommunitySummary{
			CommunityID: comm.ID,
			Summary:     summary,
			Keywords:    comm.Keywords,
			Level:       comm.Level,
			Relevance:   relevance,
			MemberCount: len(comm.Members),
		}
	}

	// Build response
	response := GlobalSearchResponse{
		Entities:          matchedEntities,
		Count:             len(matchedEntities),
		DurationMs:        time.Since(startTime).Milliseconds(),
		EntitiesTruncated: truncated,
	}

	// Conditionally include summaries (default: true). No semantic scores on the
	// text/statistical fallback path — PageRank representative selection unchanged.
	if req.shouldIncludeSummaries() {
		enriched, enrichErr := c.enrichCommunitySummaries(ctx, summaries, nil)
		if enrichErr != nil {
			return nil, enrichErr
		}
		response.CommunitySummaries = enriched
		synth := c.synthesizeQueryAnswer(ctx, req.Query, enriched, len(matchedEntities))
		response.Answer = synth.Answer
		response.AnswerModel = synth.Model
		response.Degraded = synth.Degraded
		response.DegradedReason = synth.Reason
	}

	// Conditionally extract relationships (opt-in)
	if req.IncludeRelationships {
		response.Relationships = c.extractRelationships(ctx, matchedEntities)
	}

	// Conditionally build sources (opt-in, no semantic hits in text-based search)
	if req.IncludeSources {
		response.Sources = c.buildSources(matchedEntities, nil, summaries)
	}

	c.recordGlobalSearchOutcome("graphrag", &response, "relevance_filter")
	c.recordSuccess(requestSize, 0)
	return json.Marshal(response)
}

// loadEntities loads entities by ID via graph-ingest request/reply
func (c *Component) loadEntities(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error) {
	if len(entityIDs) == 0 {
		return []*gtypes.EntityState{}, nil
	}

	// Build batch request
	reqData, err := json.Marshal(map[string]any{
		"ids": entityIDs,
	})
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "loadEntities", "marshal request")
	}

	// Request entities from graph-ingest
	subject := c.router.Route("entityBatch")
	if subject == "" {
		return nil, errs.WrapTransient(errors.New("entityBatch query routing not available"), "GraphQuery", "loadEntities", "route query")
	}
	// ADR-060: RequestClassified surfaces a handler failure via err instead of
	// an "error: <msg>" body that would mis-decode as an empty entity batch.
	respData, err := c.natsClient.RequestClassified(ctx, subject, reqData, c.config.QueryTimeout)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		return nil, errs.WrapTransient(err, "GraphQuery", "loadEntities", "request entities")
	}

	// Parse response
	var resp struct {
		Entities []*gtypes.EntityState `json:"entities"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "loadEntities", "unmarshal response")
	}
	if err := gtypes.ValidateDecodedEntityStatePointers(resp.Entities); err != nil {
		return nil, fmt.Errorf("GraphQuery.loadEntities: validate authoritative response: %w", err)
	}

	return resp.Entities, nil
}

// searchEntitiesSemantic calls graph-embedding's semantic search to find entities
// that are semantically similar to the query text using embeddings.
// This provides better results than text matching for semantic queries.
// filterEntityIDsByType filters entity IDs to those whose type segment (5th part of the
// 6-part ID: org.platform.domain.system.type.instance) matches any of the requested types.
// Returns all IDs (fellBack=false) if typeFilters is empty.
//
// Graceful-fallback contract (#645): the type filters come from the classifier
// (qwen3-0.6b). A lowercased exact-match filter over them can zero an otherwise-
// healthy semantic result set → count=0 → empty answer. That happens both when the
// classifier invents a type ("Procedure", "equipment process") AND, legitimately,
// when a valid inferred type simply has no candidate in the current retrieval
// window — this function cannot tell the two apart. So semstreams degrades, it
// does not zero: when the input is NON-EMPTY but the filter produces an EMPTY set,
// this returns the UNFILTERED input and signals fellBack=true so callers can log +
// count the fallback (as a NEUTRAL type_filter_fallback, not classifier garbage).
// An already-empty input is NOT a fallback (nothing to preserve) — (empty, false).
//
// Axis note (ADR-071): this is the TYPE-SEGMENT axis (position 5, exact segment
// equality from the classifier), which is genuinely NOT expressible as a leading
// ID prefix — it is a distinct, intentional axis, deliberately kept. It is NOT
// the leading-prefix (domain/system) scope. Leading-prefix ID scoping is the
// single source-level responsibility applied at the candidate source via
// graph.embedding.query.search's Scope field (graph.MatchesAnyIDPrefix), never
// re-added here as a second post-retrieval ID-prefix filter. If a future
// leading-prefix constraint reaches the semantic path, thread it to
// SearchRequest.Scope — do not add a prefix filter alongside this one.
func filterEntityIDsByType(entityIDs []string, typeFilters []string) (filtered []string, fellBack bool) {
	if len(typeFilters) == 0 {
		return entityIDs, false
	}
	filterSet := make(map[string]bool, len(typeFilters))
	for _, t := range typeFilters {
		filterSet[strings.ToLower(t)] = true
	}

	filtered = make([]string, 0, len(entityIDs))
	for _, id := range entityIDs {
		if filterSet[strings.ToLower(extractEntityType(id))] {
			filtered = append(filtered, id)
		}
	}

	// Zero-fallback: a non-empty semantic result set that filters to empty means
	// the classifier's type guesses matched nothing real. Do not zero it —
	// preserve the unfiltered input and signal the fallback.
	if len(entityIDs) > 0 && len(filtered) == 0 {
		return entityIDs, true
	}
	return filtered, false
}

func (c *Component) searchEntitiesSemantic(ctx context.Context, query string, limit int) ([]SemanticHit, error) {
	req := map[string]any{
		"query": query,
		"limit": limit,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, errs.Wrap(err, "GraphQuery", "searchEntitiesSemantic", "marshal request")
	}

	// gh#93 Phase 2: RequestClassified unifies transport + handler
	// errors behind one classified return. Distinguish with errors.As:
	//   - Handler-side classified error (embedder not ready, no vector
	//     for query text) → fail-open with nil so globalSearch falls
	//     to tier-2 text-based community search. Server reported
	//     "I couldn't help with this query," which is a legitimate
	//     fall-through signal.
	//   - Transport-layer failure (no responders, timeout) → propagate
	//     as Transient. The embedding service is unreachable; the
	//     caller should know vs silently degrading to text search on
	//     every query.
	resp, err := c.natsClient.RequestClassified(ctx, "graph.embedding.query.search", data, 30*time.Second)
	if err != nil {
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) {
			c.logger.Debug("semantic search handler error",
				slog.String("query", query),
				slog.Any("error", err))
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "GraphQuery", "searchEntitiesSemantic", "semantic search request")
	}

	// Response format from graph-embedding/query.go:SearchResponse
	var result struct {
		Query   string `json:"query"`
		Results []struct {
			EntityID   string  `json:"entity_id"`
			Similarity float64 `json:"similarity"`
		} `json:"results"`
		Duration     string `json:"duration"`
		EmbedderType string `json:"embedder_type"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, errs.WrapInvalid(err, "GraphQuery", "searchEntitiesSemantic", "unmarshal search response")
	}

	// Apply embedder-aware threshold: BM25 feature-hashed vectors produce
	// lower cosine similarity scores (0.0-0.73) than neural embeddings (0.5-1.0).
	threshold := c.minSemanticRelevance
	if result.EmbedderType == "bm25" {
		threshold = c.minBM25Relevance
	}

	hits := make([]SemanticHit, 0, len(result.Results))
	for _, r := range result.Results {
		if r.Similarity >= threshold {
			hits = append(hits, SemanticHit{EntityID: r.EntityID, Score: r.Similarity})
		}
	}
	return hits, nil
}

// SemanticHit represents a search result with semantic similarity score
type SemanticHit struct {
	EntityID string  `json:"entity_id"`
	Score    float64 `json:"score"`
}

// findCommunitiesForEntities returns communities that contain the given entities,
// sorted by the number of matching entities (most relevant first).
func (c *Component) findCommunitiesForEntities(entityIDs []string) []CommunitySummary {
	if c.communityCache == nil {
		return nil
	}

	entitySet := make(map[string]bool)
	for _, id := range entityIDs {
		entitySet[id] = true
	}

	var summaries []CommunitySummary
	communities := c.communityCache.GetAllCommunities()

	for _, comm := range communities {
		matchCount := 0
		for _, member := range comm.Members {
			if entitySet[member] {
				matchCount++
			}
		}
		if matchCount > 0 {
			// Resolve via the tiered join (LLM summary store → statistical floor).
			summary := c.resolveCommunitySummary(comm)

			// Calculate relevance based on match ratio
			relevance := float64(matchCount) / float64(len(comm.Members))

			summaries = append(summaries, CommunitySummary{
				CommunityID: comm.ID,
				Summary:     summary,
				Keywords:    comm.Keywords,
				Level:       comm.Level,
				Relevance:   relevance,
				MemberCount: len(comm.Members),
			})
		}
	}

	// Sort by relevance descending (communities with more matched entities first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Relevance > summaries[j].Relevance
	})

	return summaries
}

// labelPredicates is the priority-ordered list of predicates to try when resolving
// a human-readable label for an entity. The first non-empty match wins.
var labelPredicates = []string{
	vocabulary.DCTermsTitle,
	agvocab.IdentityDisplayName,
	agvocab.CapabilityName,
	agvocab.ModelName,
}

// Query-focused representative selection and tag-enrichment bounds for
// answer-synthesis digests (thematic-synthesis-context).
const (
	// MaxQueryFocusedReps caps the query-relevant representative entities
	// selected per community for the synthesis prompt. Mirrors the clustering
	// package's MaxRepEntities (5) so the prompt's rep channel stays the same
	// scale it is today — a query-relevant selection, not a wider one.
	MaxQueryFocusedReps = 5

	// MaxDigestTags caps the classification tags rendered per representative
	// digest. Corpus entities carry ~4 tags; 6 gives headroom while bounding a
	// pathologically over-tagged entity so a single member cannot balloon the
	// prompt.
	MaxDigestTags = 6
)

// digestTagPredicates lists the predicates whose (multi-valued, string) objects
// are surfaced as a representative's classification tags in the synthesis
// context. Only the tag predicate is used: it carries the thematic vocabulary
// (e.g. "battery", "door", "evacuation") that must reach the answer model;
// category/type would add noise. References the framework-recognized
// content-classification convention (vocabulary.ContentClassificationTag), the
// product-neutral predicate products emit when they classify content.
var digestTagPredicates = []string{vocabulary.ContentClassificationTag}

// collectDigestTags returns up to maxTags distinct classification tag values from
// an entity's triples, preserving first-seen order. Tags are multi-valued, so it
// iterates Triples directly rather than using GetPropertyValue (which returns
// only the first match). Non-string and empty objects are skipped. (maxTags, not
// max: the latter shadows the Go builtin and trips revive/CI.)
func collectDigestTags(entity *gtypes.EntityState, maxTags int) []string {
	if entity == nil || maxTags <= 0 {
		return nil
	}
	predSet := make(map[string]bool, len(digestTagPredicates))
	for _, p := range digestTagPredicates {
		predSet[p] = true
	}
	var tags []string
	seen := make(map[string]bool)
	for _, t := range entity.Triples {
		if !predSet[t.Predicate] {
			continue
		}
		s, ok := t.Object.(string)
		if !ok || s == "" || seen[s] {
			continue
		}
		seen[s] = true
		tags = append(tags, s)
		if len(tags) >= maxTags {
			break
		}
	}
	return tags
}

// relevanceScoresFor builds the query-relevance score map that steers digest
// representative selection, keyed ONLY by the FINAL surviving entity IDs.
//
// It first indexes every hit's score, then copies a score into the result only
// for IDs present in entityIDs. Building the map from the surviving IDs — rather
// than from the raw hit set — closes a leak: entityIDs is narrowed by
// filterEntityIDsByType, but selectDigestEntities ranks a community's WHOLE
// membership against this map, so a type-filtered-out hit left in the map could
// be promoted into a shared community's digest (reps + tags) and cite
// type-excluded evidence in the synthesized answer. Keying by the surviving IDs
// guarantees an excluded entity carries no score and cannot be selected.
//
// When the type filter fell back to the unfiltered set (fellBack), entityIDs is
// the full hit set, so every hit's ID is retained and the map is unchanged —
// the eval path (type filter empty or fallback → count ≈ all entities) is a
// no-op.
func relevanceScoresFor(entityIDs []string, hits []SemanticHit) map[string]float64 {
	hitScore := make(map[string]float64, len(hits))
	for _, h := range hits {
		hitScore[h.EntityID] = h.Score
	}
	scores := make(map[string]float64, len(entityIDs))
	for _, id := range entityIDs {
		if s, ok := hitScore[id]; ok {
			scores[id] = s
		}
	}
	return scores
}

// selectDigestEntities picks the representative entity IDs for a community's
// synthesis digest, Lever A of thematic-synthesis-context.
//
// When semanticScores carry per-entity query relevance, the community's MEMBERS
// present in that map are ranked by score (desc, tie-broken by entity ID asc for
// determinism), the top MaxQueryFocusedReps are taken, and any remaining slots
// are backfilled from the community's PageRank RepEntities. This guarantees the
// digest never carries fewer slots than today's pure-RepEntities behavior — no
// rep slot is lost — while promoting query-relevant members that PageRank would
// not surface.
//
// When semanticScores is nil/empty (the text/statistical fallback path), it
// returns comm.RepEntities unchanged, preserving the tiered graceful-fallback
// floor exactly.
func selectDigestEntities(comm *clustering.Community, semanticScores map[string]float64) []string {
	if comm == nil {
		return nil
	}
	// Full fallback: no relevance signal → PageRank representatives. Return a
	// COPIED prefix capped at MaxQueryFocusedReps rather than the caller-mutable
	// underlying slice: community records come from KV with no enforced
	// RepEntities length invariant, so a custom/historical record with more than
	// MaxQueryFocusedReps reps would otherwise bypass the bounded-cap requirement
	// (graph-query spec scenario 4). PageRank order is preserved by copying a
	// prefix.
	if len(semanticScores) == 0 {
		n := len(comm.RepEntities)
		if n > MaxQueryFocusedReps {
			n = MaxQueryFocusedReps
		}
		capped := make([]string, n)
		copy(capped, comm.RepEntities[:n])
		return capped
	}

	type scoredMember struct {
		id    string
		score float64
	}
	scored := make([]scoredMember, 0, len(comm.Members))
	for _, m := range comm.Members {
		if s, ok := semanticScores[m]; ok {
			scored = append(scored, scoredMember{id: m, score: s})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].id < scored[j].id
	})

	selected := make([]string, 0, MaxQueryFocusedReps)
	seen := make(map[string]bool)
	for _, sm := range scored {
		if len(selected) >= MaxQueryFocusedReps {
			break
		}
		if !seen[sm.id] {
			seen[sm.id] = true
			selected = append(selected, sm.id)
		}
	}
	// Backfill from PageRank reps to reach the cap when scored members are thin —
	// keeps every rep slot today's behavior would have filled.
	for _, id := range comm.RepEntities {
		if len(selected) >= MaxQueryFocusedReps {
			break
		}
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}
	return selected
}

// resolveEntityLabels loads a subset of entities and extracts human-readable labels.
// Returns a map from entity ID to label. Ordinary lookup failures and entities
// without a recognizable label remain best-effort omissions; authoritative
// graph-state failures are returned and must stop response enrichment.
func (c *Component) resolveEntityLabels(ctx context.Context, entityIDs []string) (map[string]string, error) {
	if len(entityIDs) == 0 {
		return nil, nil
	}

	entities, err := c.loadEntities(ctx, entityIDs)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		c.logger.Debug("failed to load entities for label resolution", "error", err)
		return nil, nil
	}

	labels := make(map[string]string, len(entities))
	for _, entity := range entities {
		label := resolveLabel(entity)
		if label != "" {
			labels[entity.ID] = label
		}
	}
	return labels, nil
}

// resolveLabel extracts a human-readable label from an entity by trying predicates
// in priority order. Falls back to the first string-valued triple object.
func resolveLabel(entity *gtypes.EntityState) string {
	for _, pred := range labelPredicates {
		if val, ok := entity.GetPropertyValue(pred); ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	// Last resort: first string-valued object from any triple
	for _, t := range entity.Triples {
		if s, ok := t.Object.(string); ok && s != "" && !message.IsValidEntityID(s) {
			return s
		}
	}
	return ""
}

// buildEntityDigests creates lightweight entity context from IDs, semantic scores,
// and pre-resolved labels.
func buildEntityDigests(entityIDs []string, semanticScores map[string]float64, labels map[string]string) []EntityDigest {
	digests := make([]EntityDigest, len(entityIDs))
	for i, id := range entityIDs {
		label := labels[id]
		if label == "" {
			label = extractEntityInstance(id)
		}
		digests[i] = EntityDigest{
			ID:        id,
			Type:      extractEntityType(id),
			Label:     label,
			Relevance: semanticScores[id],
		}
	}
	return digests
}

// buildEntityDigestsFromEntities creates entity digests from loaded EntityState objects.
// Labels are resolved from entity triples rather than a pre-built map.
func buildEntityDigestsFromEntities(entities []*gtypes.EntityState) []EntityDigest {
	digests := make([]EntityDigest, len(entities))
	for i, e := range entities {
		label := resolveLabel(e)
		if label == "" {
			label = extractEntityInstance(e.ID)
		}
		digests[i] = EntityDigest{
			ID:    e.ID,
			Type:  extractEntityType(e.ID),
			Label: label,
		}
	}
	return digests
}

// enrichCommunitySummaries populates MemberCount and representative entity
// digests on each CommunitySummary.
//
// Representatives are chosen by selectDigestEntities (Lever A of
// thematic-synthesis-context): query-relevant community members when
// semanticScores are supplied, backfilled from / falling back entirely to the
// community's PageRank RepEntities. The selected IDs are batch-loaded ONCE into
// full EntityStates so each digest's Label (resolveLabel) and Tags
// (collectDigestTags — Lever B) come from a single pass, without a second
// label-only round-trip.
func (c *Component) enrichCommunitySummaries(ctx context.Context, summaries []CommunitySummary, semanticScores map[string]float64) ([]CommunitySummary, error) {
	if c.communityCache == nil || len(summaries) == 0 {
		return summaries, nil
	}

	// Collect the selected representative IDs across matched communities
	// (deduplicated for a single batch load). Indexed by summary POSITION, not
	// community ID: a community ID is unique only within a level, and summaries
	// can span levels (findCommunitiesForEntities walks every level), so an
	// ID-keyed map silently gave two same-ID summaries the last level's rep
	// entities while each kept its own MemberCount — a digest stitched from two
	// different communities. Position is unique by construction.
	seen := make(map[string]bool)
	var selectedIDs []string
	selectedBySummary := make([][]string, len(summaries))
	for i := range summaries {
		// Level-qualified: community IDs are only unique within a level, and
		// Level here came off the same cache record as CommunityID.
		comm := c.communityCache.GetCommunity(summaries[i].Level, summaries[i].CommunityID)
		if comm == nil {
			continue
		}
		summaries[i].MemberCount = len(comm.Members)
		selected := selectDigestEntities(comm, semanticScores)
		selectedBySummary[i] = selected
		for _, id := range selected {
			if !seen[id] {
				seen[id] = true
				selectedIDs = append(selectedIDs, id)
			}
		}
	}

	// Single batch load of the selected entities; label + tags derive from it.
	entities, err := c.loadDigestEntities(ctx, selectedIDs)
	if err != nil {
		return nil, err
	}

	// Populate Entities on each summary
	for i := range summaries {
		sel := selectedBySummary[i]
		if len(sel) == 0 {
			continue
		}
		digests := make([]EntityDigest, len(sel))
		for j, id := range sel {
			digests[j] = buildRepDigest(id, entities[id])
		}
		summaries[i].Entities = digests
	}
	return summaries, nil
}

// loadDigestEntities batch-loads the given entity IDs and returns them keyed by
// ID for digest enrichment. Mirrors resolveEntityLabels' error contract:
// authoritative graph-state failures propagate and must stop enrichment;
// ordinary lookup failures degrade to a nil map (unlabelled, untagged digests).
func (c *Component) loadDigestEntities(ctx context.Context, entityIDs []string) (map[string]*gtypes.EntityState, error) {
	if len(entityIDs) == 0 {
		return nil, nil
	}
	entities, err := c.loadEntities(ctx, entityIDs)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		c.logger.Debug("failed to load entities for digest enrichment", "error", err)
		return nil, nil
	}
	byID := make(map[string]*gtypes.EntityState, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}
	return byID, nil
}

// buildRepDigest builds a representative EntityDigest from a selected ID and its
// (possibly nil) loaded EntityState. Label falls back to the ID instance segment
// when no entity or label is available; Tags come from the entity's
// classification triples, capped at MaxDigestTags.
func buildRepDigest(id string, entity *gtypes.EntityState) EntityDigest {
	label := ""
	var tags []string
	if entity != nil {
		label = resolveLabel(entity)
		tags = collectDigestTags(entity, MaxDigestTags)
	}
	if label == "" {
		label = extractEntityInstance(id)
	}
	return EntityDigest{
		ID:    id,
		Type:  extractEntityType(id),
		Label: label,
		Tags:  tags,
	}
}

// formatRepDigest renders a representative digest for the synthesis context as
// "Label [Type]", appending " {tags: t1, t2, …}" when the digest carries
// classification tags (Lever B). Shared by the LLM prompt (buildAnswerPrompt)
// and the template floor (synthesizeAnswer) so both surface identical context.
func formatRepDigest(e EntityDigest) string {
	base := fmt.Sprintf("%s [%s]", e.Label, e.Type)
	if len(e.Tags) == 0 {
		return base
	}
	return fmt.Sprintf("%s {tags: %s}", base, strings.Join(e.Tags, ", "))
}

// synthesizeQueryAnswer delegates to the component's answer synthesizer (LLM or template).
// Returns the SynthesisOutcome carrying answer text, model name, and
// the Degraded flag the caller propagates onto GlobalSearchResponse
// for the agent to surface.
//
// When the synthesizer returns a non-nil error (programmer-error-shape
// — distinct from the "LLM failed, fall back to template" path that
// the synthesizer absorbs internally), this function ALSO falls back
// to template synthesis with Degraded=true. Without this defensive
// fallback, a synthesizer-error path returns the zero SynthesisOutcome
// and the response carries an empty Answer with no Degraded flag —
// silently worse than the explicit fallback path. The whole point of
// beta.45 is that callers never see a less-useful response than
// template, so we synthesize template here as the floor.
func (c *Component) synthesizeQueryAnswer(ctx context.Context, query string, summaries []CommunitySummary, totalEntities int) SynthesisOutcome {
	if c.answerSynthesizer == nil {
		return SynthesisOutcome{Answer: synthesizeAnswer(summaries, totalEntities)}
	}
	outcome, err := c.answerSynthesizer.Synthesize(ctx, query, summaries, totalEntities)
	if err != nil {
		c.logger.Warn("answer synthesizer returned error; using template floor",
			"error", err,
			"reason", classifyDegradedReason(err))
		return SynthesisOutcome{
			Answer:   synthesizeAnswer(summaries, totalEntities),
			Degraded: true,
			Reason:   classifyDegradedReason(err),
		}
	}
	return outcome
}

// enrichGlobalResponse adds community context and answer synthesis to a
// GlobalSearchResponse. Looks up communities for the given entity IDs,
// enriches them with representative digests, and synthesizes an answer.
// No-op if no communities match or community cache is unavailable.
//
// semanticScores steers query-relevant representative selection (Lever A) when
// available; callers on paths without per-entity relevance (path/entity-lookup/
// temporal/spatial) pass nil, leaving PageRank representative selection unchanged.
func (c *Component) enrichGlobalResponse(ctx context.Context, resp *GlobalSearchResponse, queryText string, entityIDs []string, semanticScores map[string]float64) error {
	if len(entityIDs) == 0 {
		return nil
	}
	communityMatches := c.findCommunitiesForEntities(entityIDs)
	if len(communityMatches) == 0 {
		return nil
	}
	enriched, err := c.enrichCommunitySummaries(ctx, communityMatches, semanticScores)
	if err != nil {
		return err
	}
	resp.CommunitySummaries = enriched
	synth := c.synthesizeQueryAnswer(ctx, queryText, enriched, resp.Count)
	resp.Answer = synth.Answer
	resp.AnswerModel = synth.Model
	resp.Degraded = synth.Degraded
	resp.DegradedReason = synth.Reason
	return nil
}

// synthesizeAnswer produces a template-based natural language answer from
// community summaries. No LLM required — used as fallback when no
// answer_synthesis endpoint is configured.
func synthesizeAnswer(summaries []CommunitySummary, totalEntities int) string {
	if len(summaries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d entities across %d knowledge clusters.\n", totalEntities, len(summaries)))

	limit := len(summaries)
	if limit > MaxAnswerClusters {
		limit = MaxAnswerClusters
	}

	for _, s := range summaries[:limit] {
		b.WriteByte('\n')

		// Community header with member count
		if s.MemberCount > 0 {
			b.WriteString(fmt.Sprintf("(%d entities, %.0f%% match) ", s.MemberCount, s.Relevance*100))
		}

		// Community summary (already a narrative from LLM or statistical)
		if s.Summary != "" {
			b.WriteString(s.Summary)
		}

		// Representative entities (Label [Type] {tags: …} — floor mirrors the LLM prompt)
		if len(s.Entities) > 0 {
			names := make([]string, len(s.Entities))
			for i, e := range s.Entities {
				names[i] = formatRepDigest(e)
			}
			b.WriteString(fmt.Sprintf(" Representatives: %s.", strings.Join(names, ", ")))
		}

		// Keywords
		if len(s.Keywords) > 0 {
			kwLimit := len(s.Keywords)
			if kwLimit > 5 {
				kwLimit = 5
			}
			b.WriteString(fmt.Sprintf(" Key themes: %s.", strings.Join(s.Keywords[:kwLimit], ", ")))
		}

		b.WriteByte('\n')
	}

	return b.String()
}

// findCommunityWithFallback looks up an entity's community at the exact requested level.
// Tier 1: Try the cache at requested level
// Tier 2: Query storage directly via NATS (handles cache sync delays)
// Returns nil if entity is not in a community at the requested level.
func (c *Component) findCommunityWithFallback(ctx context.Context, entityID string, requestedLevel int) *clustering.Community {
	// Tier 1: Try requested level from cache
	community := c.communityCache.GetEntityCommunity(entityID, requestedLevel)
	if community != nil {
		if c.promMetrics != nil {
			c.promMetrics.recordCacheHit()
		}
		return community
	}

	// Cache miss - record metric before trying storage fallback
	if c.promMetrics != nil {
		c.promMetrics.recordCacheMiss()
	}

	// Tier 2: Query storage directly via NATS (handles cache sync delays)
	// This bypasses the async cache and queries graph-clustering's KV directly
	community = c.fetchEntityCommunityFromStorage(ctx, entityID, requestedLevel)
	if community != nil {
		c.logger.Debug("community found via storage query",
			"entity_id", entityID,
			"level", requestedLevel,
			"community_id", community.ID)
		if c.promMetrics != nil {
			c.promMetrics.recordStorageHit()
		}
		return community
	}

	// Entity not in a community at requested level
	if c.promMetrics != nil {
		c.promMetrics.recordStorageMiss()
	}
	c.logger.Debug("entity not in community at level",
		"entity_id", entityID,
		"level", requestedLevel)

	return nil
}

// fetchEntityCommunityFromStorage queries graph-clustering directly via NATS request.
// This bypasses the cache and reads from KV storage, handling cache sync delays.
func (c *Component) fetchEntityCommunityFromStorage(ctx context.Context, entityID string, level int) *clustering.Community {
	req := map[string]any{
		"entity_id": entityID,
		"level":     level,
	}
	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Debug("storage query marshal failed",
			"entity_id", entityID,
			"level", level,
			"error", err)
		return nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// ADR-060: RequestClassified surfaces a handler failure via err instead of
	// an "error: <msg>" body that would mis-decode as a nil community.
	resp, err := c.natsClient.RequestClassified(queryCtx, "graph.clustering.query.entity", data, 5*time.Second)
	cancel()

	if err != nil {
		c.logger.Debug("storage query request failed",
			"entity_id", entityID,
			"level", level,
			"error", err)
		return nil
	}

	var result struct {
		EntityID  string                `json:"entity_id"`
		Level     int                   `json:"level"`
		Community *clustering.Community `json:"community"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		c.logger.Debug("storage query unmarshal failed",
			"entity_id", entityID,
			"level", level,
			"error", err,
			"response", string(resp))
		return nil
	}

	if result.Community != nil {
		c.logger.Debug("storage query found community",
			"entity_id", entityID,
			"level", level,
			"community_id", result.Community.ID,
			"member_count", len(result.Community.Members))
		return result.Community
	}

	return nil
}

// scoredEntity pairs an entity with its relevance score for sorting.
type scoredEntity struct {
	entity *gtypes.EntityState
	score  float64
}

// filterEntitiesByQuery filters and sorts entities by text relevance score.
// Entities below minScore are excluded. Results are sorted by score descending.
func filterEntitiesByQuery(entities []*gtypes.EntityState, query string, minScore float64) []*gtypes.EntityState {
	queryTerms := strings.Fields(strings.ToLower(query))

	if len(queryTerms) == 0 {
		return entities // No filtering if query is empty
	}

	scored := make([]scoredEntity, 0, len(entities))
	for _, entity := range entities {
		score := scoreEntityQuery(entity, queryTerms)
		if score >= minScore {
			scored = append(scored, scoredEntity{entity: entity, score: score})
		}
	}

	// Sort by score descending — most relevant entities first
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]*gtypes.EntityState, len(scored))
	for i, se := range scored {
		result[i] = se.entity
	}
	return result
}

// scoreEntityQuery scores how well an entity matches the query terms.
// Returns a value between 0.0 and 1.0 representing the proportion of
// query terms that match, weighted by match quality:
//   - +1.0 for entity type match (5th segment of ID)
//   - +0.5 for each term found in a triple predicate or string value
//
// Normalized by the number of query terms.
func scoreEntityQuery(entity *gtypes.EntityState, queryTerms []string) float64 {
	if len(queryTerms) == 0 {
		return 0
	}

	entityType := strings.ToLower(extractEntityType(entity.ID))

	// Build searchable text from triple predicates and string values
	var searchText strings.Builder
	for _, triple := range entity.Triples {
		searchText.WriteString(strings.ToLower(triple.Predicate))
		searchText.WriteString(" ")
		if strVal, ok := triple.Object.(string); ok {
			searchText.WriteString(strings.ToLower(strVal))
			searchText.WriteString(" ")
		}
	}
	searchable := searchText.String()

	totalScore := 0.0
	for _, term := range queryTerms {
		term = strings.ToLower(term)
		if entityType == term {
			totalScore += 1.0 // Strong signal: entity type matches query term
		} else if strings.Contains(searchable, term) {
			totalScore += 0.5 // Moderate signal: term found in predicates/values
		}
	}

	return totalScore / float64(len(queryTerms))
}

// resolveCommunitySummary is the ONE place the tiered community-summary fallback
// lives: it returns the LLM summary joined from COMMUNITY_SUMMARIES by membership
// hash when one exists, and otherwise the community's statistical summary. A
// community without an LLM summary yet degrades to a non-empty statistical answer,
// never an empty one (ADR-087). This governs only CommunitySummary.Summary; the
// tag-enriched representative context on CommunitySummary.Entities is sourced
// separately from ENTITY_STATES (#702) and is unaffected.
func (c *Component) resolveCommunitySummary(comm *clustering.Community) string {
	if comm == nil {
		return ""
	}
	if c.communityCache != nil {
		if summary, ok := c.communityCache.SummaryFor(comm); ok {
			return summary
		}
	}
	return comm.StatisticalSummary
}

// scoreCommunitySummaries scores communities based on query relevance
// Returns communities sorted by relevance (highest first).
//
// summaryOf resolves each community's summary text through the tiered join (LLM
// summary store → statistical floor); it is threaded in rather than read off the
// community record because after the ownership split Community.LLMSummary is
// always empty (ADR-087).
func scoreCommunitySummaries(communities []*clustering.Community, query string, summaryOf func(*clustering.Community) string) []*clustering.Community {
	type scoredCommunity struct {
		community *clustering.Community
		score     float64
	}

	query = strings.ToLower(query)
	queryTerms := strings.Fields(query)

	scored := make([]scoredCommunity, 0, len(communities))

	for _, comm := range communities {
		score := 0.0

		// Score based on summary text (tiered join → statistical floor).
		summary := summaryOf(comm)
		if summary != "" {
			summaryLower := strings.ToLower(summary)
			for _, term := range queryTerms {
				if strings.Contains(summaryLower, term) {
					score += ScoreWeightSummary
				}
			}
		}

		// Score based on keywords
		for _, keyword := range comm.Keywords {
			keywordLower := strings.ToLower(keyword)
			for _, term := range queryTerms {
				if strings.Contains(keywordLower, term) || strings.Contains(term, keywordLower) {
					score += ScoreWeightKeyword
				}
			}
		}

		scored = append(scored, scoredCommunity{
			community: comm,
			score:     score,
		})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Extract sorted communities
	result := make([]*clustering.Community, len(scored))
	for i, sc := range scored {
		result[i] = sc.community
	}

	return result
}

// extractRelationships extracts relationships between entities in the result set.
// Only includes relationships where both endpoints are in the entity set.
func (c *Component) extractRelationships(_ context.Context, entities []*gtypes.EntityState) []Relationship {
	// Build entity ID set for fast lookup
	entitySet := make(map[string]bool)
	for _, e := range entities {
		entitySet[e.ID] = true
	}

	var relationships []Relationship

	// Extract relationships from entity triples
	for _, entity := range entities {
		for _, triple := range entity.Triples {
			// Only include relationships (object is entity reference)
			if triple.IsRelationship() {
				targetID, ok := triple.Object.(string)
				if ok && entitySet[targetID] {
					relationships = append(relationships, Relationship{
						FromEntityID: entity.ID,
						ToEntityID:   targetID,
						Predicate:    triple.Predicate,
					})
				}
			}
		}
	}

	return relationships
}

// buildSources builds source attribution for search results.
// Maps entities to their communities and assigns relevance scores.
func (c *Component) buildSources(entities []*gtypes.EntityState, semanticHits []SemanticHit, communitySummaries []CommunitySummary) []Source {
	// Build semantic score lookup
	semanticScores := make(map[string]float64)
	for _, hit := range semanticHits {
		semanticScores[hit.EntityID] = hit.Score
	}

	// Build entity → community lookup from summaries
	entityToCommunity := make(map[string]string)
	for _, cs := range communitySummaries {
		// We don't have member lists in CommunitySummary, so skip community mapping
		// unless we can access it from cache
		if c.communityCache != nil {
			community := c.communityCache.GetCommunity(cs.Level, cs.CommunityID)
			if community != nil {
				for _, member := range community.Members {
					entityToCommunity[member] = cs.CommunityID
				}
			}
		}
	}

	sources := make([]Source, 0, len(entities))
	for i, entity := range entities {
		// Calculate relevance: use semantic score if available, otherwise position-based
		relevance := 1.0 - (float64(i) / float64(len(entities)+1))
		if score, ok := semanticScores[entity.ID]; ok {
			relevance = score
		}

		sources = append(sources, Source{
			EntityID:    entity.ID,
			CommunityID: entityToCommunity[entity.ID],
			Relevance:   relevance,
		})
	}

	// Sort by relevance descending
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Relevance > sources[j].Relevance
	})

	return sources
}

// PathRAGResult wraps the result of a PathRAG query for use in handleGlobalSearch.
// It contains EntityState objects rather than PathEntity for consistency with GlobalSearchResponse.
type PathRAGResult struct {
	Entities  []*gtypes.EntityState
	Truncated bool
}

// executePathSearchForGlobal executes a PathRAG traversal starting from the given entity.
// This is used by handleGlobalSearch when path intent is detected.
// It converts the PathSearchResponse to a format usable by GlobalSearchResponse.
func (c *Component) executePathSearchForGlobal(ctx context.Context, startEntityID string, predicates []string) (*PathRAGResult, error) {
	// Build PathSearch request with reasonable defaults for NL queries
	req := PathSearchRequest{
		StartEntity: startEntityID,
		MaxDepth:    3,
		MaxNodes:    100,
		Predicates:  predicates, // May be nil (all predicates)
	}

	// Ensure pathSearcher is initialized
	searcher := c.pathSearcher
	if searcher == nil {
		searcher = NewPathSearcher(c.natsClient, c.config.QueryTimeout, c.config.MaxDepth, c.logger)
	}

	resp, err := searcher.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	// Extract entity IDs from PathSearchResponse
	entityIDs := make([]string, len(resp.Entities))
	for i, e := range resp.Entities {
		entityIDs[i] = e.ID
	}

	// Load full entity data via graph-ingest
	entities, err := c.loadEntities(ctx, entityIDs)
	if err != nil {
		if gtypes.IsStateContractError(err) {
			return nil, err
		}
		return nil, errs.WrapTransient(err, "GraphQuery", "executePathSearchForGlobal", "load entities")
	}

	return &PathRAGResult{
		Entities:  entities,
		Truncated: resp.Truncated,
	}, nil
}
