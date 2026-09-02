package agenticloop

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

const trajectoryAuditBatchBudget = 250 * time.Millisecond

type trajectoryToolCompletionEvidence struct {
	Result            agentic.ToolResult `json:"result"`
	DispatchArguments map[string]any     `json:"dispatch_arguments,omitempty"`
}

type trajectoryCompactionEvidence struct {
	Before []agentic.ChatMessage `json:"before"`
	After  []agentic.ChatMessage `json:"after"`
	Result CompactionResult      `json:"result"`
}

type trajectoryTerminalEvidence struct {
	Loop       agentic.LoopEntity          `json:"loop"`
	Completion *agentic.LoopCompletedEvent `json:"completion,omitempty"`
	Failure    *agentic.LoopFailedEvent    `json:"failure,omitempty"`
	Cancelled  *agentic.LoopCancelledEvent `json:"cancelled,omitempty"`
}

func appendTrajectoryObservation(result *HandlerResult, observation trajectoryObservation) {
	result.trajectoryObservations = append(result.trajectoryObservations, observation)
}

// releaseLoopTransientState releases every per-loop in-memory aggregate the
// component holds once a loop has no more active execution consumers: the
// trajectory step aggregate, the observed-audit-loss marker, and — since
// #1233 — every per-loop map the loop manager holds for it, via DeleteLoop.
//
// It is the single terminal-release point so a future terminal path cannot
// release one and leak the other. Callers defer it (or call it on an early
// terminal return) AFTER the loop's terminal observation and terminal graph
// write, both of which read the state this releases. Idempotent.
//
// The loop-manager release is admissible only under one invariant, which the
// agentic-loop spec states: after a loop settles, the ABSENCE of its in-process
// entity is indistinguishable from its PRESENCE in a terminal state. Every
// reader that can still be handed a message for a settled loop — a late or
// duplicate approval response, a late tool result, a late model response —
// drops it as expected rather than reporting a failure. That case was already
// reachable whenever a process replacement preceded the late message; the
// release makes it common, which is why it is now a contract rather than an
// accident. Nothing here reads agent execution evidence, and the durable loop
// record remains the authority for a settled loop's result.
//
// HandleTask's own rollback still discards only the trajectory, and correctly
// so: MessageHandler holds no *Component and no trajectoryRecorder, so
// reportTrajectoryAuditFailure is unreachable from inside HandleTask, and there
// is no audit-loss marker for it to release. It must not release the loop
// either — since #1227 that loop may be one it ATTACHED to rather than created,
// and releasing a live conversation on a failed continuation would destroy
// exactly what the fence exists to preserve.
func (c *Component) releaseLoopTransientState(loopID string) {
	c.handler.trajectoryManager.discardTrajectory(loopID)
	c.trajectoryAuditLoss.release(loopID)
	// Always nil; the signature predates this, its only production caller.
	_ = c.handler.loopManager.DeleteLoop(loopID)
}

func (c *Component) recordTrajectoryObservations(ctx context.Context, result HandlerResult) {
	c.recordTrajectoryBatch(ctx, result.trajectoryObservations)
}

func (c *Component) recordResultTerminalObservation(ctx context.Context, result HandlerResult) {
	observation, ok := c.resultTerminalObservation(result)
	if !ok {
		return
	}
	c.recordTrajectoryBatch(ctx, []trajectoryObservation{observation})
}

func (c *Component) recordHandlerResultTrajectory(ctx context.Context, result HandlerResult) {
	observations := append([]trajectoryObservation(nil), result.trajectoryObservations...)
	if terminal, ok := c.resultTerminalObservation(result); ok {
		observations = append(observations, terminal)
	}
	c.recordTrajectoryBatch(ctx, observations)
}

func (c *Component) resultTerminalObservation(result HandlerResult) (trajectoryObservation, bool) {
	if result.State != agentic.LoopStateComplete && result.State != agentic.LoopStateFailed {
		return trajectoryObservation{}, false
	}
	entity, _ := c.handler.GetLoop(result.LoopID)
	terminal := trajectoryTerminalEvidence{
		Loop:       entity,
		Completion: result.CompletionState,
		Failure:    result.FailureState,
	}
	status := agentic.TrajectoryStatusCompleted
	errorCategory := agentic.TrajectoryErrorCategory("")
	if result.State == agentic.LoopStateFailed {
		status = agentic.TrajectoryStatusFailed
		errorCategory = agentic.TrajectoryErrorUnknown
	}
	return newTerminalObservation(result.LoopID, status, errorCategory, terminal), true
}

func (c *Component) recordTerminalObservation(
	ctx context.Context,
	loopID string,
	status agentic.TrajectoryStatus,
	errorCategory agentic.TrajectoryErrorCategory,
	evidence trajectoryTerminalEvidence,
) {
	c.recordTrajectoryBatch(ctx, []trajectoryObservation{
		newTerminalObservation(loopID, status, errorCategory, evidence),
	})
}

func newTerminalObservation(
	loopID string,
	status agentic.TrajectoryStatus,
	errorCategory agentic.TrajectoryErrorCategory,
	evidence trajectoryTerminalEvidence,
) trajectoryObservation {
	iteration := uint32(0)
	elapsed := int64(0)
	if evidence.Loop.ID != "" {
		if evidence.Loop.Iterations > 0 {
			iteration = uint32(evidence.Loop.Iterations)
		}
		if !evidence.Loop.StartedAt.IsZero() {
			elapsed = time.Since(evidence.Loop.StartedAt).Milliseconds()
		}
	}
	return trajectoryObservation{
		LoopID:          loopID,
		Kind:            agentic.TrajectoryKindLoopTerminal,
		CausalIteration: iteration,
		CausalPhase:     agentic.TrajectoryPhaseTerminal,
		ElapsedMS:       elapsed,
		Status:          status,
		ErrorCategory:   errorCategory,
		Evidence:        evidence,
	}
}

func (c *Component) recordTrajectoryBatch(parent context.Context, observations []trajectoryObservation) {
	c.recordTrajectoryBatchWithin(parent, observations, trajectoryAuditBatchBudget)
}

func (c *Component) recordTrajectoryBatchWithin(
	parent context.Context,
	observations []trajectoryObservation,
	budget time.Duration,
) {
	if c.trajectoryRecorder == nil || len(observations) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), budget)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		release, acquired := c.trajectoryRecorder.acquireLoopBatch(ctx, observations[0].LoopID)
		if !acquired {
			return
		}
		defer release()
		for _, observation := range observations {
			if ctx.Err() != nil {
				return
			}
			c.trajectoryRecorder.record(ctx, observation)
		}
	}()

	select {
	case <-done:
		if ctx.Err() == nil {
			return
		}
	case <-ctx.Done():
	}
	first := observations[0]
	c.reportTrajectoryAuditFailure(trajectoryAuditFailure{
		// The batch deadline means the immutable observation was not created
		// within the framework budget. Keep the accepted audit-stage vocabulary
		// closed by classifying the visible loss at the fact-create boundary.
		Stage: trajectoryStageFactCreate, Kind: first.Kind, Reason: trajectoryReasonTimeout,
		LoopID: first.LoopID, AttemptID: c.trajectoryRecorder.newAttemptID(),
		Err: fmt.Errorf("trajectory audit batch exceeded framework budget %s", budget),
	})
}

func positiveUint32(value int) uint32 {
	if value <= 0 {
		return 0
	}
	return uint32(value)
}

func positiveUint64(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func modelResponseStatus(response agentic.AgentResponse) agentic.TrajectoryStatus {
	if response.Status == agentic.StatusError || response.Status == agentic.StatusLengthTruncated {
		return agentic.TrajectoryStatusFailed
	}
	return agentic.TrajectoryStatusCompleted
}

func modelResponseErrorCategory(response agentic.AgentResponse) agentic.TrajectoryErrorCategory {
	if response.Status == agentic.StatusLengthTruncated {
		return agentic.TrajectoryErrorValidation
	}
	if response.Status == agentic.StatusError {
		return agentic.TrajectoryErrorModel
	}
	return ""
}

func toolResultStatus(result agentic.ToolResult) agentic.TrajectoryStatus {
	if result.Error != "" {
		return agentic.TrajectoryStatusFailed
	}
	return agentic.TrajectoryStatusCompleted
}

func toolErrorCategory(result agentic.ToolResult) agentic.TrajectoryErrorCategory {
	if result.Error == "" {
		return ""
	}
	switch result.ErrorKind {
	case agentic.ToolErrorPermission:
		return agentic.TrajectoryErrorPermission
	case agentic.ToolErrorInvalidArgs:
		return agentic.TrajectoryErrorValidation
	default:
		return agentic.TrajectoryErrorTool
	}
}
