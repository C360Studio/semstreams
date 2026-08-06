package service

import (
	"fmt"

	"github.com/c360studio/semstreams/pkg/projection"
	rulepackcontract "github.com/c360studio/semstreams/pkg/rulepack"
)

// ProjectionBinder is implemented by rule processors that use reconciled
// projection writes. Contracts are validated before any client is injected.
type ProjectionBinder interface {
	ProjectionBindings() (packID string, contracts []projection.Contract)
	PreflightProjectionMutations() error
	SetPredicateReconciler(projection.PredicateReconciler) error
}

// ProjectionBinders returns enabled components that require projection clients.
func (m *Manager) ProjectionBinders() []ProjectionBinder {
	cmService, exists := m.services["component-manager"]
	if !exists {
		return nil
	}
	cm, ok := cmService.(*ComponentManager)
	if !ok {
		return nil
	}
	var result []ProjectionBinder
	for _, managed := range cm.GetManagedComponents() {
		if binder, ok := managed.Component.(ProjectionBinder); ok {
			result = append(result, binder)
		}
	}
	return result
}

type rulePackMutationPlan struct {
	binder    ProjectionBinder
	packID    string
	contracts []projection.Contract
}

// ConfigureRulePackMutations validates every enabled pack before injecting one
// canonical mutation client per contract-bearing processor.
func ConfigureRulePackMutations(manager *Manager) error {
	if manager == nil {
		return fmt.Errorf("rule-pack mutation composition requires a service manager")
	}
	plans, err := preflightRulePackMutations(manager)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if len(plan.contracts) == 0 {
			continue
		}
		client, err := projection.NewMutationClient(projection.MutationClientConfig{
			NATS: manager.natsClient, Contracts: plan.contracts,
		})
		if err != nil {
			return fmt.Errorf("build mutation client for rule pack %q: %w", plan.packID, err)
		}
		if err := plan.binder.SetPredicateReconciler(client); err != nil {
			return fmt.Errorf("inject predicate reconciler for rule pack %q: %w", plan.packID, err)
		}
	}
	return nil
}

func preflightRulePackMutations(manager *Manager) ([]rulePackMutationPlan, error) {
	binders := manager.ProjectionBinders()
	plans := make([]rulePackMutationPlan, 0, len(binders))
	seen := make(map[string]struct{}, len(binders))
	for index, binder := range binders {
		if err := binder.PreflightProjectionMutations(); err != nil {
			return nil, fmt.Errorf("preflight enabled rule processor %d: %w", index, err)
		}
	}
	for _, binder := range binders {
		packID, contracts := binder.ProjectionBindings()
		if err := rulepackcontract.ValidateID(packID); err != nil {
			return nil, fmt.Errorf("enabled rule processor has invalid pack_id: %w", err)
		}
		if _, duplicate := seen[packID]; duplicate {
			return nil, fmt.Errorf("duplicate enabled rule pack_id %q in one composition", packID)
		}
		seen[packID] = struct{}{}
		copies := cloneRulePackContracts(contracts)
		if len(copies) > 0 {
			if manager.natsClient == nil {
				return nil, fmt.Errorf("rule pack %q mutation client requires NATS", packID)
			}
			if err := projection.ValidateContracts(copies); err != nil {
				return nil, fmt.Errorf("validate contracts for rule pack %q: %w", packID, err)
			}
		}
		plans = append(plans, rulePackMutationPlan{binder: binder, packID: packID, contracts: copies})
	}
	return plans, nil
}

func cloneRulePackContracts(contracts []projection.Contract) []projection.Contract {
	if contracts == nil {
		return nil
	}
	copies := make([]projection.Contract, len(contracts))
	for i, contract := range contracts {
		copies[i] = contract
		copies[i].BirthPredicates = append([]string(nil), contract.BirthPredicates...)
		copies[i].Groups = make([]projection.PredicateGroup, len(contract.Groups))
		for j, group := range contract.Groups {
			copies[i].Groups[j] = group
			copies[i].Groups[j].Predicates = append([]string(nil), group.Predicates...)
		}
	}
	return copies
}
