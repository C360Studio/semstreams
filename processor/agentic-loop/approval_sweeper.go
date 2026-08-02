package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
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
		if err := c.persistLoopState(ctx, cand.LoopID); err != nil {
			c.logger.Error("timeout-rejected loop state not persisted",
				slog.String("loop_id", cand.LoopID),
				slog.String("error", err.Error()))
		}
		// Publish the ApprovalResponse onto agent.approval_response.<loopID> so
		// wire observers (sister-repo dashboards, audit consumers) see timeout
		// auto-rejects the same way they see human responses. The component's
		// own consumer receives this and drops it as a stale response (the
		// approval is already resolved by HandleApprovalResponse above), so
		// there is no double-processing risk.
		c.publishApprovalResponseToWire(ctx, response)
		c.logger.Info("approval timed out; auto-rejected",
			slog.String("loop_id", cand.LoopID),
			slog.String("call_id", cand.CallID),
			slog.String("tool_name", cand.ToolName),
			slog.Duration("timeout", cand.Timeout))
	}
}

// publishApprovalResponseToWire publishes an ApprovalResponse to the
// agent.approval_response.<loopID> JetStream subject so wire observers
// (dashboards, audit consumers, sister repos) see timeout auto-rejects
// symmetrically with human approvals.
//
// Subject is resolved from the input-port configuration (the same subject
// external UIs publish to). The component's own consumer safely drops the
// message as a stale-response idempotent no-op because the approval is
// already resolved before this publish fires.
//
// Guards against nil natsClient (unit tests, Stop race) and logs but does
// not fail on marshal/publish errors — the in-process state transition via
// HandleApprovalResponse is already committed.
func (c *Component) publishApprovalResponseToWire(ctx context.Context, response agentic.ApprovalResponse) {
	envelope := message.NewBaseMessage(response.Schema(), &response, "agentic-loop")
	data, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("failed to marshal approval response for wire publish",
			slog.String("loop_id", response.LoopID),
			slog.String("error", err.Error()))
		return
	}

	var inputs []component.PortDefinition
	if c.config.Ports != nil {
		inputs = c.config.Ports.Inputs
	}
	subject := component.ResolveSubject(inputs, "agent.approval_response", response.LoopID)

	if c.testPublishHook != nil {
		c.testPublishHook(subject, data)
		return
	}
	if c.natsClient == nil {
		return
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("failed to publish approval response to wire",
			slog.String("loop_id", response.LoopID),
			slog.String("subject", subject),
			slog.String("error", err.Error()))
	}
}
