package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// HandleApprovalResponse processes an ApprovalResponse for a loop
// awaiting human approval. Returns a HandlerResult populated with
// either a re-dispatched ToolCall (approve/modify) or a synthesised
// rejection ToolResult fed through the normal HandleToolResult path
// (reject). The component publishes any messages and persists the
// loop state.
//
// The transition out of awaiting_approval happens atomically inside
// LoopManager.ResolveApprovalIfPending. Two concurrent responses
// (e.g., a human approve racing an automated reject scheduler) cannot
// both pass the awaiting check — exactly one wins; the loser sees ok=
// false and we treat it as a stale-response idempotent drop. This is
// load-bearing for the safety claim: a sensitive tool must not
// dispatch twice off two responses for the same call_id.
//
// A loop this process does not hold is not decided here: memory cannot tell a
// settled loop from one a replaced process left gated, so ErrLoopNotFound is
// returned and the component reads the loop's record (the cold branch,
// settleApprovalResponseWithoutLoop; #1362, design § 5.5, D35). A loop this
// process holds that is not awaiting this gate is a stale drop: logged,
// never an error, never a dispatch.
func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (result HandlerResult, err error) {
	// A panic makes delivery ownership unsafe. Recover for diagnosis but return
	// a fatal error so the callback quarantines and drains the exact owner.
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("panic recovered in HandleApprovalResponse",
				slog.Any("panic", r),
				slog.String("loop_id", response.LoopID),
				slog.String("call_id", response.CallID),
				slog.String("decision", response.Decision))
			result = HandlerResult{LoopID: response.LoopID}
			err = errs.WrapFatal(fmt.Errorf("approval response handler panicked: %v", r),
				"agentic-loop", "HandleApprovalResponse", "recover panic")
		}
	}()

	if vErr := response.Validate(); vErr != nil {
		return HandlerResult{}, errs.WrapInvalid(vErr, "agentic-loop", "HandleApprovalResponse", "validate response")
	}

	loopID := response.LoopID

	pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID, response.ExecutionID)
	if resolveErr != nil {
		// ErrLoopNotFound included. A released loop and a loop some other
		// process gated look the same from here, and only the second still
		// owes the human's answer, so the RECORD decides — at the component,
		// which reads it (D35). Before #1362 absence was folded into the stale
		// drop below and acknowledged, and a replacement lost every answer.
		return HandlerResult{LoopID: loopID}, resolveErr
	}
	if !ok {
		// Stale or duplicate: the loop this process holds is no longer
		// awaiting approval (it was answered, timed out, or settled), or the
		// response names another gate than the one pinned. Both are an answer
		// that arrived too late to act on — log and drop, never error, never
		// dispatch.
		entity, getErr := h.GetLoop(loopID)
		state := agentic.LoopState("")
		if getErr == nil {
			state = entity.State
		}
		logApprovalResponseIgnored(h.logger, response, state)
		return HandlerResult{LoopID: loopID, State: state, staleDrop: true}, nil
	}

	// We won the resolve race. Fetch the now-resolved entity so the
	// HandlerResult.State reflects the restored prior state.
	entity, getErr := h.GetLoop(loopID)
	if getErr != nil {
		return HandlerResult{}, getErr
	}

	result = HandlerResult{
		LoopID:            loopID,
		State:             entity.State,
		PublishedMessages: []PublishedMessage{},
	}

	// The LOOP deadline outranks the human's answer, whatever it decided: an
	// approve or a modify must not dispatch a call on a loop whose budget is
	// spent, and a reject has nothing left to advance. This is the record's own
	// TimeoutAt, which a rebuild keeps (owner ruling, #1330
	// issuecomment-5781101792) — not the approval deadline. Checked after the
	// resolve, so the gate is cleared on the loop that fails, exactly as the
	// reject path always cleared it before reaching HandleToolResult's arm.
	if h.loopManager.IsTimedOut(loopID) {
		return h.failTimedOutLoop(loopID, result, "HandleApprovalResponse")
	}

	switch response.Decision {
	case agentic.ApprovalDecisionApprove:
		return result, h.dispatchApprovedCall(loopID, pending, pending.Arguments, response.ApprovedBy, &result)
	case agentic.ApprovalDecisionModify:
		args := response.ModifiedArguments
		if args == nil {
			args = pending.Arguments
		}
		return result, h.dispatchApprovedCall(loopID, pending, args, response.ApprovedBy, &result)
	case agentic.ApprovalDecisionReject:
		return h.handleRejectedApproval(ctx, loopID, pending, response)
	default:
		// Validate above already rejected unknown decisions; this is a
		// belt-and-braces guard.
		return HandlerResult{}, fmt.Errorf("unknown approval decision %q", response.Decision)
	}
}

// dispatchApprovedCall builds a ToolCall from the pending approval
// state (substituting modified args when present), stamps ApprovedBy
// so the agentic-tools approval filter recognises the bypass, and
// publishes it on the tool.execute port. The loop's normal
// tool.result path takes over from here.
func (h *MessageHandler) dispatchApprovedCall(loopID string, pending agentic.PendingApprovalState, args map[string]any, approvedBy string, result *HandlerResult) error {
	tc := agentic.ToolCall{
		ID:          pending.CallID,
		Name:        pending.ToolName,
		Arguments:   args,
		RequestID:   pending.RequestID,
		ExecutionID: pending.ExecutionID,
		CallOrdinal: pending.CallOrdinal,
		ApprovedBy:  approvedBy,
	}
	if err := h.dispatchToolCall(result, loopID, tc); err != nil {
		return errs.Wrap(err, "agentic-loop", "dispatchApprovedCall", "dispatch approved tool call")
	}
	return nil
}

// handleRejectedApproval synthesises a ToolResult carrying the
// distinctive ApprovalRejectedPrefix (NOT ApprovalRequiredPrefix) so
// the gate logic in HandleToolResult does not re-fire on this
// rejection. The synthesised result feeds through HandleToolResult so
// the trajectory step + tools-complete advancement share the normal
// code path.
func (h *MessageHandler) handleRejectedApproval(ctx context.Context, loopID string, pending agentic.PendingApprovalState, response agentic.ApprovalResponse) (HandlerResult, error) {
	approver := response.ApprovedBy
	if approver == "" {
		approver = "anonymous" // Timeout-driven auto-rejects have no human approver.
	}
	reasonSuffix := response.Reason
	if reasonSuffix == "" {
		reasonSuffix = "no reason provided"
	}
	synthetic := agentic.ToolResult{
		RequestID:   pending.RequestID,
		ExecutionID: pending.ExecutionID,
		CallID:      pending.CallID,
		CallOrdinal: pending.CallOrdinal,
		Name:        pending.ToolName,
		ErrorKind:   agentic.ToolErrorPermission,
		Error:       fmt.Sprintf("%srejected by %s: %s", agentic.ApprovalRejectedPrefix, approver, reasonSuffix),
		TraceID:     pending.TraceID,
	}
	return h.HandleToolResult(ctx, loopID, synthetic)
}

// handleApprovalResponseMessage is the component-level entry point
// that decodes the wire envelope and hands the typed payload to the
// MessageHandler. Mirrors handleSignalMessage's shape.
func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) (natsclient.DeliveryDecision, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("decode approval response: %w", err)
	}
	respPtr, ok := baseMsg.Payload().(*agentic.ApprovalResponse)
	if !ok {
		return natsclient.DeliveryDecisionTerminate,
			fmt.Errorf("unexpected approval response payload type %T", baseMsg.Payload())
	}
	response := *respPtr

	c.logger.Debug("Processing approval response",
		slog.String("loop_id", response.LoopID),
		slog.String("call_id", response.CallID),
		slog.String("decision", response.Decision),
		slog.String("approved_by", response.ApprovedBy))

	result, err := c.handler.HandleApprovalResponse(ctx, response)
	if errors.Is(err, ErrLoopNotFound) {
		rebuilt, coldErr := c.settleApprovalResponseWithoutLoop(ctx, response)
		if coldErr != nil {
			wrapped := fmt.Errorf("approval response for loop %q call %q: %w", response.LoopID, response.CallID, coldErr)
			if errs.IsFatal(coldErr) {
				return natsclient.DeliveryDecisionQuarantine, wrapped
			}
			return natsclient.DeliveryDecisionRetry, wrapped
		}
		if !rebuilt {
			return natsclient.DeliveryDecisionAck, nil
		}
		// Rebuilt: this process holds the gated loop now, and the answer takes
		// the warm path it would have taken on the process that gated it.
		result, err = c.handler.HandleApprovalResponse(ctx, response)
	}
	if err != nil && result.State.IsTerminal() && !result.terminalOwnedElsewhere {
		// A business failure — the loop's deadline, from this handler or from
		// HandleToolResult's arm on a reject — returns its populated terminal
		// result WITH the error. That result is the loop's settlement, so it is
		// committed through the terminal owner and the delivery settles on the
		// commit, as settleFailedToolResult does on the tool lane. Discarding
		// it behind a quarantine left the record gated and the lane stopped.
		c.logger.Warn("Approval answer settled its loop as failed",
			slog.String("loop_id", response.LoopID),
			slog.String("call_id", response.CallID),
			slog.String("error", err.Error()))
		err = c.persistHandlerResult(ctx, result, writeThenPublish)
		if err == nil {
			return natsclient.DeliveryDecisionAck, nil
		}
		if !errs.IsFatal(err) {
			return natsclient.DeliveryDecisionRetry,
				fmt.Errorf("approval failure for loop %q must be retried: %w", response.LoopID, err)
		}
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("approval failure for loop %q has unknown durable state: %w", response.LoopID, err)
	}
	if err != nil {
		wrapped := fmt.Errorf("handle approval response for loop %q call %q: %w", response.LoopID, response.CallID, err)
		switch {
		case errs.IsFatal(err):
			return natsclient.DeliveryDecisionQuarantine, wrapped
		case errs.IsInvalid(err):
			return natsclient.DeliveryDecisionTerminate, wrapped
		default:
			return natsclient.DeliveryDecisionRetry, wrapped
		}
	}
	if result.staleDrop {
		c.recordApprovalInapplicable(response)
		// The handler dispatched nothing and resolved nothing. Persisting would
		// re-Put a settled entity, or — once its per-loop state is released —
		// report a persistence failure for a loop that is supposed to be gone.
		// Returning here is what makes the two indistinguishable.
		return natsclient.DeliveryDecisionAck, nil
	}

	// Approval responses use the same persistence boundary as every other
	// handler result. This keeps a rejection that reaches the iteration cap from
	// bypassing the ordinary-observations-then-terminal audit ordering.
	// The lane publishes before it writes (#1362 task 1.4). A rejection that
	// completes its batch mints the loop's next request, and a crash between
	// that publication and the record update leaves the request retained under
	// a record still gated at the previous one — the reject-minted W4. The
	// redelivered answer closes it through the cold branch: step 0 adopts the
	// retained request and clears the gate in one compare-and-swap, and the
	// answer is then acknowledged as inapplicable with nothing republished.
	if result.terminalOwnedElsewhere {
		// The rejection reached a loop already terminal in memory. The gate
		// was resolved while the loop was awaiting_approval, so this is a
		// terminal (a cancel) that landed between that resolve and the
		// synthesized result's handling. The record decides.
		if err := c.settleTerminalGuard(ctx, result, c.recordTerminalToolResultDropped); err != nil {
			return natsclient.DeliveryDecisionRetry, err
		}
		return natsclient.DeliveryDecisionAck, nil
	}
	if err := c.persistHandlerResult(ctx, result, publishThenWrite); err != nil {
		// A transient failure — a lost compare-and-swap, which has released
		// the loop — is retried, not quarantined (#1362 re-review M3):
		// quarantining it would latch the lane's health on a benign race.
		if !errs.IsFatal(err) {
			return natsclient.DeliveryDecisionRetry,
				fmt.Errorf("approval result for loop %q must be retried: %w", response.LoopID, err)
		}
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("approval result for loop %q has unknown durable state: %w", response.LoopID, err)
	}
	return natsclient.DeliveryDecisionAck, nil
}

// recordApprovalInapplicable counts an approval answer acknowledged without
// effect, on the tool-result drop family (owner ruling 3, #1362
// issuecomment-5809906669). The ruling covers ANSWERS; the timeout sweeper's
// echo of its own auto-reject is not one, and is not counted (#1362
// checkpoint 2 re-review, M3).
func (c *Component) recordApprovalInapplicable(response agentic.ApprovalResponse) {
	if c.metrics != nil && !isTimeoutSweepEcho(response) {
		c.metrics.recordToolResultDropped("approval_inapplicable")
	}
}

// isTimeoutSweepEcho recognises the approval-timeout sweeper's own auto-reject
// coming back on agent.approval_response: the sweeper publishes it for wire
// observers only after its carrier committed the rejection
// (approval_sweeper.go), so by the time any consumer takes it the gate is
// resolved and it reaches only the inapplicable paths. It is recognised there,
// by the approver the sweeper stamps and the decision it makes.
func isTimeoutSweepEcho(response agentic.ApprovalResponse) bool {
	return response.ApprovedBy == approvalTimeoutSystemApprover &&
		response.Decision == agentic.ApprovalDecisionReject
}

// logApprovalResponseIgnored is the one audit line for an answer that is
// acknowledged without effect, warm or cold, so an operator greps one string
// whichever process took the delivery. The sweeper's own echo is expected
// and logs at Debug.
func logApprovalResponseIgnored(logger *slog.Logger, response agentic.ApprovalResponse, state agentic.LoopState) {
	if isTimeoutSweepEcho(response) {
		logger.Debug("approval response ignored: the timeout sweeper's own auto-reject echo",
			slog.String("loop_id", response.LoopID),
			slog.String("response_execution_id", response.ExecutionID),
			slog.String("loop_state", string(state)))
		return
	}
	logger.Warn("approval response ignored: not awaiting, or its identity does not match the pending call",
		slog.String("loop_id", response.LoopID),
		slog.String("response_call_id", response.CallID),
		slog.String("response_execution_id", response.ExecutionID),
		slog.String("loop_state", string(state)))
}

// continuationUnavailableReason is the failure reason of a loop whose approval
// could not be continued because the retained evidence a rebuild needs is
// confirmed gone (owner ruling 2026-09-13 on #1146; #1362 OQ-C).
const continuationUnavailableReason = "continuation_unavailable"

// settleApprovalResponseWithoutLoop is the approval lane's cold branch (#1362,
// design § 5.5, D35): an answer for a loop this process does not hold. It
// reports whether it REBUILT the loop, in which case the caller applies the
// answer warm; false with a nil error is an acknowledgement.
//
// In order:
//
//  1. Step 0 (design § 3.6): read the record and adopt a newer retained
//     request into it. A request newer than the gate's can only have been
//     minted after the gate's batch completed — the reject-minted W4 — so the
//     adopt clears the gate in the same compare-and-swap.
//  2. Only a record that is absent or terminal is settled as such; a live one
//     must be awaiting THIS gate, by the same identity rule the warm resolve
//     applies. Anything else is an answer that arrived too late to act on:
//     acknowledged as inapplicable, nothing written, nothing rebuilt.
//  3. I4 (the gate names the request the record names) and the gated result
//     in the applied set, against the record step 0 left. Either missing is
//     conflicting evidence and is quarantined.
//  4. Rebuild from the retained request and the retained response that
//     carries the gated batch. Either confirmed absent fails the loop with
//     continuation_unavailable through the terminal owner; an unreadable
//     stream is retried.
//
// The rebuild seats the batch from the retained response, which still lists
// every call. The gate cleared the calls queued behind it when it fired
// (gateForApproval), and restoreToolBatch rebuilds that empty queue from the
// gate's placeholder in the applied set: the process that gated the loop
// would never have dispatched them either.
func (c *Component) settleApprovalResponseWithoutLoop(
	ctx context.Context, response agentic.ApprovalResponse,
) (bool, error) {
	loopID := response.LoopID
	record, err := c.adoptNewerRetainedRequest(ctx, loopID)
	if err != nil {
		return false, err
	}
	if record.presence == loopPresenceStale {
		logApprovalResponseIgnored(c.logger, response, record.entity.State)
		c.recordApprovalInapplicable(response)
		return false, nil
	}

	gate := record.entity.PendingApproval
	if record.entity.State != agentic.LoopStateAwaitingApproval ||
		!approvalAnswersGate(gate, response.CallID, response.ExecutionID) {
		logApprovalResponseIgnored(c.logger, response, record.entity.State)
		c.recordApprovalInapplicable(response)
		return false, nil
	}

	if gate.RequestID != record.entity.PublishedRequestID {
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: its approval gate names request %q and its record names %q (I4)",
				loopID, gate.RequestID, record.entity.PublishedRequestID),
			"agentic-loop", "settleApprovalResponseWithoutLoop", "check the gate against the record")
	}
	gatedKey := gate.ExecutionID
	if gatedKey == "" {
		// Unreachable on this tree: every gate is minted from a routed tool
		// result, which carries its execution identity. Storage is greenfield
		// (pre-v1), so no record holds a gate from before execution identity.
		// Kept to mirror StoreToolResult's own keying.
		gatedKey = gate.CallID
	}
	if _, present := record.entity.PendingToolResults[gatedKey]; !present {
		return false, errs.WrapFatal(
			fmt.Errorf("loop %s: its record is gated on execution %q and carries no result for it",
				loopID, gatedKey),
			"agentic-loop", "settleApprovalResponseWithoutLoop", "check the gated result against the record")
	}

	if err := c.restoreLoopFromEvidence(ctx, loopID, record, gate.ExecutionID); err != nil {
		if errors.Is(err, errRetainedEvidenceAbsent) {
			return false, c.failContinuationUnavailable(ctx, record, err)
		}
		return false, err
	}
	return true, nil
}

// failContinuationUnavailable fails a loop whose approval cannot be continued
// because its retained request or response is confirmed gone. The failure is
// the ordinary loop failure, committed by the terminal owner — COMPLETE_ by
// Create, the graph stamps, agent.failed, then the record by compare-and-swap
// — so the answer is acknowledged only once the failure is durable, and a
// redelivery after a partial commit adopts it.
//
// The loop is seated from its record alone for the owner to write from: there
// is no conversation to rebuild, and the loop is never continued.
func (c *Component) failContinuationUnavailable(ctx context.Context, record loopRecord, cause error) error {
	loopID := record.entity.ID
	if err := c.handler.loopManager.seatRecordToFail(record.entity); err != nil {
		return err
	}
	c.rememberLoopRevision(loopID, record.revision)
	// The failure path records a terminal observation into the loop's
	// trajectory aggregate, which a seat does not create. Its error is always
	// nil (see restoreLoopFromEvidence).
	_, _ = c.handler.trajectoryManager.startTrajectory(loopID)
	return c.handleLoopFailure(ctx, loopID, record.entity, continuationUnavailableReason, cause)
}
