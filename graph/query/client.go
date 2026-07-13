package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
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
			// TTL MUST be 0 on the live graph: NATS age-eviction is
			// reachability-blind and would silently expire entities with live
			// inbound edges (ADR-068 D1). graph-ingest owns ENTITY_STATES
			// retention; the query client is a reader and must not race in a TTL.
			TTL:      0,
			History:  3,
			Replicas: 1,
		},
		SpatialIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      0, // no lifecycle retention on the live graph (ADR-068 D1)
			History:  1,
			Replicas: 1,
		},
		IncomingIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      0, // no lifecycle retention on the live graph (ADR-068 D1)
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

// indexNotReadyErr gates a direct INCOMING_INDEX read on graph-index readiness (gh#474
// Codex #6). It returns a classified ErrorCodeIndexNotReady only when graph-index is up
// and EXPLICITLY reports not-ready (still building / degraded by an unresolved write
// failure) — the cutover/failure window where a direct bucket read would return partial
// topology. It FAILS OPEN (returns nil) when the status endpoint is unreachable or
// unparseable: that means graph-index is absent, a different failure mode, and blocking
// the read on it would break setups that read the index without the handler co-deployed.
func (qc *natsClient) indexNotReadyErr(ctx context.Context) error {
	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.status", []byte("{}"), 5*time.Second)
	if err != nil {
		return nil // fail-open: can't determine readiness (graph-index absent)
	}
	var st gtypes.IndexStatusResponse
	if json.Unmarshal(respData, &st) != nil {
		return nil
	}
	if !st.Ready {
		return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady,
			errors.New("graph index not ready"))
	}
	return nil
}

// GetIncomingEdges retrieves distinct source entity IDs that reference the given
// entity (one per unique source, regardless of how many predicates connect the pair).
//
// Gates on graph-index readiness first (gh#474 Codex #6): reading the INCOMING_INDEX
// bucket directly bypasses graph-index's cutover-readiness gate, so during an in-place
// format upgrade this consumer (and its callers — CountIncomingEdges, the anomaly
// detector) could serve partial topology. When graph-index reports not-ready this
// returns ErrorCodeIndexNotReady instead of a partial result.
func (qc *natsClient) GetIncomingEdges(ctx context.Context, entityID string) ([]string, error) {
	if err := qc.indexNotReadyErr(ctx); err != nil {
		return nil, err
	}
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	// FilteredKeys handles ErrNoKeysFound → nil, nil (no error on empty).
	keys, err := natsclient.FilteredKeys(ctx, qc.incomingBucket, entityID+".>")
	if err != nil {
		return nil, fmt.Errorf("failed to list incoming edges for %s: %w", entityID, err)
	}

	// Parse the sourceID (first 6 tokens) from each composite key suffix.
	prefix := entityID + "."
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suffix := key[len(prefix):]
		// suffix = "sourceID.hex(predicate)"; sourceID is exactly 6 dot-separated tokens.
		parts := strings.SplitN(suffix, ".", 7)
		if len(parts) < 7 {
			continue
		}
		sourceID := strings.Join(parts[:6], ".")
		if _, dup := seen[sourceID]; !dup {
			seen[sourceID] = struct{}{}
			result = append(result, sourceID)
		}
	}

	// Deterministic order (gh#474 P1c): a no-op replay can reshuffle storage order.
	sort.Strings(result)
	return result, nil
}

// GetIncomingRelationships retrieves entity IDs that reference the given entity
func (qc *natsClient) GetIncomingRelationships(ctx context.Context, entityID string) ([]string, error) {
	return qc.GetIncomingEdges(ctx, entityID)
}

// GetOutgoingRelationships returns the entity IDs that this entity points to via relationship triples
func (qc *natsClient) GetOutgoingRelationships(ctx context.Context, entityID string, predicate string) ([]string, error) {
	entity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	var result []string
	for _, triple := range entity.Triples {
		// Filter by predicate if specified
		if predicate != "" && triple.Predicate != predicate {
			continue
		}
		// Check if this is a relationship triple (object is a valid 6-part EntityID)
		if triple.IsRelationship() {
			if targetID, ok := triple.Object.(string); ok {
				result = append(result, targetID)
			}
		}
	}
	return result, nil
}

// CountIncomingRelationships returns the number of entities pointing to this entity
func (qc *natsClient) CountIncomingRelationships(ctx context.Context, entityID string) (int, error) {
	return qc.CountIncomingEdges(ctx, entityID)
}

// GetEntityConnections returns all entities connected to the specified entity
func (qc *natsClient) GetEntityConnections(ctx context.Context, entityID string) ([]*gtypes.EntityState, error) {
	// Get the source entity
	sourceEntity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source entity: %w", err)
	}

	var connectedEntities []*gtypes.EntityState
	seenIDs := make(map[string]bool)

	// Get outgoing connections via relationship triples (object is a valid 6-part EntityID)
	for _, triple := range sourceEntity.Triples {
		if triple.IsRelationship() {
			if targetID, ok := triple.Object.(string); ok && !seenIDs[targetID] {
				if targetEntity, err := qc.GetEntity(ctx, targetID); err == nil {
					connectedEntities = append(connectedEntities, targetEntity)
					seenIDs[targetID] = true
				}
			}
		}
	}

	// Get incoming connections using INCOMING_INDEX
	incomingEntityIDs, err := qc.GetIncomingEdges(ctx, entityID)
	if err == nil {
		for _, incomingID := range incomingEntityIDs {
			if !seenIDs[incomingID] {
				if incomingEntity, err := qc.GetEntity(ctx, incomingID); err == nil {
					connectedEntities = append(connectedEntities, incomingEntity)
					seenIDs[incomingID] = true
				}
			}
		}
	}

	return connectedEntities, nil
}

// VerifyRelationship checks if a relationship exists between two entities
func (qc *natsClient) VerifyRelationship(ctx context.Context, fromID, toID, predicate string) (bool, error) {
	sourceEntity, err := qc.GetEntity(ctx, fromID)
	if err != nil {
		return false, err
	}

	// Check relationship triples
	for _, triple := range sourceEntity.Triples {
		if targetID, ok := triple.Object.(string); ok && targetID == toID {
			if predicate == "" || triple.Predicate == predicate {
				return true, nil
			}
		}
	}

	return false, nil
}

// CountIncomingEdges returns the number of distinct source entities pointing to the
// specified entity (one per unique source, regardless of how many predicates connect
// the pair) — it counts GetIncomingEdges' deduplicated sources, not raw edges.
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

	// Check if this entity has any valid outgoing relationship triples
	if !qc.hasValidOutgoingRelationships(entity, query.PredicateFilter) {
		qc.addCompletePath(query, state, currentPath)
		return nil
	}

	// Traverse valid outgoing relationship triples
	return qc.traverseRelationships(ctx, query, state, entity, depth, currentPath)
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

// hasValidOutgoingRelationships checks if entity has relationship triples matching the filter
func (qc *natsClient) hasValidOutgoingRelationships(entity *gtypes.EntityState, predicateFilter []string) bool {
	for _, triple := range entity.Triples {
		if qc.shouldFollowTriple(triple, predicateFilter) {
			return true
		}
	}
	return false
}

// traverseRelationships processes all valid outgoing relationship triples from an entity
func (qc *natsClient) traverseRelationships(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	entity *gtypes.EntityState,
	depth int,
	currentPath []string,
) error {
	for _, triple := range entity.Triples {
		if !qc.shouldFollowTriple(triple, query.PredicateFilter) {
			continue
		}

		// Get target ID from triple object (must be a string entity ID)
		targetID, ok := triple.Object.(string)
		if !ok {
			continue // Not a relationship triple
		}

		// Process new target entity if not visited
		if !state.visited[targetID] {
			if err := qc.processNewTargetEntity(ctx, query, state, targetID, entity.ID, currentPath); err != nil {
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

// shouldFollowTriple determines if a triple should be followed based on predicate filter
func (qc *natsClient) shouldFollowTriple(triple message.Triple, predicateFilter []string) bool {
	// Only follow relationship triples (object is a valid 6-part EntityID)
	if !triple.IsRelationship() {
		return false
	}

	// If no filter specified, follow all relationship predicates
	if len(predicateFilter) == 0 {
		return true
	}

	// Check if predicate is in filter
	for _, allowedPredicate := range predicateFilter {
		if triple.Predicate == allowedPredicate {
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

	// Check type criteria - type is now extracted from ID
	if entityType, hasType := criteria["type"].(string); hasType {
		eid, err := message.ParseEntityID(entity.ID)
		if err != nil || eid.Type != entityType {
			return false
		}
	}

	// Check status criteria - status is now domain-specific via triples
	if status, hasStatus := criteria["status"].(string); hasStatus {
		// Look for status in triples
		statusVal, found := entity.GetPropertyValue("status")
		if !found {
			return false
		}
		if statusStr, ok := statusVal.(string); !ok || statusStr != status {
			return false
		}
	}

	// Check property criteria using triples
	for key, expectedValue := range criteria {
		if key == "type" || key == "status" {
			continue // Already checked above
		}

		// Look up property value from triples
		actualValue, found := entity.GetPropertyValue(key)
		if !found || actualValue != expectedValue {
			return false
		}
	}

	return true
}

// GetEntitiesByPredicate returns entity IDs that have a specific predicate.
func (qc *natsClient) GetEntitiesByPredicate(ctx context.Context, predicate string) ([]string, error) {
	if predicate == "" {
		return nil, fmt.Errorf("predicate cannot be empty")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	req := struct {
		Predicate string `json:"predicate"`
	}{Predicate: predicate}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicate", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicate: %w", err)
	}

	var resp gtypes.PredicateQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Entities, nil
}

// ListPredicates returns all predicates with their entity counts.
func (qc *natsClient) ListPredicates(ctx context.Context) ([]gtypes.PredicateSummary, error) {
	atomic.AddInt64(&qc.queryCount, 1)

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateList", []byte("{}"), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicates: %w", err)
	}

	var resp gtypes.PredicateListQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Predicates, nil
}

// GetPredicateStats returns detailed statistics for a predicate.
func (qc *natsClient) GetPredicateStats(ctx context.Context, predicate string, sampleLimit int) (*gtypes.PredicateStatsData, error) {
	if predicate == "" {
		return nil, fmt.Errorf("predicate cannot be empty")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	req := struct {
		Predicate   string `json:"predicate"`
		SampleLimit int    `json:"sample_limit"`
	}{Predicate: predicate, SampleLimit: sampleLimit}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateStats", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicate stats: %w", err)
	}

	var resp gtypes.PredicateStatsQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp.Data, nil
}

// QueryCompoundPredicates performs a compound predicate query with AND/OR logic.
func (qc *natsClient) QueryCompoundPredicates(ctx context.Context, query gtypes.CompoundPredicateQuery) ([]string, error) {
	if len(query.Predicates) == 0 {
		return nil, fmt.Errorf("predicates cannot be empty")
	}
	if query.Operator != "AND" && query.Operator != "OR" {
		return nil, fmt.Errorf("operator must be AND or OR")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	reqData, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateCompound", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query compound predicates: %w", err)
	}

	var resp gtypes.CompoundPredicateQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Entities, nil
}
