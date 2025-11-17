// Package graph provides incoming edge index types for reverse graph traversal
package graph

import "time"

// IncomingEdges tracks all entities that point TO this entity
type IncomingEdges struct {
	EntityID  string         `json:"entity_id"`
	Incoming  []IncomingEdge `json:"incoming"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// IncomingEdge represents an edge pointing TO this entity
type IncomingEdge struct {
	FromEntityID string    `json:"from_entity_id"`
	EdgeType     string    `json:"edge_type"`
	Weight       float64   `json:"weight,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AddIncomingEdge adds or updates an incoming edge
func (ie *IncomingEdges) AddIncomingEdge(edge IncomingEdge) {
	// Replace existing edge of same type from same entity
	for i, e := range ie.Incoming {
		if e.EdgeType == edge.EdgeType && e.FromEntityID == edge.FromEntityID {
			ie.Incoming[i] = edge
			ie.UpdatedAt = time.Now()
			return
		}
	}
	// Add new edge
	ie.Incoming = append(ie.Incoming, edge)
	ie.UpdatedAt = time.Now()
}

// RemoveIncomingEdge removes an incoming edge
func (ie *IncomingEdges) RemoveIncomingEdge(fromEntityID, edgeType string) {
	filtered := ie.Incoming[:0]
	for _, edge := range ie.Incoming {
		if !(edge.EdgeType == edgeType && edge.FromEntityID == fromEntityID) {
			filtered = append(filtered, edge)
		}
	}
	ie.Incoming = filtered
	ie.UpdatedAt = time.Now()
}

// GetIncomingEdgesByType returns all incoming edges of a specific type
func (ie *IncomingEdges) GetIncomingEdgesByType(edgeType string) []IncomingEdge {
	var edges []IncomingEdge
	for _, edge := range ie.Incoming {
		if edge.EdgeType == edgeType {
			edges = append(edges, edge)
		}
	}
	return edges
}

// GetIncomingEntityIDs returns all entity IDs that have edges pointing to this entity
func (ie *IncomingEdges) GetIncomingEntityIDs() []string {
	var entityIDs []string
	seen := make(map[string]bool)

	for _, edge := range ie.Incoming {
		if !seen[edge.FromEntityID] {
			entityIDs = append(entityIDs, edge.FromEntityID)
			seen[edge.FromEntityID] = true
		}
	}

	return entityIDs
}

// Count returns the number of incoming edges
func (ie *IncomingEdges) Count() int {
	return len(ie.Incoming)
}

// HasIncomingFrom checks if there's an incoming edge from the specified entity
func (ie *IncomingEdges) HasIncomingFrom(fromEntityID string) bool {
	for _, edge := range ie.Incoming {
		if edge.FromEntityID == fromEntityID {
			return true
		}
	}
	return false
}

// HasIncomingOfType checks if there's an incoming edge of the specified type
func (ie *IncomingEdges) HasIncomingOfType(edgeType string) bool {
	for _, edge := range ie.Incoming {
		if edge.EdgeType == edgeType {
			return true
		}
	}
	return false
}
