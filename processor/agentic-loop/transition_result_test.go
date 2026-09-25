package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/internal/deliverylane"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/stretchr/testify/require"
)

// The (result, error) pairs the loop's handlers produce, and the one reading
// failedTerminal gives them (#1376, design § 2). Each row is a produced
// combination named by its design row, with a literal expectation — never one
// recomputed from the predicate's own formula. The rows that are not produced
// are marked so: they pin a clause of the requirement the produced rows cannot
// reach on their own.

var errTransitionResultStore = errors.New("store the tool result")

func timedOutResult(loopID string) HandlerResult {
	return HandlerResult{
		LoopID:            loopID,
		State:             agentic.LoopStateFailed,
		FailureState:      &agentic.LoopFailedEvent{LoopID: loopID, Reason: loopTimeoutReason},
		PublishedMessages: []PublishedMessage{{Subject: "agent.failed." + loopID, Data: []byte(`{}`)}},
	}
}

func timeoutError(op string) error {
	return errs.WrapFatal(errLoopTimedOut, "agentic-loop", op, "check timeout")
}

// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestFailedTerminalReadsEachProducedPairOnce(t *testing.T) {
	const loopID = "loop-transition-result"
	for _, row := range []struct {
		name   string
		result HandlerResult
		err    error
		want   bool
	}{
		// C: an error accompanying a populated terminal is the loop's settlement.
		{"C1 timeout failure from the tool, approval or sweep lane", timedOutResult(loopID), timeoutError("HandleToolResult"), true},
		{"C2 the same failure on the model lane", timedOutResult(loopID), timeoutError("HandleModelResponse"), true},
		{"C1 invariant (not produced): the error's class is not read", timedOutResult(loopID), errors.New("plain"), true},
		{"C1 invariant (not produced): an invalid-class error is still the cause", timedOutResult(loopID),
			errs.WrapInvalid(errors.New("invalid"), "agentic-loop", "test", "invariant"), true},
		{"E1 (no production trigger): a terminal state with no event goes to the owner",
			HandlerResult{LoopID: loopID, State: agentic.LoopStateComplete}, errors.New("marshal completion"), true},

		// B: no error is never a failed terminal, whatever the shape.
		{"B1 birth", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring, Created: true}, nil, false},
		{"B2 duplicate task", HandlerResult{LoopID: loopID}, nil, false},
		{"B3 deferred continuation", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring, Deferred: true}, nil, false},
		{"B4 terminal guard", terminalGuardResult(loopID, agentic.LoopStateFailed), nil, false},
		{"B5 ordinary advance", HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting,
			PublishedMessages: []PublishedMessage{{Subject: "tool.execute.x", Data: []byte(`{}`)}}}, nil, false},
		{"B6 approval gate", HandlerResult{LoopID: loopID, State: agentic.LoopStateAwaitingApproval}, nil, false},
		{"B7 completion", HandlerResult{LoopID: loopID, State: agentic.LoopStateComplete,
			CompletionState: &agentic.LoopCompletedEvent{LoopID: loopID}}, nil, false},
		{"B7 max-iterations failure", HandlerResult{LoopID: loopID, State: agentic.LoopStateFailed,
			FailureState: &agentic.LoopFailedEvent{LoopID: loopID, Reason: "max_iterations"}}, nil, false},
		{"B8 stale drop", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring, staleDrop: true}, nil, false},

		// A guard result is never returned with an error (design P3); if one
		// were, the record decides, not the failed-terminal reading.
		{"B4 invariant (not produced): a guard result with an error",
			terminalGuardResult(loopID, agentic.LoopStateCancelled), errors.New("never produced"), false},

		// A and D: every other error keeps its lane's own disposition.
		{"A1 cancelled before any mutation", HandlerResult{},
			fmt.Errorf("%w: %w", errCancelledBeforeMutation, context.Canceled), false},
		{"A2 invalid approval answer", HandlerResult{},
			errs.WrapInvalid(errors.New("no decision"), "agentic-loop", "HandleApprovalResponse", "validate response"), false},
		{"A3 loop not held", HandlerResult{LoopID: loopID}, ErrLoopNotFound, false},
		{"A4 superseded model response", HandlerResult{}, errResponseSuperseded, false},
		{"A4 response newer than the record", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring},
			errRequestNotYetObservable, false},
		{"A6 approval handler panic", HandlerResult{LoopID: loopID},
			errs.WrapFatal(errors.New("panic"), "agentic-loop", "HandleApprovalResponse", "recover panic"), false},
		{"A7 store failure after routing", HandlerResult{}, errTransitionResultStore, false},
		{"D1 budget exhausted on the model lane", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring},
			errs.WrapFatal(ErrMaxIterationsReached, "agentic-loop", "HandleModelResponse", "iteration budget"), false},
		{"D2 tool-lane error after a mutation", HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting},
			context.Canceled, false},
		{"D3 approved call not dispatched", HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting},
			errs.Wrap(errors.New("resolve subject"), "agentic-loop", "dispatchApprovedCall", "dispatch approved tool call"), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			require.Equal(t, row.want, failedTerminal(row.result, row.err))
		})
	}
}

// TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition drives the
// tool lane's handler-error branch through the production heartbeat policy,
// so the decision is the one the lane's binding settles. The failed-terminal
// row commits through the terminal owner and is acknowledged; every other
// error keeps the lane's own disposition — retried only when the cancellation
// provably preceded any mutation, quarantined otherwise, whatever the class.
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestTheToolLaneSettlesEachProducedErrorPairOnItsOwnDisposition(t *testing.T) {
	for _, row := range []struct {
		name      string
		pair      func(h *MessageHandler, loopID string) (HandlerResult, error)
		decision  natsclient.DeliveryDecision
		committed bool
	}{
		{"C1 the loop deadline is committed and acknowledged",
			func(h *MessageHandler, loopID string) (HandlerResult, error) {
				return h.failTimedOutLoop(loopID, HandlerResult{LoopID: loopID}, "HandleToolResult")
			}, natsclient.DeliveryDecisionAck, true},
		{"A1 cancelled before any mutation is retried",
			func(_ *MessageHandler, _ string) (HandlerResult, error) {
				return HandlerResult{}, fmt.Errorf("%w: %w", errCancelledBeforeMutation, context.Canceled)
			}, natsclient.DeliveryDecisionRetry, false},
		{"A3 a loop released mid-delivery is quarantined",
			func(_ *MessageHandler, loopID string) (HandlerResult, error) {
				return HandlerResult{}, errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "GetLoop", "find loop")
			}, natsclient.DeliveryDecisionQuarantine, false},
		{"A7 a store failure after routing is quarantined",
			func(_ *MessageHandler, _ string) (HandlerResult, error) {
				return HandlerResult{}, errTransitionResultStore
			}, natsclient.DeliveryDecisionQuarantine, false},
		{"D2 a cancellation after a mutation is quarantined",
			func(_ *MessageHandler, loopID string) (HandlerResult, error) {
				return HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting}, context.Canceled
			}, natsclient.DeliveryDecisionQuarantine, false},
		{"D2 an invalid-class error after a mutation is quarantined, not terminated",
			func(_ *MessageHandler, loopID string) (HandlerResult, error) {
				return HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting},
					errs.WrapInvalid(errors.New("bad"), "agentic-loop", "HandleToolResult", "test")
			}, natsclient.DeliveryDecisionQuarantine, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			handler := NewMessageHandler(DefaultConfig())
			loopID, err := handler.loopManager.CreateLoop("task-transition-result", "general", "model", 3)
			require.NoError(t, err)
			c := releaseTestComponent(t, handler)
			bucket := &recordingLoopBucket{}
			c.loopsBucket = bucket
			seedLoopRecord(t, c, loopID)
			result, cause := row.pair(handler, loopID)
			require.Error(t, cause, "fixture: every row here is an error pair")

			settled, admitted := deliverylane.Consume(t.Context(), &loopDeliveryOwnerMsg{data: []byte("{}")},
				heartbeatPolicyForTest(t, "tool.result", func(ctx context.Context, _ []byte) error {
					return c.settleFailedToolResult(ctx, loopID, result, cause)
				}), deliverylane.NewAdmission(nil, nil))

			require.True(t, admitted)
			require.Equal(t, row.decision, settled.Decision())
			require.Equal(t, row.committed, failedTerminal(result, cause))
			if row.committed {
				require.Contains(t, bucket.written(), terminalMarkerKey(loopID),
					"the failed terminal must pass through the terminal owner")
				require.Equal(t, agentic.LoopStateFailed, persistedLoop(t, bucket, loopID).State)
			} else {
				require.Empty(t, bucket.written(), "a refusal on this lane writes nothing")
			}
		})
	}
}
