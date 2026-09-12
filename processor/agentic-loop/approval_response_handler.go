package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

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
// LoopManager.ResolveApprovalIfPending serializes matching-execution resolution
// inside this process. This is not cross-owner exclusion. A local nonmatch
// returns staleDrop; only the component's exact durable read can authorize an
// inapplicable ACK. Invalid payloads and same-execution correlation conflicts
// return errors to the delivery owner.
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

	pending, ok, resolveErr := h.loopManager.ResolveApprovalIfPending(loopID, response.ExecutionID, response.CallID)
	if resolveErr != nil && !errors.Is(resolveErr, ErrLoopNotFound) {
		return HandlerResult{}, resolveErr
	}
	if !ok {
		// No local matching execution gate: do not dispatch. Missing process
		// state alone does not prove durable inapplicability or authorize ACK.
		entity, getErr := h.GetLoop(loopID)
		state := agentic.LoopState("")
		if getErr == nil {
			state = entity.State
		}
		h.logger.Warn("approval response ignored: no local matching execution gate",
			slog.String("loop_id", loopID),
			slog.String("execution_id", response.ExecutionID),
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
		TraceID:     pending.TraceID,
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
	if err := response.Validate(); err != nil {
		return natsclient.DeliveryDecisionTerminate, fmt.Errorf("validate approval response: %w", err)
	}

	c.logger.Debug("Processing approval response",
		slog.String("loop_id", response.LoopID),
		slog.String("call_id", response.CallID),
		slog.String("decision", response.Decision),
		slog.String("approved_by", response.ApprovedBy))

	entity, getErr := c.handler.GetLoop(response.LoopID)
	needsRecovery := getErr != nil || entity.PendingApproval == nil
	if !needsRecovery {
		loopID, routed := c.handler.loopManager.GetLoopForToolCall(entity.PendingApproval.ExecutionID)
		needsRecovery = !routed || loopID != response.LoopID
	}
	persisted, revision, err := c.readLoopEntityRevision(ctx, response.LoopID)
	if err != nil {
		return loopSettlementDecision(err), err
	}
	if revision == 0 {
		return natsclient.DeliveryDecisionRetry,
			fmt.Errorf("approval continuation for loop %q has no proven current pending state", response.LoopID)
	}
	pending := persisted.PendingApproval
	if persisted.State != agentic.LoopStateAwaitingApproval && pending != nil {
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("loop %q has pending approval outside awaiting-approval state", response.LoopID)
	}
	if persisted.State == agentic.LoopStateAwaitingApproval && (pending == nil ||
		pending.ExecutionID == "" || pending.RequestID == "" || pending.CallID == "" || pending.CallOrdinal == 0 || pending.ToolName == "") {
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("loop %q awaits approval without coherent pending execution identity", response.LoopID)
	}
	if persisted.State != agentic.LoopStateAwaitingApproval || pending.ExecutionID != response.ExecutionID {
		c.logger.WarnContext(ctx, "approval response inapplicable: no matching current gate",
			slog.String("loop_id", response.LoopID), slog.String("execution_id", response.ExecutionID))
		if c.metrics != nil {
			c.metrics.approvalDecisionsInapplicable.Inc()
		}
		return natsclient.DeliveryDecisionAck, nil
	}
	if pending.CallID != response.CallID {
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("approval execution %q conflicts with current call identity", response.ExecutionID)
	}
	if !needsRecovery && !reflect.DeepEqual(entity.PendingApproval, persisted.PendingApproval) {
		return natsclient.DeliveryDecisionQuarantine,
			fmt.Errorf("approval for loop %q conflicts with the current durable pending identity", response.LoopID)
	}
	if needsRecovery {
		if err := c.recoverApprovalResponse(ctx, response, persisted); err != nil {
			return loopSettlementDecision(err), err
		}
	}
	result, err := c.handler.HandleApprovalResponse(ctx, response)
	if err != nil {
		c.releaseLoopTransientState(response.LoopID)
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
		return natsclient.DeliveryDecisionRetry,
			fmt.Errorf("approval for loop %q lacks durable branch-applied proof", response.LoopID)
	}

	if result.State.IsTerminal() {
		// Rejection terminal effects retain the established final-marker order.
		if err := c.persistHandlerResult(ctx, result); err != nil {
			return natsclient.DeliveryDecisionQuarantine,
				fmt.Errorf("approval result for loop %q has unknown durable state: %w", response.LoopID, err)
		}
	} else {
		// Resolution is speculative until the branch publication has PubAck.
		// A failed attempt discards process state for a fresh authority read.
		resolved, err := c.handler.GetLoop(result.LoopID)
		if err != nil {
			c.releaseLoopTransientState(response.LoopID)
			return loopSettlementDecision(err), err
		}
		data, err := json.Marshal(resolved)
		if err != nil {
			c.releaseLoopTransientState(response.LoopID)
			return loopSettlementDecision(err), err
		}
		c.recordHandlerResultTrajectory(ctx, result)
		if err := c.publishResults(ctx, result); err != nil {
			c.releaseLoopTransientState(response.LoopID)
			return loopSettlementDecision(err), err
		}
		// Bind the cleared-pending commit to the checkpoint this approval used.
		// A faster result owner may already have advanced the durable loop.
		if _, err := c.loopsBucket.Update(ctx, result.LoopID, data, revision); err != nil {
			c.releaseLoopTransientState(response.LoopID)
			return natsclient.DeliveryDecisionRetry, fmt.Errorf("commit approved loop %s: %w", result.LoopID, err)
		}
	}
	return natsclient.DeliveryDecisionAck, nil
}
