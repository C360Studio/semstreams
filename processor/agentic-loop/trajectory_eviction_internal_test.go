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
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/require"
)

func TestTerminalPathsEvictActiveTrajectory(t *testing.T) {
	t.Run("completed result", func(t *testing.T) {
		handler := NewMessageHandler(DefaultConfig())
		loopID := "completed-loop"
		_, err := handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		component := &Component{handler: handler, logger: discardLogger()}
		component.persistHandlerResult(context.Background(), HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateComplete,
		})

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

		component := &Component{handler: handler, config: DefaultConfig(), logger: discardLogger()}
		component.handleLoopFailure(context.Background(), loopID, entity, "test_failure", errors.New("boom"))

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

		component := &Component{handler: handler, config: config, logger: discardLogger()}
		err = component.handleCancelSignal(context.Background(), agentic.UserSignal{
			LoopID: loopID,
			Type:   agentic.SignalCancel,
			UserID: "operator",
		})
		require.Error(t, err, "missing completion output is an unknown terminal side effect")

		_, err = handler.trajectoryManager.getTrajectory(loopID)
		require.NoError(t, err, "failed cancellation durability released its active trajectory")
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
	handler.loopManager.TrackToolCall(callID, loopID)
	registry := payloadbuiltins.NewTestRegistry(t)
	component := &Component{
		config:  config,
		handler: handler,
		decoder: message.NewDecoder(registry),
		logger:  discardLogger(),
	}
	toolResult := agentic.ToolResult{CallID: callID, Name: "search", Content: "late result"}
	envelope := message.NewBaseMessage(toolResult.Schema(), &toolResult, "test")
	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	component.handleToolResultMessage(context.Background(), data)

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
		loopID := "completed-loop-audit"
		_, err := handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		component := &Component{handler: handler, logger: discardLogger()}
		observe(t, component, loopID)
		component.persistHandlerResult(context.Background(), HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateComplete,
		})

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

		component := &Component{handler: handler, config: DefaultConfig(), logger: discardLogger()}
		observe(t, component, loopID)
		component.handleLoopFailure(context.Background(), loopID, entity, "test_failure", errors.New("boom"))

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

		component := &Component{handler: handler, config: config, logger: discardLogger()}
		observe(t, component, loopID)
		err = component.handleCancelSignal(context.Background(), agentic.UserSignal{
			LoopID: loopID,
			Type:   agentic.SignalCancel,
			UserID: "operator",
		})
		require.Error(t, err, "missing completion output is an unknown terminal side effect")

		require.True(t, component.trajectoryAuditLoss.observed(loopID),
			"failed cancellation durability released its audit-loss marker")
	})
}
