package agenticloop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/require"
)

// TestAGateNeverRevertsACancelThatRacesIt (#1362 checkpoint 2 re-review, M1):
// the gate used to read the loop, gate the copy and write the copy back, in
// two lock sections. A cancel landing between them was overwritten — the
// loop went back to awaiting_approval after it had been cancelled. The gate
// now begins under the manager's own lock, so whichever of the two runs
// second sees the other's effect.
//
// A race, so the proof is statistical: every iteration starts the gate and
// the cancel together behind one barrier and joins both, and the invariant is
// checked on every iteration. Run under -race.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAGateNeverRevertsACancelThatRacesIt(t *testing.T) {
	const iterations = 300
	ctx := context.Background()

	for i := range iterations {
		h := NewMessageHandler(DefaultConfig())
		born, err := h.HandleTask(ctx, TaskMessage{TaskID: "task-gate-race", Role: "general", Model: "m", Prompt: "p"})
		require.NoError(t, err)
		loopID := born.LoopID
		dispatch, err := h.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
			RequestID: h.OutstandingRequestForTest(loopID),
			Status:    agentic.StatusToolCall,
			Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{
				{ID: "call-gated", Name: "delete_rule"},
			}},
		})
		require.NoError(t, err)
		var call agentic.ToolCall
		for _, msg := range dispatch.PublishedMessages {
			if strings.HasPrefix(msg.Subject, "tool.execute.") {
				var envelope struct {
					Payload agentic.ToolCall `json:"payload"`
				}
				require.NoError(t, json.Unmarshal(msg.Data, &envelope))
				call = envelope.Payload
			}
		}
		require.NotEmpty(t, call.ExecutionID)
		gated := agentic.ToolResult{
			LoopID: loopID, CallID: call.ID, Name: call.Name,
			RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
			ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "needs a human",
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _ = h.HandleToolResult(ctx, loopID, gated)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _ = h.CancelLoop(loopID, "operator")
		}()
		close(start)
		wg.Wait()

		entity, err := h.GetLoop(loopID)
		require.NoError(t, err)
		require.Equalf(t, agentic.LoopStateCancelled, entity.State,
			"iteration %d: the gate wrote back a copy taken before the cancel and reverted it", i)
	}
}
