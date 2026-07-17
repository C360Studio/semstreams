// Package rule — gh#451 regression.
//
// Rules installed via the hot-reload KV reconcile path (RuntimeConfigurable.
// ApplyConfigUpdate → applyRuleChanges → applyExpressionRuleChange) must
// receive the Lifecycle harness Manager, exactly like the file/inline-load
// path (rule_loader.go). Before the fix, reconciled ExpressionRule instances
// kept a nil manager, so `$entity.lifecycle.*` conditions silently never
// resolved and the phase-gated rule never fired — defeating the documented
// `(phase == X AND command == Y) -> transition` pattern.
package rule

import (
	"log/slog"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyConfigUpdate_LifecycleManagerReachesReconciledRules drives the same
// production wire the KV-config watcher's reconcile uses and proves a
// phase-gated `$entity.lifecycle.phase` rule fires afterward. The bug lived in
// applyExpressionRuleChange, which re-creates every rule on reconcile without
// mirroring rule_loader.go's SetLifecycleManager call.
func TestApplyConfigUpdate_LifecycleManagerReachesReconciledRules(t *testing.T) {
	const (
		workflow    = "quest"
		postedEntry = "acme.ops.games.dragon.quest.001"
		claimedEnt  = "acme.ops.games.dragon.quest.002"
	)

	// Fake Manager seeded with two lifecycle-managed entities in distinct
	// phases so the assertion pair proves the phase field is genuinely
	// resolved and compared — not vacuously matched.
	mgr := newFakeManager()
	mgr.seed(workflow, &fakeParticipant{EntityIDF: postedEntry, PhaseF: "posted"})
	mgr.seed(workflow, &fakeParticipant{EntityIDF: claimedEnt, PhaseF: "claimed"})

	cfg := mustTestConfig(t, "rule-test-pack")
	rp := &Processor{
		natsClient:  &natsclient.Client{},
		logger:      slog.Default(),
		rules:       make(map[string]Rule),
		ruleConfigs: make(map[string]map[string]any),
		config:      &cfg,
	}
	// Wire the Manager the way the component factory does (factory.go:108).
	rp.SetLifecycleManager(mgr)

	// Install a phase-gated expression rule through the RuntimeConfigurable
	// wire — the reconcile path, NOT rule_loader's file/inline load. "expression"
	// (not "test_rule") is the type that yields an *ExpressionRule, the only rule
	// type that resolves $entity.lifecycle.* and implements SetLifecycleManager.
	const ruleID = "phase_gate"
	changes := map[string]any{
		"rules": map[string]any{
			ruleID: map[string]any{
				"type": "expression",
				"name": "Phase-gated transition",
				"conditions": []any{
					map[string]any{
						"field":    "$entity.lifecycle.phase", // predicate-audit:invalid {"kind":"stored-predicate","value":"$entity.lifecycle.phase","reason":"segment_start"}
						"operator": "eq",
						"value":    "posted",
						"required": true,
					},
				},
				"logic":   "and",
				"enabled": true,
			},
		},
	}
	require.NoError(t, rp.ValidateConfigUpdate(changes))
	require.NoError(t, rp.ApplyConfigUpdate(changes))

	// The reconciled instance must be an ExpressionRule carrying the Manager.
	rule, ok := rp.rules[ruleID]
	require.True(t, ok, "rule not installed by reconcile")
	er, ok := rule.(*ExpressionRule)
	require.True(t, ok, "reconciled rule is not an *ExpressionRule")
	require.NotNil(t, er.lifecycleManager,
		"gh#451: reconciled rule kept a nil lifecycleManager — $entity.lifecycle.* would never resolve")

	// Behavioural proof through the exact method the entity-watch path invokes
	// (message_handler.go: entityEval.EvaluateEntityState). Positive: an entity
	// in phase "posted" fires. Control: an entity in "claimed" does not — proves
	// the phase is resolved and compared, not treated as always-true.
	assert.True(t, er.EvaluateEntityState(&gtypes.EntityState{ID: postedEntry}),
		"phase-gated rule should fire for an entity in phase 'posted'")
	assert.False(t, er.EvaluateEntityState(&gtypes.EntityState{ID: claimedEnt}),
		"phase-gated rule must not fire for an entity in phase 'claimed'")
}
