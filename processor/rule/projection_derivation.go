package rule

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/pkg/projection"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

type projectionActionObligation struct {
	contract   string
	group      string
	predicate  string
	pattern    string
	unresolved bool
	location   string
}

type projectionActionList struct {
	name    string
	actions []Action
}

// deriveEffectiveProjectionContracts creates the boot-time authorization
// snapshot from the already copied initial definitions. A nil declared slice
// means the field was omitted and selects minimal derivation. A non-nil slice,
// including an empty one, is an explicit override that must structurally cover
// every action obligation.
func deriveEffectiveProjectionContracts(
	definitions []Definition,
	declared []projection.Contract,
) ([]projection.Contract, error) {
	obligations, err := collectProjectionActionObligations(definitions)
	if err != nil {
		return nil, err
	}
	if declared != nil {
		effective := cloneProjectionContracts(declared)
		if err := validateDeclaredProjectionSuperset(effective, obligations); err != nil {
			return nil, err
		}
		return effective, nil
	}
	if len(obligations) == 0 {
		return nil, nil
	}
	return buildMinimalProjectionContracts(obligations)
}

func collectProjectionActionObligations(
	definitions []Definition,
) ([]projectionActionObligation, error) {
	var obligations []projectionActionObligation
	for _, definition := range definitions {
		lists := []projectionActionList{
			{name: "on_enter", actions: definition.OnEnter},
			{name: "on_exit", actions: definition.OnExit},
			{name: "while_true", actions: definition.WhileTrue},
			{name: "on_recovery", actions: definition.OnRecovery},
			{name: "actions", actions: definition.Actions},
		}
		for _, list := range lists {
			for index, action := range list.actions {
				if action.Type != ActionTypeReconcilePredicates {
					continue
				}
				location := fmt.Sprintf("%s %s[%d]", definition.ID, list.name, index)
				if err := validateProjectionActionSelectors(action); err != nil {
					return nil, fmt.Errorf("%s: %w", location, err)
				}
				pattern, unresolved, err := inferProjectionActionPattern(definition, action)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", location, err)
				}
				obligations = append(obligations, projectionActionObligation{
					contract:   action.ProjectionContract,
					group:      action.ProjectionGroup,
					predicate:  action.Predicate,
					pattern:    pattern,
					unresolved: unresolved,
					location:   location,
				})
			}
		}
	}
	sort.Slice(obligations, func(i, j int) bool {
		left, right := obligations[i], obligations[j]
		switch {
		case left.contract != right.contract:
			return left.contract < right.contract
		case left.group != right.group:
			return left.group < right.group
		case left.predicate != right.predicate:
			return left.predicate < right.predicate
		default:
			return left.location < right.location
		}
	})
	return obligations, nil
}

func validateProjectionActionSelectors(action Action) error {
	if strings.TrimSpace(action.ProjectionContract) == "" {
		return fmt.Errorf("reconcile_predicates projection_contract is required")
	}
	if strings.TrimSpace(action.ProjectionGroup) == "" {
		return fmt.Errorf("reconcile_predicates projection_group is required")
	}
	if strings.TrimSpace(action.Predicate) == "" {
		return fmt.Errorf("reconcile_predicates predicate is required")
	}
	if strings.Contains(action.Predicate, "$") {
		return fmt.Errorf("reconcile_predicates predicate %q must be a literal", action.Predicate)
	}
	return nil
}

func inferProjectionActionPattern(
	definition Definition,
	action Action,
) (pattern string, unresolved bool, err error) {
	switch action.Subject {
	case "", "$entity.id":
		if definition.Entity.Pattern == "" {
			return "", true, nil
		}
		return definition.Entity.Pattern, false, nil
	default:
		if strings.Contains(action.Subject, "$") {
			return "", true, nil
		}
		if err := semtypes.ValidateEntityID(action.Subject); err != nil {
			return "", false, fmt.Errorf(
				"reconcile_predicates subject %q must be a canonical literal entity ID or a supported runtime template: %w",
				action.Subject,
				err,
			)
		}
		return action.Subject, false, nil
	}
}

type minimalProjectionContract struct {
	pattern         string
	patternLocation string
	groups          map[string]map[string]struct{}
}

func buildMinimalProjectionContracts(
	obligations []projectionActionObligation,
) ([]projection.Contract, error) {
	contracts := make(map[string]*minimalProjectionContract)
	var unresolved []string
	for _, obligation := range obligations {
		if obligation.unresolved {
			unresolved = append(unresolved, obligation.location)
			continue
		}
		contract := contracts[obligation.contract]
		if contract == nil {
			contract = &minimalProjectionContract{
				pattern:         obligation.pattern,
				patternLocation: obligation.location,
				groups:          make(map[string]map[string]struct{}),
			}
			contracts[obligation.contract] = contract
		} else if contract.pattern != obligation.pattern {
			return nil, fmt.Errorf(
				"projection contract %q has conflicting target patterns %q at %s and %q at %s; declare an explicit covering projection_contracts envelope or split contract names",
				obligation.contract,
				contract.pattern,
				contract.patternLocation,
				obligation.pattern,
				obligation.location,
			)
		}
		predicates := contract.groups[obligation.group]
		if predicates == nil {
			predicates = make(map[string]struct{})
			contract.groups[obligation.group] = predicates
		}
		predicates[obligation.predicate] = struct{}{}
	}
	if len(unresolved) > 0 {
		return nil, fmt.Errorf(
			"each reconcile_predicates target at %s requires an explicit projection_contracts envelope; unresolved targets are never widened to match-all",
			strings.Join(unresolved, ", "),
		)
	}

	names := make([]string, 0, len(contracts))
	for name := range contracts {
		names = append(names, name)
	}
	sort.Strings(names)
	effective := make([]projection.Contract, 0, len(names))
	for _, name := range names {
		minimal := contracts[name]
		groupNames := make([]string, 0, len(minimal.groups))
		for group := range minimal.groups {
			groupNames = append(groupNames, group)
		}
		sort.Strings(groupNames)
		groups := make([]projection.PredicateGroup, 0, len(groupNames))
		for _, groupName := range groupNames {
			predicateSet := minimal.groups[groupName]
			predicates := make([]string, 0, len(predicateSet))
			for predicate := range predicateSet {
				predicates = append(predicates, predicate)
			}
			sort.Strings(predicates)
			groups = append(groups, projection.PredicateGroup{
				Name:       groupName,
				Mode:       projection.ModeReconcile,
				Predicates: predicates,
			})
		}
		effective = append(effective, projection.Contract{
			Name:          name,
			EntityPattern: minimal.pattern,
			Groups:        groups,
		})
	}
	if err := projection.ValidateContracts(effective); err != nil {
		return nil, fmt.Errorf("validate derived projection contracts: %w", err)
	}
	return effective, nil
}

func validateDeclaredProjectionSuperset(
	declared []projection.Contract,
	obligations []projectionActionObligation,
) error {
	byName := make(map[string]projection.Contract, len(declared))
	for index, contract := range declared {
		if first, duplicate := byName[contract.Name]; duplicate {
			return fmt.Errorf(
				"duplicate projection contract %q at projection_contracts[%d] (first entity pattern %q)",
				contract.Name,
				index,
				first.EntityPattern,
			)
		}
		byName[contract.Name] = contract
	}
	if len(declared) > 0 {
		if err := projection.ValidateContracts(declared); err != nil {
			return fmt.Errorf("validate declared projection contracts: %w", err)
		}
	}
	for _, obligation := range obligations {
		contract, ok := byName[obligation.contract]
		if !ok {
			return fmt.Errorf(
				"%s: explicit projection_contracts is missing derived contract %q",
				obligation.location,
				obligation.contract,
			)
		}
		var matchingGroup *projection.PredicateGroup
		for index := range contract.Groups {
			if contract.Groups[index].Name == obligation.group {
				matchingGroup = &contract.Groups[index]
				break
			}
		}
		if matchingGroup == nil {
			return fmt.Errorf(
				"%s: declared contract %q does not cover projection group %q",
				obligation.location,
				obligation.contract,
				obligation.group,
			)
		}
		if matchingGroup.Mode != projection.ModeReconcile {
			return fmt.Errorf(
				"%s: declared contract %q group %q uses mode %q, want %q",
				obligation.location,
				obligation.contract,
				obligation.group,
				matchingGroup.Mode,
				projection.ModeReconcile,
			)
		}
		if !containsString(matchingGroup.Predicates, obligation.predicate) {
			return fmt.Errorf(
				"%s: declared contract %q group %q does not cover predicate %q",
				obligation.location,
				obligation.contract,
				obligation.group,
				obligation.predicate,
			)
		}
		if obligation.unresolved {
			continue
		}
		contains, err := projectionPatternContains(contract.EntityPattern, obligation.pattern)
		if err != nil {
			return fmt.Errorf("%s: compare projection entity patterns: %w", obligation.location, err)
		}
		if !contains {
			return fmt.Errorf(
				"%s: declared entity pattern %q does not contain derived target pattern %q",
				obligation.location,
				contract.EntityPattern,
				obligation.pattern,
			)
		}
	}
	return nil
}

func projectionPatternContains(declared, derived string) (bool, error) {
	if err := semtypes.ValidateEntityIDPattern(declared); err != nil {
		return false, fmt.Errorf("invalid declared pattern %q: %w", declared, err)
	}
	if err := semtypes.ValidateEntityIDPattern(derived); err != nil {
		return false, fmt.Errorf("invalid derived pattern %q: %w", derived, err)
	}
	declaredParts := strings.Split(declared, ".")
	derivedParts := strings.Split(derived, ".")
	for index := range declaredParts {
		if declaredParts[index] != "*" && declaredParts[index] != derivedParts[index] {
			return false, nil
		}
	}
	return true, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
