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
	authority, err := semtypes.NewEntityDomainAuthority(EntityDomainDelegations()...)
	if err != nil {
		t.Fatalf("NewEntityDomainAuthority: %v", err)
	}
	parsed, err := semtypes.ParseEntityID(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Authorize(Producer, parsed.Domain, parsed.Type); err != nil {
		t.Fatalf("the mission harness must delegate its own domain: %v", err)
	}
}
