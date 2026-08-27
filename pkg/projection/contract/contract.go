// Package contract holds the projection contract data types and their shape
// validation (ADR-103). It is a leaf — it imports only pkg/types and
// vocabulary — so the payload registry can hold a registered type's contracts
// without importing pkg/projection, which reaches graph, message, and
// natsclient. pkg/projection re-exports every name here as an alias, so a
// contract literal written against pkg/projection compiles unchanged.
package contract

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

// ErrInvalidContract identifies a projection contract rejected before use.
var ErrInvalidContract = errors.New("projection: invalid contract")

// WriteMode states whether a predicate group is replaced as a complete set or
// appended as evidence. It is operation intent, not semantic ownership.
type WriteMode string

const (
	// ModeReconcile declares complete-set predicate reconciliation.
	ModeReconcile WriteMode = "reconcile"
	// ModeAppend declares set-valued predicate addition.
	ModeAppend WriteMode = "append"
)

// PredicateGroup names predicates changed together through one operation.
type PredicateGroup struct {
	Name       string    `json:"name"`
	Mode       WriteMode `json:"mode"`
	Predicates []string  `json:"predicates"`
}

// Contract declares the graph shape emitted by one projection. It validates
// caller intent; it does not reserve predicates or prevent other writers.
//
// MessageType is the registered payload type the contract is bound to,
// carried structured (on the wire as {"domain","category","version"}, the
// same shape EntityState.message_type has) so nothing parses a key. A
// contract registered with a payload type inherits that type when the field
// is zero; a contract naming another type is a registration error.
type Contract struct {
	Name            string           `json:"name"`
	MessageType     semtypes.Type    `json:"message_type"`
	EntityPattern   string           `json:"entity_pattern"`
	Groups          []PredicateGroup `json:"groups,omitempty"`
	BirthPredicates []string         `json:"birth_predicates,omitempty"`
	IndexingProfile string           `json:"indexing_profile,omitempty"`
}

// Validate checks one projection contract without consulting runtime state.
// Every predicate must be declared in the vocabulary.
func (c Contract) Validate() error {
	return c.validate(true)
}

// ValidateShape checks everything Validate does except predicate declaration
// (name, entity pattern, groups, birth predicates, profile). The payload
// registry uses it so a contract can be registered before the vocabulary is
// populated; predicate declaration stays at mutation-client construction.
func (c Contract) ValidateShape() error {
	return c.validate(false)
}

func (c Contract) validate(requireDeclared bool) error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("%w: contract has no name", ErrInvalidContract)
	}
	if err := semtypes.ValidateEntityIDPattern(c.EntityPattern); err != nil {
		return fmt.Errorf("%w: contract %q entity pattern: %w", ErrInvalidContract, c.Name, err)
	}
	if len(c.Groups) == 0 && len(c.BirthPredicates) == 0 {
		return fmt.Errorf("%w: contract %q declares no predicates", ErrInvalidContract, c.Name)
	}
	if c.MessageType != (semtypes.Type{}) {
		if err := c.MessageType.Validate(); err != nil {
			return fmt.Errorf("%w: contract %q message type: %w", ErrInvalidContract, c.Name, err)
		}
	}
	if c.IndexingProfile != "" && !vocabulary.IsValidIndexingProfile(c.IndexingProfile) {
		return fmt.Errorf("%w: contract %q has invalid indexing profile %q", ErrInvalidContract, c.Name, c.IndexingProfile)
	}

	seenPredicates := make(map[string]string)
	seenGroups := make(map[string]struct{})
	for _, group := range c.Groups {
		if err := validateGroupName(group.Name); err != nil {
			return fmt.Errorf("%w: contract %q: %w", ErrInvalidContract, c.Name, err)
		}
		if _, duplicate := seenGroups[group.Name]; duplicate {
			return fmt.Errorf("%w: contract %q repeats group %q", ErrInvalidContract, c.Name, group.Name)
		}
		seenGroups[group.Name] = struct{}{}
		if group.Mode != ModeReconcile && group.Mode != ModeAppend {
			return fmt.Errorf("%w: contract %q group %q has invalid mode %q", ErrInvalidContract, c.Name, group.Name, group.Mode)
		}
		if len(group.Predicates) == 0 {
			return fmt.Errorf("%w: contract %q group %q has no predicates", ErrInvalidContract, c.Name, group.Name)
		}
		for _, predicate := range group.Predicates {
			if err := checkPredicate(predicate, requireDeclared); err != nil {
				return fmt.Errorf("%w: contract %q predicate: %w", ErrInvalidContract, c.Name, err)
			}
			if previous, duplicate := seenPredicates[predicate]; duplicate {
				return fmt.Errorf("%w: contract %q predicate %q repeats in %s", ErrInvalidContract, c.Name, predicate, previous)
			}
			seenPredicates[predicate] = "group " + group.Name
		}
	}
	for _, predicate := range c.BirthPredicates {
		if err := checkPredicate(predicate, requireDeclared); err != nil {
			return fmt.Errorf("%w: contract %q birth predicate: %w", ErrInvalidContract, c.Name, err)
		}
		if previous, duplicate := seenPredicates[predicate]; duplicate {
			return fmt.Errorf("%w: contract %q birth predicate %q repeats in %s", ErrInvalidContract, c.Name, predicate, previous)
		}
		seenPredicates[predicate] = "birth predicates"
	}
	return nil
}

// checkPredicate requires declaration when asked; otherwise it requires only
// the structural three-part shape.
func checkPredicate(predicate string, requireDeclared bool) error {
	if requireDeclared {
		return vocabulary.RequireDeclaredPredicate(predicate)
	}
	if !vocabulary.IsValidPredicate(predicate) {
		return fmt.Errorf("predicate %q is not a valid 3-part predicate", predicate)
	}
	return nil
}

// ValidateContracts validates a complete, uniquely named contract set.
func ValidateContracts(contracts []Contract) error {
	if len(contracts) == 0 {
		return fmt.Errorf("%w: no contracts", ErrInvalidContract)
	}
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if _, duplicate := seen[contract.Name]; duplicate {
			return fmt.Errorf("%w: duplicate contract %q", ErrInvalidContract, contract.Name)
		}
		seen[contract.Name] = struct{}{}
		if err := contract.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateGroupName(name string) error {
	if name == "" || strings.ContainsAny(name, ".*>") || strings.IndexFunc(name, unicode.IsSpace) >= 0 {
		return fmt.Errorf("predicate group name %q must be one nonempty subject-safe token", name)
	}
	return nil
}
