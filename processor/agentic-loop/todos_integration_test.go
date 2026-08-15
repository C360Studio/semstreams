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
	"github.com/c360studio/semstreams/vocabulary/builtins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storedTodoTripleCount reads the loop entity back over the production
// graph.ingest.query.entity surface — the same one the todo reader consumes —
// and counts its agent.todo.record triples.
func storedTodoTripleCount(ctx context.Context, t *testing.T, client *natsclient.Client, loopEntityID string) int {
	t.Helper()
	reqData, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: loopEntityID})
	require.NoError(t, err)

	respData, err := client.RequestClassified(ctx, "graph.ingest.query.entity", reqData, 5*time.Second)
	require.NoError(t, err)

	var exact graph.ExactEntity
	require.NoError(t, json.Unmarshal(respData, &exact))
	require.NotNil(t, exact.Entity)
	entity := exact.Entity

	count := 0
	for _, triple := range entity.Triples {
		if triple.Predicate == agvocab.TodoRecord {
			count++
		}
	}
	return count
}

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
	builtins.Register()

	// Boot graph-ingest to handle the mutation/query NATS surfaces.
	config := graphingest.DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	comp, err := graphingest.CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	c := comp.(*graphingest.Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	time.Sleep(100 * time.Millisecond) // wait for handlers to register

	const loopID = "compaction-survival-001"
	loopEntityID := agentic.LoopExecutionEntityID("acme", "ops", loopID)

	// Iteration 1 — plant the same one-record-per-item shape emitted by
	// write_todos. The writer integration test separately proves the real
	// projection reconcile path; this test isolates exact read and reconstruction.
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

	triples := make([]message.Triple, 0, len(items))
	for i, it := range items {
		triples = append(triples, integrationTodoRecordTriple(t, loopEntityID, i, it.id, it.content, it.status, now))
	}

	// Born-first (ADR-055): the loop-execution entity must exist before
	// anything writes todo triples to it. In production graphWriter creates it
	// through canonical create carrying agentic.LoopExecutionMessageType()
	// (#277) before the agent's first write_todos.
	//
	// The triples are planted in the same birth so the reader observes the exact
	// persisted representation. Records share status/timestamps without relying
	// on duplicate triple occurrence or storage order.
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID:          loopEntityID,
		MessageType: agentic.LoopExecutionMessageType(),
		Triples:     triples,
	}))

	// The reader consumes STORED state, so assert on stored state — read back
	// over the same graph.ingest.query.entity surface the reader itself uses.
	// Every logical item survives as one complete record.
	require.Equal(t, len(triples), storedTodoTripleCount(ctx, t, natsClient, loopEntityID),
		"each logical item must survive as one complete record triple")

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

func integrationTodoRecordTriple(
	t *testing.T,
	loopEntityID string,
	position int,
	id, content, status string,
	updatedAt time.Time,
) message.Triple {
	t.Helper()
	encoded, err := json.Marshal(struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		Status    string `json:"status"`
		Position  int    `json:"position"`
		UpdatedAt string `json:"updated_at"`
	}{id, content, status, position, updatedAt.UTC().Format(time.RFC3339Nano)})
	require.NoError(t, err)
	return message.Triple{
		Subject: loopEntityID, Predicate: agvocab.TodoRecord, Object: string(encoded),
		Datatype: agvocab.TodoRecordJSONDatatype, Source: "agent-write-todos",
		Timestamp: updatedAt, Confidence: 1,
	}
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
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	time.Sleep(100 * time.Millisecond)

	loopEntityID := agentic.LoopExecutionEntityID("acme", "ops", "fresh-loop-never-wrote-todos")

	reader := agenticloop.NewNATSTodoReader(natsClient)
	got, err := reader.ReadTodos(ctx, loopEntityID)
	require.NoError(t, err, "missing entity must not surface as an error — fail-open contract from Stage 4")
	assert.Nil(t, got, "fresh loop returns nil/empty list")
}
