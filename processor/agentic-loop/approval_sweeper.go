package agenticloop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// approvalSweepInterval is the cadence at which the component scans
// loops in LoopStateAwaitingApproval for expired timeouts. Hardcoded
// (not config) because the latency tradeoff is small — the sweep is
// cheap, human-approval timeouts are typically minutes, and a few-
// second slip past the deadline is acceptable. Make this configurable
// only if a product surfaces a real reason.
const approvalSweepInterval = 5 * time.Second

// approvalTimeoutSystemApprover is stamped into ApprovalResponse.ApprovedBy
// for sweeper-driven auto-rejects so downstream observers (audit,
// ops.diagnosis.* triples, metrics keyed on ApprovedBy) can
// distinguish framework auto-rejects from human "anonymous" rejects.
// Without a distinct sentinel, both shapes coalesce in dashboards
// because handleRejectedApproval's "anonymous" fallback fires for
// any empty wire field. Cheap to differentiate now; painful to
// retrofit once consumers start matching on the value.
const approvalTimeoutSystemApprover = "system:approval-timeout"

// runApprovalTimeoutSweeper drives the approval-timeout auto-reject
// loop. Started when the component starts; stopped when ctx cancels.
// Closes mode (f) of orphan-tool-call recovery: a stuck human-approval
// flow now auto-rejects after Config.ApprovalTimeoutStr, feeding a
// synth-rejection through the existing HandleApprovalResponse path
// rather than leaving the gated tool_call orphaned indefinitely.
//
// Restart-safe: PendingApproval is KV-persisted with RequestedAt and
// Timeout, so a restored loop's deadline is computed correctly on the
// first sweep after process restart.
func (c *Component) runApprovalTimeoutSweeper(ctx context.Context) {
	ticker := time.NewTicker(approvalSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweepExpiredApprovals(ctx)
		}
	}
}

// sweepExpiredApprovals snapshots loops with expired approval
// deadlines and feeds an auto-rejection through the normal approval
// response path. Each candidate is processed serially under the
// sweeper goroutine; this is fine because the snapshot is bounded by
// the loop count and HandleApprovalResponse is fast (no I/O beyond
// any KV writes the publisher does post-handler).
//
// Best-effort: a per-candidate failure logs and continues so one
// stuck loop can't block timeouts on its peers.
func (c *Component) sweepExpiredApprovals(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	candidates := c.handler.loopManager.SnapshotExpiredApprovals(time.Now().UTC())
	if len(candidates) == 0 {
		return
	}
	c.logger.Info("processing expired approval timeouts",
		slog.Int("count", len(candidates)))

	for _, cand := range candidates {
		response := agentic.ApprovalResponse{
			LoopID:   cand.LoopID,
			CallID:   cand.CallID,
			Decision: agentic.ApprovalDecisionReject,
			Reason: fmt.Sprintf("approval timed out after %s",
				cand.Timeout.Round(time.Second)),
			ApprovedBy: approvalTimeoutSystemApprover,
			DecidedAt:  time.Now().UTC(),
		}
		result, err := c.handler.HandleApprovalResponse(ctx, response)
		if err != nil {
			c.logger.Error("approval timeout auto-reject failed",
				slog.String("loop_id", cand.LoopID),
				slog.String("call_id", cand.CallID),
				slog.String("error", err.Error()))
			continue
		}
		c.publishResults(ctx, result)
		c.persistLoopState(ctx, cand.LoopID)
		c.logger.Info("approval timed out; auto-rejected",
			slog.String("loop_id", cand.LoopID),
			slog.String("call_id", cand.CallID),
			slog.String("tool_name", cand.ToolName),
			slog.Duration("timeout", cand.Timeout))
	}
}
