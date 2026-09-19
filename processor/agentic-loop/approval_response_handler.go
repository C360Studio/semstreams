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
// Other mismatches (loop not found, response.Validate() fails) return
// a defensive log + non-error empty result so duplicate or stale UI
// clicks don't crash the loop.
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

	pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.CallID)
	if resolveErr != nil && !errors.Is(resolveErr, ErrLoopNotFound) {
		return HandlerResult{}, resolveErr
	}
	if !ok {
		// Stale, duplicate, or settled: the loop is no longer awaiting
		// approval, the response targets a different call_id than the one
		// currently pinned, or the loop settled and its per-loop state was
		// released. All three are the same event — a response that arrived too
		// late to act on — and the right move is to log and drop, never error,
		// never dispatch. A released loop MUST land here rather than on the
		// error return above: absence and terminal presence are the same fact
		// to a late arrival, and reporting one as a fault would make an
		// expected steady state look like a defect.
		entity, getErr := h.GetLoop(loopID)
		state := agentic.LoopState("")
		if getErr == nil {
			state = entity.State
		}
		h.logger.Warn("approval response ignored: not awaiting or call_id mismatch",
			slog.String("loop_id", loopID),
			slog.String("response_call_id", response.CallID),
			slog.String("loop_state", string(state)))
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
		// The handler dispatched nothing and resolved nothing. Persisting would
		// re-Put a settled entity, or — once its per-loop state is released —
		// report a persistence failure for a loop that is supposed to be gone.
		// Returning here is what makes the two indistinguishable.
		return natsclient.DeliveryDecisionAck, nil
	}

	// Approval responses use the same persistence boundary as every other
	// handler result. This keeps a rejection that reaches the iteration cap from
	// bypassing the ordinary-observations-then-terminal audit ordering.
	if err := c.persistHandlerResult(ctx, result); err != nil {
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("approval result for loop %q has unknown durable state: %w", response.LoopID, err)
	}
	return natsclient.DeliveryDecisionAck, nil
}
