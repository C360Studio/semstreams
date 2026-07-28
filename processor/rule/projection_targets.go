package rule

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
)

// projectionTarget is the immutable rule-facing view of one exact
// contract/group/predicate replacement target.
type projectionTarget struct {
	Contract   string
	Group      string
	Predicate  string
	Predicates []string
}

// projectionTargetIndex is built once from copied boot-time contracts. Its
// maps and slices are never exposed directly.
type projectionTargetIndex struct {
	contracts map[string]map[string]projectionTargetGroup
}

type projectionTargetGroup struct {
	mode       ownership.WriteMode
	predicates map[string]struct{}
	ordered    []string
}

func buildProjectionTargetIndex(contracts []projection.Contract) (*projectionTargetIndex, error) {
	index := &projectionTargetIndex{
		contracts: make(map[string]map[string]projectionTargetGroup, len(contracts)),
	}
	if len(contracts) == 0 {
		return index, nil
	}
	copies := cloneProjectionContracts(contracts)
	if _, err := projection.Derive("rule-pack-preflight", copies...); err != nil {
		return nil, fmt.Errorf("validate projection contracts: %w", err)
	}
	for _, contract := range copies {
		groups := make(map[string]projectionTargetGroup, len(contract.Groups))
		for _, group := range contract.Groups {
			if group.Name == "" {
				continue
			}
			predicates := make(map[string]struct{}, len(group.Predicates))
			ordered := append([]string(nil), group.Predicates...)
			sort.Strings(ordered)
			for _, predicate := range ordered {
				predicates[predicate] = struct{}{}
			}
			groups[group.Name] = projectionTargetGroup{
				mode:       group.Mode,
				predicates: predicates,
				ordered:    ordered,
			}
		}
		index.contracts[contract.Name] = groups
	}
	return index, nil
}

func cloneProjectionContracts(contracts []projection.Contract) []projection.Contract {
	if contracts == nil {
		return nil
	}
	copies := make([]projection.Contract, len(contracts))
	for i, contract := range contracts {
		copies[i] = contract
		copies[i].BirthPredicates = append([]string(nil), contract.BirthPredicates...)
		copies[i].ForeignEdges = append([]projection.ForeignEdge(nil), contract.ForeignEdges...)
		copies[i].Groups = make([]projection.PredicateGroup, len(contract.Groups))
		for j, group := range contract.Groups {
			copies[i].Groups[j] = group
			copies[i].Groups[j].Predicates = append([]string(nil), group.Predicates...)
		}
	}
	return copies
}

func (index *projectionTargetIndex) resolve(contract, group, predicate string) (projectionTarget, error) {
	if strings.TrimSpace(contract) == "" {
		return projectionTarget{}, fmt.Errorf("projection_contract is required")
	}
	if strings.TrimSpace(group) == "" {
		return projectionTarget{}, fmt.Errorf("projection_group is required")
	}
	if strings.TrimSpace(predicate) == "" {
		return projectionTarget{}, fmt.Errorf("predicate is required")
	}
	if strings.Contains(predicate, "$") {
		return projectionTarget{}, fmt.Errorf("predicate %q must be a literal", predicate)
	}
	if index == nil {
		return projectionTarget{}, fmt.Errorf("projection target index is not initialized")
	}
	groups, ok := index.contracts[contract]
	if !ok {
		return projectionTarget{}, fmt.Errorf("unknown projection contract %q", contract)
	}
	targetGroup, ok := groups[group]
	if !ok {
		return projectionTarget{}, fmt.Errorf(
			"projection contract %q has no named projection group %q",
			contract,
			group,
		)
	}
	if targetGroup.mode != ownership.ModeReplaceOwned {
		return projectionTarget{}, fmt.Errorf(
			"projection group %q in contract %q uses mode %q, not replace-owned",
			group,
			contract,
			targetGroup.mode,
		)
	}
	if _, ok := targetGroup.predicates[predicate]; !ok {
		return projectionTarget{}, fmt.Errorf(
			"predicate %q is outside projection contract %q group %q",
			predicate,
			contract,
			group,
		)
	}
	return projectionTarget{
		Contract:   contract,
		Group:      group,
		Predicate:  predicate,
		Predicates: append([]string(nil), targetGroup.ordered...),
	}, nil
}
