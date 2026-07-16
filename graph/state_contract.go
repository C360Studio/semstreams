package graph

import (
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// ErrorCodeGraphStateResetRequired is the cross-component code for
// authoritative ENTITY_STATES that cannot be interpreted by the running graph
// contract.
const ErrorCodeGraphStateResetRequired = "graph_state_reset_required"

// IsKVTombstone reports whether an authoritative KV watch entry removes the
// current value. NATS emits both DEL and PURGE tombstones with empty payloads;
// neither is an entity document and both must drive identical cleanup paths.
func IsKVTombstone(operation jetstream.KeyValueOp) bool {
	return operation == jetstream.KeyValueDelete || operation == jetstream.KeyValuePurge
}

// StateResetReason is a bounded machine-readable reset cause.
type StateResetReason string

const (
	// GraphStateReasonUnreadableEntity means authoritative JSON cannot decode.
	GraphStateReasonUnreadableEntity StateResetReason = "unreadable_entity_state"
	// GraphStateReasonNoncanonicalPredicate means stored state violates the predicate grammar.
	GraphStateReasonNoncanonicalPredicate StateResetReason = "noncanonical_predicate"
	// GraphStateReasonNoncanonicalEntityID means the stored entity ID, a triple
	// subject, or an explicitly marked entity reference violates the canonical
	// entity identity contract.
	GraphStateReasonNoncanonicalEntityID StateResetReason = "noncanonical_entity_id"
)

// StateContractError means the authoritative graph cannot be safely read
// or projected. It is permanent until operators reset incompatible graph/index
// buckets, reingest canonical sources, and restart the process.
type StateContractError struct {
	Reason StateResetReason
	Err    error
}

// Error implements error.
func (e *StateContractError) Error() string {
	if e == nil {
		return ErrorCodeGraphStateResetRequired
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", ErrorCodeGraphStateResetRequired, e.Reason)
	}
	return fmt.Sprintf("%s: %s: %v", ErrorCodeGraphStateResetRequired, e.Reason, e.Err)
}

// Unwrap exposes the structural or JSON cause.
func (e *StateContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
