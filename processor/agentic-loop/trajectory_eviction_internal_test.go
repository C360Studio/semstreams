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
		loopID, err := handler.loopManager.CreateLoopWithID("failed-loop", "task", "role", "model")
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
		loopID, err := handler.loopManager.CreateLoopWithID("cancelled-loop", "task", "role", "model")
		require.NoError(t, err)
		_, err = handler.trajectoryManager.startTrajectory(loopID)
		require.NoError(t, err)

		component := &Component{handler: handler, config: config, logger: discardLogger()}
		component.handleCancelSignal(context.Background(), agentic.UserSignal{
			LoopID: loopID,
			Type:   agentic.SignalCancel,
			UserID: "operator",
		})

		_, err = handler.trajectoryManager.getTrajectory(loopID)
		require.Error(t, err, "cancelled loop retained its active trajectory")
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
	loopID, err := handler.loopManager.CreateLoopWithID("timed-out-tool-loop", "task", "role", "model")
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
