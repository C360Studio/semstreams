package graphclustering

import (
	"context"

	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/processor/graph/querymanager"
)

// QueryManagerGraphProvider implements GraphProvider using QueryManager
type QueryManagerGraphProvider struct {
	queryManager querymanager.Querier
}

// NewQueryManagerGraphProvider creates a GraphProvider backed by QueryManager
func NewQueryManagerGraphProvider(qm querymanager.Querier) *QueryManagerGraphProvider {
	return &QueryManagerGraphProvider{
		queryManager: qm,
	}
}

// GetAllEntityIDs returns all entity IDs in the graph
func (p *QueryManagerGraphProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	// QueryManager doesn't have a "get all entities" method
	// We'll need to query by common predicates or use a different approach

	// For now, we'll return an error indicating this needs implementation
	// In production, this would likely use a predicate index scan or
	// iterate through the entity store

	return nil, errors.WrapFatal(
		errors.ErrMissingConfig,
		"QueryManagerGraphProvider",
		"GetAllEntityIDs",
		"full graph scan not yet implemented - use PredicateGraphProvider instead",
	)
}

// GetNeighbors returns entity IDs connected to the given entity
func (p *QueryManagerGraphProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if entityID == "" {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "QueryManagerGraphProvider", "GetNeighbors", "entityID is empty")
	}

	// Map direction string to QueryManager direction
	var qmDirection querymanager.Direction
	switch direction {
	case "outgoing":
		qmDirection = querymanager.DirectionOutgoing
	case "incoming":
		qmDirection = querymanager.DirectionIncoming
	case "both":
		qmDirection = querymanager.DirectionBoth
	default:
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "QueryManagerGraphProvider", "GetNeighbors", "invalid direction")
	}

	// Query relationships
	rels, err := p.queryManager.QueryRelationships(ctx, entityID, qmDirection)
	if err != nil {
		return nil, errors.WrapTransient(err, "QueryManagerGraphProvider", "GetNeighbors", "query relationships")
	}

	// Extract unique neighbor IDs
	neighborMap := make(map[string]bool)
	for _, rel := range rels {
		// Add both endpoints (filter out self later)
		if rel.FromEntityID != entityID {
			neighborMap[rel.FromEntityID] = true
		}
		if rel.ToEntityID != entityID {
			neighborMap[rel.ToEntityID] = true
		}
	}

	// Convert to slice
	neighbors := make([]string, 0, len(neighborMap))
	for neighborID := range neighborMap {
		neighbors = append(neighbors, neighborID)
	}

	return neighbors, nil
}

// GetEdgeWeight returns the weight of an edge between two entities
func (p *QueryManagerGraphProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	if fromID == "" || toID == "" {
		return 0.0, errors.WrapInvalid(errors.ErrMissingConfig, "QueryManagerGraphProvider", "GetEdgeWeight", "entity IDs are empty")
	}

	// Query outgoing relationships from fromID
	rels, err := p.queryManager.QueryRelationships(ctx, fromID, querymanager.DirectionOutgoing)
	if err != nil {
		return 0.0, errors.WrapTransient(err, "QueryManagerGraphProvider", "GetEdgeWeight", "query relationships")
	}

	// Find edge to toID
	for _, rel := range rels {
		if rel.ToEntityID == toID {
			// Edge exists - return weight if available, else 1.0
			// Note: Current relationship model doesn't have weights
			// Future enhancement: extract from rel.Properties["weight"]
			return 1.0, nil
		}
	}

	// No edge found
	return 0.0, nil
}

// PredicateGraphProvider implements GraphProvider for entities matching a predicate
// This is more practical for real-world use: cluster entities of specific types
type PredicateGraphProvider struct {
	queryManager  querymanager.Querier
	predicate     string // Entity type/predicate to cluster
	validEntities map[string]bool // Cached set of valid entity IDs
}

// NewPredicateGraphProvider creates a GraphProvider for entities matching a predicate
// It caches the valid entity set at construction time for performance
func NewPredicateGraphProvider(qm querymanager.Querier, predicate string) *PredicateGraphProvider {
	return &PredicateGraphProvider{
		queryManager:  qm,
		predicate:     predicate,
		validEntities: nil, // Lazy initialization on first use
	}
}

// GetAllEntityIDs returns all entities matching the predicate
func (p *PredicateGraphProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	// Initialize cache if needed
	if err := p.ensureValidEntitiesCache(ctx); err != nil {
		return nil, err
	}

	// Return all cached entity IDs
	entityIDs := make([]string, 0, len(p.validEntities))
	for id := range p.validEntities {
		entityIDs = append(entityIDs, id)
	}

	return entityIDs, nil
}

// ensureValidEntitiesCache initializes the valid entities cache if not already done
func (p *PredicateGraphProvider) ensureValidEntitiesCache(ctx context.Context) error {
	if p.validEntities != nil {
		return nil // Already cached
	}

	// Query all entities matching predicate
	entityIDs, err := p.queryManager.QueryByPredicate(ctx, p.predicate)
	if err != nil {
		return errors.WrapTransient(err, "PredicateGraphProvider", "ensureValidEntitiesCache", "query by predicate")
	}

	// Build cache map for O(1) lookup
	p.validEntities = make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		p.validEntities[id] = true
	}

	return nil
}

// GetNeighbors returns entity IDs connected to the given entity
func (p *PredicateGraphProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if entityID == "" {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "PredicateGraphProvider", "GetNeighbors", "entityID is empty")
	}

	// Ensure cache is initialized
	if err := p.ensureValidEntitiesCache(ctx); err != nil {
		return nil, err
	}

	// Map direction
	var qmDirection querymanager.Direction
	switch direction {
	case "outgoing":
		qmDirection = querymanager.DirectionOutgoing
	case "incoming":
		qmDirection = querymanager.DirectionIncoming
	case "both":
		qmDirection = querymanager.DirectionBoth
	default:
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "PredicateGraphProvider", "GetNeighbors", "invalid direction")
	}

	// Query relationships
	rels, err := p.queryManager.QueryRelationships(ctx, entityID, qmDirection)
	if err != nil {
		return nil, errors.WrapTransient(err, "PredicateGraphProvider", "GetNeighbors", "query relationships")
	}

	// Extract neighbors that match predicate (using cached validEntities)
	neighborMap := make(map[string]bool)
	for _, rel := range rels {
		// Only include neighbors that match the predicate
		if rel.FromEntityID != entityID && p.validEntities[rel.FromEntityID] {
			neighborMap[rel.FromEntityID] = true
		}
		if rel.ToEntityID != entityID && p.validEntities[rel.ToEntityID] {
			neighborMap[rel.ToEntityID] = true
		}
	}

	// Convert to slice
	neighbors := make([]string, 0, len(neighborMap))
	for neighborID := range neighborMap {
		neighbors = append(neighbors, neighborID)
	}

	return neighbors, nil
}

// GetEdgeWeight returns the weight of an edge (unweighted: 1.0 or 0.0)
func (p *PredicateGraphProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	if fromID == "" || toID == "" {
		return 0.0, errors.WrapInvalid(errors.ErrMissingConfig, "PredicateGraphProvider", "GetEdgeWeight", "entity IDs are empty")
	}

	// Query outgoing relationships
	rels, err := p.queryManager.QueryRelationships(ctx, fromID, querymanager.DirectionOutgoing)
	if err != nil {
		return 0.0, errors.WrapTransient(err, "PredicateGraphProvider", "GetEdgeWeight", "query relationships")
	}

	// Find edge to toID
	for _, rel := range rels {
		if rel.ToEntityID == toID {
			return 1.0, nil
		}
	}

	return 0.0, nil
}
