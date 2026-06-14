package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
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
// Binding is best-effort / observe-only, matching the rest of the ADR-056
// rollout: a nil registry is a no-op (ownership disabled this boot), and any
// per-pack bind error is logged and skipped — it never aborts boot. An owner id
// is "rule-pack.<pack_id>"; the pack_id is validated subject-safe at config
// time (rule.Config.Validate), so RegisterOwner cannot reject it on charset.
func BindRulePackContracts(ctx context.Context, manager *Manager, ownerReg *ownership.Registry, hb *ownership.Heartbeater, logger *slog.Logger) {
	if ownerReg == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	bound := make(map[string]struct{}) // pack_id set, for duplicate detection
	for _, b := range manager.ProjectionBinders() {
		packID, contracts := b.ProjectionBindings()
		if packID == "" {
			continue
		}
		if _, exists := bound[packID]; exists {
			logger.Error("duplicate rule pack_id across components — skipping second bind",
				"pack_id", packID)
			continue
		}
		bound[packID] = struct{}{}

		if len(contracts) == 0 {
			// A pack_id with no contracts declares ownership identity but emits
			// nothing — nothing to bind. Recorded above so a duplicate id still
			// trips the guard.
			continue
		}

		ownerID := "rule-pack." + packID
		if err := projection.BindAndHeartbeat(ctx, ownerReg, hb, ownerID, contracts...); err != nil {
			if errors.Is(err, ownership.ErrOwnershipOverlap) {
				logger.Warn("ownership overlap on rule pack bind — continuing (observe-only)",
					"owner_id", ownerID, "err", err)
			} else {
				logger.Warn("rule pack bind error — continuing (observe-only)",
					"owner_id", ownerID, "err", err)
			}
		}
	}
}
