//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storedTodoTripleCount reads the loop entity back over the production
// graph.ingest.query.entity surface — the same one the todo reader consumes —
// and counts its agent.todo.* triples.
func storedTodoTripleCount(ctx context.Context, t *testing.T, client *natsclient.Client, loopEntityID string) int {
	t.Helper()
	reqData, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: loopEntityID})
	require.NoError(t, err)

	respData, err := client.RequestClassified(ctx, "graph.ingest.query.entity", reqData, 5*time.Second)
	require.NoError(t, err)

	var entity graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(respData, &entity))

	count := 0
	for _, triple := range entity.Triples {
		if strings.HasPrefix(triple.Predicate, "agent.todo.") {
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
			message.Triple{Subject: loopEntityID, Predicate: semantictest.Predicate(t, "agent", "todo", "id"), Object: it.id, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: semantictest.Predicate(t, "agent", "todo", "content"), Object: it.content, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: semantictest.Predicate(t, "agent", "todo", "status"), Object: it.status, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: semantictest.Predicate(t, "agent", "todo", "position"), Object: i, Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
			message.Triple{Subject: loopEntityID, Predicate: semantictest.Predicate(t, "agent", "todo", "updated-at"), Object: now.Format(time.RFC3339Nano), Timestamp: now, Confidence: 1.0, Source: "agent-write-todos"},
		)
	}

	// Born-first (ADR-055): the loop-execution entity must exist before
	// anything writes todo triples to it. In production graphWriter creates it
	// via create_with_triples carrying agentic.LoopExecutionMessageType()
	// (#277) before the agent's first write_todos.
	//
	// The triples are planted in the SAME write, because that stores what
	// production stores. write_todos does not use the append lane at all — it
	// goes through projection.ReplaceOwned (write_todos.go), the replace lane,
	// which writes the caller's desired set verbatim. The append lane
	// deduplicates by exact six-field tuple, and a real todo batch shares one
	// `now` across every item, so its three agent.todo.updated-at triples are
	// byte-identical and collapse to one — shearing the reader's fixed
	// five-stride grouping. Planting via the create lane (also verbatim)
	// reproduces the production-shaped stored state without dragging
	// owner-token and contract scaffolding into a test about compaction
	// survival. Do NOT "fix" this by making the planted timestamps distinct:
	// production genuinely writes one shared updated_at across items, so that
	// would make the fixture less faithful, not more.
	require.NoError(t, c.CreateEntityStrict(ctx, &graph.EntityState{
		ID:          loopEntityID,
		MessageType: agentic.LoopExecutionMessageType(),
		Triples:     triples,
	}))

	// The reader consumes STORED state, so assert on stored state — read back
	// over the same graph.ingest.query.entity surface the reader itself uses.
	// All five triples per item must survive, including the three identical
	// updated-at tuples the append lane would have collapsed.
	require.Equal(t, len(triples), storedTodoTripleCount(ctx, t, natsClient, loopEntityID),
		"the reader groups agent.todo.* triples in strides of five; a missing one shears every later group")

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
