package datamanager

import (
	"context"
	"fmt"
	"time"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// Edge Operations

// AddEdge adds an edge to an entity
func (m *Manager) AddEdge(ctx context.Context, fromEntityID string, edge gtypes.Edge) error {
	// Get entity
	entity, err := m.GetEntity(ctx, fromEntityID)
	if err != nil {
		// GetEntity already records its errors
		return err
	}

	// Validate edge target if configured
	if m.config.Edge.ValidateEdgeTargets {
		exists, err := m.ExistsEntity(ctx, edge.ToEntityID)
		if err != nil {
			err = errors.Wrap(err, "DataManager", "AddEdge", "validate target entity")
			return err
		}
		if !exists {
			err = errors.WrapInvalid(nil, "DataManager", "AddEdge",
				fmt.Sprintf("target entity %s does not exist", edge.ToEntityID))
			return err
		}
	}

	// Add edge
	entity.AddEdge(edge)

	// Update entity
	_, err = m.UpdateEntity(ctx, entity)
	// UpdateEntity already records its errors
	return err
}

// RemoveEdge removes an edge from an entity
func (m *Manager) RemoveEdge(ctx context.Context, fromEntityID, toEntityID, edgeType string) error {
	// Get entity
	entity, err := m.GetEntity(ctx, fromEntityID)
	if err != nil {
		// GetEntity already records its errors
		return err
	}

	// Remove edge
	removed := false
	newEdges := []gtypes.Edge{}
	for _, edge := range entity.Edges {
		if edge.ToEntityID == toEntityID && edge.EdgeType == edgeType {
			removed = true
			continue
		}
		newEdges = append(newEdges, edge)
	}

	if !removed {
		err = errors.WrapInvalid(nil, "DataManager", "RemoveEdge",
			fmt.Sprintf("edge from %s to %s with type %s not found", fromEntityID, toEntityID, edgeType))
		return err
	}

	entity.Edges = newEdges

	// Update entity
	_, err = m.UpdateEntity(ctx, entity)
	// UpdateEntity already records its errors
	return err
}

// CreateRelationship creates a relationship between two entities
func (m *Manager) CreateRelationship(
	ctx context.Context,
	fromEntityID, toEntityID string,
	edgeType string,
	properties map[string]any,
) error {
	edge := gtypes.Edge{
		ToEntityID: toEntityID,
		EdgeType:   edgeType,
		Properties: properties,
		Weight:     1.0,
		CreatedAt:  time.Now(),
	}

	return m.AddEdge(ctx, fromEntityID, edge)
}

// DeleteRelationship deletes a relationship between entities
func (m *Manager) DeleteRelationship(ctx context.Context, fromEntityID, toEntityID string) error {
	// Get entity to find edge type
	entity, err := m.GetEntity(ctx, fromEntityID)
	if err != nil {
		return err
	}

	// Find and remove edge
	for _, edge := range entity.Edges {
		if edge.ToEntityID == toEntityID {
			return m.RemoveEdge(ctx, fromEntityID, toEntityID, edge.EdgeType)
		}
	}

	return errors.WrapInvalid(nil, "DataManager", "DeleteRelationship",
		fmt.Sprintf("relationship from %s to %s not found", fromEntityID, toEntityID))
}

// CleanupIncomingReferences removes references to a deleted entity
func (m *Manager) CleanupIncomingReferences(
	ctx context.Context,
	deletedEntityID string,
	outgoingEdges []gtypes.Edge,
) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// This would typically use the INCOMING_INDEX to find entities pointing to the deleted entity
	// For now, log the cleanup requirement
	m.logger.Debug("Cleanup incoming references",
		"deleted_entity", deletedEntityID,
		"outgoing_edges_count", len(outgoingEdges),
	)

	// TODO: In Phase 2, when we consolidate with IndexManager, implement proper cleanup
	return nil
}

// CheckOutgoingEdgesConsistency checks edge consistency
func (m *Manager) CheckOutgoingEdgesConsistency(
	ctx context.Context,
	_ string,
	entity *gtypes.EntityState,
	status *EntityIndexStatus,
) {
	if entity == nil || status == nil {
		return
	}

	status.OutgoingEdgesConsistent = true
	status.InconsistentEdges = []string{}

	// Check each edge target exists
	for _, edge := range entity.Edges {
		exists, err := m.ExistsEntity(ctx, edge.ToEntityID)
		if err != nil || !exists {
			status.OutgoingEdgesConsistent = false
			status.InconsistentEdges = append(status.InconsistentEdges, edge.ToEntityID)
		}
	}
}

// HasEdgeToEntity checks if an entity has an edge to a target
func (m *Manager) HasEdgeToEntity(entity *gtypes.EntityState, targetEntityID string) bool {
	if entity == nil {
		return false
	}

	for _, edge := range entity.Edges {
		if edge.ToEntityID == targetEntityID {
			return true
		}
	}
	return false
}
