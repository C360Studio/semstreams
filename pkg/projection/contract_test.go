package projection

import (
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/ownership"
)

const sysPat = "c360.semconnect.systems.csapi.system.*"

// csapiSystem is the cs-api System projection from the ADR-056 acceptance
// fixture: six shared sensorml.* vocabulary predicates owned on the System
// id-glob, plus isHostedBy as BOTH an owned edge (System→parent) and a
// no-birth-stub foreign edge (child→System) — same owner, so the cross-type
// check must not flag it.
func csapiSystem() Contract {
	return Contract{
		Name:          "cs-api.system",
		MessageType:   "sensorml.asset.v1",
		EntityPattern: sysPat,
		Groups: []PredicateGroup{{
			Mode: ownership.ModeReplaceOwned,
			Predicates: []string{
				"sensorml.process.type", "sensorml.process.uid", "sensorml.process.label",
				"sensorml.process.description", "sensorml.process.position", "sensorml.component.isHostedBy",
			},
		}},
		ForeignEdges: []ForeignEdge{{
			Predicate: "sensorml.component.isHostedBy", Mode: ownership.EdgeNoBirthStub, TargetPattern: sysPat,
		}},
		IndexingProfile: "control",
	}
}

func TestContract_Validate(t *testing.T) {
	if err := csapiSystem().Validate(); err != nil {
		t.Fatalf("the cs-api System contract should validate: %v", err)
	}

	bad := []struct {
		name string
		c    Contract
	}{
		{"no name", Contract{EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"p"}}}}},
		{"no groups or edges", Contract{Name: "x", EntityPattern: sysPat}},
		{"bad pattern", Contract{Name: "x", EntityPattern: "a.b.c", Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"p"}}}}},
		{"bad mode", Contract{Name: "x", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.WriteMode("bogus"), Predicates: []string{"p"}}}}},
		{"bad indexing profile", Contract{Name: "x", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"p"}}}, IndexingProfile: "nonsense"}},
		{"predicate in two groups", Contract{Name: "x", EntityPattern: sysPat, Groups: []PredicateGroup{
			{Mode: ownership.ModeReplaceOwned, Predicates: []string{"p"}},
			{Mode: ownership.ModeAppendEvidence, Predicates: []string{"p"}},
		}}},
		{"bad foreign edge target", Contract{Name: "x", EntityPattern: sysPat, ForeignEdges: []ForeignEdge{{Predicate: "e", Mode: ownership.EdgeConditional, TargetPattern: "a.b"}}}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.c.Validate(); !errors.Is(err, ErrInvalidContract) {
				t.Errorf("want ErrInvalidContract, got %v", err)
			}
		})
	}
}

func TestContract_Derive_Single(t *testing.T) {
	reg, err := Derive("cs-api", csapiSystem())
	if err != nil {
		t.Fatalf("derive cs-api System: %v", err)
	}
	if reg.Owner != "cs-api" {
		t.Errorf("owner = %q, want cs-api", reg.Owner)
	}
	if len(reg.Claims) != 1 || len(reg.Claims[0].Predicates) != 6 {
		t.Errorf("expected one OwnerClaim with 6 predicates, got %d claims", len(reg.Claims))
	}
	if reg.Claims[0].Owner != "cs-api" || reg.Claims[0].Pattern != sysPat {
		t.Errorf("derived claim not bound to owner/pattern: %+v", reg.Claims[0])
	}
	if len(reg.ForeignEdges) != 1 || reg.ForeignEdges[0].Owner != "cs-api" {
		t.Errorf("expected one ForeignEdgeClaim bound to cs-api, got %+v", reg.ForeignEdges)
	}
	// The contract's MessageType is stamped onto the derived foreign edge as its
	// Producer, so the T2-seam can key its reject on (message_type, predicate).
	if reg.ForeignEdges[0].Producer != "sensorml.asset.v1" {
		t.Errorf("derived foreign edge Producer = %q, want sensorml.asset.v1", reg.ForeignEdges[0].Producer)
	}
	// The dual-use isHostedBy (owned + foreign, same owner) derives cleanly:
	// Derive runs the self-overlap validator (OwnerClaim×OwnerClaim only), and
	// the cross-type Owner×ForeignEdge check exempts the same owner — so this is
	// exercised end-to-end in the Bind integration test, not flagged here.
}

func TestContract_Derive_Aggregate(t *testing.T) {
	deployment := Contract{
		Name:          "cs-api.deployment",
		EntityPattern: "c360.semconnect.systems.csapi.deployment.*",
		Groups: []PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{"cs-api.deployment.parent", "cs-api.deployment.deployedSystems"},
		}},
	}
	reg, err := Derive("cs-api", csapiSystem(), deployment)
	if err != nil {
		t.Fatalf("aggregate derive (System + Deployment, disjoint): %v", err)
	}
	if len(reg.Claims) != 2 {
		t.Errorf("expected 2 OwnerClaims from 2 contracts, got %d", len(reg.Claims))
	}
}

func TestContract_Derive_CrossContractSelfOverlap(t *testing.T) {
	// Two contracts of the SAME owner claiming the SAME cell in owning mode — a
	// config bug Derive must catch (cross-owner overlap is RegisterOwner's job).
	a := Contract{Name: "a", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"dup.predicate"}}}}
	b := Contract{Name: "b", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"dup.predicate"}}}}
	_, err := Derive("cs-api", a, b)
	if !errors.Is(err, ownership.ErrOwnershipOverlap) {
		t.Errorf("two same-owner contracts on one cell must self-overlap, got %v", err)
	}
	if !errors.Is(err, ErrInvalidContract) {
		t.Errorf("the derivation error should also wrap ErrInvalidContract, got %v", err)
	}
}

func TestDerive_Empty(t *testing.T) {
	if _, err := Derive("cs-api"); !errors.Is(err, ErrInvalidContract) {
		t.Errorf("deriving with no contracts should error, got %v", err)
	}
}

func TestDerive_DuplicateContractName(t *testing.T) {
	c := csapiSystem()
	if _, err := Derive("cs-api", c, c); !errors.Is(err, ErrInvalidContract) {
		t.Errorf("deriving the same-named contract twice should error, got %v", err)
	}
}

// TestDerive_CrossContractModeContradiction: one owner declaring a predicate as
// replace-owned in one contract and append-evidence in another, over
// INTERSECTING patterns, is a contradiction the per-contract check can't see but
// Derive's aggregate validation must reject.
func TestDerive_CrossContractModeContradiction(t *testing.T) {
	owned := Contract{Name: "owned", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeReplaceOwned, Predicates: []string{"shared.p"}}}}
	appended := Contract{Name: "appended", EntityPattern: sysPat, Groups: []PredicateGroup{{Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.p"}}}}
	if _, err := Derive("cs-api", owned, appended); !errors.Is(err, ownership.ErrInvalidClaim) {
		t.Errorf("owned+append of the same predicate over intersecting patterns must error, got %v", err)
	}

	// Disjoint patterns: the same predicate may be owned on one entity type and
	// appended on another — legitimate, no error.
	appendedElsewhere := Contract{Name: "appended", EntityPattern: "c360.semconnect.systems.csapi.deployment.*", Groups: []PredicateGroup{{Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.p"}}}}
	if _, err := Derive("cs-api", owned, appendedElsewhere); err != nil {
		t.Errorf("owned + append on DISJOINT patterns must be allowed, got %v", err)
	}
}
