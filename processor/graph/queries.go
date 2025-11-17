package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/processor/graph/indexmanager"
	"github.com/c360/semstreams/processor/graph/querymanager"
)

// Query subject patterns for NATS request/reply
const (
	// Entity queries
	SubjectQueryEntity   = "graph.query.entity"   // Single entity lookup
	SubjectQueryEntities = "graph.query.entities" // Batch entity lookup
	SubjectQueryByAlias  = "graph.query.alias"    // Alias resolution

	// Graph traversal
	SubjectQueryPath          = "graph.query.path"          // Path traversal
	SubjectQueryRelationships = "graph.query.relationships" // Entity relationships

	// Index queries
	SubjectQuerySpatial     = "graph.query.spatial"   // Geospatial queries
	SubjectQueryTemporal    = "graph.query.temporal"  // Time-based queries
	SubjectQueryByPredicate = "graph.query.predicate" // Property-based queries
	SubjectQuerySemantic    = "graph.query.semantic"  // Semantic similarity search

	// Default timeout for query operations
	DefaultQueryTimeout = 5 * time.Second
)

// Request types for NATS queries

// EntityQueryRequest requests a single entity by ID
type EntityQueryRequest struct {
	EntityID string `json:"entity_id"`
}

// EntitiesQueryRequest requests multiple entities by IDs
type EntitiesQueryRequest struct {
	EntityIDs []string `json:"entity_ids"`
}

// AliasQueryRequest resolves an alias to entity ID
type AliasQueryRequest struct {
	Alias string `json:"alias"`
}

// PathQueryRequest requests a path traversal
type PathQueryRequest struct {
	StartEntity string                   `json:"start_entity"`
	Pattern     querymanager.PathPattern `json:"pattern"`
}

// RelationshipsQueryRequest requests entity relationships
type RelationshipsQueryRequest struct {
	EntityID  string                 `json:"entity_id"`
	Direction querymanager.Direction `json:"direction"`
}

// SpatialQueryRequest requests entities within spatial bounds
type SpatialQueryRequest struct {
	Bounds querymanager.SpatialBounds `json:"bounds"`
}

// TemporalQueryRequest requests entities within time range
type TemporalQueryRequest struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// PredicateQueryRequest requests entities by predicate
type PredicateQueryRequest struct {
	Predicate string `json:"predicate"`
}

// SemanticQueryRequest requests entities by semantic similarity
type SemanticQueryRequest struct {
	Query     string   `json:"query"`
	Types     []string `json:"types,omitempty"`
	Threshold float64  `json:"threshold,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// Response types for NATS queries

// QueryResponse is a generic response wrapper
type QueryResponse struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

// setupQueryHandlers subscribes to all query subjects
func (p *Processor) setupQueryHandlers(ctx context.Context) error {
	// Check for cancellation before setup
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if p.natsClient == nil {
		return errors.WrapFatal(nil, "GraphProcessor", "setupQueryHandlers", "NATS client not initialized")
	}

	// Get raw NATS connection for request/reply pattern
	nc := p.natsClient.GetConnection()
	if nc == nil {
		return errors.WrapFatal(nil, "GraphProcessor", "setupQueryHandlers", "NATS connection not available")
	}

	// Entity queries
	entityHandlers := map[string]nats.MsgHandler{
		SubjectQueryEntity:   p.handleQueryEntity,
		SubjectQueryEntities: p.handleQueryEntities,
		SubjectQueryByAlias:  p.handleQueryByAlias,
	}

	// Graph traversal queries
	graphHandlers := map[string]nats.MsgHandler{
		SubjectQueryPath:          p.handleQueryPath,
		SubjectQueryRelationships: p.handleQueryRelationships,
	}

	// Index queries
	indexHandlers := map[string]nats.MsgHandler{
		SubjectQuerySpatial:     p.handleQuerySpatial,
		SubjectQueryTemporal:    p.handleQueryTemporal,
		SubjectQueryByPredicate: p.handleQueryByPredicate,
		SubjectQuerySemantic:    p.handleQuerySemantic,
	}

	// Subscribe to all handlers
	allHandlers := make(map[string]nats.MsgHandler)
	for k, v := range entityHandlers {
		allHandlers[k] = v
	}
	for k, v := range graphHandlers {
		allHandlers[k] = v
	}
	for k, v := range indexHandlers {
		allHandlers[k] = v
	}

	for subject, handler := range allHandlers {
		_, err := nc.Subscribe(subject, handler)
		if err != nil {
			return errors.WrapFatal(err, "GraphProcessor", "setupQueryHandlers",
				fmt.Sprintf("failed to subscribe to %s", subject))
		}
		p.logger.Debug("Subscribed to query subject", "subject", subject)
	}

	p.logger.Info("Query handlers initialized", "subjects", len(allHandlers))
	return nil
}

// Entity query handlers

func (p *Processor) handleQueryEntity(msg *nats.Msg) {
	// Rate limiting check
	if !p.queryLimiter.Allow() {
		err := errors.WrapTransient(nil, "GraphProcessor", "handleQueryEntity",
			"rate limit exceeded")
		p.respondQueryWithError(msg, err)
		return
	}

	var req EntityQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondQueryWithError(msg, errors.WrapInvalid(err, "GraphProcessor", "handleQueryEntity", "invalid request"))
		return
	}

	entity, err := p.queryManager.GetEntity(context.Background(), req.EntityID)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entity)
}

func (p *Processor) handleQueryEntities(msg *nats.Msg) {
	// Rate limiting check
	if !p.queryLimiter.Allow() {
		err := errors.WrapTransient(nil, "GraphProcessor", "handleQueryEntities",
			"rate limit exceeded")
		p.respondQueryWithError(msg, err)
		return
	}

	var req EntitiesQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		err = errors.WrapInvalid(err, "GraphProcessor", "handleQueryEntities",
			"invalid request")
		p.respondQueryWithError(msg, err)
		return
	}

	entities, err := p.queryManager.GetEntities(context.Background(), req.EntityIDs)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entities)
}

func (p *Processor) handleQueryByAlias(msg *nats.Msg) {
	var req AliasQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondQueryWithError(msg, errors.WrapInvalid(err, "GraphProcessor", "handleQueryByAlias", "invalid request"))
		return
	}

	entity, err := p.queryManager.GetEntityByAlias(context.Background(), req.Alias)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entity)
}

// Graph traversal query handlers

func (p *Processor) handleQueryPath(msg *nats.Msg) {
	// Rate limiting check (more strict for expensive operations)
	if !p.queryLimiter.Allow() {
		err := errors.WrapTransient(nil, "GraphProcessor", "handleQueryPath",
			"rate limit exceeded")
		p.respondQueryWithError(msg, err)
		return
	}

	var req PathQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondQueryWithError(msg, errors.WrapInvalid(err, "GraphProcessor", "handleQueryPath", "invalid request"))
		return
	}

	// Create context with timeout if specified in pattern
	ctx := context.Background()
	if req.Pattern.MaxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Pattern.MaxTime)
		defer cancel()
	}

	result, err := p.queryManager.ExecutePath(ctx, req.StartEntity, req.Pattern)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, result)
}

func (p *Processor) handleQueryRelationships(msg *nats.Msg) {
	var req RelationshipsQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		err = errors.WrapInvalid(err, "GraphProcessor", "handleQueryRelationships",
			"invalid request")
		p.respondQueryWithError(msg, err)
		return
	}

	relationships, err := p.queryManager.QueryRelationships(context.Background(), req.EntityID, req.Direction)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, relationships)
}

// Index query handlers

func (p *Processor) handleQuerySpatial(msg *nats.Msg) {
	var req SpatialQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		p.respondQueryWithError(msg, errors.WrapInvalid(err, "GraphProcessor", "handleQuerySpatial", "invalid request"))
		return
	}

	entities, err := p.queryManager.QuerySpatial(context.Background(), req.Bounds)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entities)
}

func (p *Processor) handleQueryTemporal(msg *nats.Msg) {
	var req TemporalQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		err = errors.WrapInvalid(err, "GraphProcessor", "handleQueryTemporal",
			"invalid request")
		p.respondQueryWithError(msg, err)
		return
	}

	entities, err := p.queryManager.QueryTemporal(context.Background(), req.Start, req.End)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entities)
}

func (p *Processor) handleQueryByPredicate(msg *nats.Msg) {
	var req PredicateQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		err = errors.WrapInvalid(err, "GraphProcessor", "handleQueryByPredicate",
			"invalid request")
		p.respondQueryWithError(msg, err)
		return
	}

	entities, err := p.queryManager.QueryByPredicate(context.Background(), req.Predicate)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, entities)
}

func (p *Processor) handleQuerySemantic(msg *nats.Msg) {
	// Rate limiting check (semantic search can be expensive)
	if !p.queryLimiter.Allow() {
		err := errors.WrapTransient(nil, "GraphProcessor", "handleQuerySemantic",
			"rate limit exceeded")
		p.respondQueryWithError(msg, err)
		return
	}

	var req SemanticQueryRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		err = errors.WrapInvalid(err, "GraphProcessor", "handleQuerySemantic",
			"invalid request")
		p.respondQueryWithError(msg, err)
		return
	}

	// Build semantic search options from request
	opts := &indexmanager.SemanticSearchOptions{
		Threshold: req.Threshold,
		Limit:     req.Limit,
		Types:     req.Types,
	}

	// Set defaults if not specified
	if opts.Threshold == 0 {
		opts.Threshold = 0.3 // Reasonable default for similarity threshold
	}
	if opts.Limit == 0 {
		opts.Limit = 10 // Default result limit
	}

	// Execute semantic search via IndexManager
	results, err := p.indexManager.SearchSemantic(context.Background(), req.Query, opts)
	if err != nil {
		p.respondQueryWithError(msg, err)
		return
	}

	p.respondQueryWithData(msg, results)
}

// Helper methods for query responses

func (p *Processor) respondQueryWithData(msg *nats.Msg, data interface{}) {
	resp := QueryResponse{
		Data: data,
	}
	respData, err := json.Marshal(resp)
	if err != nil {
		p.logger.Error("Failed to marshal query response", "error", err)
		p.respondQueryWithError(msg, errors.WrapFatal(err, "GraphProcessor", "respondQueryWithData", "marshal failed"))
		return
	}
	if err := msg.Respond(respData); err != nil {
		p.logger.Error("Failed to send query response", "error", err)
	}
}

func (p *Processor) respondQueryWithError(msg *nats.Msg, err error) {
	resp := QueryResponse{
		Error: err.Error(),
	}
	respData, _ := json.Marshal(resp)
	if respErr := msg.Respond(respData); respErr != nil {
		p.logger.Error("Failed to send error response", "error", respErr, "original_error", err)
	}
}
