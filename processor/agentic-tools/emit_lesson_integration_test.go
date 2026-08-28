//go:build integration

// Package agentictools_test — integration test for emit_lesson (ADR-080),
// driven through the PRODUCTION tool wire: a ToolCall published to
// tool.execute.emit_lesson is executed by a real agentic-tools Component, which
// births the lesson entity through the real graph-ingest create
// handler over live NATS/KV, and publishes a ToolResult to tool.result.*. This
// exercises the assembled system end-to-end, not a helper-direct executor call,
// including the content-derived idempotency path.
package agentictools_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"
)

// lessonTestClient stands up a per-test NATS client with KV (for ENTITY_STATES)
// plus the entity and tool-wire streams graph-ingest and agentic-tools need.
func lessonTestClient(t *testing.T) *natsclient.Client {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}},
			natsclient.TestStreamConfig{Name: "AGENT", Subjects: []string{"agent.>", "tool.execute.>", "tool.result.>"}},
		),
	)
	return tc.Client
}

// startGraphIngestForLessons stands up a real graph-ingest Component that owns
// the canonical create mutation handler emit_lesson births through.
func startGraphIngestForLessons(t *testing.T, natsClient *natsclient.Client) {
	t.Helper()
	cfg := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient, PayloadRegistry: payloadbuiltins.NewTestRegistry(t)})
	require.NoError(t, err)

	gi := comp.(*graphingest.Component)
	require.NoError(t, gi.Initialize())
	require.NoError(t, gi.Start(context.Background()))
	t.Cleanup(func() { _ = gi.Stop(context.Background()) })

	time.Sleep(150 * time.Millisecond) // let mutation subscriptions propagate
}

// startToolsWithEmitLesson stands up a real agentic-tools Component bound to the
// tool.execute/tool.result wire with the emit_lesson executor registered against
// a real NATS triple publisher.
func startToolsWithEmitLesson(t *testing.T, natsClient *natsclient.Client) {
	t.Helper()
	cfg := agentictools.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "tool.execute", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.execute.>"}}, Required: true},
			},
			Outputs: []component.PortDefinition{
				{Name: "tool.result", Config: component.JetStreamPort{StreamName: "AGENT", Subjects: []string{"tool.result.*"}}},
			},
		},
		StreamName:         "AGENT",
		ConsumerNameSuffix: "emit-lesson-test",
		Timeout:            "5s",
	}
	rawConfig, err := json.Marshal(cfg)
	require.NoError(t, err)

	comp, err := agentictools.NewComponent(rawConfig, component.Dependencies{
		NATSClient:      natsClient,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
	})
	require.NoError(t, err)

	toolsComp, ok := comp.(*agentictools.Component)
	require.True(t, ok)

	store := agentictools.NewNATSLessonStore(natsClient)
	exec := agentictools.NewEmitLessonExecutor(store, types.PlatformMeta{Org: "acme", Platform: "ops"}, slog.Default())
	require.NoError(t, toolsComp.RegisterToolExecutor(exec))

	lc, ok := comp.(component.LifecycleComponent)
	require.True(t, ok)
	require.NoError(t, lc.Initialize())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, lc.Start(ctx))
	t.Cleanup(func() { _ = lc.Stop(context.Background()) })

	time.Sleep(200 * time.Millisecond) // let the tool-call consumer bind
}

func validLessonToolCall(id string) *agentic.ToolCall {
	return &agentic.ToolCall{
		ID:     id,
		Name:   agentictools.EmitLessonToolName,
		LoopID: "loop-ops-int",
		Metadata: map[string]any{
			"agent.role": "ops",
		},
		Arguments: map[string]any{
			"summary":             "cap retention sweeps to entity-owned buckets",
			"detail":              "When sweeping AGENT_LOOPS, scope deletes to COMPLETE_* keys so sibling owners' facts survive.",
			"injection_form":      "Avoid unscoped retention sweeps; scope deletes to COMPLETE_* keys.",
			"category":            "retention-policy",
			"polarity":            "avoid",
			"severity":            "warning",
			"evidence_entity_ids": []any{"acme.ops.agentic-loop.agent.execution.loop-abc"},
			"applies_to":          []any{"tag:ops", "id:acme.ops.agent"},
		},
	}
}

// TestIntegration_EmitLesson_ProductionWire drives emit_lesson through
// tool.execute → tool.result, verifies the lesson entity landed in
// ENTITY_STATES with the proposed status and typed origin, and proves the
// content-derived idempotency path: a second identical emit re-uses the same
// entity and does NOT write a second time.
func TestIntegration_EmitLesson_ProductionWire(t *testing.T) {
	ctx := context.Background()
	natsClient := lessonTestClient(t)
	startGraphIngestForLessons(t, natsClient)
	startToolsWithEmitLesson(t, natsClient)

	// Subscribe to tool results via the production decoder.
	results := make(chan agentic.ToolResult, 8)
	dec := payloadbuiltins.NewTestDecoder(t)
	_, err := natsClient.Subscribe(ctx, "tool.result.>", func(_ context.Context, msg *nats.Msg) {
		baseMsg, decErr := dec.Decode(msg.Data)
		if decErr != nil {
			return
		}
		if r, ok := baseMsg.Payload().(*agentic.ToolResult); ok {
			results <- *r
		}
	})
	require.NoError(t, err)
	time.Sleep(150 * time.Millisecond)

	publish := func(call *agentic.ToolCall) {
		baseMsg := message.NewBaseMessage(call.Schema(), call, "integration-test")
		data, mErr := json.Marshal(baseMsg)
		require.NoError(t, mErr)
		require.NoError(t, natsClient.PublishToStream(ctx, "tool.execute.emit_lesson", data))
	}

	waitResult := func(callID string) agentic.ToolResult {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case r := <-results:
				if r.CallID == callID {
					return r
				}
			case <-deadline:
				t.Fatalf("timed out waiting for tool result %q", callID)
			}
		}
	}

	// --- First emit: births the lesson entity. ---
	publish(validLessonToolCall("call-lesson-1"))
	res1 := waitResult("call-lesson-1")
	require.Empty(t, res1.Error, "first emit must succeed")
	require.False(t, res1.StopLoop, "emit_lesson must not stop the loop")
	assert.Equal(t, true, res1.Metadata["lesson_created"], "fresh birth must report created=true")
	assert.Equal(t, "proposed", res1.Metadata["lesson_status"], "fresh birth status is proposed")

	entityID, _ := res1.Metadata["lesson_id"].(string)
	require.NotEmpty(t, entityID, "result must carry lesson_id")
	require.True(t, message.IsValidEntityID(entityID), "lesson_id must be a valid 6-part entity ID")
	assert.Contains(t, entityID, "acme.ops.lesson.agent.record.")

	// Read the entity straight from ENTITY_STATES KV.
	js, err := natsClient.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue(ctx, gtypes.BucketEntityStates)
	require.NoError(t, err)

	entry1, err := kv.Get(ctx, entityID)
	require.NoError(t, err, "lesson entity must exist in ENTITY_STATES after emit")
	rev1 := entry1.Revision()

	var es gtypes.EntityState
	require.NoError(t, json.Unmarshal(entry1.Value(), &es))
	assert.Equal(t, agentic.AgentLessonMessageType(), es.MessageType, "lesson must be born with the agent_lesson typed origin")

	statusVals := objectsFor(&es, "agent.lesson.status")
	require.Len(t, statusVals, 1, "exactly one status triple")
	assert.Equal(t, "proposed", statusVals[0], "lesson must be born proposed")
	assert.Equal(t, []string{"acme.ops.agentic-loop.agent.execution.loop-abc"}, objectsFor(&es, "agent.lesson.evidence"))
	assert.Len(t, objectsFor(&es, "agent.lesson.applies-to"), 2, "both scope keys persisted")
	assert.Equal(t, []string{"acme.ops.agentic-loop.agent.execution.loop-ops-int"}, objectsFor(&es, "agent.action.executed-by"),
		"executed-by back-link derived from the loop id")
	assert.Equal(t, []string{"ops"}, objectsFor(&es, "agent.lesson.observed-role"), "observed-role derived from loop metadata")

	// --- Second emit: identical content ⇒ same entity, idempotent (no re-write). ---
	publish(validLessonToolCall("call-lesson-2"))
	res2 := waitResult("call-lesson-2")
	require.Empty(t, res2.Error, "re-emit must not error the loop")
	id2, _ := res2.Metadata["lesson_id"].(string)
	assert.Equal(t, entityID, id2, "identical lesson must derive the same entity ID")
	assert.Equal(t, false, res2.Metadata["lesson_created"], "re-emit must report created=false (dedup)")
	assert.Equal(t, "proposed", res2.Metadata["lesson_status"],
		"re-emit reports the TRUE persisted status read back from the graph, not the call's intent")

	entry2, err := kv.Get(ctx, entityID)
	require.NoError(t, err)
	assert.Equal(t, rev1, entry2.Revision(),
		"re-emitting an identical lesson must NOT write a second time (EntityExists swallowed as idempotent-success)")

	var es2 gtypes.EntityState
	require.NoError(t, json.Unmarshal(entry2.Value(), &es2))
	assert.Len(t, objectsFor(&es2, "agent.lesson.status"), 1, "still exactly one status triple after re-emit")
}

// objectsFor returns the string objects of every triple on es carrying the
// given predicate, in stored order.
func objectsFor(es *gtypes.EntityState, predicate string) []string {
	var out []string
	for _, tr := range es.Triples {
		if tr.Predicate != predicate {
			continue
		}
		if s, ok := tr.Object.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
