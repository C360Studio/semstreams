package agenticloop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
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
		h, loopID, gated := aLoopWithAGatedResultInFlight(t)

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

// aLoopWithAGatedResultInFlight is a loop that dispatched one call, and the
// approval_required result that call's executor returned.
func aLoopWithAGatedResultInFlight(t *testing.T) (*MessageHandler, string, agentic.ToolResult) {
	t.Helper()
	ctx := context.Background()
	h := NewMessageHandler(DefaultConfig())
	born, err := h.HandleTask(ctx, TaskMessage{TaskID: "task-gate", Role: "general", Model: "m", Prompt: "p"})
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
	return h, loopID, agentic.ToolResult{
		LoopID: loopID, CallID: call.ID, Name: call.Name,
		RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
		ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "needs a human",
	}
}

// The race above, run in one order: a cancel lands first and the gate that
// follows refuses, leaving the cancel standing (#1362 checkpoint 2 delta
// review).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAGateAfterACancelRefusesAndLeavesTheCancel(t *testing.T) {
	h, loopID, gated := aLoopWithAGatedResultInFlight(t)
	_, err := h.CancelLoop(loopID, "operator")
	require.NoError(t, err)

	_, err = h.gateForApproval(loopID, gated)

	var refused *gateRefusedError
	require.ErrorAs(t, err, &refused, "a gate began on a cancelled loop")
	require.Equal(t, agentic.LoopStateCancelled, refused.state)
	entity, err := h.GetLoop(loopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateCancelled, entity.State, "the gate reverted the cancel")
	require.Nil(t, entity.PendingApproval)
}

// The original clobber, run in one order: the gated result the handler stores
// before it gates survives the gate, because the gate is written over the live
// entity rather than a copy taken before the store (#1362 checkpoint 2).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTheGateKeepsTheGatedResultStoredBeforeIt(t *testing.T) {
	h, loopID, gated := aLoopWithAGatedResultInFlight(t)
	require.NoError(t, h.loopManager.StoreToolResult(loopID, gated))

	_, err := h.gateForApproval(loopID, gated)

	require.NoError(t, err)
	entity, err := h.GetLoop(loopID)
	require.NoError(t, err)
	require.Equal(t, agentic.LoopStateAwaitingApproval, entity.State)
	stored, held := entity.PendingToolResults[gated.ExecutionID]
	require.True(t, held, "the gate erased the gated result stored before it")
	require.Equal(t, gated.Error, stored.Error)
}

// A cancel that lands between the handler's terminal guard and the gate: the
// gate refuses, and the handler answers as its terminal guard does, so the
// carrier leaves the terminal to its owner and writes nothing. Claiming
// awaiting_approval there had the carrier render the cancelled entity into
// the record — a terminal outside its owner, ahead of COMPLETE_ and the cancel
// event (#1362 checkpoint 2 delta review, HIGH).
//
// spec: agentic-loop / The loop record names its outstanding request
func TestACancelBetweenTheTerminalGuardAndTheGateIsLeftToItsOwner(t *testing.T) {
	c, bucket, published, _ := terminalOwnerLoop(t)
	executionID := deriveToolExecutionID(published, "call-gated", 1)
	c.handler.loopManager.TrackToolCall(executionID, terminalOwnerLoopID)
	gated := agentic.ToolResult{
		LoopID: terminalOwnerLoopID, CallID: "call-gated", Name: "delete_rule",
		RequestID: published, ExecutionID: executionID, CallOrdinal: 1,
		ErrorKind: agentic.ToolErrorPermission, Error: agentic.ApprovalRequiredPrefix + "needs a human",
	}
	// The copy HandleToolResult holds once its terminal guard has passed.
	passedGuard, err := c.handler.GetLoop(terminalOwnerLoopID)
	require.NoError(t, err)
	require.False(t, passedGuard.State.IsTerminal())
	_, err = c.handler.CancelLoop(terminalOwnerLoopID, "operator")
	require.NoError(t, err)
	result := HandlerResult{LoopID: terminalOwnerLoopID, State: passedGuard.State}

	stop := c.handler.checkApprovalGate(terminalOwnerLoopID, &passedGuard, gated, &result)

	require.True(t, stop)
	require.True(t, result.terminalOwnedElsewhere, "a refused gate still claimed awaiting_approval")
	require.NotEqual(t, agentic.LoopStateAwaitingApproval, result.State)
	persistErr := c.persistHandlerResult(t.Context(), result)
	require.Error(t, persistErr)
	require.False(t, errs.IsFatal(persistErr), "an in-flight terminal is retried, not quarantined")
	require.Empty(t, bucket.written(), "the carrier wrote the cancelled loop outside its owner")
}
