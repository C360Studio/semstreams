package scenarios

import (
	"context"
	"testing"
)

// These constructor tests use the same scenarios the CLI selects. Invalid
// prerequisites force an observed failed check before a live stack is needed.
func TestCoreTaskScenariosDeclareAndRetainFailedEvidence(t *testing.T) {
	tests := []struct {
		name     string
		scenario Scenario
		checkID  string
	}{
		{name: "graph", scenario: NewGraphRoundTripScenario("", "", "", "acme.test", "run-1", "core-graph-roundtrip"),
			checkID: "core.graph-roundtrip.identity"},
		{name: "minted", scenario: NewMintedAuthorityScenario("", "invalid", "run-1", "core-minted-authority"),
			checkID: "core.minted-authority"},
		{name: "preidentity", scenario: NewPreIdentityBucketScenario("", "assert", "invalid", "run-1", "core-pre-identity-bucket-assert"),
			checkID: "core.preidentity.no-record"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			declarer, ok := tc.scenario.(interface{ CheckRequirements() []CheckRequirement })
			if !ok {
				t.Fatal("selected required scenario has no declarations")
			}
			checks := declarer.CheckRequirements()
			if len(checks) != 1 || checks[0].ID != tc.checkID || !checks[0].Required {
				t.Fatalf("required declarations = %+v", checks)
			}
			result, err := tc.scenario.Execute(context.Background())
			if err != nil {
				t.Fatalf("Execute error = %v", err)
			}
			if len(result.CheckObservations) != 1 || result.CheckObservations[0].Status != "failed" ||
				result.CheckObservations[0].ID != tc.checkID || result.CheckObservations[0].Reason == "" {
				t.Fatalf("failed observation = %+v", result.CheckObservations)
			}
			if err := result.FinalizeChecks(); err == nil || result.Success {
				t.Fatalf("failed required check finalized green: err=%v result=%+v", err, result)
			}
		})
	}
}
