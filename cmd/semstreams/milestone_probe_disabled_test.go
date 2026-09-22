//go:build !e2e_process_barrier

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMilestoneProbeIsInertWithoutTag pins the compile-time half of the probe's
// two gates. Passing nils is the point: an ordinary build must not reach code
// that would dereference them.
func TestMilestoneProbeIsInertWithoutTag(t *testing.T) {
	require.NoError(t, registerE2EMilestoneProbe(nil, nil, nil))
}

// TestDefaultMilestoneProbeFileDoesNotImportHarness is the guard that keeps the
// gate a build constraint rather than a comment: the ordinary cmd/semstreams
// dependency graph must not reach the E2E harness at all, and the tagged file
// must be the only one that does.
func TestDefaultMilestoneProbeFileDoesNotImportHarness(t *testing.T) {
	disabled, err := os.ReadFile("milestone_probe_disabled.go")
	require.NoError(t, err, "read default milestone probe file")
	require.Contains(t, string(disabled), "//go:build !e2e_process_barrier",
		"default milestone probe file lacks the negative build constraint")
	require.NotContains(t, string(disabled), "test/e2e/harness/milestoneprobe",
		"default cmd/semstreams build imports the E2E milestone probe harness")

	tagged, err := os.ReadFile("milestone_probe_e2e.go")
	require.NoError(t, err, "read tagged milestone probe file")
	require.Contains(t, string(tagged), "//go:build e2e_process_barrier",
		"tagged milestone probe file lacks the positive build constraint")
	require.Contains(t, string(tagged), "test/e2e/harness/milestoneprobe",
		"tagged milestone probe file does not import the harness")
}

// TestAgenticComposeArmsTheMilestoneProbe pins the runtime half. The tag alone
// leaves the probe inert, so a compose file that stopped setting the variable
// would take the whole #1155 stage-D proof with it — silently, because an
// unarmed probe records nothing rather than failing.
func TestAgenticComposeArmsTheMilestoneProbe(t *testing.T) {
	compose, err := os.ReadFile("../../docker/compose/agentic.yml")
	require.NoError(t, err, "read agentic compose file")
	require.Contains(t, string(compose), "SEMSTREAMS_E2E_MILESTONE_PROBE=1",
		"the agentic tier does not arm the milestone settlement probe")

	// Every OTHER compose file must leave it unset: the probe crashes and
	// quarantines on purpose, so an arming leak would look like a flake.
	entries, err := os.ReadDir("../../docker/compose")
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "agentic.yml" || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		body, readErr := os.ReadFile("../../docker/compose/" + entry.Name())
		require.NoError(t, readErr)
		require.NotContains(t, string(body), "SEMSTREAMS_E2E_MILESTONE_PROBE",
			"%s arms the milestone settlement probe", entry.Name())
	}
}
