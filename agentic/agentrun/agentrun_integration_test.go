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

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type integrationMilestoneHandler struct {
	events chan agentrun.LoopTerminalEvent
}

func (h *integrationMilestoneHandler) OnLoopTerminal(_ context.Context, event agentrun.LoopTerminalEvent, _ *agentrun.AgentRun) error {
	h.events <- event
	return nil
}

// TestIntegration_D1_ProjectionRoundTrip verifies that lifecycle.Manager.Get
// correctly populates AgentRun.EntityIDField with the FULL 6-part chain execution
// entity ID (ADR-053 D1 critical invariant). The projection layer reads the
// `lifecycle:"id"` field from the ENTITY_STATES KV key — not from triples.
//
// This test drives the production authority-read path:
//  1. Serve a crafted ExactEntity (with the agent.run.phase phase triple) on
//     graph.ingest.query.entity, as graph-ingest does.
//  2. Call Manager.Get (production entry point, uses the exact reader).
//  3. Assert EntityIDField == full 6-part chain.execution entity ID (not a bare UUID,
//     not a garbled form like TryChainExecutionEntityID would reject).
//
// The D1 invariant is specifically about exact authority projection, so this
// test does not add mutation behavior already covered by lifecycle integration.
func TestIntegration_D1_ProjectionRoundTrip(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
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

	// Build the exact authority value returned by graph-ingest.
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
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.ingest.query.entity",
		func(_ context.Context, request []byte) ([]byte, error) {
			var query struct {
				ID string `json:"id"`
			}
			if decodeErr := json.Unmarshal(request, &query); decodeErr != nil {
				return nil, decodeErr
			}
			require.Equal(t, fullEntityID, query.ID)
			return json.Marshal(graph.ExactEntity{Entity: state.Clone(), KVRevision: 1})
		})
	require.NoError(t, err)

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

// TestIntegration_MilestoneSubscriber_GracefulSkipWhenStreamAbsent pins gh#246:
// Start MUST NOT abort boot when the AGENT stream is absent (a deployment with no
// agentic components — e.g. graph/lifecycle-only). It logs and returns a no-op stop
// + nil error instead of the "stream not found" error that previously propagated out
// of run() in both binaries → os.Exit(1) (the silent-red e2e:lifecycle/structural
// tiers and the latent production boot failure).
func TestIntegration_MilestoneSubscriber_GracefulSkipWhenStreamAbsent(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	// The reader is unused on the stream-absent early-return path; nil logger
	// is defaulted by the constructor.
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{StreamName: agentrun.AgentStreamName})
	require.NoError(t, err, "Start must not error when the AGENT stream is absent (gh#246)")
	require.NotNil(t, stop, "Start must return a non-nil (no-op) stop func when skipping")
	require.NoError(t, stop(ctx))

	// And it did not CREATE the stream to make the error go away. This subscriber is
	// the framework's reference read-only consumer, and a stream's limits belong to
	// the component that declares it: a consumer that reached for get-or-create
	// would either invent limits it does not own or have its own declaration
	// silently discarded, after which the stream's limits are decided by boot order.
	_, getErr := tc.Client.GetStream(ctx, agentrun.AgentStreamName)
	require.ErrorIs(t, getErr, jetstream.ErrStreamNotFound,
		"a read-only consumer must bind by name, never provision the stream it reads")
}

// TestIntegration_MilestoneSubscriber_StartsWhenStreamPresent confirms the normal
// path is unchanged by the gh#246 graceful-skip: with the AGENT stream present,
// Start wires the durable consumers and returns a real stop.
func TestIntegration_MilestoneSubscriber_StartsWhenStreamPresent(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithKVBuckets(graph.BucketEntityStates))
	ctx := context.Background()

	_, err := tc.CreateStream(ctx, agentrun.AgentStreamName, []string{"agent.>"})
	require.NoError(t, err, "create AGENT stream")

	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)

	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{
		StreamName:         agentrun.AgentStreamName,
		ConsumerNameSuffix: "gh246-present",
	})
	require.NoError(t, err, "Start must succeed when the AGENT stream is present")
	require.NotNil(t, stop)
	require.NoError(t, stop(ctx))
}

func TestIntegration_MilestoneSubscriberProductionEnvelopeCallbacks(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithStreams(
		natsclient.TestStreamConfig{Name: agentrun.AgentStreamName, Subjects: []string{"agent.>"}},
	))
	ctx := t.Context()
	mgr := lifecycle.NewManager(tc.Client, nil)
	require.NoError(t, agentrun.Register(mgr))
	sub := agentrun.NewMilestoneSubscriber(mgr, nil, "acme", "ops", nil)
	handler := &integrationMilestoneHandler{events: make(chan agentrun.LoopTerminalEvent, 3)}
	sub.AddHandler(handler)
	stop, err := sub.Start(ctx, tc.Client, agentrun.StartConfig{StreamName: agentrun.AgentStreamName, ConsumerNameSuffix: "terminal-production"})
	require.NoError(t, err)
	defer func() { require.NoError(t, stop(ctx)) }()

	at := time.Now().UTC()
	payloads := []message.Payload{
		&agentic.LoopCompletedEvent{LoopID: "run-success", TaskID: "task-success", Outcome: agentic.OutcomeSuccess, CompletedAt: at, RunEntityID: "missing-run-success"},
		&agentic.LoopFailedEvent{LoopID: "run-failed", TaskID: "task-failed", Outcome: agentic.OutcomeFailed, FailedAt: at, RunEntityID: "missing-run-failed"},
		&agentic.LoopCancelledEvent{LoopID: "run-cancelled", TaskID: "task-cancelled", Outcome: agentic.OutcomeCancelled, CancelledAt: at, RunEntityID: "missing-run-cancelled"},
	}
	for _, payload := range payloads {
		data, marshalErr := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "agentic-loop"))
		require.NoError(t, marshalErr)
		subjectPrefix := "agent.complete."
		if payload.Schema().Category == agentic.CategoryLoopFailed {
			subjectPrefix = "agent.failed."
		}
		require.NoError(t, tc.Client.PublishToStream(ctx, subjectPrefix+payload.Schema().Category, data))
	}

	want := map[string]bool{
		agentic.CategoryLoopCompleted: false,
		agentic.CategoryLoopFailed:    false,
		agentic.CategoryLoopCancelled: false,
	}
	for range 3 {
		select {
		case event := <-handler.events:
			want[event.Category] = true
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for AgentRun production-envelope callback")
		}
	}
	for category, observed := range want {
		require.True(t, observed, category)
	}
}
