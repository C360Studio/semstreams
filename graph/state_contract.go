package graph

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// ErrorCodeGraphStateResetRequired is the cross-component code for
// authoritative ENTITY_STATES that cannot be interpreted by the running graph
// contract.
const ErrorCodeGraphStateResetRequired = "graph_state_reset_required"

// IsStateContractError reports whether err carries the shared authoritative
// graph-reset contract, either as the in-process typed cause or as the stable
// classified code reconstructed across request/reply.
func IsStateContractError(err error) bool {
	if err == nil {
		return false
	}
	var stateErr *StateContractError
	if errors.As(err, &stateErr) {
		return true
	}
	var classified *errs.ClassifiedError
	return errors.As(err, &classified) && classified.Code == ErrorCodeGraphStateResetRequired
}

// ClassifyStateContractError applies the one cross-component failure shape for
// incompatible authoritative graph state. Non-contract errors pass through
// unchanged so callers cannot accidentally promote operational failures.
func ClassifyStateContractError(err error) error {
	if !IsStateContractError(err) {
		return err
	}
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) &&
		classified.Class == errs.ErrorFatal &&
		classified.Code == ErrorCodeGraphStateResetRequired {
		return err
	}
	return errs.ClassifiedCode(errs.ErrorFatal, ErrorCodeGraphStateResetRequired, err)
}

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
// or projected. Recovery is reader-class-conditional: the authoritative read
// surface (graph-ingest's query lanes) scopes refusal to the poisoned entity
// and recovers as soon as operators repair the stored bytes (delete +
// recreate through canonical writes) — no restart required. Projection
// owners (derived views built from watched or replayed entity state) stay in
// sticky reset-required state and recover only by operator reset and process
// restart.
//
// EntityID names the poisoned entity when the classification site knows it
// (snapshot sweep entry key, queried entity ID, RMW target, mutation read
// seam). It is additive and in-process only: the wire carries just the error
// class and code, so consumers reconstructing the classification from headers
// never see it. Err is excluded from JSON on purpose (error values do not
// marshal usefully); the rendered Error() string is the transport for detail.
type StateContractError struct {
	Reason   StateResetReason `json:"reason"`
	EntityID string           `json:"entity_id,omitempty"`
	Err      error            `json:"-"`
}

// Error implements error.
func (e *StateContractError) Error() string {
	if e == nil {
		return ErrorCodeGraphStateResetRequired
	}
	msg := fmt.Sprintf("%s: %s", ErrorCodeGraphStateResetRequired, e.Reason)
	if e.EntityID != "" {
		msg += fmt.Sprintf(": entity %s", e.EntityID)
	}
	if e.Err != nil {
		msg += fmt.Sprintf(": %v", e.Err)
	}
	return msg
}

// Unwrap exposes the structural or JSON cause.
func (e *StateContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
