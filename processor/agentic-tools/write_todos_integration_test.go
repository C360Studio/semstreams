//go:build integration

package agentictools_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

func TestIntegration_WriteTodosExecutorReconcilesCompleteGroup(t *testing.T) {
	ctx := context.Background()
	client := graphMutationTestClient(t)
	ingest := startGraphIngestForMutationTest(t, client)
	mutations := newTestMutationClient(t, client)

	const loopID = "loop-todo-integration"
	entityID := agentic.LoopExecutionEntityID("acme", "ops", loopID)
	now := time.Now()
	require.NoError(t, ingest.CreateEntity(ctx, &graph.EntityState{
		ID:          entityID,
		MessageType: agentic.LoopExecutionMessageType(),
		Triples: []message.Triple{
			{
				Subject: entityID, Predicate: agvocab.LoopRole,
				Object: "worker", Source: "birth", Timestamp: now, Confidence: 1,
			},
			{
				Subject: entityID, Predicate: agvocab.LoopOutcome,
				Object: "running", Source: "unrelated", Timestamp: now, Confidence: 1,
			},
		},
		Version:   1,
		UpdatedAt: now,
	}))

	executor := agentictools.NewWriteTodosExecutor(
		mutations,
		types.PlatformMeta{Org: "acme", Platform: "ops"},
	)
	frozen := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	executor.SetClock(func() time.Time { return frozen })
	write := func(callID string, todos ...map[string]any) agentic.ToolResult {
		t.Helper()
		result, err := executor.Execute(ctx, agentic.ToolCall{
			ID: callID, Name: agentictools.WriteTodosToolName, LoopID: loopID,
			Arguments: map[string]any{"todos": todos},
		})
		require.NoError(t, err)
		require.Empty(t, result.Error)
		assert.Equal(t, len(todos), result.Metadata["todo_count"])
		assert.NotContains(t, result.Metadata, "triple_count")
		return result
	}

	write("multi",
		map[string]any{"id": "one", "content": "first", "status": "pending"},
		map[string]any{"id": "two", "content": "second", "status": "pending"},
	)
	entity := readEntityKV(t, client, entityID)
	assert.Equal(t, 2, predicatesPresent(entity, agvocab.TodoRecord))
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome))
	reader := agenticloop.NewNATSTodoReader(client)
	got, err := reader.ReadTodos(ctx, entityID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"one", "two"}, []string{got[0].ID, got[1].ID})
	assert.Equal(t, got[0].UpdatedAt, got[1].UpdatedAt,
		"one call shares a timestamp without collapsing distinct record triples")

	write("reorder",
		map[string]any{"id": "two", "content": "second revised", "status": "completed"},
		map[string]any{"id": "one", "content": "first", "status": "completed"},
	)
	got, err = reader.ReadTodos(ctx, entityID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"two", "one"}, []string{got[0].ID, got[1].ID},
		"position changes reorder the logical list independent of stored triple order")

	write("shorter",
		map[string]any{"id": "two", "content": "second revised", "status": "completed"},
	)
	entity = readEntityKV(t, client, entityID)
	assert.Equal(t, 1, predicatesPresent(entity, agvocab.TodoRecord), "omitted record must be deleted")
	got, err = reader.ReadTodos(ctx, entityID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "two", got[0].ID)
	assert.Equal(t, "second revised", got[0].Content)
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome),
		"unrelated fact must survive replacement")

	write("clear")
	entity = readEntityKV(t, client, entityID)
	assert.Zero(t, predicatesPresent(entity, agvocab.TodoRecord))
	got, err = reader.ReadTodos(ctx, entityID)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome),
		"unrelated fact must survive empty desired reconciliation")
	assert.Equal(t, []string{"worker"}, objectsOf(entity, agvocab.LoopRole),
		"immutable birth fact must survive todo reconciliation")
}
