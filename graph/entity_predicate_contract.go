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
		// A subject byte-equal to the root ID is already proven canonical by
		// the root check above — skip the re-validation. ENTITY_STATES
		// candidates are single-subject in the overwhelming case
		// (normalizeProjection splits foreign subjects off before commit), so
		// this removes an O(triples) ValidateEntityID pass — with a
		// strings.Split allocation per triple — from the MarshalEntityState
		// write gate on the per-key-serialized RMW hot path (gh#562
		// write-cost). Strictly verdict-preserving: equality to a validated ID
		// implies validity, and any differing subject (valid or not) takes the
		// full check with unchanged error precedence and TripleIndex.
		subject := entity.Triples[index].Subject
		if subject == entity.ID {
			continue
		}
		if err := semtypes.ValidateEntityID(subject); err != nil {
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

// ValidateDecodedEntityState applies the authoritative graph-state failure
// contract to an EntityState that was decoded as part of a larger query reply.
// Aggregate decoders must call this before exposing any candidate downstream.
func ValidateDecodedEntityState(entity *EntityState) error {
	if err := ValidateEntityStateContract(entity); err != nil {
		return classifyDecodedEntityStateError(err)
	}
	return nil
}

// ValidateDecodedEntityStates validates an entire value collection before its
// caller performs any downstream work. The indexed wrapper preserves the
// contract error for errors.As while identifying which candidate poisoned the
// reply.
func ValidateDecodedEntityStates(entities []EntityState) error {
	for index := range entities {
		if err := ValidateDecodedEntityState(&entities[index]); err != nil {
			return fmt.Errorf("entity[%d]: %w", index, err)
		}
	}
	return nil
}

// ValidateDecodedEntityStatePointers is the pointer-slice counterpart used by
// GraphRAG hydration replies. Nil entries are contract violations, not absence:
// the batch protocol represents absence by omitting an entity.
func ValidateDecodedEntityStatePointers(entities []*EntityState) error {
	for index, entity := range entities {
		if err := ValidateDecodedEntityState(entity); err != nil {
			return fmt.Errorf("entity[%d]: %w", index, err)
		}
	}
	return nil
}

// ValidateDecodedEntityIDs validates identity-only authoritative query replies
// (semantic hits, name matches, and relationship endpoints) as one unit.
func ValidateDecodedEntityIDs(entityIDs []string) error {
	for index, entityID := range entityIDs {
		if err := ValidateDecodedEntityState(&EntityState{ID: entityID}); err != nil {
			return fmt.Errorf("entity_id[%d]: %w", index, err)
		}
	}
	return nil
}

func classifyDecodedEntityStateError(err error) error {
	reason := GraphStateReasonNoncanonicalEntityID
	var predicateErr *EntityPredicateContractError
	if errors.As(err, &predicateErr) {
		reason = GraphStateReasonNoncanonicalPredicate
	}
	return ClassifyStateContractError(errs.WrapFatal(&StateContractError{
		Reason: reason,
		Err:    err,
	}, "graph", "ValidateDecodedEntityState", "validate authoritative entity state"))
}

// UnmarshalEntityState is the authoritative decoder for ENTITY_STATES and
// graph-view consumers. It refuses unreadable or noncanonical stored state.
func UnmarshalEntityState(data []byte, entity *EntityState) error {
	if err := json.Unmarshal(data, entity); err != nil {
		return ClassifyStateContractError(errs.WrapFatal(&StateContractError{
			Reason: GraphStateReasonUnreadableEntity,
			Err:    err,
		}, "graph", "UnmarshalEntityState", "decode authoritative entity state"))
	}
	return ValidateDecodedEntityState(entity)
}

// UnmarshalEntityStateTrusted decodes stored ENTITY_STATES bytes WITHOUT
// re-validating the canonical entity-state contract (gh#562).
//
// It exists for exactly one caller class: the ENTITY_STATES OWNER's own
// read-modify-write reads — graph-ingest's CAS merge and mutation closures.
// Those bytes were validated by MarshalEntityState when they were written,
// and every RMW cycle either commits a candidate re-validated by
// MarshalEntityState or commits nothing at all (no-op cycles exit via
// sentinel before any KV write). Resident noncanonical state therefore
// cannot launder through a merge: it survives into the merged candidate and
// the write gate rejects the whole write. Read-side validation on that lane
// is redundant for enforcement — it only changes WHERE the failure is
// reported — while sitting on the per-key-serialized RMW critical path
// (ADR-072), where it measurably lowers the ingest ceiling.
//
// Every other reader MUST keep UnmarshalEntityState: external and
// authoritative readers (query handlers, graph-view consumers, index
// builders, lifecycle manager), poison-detection paths (the ENTITY_STATES
// contract guard), and any surface exposing decoded state downstream. This
// decoder does NOT reject noncanonical predicates or identities — that is its
// contract, not a gap. Malformed JSON returns a plain decode error, not the
// graph-state-reset classification.
func UnmarshalEntityStateTrusted(data []byte, entity *EntityState) error {
	if err := json.Unmarshal(data, entity); err != nil {
		return fmt.Errorf("decode entity state: %w", err)
	}
	return nil
}
