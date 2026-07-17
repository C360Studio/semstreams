package rule

import "testing"

// TestHasStatefulRuleActions_OnRecoveryOnly is the gh#530 /
// rule-evaluation-completeness acceptance test for the stateful-routing
// gate itself: a Definition whose ONLY populated action list is
// OnRecovery (OnEnter, OnExit, WhileTrue all empty/nil) must still be
// admitted to the stateful evaluator on both evaluation paths — the
// subject-path gate at evaluateRulesForMessage and the entity-state/
// bootstrap-path gate at evaluateRulesForEntityState both derive from
// this shared predicate.
//
// A pure fail-closed recovery park (empty on_enter/on_exit/while_true,
// only on_recovery) must not be silently excluded from evaluation —
// that exclusion is exactly what made on_recovery-only rules inert
// before this predicate covered OnRecovery.
func TestHasStatefulRuleActions_OnRecoveryOnly(t *testing.T) {
	def := Definition{
		ID:   "recovery-only-rule",
		Type: "expression",
		OnRecovery: []Action{
			{Type: ActionTypePublish, Subject: "test.recovered"},
		},
	}
	if !hasStatefulRuleActions(def) {
		t.Fatal("hasStatefulRuleActions must return true for a rule with only OnRecovery actions defined")
	}
}

// TestHasStatefulRuleActions_TruthTable covers the full predicate —
// every action list independently trips it, and a Definition with none
// of the four populated correctly returns false (message-only /
// stateless expression rules must not pay the stateful-evaluator cost).
func TestHasStatefulRuleActions_TruthTable(t *testing.T) {
	action := Action{Type: ActionTypePublish, Subject: "test.fired"}
	cases := []struct {
		name string
		def  Definition
		want bool
	}{
		{name: "all empty", def: Definition{ID: "r"}, want: false},
		{name: "OnEnter only", def: Definition{ID: "r", OnEnter: []Action{action}}, want: true},
		{name: "OnExit only", def: Definition{ID: "r", OnExit: []Action{action}}, want: true},
		{name: "WhileTrue only", def: Definition{ID: "r", WhileTrue: []Action{action}}, want: true},
		{name: "OnRecovery only", def: Definition{ID: "r", OnRecovery: []Action{action}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasStatefulRuleActions(tc.def); got != tc.want {
				t.Errorf("hasStatefulRuleActions(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
