package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	rulepackcontract "github.com/c360studio/semstreams/pkg/rulepack"
)

// ProjectionBinder is the narrow composition contract implemented by rule
// processors. Preflight is side-effect-free; SetOwnedReplacer is one-time.
type ProjectionBinder interface {
	ProjectionBindings() (packID string, contracts []projection.Contract)
	PreflightProjectionMutations() error
	SetOwnedReplacer(projection.OwnedReplacer) error
}

// ProjectionBinders enumerates every managed component that implements
// ProjectionBinder.
func (m *Manager) ProjectionBinders() []ProjectionBinder {
	cmService, exists := m.services["component-manager"]
	if !exists {
		return nil
	}
	cm, ok := cmService.(*ComponentManager)
	if !ok {
		return nil
	}

	var out []ProjectionBinder
	for _, managed := range cm.GetManagedComponents() {
		if binder, ok := managed.Component.(ProjectionBinder); ok {
			out = append(out, binder)
		}
	}
	return out
}

type rulePackMutationPlan struct {
	binder    ProjectionBinder
	packID    string
	contracts []projection.Contract
}

// BindRulePackContracts preflights the complete enabled pack set before the
// first ownership or heartbeat side effect, then binds exactly one public
// mutation client per contract-bearing pack and injects only OwnedReplacer.
// Call once before Manager.StartAll.
func BindRulePackContracts(
	ctx context.Context,
	manager *Manager,
	ownerReg *ownership.Registry,
	heartbeater *ownership.Heartbeater,
	_ *slog.Logger,
) error {
	if manager == nil {
		return fmt.Errorf("rule-pack mutation composition requires a service manager")
	}
	plans, err := preflightRulePackMutations(manager, ownerReg, heartbeater)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if len(plan.contracts) == 0 {
			continue
		}
		client, err := projection.BindMutationClient(ctx, projection.MutationClientConfig{
			NATS:        manager.natsClient,
			Registry:    ownerReg,
			Heartbeater: heartbeater,
			Owner:       "rule-pack." + plan.packID,
			Contracts:   plan.contracts,
		})
		if err != nil {
			ownerID := "rule-pack." + plan.packID
			if errors.Is(err, ownership.ErrOwnerAlreadyBound) {
				return fmt.Errorf("rule pack owner %q already bound: %w", ownerID, err)
			}
			return fmt.Errorf("bind mutation client for rule pack %q: %w", plan.packID, err)
		}
		if err := plan.binder.SetOwnedReplacer(client); err != nil {
			return fmt.Errorf("inject owned replacer for rule pack %q: %w", plan.packID, err)
		}
	}
	return nil
}

func preflightRulePackMutations(
	manager *Manager,
	ownerReg *ownership.Registry,
	heartbeater *ownership.Heartbeater,
) ([]rulePackMutationPlan, error) {
	binders := manager.ProjectionBinders()
	plans := make([]rulePackMutationPlan, 0, len(binders))
	seen := make(map[string]struct{}, len(binders))
	var aggregate []projection.Contract

	for _, binder := range binders {
		packID, contracts := binder.ProjectionBindings()
		if err := rulepackcontract.ValidateID(packID); err != nil {
			return nil, fmt.Errorf("enabled rule processor has invalid pack_id: %w", err)
		}
		if _, duplicate := seen[packID]; duplicate {
			return nil, fmt.Errorf("duplicate enabled rule pack_id %q in one composition", packID)
		}
		seen[packID] = struct{}{}
		if err := binder.PreflightProjectionMutations(); err != nil {
			return nil, fmt.Errorf("preflight rule pack %q: %w", packID, err)
		}
		copies := cloneRulePackContracts(contracts)
		if len(copies) > 0 {
			if manager.natsClient == nil {
				return nil, fmt.Errorf("rule pack %q mutation client requires NATS", packID)
			}
			if err := validateRulePackMutationDependencies(packID, copies, ownerReg, heartbeater); err != nil {
				return nil, err
			}
			if _, err := projection.Derive("rule-pack."+packID, copies...); err != nil {
				return nil, fmt.Errorf("validate contracts for rule pack %q: %w", packID, err)
			}
			aggregate = append(aggregate, copies...)
		}
		plans = append(plans, rulePackMutationPlan{
			binder:    binder,
			packID:    packID,
			contracts: copies,
		})
	}
	if len(aggregate) > 0 {
		if _, err := projection.Derive("rule-pack-preflight-all", aggregate...); err != nil {
			return nil, fmt.Errorf("enabled rule-pack projection overlap: %w", err)
		}
	}
	return plans, nil
}

func validateRulePackMutationDependencies(
	packID string,
	contracts []projection.Contract,
	ownerReg *ownership.Registry,
	heartbeater *ownership.Heartbeater,
) error {
	requiresRegistry := false
	requiresHeartbeat := false
	for _, contract := range contracts {
		if len(contract.Groups) > 0 || len(contract.ForeignEdges) > 0 {
			requiresRegistry = true
		}
		for _, group := range contract.Groups {
			if group.Mode == ownership.ModeReplaceOwned ||
				group.Mode == ownership.ModeCASTransition {
				requiresHeartbeat = true
			}
		}
	}
	if requiresRegistry && ownerReg == nil {
		return fmt.Errorf("rule pack %q projection contracts require an ownership registry", packID)
	}
	if requiresHeartbeat && heartbeater == nil {
		return fmt.Errorf("rule pack %q owning projection contracts require a heartbeater", packID)
	}
	return nil
}

func cloneRulePackContracts(contracts []projection.Contract) []projection.Contract {
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
