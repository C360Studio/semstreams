package mission

import (
	"testing"

	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// TestMissionIdentityFollowsCanonicalOrder pins the e2e mission family in the
// canonical order: system = gcs (the source), domain = lifecycle (a delegated
// taxonomy declared by this harness, not a framework-reserved domain).
func TestMissionIdentityFollowsCanonicalOrder(t *testing.T) {
	t.Parallel()

	const want = "c360.test.gcs.lifecycle.mission.m001"
	if got := EntityIDFor("c360", "test", "m001"); got != want {
		t.Fatalf("EntityIDFor = %q, want %q", got, want)
	}
	if EntityIDPattern != "*.*.gcs.lifecycle.mission.*" {
		t.Fatalf("EntityIDPattern = %q, want *.*.gcs.lifecycle.mission.*", EntityIDPattern)
	}
	matched, err := semtypes.MatchEntityIDPattern(EntityIDPattern, want)
	if err != nil || !matched {
		t.Fatalf("MatchEntityIDPattern(%q, %q) = (%v, %v), want match", EntityIDPattern, want, matched, err)
	}
	parsed, err := semtypes.ParseEntityID(want)
	if err != nil {
		t.Fatal(err)
	}
	// The harness declares its own taxonomy rather than borrowing a
	// framework-reserved one; the audit reads that declaration for its
	// registered set. There is no Authorize call to make: the authority policy
	// was deleted by the owner ruling of 2026-08-28.
	if semtypes.IsFrameworkEntityDomain(parsed.Domain) {
		t.Fatalf("domain %q is framework-reserved; the harness must delegate its own", parsed.Domain)
	}
	declared := false
	for _, delegation := range EntityDomainDelegations() {
		if delegation.Producer == Producer && delegation.Domain == parsed.Domain {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("the mission harness must declare %q for producer %q; delegations = %+v",
			parsed.Domain, Producer, EntityDomainDelegations())
	}
}
