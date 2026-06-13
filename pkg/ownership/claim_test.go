package ownership

import (
	"errors"
	"log/slog"
	"testing"
)

func TestOwnerClaim_Validate(t *testing.T) {
	good := oc("cs-api", "c360.semconnect.systems.csapi.system.*", ModeReplaceOwned, "sensorml.process.label")
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed claim should validate: %v", err)
	}

	bad := []struct {
		name  string
		claim OwnerClaim
	}{
		{"empty owner", oc("", "a.b.c.d.e.f", ModeReplaceOwned, "p")},
		{"5-part pattern", oc("o", "a.b.c.d.e", ModeReplaceOwned, "p")},
		{"bare star pattern", oc("o", "*", ModeReplaceOwned, "p")},
		{"no predicates", oc("o", "a.b.c.d.e.f", ModeReplaceOwned)},
		{"empty predicate", oc("o", "a.b.c.d.e.f", ModeReplaceOwned, "")},
		{"wildcard predicate", oc("o", "a.b.c.d.e.f", ModeReplaceOwned, "agent.*")},
		{"invalid mode", oc("o", "a.b.c.d.e.f", WriteMode("bogus"), "p")},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.claim.Validate(); !errors.Is(err, ErrInvalidClaim) {
				t.Errorf("want ErrInvalidClaim, got %v", err)
			}
		})
	}
}

func TestForeignEdgeClaim_Validate(t *testing.T) {
	good := fe("cs-api", "sensorml.component.isHostedBy", "c360.semconnect.systems.csapi.system.*", EdgeNoBirthStub)
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed foreign-edge claim should validate: %v", err)
	}
	// Empty target pattern (match-any) is valid.
	if err := fe("o", "p", "", EdgeConditional).Validate(); err != nil {
		t.Errorf("empty target pattern should be valid (match-any): %v", err)
	}

	bad := []struct {
		name string
		fec  ForeignEdgeClaim
	}{
		{"empty owner", fe("", "p", "", EdgeConditional)},
		{"empty predicate", fe("o", "", "", EdgeConditional)},
		{"wildcard predicate", fe("o", "p.*", "", EdgeConditional)},
		{"invalid mode", fe("o", "p", "", EdgeMode("bogus"))},
		{"bad target pattern", fe("o", "p", "a.b.c", EdgeConditional)},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fec.Validate(); !errors.Is(err, ErrInvalidClaim) {
				t.Errorf("want ErrInvalidClaim, got %v", err)
			}
		})
	}
}

func TestCoordinationWaiver_Validate(t *testing.T) {
	good := CoordinationWaiver{Owner: "a", With: "b", Predicates: []string{"p"}, Reason: "because"}
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed waiver should validate: %v", err)
	}
	bad := []struct {
		name string
		w    CoordinationWaiver
	}{
		{"no with", CoordinationWaiver{Owner: "a", Predicates: []string{"p"}, Reason: "r"}},
		{"no predicates", CoordinationWaiver{Owner: "a", With: "b", Reason: "r"}},
		{"no reason", CoordinationWaiver{Owner: "a", With: "b", Predicates: []string{"p"}}},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.w.Validate(); !errors.Is(err, ErrInvalidClaim) {
				t.Errorf("want ErrInvalidClaim, got %v", err)
			}
		})
	}
}

func TestEpoch_OwnerOf(t *testing.T) {
	ep := newEpoch()
	ep.Owners["cs-api"] = ownerEntry{Claims: []OwnerClaim{
		oc("cs-api", "c360.semconnect.systems.csapi.system.*", ModeReplaceOwned, "sensorml.process.label"),
		oc("cs-api", "c360.semconnect.systems.csapi.system.*", ModeAppendEvidence, "evidence.note"),
	}}

	owner, ok := ep.ownerOf("c360.semconnect.systems.csapi.system.drone-001", "sensorml.process.label")
	if !ok || owner != "cs-api" {
		t.Errorf("ownerOf owning predicate = %q,%v, want cs-api,true", owner, ok)
	}
	// append-evidence predicate has no owner (exempt from the lease check).
	if _, ok := ep.ownerOf("c360.semconnect.systems.csapi.system.drone-001", "evidence.note"); ok {
		t.Error("append-evidence predicate must not report an owner")
	}
	// unclaimed predicate, and non-matching entity.
	if _, ok := ep.ownerOf("c360.semconnect.systems.csapi.system.drone-001", "unclaimed"); ok {
		t.Error("unclaimed predicate must not report an owner")
	}
	if _, ok := ep.ownerOf("c360.other.x.y.z.1", "sensorml.process.label"); ok {
		t.Error("non-matching entity must not report an owner")
	}
}

func TestEpoch_ForeignEdgeClaimFor(t *testing.T) {
	ep := newEpoch()
	ep.Owners["p"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{
		{Owner: "p", Predicate: "edge.specific", Mode: EdgeConditional, Producer: "type.a"},
		{Owner: "p", Predicate: "edge.any", Mode: EdgeStrict, Producer: ""},
	}}

	if c, ok := ep.foreignEdgeClaimFor("type.a", "edge.specific"); !ok || c.Producer != "type.a" {
		t.Errorf("exact producer match: got %+v ok=%v", c, ok)
	}
	if _, ok := ep.foreignEdgeClaimFor("type.b", "edge.specific"); ok {
		t.Error("a producer-specific claim must not match a different producer")
	}
	if _, ok := ep.foreignEdgeClaimFor("any.type.at.all", "edge.any"); !ok {
		t.Error("an empty-producer (any) claim should match any message type")
	}
	if _, ok := ep.foreignEdgeClaimFor("type.a", "edge.unknown"); ok {
		t.Error("an unknown predicate must not match")
	}
}

func TestEpoch_ForeignEdgeClaimFor_ExactBeatsAny(t *testing.T) {
	ep := newEpoch()
	ep.Owners["p1"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "p1", Predicate: "e", Mode: EdgeStrict, Producer: ""}}}
	ep.Owners["p2"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "p2", Predicate: "e", Mode: EdgeConditional, Producer: "type.x"}}}
	c, ok := ep.foreignEdgeClaimFor("type.x", "e")
	if !ok || c.Producer != "type.x" {
		t.Errorf("an exact producer match must win over an any-producer claim, got %+v ok=%v", c, ok)
	}
}

// TestEpoch_ForeignEdgeClaimFor_Deterministic: two owners legitimately register
// the same predicate as a foreign edge (FE×FE is allowed) with DIFFERENT modes.
// The returned claim — whose Mode the seam consumer branches on — must be stable
// across calls (sorted-owner scan), not a map-iteration coin-flip.
func TestEpoch_ForeignEdgeClaimFor_Deterministic(t *testing.T) {
	ep := newEpoch()
	ep.Owners["zeta"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "zeta", Predicate: "e", Mode: EdgeStrict, Producer: ""}}}
	ep.Owners["alpha"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "alpha", Predicate: "e", Mode: EdgeConditional, Producer: ""}}}

	first, ok := ep.foreignEdgeClaimFor("any.type", "e")
	if !ok {
		t.Fatal("expected a match")
	}
	for i := 0; i < 50; i++ {
		got, _ := ep.foreignEdgeClaimFor("any.type", "e")
		if got.Owner != first.Owner || got.Mode != first.Mode {
			t.Fatalf("non-deterministic foreign-edge lookup: got %+v, first %+v", got, first)
		}
	}
	// Sorted-owner scan ⇒ "alpha" wins over "zeta".
	if first.Owner != "alpha" {
		t.Errorf("deterministic pick should be the sorted-first owner (alpha), got %q", first.Owner)
	}
}

func TestEpoch_CompactStale(t *testing.T) {
	ep := newEpoch()
	ep.Owners["live"] = ownerEntry{Claims: []OwnerClaim{oc("live", "a.b.c.d.e.f", ModeReplaceOwned, "p")}}
	ep.Owners["dead"] = ownerEntry{Claims: []OwnerClaim{oc("dead", "a.b.c.d.e.g", ModeReplaceOwned, "p")}}
	ep.Owners["registrant"] = ownerEntry{Claims: []OwnerClaim{oc("registrant", "a.b.c.d.e.h", ModeReplaceOwned, "p")}}

	// Only "live" has presence; "registrant" is exempt; "dead" gets evicted.
	live := map[string]struct{}{"live": {}}
	evicted := ep.compactStale("registrant", live)

	if len(evicted) != 1 || evicted[0] != "dead" {
		t.Fatalf("evicted = %v, want [dead]", evicted)
	}
	if _, ok := ep.Owners["dead"]; ok {
		t.Error("dead owner should have been compacted out")
	}
	if _, ok := ep.Owners["live"]; !ok {
		t.Error("live owner must survive")
	}
	if _, ok := ep.Owners["registrant"]; !ok {
		t.Error("registrant must never be compacted (it is registering now)")
	}
}

// A dead owner whose entry is FE-claim-ONLY is exempt from compaction: a
// ForeignEdgeClaim is not a lease and is not heartbeat-enrolled, so reaping it
// on liveness only makes the T2-seam reject flap. A dead owner that ALSO holds
// an OwnerClaim is still reaped (it holds a contested cell).
func TestEpoch_CompactStale_ExemptsForeignEdgeOnlyOwners(t *testing.T) {
	ep := newEpoch()
	ep.Owners["fe-only"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{fe("fe-only", "x.edge", "", EdgeNoBirthStub)}}
	ep.Owners["mixed"] = ownerEntry{
		Claims:       []OwnerClaim{oc("mixed", "a.b.c.d.e.f", ModeReplaceOwned, "p")},
		ForeignEdges: []ForeignEdgeClaim{fe("mixed", "y.edge", "", EdgeStrict)},
	}
	ep.Owners["owning-dead"] = ownerEntry{Claims: []OwnerClaim{oc("owning-dead", "a.b.c.d.e.g", ModeReplaceOwned, "p")}}
	// A degenerate empty entry (no claims, no edges) is NOT exempt — the
	// exemption guards FE-claim-only owners, not empties. validateStructural
	// prevents registering one, so this is defense-in-depth.
	ep.Owners["empty-dead"] = ownerEntry{}

	// No owner has presence — all are "dead". Registrant is a fresh fifth owner.
	evicted := ep.compactStale("registrant", map[string]struct{}{})

	if _, ok := ep.Owners["fe-only"]; !ok {
		t.Error("FE-claim-only owner must be exempt from compaction")
	}
	if _, ok := ep.Owners["mixed"]; ok {
		t.Error("owner with an OwnerClaim (even alongside a foreign edge) must still be compacted when dead")
	}
	if _, ok := ep.Owners["owning-dead"]; ok {
		t.Error("dead owning owner must be compacted")
	}
	if _, ok := ep.Owners["empty-dead"]; ok {
		t.Error("a degenerate empty entry must still be compacted (the exemption is FE-only, not empty)")
	}
	// evicted names the three non-exempt owners, not the FE-only one.
	if len(evicted) != 3 {
		t.Fatalf("evicted = %v, want [empty-dead mixed owning-dead]", evicted)
	}
}

// The Registry-level inverse-gate (Decision 4): with a resolver wired,
// RegisterOwner rejects a Conditional/Backfill foreign edge lacking a registered
// inverse; NoBirthStub/Strict pass; with NO resolver the gate is skipped
// (observe-only fail-open). Exercises the wrapper without NATS — checkInverseGate
// touches only the resolver + logger, not the buckets.
func TestRegistry_checkInverseGate(t *testing.T) {
	resolve := func(p string) (string, bool) {
		if p == "has.inverse" {
			return "inv.of.it", true
		}
		return "", false
	}
	withResolver := &Registry{logger: slog.Default(), inverseResolver: resolve}

	condNoInv := fe("o", "no.inverse", "", EdgeConditional)
	if err := withResolver.checkInverseGate([]ForeignEdgeClaim{condNoInv}); !errors.Is(err, ErrInvalidClaim) {
		t.Errorf("Conditional edge without a registered inverse must fail the gate; got %v", err)
	}
	condWithInv := fe("o", "has.inverse", "", EdgeConditional)
	if err := withResolver.checkInverseGate([]ForeignEdgeClaim{condWithInv}); err != nil {
		t.Errorf("Conditional edge WITH a registered inverse must pass; got %v", err)
	}
	stub := fe("o", "no.inverse", "", EdgeNoBirthStub)
	if err := withResolver.checkInverseGate([]ForeignEdgeClaim{stub}); err != nil {
		t.Errorf("NoBirthStub needs no inverse, must pass; got %v", err)
	}

	// No resolver wired: the gate is skipped (fail-open), even for a Conditional
	// edge that would otherwise be rejected.
	noResolver := &Registry{logger: slog.Default()}
	if err := noResolver.checkInverseGate([]ForeignEdgeClaim{condNoInv}); err != nil {
		t.Errorf("nil resolver must skip the gate (observe-only); got %v", err)
	}
}

func TestEpoch_RoundTrip(t *testing.T) {
	ep := newEpoch()
	ep.Version = 7
	ep.Owners["cs-api"] = ownerEntry{
		Claims:       []OwnerClaim{oc("cs-api", "a.b.c.d.e.f", ModeReplaceOwned, "p")},
		ForeignEdges: []ForeignEdgeClaim{fe("cs-api", "edge", "a.b.c.d.e.*", EdgeConditional)},
	}
	b, err := ep.encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeEpoch(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 7 || len(got.Owners) != 1 {
		t.Errorf("round-trip mismatch: version=%d owners=%d", got.Version, len(got.Owners))
	}
	// Empty bytes decode to a fresh epoch, not an error.
	fresh, err := decodeEpoch(nil)
	if err != nil || fresh.Owners == nil {
		t.Errorf("empty decode should yield fresh epoch: %v", err)
	}
}
