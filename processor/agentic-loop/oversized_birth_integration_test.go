//go:build integration

package agenticloop

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// TestAnOversizedBirthStampsItsExecutionFailed is the terminal half of the
// payload-ceiling birth (#1365, PR #1387 review MEDIUM 1): the loop-execution
// entity is born by WriteSpawnIdentity BEFORE the record write the ceiling
// refuses, so terminating the task must also give that entity its terminal —
// a failure stamp naming the reason (ADR-098: agent execution is a graph
// condition). Driven through the task lane's production heartbeat callback,
// with the graph writer wired to real NATS request/reply.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestAnOversizedBirthStampsItsExecutionFailed(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := t.Context()

	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.entity.create",
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
	var (
		mu       sync.Mutex
		appended []message.Triple
	)
	_, err = tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.append",
		func(_ context.Context, data []byte) ([]byte, error) {
			var req gtypes.AppendTriplesRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return nil, err
			}
			mu.Lock()
			appended = append(appended, req.Triples...)
			mu.Unlock()
			results := make([]gtypes.AppendSubjectResult, 0, 1)
			if len(req.Triples) > 0 {
				results = append(results, gtypes.AppendSubjectResult{
					EntityID: req.Triples[0].Subject, Outcome: gtypes.MutationApplied, KVRevision: 2,
				})
			}
			return json.Marshal(gtypes.AppendTriplesResponse{Results: results})
		})
	require.NoError(t, err)

	const loopID = "7c1e5a9d-2b3f-4c6e-8a1d-0f9e8d7c6b5a"
	c := releaseTestComponent(t, NewMessageHandler(DefaultConfig()))
	platform := types.PlatformMeta{Org: "acme", Platform: "ops"}
	c.graphWriter = &graphWriter{
		natsClient: tc.Client,
		platform:   platform,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c.loopsBucket = &payloadCeilingBucket{recordingLoopBucket: &recordingLoopBucket{}, ceiling: 16}

	_, settled := deliverBirth(t, c, loopID)
	require.Equal(t, natsclient.DeliveryDecisionTerminate, settled.Decision())
	require.ErrorIs(t, settled.Cause(), nats.ErrMaxPayload)

	// The request/reply above has returned, so every stamp it made is in.
	mu.Lock()
	defer mu.Unlock()
	entityID := agentic.LoopExecutionEntityID(platform.Org, platform.Platform, loopID)
	stamped := map[string]any{}
	for _, triple := range appended {
		if triple.Subject == entityID {
			stamped[triple.Predicate] = triple.Object
		}
	}
	require.Equal(t, agentic.OutcomeFailed, stamped[agvocab.LoopOutcome],
		"the born execution entity has no failed outcome; stamped %v", stamped)
	require.Equal(t, taskIntakeRecordExceedsCeilingReason, stamped[agvocab.LoopTerminalReason],
		"the failure stamp does not name the ceiling")
}
