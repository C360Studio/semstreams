package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// spec: e2e-evidence / Selection determines proof scope
func TestResolveScenarioSelectionPreservesScopeAndCallerOptions(t *testing.T) {
	tests := []struct {
		name        string
		scenario    string
		variant     string
		wantName    string
		wantLabel   string
		wantVariant string
		wantMembers []string
		required    bool
	}{
		{name: "default core", wantName: "all", wantLabel: "core-scenarios", wantMembers: []string{"core-health", "core-dataflow"}, required: true},
		{name: "all means core", scenario: "all", wantName: "all", wantLabel: "core-scenarios", wantMembers: []string{"core-health", "core-dataflow"}, required: true},
		{name: "semantic alias", scenario: "semantic", wantName: "tiered", wantLabel: "semantic", wantVariant: "semantic", wantMembers: []string{"tiered:semantic"}, required: true},
		{name: "rules alias", scenario: "rules", wantName: "tiered", wantLabel: "structural", wantVariant: "structural", wantMembers: []string{"tiered:structural"}, required: true},
		{name: "explicit statistical", scenario: "tiered", variant: "statistical", wantName: "tiered", wantLabel: "statistical", wantVariant: "statistical", wantMembers: []string{"tiered:statistical"}, required: true},
		{name: "named structural", scenario: "structural", wantName: "tiered", wantLabel: "structural", wantVariant: "structural", wantMembers: []string{"tiered:structural"}, required: true},
		{name: "health alias", scenario: "health", wantName: "health", wantLabel: "core-health", wantMembers: []string{"core-health"}, required: true},
		{name: "minted authority alias", scenario: "minted-authority", wantName: "core-minted-authority", wantLabel: "core-minted-authority", wantMembers: []string{"core-minted-authority"}},
		{name: "preidentity seed", scenario: "core-pre-identity-seed", wantName: "core-pre-identity-seed", wantLabel: "core-pre-identity-bucket-seed", wantMembers: []string{"core-pre-identity-bucket-seed"}},
		{name: "preidentity assert", scenario: "core-pre-identity-assert", wantName: "core-pre-identity-assert", wantLabel: "core-pre-identity-bucket-assert", wantMembers: []string{"core-pre-identity-bucket-assert"}},
		{name: "legacy", scenario: "lessons", wantName: "lessons", wantLabel: "lessons", wantMembers: []string{"lessons"}},
		{name: "fallback diagnostic", scenario: "tiered", variant: "semantic-fallback", wantName: "tiered", wantLabel: "semantic-fallback", wantVariant: "semantic-fallback", wantMembers: []string{"tiered:semantic-fallback"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := &cliFlags{
				scenarioName: tc.scenario,
				variant:      tc.variant,
				outputDir:    t.TempDir(),
				baseURL:      "http://127.0.0.1:38123",
				udpEndpoint:  "127.0.0.1:38124",
				metricsURL:   "http://127.0.0.1:38125",
				wsStatusURL:  "ws://127.0.0.1:38126",
			}
			selection, err := resolveScenarioSelection(input)
			require.NoError(t, err)
			require.Equal(t, tc.wantName, selection.flags.scenarioName)
			require.Equal(t, tc.wantLabel, selection.label)
			require.Equal(t, tc.wantVariant, selection.flags.variant)
			require.True(t, slices.Equal(tc.wantMembers, selection.members), "selected execution members")
			require.Equal(t, tc.required, selection.required)
			require.Equal(t, input.outputDir, selection.flags.outputDir)
			require.Equal(t, input.baseURL, selection.flags.baseURL)
			require.Equal(t, input.udpEndpoint, selection.flags.udpEndpoint)
			require.Equal(t, input.metricsURL, selection.flags.metricsURL)
			require.Equal(t, input.wsStatusURL, selection.flags.wsStatusURL)
			require.Equal(t, tc.scenario, input.scenarioName, "resolution must not rewrite caller input")
		})
	}
}

func TestResolvedCoreIdentityCommandsMatchConstructedScenario(t *testing.T) {
	for _, name := range []string{"minted-authority", "core-minted-authority", "core-pre-identity-seed", "core-pre-identity-assert"} {
		t.Run(name, func(t *testing.T) {
			selection, err := resolveScenarioSelection(&cliFlags{scenarioName: name})
			require.NoError(t, err)
			constructed := createScenario(nil, &selection.flags)
			require.NotNil(t, constructed)
			require.Equal(t, constructed.Name(), selection.members[0])
		})
	}
}

// spec: e2e-evidence / Selection determines proof scope
func TestResolveScenarioSelectionRejectsAmbiguousOrUnknownVariants(t *testing.T) {
	for _, tc := range []struct {
		name     string
		scenario string
		variant  string
	}{
		{name: "tiered without variant", scenario: "tiered"},
		{name: "unknown tiered variant", scenario: "tiered", variant: "mystery"},
		{name: "semantic conflict", scenario: "semantic", variant: "structural"},
		{name: "rules conflict", scenario: "rules", variant: "statistical"},
		{name: "core with tiered variant", scenario: "all", variant: "semantic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveScenarioSelection(&cliFlags{scenarioName: tc.scenario, variant: tc.variant})
			require.Error(t, err)
		})
	}
}
