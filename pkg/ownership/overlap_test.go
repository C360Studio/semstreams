package ownership

import (
	"errors"
	"reflect"
	"testing"
)

// oc is a terse OwnerClaim constructor for tests.
func oc(owner, pattern string, mode WriteMode, preds ...string) OwnerClaim {
	return OwnerClaim{Owner: owner, Pattern: pattern, Predicates: preds, Mode: mode}
}

func fe(owner, predicate, targetPattern string, mode EdgeMode) ForeignEdgeClaim {
	return ForeignEdgeClaim{Owner: owner, Predicate: predicate, TargetPattern: targetPattern, Mode: mode}
}

const (
	sysPat = "c360.semconnect.systems.csapi.system.*"
	depPat = "c360.semconnect.systems.csapi.deployment.*"
)

func TestCheckOverlap_OwnerVsOwner(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, "sensorml.process.label")}},
	}

	t.Run("same cell, owning modes, different owners → reject", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeReplaceOwned, "sensorml.process.label")}}
		err := checkOverlap("other", cand, others, nil)
		var oe *OverlapError
		if !errors.As(err, &oe) {
			t.Fatalf("want *OverlapError, got %v", err)
		}
		if !errors.Is(err, ErrOwnershipOverlap) {
			t.Error("want errors.Is ErrOwnershipOverlap")
		}
		if oe.CrossType {
			t.Error("owner/owner collision must not be flagged CrossType")
		}
		if !reflect.DeepEqual(oe.Predicates, []string{"sensorml.process.label"}) {
			t.Errorf("predicates = %v", oe.Predicates)
		}
	})

	t.Run("disjoint id-space (System vs Deployment) → allowed", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{oc("other", depPat, ModeReplaceOwned, "sensorml.process.label")}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("disjoint patterns must not collide: %v", err)
		}
	})

	t.Run("disjoint predicates → allowed", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeReplaceOwned, "other.predicate")}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("disjoint predicates must not collide: %v", err)
		}
	})

	t.Run("append-evidence is exempt", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeAppendEvidence, "sensorml.process.label")}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("append-evidence candidate must be exempt: %v", err)
		}
		// And exempt when the INCUMBENT is append-evidence.
		othersAppend := map[string]ownerEntry{
			"cs-api": {Claims: []OwnerClaim{oc("cs-api", sysPat, ModeAppendEvidence, "sensorml.process.label")}},
		}
		cand2 := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeReplaceOwned, "sensorml.process.label")}}
		if err := checkOverlap("other", cand2, othersAppend, nil); err != nil {
			t.Errorf("append-evidence incumbent must be exempt: %v", err)
		}
	})

	t.Run("cas-transition counts as owning", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeCASTransition, "sensorml.process.label")}}
		if err := checkOverlap("other", cand, others, nil); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("cas-transition vs replace-owned on same cell must collide, got %v", err)
		}
	})

	t.Run("same owner re-registering does not self-collide", func(t *testing.T) {
		// `others` excludes the registrant by contract; prove a candidate that
		// would collide with itself is fine because it's filtered upstream.
		cand := ownerEntry{Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, "sensorml.process.label")}}
		if err := checkOverlap("cs-api", cand, map[string]ownerEntry{}, nil); err != nil {
			t.Errorf("empty others must not collide: %v", err)
		}
	})
}

func TestCheckOverlap_PartialPredicateCollision(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, "p.a", "p.b")}},
	}
	cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeReplaceOwned, "p.b", "p.c")}}
	err := checkOverlap("other", cand, others, nil)
	var oe *OverlapError
	if !errors.As(err, &oe) {
		t.Fatalf("want *OverlapError, got %v", err)
	}
	if !reflect.DeepEqual(oe.Predicates, []string{"p.b"}) {
		t.Errorf("only the shared predicate p.b should be reported, got %v", oe.Predicates)
	}
}

// TestCheckOverlap_CrossType covers the Owner×ForeignEdge MEDIUM (Decision 2):
// an OwnerClaim reconcile must not silently strip a DIFFERENT owner's foreign
// edge, but the SAME owner using a predicate as both own and foreign (cs-api's
// isHostedBy) is legitimate.
func TestCheckOverlap_CrossType(t *testing.T) {
	const isHostedBy = "sensorml.component.isHostedBy"

	t.Run("different owner: OwnerClaim strips foreign edge → reject", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{fe("producer", isHostedBy, sysPat, EdgeNoBirthStub)}},
		}
		cand := ownerEntry{Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, isHostedBy)}}
		err := checkOverlap("cs-api", cand, others, nil)
		var oe *OverlapError
		if !errors.As(err, &oe) {
			t.Fatalf("want *OverlapError, got %v", err)
		}
		if !oe.CrossType {
			t.Error("Owner×ForeignEdge collision must be flagged CrossType")
		}
	})

	t.Run("same owner: isHostedBy as own AND foreign → allowed (cs-api dual use)", func(t *testing.T) {
		// Registrant cs-api holds BOTH an OwnerClaim listing isHostedBy (own→parent)
		// AND a ForeignEdgeClaim for isHostedBy (child→System). They never collide.
		cand := ownerEntry{
			Claims:       []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, isHostedBy)},
			ForeignEdges: []ForeignEdgeClaim{fe("cs-api", isHostedBy, sysPat, EdgeNoBirthStub)},
		}
		if err := checkOverlap("cs-api", cand, map[string]ownerEntry{}, nil); err != nil {
			t.Errorf("same-owner dual use of isHostedBy must be allowed: %v", err)
		}
	})

	t.Run("two ForeignEdgeClaims on same predicate → allowed (FE×FE)", func(t *testing.T) {
		others := map[string]ownerEntry{
			"p1": {ForeignEdges: []ForeignEdgeClaim{fe("p1", isHostedBy, sysPat, EdgeConditional)}},
		}
		cand := ownerEntry{ForeignEdges: []ForeignEdgeClaim{fe("p2", isHostedBy, sysPat, EdgeConditional)}}
		if err := checkOverlap("p2", cand, others, nil); err != nil {
			t.Errorf("two foreign-edge producers must not collide: %v", err)
		}
	})

	t.Run("cross-type with disjoint target pattern → allowed", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{fe("producer", isHostedBy, depPat, EdgeConditional)}},
		}
		cand := ownerEntry{Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, isHostedBy)}}
		if err := checkOverlap("cs-api", cand, others, nil); err != nil {
			t.Errorf("disjoint owner pattern vs FE target pattern must not collide: %v", err)
		}
	})

	t.Run("FE with empty target pattern matches any owner pattern → reject", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{fe("producer", isHostedBy, "", EdgeConditional)}},
		}
		cand := ownerEntry{Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, isHostedBy)}}
		if err := checkOverlap("cs-api", cand, others, nil); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("empty FE target (match-any) must collide with any owner pattern, got %v", err)
		}
	})
}

func TestCheckOverlap_Waiver(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{oc("cs-api", sysPat, ModeReplaceOwned, "p.a", "p.b")}},
	}
	cand := ownerEntry{Claims: []OwnerClaim{oc("other", sysPat, ModeReplaceOwned, "p.a", "p.b")}}

	t.Run("MUTUAL waiver covering all overlapping predicates → allowed", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "cs-api", Predicates: []string{"p.a", "p.b"}, Reason: "shared by design"},
			{Owner: "cs-api", With: "other", Predicates: []string{"p.a", "p.b"}, Reason: "shared by design"},
		}
		if err := checkOverlap("other", cand, others, w); err != nil {
			t.Errorf("fully mutually-waived collision must be allowed: %v", err)
		}
	})

	t.Run("ONE-SIDED waiver does not exempt (mutual consent required)", func(t *testing.T) {
		w := []CoordinationWaiver{{Owner: "other", With: "cs-api", Predicates: []string{"p.a", "p.b"}, Reason: "unilateral"}}
		if err := checkOverlap("other", cand, others, w); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("a one-sided waiver must NOT exempt — both owners must consent, got %v", err)
		}
	})

	t.Run("mutual waiver covering only some predicates → reject the rest", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "cs-api", Predicates: []string{"p.a"}, Reason: "partial"},
			{Owner: "cs-api", With: "other", Predicates: []string{"p.a"}, Reason: "partial"},
		}
		err := checkOverlap("other", cand, others, w)
		var oe *OverlapError
		if !errors.As(err, &oe) {
			t.Fatalf("want *OverlapError, got %v", err)
		}
		if !reflect.DeepEqual(oe.Predicates, []string{"p.b"}) {
			t.Errorf("only the un-waived p.b should remain, got %v", oe.Predicates)
		}
	})

	t.Run("waiver for the wrong owner pair → no effect", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "someone-else", Predicates: []string{"p.a", "p.b"}, Reason: "wrong pair"},
			{Owner: "someone-else", With: "other", Predicates: []string{"p.a", "p.b"}, Reason: "wrong pair"},
		}
		if err := checkOverlap("other", cand, others, w); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("waiver naming the wrong pair must not exempt, got %v", err)
		}
	})
}
