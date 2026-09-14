package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTerminalPathsEvictActiveTrajectory(t *testing.T) {
	t.Run("completed result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-completed", "general", "model", 3)
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: DefaultConfig(), loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		require.NoError(t, handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, "done", ""))
		require.NoError(t, component.persistHandlerResult(t.Context(), HandlerResult{
			LoopID: loopID, State: agentic.LoopStateComplete,
			CompletionState: &agentic.LoopCompletedEvent{
				LoopID: loopID, TaskID: entity.TaskID, Outcome: agentic.OutcomeSuccess, Result: "done", CompletedAt: time.Now(),
			},
		}, revision))

		_, err = handler.trajectoryManager.getTrajectory(loopID)
		require.Error(t, err, "completed loop retained its active trajectory")
	})

	t.Run("failed result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoopWithID(handler.loopManager.GenerateLoopID(), "task", "role", "model")
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		entity, err := handler.loopManager.GetLoop(loopID)
		require.NoError(t, err)

		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: DefaultConfig(), loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		require.NoError(t, component.handleLoopFailure(t.Context(), loopID, entity, "test_failure", errors.New("boom"), revision))

		_, err = handler.trajectoryManager.getTrajectory(loopID)
		require.Error(t, err, "failed loop retained its active trajectory")
	})

	t.Run("cancelled result", func(t *testing.T) {
		config := DefaultConfig()
		config.Ports.Outputs = withoutPort(config.Ports.Outputs, "agent.complete")
		handler := NewMessageHandler(config)
		loopID, err := handler.loopManager.CreateLoopWithID(handler.loopManager.GenerateLoopID(), "task", "role", "model")
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		before := settlementLoopRecord(t, entity)
		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: before}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: config, loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		decision, cancelErr := component.handleCancelSignal(context.Background(), agentic.UserSignal{
			LoopID: loopID,
			Type:   agentic.SignalCancel,
			UserID: "operator",
		})
		err = cancelErr
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.ErrorContains(t, err, "resolve selected terminal subject", "test must reach the selected cancellation publication seam")
		require.Contains(t, bucket.values, "COMPLETE_"+loopID)
		require.Equal(t, before, bucket.values[loopID], "failed cancellation must preserve the durable nonterminal marker")
		require.Equal(t, revision, bucket.revisions[loopID])

		_, err = handler.trajectoryManager.getTrajectory(loopID)
		require.Error(t, err, "failed cancellation retained speculative trajectory state")
	})
}

func TestHandleTaskRollbackEvictsActiveTrajectory(t *testing.T) {
	config := DefaultConfig()
	config.Ports.Outputs = withoutPort(config.Ports.Outputs, "agent.request")
	handler := NewMessageHandler(config)
	loopID := "rolled-back-loop"

	_, err := handler.HandleTask(context.Background(), TaskMessage{
		LoopID: loopID,
		TaskID: "task",
		Role:   "role",
		Model:  "model",
		Prompt: "prompt",
	})
	require.Error(t, err)

	_, err = handler.trajectoryManager.getTrajectory(loopID)
	require.Error(t, err, "rolled-back task retained its active trajectory")
}

func TestTimedOutToolResultEvictsActiveTrajectory(t *testing.T) {
	config := DefaultConfig()
	handler := NewMessageHandler(config)
	loopID, err := handler.loopManager.CreateLoopWithID(handler.loopManager.GenerateLoopID(), "task", "role", "model")
	require.NoError(t, err)
	_, err = handler.trajectoryManager.startTrajectory(loopID)
	require.NoError(t, err)
	require.NoError(t, handler.loopManager.SetTimeout(loopID, -time.Second))

	const callID = "timed-out-call"
	requestID := loopID + ":req:" + uuid.NewString()
	executionID := deriveToolExecutionID(requestID, callID, 1)
	handler.loopManager.TrackToolCall(executionID, loopID)
	handler.loopManager.TrackToolName(executionID, "search")
	handler.loopManager.TrackToolOrdinal(executionID, 1)
	entity, err := handler.GetLoop(loopID)
	require.NoError(t, err)
	bucket := &terminalSelectionBucket{&approvalRevisionBucket{
		settlementBucket: &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}},
		revisions:        make(map[string]uint64),
	}}
	registry := payloadbuiltins.NewTestRegistry(t)
	component := &Component{
		config:      config,
		handler:     handler,
		decoder:     message.NewDecoder(registry),
		logger:      discardLogger(),
		loopsBucket: bucket,
	}
	toolResult := agentic.ToolResult{
		LoopID: loopID, RequestID: requestID, ExecutionID: executionID,
		CallID: callID, CallOrdinal: 1, Name: "search", Content: "late result",
	}
	envelope := message.NewBaseMessage(toolResult.Schema(), &toolResult, "test")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	decision, err := component.handleToolResultMessage(t.Context(), data)
	require.NoError(t, err)
	require.Equal(t, natsclient.DeliveryDecisionAck, decision)
	require.Contains(t, bucket.values, "COMPLETE_"+loopID, "timeout must settle its business failure before cleanup")

	_, err = handler.trajectoryManager.getTrajectory(loopID)
	require.Error(t, err, "timed-out tool-result failure retained its active trajectory")
}

func TestTrajectoryDiscardIsSafeWithConcurrentReadersAndWriters(t *testing.T) {
	manager := newTrajectoryManager()
	loopID := "concurrent-loop"
	_, err := manager.startTrajectory(loopID)
	require.NoError(t, err)

	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range 100 {
				_, _ = manager.addStep(loopID, agentic.TrajectoryStep{
					Timestamp: time.Now(),
					StepType:  "model_call",
				})
				_, _ = manager.getTrajectory(loopID)
			}
		}()
	}
	close(start)
	manager.discardTrajectory(loopID)
	workers.Wait()

	_, err = manager.getTrajectory(loopID)
	require.Error(t, err)
}

func withoutPort(ports []component.PortDefinition, name string) []component.PortDefinition {
	filtered := make([]component.PortDefinition, 0, len(ports))
	for _, port := range ports {
		if port.Name != name {
			filtered = append(filtered, port)
		}
	}
	return filtered
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The observed-audit-loss marker is per-loop state with a bounded
// lifetime: every terminal path that evicts the trajectory aggregate must
// also release the marker, or a long-running process accumulates one entry
// per audit-losing loop forever. releaseLoopTransientState is the single
// release point precisely so a terminal path cannot free one and leak the
// other — these subtests hold it to that on all three.
func TestTerminalPathsReleaseObservedAuditLoss(t *testing.T) {
	observe := func(t *testing.T, c *Component, loopID string) {
		t.Helper()
		c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
			Stage:  trajectoryStageEvidencePut,
			Kind:   agentic.TrajectoryKindToolCompleted,
			Reason: trajectoryReasonBackend,
			LoopID: loopID,
			Err:    errors.New("boom"),
		})
		require.True(t, c.trajectoryAuditLoss.observed(loopID), "marker was not set before the terminal path ran")
	}

	t.Run("completed result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoop("task-completed", "general", "model", 3)
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: DefaultConfig(), loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		require.NoError(t, handler.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete))
		require.NoError(t, handler.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, "done", ""))
		observe(t, component, loopID)
		require.NoError(t, component.persistHandlerResult(t.Context(), HandlerResult{
			LoopID: loopID, State: agentic.LoopStateComplete,
			CompletionState: &agentic.LoopCompletedEvent{
				LoopID: loopID, TaskID: entity.TaskID, Outcome: agentic.OutcomeSuccess, Result: "done", CompletedAt: time.Now(),
			},
		}, revision))

		require.False(t, component.trajectoryAuditLoss.observed(loopID),
			"completed loop retained its audit-loss marker")
	})

	t.Run("failed result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID, err := handler.loopManager.CreateLoopWithID(handler.loopManager.GenerateLoopID(), "task", "role", "model")
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)
		entity, err := handler.loopManager.GetLoop(loopID)
		require.NoError(t, err)

		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: settlementLoopRecord(t, entity)}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: DefaultConfig(), loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		observe(t, component, loopID)
		require.NoError(t, component.handleLoopFailure(t.Context(), loopID, entity, "test_failure", errors.New("boom"), revision))

		require.False(t, component.trajectoryAuditLoss.observed(loopID),
			"failed loop retained its audit-loss marker")
	})

	t.Run("cancelled result", func(t *testing.T) {
		config := DefaultConfig()
		config.Ports.Outputs = withoutPort(config.Ports.Outputs, "agent.complete")
		handler := NewMessageHandler(config)
		loopID, err := handler.loopManager.CreateLoopWithID(handler.loopManager.GenerateLoopID(), "task", "role", "model")
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		entity, err := handler.GetLoop(loopID)
		require.NoError(t, err)
		before := settlementLoopRecord(t, entity)
		bucket := &terminalSelectionBucket{&approvalRevisionBucket{
			settlementBucket: &settlementBucket{values: map[string][]byte{loopID: before}},
			revisions:        make(map[string]uint64),
		}}
		component := &Component{handler: handler, config: config, loopsBucket: bucket, logger: discardLogger()}
		_, revision, err := component.readLoopEntityRevision(t.Context(), loopID)
		require.NoError(t, err)
		observe(t, component, loopID)
		decision, cancelErr := component.handleCancelSignal(context.Background(), agentic.UserSignal{
			LoopID: loopID,
			Type:   agentic.SignalCancel,
			UserID: "operator",
		})
		err = cancelErr
		require.Equal(t, natsclient.DeliveryDecisionQuarantine, decision)
		require.ErrorContains(t, err, "resolve selected terminal subject", "test must reach the selected cancellation publication seam")
		require.Contains(t, bucket.values, "COMPLETE_"+loopID)
		require.Equal(t, before, bucket.values[loopID], "failed cancellation must preserve the durable nonterminal marker")
		require.Equal(t, revision, bucket.revisions[loopID])

		require.False(t, component.trajectoryAuditLoss.observed(loopID),
			"failed cancellation retained its speculative audit-loss marker")
	})
}
