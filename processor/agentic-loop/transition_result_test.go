package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
		{"A5 continuation refused by a busy loop", HandlerResult{}, ErrLoopBusy, false},
		{"A6 approval handler panic", HandlerResult{LoopID: loopID},
			errs.WrapFatal(errors.New("panic"), "agentic-loop", "HandleApprovalResponse", "recover panic"), false},
		{"A7 store failure after routing", HandlerResult{}, errTransitionResultStore, false},
		// GetLoop wraps ErrLoopNotFound since #1377 (OQ3 (ii)), so the lane
		// takes the cold branch on this delivery; the pair is still not a
		// failed terminal.
		{"A8 loop released after the gate resolved", HandlerResult{},
			errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "GetLoop", "find loop"), false},
		{"D1 budget exhausted on the model lane", HandlerResult{LoopID: loopID, State: agentic.LoopStateExploring},
			errs.WrapFatal(ErrMaxIterationsReached, "agentic-loop", "HandleModelResponse", "iteration budget"), false},
		{"D2 tool-lane error after a mutation", HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting},
			context.Canceled, false},
		{"D3 approved call not dispatched", HandlerResult{LoopID: loopID, State: agentic.LoopStateExecuting},
			errs.Wrap(errors.New("resolve subject"), "agentic-loop", "dispatchApprovedCall", "dispatch approved tool call"), false},
		// A cancel that lands between the approval's resolve and its GetLoop
		// hands the lane a cancelled loop; once the cancel lane has released
		// it, dispatchApprovedCall fails in AddPendingTool, before any
		// publication (design P1, amended). Cancelled is terminal: the
		// failed-terminal reading takes it to the carrier.
		{"D3 cancelled: an approved call racing a cancel", HandlerResult{LoopID: loopID,
			State: agentic.LoopStateCancelled, PublishedMessages: []PublishedMessage{}},
			errs.Wrap(errs.Wrap(fmt.Errorf("loop %s not found", loopID), "LoopManager", "operation", "find loop"),
				"agentic-loop", "dispatchApprovedCall", "dispatch approved tool call"), true},
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
				// GetLoop's own shape since #1377: the sentinel is wrapped, and
				// the tool lane still does not read it (design OQ3 (ii)).
				return HandlerResult{}, errs.Wrap(fmt.Errorf("loop %s: %w", loopID, ErrLoopNotFound), "LoopManager", "GetLoop", "find loop")
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

// TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition drives the task
// lane's failure sites through the production heartbeat policy (#1345), so the
// decision asserted is the one the lane's binding settles. Before #1345 every
// row below was acknowledged: the delivery was consumed and the task was gone,
// with an error log as its only trace.
//
// Nothing durable precedes any of these failures, so the class decides: bytes
// that never decode and invalid input are terminated, a transient refusal is
// retried into a fresh birth, and the one defined refusal among them — a
// continuation of a loop with work in flight — stays acknowledged, because a
// Retry would park the whole task lane at MaxAckPending 1 (design OQ3 (a)).
// There is no post-registration row: no production path fails there (design
// § 6.12).
//
// spec: agentic-loop / Loop input classes settle after owner-specific durable done
func TestTheTaskLaneSettlesEachProducedErrorOnItsOwnDisposition(t *testing.T) {
	const heldLoop = "0d5e7f3a-2b1c-4e8d-9f60-7a8b9c0d1e2f"
	task := func(taskID, loopID string, shape func(*agentic.TaskMessage)) []byte {
		msg := agentic.TaskMessage{TaskID: taskID, LoopID: loopID, Role: "general", Model: "model-a",
			Prompt: "a turn"}
		if shape != nil {
			shape(&msg)
		}
		return baseMessageBytes(t, &msg)
	}
	for _, row := range []struct {
		name string
		// held prepares a loop this process holds, when the row needs one.
		held     func(t *testing.T, c *Component)
		data     func(t *testing.T) []byte
		cancel   bool
		decision natsclient.DeliveryDecision
	}{
		{name: "undecodable bytes are terminated",
			data:     func(*testing.T) []byte { return []byte("not a message") },
			decision: natsclient.DeliveryDecisionTerminate},
		{name: "a payload that is not a task is terminated",
			data: func(t *testing.T) []byte {
				return baseMessageBytes(t, &agentic.AgentResponse{RequestID: "r", Status: agentic.StatusComplete})
			},
			decision: natsclient.DeliveryDecisionTerminate},
		{name: "an over-depth task is terminated",
			data: func(*testing.T) []byte {
				return task("task-too-deep", "", func(m *agentic.TaskMessage) { m.Depth, m.MaxDepth = 2, 2 })
			},
			decision: natsclient.DeliveryDecisionTerminate},
		{name: "a task whose delivery was cancelled before anything is registered is retried",
			data:     func(*testing.T) []byte { return task("task-cancelled", "", nil) },
			cancel:   true,
			decision: natsclient.DeliveryDecisionRetry},
		{name: "a continuation of a settled loop is terminated",
			held: func(t *testing.T, c *Component) {
				require.NoError(t, c.handler.loopManager.TransitionLoop(heldLoop, agentic.LoopStateComplete))
			},
			data:     func(*testing.T) []byte { return task("task-after-the-end", heldLoop, nil) },
			decision: natsclient.DeliveryDecisionTerminate},
		{name: "a continuation of a loop with a tool call in flight is acknowledged as a defined refusal",
			held: func(t *testing.T, c *Component) {
				require.NoError(t, c.handler.loopManager.AddPendingTool(heldLoop, "call-in-flight"))
			},
			data:     func(*testing.T) []byte { return task("task-while-busy", heldLoop, nil) },
			decision: natsclient.DeliveryDecisionAck},
	} {
		t.Run(row.name, func(t *testing.T) {
			handler := NewMessageHandler(DefaultConfig())
			c := releaseTestComponent(t, handler)
			c.loopsBucket = &recordingLoopBucket{}
			if row.held != nil {
				_, err := handler.loopManager.CreateLoopWithID(heldLoop, "task-held", "general", "model-a", 5)
				require.NoError(t, err)
				seedLoopRecord(t, c, heldLoop)
				row.held(t, c)
			}
			lane := c.taskInputHandler(time.Minute)
			if row.cancel {
				// handleTaskMessage's own answer, under a context cancelled
				// before it ran. taskInputHandler would also turn a nil into
				// Retry on its expired work context; this row pins that the
				// lane's handler no longer answers nil there itself.
				lane = func(ctx context.Context, data []byte) error {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					return c.handleTaskMessage(cancelled, data)
				}
			}
			msg := &loopDeliveryOwnerMsg{data: row.data(t)}
			settled, admitted := deliverylane.Consume(t.Context(), msg,
				heartbeatPolicyForTest(t, "agent.task", lane), deliverylane.NewAdmission(nil, nil))

			require.True(t, admitted)
			require.Equal(t, row.decision, settled.Decision())
			if row.cancel {
				_, active := handler.loopManager.HasActiveLoopForTask("task-cancelled")
				require.False(t, active,
					"nothing may be registered, or the retry would be deduplicated into an acknowledgement")
			}
		})
	}
}
