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
// Memory-only, deliberately (#1330, docket OQ2): the sweep reads the
// loops THIS process holds, and no startup pass reads AGENT_LOOPS to
// restore the ones it does not. PendingApproval is KV-persisted with
// RequestedAt and Timeout, so a loop a redelivery rebuilds here comes
// back with its original deadline already computed; a loop nothing
// rebuilds is never swept by this process at all and stays parked
// until the approval is answered or the loop is cancelled.
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
			LoopID: cand.LoopID,
			CallID: cand.CallID,
			// The sweeper echoes the gated call's execution identity for the
			// same reason a UI must: the matcher refuses a response that omits
			// it against a pending approval that has one, and an auto-reject
			// that could not be matched would leave the loop gated forever.
			ExecutionID: cand.ExecutionID,
			RequestID:   cand.RequestID,
			Decision:    agentic.ApprovalDecisionReject,
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
		// The sweeper keeps its own publish-then-write order until #1362; only
		// the writer itself changed, to the carrier's compare-and-swap. The
		// stamp sits between the pair, where the carrier's own publish-first
		// order puts it: the auto-reject is a request-MINTING transition
		// (handleRejectedApproval → HandleToolResult → handleToolsComplete),
		// and a record whose iterations moved without its published_request_id
		// is invariant I3 violated on a loop nothing later repairs. A sweep
		// that minted nothing stamps nothing — mintedRequestID returns "" and
		// stampPublishedRequest is a no-op.
		//
		// None of the three failures below has a delivery to retry — this is a
		// timer, not a consumer — so each is named rather than silently
		// swallowed. The publish failure STOPS this candidate, for the reason
		// written at it; the two writes after it are log-only, because no
		// loop-side counter's subject is "a write this process meant to make
		// did not commit" and #1362 owns whether one is owed (design § 5.6).
		if err := c.publishResults(ctx, result); err != nil {
			// The advance goes no further than this process's memory. Stamping
			// or persisting past a failed publication commits a record naming a
			// request the stream does not retain, which is I1; with KV still
			// writable that is exactly what lands — the record names R2, the
			// stream holds only R1, and every later cold read of the loop takes
			// adoptNewerRetainedRequest's Fatal I1 arm, quarantining a lane
			// over an approval that merely timed out. Skipping only the stamp
			// is not the answer either: persisting the advanced entity under
			// the old name is the iteration/identity mismatch the stamp exists
			// to prevent. So the record keeps the gated predecessor state it
			// already holds, which is what a replacement can still recover
			// from. This is the carrier's own shape for the same error
			// (publishThenPersistResultState returns before it stamps).
			//
			// The loop this process already advanced IN MEMORY is left as it
			// is: a timer has no delivery to classify, so there is nothing here
			// to retry or quarantine, and the sweeper's retry/counter policy
			// travels with the lane to #1362 (design § 5.6).
			c.logger.Warn("approval timeout auto-reject did not publish its results — "+
				"the record keeps the gated state it already holds",
				slog.String("loop_id", cand.LoopID),
				slog.String("call_id", cand.CallID),
				slog.String("error", err.Error()))
			continue
		}
		if err := c.stampPublishedRequest(result); err != nil {
			c.logger.Warn("approval timeout auto-reject did not name the request it published",
				slog.String("loop_id", cand.LoopID),
				slog.String("call_id", cand.CallID),
				slog.String("error", err.Error()))
		}
		if err := c.persistLoopState(ctx, cand.LoopID); err != nil {
			// On a lost compare-and-swap the loop's in-process state is
			// already released, and the record that won holds the gate.
			c.logger.Warn("approval timeout auto-reject did not commit the loop record",
				slog.String("loop_id", cand.LoopID),
				slog.String("call_id", cand.CallID),
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
	subject, err := component.ResolveSubject(inputs, "agent.approval_response", response.LoopID)
	if err != nil {
		c.logger.Error("failed to resolve approval response subject", slog.String("loop_id", response.LoopID), slog.String("error", err.Error()))
		return
	}

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
