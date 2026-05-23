// Package graph provides request/response types for the NATS mutation and query APIs.
// See mutation_requests.go for the full package documentation.
package graph

import (
	"time"

	"github.com/c360studio/semstreams/message"
)

// Mutation Response Types

// MutationResponse is the base response for all mutations.
//
// Three response shapes for entity-mutation handlers (#120):
//
//   - Success=true, Degraded=false → write committed, payload fully
//     populated (Entity, KVRevision, Version, TriplesAdded all
//     authoritative). Callers can rely on the response as the
//     post-write source of truth.
//
//   - Success=true, Degraded=true → write committed durably, but the
//     post-write read-back failed (context cancellation, JetStream
//     node failover, bucket re-keyed). Entity may be nil and
//     KVRevision may be 0; Error carries the read-back failure
//     reason. **Callers MUST NOT retry** — a retry on create returns
//     409 Conflict (the entity is there), and on update returns CAS
//     mismatch (the revision moved). Either fetch the entity through
//     a separate read path to recover the post-write state, or
//     accept the write-without-echo and continue.
//
//     Gateways translating this state to HTTP SHOULD return 200 OK
//     with the Degraded flag echoed in the response body; do NOT
//     return 202 Accepted — the write is COMMITTED, not pending.
//
//   - Success=false → write did NOT commit. Error carries the
//     pre-write failure reason. Callers MAY retry per the Error
//     semantics (transient transport errors are safe to retry;
//     validation errors are not).
//
// Triple-mutation handlers (AddTriple/RemoveTriple/AddTriplesBatch)
// don't use Degraded today — they have no post-write read-back step.
// The field stays nil for those paths.
type MutationResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	// Degraded is true when the write committed but the post-write
	// read-back failed. See type docstring for the full three-state
	// contract. Only entity-mutation handlers populate this; nil on
	// triple-mutation responses. Omitted in JSON when false (the
	// common case).
	Degraded   bool   `json:"degraded,omitempty"`
	TraceID    string `json:"trace_id,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Timestamp  int64  `json:"timestamp"`             // Unix nano timestamp
	KVRevision uint64 `json:"kv_revision,omitempty"` // KV bucket revision after write
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

// CreateEntityWithTriplesResponse response for atomic entity+triples creation
type CreateEntityWithTriplesResponse struct {
	MutationResponse
	Entity       *EntityState `json:"entity,omitempty"`
	TriplesAdded int          `json:"triples_added"`
}

// UpdateEntityWithTriplesResponse response for atomic entity+triples update
type UpdateEntityWithTriplesResponse struct {
	MutationResponse
	Entity         *EntityState `json:"entity,omitempty"`
	TriplesAdded   int          `json:"triples_added"`
	TriplesRemoved int          `json:"triples_removed"`
	Version        int64        `json:"version,omitempty"`
}

// AddTripleResponse response for triple addition
type AddTripleResponse struct {
	MutationResponse
	Triple *message.Triple `json:"triple,omitempty"`
}

// AddTriplesBatchResponse response for batched triple addition.
// Success is true iff all entities in the batch were updated. On
// partial failure, FailedSubjects names the entity IDs that did not
// commit, mapped to their error messages, so the caller can decide
// whether to retry the failed subset, fall back to per-triple
// AddTriple, or surface to the user. WrittenCount is the number of
// triples that successfully committed across all entities.
//
// Discriminating rejection phases on Success=false:
//
//   - FailedSubjects empty/nil + WrittenCount=0 → pre-CAS validation
//     rejected the whole batch (e.g. empty Subject/Predicate, malformed
//     envelope). Nothing committed; safe to retry after fixing the
//     input.
//   - FailedSubjects non-empty → CAS phase reached. Per-entity
//     atomicity means subjects NOT in FailedSubjects DID commit (their
//     triples are durable); subjects in FailedSubjects rolled back. The
//     caller can retry just the failed subset.
//   - Success=true, FailedSubjects empty, WrittenCount>0 → all
//     entities committed.
type AddTriplesBatchResponse struct {
	MutationResponse
	WrittenCount   int               `json:"written_count"`
	FailedSubjects map[string]string `json:"failed_subjects,omitempty"`
}

// RemoveTripleResponse response for triple removal
type RemoveTripleResponse struct {
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
