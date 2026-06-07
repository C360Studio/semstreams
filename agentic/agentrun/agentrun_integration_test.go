//go:build integration

// Integration tests for agentic/agentrun exercising the real lifecycle.Manager
// projection path against a testcontainer NATS server (ADR-053 C2 review finding).
//
// Build tag: go test -tags=integration -race ./agentic/agentrun/...
package agentrun_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_D1_ProjectionRoundTrip verifies that lifecycle.Manager.Get
// correctly populates AgentRun.EntityIDField with the FULL 6-part chain execution
// entity ID (ADR-053 D1 critical invariant). The projection layer reads the
// `lifecycle:"id"` field from the ENTITY_STATES KV key — not from triples.
//
// This test drives the production wire path:
//  1. Write a crafted EntityState (with the agent.run.phase phase triple) directly
//     into the ENTITY_STATES KV bucket — simulating what graph-ingest would write
//     after Manager.Create emits to graph.mutation.entity.create_with_triples.
//  2. Call Manager.Get (production entry point, reads from ENTITY_STATES).
//  3. Assert EntityIDField == full 6-part chain.execution entity ID (not a bare UUID,
//     not a garbled form like TryChainExecutionEntityID would reject).
//
// Why direct KV write rather than Manager.Create → graph-ingest responder?
// Manager.Create routes through a NATS request/reply to graph-ingest. Setting up
// a full stub responder is valid but adds test surface area. The D1 invariant
// is specifically about the projection layer (KV key → EntityIDField), so
// writing the KV state directly is more focused and avoids coupling the D1
// round-trip test to the Manager.Create → graph-ingest wire contract (which
// has its own integration test in pkg/lifecycle/manager_integration_test.go).
func TestIntegration_D1_ProjectionRoundTrip(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr), "Register must succeed on a fresh manager")

	// Build the full entity ID that Mint would produce.
	const (
		org        = "acme"
		platform   = "ops"
		rootLoopID = "d1-integration-test-loop"
	)
	fullEntityID := "acme.ops.agent.chain.execution." + rootLoopID

	// Write an EntityState with the agent.run.phase triple directly into
	// ENTITY_STATES — simulating what graph-ingest writes after Manager.Create.
	// The KV key is the full entity ID.
	now := time.Now().UTC()
	state := graph.EntityState{
		ID: fullEntityID,
		Triples: []message.Triple{
			{
				Subject:    fullEntityID,
				Predicate:  agentrun.PhasePredicate, // "agent.run.phase"
				Object:     "dispatched",
				Source:     "test",
				Timestamp:  now,
				Confidence: 1.0,
			},
		},
		Version:   1,
		UpdatedAt: now,
	}
	stateBytes, err := json.Marshal(state)
	require.NoError(t, err)

	// Write to ENTITY_STATES KV bucket using the production bucket name as key.
	bucket, err := tc.Client.GetKeyValueBucket(ctx, graph.BucketEntityStates)
	require.NoError(t, err, "ENTITY_STATES bucket must be available (WithKVBuckets creates it)")

	_, err = bucket.Put(ctx, fullEntityID, stateBytes)
	require.NoError(t, err, "KV Put must succeed for the entity state")

	// Manager.Get — the production projection path.
	participant, err := mgr.Get(ctx, agentrun.WorkflowName, fullEntityID)
	require.NoError(t, err, "Manager.Get must succeed when entity state is in ENTITY_STATES")
	require.NotNil(t, participant)

	run, ok := participant.(*agentrun.AgentRun)
	require.True(t, ok, "Manager.Get must return *agentrun.AgentRun for agent-run workflow")

	// D1 critical: EntityIDField must hold the FULL 6-part chain.execution entity ID.
	// The projection layer reads this from the KV key (entityID param), NOT from triples.
	assert.Equal(t, fullEntityID, run.EntityIDField,
		"D1: EntityIDField must hold the full 6-part entity ID populated from the KV key")
	assert.Equal(t, "dispatched", run.PhaseField,
		"PhaseField must be projected from the agent.run.phase triple")
	assert.False(t, run.IsTerminal(), "dispatched must not be terminal")

	// RunID() must derive the bare loop ID from EntityIDField — not a dot-separated path.
	runID, ok := run.RunID()
	require.True(t, ok, "RunID() must succeed for a valid chain.execution entity ID")
	assert.Equal(t, rootLoopID, runID,
		"RunID() must return the bare loop UUID, not the full entity ID")
	assert.NotContains(t, runID, ".",
		"bare RunID must not contain dots — if it does, D1 EntityIDField holds a bare ID, not the full 6-part ID")
}
