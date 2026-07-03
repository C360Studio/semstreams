// Package graphembedding query handlers
package graphembedding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/c360studio/semstreams/pkg/errs"
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
func (c *Component) handleQuerySimilarNATS(_ context.Context, data []byte) ([]byte, error) {
	start := time.Now()

	// Create context with timeout for KV operations
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if sourceRecord == nil {
		return nil, errs.WrapInvalid(errs.ErrKeyNotFound, "handleQuerySimilarNATS", "handler", fmt.Sprintf("entity not found: %s", req.EntityID))
	}
	if sourceRecord.Status != embedding.StatusGenerated {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "handleQuerySimilarNATS", "handler", fmt.Sprintf("embedding not ready for %s: status=%s", req.EntityID, sourceRecord.Status))
	}
	if len(sourceRecord.Vector) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "handleQuerySimilarNATS", "handler", fmt.Sprintf("no vector for entity %s", req.EntityID))
	}

	// Find similar entities by scanning all embeddings
	similar, err := c.findSimilarEntities(ctx, req.EntityID, sourceRecord.Vector, limit)
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
func (c *Component) handleQuerySearchNATS(_ context.Context, data []byte) ([]byte, error) {
	start := time.Now()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	// Find similar entities
	results, err := c.findSimilarEntities(ctx, "", queryVector, limit)
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
func (c *Component) findSimilarEntities(ctx context.Context, excludeID string, queryVector []float32, limit int) ([]SimilarEntity, error) {
	if c.storage == nil {
		return nil, errs.WrapFatal(errs.ErrInvalidConfig, "findSimilarEntities", "helper", "storage not initialized")
	}

	// Try in-memory cache first (zero KV I/O).
	if scored, ok := c.storage.FindSimilarFromCache(excludeID, queryVector, limit); ok {
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

	// Calculate similarities
	type scored struct {
		entityID   string
		similarity float64
	}
	var scores []scored

	for _, entityID := range entityIDs {
		// Skip the source entity
		if entityID == excludeID {
			continue
		}

		record, err := c.storage.GetEmbedding(ctx, entityID)
		if err != nil || record == nil {
			continue
		}
		if record.Status != embedding.StatusGenerated || len(record.Vector) == 0 {
			continue
		}

		// Calculate cosine similarity
		sim := embedding.CosineSimilarity(queryVector, record.Vector)
		scores = append(scores, scored{entityID: entityID, similarity: sim})
	}

	// Sort by similarity descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].similarity > scores[j].similarity
	})

	// Take top N
	if len(scores) > limit {
		scores = scores[:limit]
	}

	// Convert to response format
	results := make([]SimilarEntity, len(scores))
	for i, s := range scores {
		results[i] = SimilarEntity{
			EntityID:   s.entityID,
			Similarity: s.similarity,
		}
	}

	return results, nil
}
