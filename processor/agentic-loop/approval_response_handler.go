package agenticloop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
)

// HandleApprovalResponse processes an ApprovalResponse for a loop
// awaiting human approval. Returns a HandlerResult populated with
// either a re-dispatched ToolCall (approve/modify) or a synthesised
// rejection ToolResult fed through the normal HandleToolResult path
// (reject). The component publishes any messages and persists the
// loop state.
//
// Mismatches (loop not awaiting approval, call_id pinned to a
// different tool, response.Validate() fails) return a defensive log
// + a non-error empty result so duplicate or stale UI clicks don't
// crash the loop.
func (h *MessageHandler) HandleApprovalResponse(ctx context.Context, response agentic.ApprovalResponse) (HandlerResult, error) {
	if err := response.Validate(); err != nil {
		return HandlerResult{}, errs.WrapInvalid(err, "agentic-loop", "HandleApprovalResponse", "validate response")
	}

	loopID := response.LoopID
	entity, err := h.GetLoop(loopID)
	if err != nil {
		return HandlerResult{}, err
	}

	if entity.State != agentic.LoopStateAwaitingApproval {
		h.logger.Warn("approval response ignored: loop not awaiting approval",
			slog.String("loop_id", loopID),
			slog.String("state", string(entity.State)),
			slog.String("call_id", response.CallID))
		return HandlerResult{LoopID: loopID, State: entity.State}, nil
	}
	if entity.PendingApproval == nil || entity.PendingApproval.CallID != response.CallID {
		var pendingID string
		if entity.PendingApproval != nil {
			pendingID = entity.PendingApproval.CallID
		}
		h.logger.Warn("approval response ignored: call_id mismatch",
			slog.String("loop_id", loopID),
			slog.String("response_call_id", response.CallID),
			slog.String("pending_call_id", pendingID))
		return HandlerResult{LoopID: loopID, State: entity.State}, nil
	}

	pending := *entity.PendingApproval
	if err := entity.ResolveApproval(); err != nil {
		return HandlerResult{}, errs.Wrap(err, "agentic-loop", "HandleApprovalResponse", "resolve approval")
	}
	if err := h.UpdateLoop(entity); err != nil {
		return HandlerResult{}, errs.Wrap(err, "agentic-loop", "HandleApprovalResponse", "persist resolved entity")
	}

	result := HandlerResult{
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
		ID:         pending.CallID,
		Name:       pending.ToolName,
		Arguments:  args,
		ApprovedBy: approvedBy,
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
		CallID:    pending.CallID,
		Name:      pending.ToolName,
		ErrorKind: agentic.ToolErrorPermission,
		Error:     fmt.Sprintf("%srejected by %s: %s", agentic.ApprovalRejectedPrefix, approver, reasonSuffix),
		TraceID:   pending.TraceID,
	}
	return h.HandleToolResult(ctx, loopID, synthetic)
}

// handleApprovalResponseMessage is the component-level entry point
// that decodes the wire envelope and hands the typed payload to the
// MessageHandler. Mirrors handleSignalMessage's shape.
func (c *Component) handleApprovalResponseMessage(ctx context.Context, data []byte) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Error("Failed to decode approval response", "error", err)
		return
	}
	respPtr, ok := baseMsg.Payload().(*agentic.ApprovalResponse)
	if !ok {
		c.logger.Error("Unexpected approval response payload type",
			"type", fmt.Sprintf("%T", baseMsg.Payload()))
		return
	}
	response := *respPtr

	c.logger.Debug("Processing approval response",
		slog.String("loop_id", response.LoopID),
		slog.String("call_id", response.CallID),
		slog.String("decision", response.Decision),
		slog.String("approved_by", response.ApprovedBy))

	result, err := c.handler.HandleApprovalResponse(ctx, response)
	if err != nil {
		c.logger.Error("Failed to handle approval response",
			"error", err,
			"loop_id", response.LoopID,
			"call_id", response.CallID)
		return
	}

	c.publishResults(ctx, result)
	c.persistLoopState(ctx, response.LoopID)
}
