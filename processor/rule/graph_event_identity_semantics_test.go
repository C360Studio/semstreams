package rule

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/types"
)

// TestRuleTriggerIdentityCarriesTheDeploymentAuthority pins ADR-102 d2 for the
// trigger family: the digest still covers only (packID, ruleID) so replicas of
// one pack in ONE deployment converge, but two deployments running the same
// pack mint different entities because positions 1-2 are the deployment's own
// authority, built from the pkg/types family table rather than a literal.
func TestRuleTriggerIdentityCarriesTheDeploymentAuthority(t *testing.T) {
	t.Parallel()

	first, err := ruleTriggerEntityID("acme", "dep1", "contract-pack", "rule-a")
	if err != nil {
		t.Fatalf("ruleTriggerEntityID: %v", err)
	}
	replica, err := ruleTriggerEntityID("acme", "dep1", "contract-pack", "rule-a")
	if err != nil {
		t.Fatalf("replica ruleTriggerEntityID: %v", err)
	}
	other, err := ruleTriggerEntityID("acme", "dep2", "contract-pack", "rule-a")
	if err != nil {
		t.Fatalf("other-deployment ruleTriggerEntityID: %v", err)
	}
	if first != replica {
		t.Fatalf("replica ID = %q, want %q", replica, first)
	}
	if first == other {
		t.Fatalf("two deployments converged on one trigger entity %q", first)
	}
	if !strings.HasPrefix(first, "acme.dep1.rules.graph.trigger.") {
		t.Fatalf("trigger ID = %q, want the acme.dep1.rules.graph.trigger. prefix", first)
	}
	if strings.Contains(first, "semstreams.framework") {
		t.Fatalf("trigger ID %q still carries the retired framework namespace", first)
	}
	if err := types.ValidateEntityID(first); err != nil {
		t.Fatalf("derived trigger ID is not canonical: %v", err)
	}
	family := types.RuleTriggerIdentityFamily()
	if want := len("acme") + len("dep1") + family.FixedBytes(); len(first) != want {
		t.Fatalf("trigger ID length = %d, want %d (authority + family fixed bytes)", len(first), want)
	}
	for _, bad := range [][2]string{{"", "dep1"}, {"acme", ""}, {"ac.me", "dep1"}, {"acme", "-dep1"}} {
		if id, err := ruleTriggerEntityID(bad[0], bad[1], "contract-pack", "rule-a"); err == nil || id != "" {
			t.Fatalf("authority %q/%q = (%q, %v), want (\"\", error)", bad[0], bad[1], id, err)
		}
	}
}
