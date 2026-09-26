//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

const oversizedBirthLoopID = "7c1e5a9d-2b3f-4c6e-8a1d-0f9e8d7c6b5a"

// oversizedBirthGraph answers the two graph mutations a birth and its failure
// stamp make over real NATS request/reply, and collects every append whole so
// a test can read the terminal mutation as one write.
func oversizedBirthGraph(t *testing.T, tc *natsclient.TestClient) *appendCollector {
	t.Helper()
	_, err := tc.Client.SubscribeForRequests(t.Context(), "graph.mutation.entity.create",
		func(_ context.Context, data []byte) ([]byte, error) {
			var req gtypes.CreateEntityRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, err
			}
			return json.Marshal(gtypes.CreateEntityResponse{
				Outcome: gtypes.MutationApplied, Entity: req.Entity, KVRevision: 1,
			})
		})
	require.NoError(t, err)
	collector := &appendCollector{}
	collector.subscribe(t, t.Context(), tc.Client)
	return collector
}

// oversizedBirthComponent is a hand-built component whose graph writer talks
// to real NATS and whose loop bucket refuses the birth record for size.
func oversizedBirthComponent(t *testing.T, tc *natsclient.TestClient) *Component {
	t.Helper()
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	c.graphWriter = &graphWriter{
		natsClient: tc.Client,
		platform:   types.PlatformMeta{Org: "acme", Platform: "ops"},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c.loopsBucket = &payloadCeilingBucket{recordingLoopBucket: &recordingLoopBucket{}, ceiling: 16}
	return c
}

// requireOversizedBirthTerminated drives the birth through the task lane's
// production heartbeat callback and returns the terminal graph mutation: the
// one append on the born execution entity carrying agent.loop.outcome.
func requireOversizedBirthTerminated(t *testing.T, c *Component, collector *appendCollector) map[string][]any {
	t.Helper()
	_, settled := deliverBirth(t, c, oversizedBirthLoopID)
	require.Equal(t, natsclient.DeliveryDecisionTerminate, settled.Decision())
	require.ErrorIs(t, settled.Cause(), nats.ErrMaxPayload)

	// The request/reply above has returned, so every stamp it made is in.
	entityID := agentic.LoopExecutionEntityID("acme", "ops", oversizedBirthLoopID)
	collector.mu.Lock()
	defer collector.mu.Unlock()
	var terminal []map[string][]any
	for _, triples := range collector.requests {
		stamped := map[string][]any{}
		for _, triple := range triples {
			if triple.Subject == entityID {
				stamped[triple.Predicate] = append(stamped[triple.Predicate], triple.Object)
			}
		}
		if len(stamped[agvocab.LoopOutcome]) > 0 {
			terminal = append(terminal, stamped)
		}
	}
	require.Len(t, terminal, 1, "expected exactly one terminal mutation on the born execution entity")
	stamped := terminal[0]
	require.Equal(t, []any{agentic.OutcomeFailed}, stamped[agvocab.LoopOutcome],
		"the born execution entity has no failed outcome; stamped %v", stamped)
	require.Equal(t, []any{taskIntakeRecordExceedsCeilingReason}, stamped[agvocab.LoopTerminalReason],
		"the failure stamp does not name the ceiling")
	return stamped
}

// TestAnOversizedBirthStampsItsExecutionFailed is the terminal half of the
// payload-ceiling birth (#1365, PR #1387 review MEDIUM 1 and Codex round 1):
// the loop-execution entity is born by WriteSpawnIdentity, and the loop's
// initial trajectory observations are recorded, BEFORE the record write the
// ceiling refuses, so terminating the task owes that execution its terminal:
// a failure stamp naming the reason (ADR-098: agent execution is a graph
// condition), a failed loop.terminal observation, and on the stamp the audit
// loss the component observed for the loop. Driven through the task lane's
// production heartbeat callback, with the graph writer wired to real NATS
// request/reply.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
// spec: agentic-loop / Terminal trajectory facts are ordinary observations
// spec: agentic-loop / Observed audit loss MUST be readable from the loop entity as a classified condition
func TestAnOversizedBirthStampsItsExecutionFailed(t *testing.T) {
	t.Run("healthy recorder records a failed terminal fact and the stamp claims no loss", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		collector := oversizedBirthGraph(t, tc)
		c := oversizedBirthComponent(t, tc)
		facts := &trajectoryTestBucket{values: make(map[string][]byte)}
		store := &trajectoryTestStore{values: make(map[string][]byte)}
		registry := storeregistry.New()
		require.NoError(t, registry.Register("objectstore", store))
		c.trajectoryRecorder = newTrajectoryRecorder(facts, registry, "objectstore", c.reportTrajectoryAuditFailure)

		stamped := requireOversizedBirthTerminated(t, c, collector)
		require.Empty(t, stamped[agvocab.LoopEvidenceIntegrity],
			"a birth with no observed audit loss was stamped incomplete")

		page, err := newTrajectoryReader(facts).read(t.Context(),
			agentic.TrajectoryQueryRequest{LoopID: oversizedBirthLoopID}, 1<<20)
		require.NoError(t, err)
		require.True(t, page.TerminalObserved,
			"the reader sees the birth's initial facts and no terminal: %d facts", len(page.Facts))
		var terminal *agentic.TrajectoryFactV1
		for i := range page.Facts {
			if page.Facts[i].Kind == agentic.TrajectoryKindLoopTerminal {
				terminal = &page.Facts[i]
			}
		}
		require.NotNil(t, terminal)
		require.Equal(t, agentic.TrajectoryStatusFailed, terminal.Status)
		require.NotNil(t, terminal.Evidence, "the terminal fact captured no evidence")
		evidence, ok := store.values[terminal.Evidence.Key]
		require.True(t, ok, "the terminal fact's evidence is not in the store")
		require.NotContains(t, string(evidence), "first turn",
			"the terminal evidence carries the prompt the ceiling refused")
	})

	t.Run("a per-loop audit loss is stamped incomplete", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
		collector := oversizedBirthGraph(t, tc)
		c := oversizedBirthComponent(t, tc)
		registry := storeregistry.New()
		require.NoError(t, registry.Register("objectstore",
			&trajectoryTestStore{values: make(map[string][]byte), putErrBefore: true}))
		c.trajectoryRecorder = newTrajectoryRecorder(&trajectoryTestBucket{values: make(map[string][]byte)},
			registry, "objectstore", c.reportTrajectoryAuditFailure)

		stamped := requireOversizedBirthTerminated(t, c, collector)
		require.Equal(t, []any{"incomplete"}, stamped[agvocab.LoopEvidenceIntegrity],
			"the loop's observed audit loss did not reach its failure stamp")
	})

	t.Run("a component that records no trajectory evidence stamps incomplete", func(t *testing.T) {
		tc := natsclient.NewTestClient(t, natsclient.WithKV())
		collector := oversizedBirthGraph(t, tc)

		// Retained state that violates the AGENT_TRAJECTORIES contract, so
		// the production Start path leaves no recorder and latches the loss.
		js, err := tc.Client.JetStream()
		require.NoError(t, err)
		_, err = js.CreateKeyValue(t.Context(), jetstream.KeyValueConfig{
			Bucket: agentic.TrajectoryBucketName, History: 5,
		})
		require.NoError(t, err)
		discoverable, err := NewComponent([]byte(`{}`), component.Dependencies{
			NATSClient: tc.Client, PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
			Platform: component.PlatformMeta{Org: "acme", Platform: "ops"},
		})
		require.NoError(t, err)
		c := discoverable.(*Component)
		require.NoError(t, c.initializeKVBuckets(t.Context()))
		require.Nil(t, c.trajectoryRecorder, "incompatible bucket should leave no recorder")
		c.loopsBucket = &payloadCeilingBucket{recordingLoopBucket: &recordingLoopBucket{}, ceiling: 16}

		stamped := requireOversizedBirthTerminated(t, c, collector)
		require.Equal(t, []any{"incomplete"}, stamped[agvocab.LoopEvidenceIntegrity],
			"a birth in a process that records no trajectory evidence was stamped as if healthy")
	})
}
