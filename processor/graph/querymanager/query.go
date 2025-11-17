package querymanager

import (
	"context"
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// This file contains the complex query operations for query manager.
// These methods implement graph traversal, snapshots, and relationship queries.

// ExecutePath executes a graph path traversal starting from a given entity
func (qe *Manager) ExecutePath(ctx context.Context, start string, pattern PathPattern) (*QueryResult, error) {
	startTime := time.Now()
	defer func() {
		qe.lastActivity = time.Now()
	}()

	// Apply timeout
	pathCtx := ctx
	if qe.config.Query.PathTimeout > 0 {
		var cancel context.CancelFunc
		pathCtx, cancel = context.WithTimeout(ctx, qe.config.Query.PathTimeout)
		defer cancel()
	}

	// Validate pattern
	if err := qe.validatePathPattern(pattern); err != nil {
		return nil, errors.WrapInvalid(err, "QueryManager", "ExecutePath",
			"pattern validation failed")
	}

	// Direct execution - no caching at query manager level (index manager will handle its own caching)

	// Execute path traversal
	result, err := qe.executePathTraversal(pathCtx, start, pattern)
	if err != nil {
		qe.recordError("ExecutePath", err)
		if qe.metrics != nil {
			qe.metrics.RecordQuery("query_engine", "path", time.Since(startTime), 0, false)
		}
		return nil, err
	}

	result.Duration = time.Since(startTime)
	result.Cached = false

	// No caching at query manager level - index manager will handle its own caching

	// Record metrics
	if qe.metrics != nil {
		qe.metrics.RecordQuery("query_engine", "path", time.Since(startTime), len(result.Entities), true)
		if len(result.Paths) > 0 {
			qe.metrics.pathExecutionTotal.Inc()
			qe.metrics.pathExecutionDuration.Observe(time.Since(startTime).Seconds())
			for _, path := range result.Paths {
				qe.metrics.pathLengthHistogram.Observe(float64(path.Length))
			}
		}
	}

	return result, nil
}

// GetGraphSnapshot creates a snapshot of entities within the specified bounds
func (qe *Manager) GetGraphSnapshot(ctx context.Context, bounds QueryBounds) (*GraphSnapshot, error) {
	startTime := time.Now()
	defer func() {
		qe.lastActivity = time.Now()
	}()

	// Apply timeout
	snapshotCtx := ctx
	if qe.config.Query.SnapshotTimeout > 0 {
		var cancel context.CancelFunc
		snapshotCtx, cancel = context.WithTimeout(ctx, qe.config.Query.SnapshotTimeout)
		defer cancel()
	}

	// Validate bounds
	if err := qe.validateQueryBounds(bounds); err != nil {
		return nil, errors.WrapInvalid(err, "QueryManager", "GetGraphSnapshot",
			"bounds validation failed")
	}

	// Execute snapshot query
	snapshot, err := qe.executeSnapshotQuery(snapshotCtx, bounds)
	if err != nil {
		qe.recordError("GetGraphSnapshot", err)
		if qe.metrics != nil {
			qe.metrics.snapshotTotal.Inc()
		}
		return nil, err
	}

	snapshot.Timestamp = time.Now()

	// Record metrics
	if qe.metrics != nil {
		qe.metrics.snapshotTotal.Inc()
		qe.metrics.snapshotDuration.Observe(time.Since(startTime).Seconds())
		qe.metrics.snapshotSizeHistogram.Observe(float64(snapshot.Count))
	}

	return snapshot, nil
}

// QueryRelationships queries relationships for an entity in the specified direction
func (qe *Manager) QueryRelationships(
	ctx context.Context, entityID string, direction Direction,
) ([]*Relationship, error) {
	startTime := time.Now()
	defer func() {
		qe.lastActivity = time.Now()
	}()

	// Validate direction
	if !qe.isValidDirection(direction) {
		return nil, errors.WrapInvalid(gtypes.ErrInvalidQueryParams, "QueryManager", "QueryRelationships",
			fmt.Sprintf("invalid direction: %s", direction))
	}

	// Check if index manager is available for relationship queries
	if qe.indexManager == nil {
		return nil, errors.WrapTransient(ErrIndexManagerUnavailable, "QueryManager", "QueryRelationships",
			"index manager dependency unavailable")
	}

	var relationships []*Relationship
	var err error

	// Query based on direction
	switch direction {
	case DirectionIncoming:
		relationships, err = qe.queryIncomingRelationships(ctx, entityID)
	case DirectionOutgoing:
		relationships, err = qe.queryOutgoingRelationships(ctx, entityID)
	case DirectionBoth:
		incoming, err1 := qe.queryIncomingRelationships(ctx, entityID)
		if err1 != nil {
			return nil, err1
		}
		outgoing, err2 := qe.queryOutgoingRelationships(ctx, entityID)
		if err2 != nil {
			return nil, err2
		}
		relationships = append(incoming, outgoing...)
	default:
		return nil, errors.WrapInvalid(gtypes.ErrInvalidQueryParams, "QueryManager", "QueryRelationships",
			fmt.Sprintf("unsupported direction: %s", direction))
	}

	if err != nil {
		qe.recordError("QueryRelationships", err)
		if qe.metrics != nil {
			qe.metrics.RecordQuery("query_engine", "relationships", time.Since(startTime), 0, false)
		}
		return nil, err
	}

	// Record metrics
	if qe.metrics != nil {
		qe.metrics.RecordQuery("query_engine", "relationships", time.Since(startTime), len(relationships), true)
	}

	return relationships, nil
}

// Helper methods for complex queries

// validatePathPattern validates a path pattern
func (qe *Manager) validatePathPattern(pattern PathPattern) error {
	if pattern.MaxDepth <= 0 {
		msg := fmt.Sprintf("max depth must be positive, got %d", pattern.MaxDepth)
		return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
			"validatePathPattern", msg)
	}
	if pattern.MaxDepth > qe.config.Query.MaxPathLength {
		msg := fmt.Sprintf("max depth %d exceeds limit %d",
			pattern.MaxDepth, qe.config.Query.MaxPathLength)
		return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
			"validatePathPattern", msg)
	}
	return nil
}

// validateQueryBounds validates query bounds
func (qe *Manager) validateQueryBounds(bounds QueryBounds) error {
	if bounds.MaxEntities > qe.config.Query.MaxSnapshotSize {
		msg := fmt.Sprintf("max entities %d exceeds limit %d",
			bounds.MaxEntities, qe.config.Query.MaxSnapshotSize)
		return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
			"validateQueryBounds", msg)
	}
	if bounds.Spatial != nil {
		if bounds.Spatial.North < bounds.Spatial.South {
			return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
				"validateQueryBounds", "north latitude must be >= south latitude")
		}
		if bounds.Spatial.East < bounds.Spatial.West {
			return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
				"validateQueryBounds", "east longitude must be >= west longitude")
		}
	}
	if bounds.Temporal != nil {
		if bounds.Temporal.End.Before(bounds.Temporal.Start) {
			return errors.WrapInvalid(errors.ErrInvalidData, "query manager",
				"validateQueryBounds", "end time must be after start time")
		}
	}
	return nil
}

// isValidDirection checks if a direction is valid
func (qe *Manager) isValidDirection(direction Direction) bool {
	switch direction {
	case DirectionIncoming, DirectionOutgoing, DirectionBoth:
		return true
	default:
		return false
	}
}

// generatePathQueryKey generates a cache key for path queries
func (qe *Manager) generatePathQueryKey(start string, pattern PathPattern) string {
	return fmt.Sprintf("path:%s:%d:%v:%v", start, pattern.MaxDepth, pattern.EdgeTypes, pattern.Direction)
}

// executePathTraversal executes the actual path traversal algorithm
func (qe *Manager) executePathTraversal(ctx context.Context, start string, pattern PathPattern) (*QueryResult, error) {
	// Initialize result
	result := &QueryResult{
		Entities: []*gtypes.EntityState{},
		Paths:    []GraphPath{},
		Count:    0,
	}

	// Get starting entity
	startEntity, err := qe.GetEntity(ctx, start)
	if err != nil {
		return nil, errors.WrapTransient(err, "QueryManager", "ExecutePath",
			fmt.Sprintf("failed to get start entity: %s", start))
	}

	result.Entities = append(result.Entities, startEntity)

	// TODO: Implement actual graph traversal algorithm
	// This is a placeholder implementation that just returns the starting entity
	// Full implementation would:
	// 1. Use index manager to find relationships
	// 2. Follow edges based on pattern.EdgeTypes and pattern.Direction
	// 3. Apply filters from pattern.Filters
	// 4. Respect pattern.MaxDepth
	// 5. Build GraphPath objects for each discovered path
	// 6. Load entities for all discovered nodes

	result.Count = len(result.Entities)

	// Add a placeholder path
	if pattern.IncludeSelf {
		result.Paths = append(result.Paths, GraphPath{
			Entities: []string{start},
			Edges:    []GraphEdge{},
			Length:   0,
			Weight:   1.0,
		})
	}

	return result, nil
}

// executeSnapshotQuery executes a graph snapshot query
func (qe *Manager) executeSnapshotQuery(ctx context.Context, bounds QueryBounds) (*GraphSnapshot, error) {
	snapshot := &GraphSnapshot{
		Entities:      []*gtypes.EntityState{},
		Relationships: []Relationship{},
		Bounds:        bounds,
		Count:         0,
		Truncated:     false,
	}

	var entityIDs []string

	// Query entities based on bounds
	if bounds.Spatial != nil && qe.indexManager != nil {
		// Query by spatial bounds
		spatialIDs, err := qe.QuerySpatial(ctx, *bounds.Spatial)
		if err != nil {
			return nil, errors.WrapTransient(err, "QueryManager", "GetGraphSnapshot",
				"spatial query failed")
		}
		entityIDs = append(entityIDs, spatialIDs...)
	}

	if bounds.Temporal != nil && qe.indexManager != nil {
		// Query by temporal bounds
		temporalIDs, err := qe.QueryTemporal(ctx, bounds.Temporal.Start, bounds.Temporal.End)
		if err != nil {
			return nil, errors.WrapTransient(err, "QueryManager", "GetGraphSnapshot",
				"temporal query failed")
		}
		entityIDs = append(entityIDs, temporalIDs...)
	}

	// If no specific bounds, get entities by type
	if len(entityIDs) == 0 && len(bounds.EntityTypes) > 0 && qe.indexManager != nil {
		for _, entityType := range bounds.EntityTypes {
			typeIDs, err := qe.QueryByPredicate(ctx, "type:"+entityType)
			if err != nil {
				return nil, errors.WrapTransient(err, "QueryManager", "GetGraphSnapshot",
					"type query failed")
			}
			entityIDs = append(entityIDs, typeIDs...)
		}
	}

	// Remove duplicates
	entityIDs = qe.removeDuplicateIDs(entityIDs)

	// Apply max entities limit
	if bounds.MaxEntities > 0 && len(entityIDs) > bounds.MaxEntities {
		entityIDs = entityIDs[:bounds.MaxEntities]
		snapshot.Truncated = true
	}

	// Load entities
	if len(entityIDs) > 0 {
		entities, err := qe.GetEntities(ctx, entityIDs)
		if err != nil {
			return nil, errors.WrapTransient(err, "QueryManager", "GetGraphSnapshot",
				"failed to load entities")
		}
		snapshot.Entities = entities
	}

	snapshot.Count = len(snapshot.Entities)

	// TODO: Load relationships between entities in the snapshot
	// This would require querying the index manager for relationships
	// between the entities in the snapshot

	return snapshot, nil
}

// queryIncomingRelationships queries incoming relationships for an entity
func (qe *Manager) queryIncomingRelationships(ctx context.Context, entityID string) ([]*Relationship, error) {
	// Use index manager to get incoming relationships
	indexRelationships, err := qe.indexManager.GetIncomingRelationships(ctx, entityID)
	if err != nil {
		return nil, errors.WrapTransient(err, "QueryManager", "GetIncomingRelationships",
			fmt.Sprintf("index manager failed for entity: %s", entityID))
	}

	// Convert to query manager relationship format
	relationships := make([]*Relationship, len(indexRelationships))
	for i, rel := range indexRelationships {
		relationships[i] = &Relationship{
			FromEntityID: rel.FromEntityID,
			ToEntityID:   entityID, // This is the target entity
			EdgeType:     rel.EdgeType,
			Properties:   rel.Properties,
			Weight:       rel.Weight,
			CreatedAt:    rel.CreatedAt,
			UpdatedAt:    rel.CreatedAt, // index manager doesn't track updated time
		}
	}

	return relationships, nil
}

// queryOutgoingRelationships queries outgoing relationships for an entity
// This implementation fetches the entity and converts its edges to relationships
func (qe *Manager) queryOutgoingRelationships(ctx context.Context, entityID string) ([]*Relationship, error) {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Get the entity to access its edges
	entity, err := qe.GetEntity(ctx, entityID)
	if err != nil {
		return nil, errors.WrapTransient(err, "QueryManager", "queryOutgoingRelationships",
			fmt.Sprintf("failed to get entity: %s", entityID))
	}

	// If entity not found or has no edges, return empty slice
	if entity == nil || len(entity.Edges) == 0 {
		return []*Relationship{}, nil
	}

	// Convert entity edges to relationships
	relationships := make([]*Relationship, 0, len(entity.Edges))
	now := time.Now()

	for _, edge := range entity.Edges {
		// Create relationship from edge
		rel := &Relationship{
			FromEntityID: entityID,
			ToEntityID:   edge.ToEntityID,
			EdgeType:     edge.EdgeType,
			Properties:   edge.Properties,
			Weight:       1.0, // Default weight
			CreatedAt:    now, // We don't track creation time on edges yet
			UpdatedAt:    now,
		}

		// Extract weight from properties if available
		if weight, ok := edge.Properties["weight"].(float64); ok {
			rel.Weight = weight
		} else if weight, ok := edge.Properties["weight"].(int); ok {
			rel.Weight = float64(weight)
		}

		relationships = append(relationships, rel)
	}

	return relationships, nil
}

// removeDuplicateIDs removes duplicate entity IDs from a slice
func (qe *Manager) removeDuplicateIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	unique := make([]string, 0, len(ids))

	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	return unique
}
