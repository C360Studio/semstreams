//go:build integration

// #1096 / ADR-102 d2 wiring pin (review HIGH-1).
//
// The foreign-authority guard is only as good as the hop that gives the
// executor its authority. That hop used to be a setter call in
// initializeStateTracker, and deleting it left BOTH suites green: the executor
// silently answered "not foreign" for every entity, because the run-scope
// integration harness set the platform itself and no shipped config uses
// run_scope: new.
//
// The authority is now a CONSTRUCTOR parameter, so the hop cannot be deleted
// without a compile error — but it can still be passed the WRONG value (a zero
// PlatformMeta compiles fine). This test drives the production factory and
// pins what actually arrives.

package rule

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

// TestIntegration_ProductionExecutorCarriesTheDeploymentAuthority drives
// CreateRuleProcessor -> initializeStateTracker against real NATS and asserts
// the executor the processor actually dispatches through holds deps.Platform.
//
// It runs over BOTH production branches. initializeStateTracker constructs the
// writing executor twice — once with a triple mutator when
// enable_graph_integration is set, once without — and each passes the authority
// separately. A single-branch test looked like coverage and was not: the first
// draft of this test exercised only the default (false) branch, and a mutation
// zeroing the authority on the OTHER branch was NOT KILLED.
func TestIntegration_ProductionExecutorCarriesTheDeploymentAuthority(t *testing.T) {
	platform := types.PlatformMeta{Org: "acme", Platform: "dep1"}

	for _, graphIntegration := range []bool{false, true} {
		name := "graph_integration_disabled"
		if graphIntegration {
			name = "graph_integration_enabled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			testClient := natsclient.NewTestClient(t, natsclient.WithKV())
			rawConfig, err := json.Marshal(map[string]any{
				"pack_id":                  "wiring-pin",
				"enable_graph_integration": graphIntegration,
			})
			require.NoError(t, err)

			discoverable, err := CreateRuleProcessor(rawConfig, component.Dependencies{
				NATSClient: testClient.Client,
				Platform:   platform,
			})
			require.NoError(t, err)
			processor, ok := discoverable.(*Processor)
			require.True(t, ok, "CreateRuleProcessor must yield a *Processor")

			require.NoError(t, processor.initializeStateTracker(ctx),
				"state-tracker init is where the production executor is constructed")

			executor, ok := processor.actionExecutor.(*ActionExecutor)
			require.True(t, ok, "the production path must build a concrete *ActionExecutor")

			// The branch under test is the one this config selects.
			require.Equal(t, graphIntegration, executor.tripleMutator != nil,
				"the fixture must actually reach the intended construction branch")

			require.Equal(t, platform, executor.platform,
				"the executor the processor dispatches through must carry deps.Platform; "+
					"a zero value here retires the foreign-authority guard for every entity (#1096)")

			// The guard is live, not merely populated: it must discriminate.
			require.False(t, executor.foreignFiringEntity("acme.dep1.agentic-loop.agent.execution.local1"),
				"an entity under the deployment's own authority is not foreign")
			require.True(t, executor.foreignFiringEntity("foreign.dep9.agentic-loop.agent.execution.import1"),
				"a peer's entity IS foreign; if this is false the guard is retired and the "+
					"framework will write to an imported mirror")
		})
	}
}
