// Package querymanager provides the QueryManager for high-performance read operations.
package querymanager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/processor/graph/datamanager"
	"github.com/c360/semstreams/processor/graph/indexmanager"
)

// Ensure Manager implements the Querier interface
var _ Querier = (*Manager)(nil)

// Manager implements the Querier interface as a stateless orchestrator
// that coordinates between EntityStore and IndexManager
type Manager struct {
	// Configuration
	config Config

	// Dependencies - pure orchestration, no caching
	dataHandler       datamanager.DataHandler // Handles entity operations and caching
	indexManager      indexmanager.Indexer    // Will handle query result caching
	communityDetector any                     // Optional: CommunityDetector for GraphRAG search (type-erased to avoid import cycle)

	// Metrics (simplified - no cache metrics)
	metrics *Metrics

	// Metrics tracking
	mu           sync.RWMutex
	lastActivity time.Time
	errorCount   int64
	lastError    string

	// Logger
	logger *slog.Logger
}

// Deps holds runtime dependencies for query manager component
type Deps struct {
	Config            Config                  // Business logic configuration
	DataHandler       datamanager.DataHandler // Runtime dependency for entity access
	IndexManager      indexmanager.Indexer    // Runtime dependency
	CommunityDetector any                     // Optional: CommunityDetector for GraphRAG search (type-erased to avoid import cycle)
	Registry          *metric.MetricsRegistry // Runtime dependency
	Logger            *slog.Logger            // Runtime dependency
}

// NewManager creates a new Querier instance using idiomatic Go constructor pattern
func NewManager(deps Deps) (Querier, error) {
	// Set defaults first
	deps.Config.SetDefaults()

	// Then validate configuration
	if err := deps.Config.Validate(); err != nil {
		return nil, errors.WrapInvalid(err, "QueryManager", "NewManager",
			"configuration validation failed")
	}

	// Ensure we have a logger
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		config:            deps.Config,
		dataHandler:       deps.DataHandler,
		indexManager:      deps.IndexManager,
		communityDetector: deps.CommunityDetector, // Optional dependency
		lastActivity:      time.Now(),
		logger:            logger,
	}

	// Initialize metrics if registry provided
	if deps.Registry != nil {
		m.metrics = NewMetrics(deps.Registry, "query_engine")
	}

	return m, nil
}

// GetEntity retrieves a single entity through EntityStore (benefits from its cache)
func (m *Manager) GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error) {
	start := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	// Apply timeout
	if m.config.Timeouts.EntityGet > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.Timeouts.EntityGet)
		defer cancel()
	}

	// Fetch from DataHandler (which has its own L1/L2 cache)
	entity, err := m.dataHandler.GetEntity(ctx, id)
	if err != nil {
		m.recordError("GetEntity", err)

		// Record metrics for failure
		if m.metrics != nil {
			m.metrics.RecordEntityGet("query_engine", time.Since(start), "datahandler_error", false)
		}

		// Wrap error appropriately
		if IsEntityNotFound(err) {
			return nil, errors.WrapInvalid(gtypes.ErrEntityNotFound, "QueryManager", "GetEntity",
				fmt.Sprintf("entity ID: %s", id))
		}
		return nil, errors.WrapTransient(err, "QueryManager", "GetEntity",
			fmt.Sprintf("KV operation failed for ID: %s", id))
	}

	// Record metrics for success
	if m.metrics != nil {
		m.metrics.RecordEntityGet("query_engine", time.Since(start), "datahandler", true)
	}

	return entity, nil
}

// GetEntities retrieves multiple entities efficiently through EntityStore
func (m *Manager) GetEntities(ctx context.Context, ids []string) ([]*gtypes.EntityState, error) {
	start := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	// Apply timeout
	if m.config.Timeouts.EntityBatch > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.Timeouts.EntityBatch)
		defer cancel()
	}

	// Fetch all entities from DataHandler (which has its own cache)
	results, err := m.dataHandler.BatchGet(ctx, ids)
	if err != nil {
		m.recordError("GetEntities", err)
		return nil, err
	}

	// Record metrics
	if m.metrics != nil {
		sizeRange := m.getSizeRange(len(ids))
		m.metrics.entityBatchTotal.WithLabelValues("query_engine", "success").Inc()
		m.metrics.entityBatchDuration.WithLabelValues("query_engine", sizeRange).Observe(time.Since(start).Seconds())
	}

	return results, nil
}

// GetEntityByAlias retrieves an entity by alias or ID (extracted from graph processor)
func (m *Manager) GetEntityByAlias(ctx context.Context, aliasOrID string) (*gtypes.EntityState, error) {
	start := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	// First, try to resolve the alias using index manager
	var entityID string
	if m.indexManager != nil {
		if resolvedID, err := m.indexManager.ResolveAlias(ctx, aliasOrID); err == nil {
			entityID = resolvedID
		} else {
			// If alias resolution fails, assume it's already an entity ID
			entityID = aliasOrID
		}
	} else {
		// No index manager available, assume it's an entity ID
		entityID = aliasOrID
	}

	// Get entity using the resolved ID
	entity, err := m.GetEntity(ctx, entityID)
	if err != nil {
		// If entity not found and we tried alias resolution, return alias not found error
		if IsEntityNotFound(err) && entityID != aliasOrID {
			return nil, errors.WrapInvalid(gtypes.ErrAliasNotFound, "QueryManager", "GetEntityByAlias",
				fmt.Sprintf("alias: %s", aliasOrID))
		}
		return nil, err
	}

	// Record metrics for alias resolution
	if m.metrics != nil {
		m.metrics.RecordQuery("query_engine", "alias_resolution", time.Since(start), 1, true)
	}

	return entity, nil
}

// Query operations that delegate to index manager

// QueryByPredicate queries entities by predicate (delegated to index manager)
func (m *Manager) QueryByPredicate(ctx context.Context, predicate string) ([]string, error) {
	start := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	if m.indexManager == nil {
		return nil, errors.WrapTransient(ErrIndexManagerUnavailable, "QueryManager", "QueryByPredicate",
			"index manager dependency unavailable")
	}

	// Direct delegation to index manager (will handle its own caching in Step 2)
	entityIDs, err := m.indexManager.GetPredicateIndex(ctx, predicate)
	if err != nil {
		m.recordError("QueryByPredicate", err)
		if m.metrics != nil {
			m.metrics.RecordQuery("query_engine", "predicate", time.Since(start), 0, false)
		}
		return nil, errors.WrapTransient(err, "QueryManager", "QueryByPredicate",
			fmt.Sprintf("index manager operation failed for predicate: %s", predicate))
	}

	// Record metrics
	if m.metrics != nil {
		m.metrics.RecordQuery("query_engine", "predicate", time.Since(start), len(entityIDs), false)
	}

	return entityIDs, nil
}

// QuerySpatial queries entities by spatial bounds (delegated to index manager)
func (m *Manager) QuerySpatial(ctx context.Context, bounds SpatialBounds) ([]string, error) {
	start := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	if m.indexManager == nil {
		return nil, errors.WrapTransient(ErrIndexManagerUnavailable, "QueryManager", "QuerySpatial",
			"index manager dependency unavailable")
	}

	// Convert bounds
	indexBounds := indexmanager.Bounds{
		North: bounds.North,
		South: bounds.South,
		East:  bounds.East,
		West:  bounds.West,
	}

	// Delegate to index manager
	entityIDs, err := m.indexManager.QuerySpatial(ctx, indexBounds)
	if err != nil {
		m.recordError("QuerySpatial", err)
		if m.metrics != nil {
			m.metrics.RecordQuery("query_engine", "spatial", time.Since(start), 0, false)
		}
		return nil, errors.WrapTransient(err, "QueryManager", "QuerySpatial",
			"index manager spatial query failed")
	}

	// Record metrics
	if m.metrics != nil {
		m.metrics.RecordQuery("query_engine", "spatial", time.Since(start), len(entityIDs), true)
	}

	return entityIDs, nil
}

// QueryTemporal queries entities by temporal bounds (delegated to index manager)
func (m *Manager) QueryTemporal(ctx context.Context, start, end time.Time) ([]string, error) {
	startTime := time.Now()
	defer func() {
		m.lastActivity = time.Now()
	}()

	if m.indexManager == nil {
		return nil, errors.WrapTransient(ErrIndexManagerUnavailable, "QueryManager", "QueryTemporal",
			"index manager dependency unavailable")
	}

	// Delegate to index manager
	entityIDs, err := m.indexManager.QueryTemporal(ctx, start, end)
	if err != nil {
		m.recordError("QueryTemporal", err)
		if m.metrics != nil {
			m.metrics.RecordQuery("query_engine", "temporal", time.Since(startTime), 0, false)
		}
		return nil, errors.WrapTransient(err, "QueryManager", "QueryTemporal",
			fmt.Sprintf("index manager temporal query failed: %v to %v", start, end))
	}

	// Record metrics
	if m.metrics != nil {
		m.metrics.RecordQuery("query_engine", "temporal", time.Since(startTime), len(entityIDs), true)
	}

	return entityIDs, nil
}

// Complex query operations implemented in query.go

// Cache management operations

// InvalidateEntity - no-op since query manager is now stateless (index manager will handle its own cache invalidation)
func (m *Manager) InvalidateEntity(_ string) error {
	// No cache to invalidate at query manager level
	return nil
}

// WarmCache pre-loads entities into DataHandler's cache
func (m *Manager) WarmCache(ctx context.Context, entityIDs []string) error {
	// Simply fetch entities, which will warm DataHandler's cache
	_, err := m.GetEntities(ctx, entityIDs)
	if err != nil {
		return errors.WrapTransient(err, "QueryManager", "WarmCache",
			"batch cache warming failed")
	}

	// Entity caching is now handled by DataHandler
	// Query result caching happens automatically when queries are executed
	return nil
}

// Health and metrics

// GetCacheStats returns empty stats since query manager is now stateless
func (m *Manager) GetCacheStats() CacheStats {
	// No cache at query manager level - DataHandler and index manager manage their own caches
	return CacheStats{}
}

// Helper methods

// getSizeRange returns a size range label for metrics
func (m *Manager) getSizeRange(size int) string {
	if size <= 10 {
		return "small"
	}
	if size <= 100 {
		return "medium"
	}
	return "large"
}

// recordError records an error for health monitoring
func (m *Manager) recordError(operation string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorCount++
	m.lastError = fmt.Sprintf("%s: %v", operation, err)
	m.logger.Error("Query manager error", "operation", operation, "error", err)
}

// Background monitoring methods removed - query manager is now stateless
// Health is determined on-demand via IsReady() and HealthStatus()
