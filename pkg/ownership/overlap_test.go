package ownership

import (
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
)

const (
	sysPat = "c360.semconnect.systems.csapi.system.*"
	depPat = "c360.semconnect.systems.csapi.deployment.*"
)

func TestCheckOverlap_OwnerVsOwner(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}},
	}

	t.Run("same cell, owning modes, different owners → reject", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}
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
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: depPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("disjoint patterns must not collide: %v", err)
		}
	})

	t.Run("disjoint predicates → allowed", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "other", "value", "predicate")}}}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("disjoint predicates must not collide: %v", err)
		}
	})

	t.Run("append-evidence is exempt", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeAppendEvidence, Predicates: []string{"sensorml.process.label"}}}}
		if err := checkOverlap("other", cand, others, nil); err != nil {
			t.Errorf("append-evidence candidate must be exempt: %v", err)
		}
		// And exempt when the INCUMBENT is append-evidence.
		othersAppend := map[string]ownerEntry{
			"cs-api": {Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeAppendEvidence, Predicates: []string{"sensorml.process.label"}}}},
		}
		cand2 := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}
		if err := checkOverlap("other", cand2, othersAppend, nil); err != nil {
			t.Errorf("append-evidence incumbent must be exempt: %v", err)
		}
	})

	t.Run("cas-transition counts as owning", func(t *testing.T) {
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeCASTransition, Predicates: []string{"sensorml.process.label"}}}}
		if err := checkOverlap("other", cand, others, nil); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("cas-transition vs replace-owned on same cell must collide, got %v", err)
		}
	})

	t.Run("same owner re-registering does not self-collide", func(t *testing.T) {
		// `others` excludes the registrant by contract; prove a candidate that
		// would collide with itself is fine because it's filtered upstream.
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}}}
		if err := checkOverlap("cs-api", cand, map[string]ownerEntry{}, nil); err != nil {
			t.Errorf("empty others must not collide: %v", err)
		}
	})
}

func TestCheckOverlap_PartialPredicateCollision(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}}}},
	}
	cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "b"), semantictest.Predicate(t, "test", "value", "c")}}}}
	err := checkOverlap("other", cand, others, nil)
	var oe *OverlapError
	if !errors.As(err, &oe) {
		t.Fatalf("want *OverlapError, got %v", err)
	}
	if !reflect.DeepEqual(oe.Predicates, []string{"test.value.b"}) {
		t.Errorf("only the shared predicate p.b should be reported, got %v", oe.Predicates)
	}
}

// TestCheckOverlap_CrossType covers the Owner×ForeignEdge MEDIUM (Decision 2):
// an OwnerClaim reconcile must not silently strip a DIFFERENT owner's foreign
// edge, but the SAME owner using a predicate as both own and foreign (cs-api's
// isHostedBy) is legitimate.
func TestCheckOverlap_CrossType(t *testing.T) {
	t.Run("different owner: OwnerClaim strips foreign edge → reject", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "producer", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeNoBirthStub}}},
		}
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "sensorml", "component", "is-hosted-by")}}}}
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
			Claims:       []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "sensorml", "component", "is-hosted-by")}}},
			ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "cs-api", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeNoBirthStub}},
		}
		if err := checkOverlap("cs-api", cand, map[string]ownerEntry{}, nil); err != nil {
			t.Errorf("same-owner dual use of isHostedBy must be allowed: %v", err)
		}
	})

	t.Run("two ForeignEdgeClaims on same predicate → allowed (FE×FE)", func(t *testing.T) {
		others := map[string]ownerEntry{
			"p1": {ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "p1", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeConditional}}},
		}
		cand := ownerEntry{ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "p2", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: sysPat, Mode: EdgeConditional}}}
		if err := checkOverlap("p2", cand, others, nil); err != nil {
			t.Errorf("two foreign-edge producers must not collide: %v", err)
		}
	})

	t.Run("cross-type with disjoint target pattern → allowed", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "producer", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: depPat, Mode: EdgeConditional}}},
		}
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "sensorml", "component", "is-hosted-by")}}}}
		if err := checkOverlap("cs-api", cand, others, nil); err != nil {
			t.Errorf("disjoint owner pattern vs FE target pattern must not collide: %v", err)
		}
	})

	t.Run("FE with empty target pattern matches any owner pattern → reject", func(t *testing.T) {
		others := map[string]ownerEntry{
			"producer": {ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "producer", Predicate: semantictest.Predicate(t, "sensorml", "component", "is-hosted-by"), TargetPattern: "", Mode: EdgeConditional}}}, // entity-id-audit:classify intentional-sentinel "" line=154 column=180 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
		}
		cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "sensorml", "component", "is-hosted-by")}}}}
		if err := checkOverlap("cs-api", cand, others, nil); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("empty FE target (match-any) must collide with any owner pattern, got %v", err)
		}
	})
}

func TestCheckOverlap_Waiver(t *testing.T) {
	others := map[string]ownerEntry{
		"cs-api": {Claims: []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}}}},
	}
	cand := ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "other", Pattern: sysPat, Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}}}}

	t.Run("MUTUAL waiver covering all overlapping predicates → allowed", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "cs-api", Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}, Reason: "shared by design"},
			{Owner: "cs-api", With: "other", Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}, Reason: "shared by design"},
		}
		if err := checkOverlap("other", cand, others, w); err != nil {
			t.Errorf("fully mutually-waived collision must be allowed: %v", err)
		}
	})

	t.Run("ONE-SIDED waiver does not exempt (mutual consent required)", func(t *testing.T) {
		w := []CoordinationWaiver{{Owner: "other", With: "cs-api", Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}, Reason: "unilateral"}}
		if err := checkOverlap("other", cand, others, w); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("a one-sided waiver must NOT exempt — both owners must consent, got %v", err)
		}
	})

	t.Run("mutual waiver covering only some predicates → reject the rest", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "cs-api", Predicates: []string{semantictest.Predicate(t, "test", "value", "a")}, Reason: "partial"},
			{Owner: "cs-api", With: "other", Predicates: []string{semantictest.Predicate(t, "test", "value", "a")}, Reason: "partial"},
		}
		err := checkOverlap("other", cand, others, w)
		var oe *OverlapError
		if !errors.As(err, &oe) {
			t.Fatalf("want *OverlapError, got %v", err)
		}
		if !reflect.DeepEqual(oe.Predicates, []string{"test.value.b"}) {
			t.Errorf("only the un-waived p.b should remain, got %v", oe.Predicates)
		}
	})

	t.Run("waiver for the wrong owner pair → no effect", func(t *testing.T) {
		w := []CoordinationWaiver{
			{Owner: "other", With: "someone-else", Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}, Reason: "wrong pair"},
			{Owner: "someone-else", With: "other", Predicates: []string{semantictest.Predicate(t, "test", "value", "a"), semantictest.Predicate(t, "test", "value", "b")}, Reason: "wrong pair"},
		}
		if err := checkOverlap("other", cand, others, w); !errors.Is(err, ErrOwnershipOverlap) {
			t.Errorf("waiver naming the wrong pair must not exempt, got %v", err)
		}
	})
}
