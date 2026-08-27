package projection

import "github.com/c360studio/semstreams/pkg/projection/contract"

// The contract data types live in the leaf package pkg/projection/contract so
// the payload registry can hold a registered type's contracts (ADR-103). These
// aliases keep every existing literal — projection.Contract{...},
// projection.ModeReconcile — compiling unchanged.

// WriteMode states whether a predicate group is replaced as a complete set or
// appended as evidence. It is operation intent, not semantic ownership.
type WriteMode = contract.WriteMode

const (
	// ModeReconcile declares complete-set predicate reconciliation.
	ModeReconcile = contract.ModeReconcile
	// ModeAppend declares set-valued predicate addition.
	ModeAppend = contract.ModeAppend
)

// PredicateGroup names predicates changed together through one operation.
type PredicateGroup = contract.PredicateGroup

// Contract declares the graph shape emitted by one projection. It validates
// caller intent; it does not reserve predicates or prevent other writers.
type Contract = contract.Contract

// ErrInvalidContract identifies a projection contract rejected before use.
var ErrInvalidContract = contract.ErrInvalidContract

// ValidateContracts validates a complete, uniquely named contract set.
func ValidateContracts(contracts []Contract) error {
	return contract.ValidateContracts(contracts)
}
