package indexmanager

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/c360/semstreams/pkg/embedding"
)

// scoredHit represents a search result with its similarity score
type scoredHit struct {
	entityID string
	score    float64
}

// initializeSemanticSearch initializes semantic search components if enabled.
//
// This is called during NewManager initialization. If embedding is disabled,
// this is a no-op and semantic search methods will return errors.
//
// Provider selection with automatic fallback:
//   - "http": Try HTTP service, fall back to BM25 if unavailable
//   - "bm25": Pure Go BM25 embedder (default)
//   - "disabled" or Enabled=false: Disable semantic search
//   - Empty/unspecified: Defaults to BM25
func (m *Manager) initializeSemanticSearch(buckets map[string]jetstream.KeyValue) error {
	// Check if semantic search is explicitly disabled
	if !m.config.Embedding.Enabled || m.config.Embedding.Provider == "disabled" {
		m.logger.Info("Semantic search disabled", "provider", m.config.Embedding.Provider)
		// Caches remain nil when disabled
		return nil
	}

	// Default to BM25 if no provider specified
	provider := m.config.Embedding.Provider
	if provider == "" {
		provider = "bm25"
		m.logger.Info("No embedding provider specified, defaulting to BM25", "provider", "bm25")
	}

	m.logger.Info("Initializing semantic search", "provider", provider)

	// Create embedding cache if configured
	var embeddingCache embedding.Cache
	if m.config.Embedding.CacheBucket != "" {
		cacheBucket, ok := buckets[m.config.Embedding.CacheBucket]
		if ok && cacheBucket != nil {
			embeddingCache = embedding.NewNATSCache(cacheBucket)
			m.logger.Info("Embedding cache enabled", "bucket", m.config.Embedding.CacheBucket)
		} else {
			m.logger.Warn("Embedding cache bucket not found, caching disabled", "bucket", m.config.Embedding.CacheBucket)
		}
	}

	// Create embedder based on provider with automatic fallback
	var err error
	switch provider {
	case "http":
		// HTTP embedder for semembed, LocalAI, OpenAI, etc.
		httpConfig := embedding.HTTPConfig{
			BaseURL: m.config.Embedding.HTTPEndpoint,
			Model:   m.config.Embedding.HTTPModel,
			Cache:   embeddingCache,
			Logger:  m.logger,
		}
		m.embedder, err = embedding.NewHTTPEmbedder(httpConfig)
		if err != nil {
			return errors.WrapTransient(
				err,
				"IndexManager",
				"initializeSemanticSearch",
				"failed to create HTTP embedder",
			)
		}

		// Test connectivity with a simple embedding request
		testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, testErr := m.embedder.Generate(testCtx, []string{"connectivity test"})
		if testErr != nil {
			m.logger.Warn("HTTP embedding service unavailable - falling back to BM25",
				"endpoint", m.config.Embedding.HTTPEndpoint,
				"error", testErr.Error(),
				"fallback", "bm25",
				"hint", "Start embedding service with: task services:start:embedding")

			// Automatic fallback to BM25 instead of disabling
			m.embedder = embedding.NewBM25Embedder(embedding.BM25Config{
				Dimensions: 384,
				K1:         1.5,
				B:          0.75,
			})
			m.logger.Info("Fallback to BM25 embedder successful",
				"dimensions", m.embedder.Dimensions(),
				"model", m.embedder.Model())

			// Record fallback event
			if m.promMetrics != nil {
				m.promMetrics.embeddingFallbacks.Inc()
				m.promMetrics.embeddingProvider.Set(1) // 1 = BM25
			}
		} else {
			// HTTP embedder is working
			if m.promMetrics != nil {
				m.promMetrics.embeddingProvider.Set(2) // 2 = HTTP
			}
		}

	case "bm25":
		// Pure Go BM25 embedder - no external dependencies
		m.embedder = embedding.NewBM25Embedder(embedding.BM25Config{
			Dimensions: 384,  // Match neural embedding dimensions for compatibility
			K1:         1.5,  // Standard BM25 parameter
			B:          0.75, // Standard BM25 parameter
		})
		m.logger.Info("BM25 embedder initialized (pure Go lexical search)",
			"dimensions", m.embedder.Dimensions(),
			"model", m.embedder.Model())

		// Record provider
		if m.promMetrics != nil {
			m.promMetrics.embeddingProvider.Set(1) // 1 = BM25
		}

	default:
		return errors.WrapInvalid(
			fmt.Errorf("unknown embedding provider: %s", provider),
			"IndexManager", "initializeSemanticSearch",
			"supported providers: http, bm25, disabled")
	}

	// Initialize TTL caches for embeddings and metadata
	ctx := context.Background()

	// Create vector cache with configured retention window
	vectorCache, err := cache.NewTTL[[]float32](
		ctx,
		m.config.Embedding.RetentionWindow,
		5*time.Minute, // cleanup interval
		cache.WithEvictionCallback(func(entityID string, _ []float32) {
			m.logger.Debug("Evicted embedding vector", "entity_id", entityID, "reason", "ttl_expired")
		}),
	)
	if err != nil {
		return errors.WrapTransient(err, "IndexManager", "initializeSemanticSearch", "failed to create vector cache")
	}
	m.vectorCache = vectorCache

	// Create metadata cache with same TTL
	metadataCache, err := cache.NewTTL[*EntityMetadata](
		ctx,
		m.config.Embedding.RetentionWindow,
		5*time.Minute, // cleanup interval
		cache.WithEvictionCallback(func(entityID string, _ *EntityMetadata) {
			m.logger.Debug("Evicted entity metadata", "entity_id", entityID, "reason", "ttl_expired")
		}),
	)
	if err != nil {
		// Clean up vector cache if metadata cache fails
		vectorCache.Close()
		return errors.WrapTransient(err, "IndexManager", "initializeSemanticSearch", "failed to create metadata cache")
	}
	m.metadataCache = metadataCache

	// Initialize embedding storage (persistent KV buckets)
	embeddingIndexBucket, hasIndexBucket := buckets["EMBEDDING_INDEX"]
	embeddingDedupBucket, hasDedupBucket := buckets["EMBEDDING_DEDUP"]

	if hasIndexBucket && hasDedupBucket {
		m.embeddingStorage = embedding.NewEmbeddingStorage(embeddingIndexBucket, embeddingDedupBucket)

		// Initialize embedding worker for async generation
		m.embeddingWorker = embedding.NewEmbeddingWorker(
			m.embeddingStorage,
			m.embedder,
			embeddingIndexBucket,
			m.logger,
		).WithWorkers(m.config.Workers) // Use same worker count as index manager

		m.logger.Info("Embedding storage and worker initialized",
			"workers", m.config.Workers)
	} else {
		m.logger.Warn("Embedding buckets not found - async embeddings disabled",
			"has_index", hasIndexBucket,
			"has_dedup", hasDedupBucket)
	}

	m.logger.Info("Semantic search initialized successfully",
		"provider", m.config.Embedding.Provider,
		"model", m.embedder.Model(),
		"dimensions", m.embedder.Dimensions(),
		"retention_window", m.config.Embedding.RetentionWindow)

	return nil
}

// SearchSemantic performs semantic similarity search using embeddings.
//
// Returns an error if semantic search is not enabled in the configuration.
func (m *Manager) SearchSemantic(
	ctx context.Context,
	query string,
	opts *SemanticSearchOptions,
) (*SearchResults, error) {
	startTime := time.Now()

	// Validate inputs
	if err := m.validateSemanticSearch(query); err != nil {
		return nil, err
	}

	// Generate query embedding
	queryEmbedding, err := m.generateQueryEmbedding(ctx, query)
	if err != nil {
		return nil, err
	}

	// Default options
	if opts == nil {
		opts = &SemanticSearchOptions{
			Threshold: 0.3, // Reasonable default for all-MiniLM-L6-v2
			Limit:     10,
		}
	}

	// Compute similarity scores and filter
	hits := m.computeSimilarityScores(queryEmbedding, opts)

	// Sort and limit results
	hits = m.sortAndLimitHits(hits, opts)

	// Build final results
	results := m.buildSearchResults(hits, startTime)

	return results, nil
}

// validateSemanticSearch checks if semantic search is enabled and query is valid
func (m *Manager) validateSemanticSearch(query string) error {
	if m.embedder == nil {
		return errors.WrapInvalid(
			fmt.Errorf("semantic search not enabled"),
			"IndexManager", "SearchSemantic",
			"configure Embedding.Enabled=true and Embedding.Provider to enable semantic search")
	}

	if query == "" {
		return errors.WrapInvalid(
			fmt.Errorf("query is empty"),
			"IndexManager", "SearchSemantic",
			"query string cannot be empty")
	}

	return nil
}

// generateQueryEmbedding generates the embedding vector for the search query
func (m *Manager) generateQueryEmbedding(ctx context.Context, query string) ([]float32, error) {
	embeddings, err := m.embedder.Generate(ctx, []string{query})
	if err != nil {
		return nil, errors.WrapTransient(err, "IndexManager", "SearchSemantic", "failed to generate query embedding")
	}
	if len(embeddings) == 0 {
		return nil, errors.WrapTransient(
			fmt.Errorf("no embedding generated for query"),
			"IndexManager", "SearchSemantic", "empty embedding response")
	}
	return embeddings[0], nil
}

// computeSimilarityScores computes similarity scores for all vectors and applies filters
func (m *Manager) computeSimilarityScores(queryEmbedding []float32, opts *SemanticSearchOptions) []scoredHit {
	var hits []scoredHit
	for _, entityID := range m.vectorCache.Keys() {
		vec, ok := m.vectorCache.Get(entityID)
		if !ok {
			continue // Entry evicted between Keys() and Get()
		}

		score := embedding.CosineSimilarity(queryEmbedding, vec)

		// Filter out noise - very low similarity scores indicate no meaningful relationship
		const minReasonableScore = 0.3
		if score < minReasonableScore {
			continue
		}

		// Apply user-specified threshold
		if opts.Threshold > 0 && score < opts.Threshold {
			continue
		}

		// Apply type filter if specified
		if len(opts.Types) > 0 {
			if !m.matchesTypeFilter(entityID, opts.Types) {
				continue
			}
		}

		hits = append(hits, scoredHit{entityID: entityID, score: score})
	}
	return hits
}

// matchesTypeFilter checks if entity matches any of the specified types
func (m *Manager) matchesTypeFilter(entityID string, types []string) bool {
	meta, ok := m.metadataCache.Get(entityID)
	if !ok {
		return false
	}
	for _, t := range types {
		if meta.EntityType == t {
			return true
		}
	}
	return false
}

// sortAndLimitHits sorts hits by score descending and applies limit
func (m *Manager) sortAndLimitHits(hits []scoredHit, opts *SemanticSearchOptions) []scoredHit {
	// Sort by score descending
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}

// buildSearchResults constructs the final SearchResults from scored hits
func (m *Manager) buildSearchResults(hits []scoredHit, startTime time.Time) *SearchResults {
	results := &SearchResults{
		Hits:      make([]*SearchHit, 0, len(hits)),
		Total:     len(hits),
		QueryTime: time.Since(startTime),
	}

	for _, hit := range hits {
		meta, ok := m.metadataCache.Get(hit.entityID)
		if !ok {
			continue // Entry evicted between scoring and result building
		}

		// Extract text for snippet
		text := m.extractText(meta.Properties)
		snippet := text
		if len(snippet) > 150 {
			snippet = snippet[:150] + "..."
		}

		// Get location if available from spatial index
		location := m.extractLocation(meta.Properties)

		results.Hits = append(results.Hits, &SearchHit{
			EntityID:   hit.entityID,
			Score:      hit.score,
			Snippet:    snippet,
			Properties: meta.Properties,
			Timestamp:  meta.Updated,
			Location:   location,
		})
	}

	return results
}

// extractLocation extracts location from entity properties if available
func (m *Manager) extractLocation(properties map[string]interface{}) *GeoPoint {
	if !m.config.Indexes.Spatial {
		return nil
	}

	// Try "latitude"/"longitude" first
	if lat, ok := properties["latitude"].(float64); ok {
		if lon, ok := properties["longitude"].(float64); ok {
			return &GeoPoint{Lat: lat, Lon: lon}
		}
	}

	// Try "lat"/"lon" as fallback
	if lat, ok := properties["lat"].(float64); ok {
		if lon, ok := properties["lon"].(float64); ok {
			return &GeoPoint{Lat: lat, Lon: lon}
		}
	}

	return nil
}

// SearchHybrid combines semantic, temporal, and spatial filters.
//
// Returns an error if semantic search is not enabled when SemanticQuery is provided.
func (m *Manager) SearchHybrid(ctx context.Context, query *HybridQuery) (*SearchResults, error) {
	startTime := time.Now()

	if err := m.validateHybridQuery(query); err != nil {
		return nil, err
	}

	// Start with all entities and apply filters
	candidateIDs := m.initializeCandidates()
	candidateIDs = m.applyTemporalFilter(candidateIDs, query.TimeRange)
	candidateIDs = m.applySpatialFilter(candidateIDs, query.GeoBounds)
	candidateIDs = m.applyTypeFilter(candidateIDs, query.Types)

	// Apply semantic search if query provided
	if query.SemanticQuery != "" {
		return m.executeSemanticSearch(ctx, query, candidateIDs, startTime)
	}

	// No semantic query - return all candidates
	return m.buildNonSemanticResults(candidateIDs, startTime), nil
}

// validateHybridQuery validates the hybrid query parameters
func (m *Manager) validateHybridQuery(query *HybridQuery) error {
	if query == nil {
		return errors.WrapInvalid(fmt.Errorf("query is nil"), "IndexManager", "SearchHybrid", "query cannot be nil")
	}

	if query.SemanticQuery != "" && m.embedder == nil {
		return errors.WrapInvalid(
			fmt.Errorf("semantic search not enabled"),
			"IndexManager", "SearchHybrid",
			"configure Embedding.Enabled=true to use semantic queries")
	}

	return nil
}

// initializeCandidates creates initial candidate set from all entities
func (m *Manager) initializeCandidates() map[string]bool {
	candidateIDs := make(map[string]bool)
	for _, entityID := range m.metadataCache.Keys() {
		candidateIDs[entityID] = true
	}
	return candidateIDs
}

// applyTemporalFilter filters candidates by time range
func (m *Manager) applyTemporalFilter(candidates map[string]bool, timeRange *TimeRange) map[string]bool {
	if timeRange == nil {
		return candidates
	}

	filtered := make(map[string]bool)
	for entityID := range candidates {
		meta, ok := m.metadataCache.Get(entityID)
		if ok && !meta.Updated.Before(timeRange.Start) && !meta.Updated.After(timeRange.End) {
			filtered[entityID] = true
		}
	}
	return filtered
}

// applySpatialFilter filters candidates by geographic bounds
func (m *Manager) applySpatialFilter(candidates map[string]bool, bounds *GeoBounds) map[string]bool {
	if bounds == nil {
		return candidates
	}

	filtered := make(map[string]bool)
	for entityID := range candidates {
		meta, ok := m.metadataCache.Get(entityID)
		if !ok {
			continue
		}

		lat, lon, hasLocation := extractLatLon(meta.Properties)
		if hasLocation && isWithinBounds(lat, lon, bounds) {
			filtered[entityID] = true
		}
	}
	return filtered
}

// extractLatLon extracts latitude and longitude from properties
func extractLatLon(properties map[string]interface{}) (float64, float64, bool) {
	if lat, ok := properties["latitude"].(float64); ok {
		if lon, ok := properties["longitude"].(float64); ok {
			return lat, lon, true
		}
	}

	if lat, ok := properties["lat"].(float64); ok {
		if lon, ok := properties["lon"].(float64); ok {
			return lat, lon, true
		}
	}

	return 0, 0, false
}

// isWithinBounds checks if coordinates are within geographic bounds
func isWithinBounds(lat, lon float64, bounds *GeoBounds) bool {
	return lat >= bounds.SouthWest.Lat &&
		lat <= bounds.NorthEast.Lat &&
		lon >= bounds.SouthWest.Lon &&
		lon <= bounds.NorthEast.Lon
}

// applyTypeFilter filters candidates by entity type
func (m *Manager) applyTypeFilter(candidates map[string]bool, types []string) map[string]bool {
	if len(types) == 0 {
		return candidates
	}

	filtered := make(map[string]bool)
	for entityID := range candidates {
		meta, ok := m.metadataCache.Get(entityID)
		if !ok {
			continue
		}
		for _, t := range types {
			if meta.EntityType == t {
				filtered[entityID] = true
				break
			}
		}
	}
	return filtered
}

// executeSemanticSearch performs semantic search on filtered candidates
func (m *Manager) executeSemanticSearch(
	ctx context.Context,
	query *HybridQuery,
	candidates map[string]bool,
	startTime time.Time,
) (*SearchResults, error) {
	queryEmbedding, err := m.generateHybridQueryEmbedding(ctx, query.SemanticQuery)
	if err != nil {
		return nil, err
	}

	hits := m.scoreAndFilterCandidates(candidates, queryEmbedding, query.MinScore)
	hits = m.sortAndLimitHybridHits(hits, query.Limit)

	return m.buildSemanticResults(hits, startTime), nil
}

// generateHybridQueryEmbedding generates embedding for the hybrid query text
func (m *Manager) generateHybridQueryEmbedding(ctx context.Context, queryText string) ([]float32, error) {
	embeddings, err := m.embedder.Generate(ctx, []string{queryText})
	if err != nil {
		return nil, errors.WrapTransient(err, "IndexManager", "SearchHybrid", "failed to generate query embedding")
	}
	if len(embeddings) == 0 {
		return nil, errors.WrapTransient(
			fmt.Errorf("no embedding generated for query"),
			"IndexManager", "SearchHybrid", "empty embedding response")
	}
	return embeddings[0], nil
}

// scoreAndFilterCandidates scores candidates and filters by threshold
func (m *Manager) scoreAndFilterCandidates(
	candidates map[string]bool,
	queryEmbedding []float32,
	minScore float64,
) []scoredHit {
	const minReasonableScore = 0.3
	var hits []scoredHit

	for entityID := range candidates {
		vec, ok := m.vectorCache.Get(entityID)
		if !ok {
			continue
		}

		score := embedding.CosineSimilarity(queryEmbedding, vec)
		if score < minReasonableScore {
			continue
		}
		if minScore > 0 && score < minScore {
			continue
		}

		hits = append(hits, scoredHit{entityID: entityID, score: score})
	}

	return hits
}

// sortAndLimitHybridHits sorts hits by score and applies limit
func (m *Manager) sortAndLimitHybridHits(hits []scoredHit, limit int) []scoredHit {
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})

	if limit == 0 {
		limit = 10
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}

// buildSemanticResults builds search results from scored hits
func (m *Manager) buildSemanticResults(hits []scoredHit, startTime time.Time) *SearchResults {
	results := &SearchResults{
		Hits:      make([]*SearchHit, 0, len(hits)),
		Total:     len(hits),
		QueryTime: time.Since(startTime),
	}

	for _, hit := range hits {
		meta, ok := m.metadataCache.Get(hit.entityID)
		if !ok {
			continue
		}

		text := m.extractText(meta.Properties)
		snippet := text
		if len(snippet) > 150 {
			snippet = snippet[:150] + "..."
		}

		results.Hits = append(results.Hits, &SearchHit{
			EntityID:   hit.entityID,
			Score:      hit.score,
			Snippet:    snippet,
			Properties: meta.Properties,
			Timestamp:  meta.Updated,
			Location:   extractGeoPoint(meta.Properties),
		})
	}

	return results
}

// buildNonSemanticResults builds results for non-semantic queries
func (m *Manager) buildNonSemanticResults(candidates map[string]bool, startTime time.Time) *SearchResults {
	results := &SearchResults{
		Hits:      make([]*SearchHit, 0, len(candidates)),
		Total:     len(candidates),
		QueryTime: time.Since(startTime),
	}

	for entityID := range candidates {
		meta, ok := m.metadataCache.Get(entityID)
		if !ok {
			continue
		}

		results.Hits = append(results.Hits, &SearchHit{
			EntityID:   entityID,
			Score:      1.0,
			Snippet:    "",
			Properties: meta.Properties,
			Timestamp:  meta.Updated,
			Location:   extractGeoPoint(meta.Properties),
		})
	}

	return results
}

// extractGeoPoint extracts GeoPoint from properties
func extractGeoPoint(properties map[string]interface{}) *GeoPoint {
	lat, lon, hasLocation := extractLatLon(properties)
	if hasLocation {
		return &GeoPoint{Lat: lat, Lon: lon}
	}
	return nil
}

// extractText extracts text from entity properties for embedding generation.
//
// Uses the configured text fields to extract relevant text content.
func (m *Manager) extractText(properties map[string]interface{}) string {
	var parts []string

	textFields := m.config.Embedding.TextFields
	if len(textFields) == 0 {
		// Default text fields
		textFields = []string{"title", "content", "description", "summary", "text", "name"}
	}

	for _, field := range textFields {
		if value, ok := properties[field]; ok {
			if str, ok := value.(string); ok && str != "" {
				parts = append(parts, str)
			}
		}
	}

	return strings.Join(parts, " ")
}
