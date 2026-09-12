package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/looptoken"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

// restoreApprovalDeadlines hydrates only timer candidates before input admission.
// The native approval owner restores task and execution context when a decision arrives.
func (c *Component) restoreApprovalDeadlines(ctx context.Context) (restoreErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.loopsBucket == nil {
		return errors.New("AGENT_LOOPS is unavailable")
	}
	watcher, err := c.loopsBucket.WatchAll(ctx, jetstream.MetaOnly(), jetstream.IgnoreDeletes())
	if err != nil {
		return fmt.Errorf("snapshot approval deadlines: %w", err)
	}
	stopped := false
	defer func() {
		if !stopped {
			restoreErr = errors.Join(restoreErr, watcher.Stop())
		}
	}()
	var keys []string
snapshot:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, open := <-watcher.Updates():
			if !open {
				return errors.New("approval deadline snapshot closed before initial completion")
			}
			if entry == nil {
				break snapshot
			}
			if looptoken.Valid(entry.Key()) {
				keys = append(keys, entry.Key())
			}
		}
	}
	stopped = true
	if err := watcher.Stop(); err != nil {
		return fmt.Errorf("stop approval deadline snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		entity, found, err := c.readLoopEntity(ctx, key)
		if err != nil {
			return err
		}
		if !found || entity.State != agentic.LoopStateAwaitingApproval ||
			entity.PendingApproval == nil || entity.PendingApproval.Timeout <= 0 {
			continue
		}
		if _, err := c.handler.loopManager.CreateLoopWithID(entity.ID, entity.TaskID, entity.Role,
			entity.Model, entity.MaxIterations); err != nil {
			return err
		}
		if err := c.handler.UpdateLoop(entity); err != nil {
			return err
		}
	}
	return nil
}

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
// Startup restores timed PendingApproval records before input admission;
// their persisted RequestedAt and Timeout govern the original deadline.
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
// deadlines and publishes rejection work to the existing native approval
// input. Only that consumer applies and persists the approval decision.
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
			LoopID:      cand.LoopID,
			CallID:      cand.CallID,
			ExecutionID: cand.ExecutionID,
			Decision:    agentic.ApprovalDecisionReject,
			Reason: fmt.Sprintf("approval timed out after %s",
				cand.Timeout.Round(time.Second)),
			ApprovedBy: approvalTimeoutSystemApprover,
			DecidedAt:  time.Now().UTC(),
		}
		if err := c.publishApprovalResponseToWire(ctx, response); err != nil {
			// The publisher logs the error. Pending stays intact for retry, never a silent skip.
			if c.metrics != nil {
				c.metrics.approvalTimeoutPublishFailures.Inc()
			}
			continue
		}
		c.logger.Info("approval timeout rejection published",
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
// external UIs publish to). The component's native consumer owns application,
// required effects, persistence and source settlement.
//
// Failures are logged and returned; none may imply that pending was resolved.
func (c *Component) publishApprovalResponseToWire(ctx context.Context, response agentic.ApprovalResponse) error {
	envelope := message.NewBaseMessage(response.Schema(), &response, "agentic-loop")
	data, err := json.Marshal(envelope)
	if err != nil {
		c.logger.Error("failed to marshal approval response for wire publish",
			slog.String("loop_id", response.LoopID),
			slog.String("error", err.Error()))
		return err
	}

	var inputs []component.PortDefinition
	if c.config.Ports != nil {
		inputs = c.config.Ports.Inputs
	}
	subject, err := component.ResolveSubject(inputs, "agent.approval_response", response.LoopID)
	if err != nil {
		c.logger.Error("failed to resolve approval response subject", slog.String("loop_id", response.LoopID), slog.String("error", err.Error()))
		return err
	}

	if c.testPublishHook != nil {
		c.testPublishHook(subject, data)
		return nil
	}
	if c.natsClient == nil {
		err := errors.New("approval response publisher is unavailable")
		c.logger.Error("failed to publish approval response to wire",
			slog.String("loop_id", response.LoopID), slog.String("error", err.Error()))
		return err
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("failed to publish approval response to wire",
			slog.String("loop_id", response.LoopID),
			slog.String("subject", subject),
			slog.String("error", err.Error()))
		return err
	}
	return nil
}
