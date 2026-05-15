package governance_test

import (
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/governance"
)

// TestRegister_PredicatesPresent confirms Register populates the
// global vocabulary registry with all four governance predicates.
func TestRegister_PredicatesPresent(t *testing.T) {
	governance.Register()

	want := []string{
		governance.InjectionSignal,
		governance.InjectionTier,
		governance.InjectionScore,
		governance.InjectionTopMatchID,
	}

	for _, name := range want {
		meta := vocabulary.GetPredicateMetadata(name)
		if meta == nil {
			t.Errorf("predicate %q not registered", name)
		}
	}
}

// TestRegister_RuleOpacitySplit pins the rule-opacity decision from
// ADR-043 Phase 2. Score and TopMatchID must be rule-opaque; signal
// and tier must stay rule-matchable. This test is the regression
// guard if anyone removes the WithRuleOpaque marker.
func TestRegister_RuleOpacitySplit(t *testing.T) {
	governance.Register()

	cases := []struct {
		name   string
		opaque bool
	}{
		{governance.InjectionSignal, false},
		{governance.InjectionTier, false},
		{governance.InjectionScore, true},
		{governance.InjectionTopMatchID, true},
	}

	for _, tc := range cases {
		got := vocabulary.IsRuleOpaque(tc.name)
		if got != tc.opaque {
			t.Errorf("IsRuleOpaque(%q) = %v, want %v", tc.name, got, tc.opaque)
		}
	}
}

// TestRegister_Idempotent confirms multiple Register calls do not
// panic or alter metadata. The registry overwrites by design; this
// test makes the contract explicit.
func TestRegister_Idempotent(t *testing.T) {
	governance.Register()
	governance.Register()

	if !vocabulary.IsRuleOpaque(governance.InjectionScore) {
		t.Errorf("InjectionScore rule-opacity lost after re-register")
	}
	if vocabulary.IsRuleOpaque(governance.InjectionSignal) {
		t.Errorf("InjectionSignal incorrectly marked rule-opaque after re-register")
	}
}
