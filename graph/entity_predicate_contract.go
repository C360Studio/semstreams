package graph

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

// InvalidEntityPredicate identifies one unique noncanonical predicate in an
// EntityState candidate. Predicate values are included for diagnostics; Reason
// is bounded and safe for metric labels.
type InvalidEntityPredicate struct {
	Predicate string                               `json:"predicate"`
	Reason    vocabulary.PredicateValidationReason `json:"reason"`
}

// EntityPredicateContractError reports every unique invalid predicate in one
// candidate. Writers must reject the whole candidate when this error is
// returned.
type EntityPredicateContractError struct {
	Violations []InvalidEntityPredicate `json:"violations"`
}

// Error implements error with deterministic ordering for logs and tests.
func (e *EntityPredicateContractError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return "entity predicate contract violation"
	}
	parts := make([]string, 0, len(e.Violations))
	for _, violation := range e.Violations {
		parts = append(parts, fmt.Sprintf("%q (%s)", violation.Predicate, violation.Reason))
	}
	return "entity predicate contract violation: " + strings.Join(parts, ", ")
}

// ValidateEntityPredicates validates the complete final EntityState candidate.
// It returns all unique predicate/reason pairs, sorted deterministically.
func ValidateEntityPredicates(entity *EntityState) error {
	if entity == nil {
		return nil
	}

	unique := make(map[InvalidEntityPredicate]struct{})
	for _, triple := range entity.Triples {
		if _, err := vocabulary.ParsePredicate(triple.Predicate); err != nil {
			validationErr, ok := err.(*vocabulary.PredicateValidationError)
			if !ok {
				continue
			}
			unique[InvalidEntityPredicate{
				Predicate: triple.Predicate,
				Reason:    validationErr.Reason,
			}] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return nil
	}

	violations := make([]InvalidEntityPredicate, 0, len(unique))
	for violation := range unique {
		violations = append(violations, violation)
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Predicate == violations[j].Predicate {
			return violations[i].Reason < violations[j].Reason
		}
		return violations[i].Predicate < violations[j].Predicate
	})
	return &EntityPredicateContractError{Violations: violations}
}

// MarshalEntityState is the authoritative in-process ENTITY_STATES persistence
// seam. It validates the complete final candidate before serialization.
func MarshalEntityState(entity *EntityState) ([]byte, error) {
	if err := ValidateEntityPredicates(entity); err != nil {
		return nil, errs.WrapInvalid(err, "graph", "MarshalEntityState", "validate predicate contract")
	}
	return json.Marshal(entity)
}

// UnmarshalEntityState is the authoritative decoder for ENTITY_STATES and
// graph-view consumers. It refuses unreadable or noncanonical stored state.
func UnmarshalEntityState(data []byte, entity *EntityState) error {
	if err := json.Unmarshal(data, entity); err != nil {
		return errs.WrapFatal(&StateContractError{
			Reason: GraphStateReasonUnreadableEntity,
			Err:    err,
		}, "graph", "UnmarshalEntityState", "decode authoritative entity state")
	}
	if err := ValidateEntityPredicates(entity); err != nil {
		return errs.WrapFatal(&StateContractError{
			Reason: GraphStateReasonNoncanonicalPredicate,
			Err:    err,
		}, "graph", "UnmarshalEntityState", "decode authoritative entity state")
	}
	return nil
}
