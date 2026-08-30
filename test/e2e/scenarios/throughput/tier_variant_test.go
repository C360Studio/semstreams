package throughput

import (
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/test/e2e/config"
)

// throughputTaskfile is the task definition that decides which compose profile
// — and therefore which deployment authority — this scenario runs against.
const throughputTaskfile = "../../../../taskfiles/e2e/throughput.yml"

// composeProfile matches the `--profile <name>` argument of a compose command.
var composeProfile = regexp.MustCompile(`--profile\s+([a-z0-9-]+)`)

// TestTierEntityMatchesTheProfileTheTaskBringsUp pins the one prediction the
// throughput scenario still makes. Setup READS the deployment authority from
// the running stack (ADR-104), but it has to say which tier's stem it expects,
// and the scenario has no --variant flag; throughputVariant names it
// statically. This test requires that name to equal the compose profile every
// task in throughput.yml actually brings up. Point the task at a different
// profile and Setup fails with "the stack under test is not the configuration
// this scenario names" — loud, but at run time; this fails in a second.
func TestTierEntityMatchesTheProfileTheTaskBringsUp(t *testing.T) {
	body, err := os.ReadFile(throughputTaskfile)
	if err != nil {
		t.Fatalf("read %s: %v", throughputTaskfile, err)
	}

	matches := composeProfile.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		t.Fatalf("no --profile argument found in %s; the task shape or the regex changed", throughputTaskfile)
	}

	seen := map[string]bool{}
	for _, match := range matches {
		seen[match[1]] = true
	}
	profiles := make([]string, 0, len(seen))
	for profile := range seen {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)

	if len(profiles) != 1 {
		t.Fatalf("%s brings up %v; tierEntity can only be right about one", throughputTaskfile, profiles)
	}

	// Assert through the value Setup actually resolves against, not against a
	// literal that happens to match today.
	if throughputVariant != profiles[0] {
		t.Errorf("throughput brings up the %q profile, but Setup reads the deployment authority for %q",
			profiles[0], throughputVariant)
	}
	// The stem the observation cross-checks against must still be the one that
	// profile boots; TestTierAuthorityMatchesShippedConfigs owns the rest.
	if stem := config.TierAuthorityStem(throughputVariant); stem == "" {
		t.Errorf("no authority stem registered for %q", throughputVariant)
	}
}
