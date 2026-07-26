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
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

func TestIntegration_WriteTodosExecutorReconcilesCompleteGroup(t *testing.T) {
	ctx := context.Background()
	client := ofwTestClient(t)
	ingest := startGraphIngestForOFW(t, client)
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
	write := func(callID string, todos ...map[string]any) {
		t.Helper()
		result, err := executor.Execute(ctx, agentic.ToolCall{
			ID: callID, Name: agentictools.WriteTodosToolName, LoopID: loopID,
			Arguments: map[string]any{"todos": todos},
		})
		require.NoError(t, err)
		require.Empty(t, result.Error)
	}

	write("multi",
		map[string]any{"id": "one", "content": "first", "status": "completed"},
		map[string]any{"id": "two", "content": "second", "status": "in_progress"},
	)
	entity := readEntityKV(t, client, entityID)
	assert.ElementsMatch(t, []string{"one", "two"}, objectsOf(entity, agvocab.TodoID))
	for _, predicate := range []string{
		agvocab.TodoID,
		agvocab.TodoContent,
		agvocab.TodoStatus,
		agvocab.TodoPosition,
		agvocab.TodoUpdatedAt,
	} {
		assert.Equal(t, 2, predicatesPresent(entity, predicate), predicate)
	}
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome))

	write("shorter",
		map[string]any{"id": "two", "content": "second revised", "status": "completed"},
	)
	entity = readEntityKV(t, client, entityID)
	assert.Equal(t, []string{"two"}, objectsOf(entity, agvocab.TodoID),
		"omitted todo ID must be deleted")
	assert.Equal(t, []string{"second revised"}, objectsOf(entity, agvocab.TodoContent))
	for _, predicate := range []string{
		agvocab.TodoID,
		agvocab.TodoContent,
		agvocab.TodoStatus,
		agvocab.TodoPosition,
		agvocab.TodoUpdatedAt,
	} {
		assert.Equal(t, 1, predicatesPresent(entity, predicate), predicate)
	}
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome),
		"unrelated fact must survive replacement")

	write("clear")
	entity = readEntityKV(t, client, entityID)
	for _, predicate := range []string{
		agvocab.TodoID,
		agvocab.TodoContent,
		agvocab.TodoStatus,
		agvocab.TodoPosition,
		agvocab.TodoUpdatedAt,
	} {
		assert.Zero(t, predicatesPresent(entity, predicate), predicate)
	}
	assert.Equal(t, []string{"running"}, objectsOf(entity, agvocab.LoopOutcome),
		"unrelated fact must survive empty desired reconciliation")
	assert.Equal(t, []string{"worker"}, objectsOf(entity, agvocab.LoopRole),
		"immutable birth fact must survive todo reconciliation")
}
