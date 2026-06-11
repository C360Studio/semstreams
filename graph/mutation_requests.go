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
	Entity  *EntityState     `json:"entity"`
	Triples []message.Triple `json:"triples"`
	// IndexingProfile optionally declares the entity's indexing profile at
	// creation (ADR-054 channel b): one of "content"|"control"|"signal"|
	// "trace". When set, graph-ingest stamps entity.indexing.profile from it
	// (highest precedence). When empty, graph-ingest falls through to the
	// Graphable IndexingProfiler, then the fallback floor. Invalid values are
	// treated as absent (lenient). This is the channel the registry-only
	// design lacked — rule/lifecycle/loop/memory/research-graph writers use it
	// to declare intent. Only meaningful on CREATE; entity-birth is the only
	// place a profile is set (see also UpdateEntityWithTriplesRequest for the
	// explicit-override exception).
	IndexingProfile string `json:"indexing_profile,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
}

// UpdateEntityWithTriplesRequest updates entity and modifies triples atomically.
//
// CAS-on-condition (ADR-049): when ExpectedRevision is non-zero, the
// handler reads the entity's current KV revision and rejects the
// request with a "revision mismatch" error if it doesn't match. This
// gives callers (notably pkg/lifecycle.Manager.Transition) the
// primitive needed to handle state-machine races — two writers
// transitioning the same entity from the same start state both pass
// validation but only one is allowed to commit.
//
// When ExpectedRevision is zero (the default + the shape every
// existing caller uses), the handler uses internal UpdateWithRetry
// semantics: re-read + re-apply the delta on each retry until the
// CAS succeeds or the retry budget is exhausted. This is correct for
// the "facts accumulate" model the graph layer was built around.
type UpdateEntityWithTriplesRequest struct {
	Entity *EntityState `json:"entity"`
	// AddTriples are applied with REPLACE-by-(subject,predicate) semantics
	// (MergeTriples — gh#244): every predicate present in AddTriples has its
	// prior values on the entity fully replaced. This is upsert, NOT
	// incremental append. For a SINGLE-valued predicate, send the one new
	// value (the old one is dropped). For a MULTI-valued predicate, send the
	// FULL desired set — a partial set drops the omitted siblings. To ADD to
	// a predicate while preserving existing values, use triple.add /
	// triple.add_batch instead. Predicates absent from AddTriples are
	// untouched.
	AddTriples    []message.Triple `json:"add_triples,omitempty"`
	RemoveTriples []string         `json:"remove_triples,omitempty"` // Predicates to delete (pure delete; runs before the AddTriples merge)
	// ExpectedRevision opts into single-pass CAS-on-condition writes.
	// Non-zero = require entity's current KV revision to match exactly;
	// zero = existing UpdateWithRetry behavior (no CAS check on caller
	// side; internal retry on conflict). Lifecycle Manager.Transition
	// uses the non-zero path per ADR-049 to preserve state-machine
	// correctness under concurrent writes.
	ExpectedRevision uint64 `json:"expected_revision,omitempty"`
	// IndexingProfile, when non-empty, is the EXPLICIT-OVERRIDE channel for an
	// entity's indexing profile (ADR-054 §5). A profile is otherwise immutable
	// after creation; setting it here replaces entity.indexing.profile
	// (RemoveTriples + AddTriples, single-valued). Empty leaves the existing
	// profile untouched. This is the only post-creation way to re-profile an
	// entity — a later triple.add by a different writer must NOT change it.
	IndexingProfile string `json:"indexing_profile,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
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
