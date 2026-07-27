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

// ProjectionBinder is implemented by components that declare a pack-level graph
// projection ownership (today: rule.Processor). It is defined as an interface
// here, rather than importing processor/rule directly, ON PURPOSE: a
// service→processor/rule import would close a TEST-ONLY cycle
// (processor/rule's actions_test.go imports processor/agentic-tools, which
// imports service), so the enumeration is duck-typed instead.
//
// ProjectionBindings returns the pack id (owner becomes "rule-pack.<packID>")
// and the contracts the pack owns. Read ONCE at the composition root before
// StartAll; never re-derived on hot-reload (ADR-056 #278 inc 2).
type ProjectionBinder interface {
	ProjectionBindings() (packID string, contracts []projection.Contract)
}

// ProjectionBinders enumerates every managed component that implements
// ProjectionBinder. Returns nil when the ComponentManager is not yet available.
// Mirrors the GetManagedComponents enumeration idiom used by
// registerComponentHandlers.
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
	for _, mc := range cm.GetManagedComponents() {
		if b, ok := mc.Component.(ProjectionBinder); ok {
			out = append(out, b)
		}
	}
	return out
}

// BindRulePackContracts binds the projection contracts of every rule pack the
// manager holds under the ownership substrate (ADR-056 #278 inc 2).
//
// INVARIANT — call this ONCE at the composition root, BEFORE manager.StartAll.
// Rule-pack contracts are PACK-LEVEL and STATIC: they are read once here and
// the binding is NEVER re-derived. NEVER call this from a hot-reload path —
// hot-reload must not re-bind ownership (a re-bind would self-overlap against
// the pack's own already-registered claims and would churn the ownership
// epoch). The only entry point that reads the contracts is
// ProjectionBinder.ProjectionBindings(), which carries the same invariant.
//
// The complete enabled binder set is preflighted before any owner token is
// minted or claim is bound. Missing or duplicate pack IDs are composition
// errors and abort boot. Cross-owner overlap and transient substrate failures
// remain observe-only; ErrOwnerAlreadyBound aborts because continuing would
// activate a pack under an owner whose complete contract set came from another
// bind.
//
// ADR-056 PR-3.5: also mints the typed OwnerToken via ownerReg.OwnerToken and
// stamps it on the binder (if it implements SetProjectionOwnerToken) so the
// pack's ActionExecutor can put the credential on replace_owned mutation
// requests. This is the only point where the process-local Registry is reachable
// alongside the binders; minting here keeps producers from hand-composing the
// "<owner>#<incarnation>" format.
func BindRulePackContracts(ctx context.Context, manager *Manager, ownerReg *ownership.Registry, hb *ownership.Heartbeater, logger *slog.Logger) error {
	binders := manager.ProjectionBinders()
	if err := validateRulePackComposition(binders); err != nil {
		return err
	}
	if ownerReg == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	for _, b := range binders {
		packID, contracts := b.ProjectionBindings()
		ownerID := "rule-pack." + packID

		// ADR-056 PR-3.5: mint the typed OwnerToken from the Registry and stamp
		// it on the binder BEFORE Start so initializeStateTracker forwards it to
		// the ActionExecutor. Minting via Registry.OwnerToken keeps the
		// "<owner>#<incarnation>" credential format inside pkg/ownership — the
		// pack never hand-composes it. Stamped even for no-contract packs (which
		// own nothing and never reach BindAndHeartbeat below) so a replace_owned
		// action on an unowned predicate still presents a comparable token to the
		// observe-only lease check. Duck-typed to keep service→processor/rule
		// free of a direct import.
		if setter, ok := b.(interface {
			SetProjectionOwnerToken(ownership.OwnerToken)
		}); ok {
			setter.SetProjectionOwnerToken(ownerReg.OwnerToken(ownerID))
		}

		if len(contracts) == 0 {
			// A pack_id with no contracts declares ownership identity but emits
			// nothing — nothing to bind. Recorded above so a duplicate id still
			// trips the guard.
			continue
		}

		if _, err := projection.BindAndHeartbeat(ctx, ownerReg, hb, ownerID, contracts...); err != nil {
			if errors.Is(err, ownership.ErrOwnerAlreadyBound) {
				return fmt.Errorf("rule pack owner %q already bound: %w", ownerID, err)
			} else if errors.Is(err, ownership.ErrOwnershipOverlap) {
				logger.Warn("ownership overlap on rule pack bind — continuing (observe-only)",
					"owner_id", ownerID, "err", err)
			} else {
				logger.Warn("rule pack bind error — continuing (observe-only)",
					"owner_id", ownerID, "err", err)
			}
		}
	}
	return nil
}

func validateRulePackComposition(binders []ProjectionBinder) error {
	seen := make(map[string]struct{}, len(binders))
	for _, binder := range binders {
		packID, _ := binder.ProjectionBindings()
		if err := rulepackcontract.ValidateID(packID); err != nil {
			return fmt.Errorf("enabled rule processor has invalid pack_id: %w", err)
		}
		if _, duplicate := seen[packID]; duplicate {
			return fmt.Errorf("duplicate enabled rule pack_id %q in one composition", packID)
		}
		seen[packID] = struct{}{}
	}
	return nil
}
