// Package graph provides request/response types for the NATS mutation and query APIs.
//
// These types are the public contract for graph operations via NATS request/reply.
// External consumers (semspec, semdragon) and internal components import this
// package directly to build requests and parse responses.
//
// Graph request/reply is intentionally exempt from the BaseMessage payload registry.
// Request/reply is point-to-point — the subject determines the handler, so
// type-discriminated dispatch adds no value. See docs/concepts/15-payload-registry.md
// for the full rationale.
package graph

import "github.com/c360studio/semstreams/message"

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

// CreateEntityWithTriplesRequest creates entity with triples atomically
type CreateEntityWithTriplesRequest struct {
	Entity    *EntityState     `json:"entity"`
	Triples   []message.Triple `json:"triples"`
	TraceID   string           `json:"trace_id,omitempty"`
	RequestID string           `json:"request_id,omitempty"`
}

// UpdateEntityWithTriplesRequest updates entity and modifies triples atomically
type UpdateEntityWithTriplesRequest struct {
	Entity        *EntityState     `json:"entity"`
	AddTriples    []message.Triple `json:"add_triples,omitempty"`
	RemoveTriples []string         `json:"remove_triples,omitempty"` // Triple predicates to remove
	TraceID       string           `json:"trace_id,omitempty"`
	RequestID     string           `json:"request_id,omitempty"`
}

// AddTripleRequest adds a triple to an existing entity
type AddTripleRequest struct {
	Triple    message.Triple `json:"triple"`
	TraceID   string         `json:"trace_id,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// AddTriplesBatchRequest adds many triples in one request, atomically
// per-entity (one CAS per distinct triple.Subject). Optimised for tools
// that emit multiple triples in a single tool call (e.g. write_todos
// per ADR-036 — 5 triples per todo × N todos all on the same loop
// entity collapse to 1 CAS round-trip).
//
// The batch is NOT atomic across entities — if a write to entity A
// succeeds but a CAS conflict on entity B exhausts retries, the
// response surfaces the partial-failure shape and triples for A
// remain written. Single-entity callers (the common case) see this
// as fully atomic because there is only one CAS group.
type AddTriplesBatchRequest struct {
	Triples   []message.Triple `json:"triples"`
	TraceID   string           `json:"trace_id,omitempty"`
	RequestID string           `json:"request_id,omitempty"`
}

// RemoveTripleRequest removes a triple from an entity
type RemoveTripleRequest struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}
