// Package graph provides types for NATS mutation API
package graph

import "time"

// Mutation Response Types

// MutationResponse is the base response for all mutations
type MutationResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp int64  `json:"timestamp"` // Unix nano timestamp
}

// CreateEntityResponse response for entity creation
type CreateEntityResponse struct {
	MutationResponse
	Entity *EntityState `json:"entity,omitempty"`
}

// UpdateEntityResponse response for entity update
type UpdateEntityResponse struct {
	MutationResponse
	Entity  *EntityState `json:"entity,omitempty"`
	Version int64        `json:"version,omitempty"`
}

// DeleteEntityResponse response for entity deletion
type DeleteEntityResponse struct {
	MutationResponse
	Deleted bool `json:"deleted"`
}

// CreateEntityWithEdgesResponse response for atomic entity+edges creation
type CreateEntityWithEdgesResponse struct {
	MutationResponse
	Entity     *EntityState `json:"entity,omitempty"`
	EdgesAdded int          `json:"edges_added"`
}

// UpdateEntityWithEdgesResponse response for atomic entity+edges update
type UpdateEntityWithEdgesResponse struct {
	MutationResponse
	Entity       *EntityState `json:"entity,omitempty"`
	EdgesAdded   int          `json:"edges_added"`
	EdgesRemoved int          `json:"edges_removed"`
	Version      int64        `json:"version,omitempty"`
}

// AddEdgeResponse response for edge addition
type AddEdgeResponse struct {
	MutationResponse
	Edge *Edge `json:"edge,omitempty"`
}

// RemoveEdgeResponse response for edge removal
type RemoveEdgeResponse struct {
	MutationResponse
	Removed bool `json:"removed"`
}

// Helper functions

// NewMutationResponse creates a base mutation response
func NewMutationResponse(success bool, err error, traceID, requestID string) MutationResponse {
	resp := MutationResponse{
		Success:   success,
		TraceID:   traceID,
		RequestID: requestID,
		Timestamp: time.Now().UnixNano(),
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp
}
