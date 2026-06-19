//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_TodoWriteReadRoundTrip is the end-to-end proof of
// the ADR-036 compaction-survival mechanism: triples written to the
// loop entity via the Stage 2 batch handler are reconstructible via
// the Stage 4 NATS reader, in the original array order, with no
// state held in the agent's chat history.
//
// The test deliberately does NOT boot the full agentic-loop: it
// exercises only the contract Stage 4's reader depends on (write
// triples → query the loop entity → reconstruct todos). This is the
// invariant that makes context compaction safe to run without
// destroying the working list.
func TestIntegration_TodoWriteReadRoundTrip(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	// Boot graph-ingest to handle the mutation/query NATS surfaces.
	config := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	c := comp.(*graphingest.Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })
	time.Sleep(100 * time.Millisecond) // wait for handlers to register

	const loopID = "compaction-survival-001"
	loopEntityID := agentic.LoopExecutionEntityID("acme", "ops", loopID)

	// Iteration 1 — agent calls write_todos with three items.
	// Build the same triple shape Stage 3's executor emits.
	now := time.Now()
	items := []struct {
		id      string
		content string
		status  string
	}{
		{"1", "Survey existing rules", "completed"},
		{"2", "Draft new rule", "in_progress"},
		{"3", "Wire e2e test", "pending"},
	}

	triples := make([]message.Triple, 0, len(items)*5)
	for i, it := range items {
		triples = append(triples,
			message.Triple{Subject: loopEntityID, Predicate: agvocab.TodoID, Object: it.id, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: agvocab.TodoContent, Object: it.content, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: agvocab.TodoStatus, Object: it.status, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: agvocab.TodoPosition, Object: i, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: agvocab.TodoUpdatedAt, Object: now.Format(time.RFC3339Nano), Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
		)
	}

	// Born-first (ADR-055): the loop-execution entity must exist before
	// write_todos appends triples to it. In production graphWriter creates it
	// via create_with_triples carrying agentic.LoopExecutionMessageType()
	// (#277) before the agent's first write_todos; post-must-exist-flip a
	// triple.add_batch to an absent entity is rejected, not auto-vivified.
	require.NoError(t, c.CreateEntityStrict(ctx, &graph.EntityState{
		ID:          loopEntityID,
		MessageType: agentic.LoopExecutionMessageType(),
	}))

	written, failed, err := c.AddTriples(ctx, triples)
	require.NoError(t, err)
	require.Empty(t, failed)
	require.Equal(t, len(triples), written)

	// Iteration 2 — context has been "compacted" (irrelevant to this
	// test — the assembler doesn't read trajectory; it reads triples).
	// The reader fetches from the loop entity and reconstructs the list.
	reader := agenticloop.NewNATSTodoReader(natsClient)
	got, err := reader.ReadTodos(ctx, loopEntityID)
	require.NoError(t, err)
	require.Len(t, got, len(items), "compaction-survival: all 3 todos reconstructed from triples")

	for i, want := range items {
		assert.Equal(t, want.id, got[i].ID, "todos[%d].ID", i)
		assert.Equal(t, want.content, got[i].Content, "todos[%d].Content", i)
		assert.Equal(t, want.status, got[i].Status, "todos[%d].Status", i)
		assert.Equal(t, i, got[i].Position, "todos[%d].Position", i)
	}

	// Format as the system message — pin the operator-visible output
	// matches what the prompt assembler actually injects.
	msg := agenticloop.BuildTodoStateMessage(got)
	require.Equal(t, "system", msg.Role)
	assert.Contains(t, msg.Content, "[Working list", "header line")
	assert.Contains(t, msg.Content, "[x] Survey existing rules", "completed marker")
	assert.Contains(t, msg.Content, "[~] Draft new rule", "in_progress marker")
	assert.Contains(t, msg.Content, "[ ] Wire e2e test", "pending marker")
}

// TestIntegration_FreshLoopReturnsEmptyList covers the case Stage 4's
// reviewer flagged H1 on: a loop with no entity yet (first iteration,
// agent hasn't called write_todos) must return an empty list, NOT a
// JSON-unmarshal error. Without the fix, every fresh loop's first
// iteration would emit a Debug log line.
func TestIntegration_FreshLoopReturnsEmptyList(t *testing.T) {
	ctx := context.Background()

	streams := []natsclient.TestStreamConfig{
		{Name: "ENTITY", Subjects: []string{"entity.>"}},
	}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))
	natsClient := testClient.Client

	config := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	c := comp.(*graphingest.Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })
	time.Sleep(100 * time.Millisecond)

	loopEntityID := agentic.LoopExecutionEntityID("acme", "ops", "fresh-loop-never-wrote-todos")

	reader := agenticloop.NewNATSTodoReader(natsClient)
	got, err := reader.ReadTodos(ctx, loopEntityID)
	require.NoError(t, err, "missing entity must not surface as an error — fail-open contract from Stage 4")
	assert.Nil(t, got, "fresh loop returns nil/empty list")
}
