package ownership

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/internal/semantictest"
)

func TestOwnerClaim_Validate(t *testing.T) {
	good := OwnerClaim{Owner: "cs-api", Pattern: "c360.semconnect.systems.csapi.system.*", Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed claim should validate: %v", err)
	}

	bad := []struct {
		name  string
		claim OwnerClaim
	}{
		{"empty owner", OwnerClaim{Owner: "", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
		{"5-part pattern", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
		{"bare star pattern", OwnerClaim{Owner: "o", Pattern: "*", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
		{"partial wildcard pattern", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e.foo*", Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
		{"leading underscore pattern", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e._bad", Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}}},
		{"no predicates", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned}},
		{"empty predicate", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{""}}},           // predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{"wildcard predicate", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{"agent.*"}}}, // predicate-audit:invalid {"kind":"stored-predicate","value":"agent.*","reason":"arity"}
		{"invalid mode", OwnerClaim{Owner: "o", Pattern: "a.b.c.d.e.f", Mode: WriteMode("bogus"), Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
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
	good := ForeignEdgeClaim{Owner: "cs-api", Predicate: "sensorml.component.is-hosted-by", TargetPattern: "c360.semconnect.systems.csapi.system.*", Mode: EdgeNoBirthStub}
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed foreign-edge claim should validate: %v", err)
	}
	// Empty target pattern (match-any) is valid.
	if err := (ForeignEdgeClaim{Owner: "o", Predicate: "test.edge.p", TargetPattern: "", Mode: EdgeConditional}).Validate(); err != nil { // entity-id-audit:classify intentional-sentinel "" line=46 column=83 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
		t.Errorf("empty target pattern should be valid (match-any): %v", err)
	}

	bad := []struct {
		name string
		fec  ForeignEdgeClaim
	}{
		{"empty owner", ForeignEdgeClaim{Owner: "", Predicate: semantictest.Predicate(t, "test", "edge", "p"), TargetPattern: "", Mode: EdgeConditional}},                       // entity-id-audit:classify intentional-sentinel "" line=54 column=121 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
		{"empty predicate", ForeignEdgeClaim{Owner: "o", Predicate: "", TargetPattern: "", Mode: EdgeConditional}},                                                              // entity-id-audit:classify intentional-sentinel "" line=55 column=82 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel; predicate-audit:invalid {"kind":"stored-predicate","value":"","reason":"empty"}
		{"wildcard predicate", ForeignEdgeClaim{Owner: "o", Predicate: "p.*", TargetPattern: "", Mode: EdgeConditional}},                                                        // entity-id-audit:classify intentional-sentinel "" line=56 column=88 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel; predicate-audit:invalid {"kind":"stored-predicate","value":"p.*","reason":"arity"}
		{"invalid mode", ForeignEdgeClaim{Owner: "o", Predicate: "test.edge.p", TargetPattern: "", Mode: EdgeMode("bogus")}},                                                    // entity-id-audit:classify intentional-sentinel "" line=57 column=90 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
		{"bad target pattern", ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "test", "edge", "p"), TargetPattern: "a.b.c", Mode: EdgeConditional}},          // entity-id-audit:classify intentional-malformed "a.b.c" line=58 column=129 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:arity short target rejection fixture
		{"partial wildcard target pattern", ForeignEdgeClaim{Owner: "o", Predicate: "sensorml.component.is-hosted-by", TargetPattern: "a.b.c.d.e.foo*", Mode: EdgeConditional}}, // entity-id-audit:classify intentional-malformed "a.b.c.d.e.foo*" line=59 column=129 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:alphabet partial wildcard rejection fixture
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
	good := CoordinationWaiver{Owner: "a", With: "b", Predicates: []string{"test.value.p"}, Reason: "because"}
	if err := good.Validate(); err != nil {
		t.Fatalf("well-formed waiver should validate: %v", err)
	}
	bad := []struct {
		name string
		w    CoordinationWaiver
	}{
		{"no with", CoordinationWaiver{Owner: "a", Predicates: []string{"test.value.p"}, Reason: "r"}},
		{"no predicates", CoordinationWaiver{Owner: "a", With: "b", Reason: "r"}},
		{"no reason", CoordinationWaiver{Owner: "a", With: "b", Predicates: []string{"test.value.p"}}},
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
		OwnerClaim{Owner: "cs-api", Pattern: "c360.semconnect.systems.csapi.system.*", Mode: ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}},
		OwnerClaim{Owner: "cs-api", Pattern: "c360.semconnect.systems.csapi.system.*", Mode: ModeAppendEvidence, Predicates: []string{semantictest.Predicate(t, "evidence", "annotation", "note")}},
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
		{Owner: "p", Predicate: semantictest.Predicate(t, "test", "edge", "specific"), Mode: EdgeConditional, Producer: "type.a"},
		{Owner: "p", Predicate: semantictest.Predicate(t, "test", "edge", "any"), Mode: EdgeStrict, Producer: ""},
	}}

	if c, ok := ep.foreignEdgeClaimFor("type.a", semantictest.Predicate(t, "test", "edge", "specific")); !ok || c.Producer != "type.a" {
		t.Errorf("exact producer match: got %+v ok=%v", c, ok)
	}
	if _, ok := ep.foreignEdgeClaimFor("type.b", semantictest.Predicate(t, "test", "edge", "specific")); ok {
		t.Error("a producer-specific claim must not match a different producer")
	}
	if _, ok := ep.foreignEdgeClaimFor("any.type.at.all", semantictest.Predicate(t, "test", "edge", "any")); !ok {
		t.Error("an empty-producer (any) claim should match any message type")
	}
	if _, ok := ep.foreignEdgeClaimFor("type.a", semantictest.Predicate(t, "test", "edge", "unknown")); ok {
		t.Error("an unknown predicate must not match")
	}
}

func TestEpoch_ForeignEdgeClaimFor_ExactBeatsAny(t *testing.T) {
	ep := newEpoch()
	ep.Owners["p1"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "p1", Predicate: semantictest.Predicate(t, "test", "edge", "e"), Mode: EdgeStrict, Producer: ""}}}
	ep.Owners["p2"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "p2", Predicate: semantictest.Predicate(t, "test", "edge", "e"), Mode: EdgeConditional, Producer: "type.x"}}}
	c, ok := ep.foreignEdgeClaimFor("type.x", semantictest.Predicate(t, "test", "edge", "e"))
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
	ep.Owners["zeta"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "zeta", Predicate: semantictest.Predicate(t, "test", "edge", "e"), Mode: EdgeStrict, Producer: ""}}}
	ep.Owners["alpha"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{{Owner: "alpha", Predicate: semantictest.Predicate(t, "test", "edge", "e"), Mode: EdgeConditional, Producer: ""}}}

	first, ok := ep.foreignEdgeClaimFor("any.type", semantictest.Predicate(t, "test", "edge", "e"))
	if !ok {
		t.Fatal("expected a match")
	}
	for i := 0; i < 50; i++ {
		got, _ := ep.foreignEdgeClaimFor("any.type", semantictest.Predicate(t, "test", "edge", "e"))
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
	ep.Owners["live"] = ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "live", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}}}
	ep.Owners["dead"] = ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "dead", Pattern: "a.b.c.d.e.g", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}}}
	ep.Owners["registrant"] = ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "registrant", Pattern: "a.b.c.d.e.h", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}}}

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
	ep.Owners["fe-only"] = ownerEntry{ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "fe-only", Predicate: semantictest.Predicate(t, "test", "edge", "x"), TargetPattern: "", Mode: EdgeNoBirthStub}}} // entity-id-audit:classify intentional-sentinel "" line=202 column=178 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
	ep.Owners["mixed"] = ownerEntry{
		Claims:       []OwnerClaim{{Owner: "mixed", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
		ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "mixed", Predicate: semantictest.Predicate(t, "test", "edge", "y"), TargetPattern: "", Mode: EdgeStrict}}, // entity-id-audit:classify intentional-sentinel "" line=205 column=143 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
	}
	ep.Owners["owning-dead"] = ownerEntry{Claims: []OwnerClaim{OwnerClaim{Owner: "owning-dead", Pattern: "a.b.c.d.e.g", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}}}
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
		if p == "test.edge.has-inverse" {
			return "inv.of.it", true
		}
		return "", false
	}
	withResolver := &Registry{logger: slog.Default(), inverseResolver: resolve}

	condNoInv := ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "test", "edge", "no-inverse"), TargetPattern: "", Mode: EdgeConditional} // entity-id-audit:classify intentional-sentinel "" line=248 column=127 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
	if err := withResolver.checkInverseGate([]ForeignEdgeClaim{condNoInv}); !errors.Is(err, ErrInvalidClaim) {
		t.Errorf("Conditional edge without a registered inverse must fail the gate; got %v", err)
	}
	condWithInv := ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "test", "edge", "has-inverse"), TargetPattern: "", Mode: EdgeConditional} // entity-id-audit:classify intentional-sentinel "" line=252 column=130 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
	if err := withResolver.checkInverseGate([]ForeignEdgeClaim{condWithInv}); err != nil {
		t.Errorf("Conditional edge WITH a registered inverse must pass; got %v", err)
	}
	stub := ForeignEdgeClaim{Owner: "o", Predicate: semantictest.Predicate(t, "test", "edge", "no-inverse"), TargetPattern: "", Mode: EdgeNoBirthStub} // entity-id-audit:classify intentional-sentinel "" line=256 column=122 surface=go-field:ForeignEdgeClaim.TargetPattern entity_id_pattern_invalid:empty empty target is the match-any sentinel
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
		Claims:       []OwnerClaim{OwnerClaim{Owner: "cs-api", Pattern: "a.b.c.d.e.f", Mode: ModeReplaceOwned, Predicates: []string{semantictest.Predicate(t, "test", "value", "p")}}},
		ForeignEdges: []ForeignEdgeClaim{ForeignEdgeClaim{Owner: "cs-api", Predicate: semantictest.Predicate(t, "test", "edge", "related"), TargetPattern: "a.b.c.d.e.*", Mode: EdgeConditional}},
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
