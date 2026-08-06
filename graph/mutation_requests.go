// Package graph provides the canonical graph mutation request/reply contract.
// Request/reply messages are identified by their typed port and operation
// subject, so they do not use the polymorphic payload registry.
package graph

import "github.com/c360studio/semstreams/message"

// CreateEntityRequest atomically creates one entity with its initial facts.
type CreateEntityRequest struct {
	Entity          *EntityState     `json:"entity"`
	Triples         []message.Triple `json:"triples"`
	IndexingProfile string           `json:"indexing_profile,omitempty"`
	TraceID         string           `json:"trace_id,omitempty"`
	RequestID       string           `json:"request_id,omitempty"`
}

// DeleteEntityRequest conditionally deletes one entity.
type DeleteEntityRequest struct {
	EntityID         string `json:"entity_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	TraceID          string `json:"trace_id,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
}

// ReconcilePredicatesRequest makes the named predicate set equal Desired on
// one existing entity at ExpectedRevision. Desired may be empty.
type ReconcilePredicatesRequest struct {
	EntityID         string           `json:"entity_id"`
	ExpectedRevision uint64           `json:"expected_revision"`
	Predicates       []string         `json:"predicates"`
	Desired          []message.Triple `json:"desired"`
	TraceID          string           `json:"trace_id,omitempty"`
	RequestID        string           `json:"request_id,omitempty"`
}

// AppendTriplesRequest appends canonical tuples independently per subject.
type AppendTriplesRequest struct {
	Triples   []message.Triple `json:"triples"`
	TraceID   string           `json:"trace_id,omitempty"`
	RequestID string           `json:"request_id,omitempty"`
}
