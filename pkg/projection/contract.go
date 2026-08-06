package projection

import (
	"fmt"
	"strings"
	"unicode"

	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
)

var validIndexingProfiles = map[string]struct{}{
	"content": {}, "control": {}, "signal": {}, "trace": {},
}

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
type Contract struct {
	Name            string           `json:"name"`
	MessageType     string           `json:"message_type,omitempty"`
	EntityPattern   string           `json:"entity_pattern"`
	Groups          []PredicateGroup `json:"groups,omitempty"`
	BirthPredicates []string         `json:"birth_predicates,omitempty"`
	IndexingProfile string           `json:"indexing_profile,omitempty"`
}

// Validate checks one projection contract without consulting runtime state.
func (c Contract) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("%w: contract has no name", ErrInvalidContract)
	}
	if err := semtypes.ValidateEntityIDPattern(c.EntityPattern); err != nil {
		return fmt.Errorf("%w: contract %q entity pattern: %w", ErrInvalidContract, c.Name, err)
	}
	if len(c.Groups) == 0 && len(c.BirthPredicates) == 0 {
		return fmt.Errorf("%w: contract %q declares no predicates", ErrInvalidContract, c.Name)
	}
	if c.IndexingProfile != "" {
		if _, ok := validIndexingProfiles[c.IndexingProfile]; !ok {
			return fmt.Errorf("%w: contract %q has invalid indexing profile %q", ErrInvalidContract, c.Name, c.IndexingProfile)
		}
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
			if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
				return fmt.Errorf("%w: contract %q predicate: %w", ErrInvalidContract, c.Name, err)
			}
			if previous, duplicate := seenPredicates[predicate]; duplicate {
				return fmt.Errorf("%w: contract %q predicate %q repeats in %s", ErrInvalidContract, c.Name, predicate, previous)
			}
			seenPredicates[predicate] = "group " + group.Name
		}
	}
	for _, predicate := range c.BirthPredicates {
		if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
			return fmt.Errorf("%w: contract %q birth predicate: %w", ErrInvalidContract, c.Name, err)
		}
		if previous, duplicate := seenPredicates[predicate]; duplicate {
			return fmt.Errorf("%w: contract %q birth predicate %q repeats in %s", ErrInvalidContract, c.Name, predicate, previous)
		}
		seenPredicates[predicate] = "birth predicates"
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
