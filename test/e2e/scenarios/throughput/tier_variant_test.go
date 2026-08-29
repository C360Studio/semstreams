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

// TestTierEntityMatchesTheProfileTheTaskBringsUp turns tierEntity's variant
// from a prediction into an observation. The scenario has no --variant flag
// and no runtime way to ask the deployment who it is, so tierEntity names the
// tier statically; this test requires that name to equal the compose profile
// every task in throughput.yml actually brings up. Point the task at a
// different profile and the deployment authority changes under the fixtures —
// which surfaces as "entity not found" mid-run rather than as a compile error.
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

	// Assert through tierEntity itself, not against the constant it happens to
	// name: a test that compares config.VariantStatistical to the taskfile
	// passes even when tierEntity is changed to mint under a different tier.
	want := config.TierAuthority(profiles[0]) + ".sensor.environmental.temperature.temp-sensor-001"
	if got := tierEntity("sensor.environmental.temperature.temp-sensor-001"); got != want {
		t.Errorf("throughput brings up the %q profile, so its fixtures must be %q; tierEntity produced %q",
			profiles[0], want, got)
	}
}
