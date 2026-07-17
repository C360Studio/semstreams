//go:build integration

// Package rule — integration tests for the replace_owned action (ADR-056
// Decision 3) driven against a REAL graph-ingest update_with_triples handler.
// These exercise the atomic-write contract (exactly-one value, clear, must-exist)
// and the processor owner-wiring path through live NATS/KV.
package rule

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/vocabulary/rulepacks"
)

// startGraphIngest stands up a real graph-ingest Component bound to the test
// NATS client and returns it (already Started). It owns the
// graph.mutation.entity.update_with_triples handler the rule mutator drives.
func startGraphIngest(t *testing.T, natsClient *natsclient.Client) *graphingest.Component {
	t.Helper()
	ctx := context.Background()

	cfg := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)

	gi := comp.(*graphingest.Component)
	require.NoError(t, gi.Initialize())
	require.NoError(t, gi.Start(ctx))
	t.Cleanup(func() { _ = gi.Stop(5 * time.Second) })

	// Let the mutation subscriptions propagate.
	time.Sleep(150 * time.Millisecond)
	return gi
}

// readEntity fetches the stored EntityState directly from the ENTITY_STATES KV
// bucket (graph-ingest is the writer; this is a read-back probe). Reading the
// bucket rather than a component method keeps the assertion on the durable
// post-write state, independent of any read-cache.
func readEntity(t *testing.T, natsClient *natsclient.Client, entityID string) *gtypes.EntityState {
	t.Helper()
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(context.Background(), gtypes.BucketEntityStates)
	require.NoError(t, err)
	entry, err := kv.Get(context.Background(), entityID)
	require.NoError(t, err)
	var es gtypes.EntityState
	require.NoError(t, json.Unmarshal(entry.Value(), &es))
	return &es
}

// triplesForPredicate returns the triples on es whose Predicate matches.
func triplesForPredicate(es *gtypes.EntityState, predicate string) []message.Triple {
	var out []message.Triple
	for _, tr := range es.Triples {
		if tr.Predicate == predicate {
			out = append(out, tr)
		}
	}
	return out
}

// --- Case 1: replace inside envelope succeeds, atomic, exactly-one value ---

func TestIntegration_ReplaceOwned_ReplaceExactlyOneValue(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := tc.Client

	gi := startGraphIngest(t, natsClient)

	const entityID = "acme.ops.robotics.gcs.drone.001"
	const pred = rulepacks.WorkflowStatePhase

	// Seed the entity carrying an initial value for the owned predicate.
	require.NoError(t, gi.CreateEntity(ctx, &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "phase"), Object: "initializing", Confidence: 1.0, Timestamp: time.Now()},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}))

	mutator := newTripleMutator(natsClient, nil)
	rev, err := mutator.ReplaceOwned(ctx, "rule-1", "rule-pack.test", entityID, pred,
		[]message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "phase"), Object: "running", Confidence: 1.0, Timestamp: time.Now()}})
	require.NoError(t, err, "in-envelope replace against the real handler must succeed")
	assert.Positive(t, rev, "a successful replace returns a non-zero KV revision")

	es := readEntity(t, natsClient, entityID)
	got := triplesForPredicate(es, pred)
	require.Len(t, got, 1, "replace_owned must leave EXACTLY ONE triple for the owned predicate (atomic replace-by-predicate)")
	assert.Equal(t, "running", got[0].Object, "the single remaining value must be the new one")
}

// --- Case 5 (integration): clear sub-case ---

func TestIntegration_ReplaceOwned_ClearRemovesPredicate(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := tc.Client

	gi := startGraphIngest(t, natsClient)

	const entityID = "acme.ops.robotics.gcs.drone.002"
	const pred = rulepacks.WorkflowStatePhase

	require.NoError(t, gi.CreateEntity(ctx, &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "phase"), Object: "running", Confidence: 1.0, Timestamp: time.Now()},
			// A second, unrelated predicate that must survive the clear.
			{Subject: entityID, Predicate: semantictest.Predicate(t, "entity", "identity", "type"), Object: "drone", Confidence: 1.0, Timestamp: time.Now()},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}))

	mutator := newTripleMutator(natsClient, nil)
	// Empty objects slice = clear (RemoveTriples only).
	_, err := mutator.ReplaceOwned(ctx, "rule-1", "rule-pack.test", entityID, pred, nil)
	require.NoError(t, err)

	es := readEntity(t, natsClient, entityID)
	assert.Empty(t, triplesForPredicate(es, pred), "clear must leave ZERO triples for the predicate")
	assert.Len(t, triplesForPredicate(es, rulepacks.EntityIdentityType), 1, "clear must not touch sibling predicates")
}

// --- Case 9 (integration): must-exist — non-existent entity → EntityNotFound ---

func TestIntegration_ReplaceOwned_MustExist(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := tc.Client

	gi := startGraphIngest(t, natsClient)
	_ = gi // started so the handler is live; the entity is deliberately absent.

	const missing = "acme.ops.robotics.gcs.drone.404"
	mutator := newTripleMutator(natsClient, nil)
	_, err := mutator.ReplaceOwned(ctx, "rule-1", "rule-pack.test", missing, rulepacks.WorkflowStatePhase,
		[]message.Triple{{Subject: missing, Predicate: semantictest.Predicate(t, "workflow", "state", "phase"), Object: "running", Confidence: 1.0, Timestamp: time.Now()}})
	require.Error(t, err, "replace_owned on a non-existent entity must error (no auto-vivify)")
	assert.Contains(t, err.Error(), gtypes.ErrorCodeEntityNotFound,
		"the handler's entity_not_found code must surface to the caller")
}

// --- The production tripleMutator stamps OwnerToken onto the wire (M2) ---

// TestIntegration_ReplaceOwned_StampsOwnerTokenOnWire drives the REAL
// tripleMutator.ReplaceOwned and captures the marshalled
// UpdateEntityWithTriplesRequest off the wire via a fake responder, asserting the
// OwnerToken field is actually placed on the request (the literal
// `req.OwnerToken = ownerToken` line PR-1 adds at triple_mutator.go). A unit test
// cannot cover this seam — tripleMutator holds a concrete *natsclient.Client, not
// an interface — so the production marshal path is only reachable through live
// NATS. The fake responder stands in for graph-ingest so the assertion is purely
// on the bytes the production mutator emits.
// (feedback_integration_tests_must_drive_production_wire)
func TestIntegration_ReplaceOwned_StampsOwnerTokenOnWire(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	natsClient := tc.Client

	captured := make(chan gtypes.UpdateEntityWithTriplesRequest, 1)
	_, err := natsClient.Subscribe(ctx, SubjectEntityUpdateWithTriples, func(_ context.Context, msg *nats.Msg) {
		var req gtypes.UpdateEntityWithTriplesRequest
		if err := json.Unmarshal(msg.Data, &req); err == nil {
			select {
			case captured <- req:
			default:
			}
		}
		resp, _ := json.Marshal(gtypes.UpdateEntityWithTriplesResponse{
			MutationResponse: gtypes.MutationResponse{KVRevision: 1},
		})
		_ = msg.Respond(resp)
	})
	require.NoError(t, err)
	// Let the subscription propagate before the request fires.
	time.Sleep(100 * time.Millisecond)

	const entityID = "acme.ops.robotics.gcs.drone.001"
	const wantToken = "rule-pack.test#deadbeef01234567"
	mutator := newTripleMutator(natsClient, nil)
	_, err = mutator.ReplaceOwned(ctx, "rule-1", wantToken, entityID, rulepacks.WorkflowStatePhase,
		[]message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "workflow", "state", "phase"), Object: "running", Confidence: 1.0, Timestamp: time.Now()}})
	require.NoError(t, err, "fake responder replies success; the call must round-trip")

	select {
	case req := <-captured:
		assert.Equal(t, wantToken, req.OwnerToken,
			"production tripleMutator.ReplaceOwned must stamp the ownerToken argument onto req.OwnerToken")
	case <-time.After(2 * time.Second):
		t.Fatal("no update_with_triples request captured on the wire")
	}
}

// --- Processor owner-wiring (requires NATS for initializeStateTracker) ---

func TestIntegration_ReplaceOwned_ProcessorWiresOwner(t *testing.T) {
	ctx := context.Background()
	tc := natsclient.NewTestClient(t, natsclient.WithKV())
	natsClient := tc.Client

	cfg := DefaultConfig()
	cfg.PackID = "replace-owned-test"
	cfg.ProjectionContracts = replaceOwnedTestContracts()
	rp, err := NewProcessorWithMetrics(natsClient, &cfg, nil)
	require.NoError(t, err)

	require.NoError(t, rp.initializeStateTracker(ctx))
	exec, ok := rp.actionExecutor.(*ActionExecutor)
	require.True(t, ok, "expected concrete *ActionExecutor")
	assert.Equal(t, "rule-pack.replace-owned-test", exec.ownerID,
		"processor must wire rule-pack.<PackID> onto the executor at state-tracker init")
}
