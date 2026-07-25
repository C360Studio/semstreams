// Package graphembedding query handlers
package graphembedding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to similar entity query
	sub, err := c.natsClient.SubscribeForRequests(ctx, "graph.embedding.query.similar", c.handleQuerySimilarNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe similar query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to text search query
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.embedding.query.search", c.handleQuerySearchNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe search query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	// Subscribe to the honest embedding-readiness signal (ADR-066 §3). No consumer
	// gates on embedding.ready until it is proven; this exposes it for observability.
	sub, err = c.natsClient.SubscribeForRequests(ctx, "graph.embedding.query.status", c.handleEmbeddingStatusNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe status query")
	}
	c.querySubscriptions = append(c.querySubscriptions, sub)

	c.logger.Info("query handlers registered",
		"subjects", []string{"graph.embedding.query.similar", "graph.embedding.query.search", "graph.embedding.query.status"})

	return nil
}

// SimilarRequest is the request format for similar entity queries
type SimilarRequest struct {
	EntityID string `json:"entity_id"`
	Limit    int    `json:"limit"`
}

// SimilarResponse is the response format for similar entity queries
type SimilarResponse struct {
	EntityID string          `json:"entity_id"`
	Similar  []SimilarEntity `json:"similar"`
	Duration string          `json:"duration"`
}

// SimilarEntity represents an entity with similarity score
type SimilarEntity struct {
	EntityID   string  `json:"entity_id"`
	Similarity float64 `json:"similarity"`
}

// SearchRequest is the request format for text search queries
type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	// Scope optionally constrains the search to candidates whose entity ID
	// matches at least one of these dot-delimited prefixes (OR-matched, via
	// graph.MatchesAnyIDPrefix). Empty/absent = no filter (today's behavior).
	// The filter is applied at the candidate source in every similarity path so
	// a small domain is never crowded out of the ranked window (ADR-071).
	// Decoded with json.Unmarshal (unknown-field-tolerant), so a producer that
	// sends Scope to an un-migrated server degrades to an unscoped search.
	Scope []string `json:"scope,omitempty"`
}

// SearchResponse is the response format for text search queries
type SearchResponse struct {
	Query        string         `json:"query"`
	Results      []SearchResult `json:"results"`
	Duration     string         `json:"duration"`
	EmbedderType string         `json:"embedder_type,omitempty"` // "bm25" or "http"
}

// SearchResult represents a search result with relevance score
type SearchResult struct {
	EntityID   string  `json:"entity_id"`
	Similarity float64 `json:"similarity"`
}

// handleQuerySimilarNATS handles similar entity query requests via NATS request/reply
func (c *Component) handleQuerySimilarNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.ensureBootstrapReady(); err != nil {
		return nil, err
	}
	start := time.Now()

	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(requestCtx, 30*time.Second)
	defer cancel()

	// Parse request
	var req SimilarRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "handleQuerySimilarNATS", "handler", "request unmarshal")
	}

	// Validate request
	if req.EntityID == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "handleQuerySimilarNATS", "handler", "empty entity_id")
	}

	// Apply defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Get source entity embedding
	if c.storage == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "handleQuerySimilarNATS", "handler", "storage not initialized")
	}

	sourceRecord, err := c.storage.GetEmbedding(ctx, req.EntityID)
	if err != nil {
		return nil, errs.Wrap(err, "handleQuerySimilarNATS", "handler", "get source embedding")
	}
	// The three per-entity misses below all mean "this SOURCE entity has no usable
	// embedding to query FROM." They carry the stable ErrorCodeEmbeddingUnavailable
	// (Invalid class) so a consumer distinguishes a definitive per-entity miss from a
	// malformed/unknown reply BY CODE, never by message text: the semantic-edge cache
	// fails open to empty on this code, but counts an uncoded/parse error toward its
	// coverage-threshold abort rather than caching a hollow empty (#662 / Codex P1#2).
	// It is per-entity, NOT the producer-wide ErrorCodeIndexNotReady — the index is
	// sound, this one entity just has no vector yet.
	if sourceRecord == nil {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEmbeddingUnavailable,
			fmt.Errorf("entity not found: %s", req.EntityID))
	}
	if sourceRecord.Status != embedding.StatusGenerated {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEmbeddingUnavailable,
			fmt.Errorf("embedding not ready for %s: status=%s", req.EntityID, sourceRecord.Status))
	}
	if len(sourceRecord.Vector) == 0 {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEmbeddingUnavailable,
			fmt.Errorf("no vector for entity %s", req.EntityID))
	}

	// Find similar entities by scanning all embeddings. The similar-to-entity
	// path carries no scope (scope is an NL-search concern only).
	similar, err := c.findSimilarEntities(ctx, req.EntityID, sourceRecord.Vector, nil, limit)
	if err != nil {
		return nil, errs.Wrap(err, "handleQuerySimilarNATS", "handler", "find similar entities")
	}

	response := SimilarResponse{
		EntityID: req.EntityID,
		Similar:  similar,
		Duration: time.Since(start).String(),
	}

	return json.Marshal(response)
}

// handleQuerySearchNATS handles text search query requests via NATS request/reply
func (c *Component) handleQuerySearchNATS(requestCtx context.Context, data []byte) ([]byte, error) {
	if err := c.ensureBootstrapReady(); err != nil {
		return nil, err
	}
	start := time.Now()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(requestCtx, 30*time.Second)
	defer cancel()

	// Parse request
	var req SearchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "handleQuerySearchNATS", "handler", "request unmarshal")
	}

	// Validate request
	if req.Query == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "handleQuerySearchNATS", "handler", "empty query")
	}
	// Reject malformed scope before invoking the embedder. GenerateQuery may be
	// a paid remote call; invalid requests must not consume that resource.
	if err := validateEntityIDScope(req.Scope); err != nil {
		return nil, err
	}

	// Apply defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Check embedder is available
	if c.embedder == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "handleQuerySearchNATS", "handler", "embedder not initialized")
	}

	// Generate embedding for query text via the QUERY-side path so asymmetric models
	// get their query instruction prefix (gh#438); symmetric embedders (BM25) no-op it.
	vectors, err := c.embedder.GenerateQuery(ctx, []string{req.Query})
	if err != nil {
		return nil, errs.Wrap(err, "handleQuerySearchNATS", "handler", "generate query embedding")
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "handleQuerySearchNATS", "handler", "empty query embedding")
	}

	queryVector := vectors[0]

	// Find similar entities, scoped to the requested ID prefixes (if any).
	results, err := c.findSimilarEntities(ctx, "", queryVector, req.Scope, limit)
	if err != nil {
		return nil, errs.Wrap(err, "handleQuerySearchNATS", "handler", "find similar entities")
	}

	// Convert to search results
	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			EntityID:   r.EntityID,
			Similarity: r.Similarity,
		}
	}

	response := SearchResponse{
		Query:        req.Query,
		Results:      searchResults,
		Duration:     time.Since(start).String(),
		EmbedderType: c.config.EmbedderType,
	}

	return json.Marshal(response)
}

// findSimilarEntities finds entities similar to the given vector.
//
// It first attempts to serve the query from the in-memory vector cache
// (zero KV round-trips). If the cache is not yet warm it falls back to
// the original O(n) KV scan path so queries are never blocked during startup.
//
// A non-empty scope constrains candidates to entity IDs matching at least one
// of the given prefixes (ADR-071). The SAME filter is applied on BOTH paths —
// the warm cache and the cold KV scan — via one shared predicate, BEFORE the
// expensive per-candidate op (cosine in the cache loop / the GetEmbedding KV
// round-trip in the scan). Filtering only the cold fallback would be a silent
// no-op for essentially every warm-production query. An empty/nil scope keeps a
// nil predicate so unscoped queries are unchanged.
func (c *Component) findSimilarEntities(ctx context.Context, excludeID string, queryVector []float32, scope []string, limit int) ([]SimilarEntity, error) {
	if err := validateEntityIDScope(scope); err != nil {
		return nil, err
	}
	if c.storage == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "findSimilarEntities", "helper", "storage not initialized")
	}

	var keep func(string) bool
	if len(scope) > 0 {
		keep = func(id string) bool { return graph.MatchesAnyIDPrefix(id, scope) }
	}

	// Try in-memory cache first (zero KV I/O). The scope predicate filters
	// inside the cache loop, before cosine similarity.
	if scored, ok := c.storage.FindSimilarFromCache(excludeID, queryVector, keep, limit); ok {
		results := make([]SimilarEntity, len(scored))
		for i, s := range scored {
			results[i] = SimilarEntity{EntityID: s.EntityID, Similarity: s.Similarity}
		}
		return results, nil
	}

	// Cache not ready yet — fall back to KV scan.
	entityIDs, err := c.storage.ListGeneratedEntityIDs(ctx)
	if err != nil {
		return nil, errs.Wrap(err, "findSimilarEntities", "helper", "list entity IDs")
	}

	// Calculate similarities. Uses the shared embedding.ScoredEntity type so the
	// cold path sorts through the SAME canonical comparator as the warm cache path
	// (embedding.ScoredEntityLess) — an equal-similarity group crossing the top-k
	// boundary must resolve identically on both paths.
	var scores []embedding.ScoredEntity

	for _, entityID := range entityIDs {
		if err := ctx.Err(); err != nil {
			return nil, errs.WrapTransient(err, "findSimilarEntities", "helper", "scan embeddings")
		}

		// Skip the source entity
		if entityID == excludeID {
			continue
		}

		// Scope filter runs BEFORE the per-candidate KV round-trip: on the
		// httpx case a docs scope turns ~1334 GetEmbedding reads into ~30.
		if keep != nil && !keep(entityID) {
			continue
		}

		record, err := c.storage.GetEmbedding(ctx, entityID)
		if err != nil {
			return nil, errs.Wrap(err, "findSimilarEntities", "helper", "get candidate embedding")
		}
		// GetEmbedding returns nil,nil only for a proven key-not-found race
		// between ListGeneratedEntityIDs and this point read.
		if record == nil {
			continue
		}
		if record.Status != embedding.StatusGenerated || len(record.Vector) == 0 {
			continue
		}

		// Calculate cosine similarity
		sim := embedding.CosineSimilarity(queryVector, record.Vector)
		scores = append(scores, embedding.ScoredEntity{EntityID: entityID, Similarity: sim})
	}

	// Sort by the canonical (similarity DESC, entity ID ASC) ordering so the
	// top-k tie-break at the boundary is deterministic regardless of the KV
	// key-enumeration order.
	sort.Slice(scores, func(i, j int) bool {
		return embedding.ScoredEntityLess(scores[i], scores[j])
	})

	// Take top N
	if len(scores) > limit {
		scores = scores[:limit]
	}

	// Convert to response format
	results := make([]SimilarEntity, len(scores))
	for i, s := range scores {
		results[i] = SimilarEntity{
			EntityID:   s.EntityID,
			Similarity: s.Similarity,
		}
	}

	return results, nil
}

func validateEntityIDScope(scope []string) error {
	for _, prefix := range scope {
		// graph.MatchesAnyIDPrefix documents an empty element as explicit
		// match-all, so preserve it instead of feeding it to the non-empty API.
		if prefix == "" {
			continue
		}
		if err := semtypes.ValidateEntityIDPrefix(prefix); err != nil {
			return err
		}
	}
	return nil
}
