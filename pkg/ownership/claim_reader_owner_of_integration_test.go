//go:build integration

package ownership

import (
	"context"
	"testing"

	"github.com/c360studio/semstreams/natsclient"
)

// TestIntegration_ClaimReader_OwnerOf proves the ADR-056 PR-2 lease-reader
// seam: ClaimReader.OwnerOf resolves the LIVE owning claim's owner id and
// per-process incarnation nonce from OWNER_CLAIMS, returning (owner,
// incarnation, true, nil) for a covered (entityID, predicate) pair and
// ("", "", false, nil) for unclaimed / pattern-miss / empty-registry cases.
//
// The test drives through the real NATS KV (testcontainer) and the production
// RegisterOwner → ClaimReader.OwnerOf wire, so a deletion of the stamp loop
// in RegisterOwner or the epoch read in OwnerOf would cause failures here.
func TestIntegration_ClaimReader_OwnerOf(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	ctx := context.Background()

	reg, err := EnsureBuckets(ctx, tc.Client, nil, nil)
	if err != nil {
		t.Fatalf("EnsureBuckets: %v", err)
	}

	cr, err := NewClaimReader(ctx, tc.Client, nil)
	if err != nil {
		t.Fatalf("NewClaimReader: %v", err)
	}

	// --- Case 1: empty / absent registry → ok=false, nil err (NOT an error). ---
	owner, inc, ok, err := cr.OwnerOf(ctx, "c360.semconnect.systems.csapi.system.drone-001", "sensorml.process.label")
	if err != nil {
		t.Fatalf("OwnerOf on empty registry must not error: %v", err)
	}
	if ok || owner != "" || inc != "" {
		t.Fatalf("OwnerOf on empty registry: got (%q,%q,%v), want (\"\",\"\",false)", owner, inc, ok)
	}

	// Register cs-api as the owning owner of sensorml.process.label on sysPat
	// entities. The registry stamps its per-process incarnation onto the stored
	// claim so OwnerOf can return it.
	if err := reg.RegisterOwner(ctx, systemOwnerRegistration("cs-api")); err != nil {
		t.Fatalf("RegisterOwner: %v", err)
	}
	wantIncarnation := reg.Incarnation()

	// --- Case 2: matching entity + registered predicate → ok=true with owner + incarnation. ---
	const inPatternEntity = "c360.semconnect.systems.csapi.system.drone-001"
	owner, inc, ok, err = cr.OwnerOf(ctx, inPatternEntity, "sensorml.process.label")
	if err != nil {
		t.Fatalf("OwnerOf(claimed): unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("OwnerOf(claimed): ok=false, want true (registered owning claim should be found)")
	}
	if owner != "cs-api" {
		t.Errorf("OwnerOf(claimed): owner = %q, want %q", owner, "cs-api")
	}
	if inc != wantIncarnation {
		t.Errorf("OwnerOf(claimed): incarnation = %q, want the registry's per-process nonce %q", inc, wantIncarnation)
	}

	// --- Case 3: predicate NOT in the claim → ok=false (unclaimed). ---
	owner, inc, ok, err = cr.OwnerOf(ctx, inPatternEntity, "test.value.p")
	if err != nil {
		t.Fatalf("OwnerOf(unclaimed predicate): unexpected error: %v", err)
	}
	if ok || owner != "" || inc != "" {
		t.Fatalf("OwnerOf(unclaimed predicate): got (%q,%q,%v), want (\"\",\"\",false)", owner, inc, ok)
	}

	// --- Case 4: entity NOT matching the pattern → ok=false. ---
	// sysPat = "c360.semconnect.systems.csapi.system.*" so a deployment entity
	// does not match.
	const outOfPatternEntity = "c360.semconnect.systems.csapi.deployment.deploy-001"
	owner, inc, ok, err = cr.OwnerOf(ctx, outOfPatternEntity, "sensorml.process.label")
	if err != nil {
		t.Fatalf("OwnerOf(pattern miss): unexpected error: %v", err)
	}
	if ok || owner != "" || inc != "" {
		t.Fatalf("OwnerOf(pattern miss): got (%q,%q,%v), want (\"\",\"\",false)", owner, inc, ok)
	}

	// --- Case 5: append-evidence mode claim → ok=false (non-owning; no lease). ---
	// Register a second owner with an APPEND-EVIDENCE claim on the same entity
	// space and a different predicate, confirming the non-owning mode is
	// transparent to OwnerOf.
	if err := reg.RegisterOwner(ctx, Registration{
		Owner: "evidence-writer",
		Claims: []OwnerClaim{
			OwnerClaim{Owner: "evidence-writer", Pattern: sysPat, Mode: ModeAppendEvidence, Predicates: []string{"web.relation.backlink"}},
		},
	}); err != nil {
		t.Fatalf("RegisterOwner(append-evidence): %v", err)
	}
	owner, inc, ok, err = cr.OwnerOf(ctx, inPatternEntity, "web.relation.backlink")
	if err != nil {
		t.Fatalf("OwnerOf(append-evidence): unexpected error: %v", err)
	}
	if ok || owner != "" || inc != "" {
		t.Fatalf("OwnerOf(append-evidence): got (%q,%q,%v), want (\"\",\"\",false) — append-evidence has no owner", owner, inc, ok)
	}
}

// systemOwnerRegistration keeps the shared integration setup concise while
// leaving both governed semantic values exact at their authoring surface.
func systemOwnerRegistration(owner string) Registration {
	return Registration{
		Owner: owner,
		Claims: []OwnerClaim{{
			Owner: owner, Pattern: "c360.semconnect.systems.csapi.system.*", Mode: ModeReplaceOwned,
			Predicates: []string{"sensorml.process.label"},
		}},
	}
}
