package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// EntityStateContractField identifies the first identity-bearing field that
// makes a complete EntityState candidate noncanonical.
type EntityStateContractField string

const (
	// EntityStateContractFieldID is the EntityState.ID root key.
	EntityStateContractFieldID EntityStateContractField = "id"
	// EntityStateContractFieldSubject is a persisted Triple.Subject.
	EntityStateContractFieldSubject EntityStateContractField = "subject"
	// EntityStateContractFieldReference is an explicitly marked @id object.
	EntityStateContractFieldReference EntityStateContractField = "reference"
)

var errEntityReferenceObjectType = errors.New("explicit entity reference object must be a string")

// EntityStateContractError reports the first noncanonical identity-bearing
// field in a complete EntityState. TripleIndex is -1 for the root ID.
//
// Precedence is root ID, subjects in slice order, explicit references in slice
// order, then the existing deterministic predicate contract. This keeps error
// classification stable without echoing rejected identity bytes.
type EntityStateContractError struct {
	Field       EntityStateContractField
	TripleIndex int
	Err         error
}

// Error implements error without exposing the rejected identity.
func (e *EntityStateContractError) Error() string {
	if e == nil {
		return "entity state contract violation"
	}
	if e.TripleIndex < 0 {
		return fmt.Sprintf("entity state contract violation: %s", e.Field)
	}
	return fmt.Sprintf("entity state contract violation: triple[%d] %s", e.TripleIndex, e.Field)
}

// Unwrap exposes the canonical entity-ID or object-type cause.
func (e *EntityStateContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

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
			violation, classificationErr := predicateViolationFromError(triple.Predicate, err)
			if classificationErr != nil {
				return classificationErr
			}
			unique[violation] = struct{}{}
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

func predicateViolationFromError(predicate string, err error) (InvalidEntityPredicate, error) {
	var validationErr *vocabulary.PredicateValidationError
	if !errors.As(err, &validationErr) {
		return InvalidEntityPredicate{}, fmt.Errorf("predicate validator returned unexpected error: %w", err)
	}
	return InvalidEntityPredicate{Predicate: predicate, Reason: validationErr.Reason}, nil
}

// ValidateEntityStateContract validates one complete final EntityState
// candidate. It never fills or rewrites fields; the Graphable fact lane owns
// its one allowed empty-subject projection convenience before this seam.
func ValidateEntityStateContract(entity *EntityState) error {
	if entity == nil {
		return &EntityStateContractError{
			Field: EntityStateContractFieldID, TripleIndex: -1, Err: errs.ErrInvalidData,
		}
	}
	if err := semtypes.ValidateEntityID(entity.ID); err != nil {
		return &EntityStateContractError{
			Field: EntityStateContractFieldID, TripleIndex: -1, Err: err,
		}
	}
	for index := range entity.Triples {
		if err := semtypes.ValidateEntityID(entity.Triples[index].Subject); err != nil {
			return &EntityStateContractError{
				Field: EntityStateContractFieldSubject, TripleIndex: index, Err: err,
			}
		}
	}
	for index := range entity.Triples {
		triple := &entity.Triples[index]
		if triple.Datatype != message.EntityReferenceDatatype {
			continue
		}
		object, ok := triple.Object.(string)
		if !ok {
			return &EntityStateContractError{
				Field: EntityStateContractFieldReference, TripleIndex: index, Err: errEntityReferenceObjectType,
			}
		}
		if err := semtypes.ValidateEntityID(object); err != nil {
			return &EntityStateContractError{
				Field: EntityStateContractFieldReference, TripleIndex: index, Err: err,
			}
		}
	}
	return ValidateEntityPredicates(entity)
}

// MarshalEntityState is the authoritative in-process ENTITY_STATES persistence
// seam. It validates the complete final candidate before serialization.
func MarshalEntityState(entity *EntityState) ([]byte, error) {
	if err := ValidateEntityStateContract(entity); err != nil {
		return nil, errs.WrapInvalid(err, "graph", "MarshalEntityState", "validate entity state contract")
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
	if err := ValidateEntityStateContract(entity); err != nil {
		reason := GraphStateReasonNoncanonicalEntityID
		var predicateErr *EntityPredicateContractError
		if errors.As(err, &predicateErr) {
			reason = GraphStateReasonNoncanonicalPredicate
		}
		return errs.WrapFatal(&StateContractError{
			Reason: reason,
			Err:    err,
		}, "graph", "UnmarshalEntityState", "decode authoritative entity state")
	}
	return nil
}
