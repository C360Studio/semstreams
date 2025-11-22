// Package graph provides types for NATS mutation API
package graph

// Mutation Request Types

// CreateEntityRequest creates a new entity
type CreateEntityRequest struct {
	Entity    *EntityState `json:"entity"`
	TraceID   string       `json:"trace_id,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// UpdateEntityRequest updates an existing entity
type UpdateEntityRequest struct {
	Entity    *EntityState `json:"entity"`
	TraceID   string       `json:"trace_id,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// DeleteEntityRequest deletes an entity
type DeleteEntityRequest struct {
	EntityID  string `json:"entity_id"`
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// CreateEntityWithEdgesRequest creates entity with edges atomically
type CreateEntityWithEdgesRequest struct {
	Entity    *EntityState `json:"entity"`
	Edges     []Edge       `json:"edges"`
	TraceID   string       `json:"trace_id,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// UpdateEntityWithEdgesRequest updates entity and modifies edges atomically
type UpdateEntityWithEdgesRequest struct {
	Entity      *EntityState `json:"entity"`
	AddEdges    []Edge       `json:"add_edges,omitempty"`
	RemoveEdges []string     `json:"remove_edges,omitempty"` // Edge IDs to remove
	TraceID     string       `json:"trace_id,omitempty"`
	RequestID   string       `json:"request_id,omitempty"`
}

// AddEdgeRequest adds an edge to an existing entity
type AddEdgeRequest struct {
	FromEntityID string                 `json:"from_entity_id"`
	ToEntityID   string                 `json:"to_entity_id"`
	EdgeType     string                 `json:"edge_type"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
	Weight       float64                `json:"weight,omitempty"`
	TraceID      string                 `json:"trace_id,omitempty"`
	RequestID    string                 `json:"request_id,omitempty"`
}

// RemoveEdgeRequest removes an edge from an entity
type RemoveEdgeRequest struct {
	FromEntityID string `json:"from_entity_id"`
	ToEntityID   string `json:"to_entity_id"`
	EdgeType     string `json:"edge_type"`
	TraceID      string `json:"trace_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
}
