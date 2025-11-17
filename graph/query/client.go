package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
)

// Config contains configuration for the Client
type Config struct {
	// EntityCache configuration
	EntityCache cache.Config `json:"entity_cache"`

	// KV Bucket configurations (should match GraphProcessor config)
	EntityStates struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"entity_states"`

	SpatialIndex struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"spatial_index"`

	IncomingIndex struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"incoming_index"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		EntityCache: cache.Config{
			Strategy:        cache.StrategyHybrid,
			MaxSize:         1000,
			TTL:             5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		},
		EntityStates: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      24 * time.Hour,
			History:  3,
			Replicas: 1,
		},
		SpatialIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      1 * time.Hour,
			History:  1,
			Replicas: 1,
		},
		IncomingIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      24 * time.Hour,
			History:  1,
			Replicas: 1,
		},
	}
}

// natsClient implements the Client interface for reading graph data from NATS KV buckets
type natsClient struct {
	natsClient *natsclient.Client
	cache      cache.Cache[*gtypes.EntityState]
	config     *Config

	// KV bucket handles
	entityBucket   jetstream.KeyValue
	spatialBucket  jetstream.KeyValue
	incomingBucket jetstream.KeyValue

	// Metrics
	queryCount  int64
	cacheHits   int64
	cacheMisses int64

	// Mutex for bucket initialization
	initMu      sync.Mutex
	initialized bool
}

// NewClient creates a new query client with the given NATS client and configuration
func NewClient(ctx context.Context, nc *natsclient.Client, config *Config) (Client, error) {
	return NewClientWithMetrics(ctx, nc, config, nil)
}

// NewClientWithMetrics creates a new query client with the given NATS client, configuration, and optional metrics
func NewClientWithMetrics(
	ctx context.Context,
	nc *natsclient.Client,
	config *Config,
	metricsRegistry *metric.MetricsRegistry,
) (Client, error) {
	if nc == nil {
		return nil, fmt.Errorf("natsClient cannot be nil")
	}
	if config == nil {
		config = DefaultConfig()
	}

	// Create cache with metrics
	entityCache, err := cache.NewFromConfig[*gtypes.EntityState](ctx, config.EntityCache,
		cache.WithMetrics[*gtypes.EntityState](metricsRegistry, "query_client"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create entity cache: %w", err)
	}

	client := &natsClient{
		natsClient: nc,
		cache:      entityCache,
		config:     config,
	}

	return client, nil
}

// ensureBuckets initializes the KV buckets if they haven't been initialized yet
func (qc *natsClient) ensureBuckets(ctx context.Context) error {
	qc.initMu.Lock()
	defer qc.initMu.Unlock()

	if qc.initialized {
		return nil
	}

	// Get or create ENTITY_STATES bucket
	entityConfig := jetstream.KeyValueConfig{
		Bucket:   "ENTITY_STATES",
		TTL:      qc.config.EntityStates.TTL,
		History:  qc.config.EntityStates.History,
		Replicas: qc.config.EntityStates.Replicas,
	}
	entityBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, entityConfig)
	if err != nil {
		return fmt.Errorf("failed to get ENTITY_STATES bucket: %w", err)
	}
	qc.entityBucket = entityBucket

	// Get or create SPATIAL_INDEX bucket
	spatialConfig := jetstream.KeyValueConfig{
		Bucket:   "SPATIAL_INDEX",
		TTL:      qc.config.SpatialIndex.TTL,
		History:  qc.config.SpatialIndex.History,
		Replicas: qc.config.SpatialIndex.Replicas,
	}
	spatialBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, spatialConfig)
	if err != nil {
		return fmt.Errorf("failed to get SPATIAL_INDEX bucket: %w", err)
	}
	qc.spatialBucket = spatialBucket

	// Get or create INCOMING_INDEX bucket
	incomingConfig := jetstream.KeyValueConfig{
		Bucket:   "INCOMING_INDEX",
		TTL:      qc.config.IncomingIndex.TTL,
		History:  qc.config.IncomingIndex.History,
		Replicas: qc.config.IncomingIndex.Replicas,
	}
	incomingBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, incomingConfig)
	if err != nil {
		return fmt.Errorf("failed to get INCOMING_INDEX bucket: %w", err)
	}
	qc.incomingBucket = incomingBucket

	qc.initialized = true
	return nil
}

// GetEntity retrieves a specific entity by ID
func (qc *natsClient) GetEntity(ctx context.Context, entityID string) (*gtypes.EntityState, error) {
	atomic.AddInt64(&qc.queryCount, 1)

	// Check cache first
	if cached, exists := qc.cache.Get(entityID); exists {
		atomic.AddInt64(&qc.cacheHits, 1)
		return cached, nil
	}

	atomic.AddInt64(&qc.cacheMisses, 1)

	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	// Get from bucket
	entry, err := qc.entityBucket.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	// Unmarshal
	var state gtypes.EntityState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity state: %w", err)
	}

	// Update cache
	qc.cache.Set(entityID, &state)

	return &state, nil
}

// GetEntitiesByType returns all entities of a specific type
func (qc *natsClient) GetEntitiesByType(ctx context.Context, entityType string) ([]*gtypes.EntityState, error) {
	entities, err := qc.QueryEntities(ctx, map[string]any{"type": entityType})
	if err != nil {
		return nil, fmt.Errorf("failed to get entities by type: %w", err)
	}
	return entities, nil
}

// GetEntitiesBatch retrieves multiple entities in a single operation
func (qc *natsClient) GetEntitiesBatch(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error) {
	if len(entityIDs) == 0 {
		return []*gtypes.EntityState{}, nil
	}

	entities := make([]*gtypes.EntityState, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, err := qc.GetEntity(ctx, entityID)
		if err != nil {
			// Log but continue with other entities
			log.Printf("Warning: failed to get entity %s: %v", entityID, err)
			continue
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// ListEntities returns all entity IDs in the graph
func (qc *natsClient) ListEntities(ctx context.Context) ([]string, error) {
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	// Get all keys from bucket
	keys, err := qc.entityBucket.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list entity keys: %w", err)
	}

	return keys, nil
}

// CountEntities returns the total number of entities in the graph
func (qc *natsClient) CountEntities(ctx context.Context) (int, error) {
	keys, err := qc.ListEntities(ctx)
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

// GetIncomingEdges retrieves entity IDs that reference the given entity
func (qc *natsClient) GetIncomingEdges(ctx context.Context, entityID string) ([]string, error) {
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	entry, err := qc.incomingBucket.Get(ctx, entityID)
	if err != nil {
		// No incoming edges found
		return []string{}, nil
	}

	var incomingData map[string][]string
	if err := json.Unmarshal(entry.Value(), &incomingData); err != nil {
		return []string{}, fmt.Errorf("failed to unmarshal incoming index: %w", err)
	}

	if incoming, exists := incomingData["incoming"]; exists {
		return incoming, nil
	}

	return []string{}, nil
}

// GetOutgoingEdges returns the outgoing edges for an entity
func (qc *natsClient) GetOutgoingEdges(ctx context.Context, entityID string) ([]gtypes.Edge, error) {
	entity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	return entity.Edges, nil
}

// GetEntityConnections returns all entities connected to the specified entity
func (qc *natsClient) GetEntityConnections(ctx context.Context, entityID string) ([]*gtypes.EntityState, error) {
	// Get the source entity
	sourceEntity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source entity: %w", err)
	}

	var connectedEntities []*gtypes.EntityState

	// Get outgoing connections
	for _, edge := range sourceEntity.Edges {
		if targetEntity, err := qc.GetEntity(ctx, edge.ToEntityID); err == nil {
			connectedEntities = append(connectedEntities, targetEntity)
		}
	}

	// Get incoming connections using INCOMING_INDEX
	incomingEntityIDs, err := qc.GetIncomingEdges(ctx, entityID)
	if err == nil {
		for _, incomingID := range incomingEntityIDs {
			if incomingEntity, err := qc.GetEntity(ctx, incomingID); err == nil {
				connectedEntities = append(connectedEntities, incomingEntity)
			}
		}
	}

	return connectedEntities, nil
}

// VerifyRelationship checks if a relationship exists between two entities
func (qc *natsClient) VerifyRelationship(ctx context.Context, fromID, toID, edgeType string) (bool, error) {
	sourceEntity, err := qc.GetEntity(ctx, fromID)
	if err != nil {
		return false, err
	}

	// Check outgoing edges
	for _, edge := range sourceEntity.Edges {
		if edge.ToEntityID == toID && (edgeType == "" || edge.EdgeType == edgeType) {
			return true, nil
		}
	}

	return false, nil
}

// CountIncomingEdges returns the number of edges pointing to the specified entity
func (qc *natsClient) CountIncomingEdges(ctx context.Context, entityID string) (int, error) {
	incomingIDs, err := qc.GetIncomingEdges(ctx, entityID)
	if err != nil {
		return 0, err
	}
	return len(incomingIDs), nil
}

// QueryEntities searches for entities matching the specified criteria
func (qc *natsClient) QueryEntities(ctx context.Context, criteria map[string]any) ([]*gtypes.EntityState, error) {
	keys, err := qc.ListEntities(ctx)
	if err != nil {
		return nil, err
	}

	var matchingEntities []*gtypes.EntityState

	for _, key := range keys {
		entity, err := qc.GetEntity(ctx, key)
		if err != nil {
			continue // Skip entities that can't be loaded
		}

		if qc.entityMatchesCriteria(entity, criteria) {
			matchingEntities = append(matchingEntities, entity)
		}
	}

	return matchingEntities, nil
}

// GetEntitiesInRegion returns entities in a specific geohash region
func (qc *natsClient) GetEntitiesInRegion(ctx context.Context, geohash string) ([]*gtypes.EntityState, error) {
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	entry, err := qc.spatialBucket.Get(ctx, geohash)
	if err != nil {
		// No entities in this geohash region
		return []*gtypes.EntityState{}, nil
	}

	var spatialData map[string]map[string]any
	if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spatial index: %w", err)
	}

	var entities []*gtypes.EntityState
	if entitiesData, exists := spatialData["entities"]; exists {
		for entityID := range entitiesData {
			if entity, err := qc.GetEntity(ctx, entityID); err == nil {
				entities = append(entities, entity)
			}
		}
	}

	return entities, nil
}

// GetCacheStats returns cache performance statistics
func (qc *natsClient) GetCacheStats() CacheStats {
	hits := atomic.LoadInt64(&qc.cacheHits)
	misses := atomic.LoadInt64(&qc.cacheMisses)
	total := hits + misses

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return CacheStats{
		Hits:        hits,
		Misses:      misses,
		Size:        qc.cache.Size(),
		HitRate:     hitRate,
		LastCleared: time.Time{}, // Not available in current cache implementation
	}
}

// Clear clears the cache
func (qc *natsClient) Clear() error {
	qc.cache.Clear()
	atomic.StoreInt64(&qc.cacheHits, 0)
	atomic.StoreInt64(&qc.cacheMisses, 0)
	atomic.StoreInt64(&qc.queryCount, 0)
	return nil
}

// Close shuts down the Client and releases resources
func (qc *natsClient) Close() error {
	return qc.cache.Close()
}

// ExecutePathQuery performs bounded graph traversal with resource limits
func (qc *natsClient) ExecutePathQuery(ctx context.Context, query PathQuery) (*PathResult, error) {
	// Validate query parameters
	if query.StartEntity == "" {
		return nil, fmt.Errorf("start_entity is required")
	}
	if query.MaxDepth <= 0 {
		return nil, fmt.Errorf("max_depth must be positive")
	}
	if query.MaxNodes <= 0 {
		return nil, fmt.Errorf("max_nodes must be positive")
	}
	if query.DecayFactor < 0.0 || query.DecayFactor > 1.0 {
		return nil, fmt.Errorf("decay_factor must be between 0.0 and 1.0")
	}

	// Validate MaxTime is not negative
	if query.MaxTime < 0 {
		return nil, fmt.Errorf("max_time cannot be negative")
	}

	// Validate MaxPaths is not negative
	if query.MaxPaths < 0 {
		return nil, fmt.Errorf("max_paths cannot be negative")
	}

	// Create context with timeout
	queryCtx := ctx
	if query.MaxTime > 0 {
		var cancel context.CancelFunc
		queryCtx, cancel = context.WithTimeout(ctx, query.MaxTime)
		defer cancel()
	}

	// Initialize traversal state
	state := &pathTraversalState{
		visited:  make(map[string]bool),
		entities: make(map[string]*gtypes.EntityState),
		paths:    make([][]string, 0),
		scores:   make(map[string]float64),
	}

	// Get start entity
	startEntity, err := qc.GetEntity(queryCtx, query.StartEntity)
	if err != nil {
		return nil, fmt.Errorf("failed to get start entity %s: %w", query.StartEntity, err)
	}
	if startEntity == nil {
		return nil, fmt.Errorf("start entity %s not found", query.StartEntity)
	}

	// Initialize with start entity
	state.visited[query.StartEntity] = true
	state.entities[query.StartEntity] = startEntity
	state.scores[query.StartEntity] = 1.0 // Start entity has full relevance
	state.nodesVisited = 1

	// Perform depth-first traversal
	if err := qc.traverseGraph(queryCtx, query, state, query.StartEntity, 0, []string{query.StartEntity}); err != nil {
		return nil, fmt.Errorf("traversal failed: %w", err)
	}

	// Convert state to result
	result := &PathResult{
		Entities:  make([]*gtypes.EntityState, 0, len(state.entities)),
		Paths:     state.paths,
		Scores:    state.scores,
		Truncated: state.truncated,
	}

	// Collect entities in deterministic order
	for _, entity := range state.entities {
		result.Entities = append(result.Entities, entity)
	}

	return result, nil
}

// pathTraversalState tracks state during bounded graph traversal
type pathTraversalState struct {
	visited      map[string]bool
	entities     map[string]*gtypes.EntityState
	paths        [][]string
	scores       map[string]float64
	nodesVisited int
	truncated    bool
}

// traverseGraph performs recursive breadth-first traversal with bounds
func (qc *natsClient) traverseGraph(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	entityID string,
	depth int,
	currentPath []string,
) error {
	// Check context cancellation
	if err := qc.checkTraversalContext(ctx, state); err != nil {
		return err
	}

	// Check depth limit
	if depth >= query.MaxDepth {
		qc.addCompletePath(query, state, currentPath)
		return nil
	}

	// Get current entity
	entity, exists := state.entities[entityID]
	if !exists {
		return fmt.Errorf("entity %s not found in state", entityID)
	}

	// Check if this entity has any valid outgoing edges
	if !qc.hasValidOutgoingEdges(entity, query.EdgeFilter) {
		qc.addCompletePath(query, state, currentPath)
		return nil
	}

	// Traverse valid outgoing edges
	return qc.traverseEdges(ctx, query, state, entity, depth, currentPath)
}

// checkTraversalContext checks for context cancellation
func (qc *natsClient) checkTraversalContext(ctx context.Context, state *pathTraversalState) error {
	select {
	case <-ctx.Done():
		state.truncated = true
		return ctx.Err()
	default:
		return nil
	}
}

// addCompletePath adds a path copy to the state if under MaxPaths limit
func (qc *natsClient) addCompletePath(query PathQuery, state *pathTraversalState, currentPath []string) {
	// Check MaxPaths limit (0 means unlimited)
	if query.MaxPaths > 0 && len(state.paths) >= query.MaxPaths {
		return // Don't add more paths
	}

	pathCopy := make([]string, len(currentPath))
	copy(pathCopy, currentPath)
	state.paths = append(state.paths, pathCopy)
}

// hasValidOutgoingEdges checks if entity has edges matching the filter
func (qc *natsClient) hasValidOutgoingEdges(entity *gtypes.EntityState, edgeFilter []string) bool {
	for _, edge := range entity.Edges {
		if qc.shouldFollowEdge(edge, edgeFilter) {
			return true
		}
	}
	return false
}

// traverseEdges processes all valid outgoing edges from an entity
func (qc *natsClient) traverseEdges(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	entity *gtypes.EntityState,
	depth int,
	currentPath []string,
) error {
	for _, edge := range entity.Edges {
		if !qc.shouldFollowEdge(edge, query.EdgeFilter) {
			continue
		}

		targetID := edge.ToEntityID

		// Process new target entity if not visited
		if !state.visited[targetID] {
			if err := qc.processNewTargetEntity(ctx, query, state, targetID, entity.Node.ID, currentPath); err != nil {
				if err == errNodeLimitReached {
					return nil // Truncated, but not an error
				}
				return err
			}
		}

		// Continue traversal if not cyclic
		if err := qc.continueTraversalIfValid(ctx, query, state, targetID, depth, currentPath); err != nil {
			return err
		}
	}

	return nil
}

// errNodeLimitReached is returned when node limit is hit
var errNodeLimitReached = fmt.Errorf("node limit reached")

// processNewTargetEntity loads and processes a new target entity
func (qc *natsClient) processNewTargetEntity(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	targetID, sourceID string,
	currentPath []string,
) error {
	// Check node limit
	if state.nodesVisited >= query.MaxNodes {
		state.truncated = true
		qc.addCompletePath(query, state, currentPath)
		return errNodeLimitReached
	}

	// Load target entity
	targetEntity, err := qc.GetEntity(ctx, targetID)
	if err != nil || targetEntity == nil {
		// Skip entities that can't be loaded
		return nil
	}

	// Add to state
	state.visited[targetID] = true
	state.entities[targetID] = targetEntity
	state.nodesVisited++

	// Calculate score with decay
	decayedScore := state.scores[sourceID] * query.DecayFactor
	state.scores[targetID] = decayedScore

	return nil
}

// continueTraversalIfValid continues traversal if target is not in current path
func (qc *natsClient) continueTraversalIfValid(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	targetID string,
	depth int,
	currentPath []string,
) error {
	// Avoid cycles by checking current path
	if !qc.containsString(currentPath, targetID) {
		nextPath := append(currentPath, targetID)
		return qc.traverseGraph(ctx, query, state, targetID, depth+1, nextPath)
	}
	return nil
}

// shouldFollowEdge determines if an edge should be followed based on edge filter
func (qc *natsClient) shouldFollowEdge(edge gtypes.Edge, edgeFilter []string) bool {
	// If no filter specified, follow all edges
	if len(edgeFilter) == 0 {
		return true
	}

	// Check if edge type is in filter
	for _, allowedType := range edgeFilter {
		if edge.EdgeType == allowedType {
			return true
		}
	}

	return false
}

// containsString checks if a string slice contains a specific value
func (qc *natsClient) containsString(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// entityMatchesCriteria checks if an entity matches the given criteria
func (qc *natsClient) entityMatchesCriteria(entity *gtypes.EntityState, criteria map[string]any) bool {
	if entity == nil {
		return false
	}

	// Check type criteria
	if entityType, hasType := criteria["type"].(string); hasType {
		if entity.Node.Type != entityType {
			return false
		}
	}

	// Check status criteria
	if status, hasStatus := criteria["status"].(string); hasStatus {
		if entity.Node.Status.String() != status {
			return false
		}
	}

	// Check property criteria
	for key, expectedValue := range criteria {
		if key == "type" || key == "status" {
			continue // Already checked above
		}

		if entity.Node.Properties == nil {
			return false
		}

		actualValue, exists := entity.Node.Properties[key]
		if !exists || actualValue != expectedValue {
			return false
		}
	}

	return true
}
