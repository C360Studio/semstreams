package graph

import (
	"testing"
	"time"
)

func TestIncomingEdgesAddEdge(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{},
	}

	// Add first edge
	edge1 := IncomingEdge{
		FromEntityID: "source-entity-1",
		EdgeType:     "POWERED_BY",
		Weight:       1.0,
		UpdatedAt:    time.Now(),
	}

	ie.AddIncomingEdge(edge1)

	if len(ie.Incoming) != 1 {
		t.Errorf("expected 1 incoming edge, got %d", len(ie.Incoming))
	}

	if ie.Incoming[0].FromEntityID != "source-entity-1" {
		t.Errorf("expected source-entity-1, got %s", ie.Incoming[0].FromEntityID)
	}
}

func TestIncomingEdgesAddDuplicateEdge(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{},
	}

	// Add first edge
	edge1 := IncomingEdge{
		FromEntityID: "source-entity-1",
		EdgeType:     "POWERED_BY",
		Weight:       1.0,
		UpdatedAt:    time.Now(),
	}

	ie.AddIncomingEdge(edge1)

	// Add same edge with different weight (should replace)
	edge2 := IncomingEdge{
		FromEntityID: "source-entity-1",
		EdgeType:     "POWERED_BY",
		Weight:       2.0,
		UpdatedAt:    time.Now(),
	}

	ie.AddIncomingEdge(edge2)

	if len(ie.Incoming) != 1 {
		t.Errorf("expected 1 incoming edge after duplicate, got %d", len(ie.Incoming))
	}

	if ie.Incoming[0].Weight != 2.0 {
		t.Errorf("expected weight 2.0, got %f", ie.Incoming[0].Weight)
	}
}

func TestIncomingEdgesRemoveEdge(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{
			{
				FromEntityID: "source-entity-1",
				EdgeType:     "POWERED_BY",
				Weight:       1.0,
				UpdatedAt:    time.Now(),
			},
			{
				FromEntityID: "source-entity-2",
				EdgeType:     "NEAR",
				Weight:       0.5,
				UpdatedAt:    time.Now(),
			},
		},
	}

	// Remove first edge
	ie.RemoveIncomingEdge("source-entity-1", "POWERED_BY")

	if len(ie.Incoming) != 1 {
		t.Errorf("expected 1 incoming edge after removal, got %d", len(ie.Incoming))
	}

	if ie.Incoming[0].FromEntityID != "source-entity-2" {
		t.Errorf("expected source-entity-2 to remain, got %s", ie.Incoming[0].FromEntityID)
	}
}

func TestIncomingEdgesGetByType(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{
			{
				FromEntityID: "source-entity-1",
				EdgeType:     "POWERED_BY",
				Weight:       1.0,
				UpdatedAt:    time.Now(),
			},
			{
				FromEntityID: "source-entity-2",
				EdgeType:     "NEAR",
				Weight:       0.5,
				UpdatedAt:    time.Now(),
			},
			{
				FromEntityID: "source-entity-3",
				EdgeType:     "POWERED_BY",
				Weight:       1.5,
				UpdatedAt:    time.Now(),
			},
		},
	}

	poweredByEdges := ie.GetIncomingEdgesByType("POWERED_BY")

	if len(poweredByEdges) != 2 {
		t.Errorf("expected 2 POWERED_BY edges, got %d", len(poweredByEdges))
	}

	nearEdges := ie.GetIncomingEdgesByType("NEAR")

	if len(nearEdges) != 1 {
		t.Errorf("expected 1 NEAR edge, got %d", len(nearEdges))
	}

	nonExistentEdges := ie.GetIncomingEdgesByType("NON_EXISTENT")

	if len(nonExistentEdges) != 0 {
		t.Errorf("expected 0 NON_EXISTENT edges, got %d", len(nonExistentEdges))
	}
}

func TestIncomingEdgesGetEntityIDs(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{
			{
				FromEntityID: "source-entity-1",
				EdgeType:     "POWERED_BY",
				Weight:       1.0,
				UpdatedAt:    time.Now(),
			},
			{
				FromEntityID: "source-entity-2",
				EdgeType:     "NEAR",
				Weight:       0.5,
				UpdatedAt:    time.Now(),
			},
			{
				FromEntityID: "source-entity-1", // Duplicate entity with different edge type
				EdgeType:     "NEAR",
				Weight:       0.8,
				UpdatedAt:    time.Now(),
			},
		},
	}

	entityIDs := ie.GetIncomingEntityIDs()

	if len(entityIDs) != 2 {
		t.Errorf("expected 2 unique entity IDs, got %d", len(entityIDs))
	}

	// Check that both entities are present (order doesn't matter)
	found1, found2 := false, false
	for _, id := range entityIDs {
		if id == "source-entity-1" {
			found1 = true
		}
		if id == "source-entity-2" {
			found2 = true
		}
	}

	if !found1 {
		t.Error("expected to find source-entity-1 in entity IDs")
	}
	if !found2 {
		t.Error("expected to find source-entity-2 in entity IDs")
	}
}

func TestIncomingEdgesCount(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{},
	}

	if ie.Count() != 0 {
		t.Errorf("expected count 0 for empty incoming edges, got %d", ie.Count())
	}

	ie.Incoming = append(ie.Incoming, IncomingEdge{
		FromEntityID: "source-entity-1",
		EdgeType:     "POWERED_BY",
		Weight:       1.0,
		UpdatedAt:    time.Now(),
	})

	if ie.Count() != 1 {
		t.Errorf("expected count 1 after adding edge, got %d", ie.Count())
	}
}

func TestIncomingEdgesHasIncomingFrom(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{
			{
				FromEntityID: "source-entity-1",
				EdgeType:     "POWERED_BY",
				Weight:       1.0,
				UpdatedAt:    time.Now(),
			},
		},
	}

	if !ie.HasIncomingFrom("source-entity-1") {
		t.Error("expected to find incoming edge from source-entity-1")
	}

	if ie.HasIncomingFrom("non-existent-entity") {
		t.Error("expected not to find incoming edge from non-existent-entity")
	}
}

func TestIncomingEdgesHasIncomingOfType(t *testing.T) {
	ie := &IncomingEdges{
		EntityID: "target-entity",
		Incoming: []IncomingEdge{
			{
				FromEntityID: "source-entity-1",
				EdgeType:     "POWERED_BY",
				Weight:       1.0,
				UpdatedAt:    time.Now(),
			},
		},
	}

	if !ie.HasIncomingOfType("POWERED_BY") {
		t.Error("expected to find incoming edge of type POWERED_BY")
	}

	if ie.HasIncomingOfType("NON_EXISTENT") {
		t.Error("expected not to find incoming edge of type NON_EXISTENT")
	}
}

func TestIncomingEdgesUpdatedAtModification(t *testing.T) {
	ie := &IncomingEdges{
		EntityID:  "target-entity",
		Incoming:  []IncomingEdge{},
		UpdatedAt: time.Now().Add(-1 * time.Hour), // Set to past time
	}

	originalTime := ie.UpdatedAt

	// Adding an edge should update the timestamp
	edge := IncomingEdge{
		FromEntityID: "source-entity-1",
		EdgeType:     "POWERED_BY",
		Weight:       1.0,
		UpdatedAt:    time.Now(),
	}

	ie.AddIncomingEdge(edge)

	if !ie.UpdatedAt.After(originalTime) {
		t.Error("expected UpdatedAt to be updated after adding edge")
	}

	newTime := ie.UpdatedAt

	// Removing an edge should also update the timestamp
	ie.RemoveIncomingEdge("source-entity-1", "POWERED_BY")

	if !ie.UpdatedAt.After(newTime) {
		t.Error("expected UpdatedAt to be updated after removing edge")
	}
}
